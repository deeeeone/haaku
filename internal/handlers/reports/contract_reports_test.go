package reports

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupContractDB migrates the tables the contract report routes resolve
// against and points the DB singleton at it.
func setupContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Movie{}, &database.Show{}, &database.Season{}, &database.Episode{},
		&database.Person{}, &database.ContentReport{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// makeContractReportApp registers the contract routes behind a stub that injects
// the reporter id (bypassing auth), so the write paths can be exercised.
func makeContractReportApp(userID string) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	stub := func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	}
	v1 := app.Group("/v1")
	v1.Post("/people/:id/report", stub, handleContractPersonReport)
	v1.Post("/shows/:id/seasons/:season/report", stub, handleContractSeasonReport)
	v1.Post("/shows/:id/seasons/:season/episodes/:episode/report", stub, handleContractEpisodeReport)
	return app
}

func seedContractTargets(t *testing.T, db *gorm.DB) (show database.Show, season database.Season, episode database.Episode, person database.Person) {
	t.Helper()
	now := time.Now().UTC()
	show = database.Show{TmdbID: 1396, Title: "Breaking Bad", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&show).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	season = database.Season{ShowID: show.ID, SeasonNumber: 1, EpisodeCount: 7, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&season).Error; err != nil {
		t.Fatalf("seed season: %v", err)
	}
	episode = database.Episode{ShowID: show.ID, SeasonID: season.ID, EpisodeNumber: 1, Title: "Pilot", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&episode).Error; err != nil {
		t.Fatalf("seed episode: %v", err)
	}
	person = database.Person{TmdbID: 17419, Name: "Bryan Cranston", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("seed person: %v", err)
	}
	return show, season, episode, person
}

func TestContractReports(t *testing.T) {
	db := setupContractDB(t)
	show, _, _, person := seedContractTargets(t, db)
	app := makeContractReportApp("reporter-1")

	t.Run("person report creates a row (201)", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/people/%d/report", person.ID), []byte(`{"reason":"metadata","message":"wrong birth date"}`))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
		var stored database.ContentReport
		if err := db.Where("reporter_id = ? AND media_type = ?", "reporter-1", "person").First(&stored).Error; err != nil {
			t.Fatalf("row not stored: %v", err)
		}
		if stored.Reason != "metadata" || stored.Description != "wrong birth date" {
			t.Errorf("stored report = %+v", stored)
		}
	})

	t.Run("a duplicate report is 409", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/people/%d/report", person.ID), []byte(`{"reason":"other"}`))
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("an invalid reason is 400", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/people/%d/report", person.ID), []byte(`{"reason":"bogus"}`))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("a missing target is 404", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, "/v1/people/999999/report", []byte(`{"reason":"metadata"}`))
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("season report resolves show+season (201)", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/shows/%d/seasons/1/report", show.ID), []byte(`{"reason":"runtime"}`))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
		var stored database.ContentReport
		if err := db.Where("reporter_id = ? AND media_type = ?", "reporter-1", "season").First(&stored).Error; err != nil {
			t.Fatalf("season row not stored: %v", err)
		}
		if stored.MediaID == 0 {
			t.Error("season report stored with media_id 0")
		}
	})

	t.Run("episode report resolves show+season+episode (201)", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/shows/%d/seasons/1/episodes/1/report", show.ID), []byte(`{"reason":"metadata"}`))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("an invalid season number is 400", func(t *testing.T) {
		resp := do(t, app, http.MethodPost, pathf("/v1/shows/%d/seasons/abc/report", show.ID), []byte(`{"reason":"metadata"}`))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

// TestContractReportsRateLimited proves the report-specific per-user window is
// enforced on the contract routes: once past the limit the next submission is a
// 429, before any target lookup or database work. The limiter is substituted
// because the Redis-backed one fails open without Redis (the sqlite suite).
func TestContractReportsRateLimited(t *testing.T) {
	db := setupContractDB(t)
	_, _, _, person := seedContractTargets(t, db)
	app := makeContractReportApp("reporter-rl")

	orig := reportRateCheck
	t.Cleanup(func() { reportRateCheck = orig })
	var calls int
	reportRateCheck = func(c *fiber.Ctx, userID string) error {
		calls++
		if calls > 1 {
			c.Set("Retry-After", "60")
			_ = response.WriteError(c, fiber.StatusTooManyRequests, "rate_limited", "too many reports — slow down")
			return response.ErrHandled
		}
		return nil
	}

	first := do(t, app, http.MethodPost, pathf("/v1/people/%d/report", person.ID), []byte(`{"reason":"metadata"}`))
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first submission: expected 201, got %d", first.StatusCode)
	}
	// The second submission hits the same target. A 429 (not the 409 a duplicate
	// would otherwise produce) shows the limit is applied before the duplicate
	// check and the insert.
	second := do(t, app, http.MethodPost, pathf("/v1/people/%d/report", person.ID), []byte(`{"reason":"metadata"}`))
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second submission past the limit: expected 429, got %d", second.StatusCode)
	}
	if second.Header.Get("Retry-After") == "" {
		t.Error("429 response is missing Retry-After")
	}
}

func TestContractReportsRequireAuth(t *testing.T) {
	setupContractDB(t)
	app := fiber.New(fiber.Config{ErrorHandler: response.HandleError})
	registerContractReportRoutes(app.Group("/v1"))
	for _, path := range []string{"/v1/people/1/report", "/v1/shows/1/seasons/1/report", "/v1/shows/1/seasons/1/episodes/1/report"} {
		resp := do(t, app, http.MethodPost, path, []byte(`{"reason":"metadata"}`))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a token: expected 401, got %d", path, resp.StatusCode)
		}
	}
}

func pathf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
