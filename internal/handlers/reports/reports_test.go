package reports

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB opens an in-memory sqlite DB, migrates the tables the report
// handlers touch, and points the database singleton at it via the test-only
// SetDBForTest seam. Each call gets a fresh DB; the seam resets the singleton
// when the test ends.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.Movie{}, &database.ContentReport{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func seedUser(t *testing.T, db *gorm.DB, id string, isModerator, isAdmin bool) {
	t.Helper()
	u := database.User{ID: id, Username: id, IsModerator: isModerator, IsAdmin: isAdmin}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func seedMovie(t *testing.T, db *gorm.DB, title string) database.Movie {
	t.Helper()
	now := time.Now().UTC()
	m := database.Movie{Title: title, TmdbID: len(title) + 100, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	return m
}

func seedReport(t *testing.T, db *gorm.DB, r database.ContentReport) database.ContentReport {
	t.Helper()
	if err := db.Create(&r).Error; err != nil {
		t.Fatalf("seed report: %v", err)
	}
	return r
}

// makeModApp registers the moderation routes behind a stub that injects userID
// into ctx locals (bypassing Clerk) but keeps the real RequireModerator gate,
// so 200/400/403/404 can be asserted without a Clerk emulator.
func makeModApp(userID string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	v1 := app.Group("/v1")
	v1.Get("/reports", stub, auth.RequireModerator(), handleListReports)
	v1.Post("/reports/:id/resolve", stub, auth.RequireModerator(), handleResolveReport)
	v1.Post("/reports/:id/dismiss", stub, auth.RequireModerator(), handleDismissReport)
	return app
}

// makeSubmitApp registers the four report-submission routes behind the same
// stub, so the reopen/duplicate paths can be exercised.
func makeSubmitApp(userID string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	v1 := app.Group("/v1")
	v1.Post("/movies/:id/report", stub, handleReportMovie)
	v1.Post("/shows/:id/report", stub, handleReportShow)
	v1.Post("/seasons/:id/report", stub, handleReportSeason)
	v1.Post("/episodes/:id/report", stub, handleReportEpisode)
	return app
}

// makeRealApp registers the routes exactly as production does, with the shared
// auth.AuthRequired() middleware, so the 401 guard can be asserted.
func makeRealApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	v1 := app.Group("/v1")
	RegisterRoutes(v1)
	return app
}

func do(t *testing.T, app *fiber.App, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, path, rdr)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	req.Host = "example.com"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}
	return resp
}

func decodeList(t *testing.T, resp *http.Response) []reportResponse {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []reportResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

func decodeOne(t *testing.T, resp *http.Response) reportResponse {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out reportResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	return out
}

// ---------------------------------------------------------------------------
// Existing unit tests
// ---------------------------------------------------------------------------

func TestValidReportReasonsAllowlist(t *testing.T) {
	allowed := []string{"spam", "spoiler", "offensive", "incorrect", "duplicate", "other"}
	for _, r := range allowed {
		if !validReportReasons[r] {
			t.Errorf("validReportReasons missing %q", r)
		}
	}
	for _, r := range []string{"", "violence", "SPAM"} {
		if validReportReasons[r] {
			t.Errorf("validReportReasons unexpectedly accepts %q", r)
		}
	}
}

func TestMaxReportDescription(t *testing.T) {
	if maxReportDescription != 1000 {
		t.Errorf("maxReportDescription = %d, want 1000", maxReportDescription)
	}
}

func TestToReportResponse(t *testing.T) {
	created := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	r := &database.ContentReport{
		ID:          3,
		ReporterID:  "user-1",
		MediaType:   "movie",
		MediaID:     42,
		Reason:      "spam",
		Description: "dupe",
		CreatedAt:   created,
	}
	got := toReportResponse(r)
	if got.ID != 3 || got.ReporterID != "user-1" || got.MediaType != "movie" || got.MediaID != 42 {
		t.Errorf("report base wrong: %+v", got)
	}
	if got.Reason != "spam" || got.Description != "dupe" {
		t.Errorf("report content wrong: %+v", got)
	}
	if got.CreatedAt != "2026-06-01T10:00:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
	// A row with no status column value (pre-migration read) reports as open.
	if got.Status != reportStatusOpen {
		t.Errorf("Status = %q, want open", got.Status)
	}
}

// ---------------------------------------------------------------------------
// Moderation queue: list
// ---------------------------------------------------------------------------

func TestListReports(t *testing.T) {
	db := setupTestDB(t)
	seedUser(t, db, "mod-1", true, false)
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	seedReport(t, db, database.ContentReport{ReporterID: "a", MediaType: "movie", MediaID: 1, Reason: "spam", Status: reportStatusOpen, CreatedAt: base})
	seedReport(t, db, database.ContentReport{ReporterID: "b", MediaType: "movie", MediaID: 2, Reason: "spoiler", Status: reportStatusOpen, CreatedAt: base.Add(time.Minute)})
	seedReport(t, db, database.ContentReport{ReporterID: "c", MediaType: "show", MediaID: 3, Reason: "offensive", Status: reportStatusResolved, ResolvedBy: "mod-1", CreatedAt: base.Add(2 * time.Minute)})

	app := makeModApp("mod-1")

	t.Run("200 lists every report, newest first, with pagination headers", func(t *testing.T) {
		resp := do(t, app, http.MethodGet, "/v1/reports", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Pagination-Total-Items"); got != "3" {
			t.Errorf("X-Pagination-Total-Items = %q, want 3", got)
		}
		if got := resp.Header.Get("X-Pagination-Items-Count"); got != "3" {
			t.Errorf("X-Pagination-Items-Count = %q, want 3", got)
		}
		body := decodeList(t, resp)
		if len(body) != 3 {
			t.Fatalf("expected 3 reports, got %d", len(body))
		}
		if body[0].MediaType != "show" || body[0].Status != reportStatusResolved {
			t.Errorf("expected newest (resolved show) first, got %+v", body[0])
		}
	})

	t.Run("status filter narrows to open", func(t *testing.T) {
		resp := do(t, app, http.MethodGet, "/v1/reports?status=open", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Pagination-Total-Items"); got != "2" {
			t.Errorf("X-Pagination-Total-Items = %q, want 2", got)
		}
		if body := decodeList(t, resp); len(body) != 2 {
			t.Fatalf("expected 2 open reports, got %d", len(body))
		}
	})

	t.Run("target_type filter narrows to shows", func(t *testing.T) {
		resp := do(t, app, http.MethodGet, "/v1/reports?target_type=show", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if body := decodeList(t, resp); len(body) != 1 || body[0].MediaType != "show" {
			t.Fatalf("expected 1 show report, got %+v", body)
		}
	})

	t.Run("invalid status is 400", func(t *testing.T) {
		resp := do(t, app, http.MethodGet, "/v1/reports?status=bogus", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid target_type is 400", func(t *testing.T) {
		resp := do(t, app, http.MethodGet, "/v1/reports?target_type=bogus", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// ---------------------------------------------------------------------------
// Moderation queue: resolve / dismiss
// ---------------------------------------------------------------------------

func TestResolveReport(t *testing.T) {
	db := setupTestDB(t)
	seedUser(t, db, "mod-1", false, true)
	rep := seedReport(t, db, database.ContentReport{ReporterID: "reporter", MediaType: "movie", MediaID: 1, Reason: "spam", Status: reportStatusOpen, CreatedAt: time.Now().UTC()})

	app := makeModApp("mod-1")

	t.Run("200 records the operator, note and timestamp", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/reports/1/resolve", []byte(`{"note":"handled"}`))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeOne(t, resp)
		if got.Status != reportStatusResolved {
			t.Errorf("Status = %q, want resolved", got.Status)
		}
		if got.ResolvedBy != "mod-1" {
			t.Errorf("ResolvedBy = %q, want mod-1", got.ResolvedBy)
		}
		if got.ResolutionNote != "handled" {
			t.Errorf("ResolutionNote = %q, want handled", got.ResolutionNote)
		}
		if got.ResolvedAt == "" {
			t.Error("ResolvedAt is empty, want a timestamp")
		}
	})

	t.Run("409 on a second action", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/reports/1/dismiss", nil)
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})

	_ = rep
}

func TestDismissReport(t *testing.T) {
	db := setupTestDB(t)
	seedUser(t, db, "mod-1", true, false)
	seedReport(t, db, database.ContentReport{ReporterID: "reporter", MediaType: "show", MediaID: 9, Reason: "duplicate", Status: reportStatusOpen, CreatedAt: time.Now().UTC()})

	app := makeModApp("mod-1")
	resp := do(t, app, http.MethodPost, "/v1/reports/1/dismiss", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	got := decodeOne(t, resp)
	if got.Status != reportStatusDismissed || got.ResolvedBy != "mod-1" {
		t.Errorf("unexpected dismiss result: %+v", got)
	}
}

func TestActOnReportErrors(t *testing.T) {
	db := setupTestDB(t)
	seedUser(t, db, "mod-1", true, false)
	seedReport(t, db, database.ContentReport{ReporterID: "reporter", MediaType: "movie", MediaID: 1, Reason: "spam", Status: reportStatusOpen, CreatedAt: time.Now().UTC()})
	app := makeModApp("mod-1")

	t.Run("404 for a missing report", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/reports/9999/resolve", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("400 for a non-numeric id", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/reports/abc/resolve", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("400 for a note over the cap", func(t *testing.T) {
		big := make([]byte, maxResolutionNote+1)
		for i := range big {
			big[i] = 'x'
		}
		payload, _ := json.Marshal(map[string]string{"note": string(big)})
		resp := do(t, app, http.MethodPost, "/v1/reports/1/resolve", payload)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// ---------------------------------------------------------------------------
// Authorization
// ---------------------------------------------------------------------------

func TestModerationRequiresAuth(t *testing.T) {
	app := makeRealApp()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/v1/reports"},
		{http.MethodPost, "/v1/reports/1/resolve"},
		{http.MethodPost, "/v1/reports/1/dismiss"},
	} {
		resp := do(t, app, tc.method, tc.path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestModerationForbiddenForNonModerator(t *testing.T) {
	db := setupTestDB(t)
	seedUser(t, db, "plain-user", false, false)
	auth.InitModerationAllowlists(nil, nil)
	t.Cleanup(func() { auth.InitModerationAllowlists(nil, nil) })

	app := makeModApp("plain-user")
	resp := do(t, app, http.MethodGet, "/v1/reports", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for a non-moderator, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Re-reporting behaviour
// ---------------------------------------------------------------------------

func TestReportDuplicateAndReopen(t *testing.T) {
	db := setupTestDB(t)
	movie := seedMovie(t, db, "Inception")
	app := makeSubmitApp("reporter-1")

	fresh := `{"reason":"spam","description":"hi"}`
	resp := do(t, app, http.MethodPost, "/v1/movies/1/report", []byte(fresh))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first report: expected 201, got %d", resp.StatusCode)
	}

	t.Run("a second report while open is 400", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/movies/1/report", []byte(fresh))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("re-reporting a dismissed report reopens it (200)", func(t *testing.T) {
		if err := db.Model(&database.ContentReport{}).
			Where("media_id = ?", movie.ID).
			Updates(map[string]interface{}{
				"status":      reportStatusDismissed,
				"resolved_by": "mod-1",
			}).Error; err != nil {
			t.Fatalf("mark dismissed: %v", err)
		}
		resp := do(t, app, http.MethodPost, "/v1/movies/1/report", []byte(`{"reason":"offensive"}`))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on reopen, got %d", resp.StatusCode)
		}
		got := decodeOne(t, resp)
		if got.Status != reportStatusOpen {
			t.Errorf("Status = %q, want open after reopen", got.Status)
		}
		if got.ResolvedBy != "" {
			t.Errorf("ResolvedBy = %q, want cleared", got.ResolvedBy)
		}
		if got.Reason != "offensive" {
			t.Errorf("Reason = %q, want the new reason", got.Reason)
		}
	})
}
