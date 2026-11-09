package watchnow

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWatchNowDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.WatchNowSource{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

func makeWatchNowApp() *fiber.App {
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))
	return app
}

func decodeRecords(t *testing.T, resp *http.Response) []map[string][]map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var out []map[string][]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func seedCatalogue(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []database.WatchNowSource{
		{MediaType: "movie", ProviderID: 8, Source: "netflix", Name: "Netflix", Country: "us", Priority: 1, LogoPath: "/netflix.jpg"},
		{MediaType: "tv", ProviderID: 8, Source: "netflix", Name: "Netflix", Country: "us", Priority: 1, LogoPath: "/netflix.jpg"},
		{MediaType: "movie", ProviderID: 9, Source: "amazon-prime-video", Name: "Amazon Prime Video", Country: "us", Priority: 2},
		{MediaType: "movie", ProviderID: 8, Source: "netflix", Name: "Netflix", Country: "gb", Priority: 3, LogoPath: "/netflix.jpg"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed catalogue: %v", err)
	}
}

func TestWatchNowSources(t *testing.T) {
	db := setupWatchNowDB(t)
	seedCatalogue(t, db)
	app := makeWatchNowApp()

	t.Run("one record keyed by lower-case country, providers deduped and priority-ordered", func(t *testing.T) {
		records := decodeRecords(t, func() *http.Response {
			req, _ := http.NewRequest(http.MethodGet, "/v1/watchnow/sources", nil)
			req.Host = "example.com"
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			return resp
		}())
		if len(records) != 1 {
			t.Fatalf("expected exactly one record, got %d", len(records))
		}
		if len(records[0]["us"]) != 2 {
			t.Fatalf("expected US to list 2 providers (movie+tv netflix deduped), got %+v", records[0]["us"])
		}
		first := records[0]["us"][0]
		if first["source"] != "netflix" || first["name"] != "Netflix" {
			t.Errorf("unexpected first provider: %+v", first)
		}
		if first["type"] != nil || first["free"] != false || first["cinema"] != false ||
			first["amazon"] != false || first["color"] != "" || first["link_count"] != float64(0) {
			t.Errorf("neutral fields wrong: %+v", first)
		}
		images, ok := first["images"].(map[string]any)
		if !ok {
			t.Fatalf("images missing: %+v", first)
		}
		if images["logo"] != "https://image.tmdb.org/t/p/w500/netflix.jpg" || images["channel"] != nil {
			t.Errorf("images wrong: %+v", images)
		}
		second := records[0]["us"][1]
		if second["amazon"] != true || second["source"] != "amazon-prime-video" {
			t.Errorf("amazon flag/slug wrong: %+v", second)
		}
		if second["images"].(map[string]any)["logo"] != nil {
			t.Errorf("a provider with no logo path must not invent a URL: %+v", second)
		}
	})

	t.Run("country path returns only that country", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/watchnow/sources/GB", nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		records := decodeRecords(t, resp)
		if len(records) != 1 {
			t.Fatalf("expected one record, got %d", len(records))
		}
		if _, ok := records[0]["gb"]; !ok {
			t.Fatalf("expected gb key, got %+v", records[0])
		}
		if _, ok := records[0]["us"]; ok {
			t.Fatalf("country route leaked another country: %+v", records[0])
		}
	})

	t.Run("unknown country is an empty array, not a 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/watchnow/sources/zz", nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		records := decodeRecords(t, resp)
		if len(records) != 0 {
			t.Fatalf("expected [], got %+v", records)
		}
	})
}
