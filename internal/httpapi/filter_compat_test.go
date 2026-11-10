package httpapi

// Route-level guards for the filter compatibility split, through the REAL
// registered routes (httpapi.RegisterRoutes) rather than a package-local test
// app. PR #130 made every documented-but-unstored catalogue parameter a 400, so
// a client sending ?watchnow=1 to a history read got an error envelope instead
// of its rows. The rule now (see internal/handlers/shared/filters.go) is:
//
//   - a malformed value of a filter we support stays a 400 naming the accepted
//     values;
//   - a documented parameter we have no data to honour is ignored, the request
//     succeeds, and the response names it in X-Haaku-Ignored-Filters so the
//     parity row can be (and is) graded divergent instead of a false
//     `implemented`.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
)

// compatFilterApp builds the real app over a sqlite database with one watched
// movie history row and one watched episode history row for the caller, and
// returns it with a session token.
func compatFilterApp(t *testing.T) (*fiber.App, string) {
	t.Helper()
	// Built at runtime: a secret-shaped literal on any branch fails gitleaks.
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}

	db := meAliasDB(t) // seeds the caller and installs the DB for the handlers
	if err := db.AutoMigrate(&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{}); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	now := time.Now().UTC()
	released := &database.Date{Time: time.Date(2011, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := db.Create(&database.Movie{ID: 501, TmdbID: 501, Slug: "regression-movie",
		Title: "Regression Movie", Released: released, Runtime: 100, RatingAvg: 7.5,
		Status: "Released", Genres: database.StringSlice{"Drama"},
		CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.Show{ID: 601, TmdbID: 601, Slug: "regression-show",
		Title: "Regression Show", FirstAired: released, RatingAvg: 7.0, Status: "Ended",
		CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{ID: 1, ShowID: 601, SeasonNumber: 1,
		EpisodeCount: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	if err := db.Create(&database.Episode{ID: 701, ShowID: 601, SeasonID: 1, EpisodeNumber: 1,
		Title: "Pilot", Runtime: 45, AiredAt: released, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	episodeID := int64(701)
	for _, h := range []database.UserHistory{
		{UserID: meAliasUserID, MediaType: "movie", MediaID: 501, WatchedAt: now, Source: "api"},
		{UserID: meAliasUserID, MediaType: "episode", MediaID: 701, EpisodeID: &episodeID, WatchedAt: now, Source: "api"},
	} {
		if err := db.Create(&h).Error; err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}

	token, _, err := auth.CreateSession(db, meAliasUserID, "", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	RegisterRoutes(app)
	return app, token
}

// compatFilterGet issues an authenticated GET and returns status, the ignored-
// filters header and the body.
func compatFilterGet(t *testing.T, app *fiber.App, token, path string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, resp.Header.Get("X-Haaku-Ignored-Filters"), string(raw)
}

// TestUnstoredDocumentedFilterServesTheSuperset is acceptance case 1: the two
// history reads that regressed answer 200 with rows when the client sends
// ?watchnow=1, and say which parameter they did not apply.
func TestUnstoredDocumentedFilterServesTheSuperset(t *testing.T) {
	app, token := compatFilterApp(t)

	for _, path := range []string{
		"/v1/users/me/history/movies?watchnow=1",
		"/v1/users/me/history/episodes?watchnow=1",
	} {
		status, ignored, body := compatFilterGet(t, app, token, path)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d, want 200 (%s)", path, status, body)
		}
		var items []json.RawMessage
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("%s: decode: %v (%s)", path, err, body)
		}
		if len(items) == 0 {
			t.Errorf("%s: served an empty array; the seeded history row must come back as the superset", path)
		}
		if ignored != "watchnow" {
			t.Errorf("%s: %s = %q, want watchnow", path, "X-Haaku-Ignored-Filters", ignored)
		}
	}
}

// TestMalformedSupportedFilterStillBadRequest is acceptance case 2: a value the
// filter cannot represent is a 400 that names the accepted range, not a silently
// clamped or ignored filter.
func TestMalformedSupportedFilterStillBadRequest(t *testing.T) {
	app, token := compatFilterApp(t)

	for _, path := range []string{
		"/v1/users/me/history/movies?years=99999999",
		"/v1/users/me/history/movies?ratings=abc",
	} {
		status, _, body := compatFilterGet(t, app, token, path)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400 (%s)", path, status, body)
		}
		if !strings.Contains(body, "invalid_request") {
			t.Errorf("%s: 400 body is not the error envelope: %s", path, body)
		}
		if !strings.Contains(body, "9998") && !strings.Contains(body, "number or range") {
			t.Errorf("%s: 400 body does not name what is accepted: %s", path, body)
		}
	}
}

// TestBogusSortStillBadRequest is acceptance case 3: an unknown sort_by is a
// 400 naming the accepted orderings, not a fallback to one of them.
func TestBogusSortStillBadRequest(t *testing.T) {
	app, token := compatFilterApp(t)

	status, _, body := compatFilterGet(t, app, token, "/v1/sync/progress/up_next?sort_by=bogus")
	if status != http.StatusBadRequest {
		t.Fatalf("bogus sort_by: status %d, want 400 (%s)", status, body)
	}
	for _, accepted := range []string{"aired", "last_watched_at", "title"} {
		if !strings.Contains(body, accepted) {
			t.Errorf("400 body does not name accepted sort %q: %s", accepted, body)
		}
	}
}
