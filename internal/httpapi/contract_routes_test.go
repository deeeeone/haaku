package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestContractRoutesSmoke exercises every contract-shaped route that can run
// without an external provider, from seeded sqlite rows, and asserts a 2xx and
// the contract's top-level shape (array vs object).
func TestContractRoutesSmoke(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:contract_smoke?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&database.User{},
	); err != nil {
		t.Fatalf("migrate user: %v", err)
	}
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	if err := db.AutoMigrate(database.SyncModels()...); err != nil {
		t.Fatalf("migrate sync: %v", err)
	}
	if err := db.AutoMigrate(database.CommentModels()...); err != nil {
		t.Fatalf("migrate comments: %v", err)
	}
	if err := db.AutoMigrate(database.ListModels()...); err != nil {
		t.Fatalf("migrate lists: %v", err)
	}
	database.SetDBForTest(t, db)

	seedContractFixtures(t, db)

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "user_test")
		return c.Next()
	})
	RegisterRoutes(app)

	arrayRoutes := []string{
		"/v1/calendars/releases/hot/new/2000-01-01/30",
		"/v1/calendars/releases/hot/premieres/2000-01-01/30",
		"/v1/calendars/releases/hot/finales/2000-01-01/30",
		"/v1/comments/trending/all/shows",
		"/v1/lists/popular/movies",
		"/v1/lists/trending/movies",
		"/v1/movies/anticipated",
		"/v1/shows/anticipated",
		"/v1/search/imdb/tt0133093",
		"/v1/shows/1/seasons/1/episodes/1/lists/movies/popular",
		"/v1/sync/collection/episodes",
		"/v1/sync/favorites/movies/rank/asc",
		"/v1/sync/ratings/movies/8",
		"/v1/sync/watchlist/movies/rank/asc",
		"/v1/sync/history/movies/1",
		"/v1/users/user_test/watchlist/movies/rank/asc",
	}
	for _, path := range arrayRoutes {
		raw := doContractRequest(t, app, path)
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			t.Errorf("%s: expected a JSON array, got %s (%v)", path, string(raw), err)
		}
	}

	objectRoutes := []string{
		"/v1/movies/1/watchnow/us",
		"/v1/shows/1/watchnow/us",
		"/v1/shows/1/seasons/1/ratings",
		"/v1/shows/1/seasons/1/stats",
		"/v1/users/settings",
	}
	for _, path := range objectRoutes {
		raw := doContractRequest(t, app, path)
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Errorf("%s: expected a JSON object, got %s (%v)", path, string(raw), err)
		}
	}
}

func doContractRequest(t *testing.T, app *fiber.App, path string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s: request: %v", path, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t.Fatalf("%s: status %d", path, res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: read body: %v", path, err)
	}
	return raw
}

func seedContractFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()

	mustCreate := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed %T: %v", v, err)
		}
	}
	mustCreate(&database.User{ID: "user_test", Username: "tester", Email: "t@example.com", CreatedAt: now})
	mustCreate(&database.Movie{ID: 1, TmdbID: 603, ImdbID: "tt0133093", Slug: "the-matrix", Title: "The Matrix", Released: &database.Date{Time: time.Date(1999, 3, 31, 0, 0, 0, 0, time.UTC)}, RatingAvg: 8.2, RatingCount: 10, Metadata: database.JSONMap{}})
	mustCreate(&database.Show{ID: 1, TmdbID: 1399, Slug: "the-show", Title: "The Show", FirstAired: &database.Date{Time: time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)}, RatingAvg: 7.5, RatingCount: 5, Metadata: database.JSONMap{}})
	mustCreate(&database.Season{ID: 1, ShowID: 1, SeasonNumber: 1, EpisodeCount: 2})
	mustCreate(&database.Episode{ID: 1, SeasonID: 1, ShowID: 1, EpisodeNumber: 1, Title: "Pilot", AiredAt: &database.Date{Time: now}})
	mustCreate(&database.Episode{ID: 2, SeasonID: 1, ShowID: 1, EpisodeNumber: 2, Title: "Second", AiredAt: &database.Date{Time: now}})
	mustCreate(&database.ExternalID{MediaType: "movie", MediaID: 1, Source: "imdb", ExternalID: "tt0133093"})

	mustCreate(&database.UserWatchlist{UserID: "user_test", MediaType: "movie", MediaID: 1, Rank: 1, ListedAt: now})
	mustCreate(&database.UserWatchlist{UserID: "user_test", MediaType: "show", MediaID: 1, Rank: 2, ListedAt: now})
	mustCreate(&database.UserFavorite{UserID: "user_test", MediaType: "movie", MediaID: 1, Rank: 1, FavoritedAt: now})
	mustCreate(&database.UserRating{UserID: "user_test", MediaType: "movie", MediaID: 1, Rating: 8, RatedAt: now})
	mustCreate(&database.UserCollection{UserID: "user_test", MediaType: "movie", MediaID: 1, CollectedAt: now})
	mustCreate(&database.UserHistory{UserID: "user_test", MediaType: "movie", MediaID: 1, WatchedAt: now, Source: "api"})

	notShown := false
	mustCreate(&database.Comment{ID: 1, UserID: "user_test", MediaType: "show", MediaID: ptrInt64(1), CommentType: "shout", Body: "hi", CreatedAt: now, UpdatedAt: now, Spoiler: notShown})
	mustCreate(&database.List{ID: 1, UserID: "user_test", Name: "Faves", Slug: "faves", Privacy: "public", SortBy: "rank", SortHow: "asc", AllowComments: true, CreatedAt: now, UpdatedAt: now})
	mustCreate(&database.ListItem{ListID: 1, MediaType: "movie", MediaID: 1, Rank: 1, AddedBy: "user_test", AddedAt: now})

	// Seed the cached TMDB watch providers so the watch-now routes answer from
	// stored data without a provider call.
	watchCache := database.JSONMap{
		"watch_providers": map[string]any{
			"_synced_at": now.Format(time.RFC3339),
			"results": map[string]any{
				"US": map[string]any{
					"link":     "https://example.test/watch",
					"flatrate": []map[string]any{{"provider_id": 8, "provider_name": "Netflix", "logo_path": "/n.png"}},
					"rent":     []map[string]any{},
					"buy":      []map[string]any{},
				},
			},
		},
	}
	db.Model(&database.Movie{}).Where("id = ?", 1).Update("metadata", watchCache)
	db.Model(&database.Show{}).Where("id = ?", 1).Update("metadata", watchCache)
}

func ptrInt64(v int64) *int64 { return &v }
