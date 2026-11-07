package calendar

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupMoviesDB opens an in-memory sqlite DB with the tables the movie
// calendars read, and points the database singleton at it via the test-only
// SetDBForTest seam. Each call gets a fresh DB.
func setupMoviesDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	models := []interface{}{
		&database.Movie{}, &database.ExternalID{},
		&database.UserHistory{}, &database.UserWatchlist{}, &database.UserHidden{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// testDate builds a *database.Date for a UTC calendar date.
func testDate(y int, m time.Month, d int) *database.Date {
	return &database.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// stubUser injects a user_id into ctx locals, bypassing Clerk so the "my"
// calendar body runs under test.
func stubUser(userID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
}

// makeMoviesApp registers the movie calendar handlers on a bare app (no real
// auth middleware; the "my" route uses the stub above).
func makeMoviesApp() *fiber.App {
	app := fiber.New()
	cal := app.Group("/v1/calendars")
	cal.Get("/all/movies/:start_date/:days", handleCalendarMovies)
	cal.Get("/my/movies/:start_date/:days", stubUser("user-1"), handleCalendarMovies)
	return app
}

func doGet(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	return resp
}

func decodeMovies(t *testing.T, resp *http.Response) []CalendarMovieEntry {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []CalendarMovieEntry
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode calendar movie body: %v", err)
	}
	return out
}

// seedMovies creates movies spread around a window anchored on 2031-06-01.
func seedMovies(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	inA := database.Movie{TmdbID: 101, Title: "In Window A", Released: testDate(2031, 6, 2)}
	inB := database.Movie{TmdbID: 102, Title: "In Window B", Released: testDate(2031, 6, 4)}
	before := database.Movie{TmdbID: 103, Title: "Before Window", Released: testDate(2031, 5, 20)}
	after := database.Movie{TmdbID: 104, Title: "After Window", Released: testDate(2031, 7, 1)}
	for _, m := range []*database.Movie{&inA, &inB, &before, &after} {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed movie %q: %v", m.Title, err)
		}
	}
	return map[string]int64{
		"inA": inA.ID, "inB": inB.ID, "before": before.ID, "after": after.ID,
	}
}

func TestCalendarMoviesAll(t *testing.T) {
	db := setupMoviesDB(t)
	ids := seedMovies(t, db)
	app := makeMoviesApp()

	t.Run("window returns only movies with a release date inside it, with the Trakt released/movie shape", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/all/movies/2031-06-01/7")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeMovies(t, resp)
		if len(got) != 2 {
			t.Fatalf("expected 2 movies in window, got %d (%+v)", len(got), got)
		}
		// Ordered by released ASC: inA (06-02) then inB (06-04).
		if got[0].Movie.IDs.ID != ids["inA"] || got[1].Movie.IDs.ID != ids["inB"] {
			t.Errorf("unexpected order/ids: %+v", got)
		}
		if got[0].Released != "2031-06-02" || got[1].Released != "2031-06-04" {
			t.Errorf("released dates wrong: %q / %q", got[0].Released, got[1].Released)
		}
		if got[0].Movie.Title != "In Window A" {
			t.Errorf("movie payload not embedded: %+v", got[0].Movie)
		}
	})

	t.Run("empty window returns [] not null", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/all/movies/2031-09-01/7")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeMovies(t, resp)
		if got == nil || len(got) != 0 {
			t.Errorf("expected empty non-nil slice, got %#v", got)
		}
	})

	t.Run("invalid start_date is 400", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/all/movies/not-a-date/7")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestCalendarMoviesMy(t *testing.T) {
	db := setupMoviesDB(t)
	ids := seedMovies(t, db)

	// user-1 watched inA (history) and watchlisted inB, but hid inB.
	now := time.Now().UTC()
	if err := db.Create(&database.UserHistory{
		UserID: "user-1", MediaType: "movie", MediaID: ids["inA"], WatchedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := db.Create(&database.UserWatchlist{
		UserID: "user-1", MediaType: "movie", MediaID: ids["inB"], ListedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed watchlist: %v", err)
	}
	if err := db.Create(&database.UserHidden{
		UserID: "user-1", Section: "calendar", MediaType: "movie", MediaID: ids["inB"], HiddenAt: now,
	}).Error; err != nil {
		t.Fatalf("seed hidden: %v", err)
	}
	// Another user watched inA + inB; that must not leak into user-1's calendar.
	if err := db.Create(&database.UserHistory{
		UserID: "other-user", MediaType: "movie", MediaID: ids["inB"], WatchedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed other history: %v", err)
	}

	app := makeMoviesApp()

	t.Run("my returns only the caller's watched movies, minus hidden ones", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/my/movies/2031-06-01/7")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeMovies(t, resp)
		if len(got) != 1 {
			t.Fatalf("expected 1 movie (inA; inB hidden), got %d (%+v)", len(got), got)
		}
		if got[0].Movie.IDs.ID != ids["inA"] {
			t.Errorf("expected inA (%d), got %d", ids["inA"], got[0].Movie.IDs.ID)
		}
	})

	t.Run("my scopes to the window too", func(t *testing.T) {
		resp := doGet(t, app, "/v1/calendars/my/movies/2031-09-01/7")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got := decodeMovies(t, resp); len(got) != 0 {
			t.Errorf("expected empty window for my calendar, got %+v", got)
		}
	})
}

// TestCalendarMoviesMyRequiresAuth registers the real route table so the
// auth.AuthRequired middleware is exercised: no token means 401, never a leak.
func TestCalendarMoviesMyRequiresAuth(t *testing.T) {
	setupMoviesDB(t)
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))

	resp := doGet(t, app, "/v1/calendars/my/movies/2031-06-01/7")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", resp.StatusCode)
	}
}
