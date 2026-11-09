package watch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestRefreshMovieWithNilMetadata proves a row whose metadata column is NULL
// (which loads as a nil JSONMap) can still be refreshed: the handler allocates
// the map so the fetched providers are actually stored, instead of a 500 or a
// silent no-op.
func TestRefreshMovieWithNilMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.Show{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.SetDBForTest(t, db)

	now := time.Now().UTC()
	movie := database.Movie{TmdbID: 603, Title: "Inception", CreatedAt: now, UpdatedAt: now} // Metadata stays nil
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/movie/603/watch/providers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":603,"results":{"US":{"link":"https://tmdb/justwatch","flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.jpg"}]}}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	tmdbOnce.Do(func() {})
	tmdbClient = client
	tmdbClientErr = nil

	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", "u1")
		return c.Next()
	}
	app.Group("/v1").Post("/movies/:id/refresh/justwatch", stub, handleRefreshMovieProviders)

	req, _ := http.NewRequest(http.MethodPost, "/v1/movies/1/refresh/justwatch", nil)
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for a nil-metadata row, got %d", resp.StatusCode)
	}

	var stored database.Movie
	if err := db.First(&stored, movie.ID).Error; err != nil {
		t.Fatalf("reload movie: %v", err)
	}
	raw, _ := json.Marshal(stored.Metadata)
	if !strings.Contains(string(raw), "Netflix") {
		t.Fatalf("providers not stored into a previously-nil metadata column: %s", raw)
	}
}

func TestRefreshMovieWatchProviders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.Show{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.SetDBForTest(t, db)

	now := time.Now().UTC()
	movie := database.Movie{TmdbID: 603, Title: "Inception", Metadata: database.JSONMap{}, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&movie).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}

	var mu sync.Mutex
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/603/watch/providers", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":603,"results":{"US":{"link":"https://tmdb/justwatch","flatrate":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.jpg"}]}}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("TMDB_API_KEY", "test-token")
	client, err := tmdb.New()
	if err != nil {
		t.Fatalf("tmdb.New: %v", err)
	}
	client.SetBaseURL(srv.URL)
	tmdbOnce.Do(func() {})
	tmdbClient = client
	tmdbClientErr = nil

	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", "u1")
		return c.Next()
	}
	app.Group("/v1").Post("/movies/:id/refresh/justwatch", stub, handleRefreshMovieProviders)

	do := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, "/v1/movies/1/refresh/justwatch", nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		return resp
	}

	resp := do()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	var stored database.Movie
	if err := db.First(&stored, movie.ID).Error; err != nil {
		t.Fatalf("reload movie: %v", err)
	}
	raw, _ := json.Marshal(stored.Metadata)
	if !strings.Contains(string(raw), "Netflix") {
		t.Fatalf("providers not stored in metadata: %s", raw)
	}

	// A refresh is a real re-fetch: the second call hits the provider again.
	_ = do().Body.Close()
	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 2 {
		t.Errorf("provider hits = %d, want 2 (refresh must re-fetch, not 202)", got)
	}
}
