package syncs

import (
	"net/http"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
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
		Errors:         database.JSONMap{},
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
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("valid id status = %d, want 200", resp.StatusCode)
	}

	// Non-numeric id → 400.
	resp2, err := app.Test(getReq("/sync/abc"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("non-numeric id status = %d, want 400", resp2.StatusCode)
	}

	// Zero id → 400.
	resp3, err := app.Test(getReq("/sync/0"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Errorf("zero id status = %d, want 400", resp3.StatusCode)
	}
}
