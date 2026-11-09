package comments

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestListMediaCommentsSortGrid proves the media-scoped {sort} segment (the one
// the contract exposes on season and episode comments) really orders the rows,
// and that a value it cannot serve answers 400 naming the accepted set instead
// of silently defaulting.
func TestListMediaCommentsSortGrid(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Comment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seasonID := int64(7)
	seed := []database.Comment{
		{ID: 1, UserID: "u1", MediaType: "season", MediaID: &seasonID, Body: "old", CreatedAt: base},
		{ID: 2, UserID: "u2", MediaType: "season", MediaID: &seasonID, Body: "new", CreatedAt: base.Add(time.Hour)},
		{ID: 3, UserID: "u3", MediaType: "season", MediaID: &seasonID, Body: "liked", CreatedAt: base.Add(2 * time.Hour), LikeCount: 9},
	}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	app := fiber.New()
	app.Get("/comments/:sort", func(c *fiber.Ctx) error {
		return ListMediaComments(c, db, "season", 7, c.Params("sort"))
	})

	order := func(t *testing.T, sort string) []string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "/comments/"+sort, nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request %s: %v", sort, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sort %s: status %d", sort, resp.StatusCode)
		}
		var items []struct {
			Comment string `json:"body"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			t.Fatalf("decode %s: %v", sort, err)
		}
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it.Comment)
		}
		return out
	}

	newest := order(t, "newest")
	if len(newest) == 0 || newest[0] != "liked" {
		t.Errorf("newest order = %v, want the latest comment first", newest)
	}
	oldest := order(t, "oldest")
	if len(oldest) == 0 || oldest[0] != "old" {
		t.Errorf("oldest order = %v, want the earliest comment first", oldest)
	}

	// "highest"/"lowest"/"plays" are declared by the contract but not computed
	// from stored rows, so they must 400 and name what is served.
	req, _ := http.NewRequest(http.MethodGet, "/comments/highest", nil)
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("highest: status %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "newest") {
		t.Errorf("400 body does not name accepted sorts: %s", raw)
	}
}
