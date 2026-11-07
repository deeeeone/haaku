package sync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	perTypeUser  = "user-per-type"
	perTypeOther = "user-per-type-other"
)

// perTypeBody is the envelope the shared list handlers return. Decoding the
// media_type per item is what proves a variant is pinned rather than relying
// on the route table being right.
type perTypeBody struct {
	Items []struct {
		MediaType string `json:"media_type"`
		MediaID   int64  `json:"media_id"`
	} `json:"items"`
	Page     int   `json:"page"`
	Limit    int   `json:"limit"`
	RowCount int64 `json:"row_count"`
}

// setupPerTypeDB mirrors the other sync handler tests: real PostgreSQL when
// DATABASE_URL is set, in-memory sqlite otherwise (CI runs with no services).
// Only the five read tables matter — the per-type variants never write.
func setupPerTypeDB(t *testing.T) *gorm.DB {
	t.Helper()
	models := []interface{}{
		&database.UserHistory{}, &database.UserCollection{}, &database.UserWatchlist{},
		&database.UserRating{}, &database.UserFavorite{},
	}
	var db *gorm.DB
	var err error
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
		if err != nil {
			t.Fatalf("open PostgreSQL: %v", err)
		}
		if err := db.Migrator().DropTable(models...); err != nil {
			t.Fatalf("drop stale PG tables: %v", err)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open in-memory sqlite: %v", err)
		}
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// seedPerTypeRows writes one fixture per (collection, media type) for
// perTypeUser, plus a decoy row of the same media type for another user, and
// returns the ids the caller must see keyed by "<kind>/<media_type>".
func seedPerTypeRows(t *testing.T, db *gorm.DB) map[string][]int64 {
	t.Helper()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// Distinct, increasing timestamps keep the DESC order deterministic.
	ts := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }
	seasonID := int64(11)
	episodeID := int64(201)

	seed := func(model interface{}) {
		t.Helper()
		if err := db.Create(model).Error; err != nil {
			t.Fatalf("seed %T: %v", model, err)
		}
	}

	want := map[string][]int64{
		"history/movie":   {101, 103},
		"history/episode": {201},
	}

	// History: two movies (for pagination) and one episode, caller-scoped.
	seed(&database.UserHistory{UserID: perTypeUser, MediaType: "movie", MediaID: 101, WatchedAt: ts(0), CreatedAt: ts(0)})
	seed(&database.UserHistory{UserID: perTypeUser, MediaType: "movie", MediaID: 103, WatchedAt: ts(20), CreatedAt: ts(20)})
	seed(&database.UserHistory{UserID: perTypeUser, MediaType: "episode", MediaID: 201, SeasonID: &seasonID, EpisodeID: &episodeID, WatchedAt: ts(10), CreatedAt: ts(10)})
	seed(&database.UserHistory{UserID: perTypeOther, MediaType: "movie", MediaID: 102, WatchedAt: ts(5), CreatedAt: ts(5)})
	seed(&database.UserHistory{UserID: perTypeOther, MediaType: "episode", MediaID: 202, WatchedAt: ts(5), CreatedAt: ts(5)})

	// Collection: movie + show.
	seed(&database.UserCollection{UserID: perTypeUser, MediaType: "movie", MediaID: 111, CollectedAt: ts(0), CreatedAt: ts(0)})
	seed(&database.UserCollection{UserID: perTypeUser, MediaType: "show", MediaID: 211, CollectedAt: ts(1), CreatedAt: ts(1)})
	seed(&database.UserCollection{UserID: perTypeOther, MediaType: "movie", MediaID: 112, CollectedAt: ts(0), CreatedAt: ts(0)})
	seed(&database.UserCollection{UserID: perTypeOther, MediaType: "show", MediaID: 212, CollectedAt: ts(0), CreatedAt: ts(0)})
	want["collection/movie"] = []int64{111}
	want["collection/show"] = []int64{211}

	// Watchlist: movie + show.
	seed(&database.UserWatchlist{UserID: perTypeUser, MediaType: "movie", MediaID: 121, Rank: 1, ListedAt: ts(0), CreatedAt: ts(0)})
	seed(&database.UserWatchlist{UserID: perTypeUser, MediaType: "show", MediaID: 221, Rank: 2, ListedAt: ts(1), CreatedAt: ts(1)})
	seed(&database.UserWatchlist{UserID: perTypeOther, MediaType: "movie", MediaID: 122, Rank: 1, ListedAt: ts(0), CreatedAt: ts(0)})
	want["watchlist/movie"] = []int64{121}
	want["watchlist/show"] = []int64{221}

	// Ratings: all four media types exist in user_ratings.
	seed(&database.UserRating{UserID: perTypeUser, MediaType: "movie", MediaID: 131, Rating: 8, RatedAt: ts(0), CreatedAt: ts(0), UpdatedAt: ts(0)})
	seed(&database.UserRating{UserID: perTypeUser, MediaType: "show", MediaID: 231, Rating: 7, RatedAt: ts(1), CreatedAt: ts(1), UpdatedAt: ts(1)})
	seed(&database.UserRating{UserID: perTypeUser, MediaType: "season", MediaID: 241, Rating: 6, RatedAt: ts(2), CreatedAt: ts(2), UpdatedAt: ts(2)})
	seed(&database.UserRating{UserID: perTypeUser, MediaType: "episode", MediaID: 251, Rating: 9, RatedAt: ts(3), CreatedAt: ts(3), UpdatedAt: ts(3)})
	seed(&database.UserRating{UserID: perTypeOther, MediaType: "movie", MediaID: 132, Rating: 5, RatedAt: ts(0), CreatedAt: ts(0), UpdatedAt: ts(0)})
	want["ratings/movie"] = []int64{131}
	want["ratings/show"] = []int64{231}
	want["ratings/season"] = []int64{241}
	want["ratings/episode"] = []int64{251}

	// Favorites: movie + show.
	seed(&database.UserFavorite{UserID: perTypeUser, MediaType: "movie", MediaID: 141, Rank: 1, FavoritedAt: ts(0), CreatedAt: ts(0)})
	seed(&database.UserFavorite{UserID: perTypeUser, MediaType: "show", MediaID: 241, Rank: 2, FavoritedAt: ts(1), CreatedAt: ts(1)})
	seed(&database.UserFavorite{UserID: perTypeOther, MediaType: "show", MediaID: 242, Rank: 1, FavoritedAt: ts(0), CreatedAt: ts(0)})
	want["favorites/movie"] = []int64{141}
	want["favorites/show"] = []int64{241}

	return want
}

// newPerTypeApp mounts the per-type variants exactly as RegisterRoutes does —
// through registerPerTypeReads — with a stub auth middleware pinning user_id.
func newPerTypeApp(userID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	registerPerTypeReads(v1.Group("/sync"))
	// The mixed lists too: the per-type routes share these handlers, and the
	// shared handlers must keep honouring ?type= (and defaulting to all).
	v1.Get("/sync/history", handleGetHistory)
	v1.Get("/sync/collection", handleGetCollection)
	v1.Get("/sync/watchlist", handleGetWatchlist)
	v1.Get("/sync/ratings", handleGetRatings)
	v1.Get("/sync/favorites", handleGetFavorites)
	return app
}

// TestPerTypeReadsAreDeclaredOnlyForBackedMediaTypes locks the honesty rule:
// a declared variant must be a (collection, media_type) pair the backing table
// stores, and the pairs we cannot serve must be absent rather than registered
// as always-empty stubs.
func TestPerTypeReadsAreDeclaredOnlyForBackedMediaTypes(t *testing.T) {
	backed := map[string]bool{
		"history/movie": true, "history/episode": true,
		"collection/movie": true, "collection/show": true,
		"watchlist/movie": true, "watchlist/show": true,
		"ratings/movie": true, "ratings/show": true, "ratings/season": true, "ratings/episode": true,
		"favorites/movie": true, "favorites/show": true,
	}
	declared := map[string]bool{}
	for _, r := range perTypeReads {
		key := r.kind + "/" + r.mediaType
		declared[key] = true
		if !backed[key] {
			t.Errorf("variant %s/%s is declared but no table stores that media_type for that collection", r.kind, r.segment)
		}
		if r.handler == nil {
			t.Errorf("variant %s/%s has no handler", r.kind, r.segment)
		}
	}
	for key := range backed {
		if !declared[key] {
			t.Errorf("backed variant %s is not declared", key)
		}
	}
	for _, absent := range []string{
		"history/show", "history/season", // user_history stores movie|episode only
		"watchlist/season", "watchlist/episode", // user_watchlist stores movie|show only
		"collection/season", "collection/episode", // user_collection stores movie|show only
	} {
		if declared[absent] {
			t.Errorf("variant %s must not be registered: no rows of that media type exist to serve", absent)
		}
	}
}

// TestPerTypeReadsReturnOnlyPinnedTypeForCaller is the main contract test: each
// variant returns exactly the caller's rows of its own media type — never
// another type, never another user's rows, and never an empty set (a route
// that can only ever return nothing would be a stub).
func TestPerTypeReadsReturnOnlyPinnedTypeForCaller(t *testing.T) {
	db := setupPerTypeDB(t)
	want := seedPerTypeRows(t, db)
	app := newPerTypeApp(perTypeUser)

	for _, r := range perTypeReads {
		t.Run(r.kind+"/"+r.segment, func(t *testing.T) {
			path := "/v1/sync/" + r.kind + "/" + r.segment
			status, raw := doGET(t, app, path)
			if status != http.StatusOK {
				t.Fatalf("GET %s: expected 200, got %d (%s)", path, status, raw)
			}
			var body perTypeBody
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode %s: %v (%s)", path, err, raw)
			}

			got := make([]int64, 0, len(body.Items))
			for _, item := range body.Items {
				if item.MediaType != r.mediaType {
					t.Errorf("%s returned a %q row — the route is not pinned", path, item.MediaType)
				}
				got = append(got, item.MediaID)
			}
			if len(got) == 0 {
				t.Fatalf("%s returned no rows for a seeded %s fixture", path, r.mediaType)
			}

			expected := append([]int64(nil), want[r.kind+"/"+r.mediaType]...)
			sortIDs(got)
			sortIDs(expected)
			if fmt.Sprint(got) != fmt.Sprint(expected) {
				t.Errorf("%s returned media ids %v, want %v", path, got, expected)
			}
			if body.RowCount != int64(len(expected)) {
				t.Errorf("%s row_count = %d, want %d", path, body.RowCount, len(expected))
			}
			if body.Page != 1 || body.Limit != 25 {
				t.Errorf("%s page/limit = %d/%d, want 1/25", path, body.Page, body.Limit)
			}
		})
	}
}

func sortIDs(ids []int64) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

// TestPerTypeReadsEmptyIsArrayNotNull: a caller with nothing in a collection
// gets 200 and "items":[] — never null, never an error.
func TestPerTypeReadsEmptyIsArrayNotNull(t *testing.T) {
	db := setupPerTypeDB(t)
	seedPerTypeRows(t, db)
	app := newPerTypeApp("user-with-nothing")

	for _, r := range perTypeReads {
		t.Run(r.kind+"/"+r.segment, func(t *testing.T) {
			status, raw := doGET(t, app, "/v1/sync/"+r.kind+"/"+r.segment)
			if status != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", status, raw)
			}
			// Decode into raw messages: a decoded []byte cannot tell [] from
			// null, and null is exactly the shape Trakt clients crash on.
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(raw, &envelope); err != nil {
				t.Fatalf("decode envelope: %v (%s)", err, raw)
			}
			if got := string(envelope["items"]); got != "[]" {
				t.Errorf("items = %s, want [] (%s)", got, raw)
			}
			if got := string(envelope["row_count"]); got != "0" {
				t.Errorf("row_count = %s, want 0", got)
			}
		})
	}
}

// TestPerTypeReadsPagination: a per-type route paginates like the mixed list —
// page 1 is capped at limit, page 2 carries the remainder, row_count is the
// unpaginated total of that media type only.
func TestPerTypeReadsPagination(t *testing.T) {
	db := setupPerTypeDB(t)
	seedPerTypeRows(t, db)
	app := newPerTypeApp(perTypeUser)

	status, raw := doGET(t, app, "/v1/sync/history/movies?page=1&limit=1")
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (%s)", status, raw)
	}
	var first perTypeBody
	if err := json.Unmarshal(raw, &first); err != nil {
		t.Fatalf("decode page 1: %v (%s)", err, raw)
	}
	if len(first.Items) != 1 || first.RowCount != 2 || first.Page != 1 || first.Limit != 1 {
		t.Fatalf("page 1 = %d items, row_count %d, page %d, limit %d; want 1/2/1/1",
			len(first.Items), first.RowCount, first.Page, first.Limit)
	}
	if got := first.Items[0].MediaID; got != 103 {
		t.Errorf("page 1 newest movie = %d, want 103 (watched_at DESC)", got)
	}

	status, raw = doGET(t, app, "/v1/sync/history/movies?page=2&limit=1")
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (%s)", status, raw)
	}
	var second perTypeBody
	if err := json.Unmarshal(raw, &second); err != nil {
		t.Fatalf("decode page 2: %v (%s)", err, raw)
	}
	if len(second.Items) != 1 || second.Items[0].MediaID != 101 || second.RowCount != 2 {
		t.Fatalf("page 2 = %+v, want one item (101) and row_count 2", second)
	}
}

// TestPerTypeReadsPathWinsOverQueryType: ?type= cannot widen a per-type route,
// and the mixed lists still honour ?type (the shared handler must not have
// stopped reading it).
func TestPerTypeReadsPathWinsOverQueryType(t *testing.T) {
	db := setupPerTypeDB(t)
	seedPerTypeRows(t, db)
	app := newPerTypeApp(perTypeUser)

	status, raw := doGET(t, app, "/v1/sync/history/movies?type=episode")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", status, raw)
	}
	if strings.Contains(string(raw), `"media_type":"episode"`) {
		t.Errorf("?type=episode leaked an episode row into /sync/history/movies: %s", raw)
	}

	// The mixed list is still filtered by ?type=.
	status, raw = doGET(t, app, "/v1/sync/history?type=episode")
	if status != http.StatusOK {
		t.Fatalf("mixed list: expected 200, got %d (%s)", status, raw)
	}
	var body perTypeBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode mixed list: %v (%s)", err, raw)
	}
	if len(body.Items) != 1 || body.Items[0].MediaID != 201 {
		t.Errorf("mixed ?type=episode = %+v, want exactly the episode row 201", body.Items)
	}
}
