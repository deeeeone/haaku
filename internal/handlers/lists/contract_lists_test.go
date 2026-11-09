package lists

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// contractTestDB builds a migrated database and points the package at it.
func contractTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		db, err = gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{})
	} else {
		db, err = gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	}
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if sqlDB, serr := db.DB(); serr == nil {
		// A single connection keeps the in-memory schema visible inside
		// cache.WithCacheTransaction, which opens a transaction.
		sqlDB.SetMaxOpenConns(1)
	}
	models := []any{
		&database.User{}, &database.UserFollow{}, &database.UserBlock{},
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.ExternalID{},
	}
	models = append(models, database.ListModels()...)
	models = append(models, &database.ListCollaborator{}, &database.ContentReport{})
	// On a real PostgreSQL that Goose already migrated (the dev-pg recipe) the
	// schema is authoritative: AutoMigrate would try to retype tsvector columns
	// and fail. sqlite keeps building its own schema with AutoMigrate.
	if os.Getenv("DATABASE_URL") != "" && db.Migrator().HasTable(&database.List{}) {
		database.SetDBForTest(t, db)
		return db
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// contractTestApp mounts the contract handlers behind a header-selected caller.
// AuthRequired cannot be satisfied offline, so the caller is pinned by
// X-Test-User and the same handlers are mounted without the token gate.
func contractTestApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if u := c.Get("X-Test-User"); u != "" {
			c.Locals("user_id", u)
		}
		return c.Next()
	})

	v1.Post("/users/:id/lists", handleContractCreateList)
	v1.Get("/users/:id/lists/collaborations", handleContractCollaborations)
	v1.Post("/users/:id/lists/reorder", handleContractReorderUserLists)
	v1.Get("/users/:id/lists/:list_id/", handleContractGetList)
	v1.Put("/users/:id/lists/:list_id/", handleContractUpdateList)
	v1.Delete("/users/:id/lists/:list_id/", handleContractDeleteList)
	v1.Post("/users/:id/lists/:list_id/items", handleContractAddItems)
	v1.Post("/users/:id/lists/:list_id/items/remove", handleContractRemoveItems)
	v1.Post("/users/:id/lists/:list_id/items/reorder", handleContractReorderItems)
	v1.Post("/users/:id/lists/:list_id/reorder", handleContractReorderItems)
	v1.Put("/users/:id/lists/:list_id/items/:list_item_id", handleContractUpdateItem)
	v1.Get("/users/:id/lists/:list_id/items/movie", handleContractListItemsMovie)
	v1.Get("/users/:id/lists/:list_id/items/show", handleContractListItemsShow)
	v1.Get("/users/:id/lists/:list_id/items/movie,show", handleContractListItemsMovieShow)
	v1.Get("/users/:id/lists/:list_id/items/movie,show,season,episode", handleContractListItemsAll)
	v1.Get("/users/:id/lists/:list_id/items/:type/:sort_by/:sort_how", handleContractListItemsTyped)
	v1.Get("/lists/:id/items/movie", handleContractListItemsMovie)
	v1.Get("/lists/:id/items/show", handleContractListItemsShow)
	v1.Get("/lists/:id/items/movie,show", handleContractListItemsMovieShow)
	v1.Get("/lists/:id/items/movie,show,episode,season", handleContractListItemsAll)
	v1.Get("/lists/:id/items/:type/:sort_by/:sort_how", handleContractListItemsTyped)
	v1.Get("/users/:id/lists/:list_id/likes", handleContractListLikes)
	v1.Post("/users/:id/lists/:list_id/like", handleContractLikeList)
	v1.Delete("/users/:id/lists/:list_id/like", handleContractUnlikeList)
	v1.Get("/users/:id/lists/:list_id/comments/:sort", handleContractListComments)
	v1.Get("/lists/:id/comments/:sort", handleContractListComments)
	v1.Post("/users/:id/lists/:list_id/report", handleContractReportList)
	v1.Post("/lists/:id/report", handleContractReportList)
	v1.Get("/users/:id/smart-lists", handleContractGetSmartLists)
	v1.Post("/users/:id/smart-lists", handleContractCreateSmartList)
	v1.Get("/users/:id/smart-lists/:list_id/", handleContractGetSmartList)
	v1.Put("/users/:id/smart-lists/:list_id/", handleContractUpdateSmartList)
	v1.Delete("/users/:id/smart-lists/:list_id/", handleContractDeleteSmartList)
	v1.Get("/smart-lists/:list_id/items/:type/:sort_by/:sort_how", handleContractSmartListItems)
	v1.Get("/users/:id/lists/:list_id/smart-list-draft", handleContractSmartListDraft)
	v1.Post("/users/:id/lists/:list_id/smart-list-draft/convert", handleContractConvertDraft)
	return app
}

func contractDo(t *testing.T, app *fiber.App, caller, method, path string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, path, rdr)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Host = "example.com"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if caller != "" {
		req.Header.Set("X-Test-User", caller)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func seedContractUser(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Create(&database.User{
		ID: id, Username: "u-" + id, Name: id, Status: "active",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func seedContractMedia(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now()
	// Media ids are deliberately different from list-item ids, so a handler
	// that returns the media id in the item "id" field is visible immediately.
	if err := db.Create(&database.Movie{ID: 11, TmdbID: 603, Slug: "the-matrix", Title: "The Matrix", Metadata: database.JSONMap{}, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	if err := db.Create(&database.Show{ID: 22, TmdbID: 1399, Slug: "the-show", Title: "The Show", Metadata: database.JSONMap{}, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.ExternalID{MediaType: "movie", MediaID: 11, Source: "tmdb", ExternalID: "603"}).Error; err != nil {
		t.Fatalf("seed movie external id: %v", err)
	}
	if err := db.Create(&database.ExternalID{MediaType: "show", MediaID: 22, Source: "tmdb", ExternalID: "1399"}).Error; err != nil {
		t.Fatalf("seed show external id: %v", err)
	}
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode object %s: %v", raw, err)
	}
	return out
}

func decodeArray(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode array %s: %v", raw, err)
	}
	return out
}

// TestContractListRoundTrip walks create -> add items -> reorder -> remove ->
// delete through the contract-shaped routes and asserts each step from stored
// rows.
func TestContractListRoundTrip(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractMedia(t, db)
	app := contractTestApp()

	status, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists", map[string]any{
		"name": "My List", "privacy": "public", "sort_by": "rank", "sort_how": "asc",
	})
	if status != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", status, raw)
	}
	created := decode(t, raw)
	ids := created["ids"].(map[string]any)
	listID := int64(ids["trakt"].(float64))
	if created["item_count"].(float64) != 0 {
		t.Errorf("new list item_count = %v, want 0", created["item_count"])
	}

	listPath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10)

	// Add one movie and one show.
	status, raw = contractDo(t, app, "owner", http.MethodPost, listPath+"/items", map[string]any{
		"movies": []map[string]any{{"ids": map[string]any{"tmdb": 603}}},
		"shows":  []map[string]any{{"ids": map[string]any{"tmdb": 1399}}},
	})
	if status != http.StatusCreated {
		t.Fatalf("add items: want 201, got %d (%s)", status, raw)
	}
	add := decode(t, raw)
	added := add["added"].(map[string]any)
	if added["movies"].(float64) != 1 || added["shows"].(float64) != 1 {
		t.Fatalf("added = %v, want movies=1 shows=1", added)
	}
	if add["list"].(map[string]any)["item_count"].(float64) != 2 {
		t.Errorf("add response item_count = %v, want 2", add["list"].(map[string]any)["item_count"])
	}

	// The plain type filter must split the mixed list.
	_, raw = contractDo(t, app, "", http.MethodGet, "/v1/lists/"+strconv.FormatInt(listID, 10)+"/items/movie", nil)
	movieItems := decodeArray(t, raw)
	_, raw = contractDo(t, app, "", http.MethodGet, "/v1/lists/"+strconv.FormatInt(listID, 10)+"/items/show", nil)
	showItems := decodeArray(t, raw)
	if len(movieItems) != 1 || movieItems[0]["type"] != "movie" {
		t.Fatalf("items/movie = %v, want one movie", movieItems)
	}
	if len(showItems) != 1 || showItems[0]["type"] != "show" {
		t.Fatalf("items/show = %v, want one show", showItems)
	}

	// Reorder using exactly the ids the read returned: the show first, then
	// the movie. That is the only way to catch the item id being filled from
	// the media row instead of the list_items row.
	movieItemID := int64(movieItems[0]["id"].(float64))
	showItemID := int64(showItems[0]["id"].(float64))
	if movieItemID == 11 || showItemID == 22 {
		t.Fatalf("item ids %d/%d are the media ids, want the list_items ids", movieItemID, showItemID)
	}
	order := []int64{showItemID, movieItemID}
	status, raw = contractDo(t, app, "owner", http.MethodPost, listPath+"/items/reorder", map[string]any{"rank": order})
	if status != http.StatusOK {
		t.Fatalf("reorder: want 200, got %d (%s)", status, raw)
	}
	reorder := decode(t, raw)
	if reorder["updated"].(float64) != 2 {
		t.Errorf("reorder updated = %v, want 2", reorder["updated"])
	}
	var first database.ListItem
	db.Where("list_id = ? AND rank = 0", listID).First(&first)
	if first.ID != order[0] {
		t.Errorf("rank 0 item = %d, want %d", first.ID, order[0])
	}
	if first.MediaType != "show" {
		t.Errorf("rank 0 item type = %q, want show (the id we sent first)", first.MediaType)
	}
	// The summary read must report the real item count, not the shape default.
	_, raw = contractDo(t, app, "owner", http.MethodGet, listPath+"/", nil)
	if got := decode(t, raw)["item_count"].(float64); got != 2 {
		t.Errorf("summary item_count = %v, want 2", got)
	}

	// Remove the movie by tmdb reference.
	status, raw = contractDo(t, app, "owner", http.MethodPost, listPath+"/items/remove", map[string]any{
		"movies": []map[string]any{{"ids": map[string]any{"tmdb": 603}}},
	})
	if status != http.StatusOK {
		t.Fatalf("remove: want 200, got %d (%s)", status, raw)
	}
	rem := decode(t, raw)
	if rem["deleted"].(map[string]any)["movies"].(float64) != 1 {
		t.Errorf("deleted = %v, want movies=1", rem["deleted"])
	}

	// Delete the list.
	status, raw = contractDo(t, app, "owner", http.MethodDelete, listPath+"/", nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (%s)", status, raw)
	}
	var n int64
	db.Model(&database.List{}).Where("id = ?", listID).Count(&n)
	if n != 0 {
		t.Errorf("list still exists after delete")
	}
}

// TestContractListPrivacy pins the list gate: a private list is 403 to a
// stranger and 200 to its owner, on the summary read and the item read.
func TestContractListPrivacy(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractUser(t, db, "stranger")
	seedContractMedia(t, db)
	app := contractTestApp()

	status, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists", map[string]any{
		"name": "Private", "privacy": "private",
	})
	if status != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", status, raw)
	}
	listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	listPath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10)

	status, raw = contractDo(t, app, "owner", http.MethodGet, listPath+"/", nil)
	if status != http.StatusOK {
		t.Errorf("owner private list: want 200, got %d (%s)", status, raw)
	}
	status, raw = contractDo(t, app, "stranger", http.MethodGet, listPath+"/", nil)
	if status != http.StatusForbidden {
		t.Errorf("stranger private list: want 403, got %d (%s)", status, raw)
	}
	status, raw = contractDo(t, app, "stranger", http.MethodGet, listPath+"/items/movie", nil)
	if status != http.StatusForbidden {
		t.Errorf("stranger private items: want 403, got %d (%s)", status, raw)
	}
}

// TestContractListTypeCSVFilter proves the CSV {type} segment is a real filter,
// not decoration: movie,show returns both sets, and a mixed list's movie-only
// read excludes the show.
func TestContractListTypeCSVFilter(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractMedia(t, db)
	app := contractTestApp()

	_, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists", map[string]any{"name": "Mixed"})
	listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	listPath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10)
	contractDo(t, app, "owner", http.MethodPost, listPath+"/items", map[string]any{
		"movies": []map[string]any{{"ids": map[string]any{"tmdb": 603}}},
		"shows":  []map[string]any{{"ids": map[string]any{"tmdb": 1399}}},
	})

	_, raw = contractDo(t, app, "", http.MethodGet, listPath+"/items/movie,show", nil)
	both := decodeArray(t, raw)
	if len(both) != 2 {
		t.Fatalf("items/movie,show = %d items, want 2", len(both))
	}
	_, raw = contractDo(t, app, "", http.MethodGet, listPath+"/items/movie", nil)
	if len(decodeArray(t, raw)) != 1 {
		t.Errorf("items/movie should exclude the show")
	}
	// The generic typed form honours the same CSV filter.
	_, raw = contractDo(t, app, "", http.MethodGet, listPath+"/items/show/rank/asc", nil)
	if got := decodeArray(t, raw); len(got) != 1 || got[0]["type"] != "show" {
		t.Errorf("items/show/rank/asc = %v, want one show", got)
	}
}
