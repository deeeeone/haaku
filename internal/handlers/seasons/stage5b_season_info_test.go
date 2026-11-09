package seasons

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newSeasonInfoTestApp(t *testing.T) (*fiber.App, *gorm.DB) {
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

	if err := db.AutoMigrate(database.MediaModels()...); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	database.SetDBForTest(t, db)

	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	RegisterRoutes(app.Group("/v1"))
	return app, db
}

func seasonInfoGET(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), -1)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, raw
}

func TestSeasonInfoContractShape(t *testing.T) {
	app, db := newSeasonInfoTestApp(t)
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad", Slug: "breaking-bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{
		ID:           1,
		ShowID:       1,
		SeasonNumber: 1,
		EpisodeCount: 8,
		AiredAt:      &database.Date{Time: time.Date(2008, 1, 20, 0, 0, 0, 0, time.UTC)},
	}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}

	status, raw := seasonInfoGET(t, app, "/v1/shows/1/seasons/1/info")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode body: %v (%s)", err, raw)
	}
	if got["number"] != float64(1) || got["episode_count"] != float64(8) || got["aired_episodes"] != float64(8) {
		t.Fatalf("season fields = %v", got)
	}
	if got["first_aired"] != "2008-01-20" {
		t.Fatalf("first_aired = %v", got["first_aired"])
	}
	ids, ok := got["ids"].(map[string]any)
	if !ok {
		t.Fatalf("ids = %v", got["ids"])
	}
	if ids["trakt"] != float64(1) {
		t.Fatalf("ids.trakt = %#v", ids["trakt"])
	}
	images, ok := got["images"].(map[string]any)
	if !ok {
		t.Fatalf("images = %v", got["images"])
	}
	if _, ok := images["poster"].([]any); !ok {
		t.Fatalf("images.poster = %#v, want an array", images["poster"])
	}
}

func TestSeasonInfoUnknownIDsAreNotFound(t *testing.T) {
	app, db := newSeasonInfoTestApp(t)
	if err := db.Create(&database.Show{ID: 1, TmdbID: 1396, Title: "Breaking Bad"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	if err := db.Create(&database.Season{ID: 1, ShowID: 1, SeasonNumber: 1, EpisodeCount: 8}).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}

	for _, path := range []string{
		"/v1/shows/999/seasons/1/info",
		"/v1/shows/1/seasons/9/info",
	} {
		if status, raw := seasonInfoGET(t, app, path); status != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 (%s)", path, status, raw)
		}
	}
}
