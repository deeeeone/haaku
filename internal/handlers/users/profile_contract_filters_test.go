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
// catalogue filters read, and a watchlist + history row for the target.

func TestContractWatchlistCatalogueFilters(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
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

// TestContractCatalogueFiltersRefuseUnstored proves a declared-but-unstored
// filter is a 400, and that runtimes is refused where the media table has no
// runtime column (shows).
func TestContractCatalogueFiltersRefuseUnstored(t *testing.T) {
	db := contractTestDB(t)
	seedUser(t, db, "target", false, "active")
	app := newContractApp("target")

	for _, path := range []string{
		"/v1/users/target/watchlist/movies/rank?countries=us",
		"/v1/users/target/watchlist/movies/rank?watchnow=free",
		"/v1/users/target/watchlist/shows/rank?runtimes=30-60",
		"/v1/users/target/watchlist/movie,show/rank?runtimes=30-60",
	} {
		if status, body := doGET(t, app, path); status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", path, status, body)
		}
	}
	// A movie watchlist can honour runtimes.
	if status, body := doGET(t, app, "/v1/users/target/watchlist/movies/rank?runtimes=30-60"); status != http.StatusOK {
		t.Errorf("movie runtimes: status %d, want 200 (%s)", status, body)
	}
}

// TestContractWatchlistHideNarrows proves hide/ignore_* are applied rather than
// ignored.
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
	app := newContractApp("target")

	_, body := doGETFull(t, app, "/v1/users/target/watchlist/movies/rank?hide=unwatched")
	var items []map[string]any
	_ = json.Unmarshal([]byte(body), &items)
	if len(items) != 1 {
		t.Fatalf("hide=unwatched: got %d items, want 1 (%s)", len(items), body)
	}

	if status, body := doGET(t, app, "/v1/users/target/watchlist/movies/rank?hide=ended"); status != http.StatusBadRequest {
		t.Errorf("hide=ended: status %d, want 400 (%s)", status, body)
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
	if status, body := doGET(t, app, "/v1/users/target/history/movies?countries=us"); status != http.StatusBadRequest {
		t.Errorf("history countries: status %d, want 400 (%s)", status, body)
	}
}
