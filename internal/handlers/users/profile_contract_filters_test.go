package users

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// doGETFull is doGET plus the response, so a test can read the pagination
// headers the filtered count must drive.
func doGETFull(t *testing.T, app *fiber.App, path string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp, string(raw)
}

func day(y int, m time.Month, d int) *database.Date {
	return &database.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// seedFilterCatalogue seeds two movies and one show with the columns the
// catalogue filters read, plus watchlist rows for the target.
func seedFilterCatalogue(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&database.Movie{ID: 101, TmdbID: 101, Slug: "alpha", Title: "Alpha",
		Genres: database.StringSlice{"Drama"}, Released: day(2011, 1, 1), Runtime: 100,
		RatingAvg: 8.0, Status: "Released", Certification: "R", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Movie{ID: 102, TmdbID: 102, Slug: "beta", Title: "Beta",
		Genres: database.StringSlice{"Comedy"}, Released: day(2021, 1, 1), Runtime: 90,
		RatingAvg: 4.0, Status: "Released", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Show{ID: 201, TmdbID: 201, Slug: "gamma", Title: "Gamma",
		Genres: database.StringSlice{"Drama"}, FirstAired: day(2015, 1, 1),
		RatingAvg: 7.0, Status: "Ended", Certification: "TV-14", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, w := range []database.UserWatchlist{
		{UserID: "target", MediaType: "movie", MediaID: 101, Rank: 1, ListedAt: now.Add(-2 * time.Hour)},
		{UserID: "target", MediaType: "movie", MediaID: 102, Rank: 2, ListedAt: now.Add(-1 * time.Hour)},
		{UserID: "target", MediaType: "show", MediaID: 201, Rank: 1, ListedAt: now},
	} {
		if err := db.Create(&w).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestContractWatchlistCatalogueFilters(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedFilterCatalogue(t, db)
	app := newContractApp("target")

	cases := []struct {
		name  string
		path  string
		title string
	}{
		{"genre", "/v1/users/target/watchlist/movies/rank?genres=drama", "Alpha"},
		{"genre case-insensitive slug", "/v1/users/target/watchlist/movies/rank?genres=Drama", "Alpha"},
		{"years", "/v1/users/target/watchlist/movies/rank?years=2010-2012", "Alpha"},
		{"ratings", "/v1/users/target/watchlist/movies/rank?ratings=70-100", "Alpha"},
		{"runtimes", "/v1/users/target/watchlist/movies/rank?runtimes=95-105", "Alpha"},
		{"status", "/v1/users/target/watchlist/movies/rank?certifications=R", "Alpha"},
		{"show genre", "/v1/users/target/watchlist/shows/rank?genres=drama", "Gamma"},
	}
	for _, tc := range cases {
		resp, body := doGETFull(t, app, tc.path)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d (%s)", tc.name, resp.StatusCode, body)
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Errorf("%s: decode: %v", tc.name, err)
			continue
		}
		if len(items) != 1 {
			t.Errorf("%s: got %d items, want 1 (%s)", tc.name, len(items), body)
		}
		// The filtered count drives the pagination header; an unfiltered total
		// would read 2 here.
		if got := resp.Header.Get("X-Pagination-Total-Items"); got != "1" {
			t.Errorf("%s: X-Pagination-Total-Items = %q, want 1", tc.name, got)
		}
	}
}

// TestContractCatalogueFiltersIgnoreUnstored proves a declared-but-unstored
// filter is IGNORED rather than a 400: the request answers 200 with the
// superset, and the response names the parameter it did not apply (which is why
// the parity row for these operations is graded divergent with a compat note).
// A malformed value of a filter we do support stays a 400 — proven in
// shared.TestParseRefusesBadValuesAndIgnoresUnstored and by the years case here.
func TestContractCatalogueFiltersIgnoreUnstored(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedFilterCatalogue(t, db)
	app := newContractApp("target")

	cases := []struct {
		path    string
		items   int
		ignored string
	}{
		// movies: two watchlist rows, neither narrowed by an unstored filter.
		{"/v1/users/target/watchlist/movies/rank?countries=us", 2, "countries"},
		{"/v1/users/target/watchlist/movies/rank?watchnow=free", 2, "watchnow"},
		// runtimes is supported on movies but shows carry no runtime column.
		{"/v1/users/target/watchlist/shows/rank?runtimes=30-60", 1, "runtimes"},
		{"/v1/users/target/watchlist/movie,show/rank?runtimes=30-60", 3, "runtimes"},
	}
	for _, tc := range cases {
		resp, body := doGETFull(t, app, tc.path)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200 (%s)", tc.path, resp.StatusCode, body)
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Errorf("%s: decode: %v", tc.path, err)
			continue
		}
		if len(items) != tc.items {
			t.Errorf("%s: got %d items, want the %d-item superset (%s)", tc.path, len(items), tc.items, body)
		}
		if got := resp.Header.Get("X-Haaku-Ignored-Filters"); got != tc.ignored {
			t.Errorf("%s: X-Haaku-Ignored-Filters = %q, want %q", tc.path, got, tc.ignored)
		}
	}

	// A movie watchlist can honour runtimes, so nothing is ignored and the
	// filter really narrows (a 30-60 minute movie is not in this catalogue).
	resp, body := doGETFull(t, app, "/v1/users/target/watchlist/movies/rank?runtimes=30-60")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("movie runtimes: status %d, want 200 (%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Haaku-Ignored-Filters"); got != "" {
		t.Errorf("movie runtimes ignored %q; it is honour-able here", got)
	}

	// The hard 400 stays for a malformed value of a supported filter.
	resp, body = doGETFull(t, app, "/v1/users/target/watchlist/movies/rank?years=99999999")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("years=99999999: status %d, want 400 (%s)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "9998") {
		t.Errorf("400 body does not name the accepted range: %s", body)
	}
}

// TestContractWatchlistHideNarrows proves hide/ignore_* are applied rather than
// ignored — for movies through a direct history row, and for shows through
// their watched episodes (a show watch is stored as an episode row).
func TestContractWatchlistHideNarrows(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	now := time.Now().UTC()
	if err := db.Create(&database.Movie{ID: 101, TmdbID: 101, Slug: "alpha", Title: "Alpha", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Movie{ID: 102, TmdbID: 102, Slug: "beta", Title: "Beta", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{101, 102} {
		if err := db.Create(&database.UserWatchlist{UserID: "target", MediaType: "movie", MediaID: id, Rank: int(id), ListedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&database.UserHistory{UserID: "target", MediaType: "movie", MediaID: 101, WatchedAt: now, Source: "api"}).Error; err != nil {
		t.Fatal(err)
	}

	// One watchlisted show the user has watched episodes of, and one they have
	// not. Only the second is unwatched.
	watchedShow, _, episodes := seedContractMedia(t, db)
	unwatchedShow := database.Show{TmdbID: 702, Title: "Unwatched Show", Slug: "unwatched-show", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&unwatchedShow).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{watchedShow.ID, unwatchedShow.ID} {
		if err := db.Create(&database.UserWatchlist{UserID: "target", MediaType: "show", MediaID: id, Rank: 1, ListedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedEpisodeHistory(t, db, "target", episodes[0], now, "api")

	app := newContractApp("target")

	_, body := doGETFull(t, app, "/v1/users/target/watchlist/movies/rank?hide=unwatched")
	var items []map[string]any
	_ = json.Unmarshal([]byte(body), &items)
	if len(items) != 1 {
		t.Fatalf("hide=unwatched movies: got %d items, want 1 (%s)", len(items), body)
	}

	// The show watched through an episode must be hidden; the one with no
	// history must stay.
	_, body = doGETFull(t, app, "/v1/users/target/watchlist/shows/rank?hide=unwatched")
	items = nil
	_ = json.Unmarshal([]byte(body), &items)
	if len(items) != 1 {
		t.Fatalf("hide=unwatched shows: got %d items, want 1 (%s)", len(items), body)
	}
	if !strings.Contains(body, "Unwatched Show") || strings.Contains(body, "Parity Show") {
		t.Fatalf("hide=unwatched shows returned the wrong item: %s", body)
	}

	if status, body := doGET(t, app, "/v1/users/target/watchlist/movies/rank?hide=ended"); status != http.StatusBadRequest {
		t.Errorf("hide=ended: status %d, want 400 (%s)", status, body)
	}
}

// TestContractWatchlistInvalidSortIsBadRequest proves an unsupported sort_by or
// sort_how on the contract's sorted watchlist path is the declared 400 naming
// the accepted values, not a plain error the app maps to 500.
func TestContractWatchlistInvalidSortIsBadRequest(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newContractApp("target")

	cases := []struct {
		path string
		want string
	}{
		{"/v1/users/target/watchlist/movies/rank/sideways", "asc"},
		{"/v1/users/target/watchlist/movies/notasort/asc", "rank"},
		{"/v1/users/target/watchlist/shows/rank/sideways", "asc"},
	}
	for _, tc := range cases {
		status, body := doGET(t, app, tc.path)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", tc.path, status, body)
			continue
		}
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: 400 body does not name %q: %s", tc.path, tc.want, body)
		}
	}
}

// TestContractWatchedAggregateKeepsCalendarHiddenItems proves `hidden` narrows
// seasons, not whole items: a movie hidden from the calendar is still in the
// default watched aggregate and its pagination count. The aggregate's movie
// branch is reached directly because the map-shaped /watched/movies read is
// registered at the same path.
func TestContractWatchedAggregateKeepsCalendarHiddenItems(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	now := time.Now().UTC()
	if err := db.Create(&database.Movie{ID: 301, TmdbID: 301, Slug: "hidden-movie", Title: "Hidden Movie", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.UserHistory{UserID: "target", MediaType: "movie", MediaID: 301, WatchedAt: now, Source: "api"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.UserHidden{UserID: "target", Section: "calendar",
		MediaType: "movie", MediaID: 301, HiddenAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	app := newContractApp("target")
	app.Get("/v1/aggregate/users/:id/watched/:type", handleContractWatchedType)

	resp, body := doGETFull(t, app, "/v1/aggregate/users/target/watched/movies")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("watched aggregate: status %d (%s)", resp.StatusCode, body)
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(items) != 1 {
		t.Fatalf("calendar-hidden movie dropped from watched aggregate: got %d items (%s)", len(items), body)
	}
	if got := resp.Header.Get("X-Pagination-Total-Items"); got != "1" {
		t.Errorf("X-Pagination-Total-Items = %q, want 1", got)
	}
}

// TestContractActivitiesFilterValidationRunsBeforeAccountLookup proves filter
// handling runs before the account set is resolved: a malformed value of a
// supported filter is a 400 even for an account with no visible friends (not an
// empty 200), while an unstored documented parameter is ignored there too.
func TestContractActivitiesFilterValidationRunsBeforeAccountLookup(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	seedUser(t, db, "stranger", false, "active")
	app := newContractApp("stranger")

	for _, query := range []string{"ignore_watched=true", "years=99999999"} {
		status, body := doGET(t, app, "/v1/users/target/friends/activities?"+query)
		if status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", query, status, body)
		}
	}
	// countries has no stored column, so it is ignored rather than refused.
	if status, body := doGET(t, app, "/v1/users/target/friends/activities?countries=us"); status != http.StatusOK {
		t.Errorf("countries=us: status %d, want 200 (ignored) (%s)", status, body)
	}
}

// TestContractHistoryRejectsOutOfRangeYear proves the year validation is a 400
// on a real route, not a database error surfacing as 500.
func TestContractHistoryRejectsOutOfRangeYear(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newContractApp("target")

	status, body := doGET(t, app, "/v1/users/target/history/movies?years=99999999")
	if status != http.StatusBadRequest {
		t.Fatalf("years=99999999: status %d, want 400 (%s)", status, body)
	}
	if !strings.Contains(body, "99999999") {
		t.Errorf("400 body does not name the rejected year: %s", body)
	}
}

// TestContractSortRejectionsNameAccepted proves an unsupported {sort} answers
// 400 with the accepted values, not a silent default ordering.
func TestContractSortRejectionsNameAccepted(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newContractApp("target")

	status, body := doGET(t, app, "/v1/users/target/watchlist/movies/notasort")
	if status != http.StatusBadRequest {
		t.Fatalf("unknown sort: status %d, want 400", status)
	}
	if !strings.Contains(body, "added") || !strings.Contains(body, "released") {
		t.Errorf("400 body does not name accepted sorts: %s", body)
	}
}

// TestContractHistoryCatalogueFilter proves the history read applies the
// catalogue filter and reports a filtered total.
func TestContractHistoryCatalogueFilter(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	now := time.Now().UTC()
	if err := db.Create(&database.Movie{ID: 101, TmdbID: 101, Slug: "alpha", Title: "Alpha",
		Genres: database.StringSlice{"Drama"}, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.Movie{ID: 102, TmdbID: 102, Slug: "beta", Title: "Beta",
		Genres: database.StringSlice{"Comedy"}, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{101, 102} {
		if err := db.Create(&database.UserHistory{UserID: "target", MediaType: "movie", MediaID: id,
			WatchedAt: now, Source: "api"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := newContractApp("target")

	resp, body := doGETFull(t, app, "/v1/users/target/history/movies?genres=drama")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history genres: status %d (%s)", resp.StatusCode, body)
	}
	var items []map[string]any
	_ = json.Unmarshal([]byte(body), &items)
	if len(items) != 1 {
		t.Errorf("history genres=drama: got %d items, want 1", len(items))
	}
	if got := resp.Header.Get("X-Pagination-Total-Items"); got != "1" {
		t.Errorf("history X-Pagination-Total-Items = %q, want 1", got)
	}
	// An unstored documented filter is ignored: both history rows come back and
	// the response says which parameter it did not apply.
	resp, body = doGETFull(t, app, "/v1/users/target/history/movies?countries=us")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history countries: status %d, want 200 (ignored) (%s)", resp.StatusCode, body)
	}
	var all []map[string]any
	_ = json.Unmarshal([]byte(body), &all)
	if len(all) != 2 {
		t.Errorf("history countries=us: got %d items, want the 2-item superset", len(all))
	}
	if got := resp.Header.Get("X-Haaku-Ignored-Filters"); got != "countries" {
		t.Errorf("history countries: X-Haaku-Ignored-Filters = %q, want countries", got)
	}
}
