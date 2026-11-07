package syncs

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

func getReq(target string) *http.Request {
	req, _ := http.NewRequest("GET", target, nil)
	req.Host = "example.com"
	return req
}

func TestBuildSyncResponse(t *testing.T) {
	created := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	s := &database.SyncSession{
		ID:             11,
		UserID:         "u",
		Kind:           "history",
		Status:         "completed",
		TotalItems:     100,
		ProcessedItems: 100,
		Errors:         database.SyncErrors{},
		CreatedAt:      created,
		UpdatedAt:      created,
	}
	got := buildSyncResponse(s)
	if got.ID != 11 || got.UserID != "u" || got.Kind != "history" || got.Status != "completed" {
		t.Errorf("sync base wrong: %+v", got)
	}
	if got.TotalItems != 100 || got.ProcessedItems != 100 {
		t.Errorf("sync counts wrong: %+v", got)
	}
	if got.CreatedAt != "2026-07-01T09:00:00.000Z" || got.UpdatedAt != "2026-07-01T09:00:00.000Z" {
		t.Errorf("timestamps wrong: %+v", got)
	}
}

func TestParseSyncID(t *testing.T) {
	app := fiber.New()
	app.Get("/sync/:id", func(c *fiber.Ctx) error {
		id, err := parseSyncID(c)
		if err != nil {
			return err
		}
		return c.SendString(string(rune(id)))
	})

	// Valid positive id parses.
	resp, err := app.Test(getReq("/sync/42"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid id status = %d, want 200", resp.StatusCode)
	}

	// Non-numeric id → 400.
	resp2, err := app.Test(getReq("/sync/abc"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("non-numeric id status = %d, want 400", resp2.StatusCode)
	}

	// Zero id → 400.
	resp3, err := app.Test(getReq("/sync/0"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Errorf("zero id status = %d, want 400", resp3.StatusCode)
	}
}

// --- DB-backed handler tests ------------------------------------------------

const syncsTestUser = "user-syncs-test"

// setupSyncsDB mirrors the per-module harness: real PostgreSQL when DATABASE_URL
// is set, in-memory sqlite otherwise, so CI needs no services.
func setupSyncsDB(t *testing.T) *gorm.DB {
	t.Helper()
	models := []interface{}{&database.SyncSession{}, &database.SyncSessionItem{}}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err = gorm.Open(postgres.Open(url), &gorm.Config{})
	}
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Migrator().DropTable(models...); err != nil {
		t.Fatalf("drop stale tables: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// newSyncsApp mounts the four handlers behind a stub auth that pins user_id
// from an X-Test-User header (default syncsTestUser), so ownership scoping can
// be exercised without a token. Routes are registered exactly as RegisterRoutes
// does: static paths before the :id<int> variants they share a depth with.
func newSyncsApp() *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		u := c.Get("X-Test-User")
		if u == "" {
			u = syncsTestUser
		}
		c.Locals("user_id", u)
		return c.Next()
	})
	app.Post("/v1/syncs", handleCreateSync)
	app.Get("/v1/syncs/:id<int>", handleGetSync)
	app.Get("/v1/syncs/:id<int>/paused", handleGetSyncPaused)
	app.Get("/v1/syncs/:id<int>/skipped", handleGetSyncSkipped)
	return app
}

func doJSON(t *testing.T, app *fiber.App, method, path, user, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(method, path, r)
	req.Host = "example.com"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, raw
}

// TestCreateSyncPersistsItems proves POST /v1/syncs no longer discards the
// submitted batch: the items are stored, seq-ordered and pending, so the
// session is resumable — the core gap issue #6 names.
func TestCreateSyncPersistsItems(t *testing.T) {
	db := setupSyncsDB(t)
	app := newSyncsApp()

	status, raw := doJSON(t, app, http.MethodPost, "/v1/syncs", "",
		`{"kind":"history","items":[{"ids":{"tmdb":550},"media_type":"movie"},{"ids":{"tmdb":999},"media_type":"movie"}]}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", status, raw)
	}
	var created SyncResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.TotalItems != 2 || created.Status != database.SyncStatusPending {
		t.Errorf("created = %+v, want 2 pending items", created)
	}
	// Errors is the typed shape, and it marshals as an object, not the old
	// untyped map / null.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if string(m["errors"]) == "null" {
		t.Error("errors should serialize as an object, got null")
	}

	var items []database.SyncSessionItem
	if err := db.Where("session_id = ?", created.ID).Order("seq ASC").Find(&items).Error; err != nil {
		t.Fatalf("load items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("persisted items = %d, want 2", len(items))
	}
	if items[0].Seq != 1 || items[1].Seq != 2 {
		t.Errorf("seqs = %d,%d want 1,2", items[0].Seq, items[1].Seq)
	}
	if items[0].State != "pending" || items[1].State != "pending" {
		t.Errorf("states = %q,%q want pending", items[0].State, items[1].State)
	}
	if items[0].Payload["ids"] == nil {
		t.Errorf("item payload not persisted: %+v", items[0].Payload)
	}
}

// TestCreateSyncRejectsNonObjectItems pins the up-front validation: an item the
// engine could never resolve is refused rather than stored as poison.
func TestCreateSyncRejectsNonObjectItems(t *testing.T) {
	setupSyncsDB(t)
	app := newSyncsApp()

	cases := []string{
		`{"kind":"history","items":[123]}`,
		`{"kind":"history","items":[["a"]]}`,
		`{"kind":"history","items":["oops"]}`,
	}
	for _, body := range cases {
		status, raw := doJSON(t, app, http.MethodPost, "/v1/syncs", "", body)
		if status != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400 (%s)", body, status, raw)
		}
	}
	// An empty batch is valid: a session with no items is a real (if trivial)
	// session, not an error.
	status, raw := doJSON(t, app, http.MethodPost, "/v1/syncs", "", `{"kind":"history","items":[]}`)
	if status != http.StatusCreated {
		t.Errorf("empty batch status = %d, want 201 (%s)", status, raw)
	}
}

// seedProcessedSession writes a session with one item per terminal outcome and
// marks it completed, standing in for what the engine (a worker) persists.
func seedProcessedSession(t *testing.T, db *gorm.DB, userID string) *database.SyncSession {
	t.Helper()
	s := &database.SyncSession{UserID: userID, Kind: "history", Status: database.SyncStatusCompleted, TotalItems: 3, ProcessedItems: 3}
	if err := db.Create(s).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	added, notFound, duplicate := "added", "not_found", "duplicate"
	code, msg := "not_found", "tmdb id 999 did not resolve"
	rows := []database.SyncSessionItem{
		{SessionID: s.ID, Seq: 1, Payload: database.JSONMap{"ids": database.JSONMap{"tmdb": 550}}, State: "processed", Outcome: &added},
		{SessionID: s.ID, Seq: 2, Payload: database.JSONMap{"ids": database.JSONMap{"tmdb": 999}}, State: "processed", Outcome: &notFound, ErrorCode: &code, Error: &msg},
		{SessionID: s.ID, Seq: 3, Payload: database.JSONMap{"ids": database.JSONMap{"tmdb": 550}}, State: "processed", Outcome: &duplicate},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("create items: %v", err)
	}
	return s
}

// TestPausedAndSkippedServeEngineOutput is the acceptance criterion that
// /paused and /skipped return real paginated data instead of 501.
func TestPausedAndSkippedServeEngineOutput(t *testing.T) {
	db := setupSyncsDB(t)
	app := newSyncsApp()
	s := seedProcessedSession(t, db, syncsTestUser)

	// /paused = not_found + error (one item here).
	status, raw := doJSON(t, app, http.MethodGet, "/v1/syncs/"+itoa(s.ID)+"/paused", "", "")
	if status != http.StatusOK {
		t.Fatalf("paused status = %d, want 200 (%s)", status, raw)
	}
	var paused struct {
		SessionID int64              `json:"session_id"`
		Status    string             `json:"status"`
		Items     []SyncItemResponse `json:"items"`
		RowCount  int64              `json:"row_count"`
	}
	if err := json.Unmarshal(raw, &paused); err != nil {
		t.Fatalf("decode paused: %v (%s)", err, raw)
	}
	if paused.SessionID != s.ID || paused.RowCount != 1 || len(paused.Items) != 1 {
		t.Fatalf("paused = %+v, want 1 item", paused)
	}
	if paused.Items[0].Seq != 2 || paused.Items[0].Outcome != "not_found" {
		t.Errorf("paused item = %+v, want seq 2 not_found", paused.Items[0])
	}
	if paused.Items[0].ErrorCode == nil || *paused.Items[0].ErrorCode != "not_found" {
		t.Errorf("paused item error_code = %v", paused.Items[0].ErrorCode)
	}

	// /skipped = duplicate (one item here).
	status, raw = doJSON(t, app, http.MethodGet, "/v1/syncs/"+itoa(s.ID)+"/skipped", "", "")
	if status != http.StatusOK {
		t.Fatalf("skipped status = %d, want 200 (%s)", status, raw)
	}
	var skipped struct {
		Items    []SyncItemResponse `json:"items"`
		RowCount int64              `json:"row_count"`
	}
	if err := json.Unmarshal(raw, &skipped); err != nil {
		t.Fatalf("decode skipped: %v (%s)", err, raw)
	}
	if skipped.RowCount != 1 || len(skipped.Items) != 1 || skipped.Items[0].Seq != 3 {
		t.Fatalf("skipped = %+v, want the seq-3 duplicate", skipped)
	}
}

// TestPausedScopesToOwner pins that another user's session id is
// indistinguishable from a missing one (404, not 403, and never its data).
func TestPausedScopesToOwner(t *testing.T) {
	db := setupSyncsDB(t)
	app := newSyncsApp()
	s := seedProcessedSession(t, db, "someone-else")

	status, raw := doJSON(t, app, http.MethodGet, "/v1/syncs/"+itoa(s.ID)+"/paused", "", "")
	if status != http.StatusNotFound {
		t.Errorf("other user's session status = %d, want 404 (%s)", status, raw)
	}
	status, raw = doJSON(t, app, http.MethodGet, "/v1/syncs/999999/paused", "", "")
	if status != http.StatusNotFound {
		t.Errorf("missing session status = %d, want 404 (%s)", status, raw)
	}
}

// TestGetSyncReadsOwnSession covers progress read-back for the caller's own
// session.
func TestGetSyncReadsOwnSession(t *testing.T) {
	db := setupSyncsDB(t)
	app := newSyncsApp()
	s := seedProcessedSession(t, db, syncsTestUser)

	status, raw := doJSON(t, app, http.MethodGet, "/v1/syncs/"+itoa(s.ID), "", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	var got SyncResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if got.ID != s.ID || got.Status != database.SyncStatusCompleted || got.TotalItems != 3 {
		t.Errorf("sync = %+v", got)
	}
}

// TestItemObject is the unit guard for the payload normaliser.
func TestItemObject(t *testing.T) {
	if _, ok := itemObject("scalar"); ok {
		t.Error("a string is not an item object")
	}
	if _, ok := itemObject([]any{"a"}); ok {
		t.Error("an array is not an item object")
	}
	if _, ok := itemObject(nil); ok {
		t.Error("nil is not an item object")
	}
	obj, ok := itemObject(map[string]any{"ids": map[string]any{"tmdb": 550}})
	if !ok || obj == nil || obj["ids"] == nil {
		t.Errorf("valid object rejected: %+v ok=%v", obj, ok)
	}
}

// TestBuildSyncItemResponse covers the pointer-to-value projection: a pending
// item has no outcome and must not panic.
func TestBuildSyncItemResponse(t *testing.T) {
	pending := database.SyncSessionItem{ID: 1, Seq: 7, State: "pending"}
	got := buildSyncItemResponse(&pending)
	if got.ID != 1 || got.Seq != 7 || got.Outcome != "" {
		t.Errorf("pending item = %+v", got)
	}
	if got.ProcessedAt != nil || got.WatchedAt != nil {
		t.Errorf("pending item should have no timestamps: %+v", got)
	}

	done := "added"
	code := "write_error"
	watched := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	it := database.SyncSessionItem{
		ID: 2, Seq: 8, State: "processed", Outcome: &done, ErrorCode: &code,
		WatchedAt: &watched, ProcessedAt: &watched,
	}
	got = buildSyncItemResponse(&it)
	if got.Outcome != "added" || *got.WatchedAt != "2026-01-02T03:04:05.000Z" {
		t.Errorf("processed item = %+v", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
