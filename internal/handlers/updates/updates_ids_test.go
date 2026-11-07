package updates

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupIDsDB opens an in-memory sqlite DB with the movies/shows tables the
// updated-ID feeds read, and points the database singleton at it.
func setupIDsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.Movie{}, &database.Show{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// seedUpdated creates one recently-updated and one long-stale movie and show,
// pinning updated_at directly (GORM's autoUpdateTime overwrites provided
// timestamps on create, so the values are forced with UpdateColumn).
func seedUpdated(t *testing.T, db *gorm.DB) (recentMovie, oldMovie, recentShow, oldShow int64) {
	t.Helper()
	now := time.Now().UTC()
	recent := now.Add(-2 * time.Hour)
	old := now.AddDate(0, 0, -10)

	rm := database.Movie{TmdbID: 201, Title: "Recent Movie"}
	om := database.Movie{TmdbID: 202, Title: "Old Movie"}
	rs := database.Show{TmdbID: 301, Title: "Recent Show"}
	os := database.Show{TmdbID: 302, Title: "Old Show"}
	for _, v := range []interface{}{&rm, &om, &rs, &os} {
		if err := db.Create(v).Error; err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}
	db.Model(&database.Movie{}).Where("id = ?", rm.ID).UpdateColumn("updated_at", recent)
	db.Model(&database.Movie{}).Where("id = ?", om.ID).UpdateColumn("updated_at", old)
	db.Model(&database.Show{}).Where("id = ?", rs.ID).UpdateColumn("updated_at", recent)
	db.Model(&database.Show{}).Where("id = ?", os.ID).UpdateColumn("updated_at", old)

	return rm.ID, om.ID, rs.ID, os.ID
}

func idsApp() *fiber.App {
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))
	return app
}

func doIDsGet(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s failed: %v", path, err)
	}
	return resp
}

func decodeIDs(t *testing.T, resp *http.Response) []int64 {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []int64
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode id body: %v", err)
	}
	return out
}

func TestUpdatedIDFeeds(t *testing.T) {
	db := setupIDsDB(t)
	recentMovie, oldMovie, recentShow, oldShow := seedUpdated(t, db)
	app := idsApp()
	since := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	t.Run("movies id returns only recently updated ids", func(t *testing.T) {
		resp := doIDsGet(t, app, "/v1/movies/updates/id/"+since)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeIDs(t, resp)
		if len(got) != 1 || got[0] != recentMovie {
			t.Fatalf("expected [%d], got %v", recentMovie, got)
		}
		for _, id := range got {
			if id == oldMovie {
				t.Errorf("stale movie leaked into feed: %v", got)
			}
		}
	})

	t.Run("shows id returns only recently updated ids", func(t *testing.T) {
		resp := doIDsGet(t, app, "/v1/shows/updates/id/"+since)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeIDs(t, resp)
		if len(got) != 1 || got[0] != recentShow {
			t.Fatalf("expected [%d], got %v", recentShow, got)
		}
		if got[0] == oldShow {
			t.Errorf("stale show leaked into feed: %v", got)
		}
	})

	t.Run("empty window returns [] not null", func(t *testing.T) {
		future := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")
		resp := doIDsGet(t, app, "/v1/movies/updates/id/"+future)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		got := decodeIDs(t, resp)
		if got == nil || len(got) != 0 {
			t.Errorf("expected empty non-nil slice, got %#v", got)
		}
	})

	t.Run("invalid start_date is 400", func(t *testing.T) {
		resp := doIDsGet(t, app, "/v1/shows/updates/id/not-a-date")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})
}
