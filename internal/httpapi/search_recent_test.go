package httpapi

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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const search70User = "user-search-test"

// newSearch70DB builds a sqlite database carrying the tables the recent-search
// and exact-search handlers touch, and installs it as the process-wide DB.
func newSearch70DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Movie{}, &database.Show{}, &database.Person{},
		&database.ExternalID{}, &database.List{}, &database.RecentSearch{},
	); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// newSearch70App mounts the real route table with a stub identity, so the tests
// exercise the registered paths rather than a hand-wired subset.
func newSearch70App(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	db := newSearch70DB(t)
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", search70User)
		return c.Next()
	})
	RegisterRoutes(app)
	return app, db
}

func seedSearch70(t *testing.T, db *gorm.DB) {
	t.Helper()
	must := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed %T: %v", v, err)
		}
	}
	must(&database.Movie{ID: 1, Title: "Batman Begins", Slug: "batman-begins", TmdbID: 272})
	must(&database.Movie{ID: 2, Title: "Batman Returns", Slug: "batman-returns", TmdbID: 364})
	must(&database.Show{ID: 1, Title: "Batman: The Animated Series", Slug: "batman-tas", TmdbID: 2004})
	must(&database.Person{ID: 1, Name: "Christian Bale", TmdbID: 3894})
	must(&database.List{ID: 1, UserID: search70User, Name: "Faves", Slug: "faves", Privacy: "public"})
	must(&database.ExternalID{MediaType: "movie", MediaID: 1, Source: "tmdb", ExternalID: "272"})
}

func doSearch70(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	_ = resp.Body.Close()
	return resp, raw
}

func seedRecent70(t *testing.T, db *gorm.DB, rows ...database.RecentSearch) {
	t.Helper()
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed recent search: %v", err)
		}
	}
}

func decodeSearch70Array(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode search response: %v (%s)", err, raw)
	}
	return out
}

func TestSearch70PostRecentIsCreatedAndIncrements(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	payload := `{"query":"  Batman  ","id":1,"type":"movies"}`
	resp, raw := doSearch70(t, app, http.MethodPost, "/v1/search/recent", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST recent = %d, want 201 (%s)", resp.StatusCode, raw)
	}
	if len(raw) != 0 {
		t.Fatalf("POST recent body = %q, want an empty body", raw)
	}

	var got database.RecentSearch
	if err := db.Where("media_type = ? AND media_id = ? AND query = ?", "movie", 1, "batman").First(&got).Error; err != nil {
		t.Fatalf("stored recent search missing: %v", err)
	}
	if got.Count != 1 {
		t.Fatalf("count = %d, want 1", got.Count)
	}

	resp, raw = doSearch70(t, app, http.MethodPost, "/v1/search/recent", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second POST recent = %d, want 201 (%s)", resp.StatusCode, raw)
	}
	var again database.RecentSearch
	if err := db.Where("media_type = ? AND media_id = ? AND query = ?", "movie", 1, "batman").First(&again).Error; err != nil {
		t.Fatalf("stored recent search missing after repeat: %v", err)
	}
	if again.Count != 2 {
		t.Fatalf("count after repeat = %d, want 2", again.Count)
	}
}

func TestSearch70PostRecentRequiresAuth(t *testing.T) {
	app := fiber.New()
	app.Post("/v1/search/recent", auth.AuthRequired(), handlePostRecentSearch)

	resp, raw := doSearch70(t, app, http.MethodPost, "/v1/search/recent", `{"query":"batman","id":1,"type":"movies"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST recent without auth = %d, want 401 (%s)", resp.StatusCode, raw)
	}
}

func TestSearch70PostRecentValidation(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"bad type", `{"query":"batman","id":1,"type":"episode"}`, http.StatusBadRequest},
		{"missing query", `{"id":1,"type":"movies"}`, http.StatusBadRequest},
		{"missing id", `{"query":"batman","type":"movies"}`, http.StatusBadRequest},
		{"unknown item", `{"query":"ghost","id":999,"type":"movies"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := doSearch70(t, app, http.MethodPost, "/v1/search/recent", tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("POST %s = %d, want %d (%s)", tc.body, resp.StatusCode, tc.want, raw)
			}
		})
	}
}

func TestSearch70TrendingOrdersBySummedCount(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)
	now := time.Now().UTC()
	seedRecent70(t, db,
		database.RecentSearch{MediaType: "movie", MediaID: 1, Query: "batman", Count: 2, LastSearchedAt: now.Add(-2 * time.Hour)},
		database.RecentSearch{MediaType: "movie", MediaID: 1, Query: "dark knight", Count: 1, LastSearchedAt: now.Add(-time.Hour)},
		database.RecentSearch{MediaType: "movie", MediaID: 2, Query: "batman returns", Count: 2, LastSearchedAt: now},
	)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/movies", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET trending = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	items := decodeSearch70Array(t, raw)
	if len(items) != 2 {
		t.Fatalf("trending returned %d items, want 2 (%s)", len(items), raw)
	}
	if id := items[0]["id"].(float64); id != 1 {
		t.Fatalf("first item id = %v, want 1 (the summed count wins)", id)
	}
	if count := items[0]["count"].(float64); count != 3 {
		t.Fatalf("first item count = %v, want 3", count)
	}
	if count := items[1]["count"].(float64); count != 2 {
		t.Fatalf("second item count = %v, want 2", count)
	}
	if typ := items[0]["type"]; typ != "movie" {
		t.Fatalf("type = %v, want movie", typ)
	}
	movie, ok := items[0]["movie"].(map[string]any)
	if !ok {
		t.Fatalf("item has no movie object: %v", items[0])
	}
	if movie["title"] != "Batman Begins" {
		t.Fatalf("movie.title = %v, want Batman Begins", movie["title"])
	}
	ids, _ := movie["ids"].(map[string]any)
	if ids["trakt"] != float64(1) {
		t.Fatalf("movie.ids.trakt = %v, want 1", ids["trakt"])
	}
}

func TestSearch70TrendingFiltersByQuery(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)
	now := time.Now().UTC()
	seedRecent70(t, db,
		database.RecentSearch{MediaType: "movie", MediaID: 1, Query: "batman", Count: 2, LastSearchedAt: now},
		database.RecentSearch{MediaType: "movie", MediaID: 1, Query: "dark knight", Count: 1, LastSearchedAt: now},
		database.RecentSearch{MediaType: "movie", MediaID: 2, Query: "batman returns", Count: 2, LastSearchedAt: now},
	)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/movies?query=DARK", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET trending with filter = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	items := decodeSearch70Array(t, raw)
	if len(items) != 1 {
		t.Fatalf("filtered trending returned %d items, want 1 (%s)", len(items), raw)
	}
	if id := items[0]["id"].(float64); id != 1 {
		t.Fatalf("filtered item id = %v, want 1", id)
	}
}

func TestSearch70TrendingPagination(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)
	now := time.Now().UTC()
	seedRecent70(t, db,
		database.RecentSearch{MediaType: "movie", MediaID: 1, Query: "batman", Count: 2, LastSearchedAt: now},
		database.RecentSearch{MediaType: "movie", MediaID: 2, Query: "batman returns", Count: 1, LastSearchedAt: now},
	)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/movies?limit=1&page=1", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET trending page = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("X-Pagination-Total-Items"); got != "2" {
		t.Fatalf("X-Pagination-Total-Items = %q, want 2", got)
	}
	if got := resp.Header.Get("X-Pagination-Total-Pages"); got != "2" {
		t.Fatalf("X-Pagination-Total-Pages = %q, want 2", got)
	}
	if got := resp.Header.Get("X-Pagination-Items-Count"); got != "1" {
		t.Fatalf("X-Pagination-Items-Count = %q, want 1", got)
	}
	if link := resp.Header.Get("Link"); !strings.Contains(link, "page=2") {
		t.Fatalf("Link = %q, want a next-page link", link)
	}
	if items := decodeSearch70Array(t, raw); len(items) != 1 {
		t.Fatalf("page returned %d items, want 1", len(items))
	}
}

func TestSearch70TrendingRejectsLists(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/lists", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET trending lists = %d, want 400 (%s)", resp.StatusCode, raw)
	}
}

func TestSearch70TrendingSkipsDeletedItem(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)
	seedRecent70(t, db,
		database.RecentSearch{MediaType: "movie", MediaID: 999, Query: "ghost", Count: 9, LastSearchedAt: time.Now().UTC()},
	)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/movies", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET trending = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	if items := decodeSearch70Array(t, raw); len(items) != 0 {
		t.Fatalf("trending returned %d items for a deleted row, want 0 (%s)", len(items), raw)
	}
}

func TestSearch70TrendingPersonShape(t *testing.T) {
	app, db := newSearch70App(t)
	seedSearch70(t, db)
	seedRecent70(t, db,
		database.RecentSearch{MediaType: "person", MediaID: 1, Query: "christian bale", Count: 4, LastSearchedAt: time.Now().UTC()},
	)

	resp, raw := doSearch70(t, app, http.MethodGet, "/v1/search/recent_by_id/global/people", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET trending people = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	items := decodeSearch70Array(t, raw)
	if len(items) != 1 {
		t.Fatalf("trending people returned %d items, want 1 (%s)", len(items), raw)
	}
	if typ := items[0]["type"]; typ != "person" {
		t.Fatalf("type = %v, want person", typ)
	}
	person, ok := items[0]["person"].(map[string]any)
	if !ok {
		t.Fatalf("item has no person object: %v", items[0])
	}
	if person["name"] != "Christian Bale" {
		t.Fatalf("person.name = %v, want Christian Bale", person["name"])
	}
	ids, _ := person["ids"].(map[string]any)
	if ids["trakt"] != float64(1) {
		t.Fatalf("person.ids.trakt = %v, want 1", ids["trakt"])
	}
	if slug, _ := ids["slug"].(string); slug == "" {
		t.Fatalf("person.ids.slug = %v, want a non-empty slug", ids["slug"])
	}
}
