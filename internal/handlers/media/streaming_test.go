package media

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

// fixedNow pins the period windows for the test.
var fixedNow = time.Date(2031, 6, 15, 12, 0, 0, 0, time.UTC)

func setupStreamingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.MovieRelease{}, &database.UserHistory{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)

	previous := streamingNow
	streamingNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { streamingNow = previous })
	return db
}

func makeStreamingApp() *fiber.App {
	app := fiber.New()
	movies := app.Group("/v1/movies")
	registerStreamingRoutes(movies)
	return app
}

func dateString(y int, m time.Month, d int) string {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

func TestMovieStreaming(t *testing.T) {
	db := setupStreamingDB(t)
	app := makeStreamingApp()

	movies := []database.Movie{
		{TmdbID: 301, Title: "Two Watchers", Released: &database.Date{Time: time.Date(2031, 6, 13, 0, 0, 0, 0, time.UTC)}},
		{TmdbID: 302, Title: "One Watcher", Released: &database.Date{Time: time.Date(2031, 6, 15, 0, 0, 0, 0, time.UTC)}},
		{TmdbID: 303, Title: "Old Release", Released: &database.Date{Time: time.Date(2031, 6, 5, 0, 0, 0, 0, time.UTC)}},
	}
	for i := range movies {
		if err := db.Create(&movies[i]).Error; err != nil {
			t.Fatalf("seed movie: %v", err)
		}
	}
	releases := []database.MovieRelease{
		{MovieID: movies[0].ID, Country: "us", ReleaseType: "digital", ReleaseDate: database.Date{Time: time.Date(2031, 6, 13, 0, 0, 0, 0, time.UTC)}},
		{MovieID: movies[1].ID, Country: "us", ReleaseType: "digital", ReleaseDate: database.Date{Time: time.Date(2031, 6, 15, 0, 0, 0, 0, time.UTC)}},
		{MovieID: movies[2].ID, Country: "us", ReleaseType: "digital", ReleaseDate: database.Date{Time: time.Date(2031, 6, 5, 0, 0, 0, 0, time.UTC)}},
	}
	if err := db.Create(&releases).Error; err != nil {
		t.Fatalf("seed releases: %v", err)
	}

	watchedAt := time.Date(2031, 6, 15, 9, 0, 0, 0, time.UTC)
	history := []database.UserHistory{
		{UserID: "u1", MediaType: "movie", MediaID: movies[0].ID, WatchedAt: watchedAt},
		{UserID: "u2", MediaType: "movie", MediaID: movies[0].ID, WatchedAt: watchedAt},
		{UserID: "u1", MediaType: "movie", MediaID: movies[1].ID, WatchedAt: watchedAt},
		{UserID: "u1", MediaType: "movie", MediaID: movies[2].ID, WatchedAt: watchedAt},
		{UserID: "u2", MediaType: "movie", MediaID: movies[2].ID, WatchedAt: watchedAt},
		{UserID: "u3", MediaType: "movie", MediaID: movies[2].ID, WatchedAt: watchedAt},
	}
	if err := db.Create(&history).Error; err != nil {
		t.Fatalf("seed history: %v", err)
	}

	decode := func(resp *http.Response) []map[string]any {
		t.Helper()
		defer func() { _ = resp.Body.Close() }()
		var out []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return out
	}
	get := func(path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		return resp
	}

	t.Run("weekly ranks by watchers in the period and excludes older releases", func(t *testing.T) {
		entries := decode(get("/v1/movies/streaming/weekly"))
		if len(entries) != 2 {
			t.Fatalf("expected the two releases inside the last 7 days, got %+v", entries)
		}
		if entries[0]["rank"] != float64(1) || entries[0]["delta"] != nil {
			t.Errorf("rank/delta wrong: %+v", entries[0])
		}
		if entries[0]["movie"].(map[string]any)["title"] != "Two Watchers" {
			t.Errorf("watcher count did not rank first: %+v", entries[0])
		}
		if entries[1]["movie"].(map[string]any)["title"] != "One Watcher" {
			t.Errorf("second rank wrong: %+v", entries[1])
		}
	})

	t.Run("monthly includes the older release and its higher watcher count ranks it first", func(t *testing.T) {
		entries := decode(get("/v1/movies/streaming/monthly"))
		if len(entries) != 3 {
			t.Fatalf("expected 3 releases in the last 30 days, got %+v", entries)
		}
		if entries[0]["movie"].(map[string]any)["title"] != "Old Release" {
			t.Errorf("3 watchers should rank first: %+v", entries[0])
		}
	})

	t.Run("daily is today's window only and starts at midnight", func(t *testing.T) {
		entries := decode(get("/v1/movies/streaming/daily"))
		if len(entries) != 1 {
			t.Fatalf("expected only today's release, got %+v", entries)
		}
		if entries[0]["movie"].(map[string]any)["title"] != "One Watcher" {
			t.Errorf("daily window wrong: %+v", entries[0])
		}
	})

	t.Run("an unknown period is 400", func(t *testing.T) {
		resp := get("/v1/movies/streaming/yearly")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}
