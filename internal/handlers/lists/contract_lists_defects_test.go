package lists

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// defectDo is contractDo with an Authorization header, for tests that mount the
// routes with the real auth middleware instead of the X-Test-User shim.
func defectDo(t *testing.T, app *fiber.App, token, method, path string, body any) (int, []byte) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// contractListItemIDs returns the stored list_items ids for a list, keyed by
// media type.
func contractListItemIDs(t *testing.T, db *gorm.DB, listID int64) map[string]int64 {
	t.Helper()
	var items []database.ListItem
	if err := db.Where("list_id = ?", listID).Find(&items).Error; err != nil {
		t.Fatalf("load list items: %v", err)
	}
	out := make(map[string]int64, len(items))
	for i := range items {
		out[items[i].MediaType] = items[i].ID
	}
	return out
}

// TestContractItemReadReturnsListItemIDs is the regression test for the item
// `id` field: it must be list_items.id (what reorder and item-update accept),
// never the media id. A client that reads a list and then reorders with the ids
// it was given must move the rows it named. Movie and show ids come from
// separate tables, so returning the media id can also target an unrelated row.
func TestContractItemReadReturnsListItemIDs(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractMedia(t, db)
	app := contractTestApp()

	_, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists", map[string]any{"name": "Ids"})
	listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	listPath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10)
	_, raw = contractDo(t, app, "owner", http.MethodPost, listPath+"/items", map[string]any{
		"movies": []map[string]any{{"ids": map[string]any{"tmdb": 603}}},
		"shows":  []map[string]any{{"ids": map[string]any{"tmdb": 1399}}},
	})
	if raw == nil {
		t.Fatal("add items returned no body")
	}

	stored := contractListItemIDs(t, db, listID)
	_, raw = contractDo(t, app, "owner", http.MethodGet, listPath+"/items/movie,show", nil)
	read := decodeArray(t, raw)
	if len(read) != 2 {
		t.Fatalf("read %d items, want 2", len(read))
	}
	for _, it := range read {
		want := stored[it["type"].(string)]
		if got := int64(it["id"].(float64)); got != want {
			t.Errorf("%s item id = %d, want list_items.id %d", it["type"], got, want)
		}
	}

	// Reorder with the ids the read returned, putting the show first.
	var showID, movieID int64
	for _, it := range read {
		if it["type"].(string) == "show" {
			showID = int64(it["id"].(float64))
		}
		if it["type"].(string) == "movie" {
			movieID = int64(it["id"].(float64))
		}
	}
	status, raw := contractDo(t, app, "owner", http.MethodPost, listPath+"/items/reorder",
		map[string]any{"rank": []int64{showID, movieID}})
	if status != http.StatusOK {
		t.Fatalf("reorder: want 200, got %d (%s)", status, raw)
	}
	if got := decode(t, raw)["updated"].(float64); got != 2 {
		t.Fatalf("reorder updated = %v, want 2", got)
	}
	var first database.ListItem
	if err := db.Where("list_id = ? AND rank = 0", listID).First(&first).Error; err != nil {
		t.Fatalf("load rank 0 item: %v", err)
	}
	if first.ID != showID || first.MediaType != "show" {
		t.Errorf("rank 0 item = %d (%s), want the show item %d", first.ID, first.MediaType, showID)
	}
}

// TestContractSetLikeSurfacesStoreErrors is the regression test for a dropped
// query error: if the like insert (or the unlike delete) fails, the handler must
// not answer 204 as though it worked. Dropping the table is a store failure the
// sqlite handler suite can produce deterministically.
func TestContractSetLikeSurfacesStoreErrors(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	app := contractTestApp()

	_, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists", map[string]any{"name": "Likes"})
	listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	likePath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10) + "/like"

	if err := db.Migrator().DropTable(&database.ListLike{}); err != nil {
		t.Fatalf("drop list_likes: %v", err)
	}
	status, raw := contractDo(t, app, "owner", http.MethodPost, likePath, nil)
	if status != http.StatusInternalServerError {
		t.Errorf("like with a failing insert = %d (%s), want 500", status, raw)
	}

	status, raw = contractDo(t, app, "owner", http.MethodDelete, likePath, nil)
	if status != http.StatusInternalServerError {
		t.Errorf("unlike with a failing delete = %d (%s), want 500", status, raw)
	}
}

// TestContractOptionalReadsEstablishIdentity proves the optional read routes
// resolve the bearer token themselves instead of relying on the rate limiter to
// have done it. No rate-limit middleware is mounted here, which is exactly the
// RATE_LIMIT_ENABLED=false shape: the owner must still get 200 on a private
// list (including via /users/me) and a non-owner must get 403.
func TestContractOptionalReadsEstablishIdentity(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractUser(t, db, "stranger")
	seedContractMedia(t, db)
	if err := db.AutoMigrate(&database.LocalSession{}); err != nil {
		t.Fatalf("migrate local_sessions: %v", err)
	}
	if err := auth.InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	// Built at runtime: a secret-shaped literal anywhere in history fails the
	// gitleaks job for every PR.
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))

	ownerToken, _, err := auth.CreateSession(db, "owner", "", "")
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	strangerToken, _, err := auth.CreateSession(db, "stranger", "", "")
	if err != nil {
		t.Fatalf("create stranger session: %v", err)
	}

	app := fiber.New()
	RegisterManagementContractRoutes(app.Group("/v1"))

	status, raw := defectDo(t, app, ownerToken, http.MethodPost, "/v1/users/owner/lists",
		map[string]any{"name": "Private", "privacy": "private"})
	if status != http.StatusCreated {
		t.Fatalf("create private list: want 201, got %d (%s)", status, raw)
	}
	listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	listPath := "/v1/users/owner/lists/" + strconv.FormatInt(listID, 10) + "/"

	if status, raw = defectDo(t, app, ownerToken, http.MethodGet, listPath, nil); status != http.StatusOK {
		t.Errorf("owner private read = %d (%s), want 200", status, raw)
	}
	if status, raw = defectDo(t, app, ownerToken, http.MethodGet, "/v1/users/me/lists/"+strconv.FormatInt(listID, 10)+"/", nil); status != http.StatusOK {
		t.Errorf("owner /users/me private read = %d (%s), want 200", status, raw)
	}
	if status, raw = defectDo(t, app, strangerToken, http.MethodGet, listPath, nil); status != http.StatusForbidden {
		t.Errorf("non-owner private read = %d (%s), want 403", status, raw)
	}
	if status, raw = defectDo(t, app, "", http.MethodGet, listPath, nil); status != http.StatusForbidden {
		t.Errorf("anonymous private read = %d (%s), want 403", status, raw)
	}
	// The item read is optional-auth too.
	if status, raw = defectDo(t, app, ownerToken, http.MethodGet, "/v1/lists/"+strconv.FormatInt(listID, 10)+"/items/movie", nil); status != http.StatusOK {
		t.Errorf("owner private item read = %d (%s), want 200", status, raw)
	}
}

// TestContractDuplicateSlugCreateRetries proves a second list with the same name
// is created with a distinct slug instead of failing. On PostgreSQL this is the
// savepoint path: a failed insert aborts the transaction and the retry must roll
// back to a savepoint or it dies with "current transaction is aborted".
func TestContractDuplicateSlugCreateRetries(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	app := contractTestApp()

	var slugs []string
	for i := 0; i < 2; i++ {
		status, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/lists",
			map[string]any{"name": "Same Name"})
		if status != http.StatusCreated {
			t.Fatalf("create #%d: want 201, got %d (%s)", i+1, status, raw)
		}
		listID := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
		var l database.List
		if err := db.First(&l, listID).Error; err != nil {
			t.Fatalf("load list #%d: %v", i+1, err)
		}
		slugs = append(slugs, l.Slug)
	}
	if len(slugs) != 2 || slugs[0] == slugs[1] {
		t.Fatalf("slugs = %v, want two distinct values", slugs)
	}
}

// TestContractSmartListPartialUpdateMerges proves a PUT that names one field
// does not erase the stored filters, source or media_type.
func TestContractSmartListPartialUpdateMerges(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	app := contractTestApp()

	status, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/smart-lists", map[string]any{
		"name": "Action", "source": "library", "media_type": "movies",
		"filters": map[string]any{"genre": "action"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create smart list: want 201, got %d (%s)", status, raw)
	}
	id := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	path := "/v1/users/owner/smart-lists/" + strconv.FormatInt(id, 10) + "/"

	status, raw = contractDo(t, app, "owner", http.MethodPut, path, map[string]any{"source": "watchlist"})
	if status != http.StatusOK {
		t.Fatalf("update smart list: want 200, got %d (%s)", status, raw)
	}
	got := decode(t, raw)
	if got["source"] != "watchlist" {
		t.Errorf("source = %v, want watchlist", got["source"])
	}
	if got["media_type"] != "movies" {
		t.Errorf("media_type = %v, want the stored movies", got["media_type"])
	}
	filters, _ := got["filters"].(map[string]any)
	if filters["genre"] != "action" {
		t.Errorf("filters = %v, want the stored genre kept", got["filters"])
	}
}

// TestContractSmartListItemsRespectSavedMediaType proves a movies-only smart
// list asked for the broad "media" form cannot return shows.
func TestContractSmartListItemsRespectSavedMediaType(t *testing.T) {
	db := contractTestDB(t)
	seedContractUser(t, db, "owner")
	seedContractMedia(t, db)
	app := contractTestApp()

	status, raw := contractDo(t, app, "owner", http.MethodPost, "/v1/users/owner/smart-lists", map[string]any{
		"name": "Movies only", "source": "library", "media_type": "movies",
	})
	if status != http.StatusCreated {
		t.Fatalf("create smart list: want 201, got %d (%s)", status, raw)
	}
	id := int64(decode(t, raw)["ids"].(map[string]any)["trakt"].(float64))
	path := "/v1/smart-lists/" + strconv.FormatInt(id, 10) + "/items/media/rank/desc"

	status, raw = contractDo(t, app, "owner", http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("smart list items: want 200, got %d (%s)", status, raw)
	}
	items := decodeArray(t, raw)
	if len(items) == 0 {
		t.Fatal("smart list returned no items; the test would pass vacuously")
	}
	for _, it := range items {
		if it["type"] != "movie" {
			t.Errorf("items/media returned a %v for a movies-only smart list", it["type"])
		}
	}

	// A contradictory explicit type is a valid request with an empty result.
	status, raw = contractDo(t, app, "owner", http.MethodGet,
		"/v1/smart-lists/"+strconv.FormatInt(id, 10)+"/items/show/rank/desc", nil)
	if status != http.StatusOK {
		t.Fatalf("contradictory type: want 200, got %d (%s)", status, raw)
	}
	if got := decodeArray(t, raw); len(got) != 0 {
		t.Errorf("contradictory type returned %d items, want 0", len(got))
	}

	// The rejection message must only name types the handler accepts.
	status, raw = contractDo(t, app, "owner", http.MethodGet,
		"/v1/smart-lists/"+strconv.FormatInt(id, 10)+"/items/season/rank/desc", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("bad type: want 400, got %d (%s)", status, raw)
	}
	if msg := string(raw); strings.Contains(msg, "season") || strings.Contains(msg, "episode") {
		t.Errorf("400 message still advertises a rejected type: %s", msg)
	}
}
