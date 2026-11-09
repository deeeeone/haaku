package media

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// The routes here name a media type, and movie / show / person ids are separate
// sequences that all start at 1. A type-blind resolver let the movies table win
// every collision, so /v1/shows/1 answered 404 "not a show" while show 1 sat in
// the database — the reported failure. These tests pin the route's type as the
// authority.

// scopeApp builds the media routes with the production error handler wired:
// the resolver writes its own 404 envelope and returns response.ErrHandled, and
// without HandleError Fiber's default handler would turn that into a 500.
func scopeApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app
}

func scopeGet(t *testing.T, app *fiber.App, path string) *http.Response {
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

// errorMessage returns the envelope's message for a failed request.
func errorMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Message == "" {
		t.Fatalf("response carried no error envelope")
	}
	return body.Error.Message
}

// titleOf returns the "title" field of a 200 response.
func titleOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body.Title
}

// seedCollidingIDs creates a movie and a show that share row id 1, which is what
// a fresh install produces (each table's sequence starts at 1).
func seedCollidingIDs(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&database.Movie{ID: 1, Title: "The Matrix", TmdbID: 603, Slug: "the-matrix"}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.Show{ID: 1, Title: "Severance", TmdbID: 95396, Slug: "severance"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{ShowID: 1, SeasonNumber: 1}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
}

func TestNumericIDIsScopedToTheRouteType(t *testing.T) {
	db := setupShowsDB(t)
	// /people reads the credits table, which the shared helper does not migrate.
	if err := db.AutoMigrate(&database.Credit{}); err != nil {
		t.Fatalf("migrate credits: %v", err)
	}
	seedCollidingIDs(t, db)
	app := scopeApp()

	t.Run("a show id that a movie shares is reachable on the show route", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/shows/1")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/shows/1 = %d (%s), want 200", resp.StatusCode, errorMessage(t, resp))
		}
		if got := titleOf(t, resp); got != "Severance" {
			t.Errorf("GET /v1/shows/1 returned %q, want Severance", got)
		}
	})

	t.Run("the movie route still answers its own row", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/movies/1")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/movies/1 = %d, want 200", resp.StatusCode)
		}
		if got := titleOf(t, resp); got != "The Matrix" {
			t.Errorf("GET /v1/movies/1 returned %q, want The Matrix", got)
		}
	})

	t.Run("show subroutes resolve the same row", func(t *testing.T) {
		for _, path := range []string{"/v1/shows/1/seasons", "/v1/shows/1/people", "/v1/shows/1/ratings"} {
			resp := scopeGet(t, app, path)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET %s = %d (%s), want 200", path, resp.StatusCode, errorMessage(t, resp))
				continue
			}
			_ = resp.Body.Close()
		}
	})

	t.Run("an id that is only a movie is not a show", func(t *testing.T) {
		if err := db.Create(&database.Movie{ID: 50, Title: "Movie Only"}).Error; err != nil {
			t.Fatalf("seed movie: %v", err)
		}
		resp := scopeGet(t, app, "/v1/shows/50")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /v1/shows/50 = %d, want 404", resp.StatusCode)
		}
		if msg := errorMessage(t, resp); msg != "not a show" {
			t.Errorf("message = %q, want %q", msg, "not a show")
		}
	})

	t.Run("a numeric id that only a show owns is not a movie", func(t *testing.T) {
		if err := db.Create(&database.Show{ID: 60, Title: "Show Only"}).Error; err != nil {
			t.Fatalf("seed show: %v", err)
		}
		// The movie routes must not answer for a show's row id — not with an
		// empty 200 (/people queried credits and returned nothing) and not with
		// a rating summary read from another table.
		for _, path := range []string{"/v1/movies/60", "/v1/movies/60/people", "/v1/movies/60/ratings"} {
			resp := scopeGet(t, app, path)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
				continue
			}
			if msg := errorMessage(t, resp); msg != "not a movie" {
				t.Errorf("GET %s message = %q, want %q", path, msg, "not a movie")
			}
		}
	})

	t.Run("an unknown id says media not found, not a wrong type", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/shows/999999")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /v1/shows/999999 = %d, want 404", resp.StatusCode)
		}
		if msg := errorMessage(t, resp); msg != "media not found" {
			t.Errorf("message = %q, want %q", msg, "media not found")
		}
	})

	t.Run("an unknown person answers an envelope, not an empty array", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/people/999999/movies")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /v1/people/999999/movies = %d, want 404", resp.StatusCode)
		}
		if msg := errorMessage(t, resp); msg != "media not found" {
			t.Errorf("message = %q, want %q", msg, "media not found")
		}
	})
}

func TestExternalIDLookupIsScopedToTheRouteType(t *testing.T) {
	db := setupShowsDB(t)
	seedCollidingIDs(t, db)
	// The same TMDB id and the same slug on both media types: one id, two works.
	if err := db.Create(&database.Show{ID: 2, Title: "Severance (1996)", TmdbID: 603, Slug: "the-matrix"}).Error; err != nil {
		t.Fatalf("seed second show: %v", err)
	}
	ext := []database.ExternalID{
		{MediaType: "movie", MediaID: 1, Source: "tmdb", ExternalID: "603"},
		{MediaType: "show", MediaID: 2, Source: "tmdb", ExternalID: "603"},
		{MediaType: "movie", MediaID: 1, Source: "slug", ExternalID: "the-matrix"},
		{MediaType: "show", MediaID: 2, Source: "slug", ExternalID: "the-matrix"},
	}
	for _, e := range ext {
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("seed external id: %v", err)
		}
	}
	app := scopeApp()

	t.Run("?id_type=tmdb is scoped to the route", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/shows/603?id_type=tmdb")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/shows/603?id_type=tmdb = %d (%s), want 200", resp.StatusCode, errorMessage(t, resp))
		}
		if got := titleOf(t, resp); got != "Severance (1996)" {
			t.Errorf("show lookup returned %q, want the show that owns tmdb 603", got)
		}

		resp2 := scopeGet(t, app, "/v1/movies/603?id_type=tmdb")
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/movies/603?id_type=tmdb = %d, want 200", resp2.StatusCode)
		}
		if got := titleOf(t, resp2); got != "The Matrix" {
			t.Errorf("movie lookup returned %q, want The Matrix", got)
		}
	})

	t.Run("a slug shared by two media types resolves per route", func(t *testing.T) {
		resp := scopeGet(t, app, "/v1/shows/the-matrix")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /v1/shows/the-matrix = %d (%s), want 200", resp.StatusCode, errorMessage(t, resp))
		}
		if got := titleOf(t, resp); got != "Severance (1996)" {
			t.Errorf("slug lookup returned %q, want the show with that slug", got)
		}
	})

	t.Run("an external id that exists only on another type is a wrong-type 404", func(t *testing.T) {
		if err := db.Create(&database.ExternalID{
			MediaType: "show", MediaID: 2, Source: "tmdb", ExternalID: "1111",
		}).Error; err != nil {
			t.Fatalf("seed external id: %v", err)
		}
		resp := scopeGet(t, app, "/v1/movies/1111?id_type=tmdb")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /v1/movies/1111?id_type=tmdb = %d, want 404", resp.StatusCode)
		}
		if msg := errorMessage(t, resp); msg != "not a movie" {
			t.Errorf("message = %q, want %q", msg, "not a movie")
		}
	})
}

func TestResolveIDAsRequiresAType(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	var got string
	app.Get("/v1/x/:id", func(c *fiber.Ctx) error {
		_, _, err := ResolveIDAs(c, c.Params("id"), "")
		if err != nil && !strings.Contains(err.Error(), "requires a media type") {
			got = err.Error()
		}
		return err
	})

	req, err := http.NewRequest(http.MethodGet, "/v1/x/1", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a programming error", resp.StatusCode)
	}
	if got != "" {
		t.Errorf("unexpected error text: %s", got)
	}
}
