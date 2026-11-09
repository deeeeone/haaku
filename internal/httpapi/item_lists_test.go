package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newItemListsDB builds the tables the "lists containing an item" routes read:
// media rows to resolve the item, and list/list_items/likes/comments rows to
// rank the containing lists.
func newItemListsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sqlite handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&database.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	if err := db.AutoMigrate(database.ListModels()...); err != nil {
		t.Fatalf("migrate lists: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func newItemListsApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	db := newItemListsDB(t)
	seedItemLists(t, db)
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "viewer")
		return c.Next()
	})
	RegisterRoutes(app)
	return app, db
}

func seedItemLists(t *testing.T, db *gorm.DB) {
	t.Helper()
	must := func(v any) {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed %T: %v", v, err)
		}
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for _, u := range []database.User{
		{ID: "u1", Username: "one", Email: "one@example.test", CreatedAt: base},
		{ID: "u2", Username: "two", Email: "two@example.test", CreatedAt: base},
		{ID: "u3", Username: "three", Email: "three@example.test", CreatedAt: base},
	} {
		must(&u)
	}

	must(&database.Movie{ID: 1, Title: "The Matrix", Slug: "the-matrix", TmdbID: 603})
	must(&database.Show{ID: 1, Title: "The Show", Slug: "the-show", TmdbID: 1399})
	must(&database.Person{ID: 1, Name: "Keanu Reeves", TmdbID: 6384})
	must(&database.Season{ID: 1, ShowID: 1, SeasonNumber: 1, EpisodeCount: 2})
	must(&database.Season{ID: 2, ShowID: 1, SeasonNumber: 2, EpisodeCount: 2})

	// L1 public: oldest created, most recently updated, 3 items.
	must(&database.List{ID: 1, UserID: "u1", Name: "Three things", Slug: "three-things", Privacy: "public",
		LikeCount: 1, CommentCount: 0, CreatedAt: base, UpdatedAt: base.Add(3 * time.Hour)})
	// L2 private: wins every ranking but must never be served.
	must(&database.List{ID: 2, UserID: "u2", Name: "Secret", Slug: "secret", Privacy: "private",
		LikeCount: 100, CommentCount: 50, CreatedAt: base.Add(5 * time.Hour), UpdatedAt: base.Add(5 * time.Hour)})
	// L3 public: more likes/comments and newer than L1, but only one item.
	must(&database.List{ID: 3, UserID: "u3", Name: "Popular", Slug: "popular", Privacy: "public",
		LikeCount: 10, CommentCount: 2, CreatedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)})
	// L4 public but contains a different item (show only) and outranks L3.
	must(&database.List{ID: 4, UserID: "u1", Name: "Shows only", Slug: "shows-only", Privacy: "public",
		LikeCount: 200, CommentCount: 0, CreatedAt: base.Add(2 * time.Hour), UpdatedAt: base.Add(2 * time.Hour)})
	// L5 public: another list containing season 1.
	must(&database.List{ID: 5, UserID: "u1", Name: "Season one", Slug: "season-one", Privacy: "public",
		LikeCount: 2, CommentCount: 0, CreatedAt: base, UpdatedAt: base})
	// L6 public: contains the person.
	must(&database.List{ID: 6, UserID: "u3", Name: "Keanu", Slug: "keanu", Privacy: "public",
		LikeCount: 3, CommentCount: 0, CreatedAt: base, UpdatedAt: base})

	items := []database.ListItem{
		{ListID: 1, MediaType: "movie", MediaID: 1, Rank: 1, AddedBy: "u1", AddedAt: base},
		{ListID: 1, MediaType: "show", MediaID: 1, Rank: 2, AddedBy: "u1", AddedAt: base},
		{ListID: 1, MediaType: "season", MediaID: 1, Rank: 3, AddedBy: "u1", AddedAt: base},
		{ListID: 2, MediaType: "movie", MediaID: 1, Rank: 1, AddedBy: "u2", AddedAt: base},
		{ListID: 2, MediaType: "show", MediaID: 1, Rank: 2, AddedBy: "u2", AddedAt: base},
		{ListID: 2, MediaType: "season", MediaID: 1, Rank: 3, AddedBy: "u2", AddedAt: base},
		{ListID: 2, MediaType: "episode", MediaID: 1, Rank: 4, AddedBy: "u2", AddedAt: base},
		{ListID: 3, MediaType: "movie", MediaID: 1, Rank: 1, AddedBy: "u3", AddedAt: base},
		{ListID: 4, MediaType: "show", MediaID: 1, Rank: 1, AddedBy: "u1", AddedAt: base},
		{ListID: 5, MediaType: "season", MediaID: 1, Rank: 1, AddedBy: "u1", AddedAt: base},
		{ListID: 6, MediaType: "person", MediaID: 1, Rank: 1, AddedBy: "u3", AddedAt: base},
	}
	for i := range items {
		must(&items[i])
	}
}

func itemListGET(t *testing.T, app *fiber.App, path string) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp, raw
}

func decodeItemListArray(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode list array: %v (%s)", err, raw)
	}
	return out
}

func itemListIDs(t *testing.T, items []map[string]any) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		idsMap, ok := item["ids"].(map[string]any)
		if !ok {
			t.Fatalf("list has no ids object: %v", item)
		}
		id, ok := idsMap["trakt"].(float64)
		if !ok {
			t.Fatalf("list ids.trakt = %v, want a number", idsMap["trakt"])
		}
		ids = append(ids, int64(id))
	}
	return ids
}

func TestItemListsArePublicAndSorted(t *testing.T) {
	app, _ := newItemListsApp(t)

	cases := []struct {
		sort string
		want []int64
	}{
		{"likes", []int64{3, 1}},
		{"comments", []int64{3, 1}},
		{"items", []int64{1, 3}},
		{"added", []int64{3, 1}},
		{"updated", []int64{1, 3}},
		{"popular", []int64{3, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.sort, func(t *testing.T) {
			resp, raw := itemListGET(t, app, "/v1/movies/1/lists/personal/"+tc.sort)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, raw)
			}
			items := decodeItemListArray(t, raw)
			if got := itemListIDs(t, items); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ids = %v, want %v (private list and lists without the movie must be absent)", got, tc.want)
			}
		})
	}
}

func TestItemListShapeUsesContractList(t *testing.T) {
	app, _ := newItemListsApp(t)

	resp, raw := itemListGET(t, app, "/v1/movies/1/lists/personal/items")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, raw)
	}
	items := decodeItemListArray(t, raw)
	if len(items) != 2 {
		t.Fatalf("got %d lists, want 2", len(items))
	}
	first := items[0]
	if first["name"] != "Three things" {
		t.Fatalf("name = %v, want Three things", first["name"])
	}
	if count, _ := first["item_count"].(float64); count != 3 {
		t.Fatalf("item_count = %v, want 3", first["item_count"])
	}
	if likes, _ := first["likes"].(float64); likes != 1 {
		t.Fatalf("likes = %v, want 1", first["likes"])
	}
	user, ok := first["user"].(map[string]any)
	if !ok || user["username"] != "one" {
		t.Fatalf("user = %v, want owner username one", first["user"])
	}
}

func TestItemListsComputedEmptyKinds(t *testing.T) {
	app, _ := newItemListsApp(t)

	for _, typ := range []string{"official", "watchlist", "favorites"} {
		t.Run(typ, func(t *testing.T) {
			resp, raw := itemListGET(t, app, "/v1/movies/1/lists/"+typ+"/popular")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, raw)
			}
			if items := decodeItemListArray(t, raw); len(items) != 0 {
				t.Fatalf("got %d items, want an empty array (%s)", len(items), raw)
			}
		})
	}
}

func TestItemListsRejectsBadTypeAndSort(t *testing.T) {
	app, _ := newItemListsApp(t)

	for _, path := range []string{
		"/v1/movies/1/lists/blocked/popular",
		"/v1/movies/1/lists/personal/unknown",
	} {
		resp, raw := itemListGET(t, app, path)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400 (%s)", path, resp.StatusCode, raw)
		}
	}
}

func TestItemListsUnknownAndNonNumericIDs(t *testing.T) {
	app, _ := newItemListsApp(t)

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/v1/movies/999/lists/personal/popular", http.StatusNotFound},
		{"/v1/movies/abc/lists/personal/popular", http.StatusBadRequest},
		{"/v1/people/999/lists/personal/popular", http.StatusNotFound},
		{"/v1/shows/1/seasons/99/lists/personal/popular", http.StatusNotFound},
	} {
		resp, raw := itemListGET(t, app, tc.path)
		if resp.StatusCode != tc.want {
			t.Fatalf("%s status = %d, want %d (%s)", tc.path, resp.StatusCode, tc.want, raw)
		}
	}
}

func TestItemListsPaginationHeaders(t *testing.T) {
	app, _ := newItemListsApp(t)

	resp, raw := itemListGET(t, app, "/v1/movies/1/lists/personal/likes?limit=1&page=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.StatusCode, raw)
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
}

func TestPlainItemListsDefaultsToPersonalPopular(t *testing.T) {
	app, _ := newItemListsApp(t)

	_, plain := itemListGET(t, app, "/v1/movies/1/lists")
	_, explicit := itemListGET(t, app, "/v1/movies/1/lists/personal/popular")

	var plainArr, explicitArr []map[string]any
	if err := json.Unmarshal(plain, &plainArr); err != nil {
		t.Fatalf("decode plain lists: %v (%s)", err, plain)
	}
	if err := json.Unmarshal(explicit, &explicitArr); err != nil {
		t.Fatalf("decode explicit lists: %v (%s)", err, explicit)
	}
	if !reflect.DeepEqual(plainArr, explicitArr) {
		t.Fatalf("plain /lists = %s, want it to equal /lists/personal/popular (%s)", plain, explicit)
	}
}

func TestItemListsSeasonAndPersonRoutes(t *testing.T) {
	app, _ := newItemListsApp(t)

	seasonResp, seasonRaw := itemListGET(t, app, "/v1/shows/1/seasons/1/lists/personal/likes")
	if seasonResp.StatusCode != http.StatusOK {
		t.Fatalf("season status = %d, want 200 (%s)", seasonResp.StatusCode, seasonRaw)
	}
	if got := itemListIDs(t, decodeItemListArray(t, seasonRaw)); !reflect.DeepEqual(got, []int64{5, 1}) {
		t.Fatalf("season list ids = %v, want [5 1]", got)
	}

	personResp, personRaw := itemListGET(t, app, "/v1/people/1/lists/personal/popular")
	if personResp.StatusCode != http.StatusOK {
		t.Fatalf("person status = %d, want 200 (%s)", personResp.StatusCode, personRaw)
	}
	if got := itemListIDs(t, decodeItemListArray(t, personRaw)); !reflect.DeepEqual(got, []int64{6}) {
		t.Fatalf("person list ids = %v, want [6]", got)
	}

	showResp, showRaw := itemListGET(t, app, "/v1/shows/1/lists/personal/likes")
	if showResp.StatusCode != http.StatusOK {
		t.Fatalf("show status = %d, want 200 (%s)", showResp.StatusCode, showRaw)
	}
	if got := itemListIDs(t, decodeItemListArray(t, showRaw)); !reflect.DeepEqual(got, []int64{4, 1}) {
		t.Fatalf("show list ids = %v, want [4 1]", got)
	}
}
