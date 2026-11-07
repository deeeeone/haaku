package auth

import (
	"net/http"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupModerationTestDB points the database singleton at a fresh in-memory
// sqlite DB seeded with just the users table RequireModerator reads.
func setupModerationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// moderationApp registers a guarded probe behind a stub identity injector so
// the middleware can be exercised without Clerk.
func moderationApp(userID string) *fiber.App {
	app := fiber.New()
	stub := func(c *fiber.Ctx) error {
		if userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	}
	app.Get("/guarded", stub, RequireModerator(), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	return app
}

func getGuarded(t *testing.T, app *fiber.App) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "/guarded", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Host = "example.com"
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestRequireModeratorUnauthenticated(t *testing.T) {
	app := moderationApp("")
	if got := getGuarded(t, app); got != http.StatusUnauthorized {
		t.Errorf("expected 401 for an unauthenticated caller, got %d", got)
	}
}

func TestRequireModeratorEnvAllowlist(t *testing.T) {
	setupModerationTestDB(t)
	InitModerationAllowlists([]string{"admin-x"}, []string{"mod-x"})
	t.Cleanup(func() { InitModerationAllowlists(nil, nil) })

	for _, id := range []string{"admin-x", "mod-x"} {
		if got := getGuarded(t, moderationApp(id)); got != http.StatusNoContent {
			t.Errorf("%s from the env allowlist: expected 204, got %d", id, got)
		}
	}
}

func TestRequireModeratorDBFlag(t *testing.T) {
	db := setupModerationTestDB(t)
	InitModerationAllowlists(nil, nil)
	t.Cleanup(func() { InitModerationAllowlists(nil, nil) })

	db.Create(&database.User{ID: "mod-flag", Username: "mod-flag", IsModerator: true})
	db.Create(&database.User{ID: "admin-flag", Username: "admin-flag", IsAdmin: true})

	for _, id := range []string{"mod-flag", "admin-flag"} {
		if got := getGuarded(t, moderationApp(id)); got != http.StatusNoContent {
			t.Errorf("%s (DB role flag): expected 204, got %d", id, got)
		}
	}
}

func TestRequireModeratorForbidden(t *testing.T) {
	db := setupModerationTestDB(t)
	InitModerationAllowlists(nil, nil)
	t.Cleanup(func() { InitModerationAllowlists(nil, nil) })
	db.Create(&database.User{ID: "plain", Username: "plain"})

	t.Run("known user without a role flag is 403", func(t *testing.T) {
		if got := getGuarded(t, moderationApp("plain")); got != http.StatusForbidden {
			t.Errorf("expected 403, got %d", got)
		}
	})

	t.Run("authenticated principal with no user row is 403", func(t *testing.T) {
		if got := getGuarded(t, moderationApp("ghost")); got != http.StatusForbidden {
			t.Errorf("expected 403, got %d", got)
		}
	})
}
