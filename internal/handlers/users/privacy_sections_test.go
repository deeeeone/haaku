package users

import (
	"net/http"
	"os"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Route-level proof that the per-section flags gate the routes they name.
//
// The app mounts the REAL registrations (users.RegisterRoutes for /users/:id
// and RegisterProfileReadRoutes for the read surface), so the assertions run
// through the wired middleware chain and the real handlers — a 403 means the
// gate denied, a 200 means the handler served the (empty) collection.
// ---------------------------------------------------------------------------

func newPrivacyRouteApp(callerID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	})
	RegisterRoutes(v1) // GET /users/:id, block routes
	RegisterProfileReadRoutes(v1)
	return app
}

func seedUserSettings(t *testing.T, db *gorm.DB, id string, private, admin bool, settings string) {
	t.Helper()
	u := database.User{
		ID:        id,
		Username:  "u-" + id,
		Name:      "User " + id,
		IsPrivate: private,
		IsAdmin:   admin,
		Status:    "active",
		Settings:  settings,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func follow(t *testing.T, db *gorm.DB, follower, target string) {
	t.Helper()
	if err := db.Create(&database.UserFollow{
		FollowerID: follower, FollowingID: target, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed follow %s->%s: %v", follower, target, err)
	}
}

// sectionRoutes maps each flag to the route it must gate.
var sectionRoutes = map[string]string{
	"profile":  "/v1/users/target",
	"history":  "/v1/users/target/history",
	"ratings":  "/v1/users/target/ratings",
	"activity": "/v1/users/target/watching",
}

func TestFlagGatesItsRouteToStrangerOnly(t *testing.T) {
	for section, route := range sectionRoutes {
		db := profileTestDB(t)
		seedUserSettings(t, db, "target", false, false,
			`{"privacy":{"`+section+`":"friends"}}`)
		seedUserSettings(t, db, "follower", false, false, "{}")
		seedUserSettings(t, db, "admin", false, true, "{}")
		follow(t, db, "follower", "target")

		// Stranger (and anonymous): 403.
		for _, caller := range []string{"stranger", ""} {
			status, body := doGET(t, newPrivacyRouteApp(caller), route)
			if status != http.StatusForbidden {
				t.Errorf("%s=%s caller=%q %s: want 403, got %d (%s)",
					section, "friends", caller, route, status, body)
			}
		}
		// Self, follower, admin: 200.
		for _, caller := range []string{"target", "follower", "admin"} {
			status, body := doGET(t, newPrivacyRouteApp(caller), route)
			if status != http.StatusOK {
				t.Errorf("%s=friends caller=%q %s: want 200, got %d (%s)",
					section, caller, route, status, body)
			}
		}
	}
}

func TestPrivateFlagExcludesFollower(t *testing.T) {
	db := profileTestDB(t)
	seedUserSettings(t, db, "target", false, false, `{"privacy":{"history":"private"}}`)
	seedUserSettings(t, db, "follower", false, false, "{}")
	seedUserSettings(t, db, "admin", false, true, "{}")
	follow(t, db, "follower", "target")

	if status, body := doGET(t, newPrivacyRouteApp("follower"), "/v1/users/target/history"); status != http.StatusForbidden {
		t.Errorf("history=private to a follower: want 403, got %d (%s)", status, body)
	}
	for _, caller := range []string{"target", "admin"} {
		if status, body := doGET(t, newPrivacyRouteApp(caller), "/v1/users/target/history"); status != http.StatusOK {
			t.Errorf("history=private to %s: want 200, got %d (%s)", caller, status, body)
		}
	}
}

// Tightening one flag must not hide the sections no flag names, nor the others.
func TestNoGlobalLockOut(t *testing.T) {
	db := profileTestDB(t)
	seedUserSettings(t, db, "target", false, false, `{"privacy":{"history":"private"}}`)
	app := newPrivacyRouteApp("stranger")

	for _, route := range []string{
		"/v1/users/target",                  // profile: unset
		"/v1/users/target/ratings",          // ratings: unset
		"/v1/users/target/watching",         // activity: unset
		"/v1/users/target/collection",       // no flag names this
		"/v1/users/target/watchlist",        // no flag names this
		"/v1/users/target/comments/all/all", // no flag names this
		"/v1/users/target/notes/all",        // no flag names this
		"/v1/users/target/likes/comments",   // no flag names this
		"/v1/users/target/likes/lists",      // no flag names this
	} {
		status, body := doGET(t, app, route)
		if status != http.StatusOK {
			t.Errorf("history=private must not lock out %s: want 200, got %d (%s)", route, status, body)
		}
	}
	if status, body := doGET(t, app, "/v1/users/target/history"); status != http.StatusForbidden {
		t.Errorf("history=private: want 403, got %d (%s)", status, body)
	}
}

// is_private hides every gated route even when the sub-flags say public.
func TestIsPrivateMasterOverrideOnRoutes(t *testing.T) {
	db := profileTestDB(t)
	seedUserSettings(t, db, "target", true, false,
		`{"privacy":{"profile":"public","history":"public","ratings":"public","activity":"public"}}`)
	seedUserSettings(t, db, "follower", false, false, "{}")
	seedUserSettings(t, db, "admin", false, true, "{}")
	follow(t, db, "follower", "target")

	for _, route := range []string{
		"/v1/users/target",
		"/v1/users/target/history",
		"/v1/users/target/ratings",
		"/v1/users/target/watching",
		"/v1/users/target/collection",
	} {
		if status, body := doGET(t, newPrivacyRouteApp("stranger"), route); status != http.StatusForbidden {
			t.Errorf("private profile: %s to a stranger want 403, got %d (%s)", route, status, body)
		}
		for _, caller := range []string{"target", "follower", "admin"} {
			if status, body := doGET(t, newPrivacyRouteApp(caller), route); status != http.StatusOK {
				t.Errorf("private profile: %s to %s want 200, got %d (%s)", route, caller, status, body)
			}
		}
	}
}

// Unset flags (pre-feature rows, users.settings = '{}') behave exactly as before.
func TestUnsetFlagsOnRoutes(t *testing.T) {
	db := profileTestDB(t)
	seedUserSettings(t, db, "target", false, false, "")
	app := newPrivacyRouteApp("stranger")

	for _, route := range []string{
		"/v1/users/target",
		"/v1/users/target/history",
		"/v1/users/target/ratings",
		"/v1/users/target/watching",
	} {
		if status, body := doGET(t, app, route); status != http.StatusOK {
			t.Errorf("unset flag %s: want 200 (today's behaviour), got %d (%s)", route, status, body)
		}
	}
}

// A malformed settings blob must not 500 and must not over-share.
func TestMalformedSettingsOnRoutes(t *testing.T) {
	if os.Getenv("DATABASE_URL") != "" {
		t.Skip("users.settings is jsonb on PostgreSQL — a malformed blob cannot exist at rest " +
			"(the write is rejected with 22P02). Covered at parser level by TestSectionFlagValue.")
	}
	db := profileTestDB(t)
	seedUserSettings(t, db, "target", false, false, "{broken json")
	app := newPrivacyRouteApp("stranger")
	for _, route := range []string{
		"/v1/users/target",
		"/v1/users/target/history",
		"/v1/users/target/ratings",
		"/v1/users/target/watching",
	} {
		if status, body := doGET(t, app, route); status != http.StatusOK {
			t.Errorf("malformed blob %s: want 200, got %d (%s)", route, status, body)
		}
	}

	// Private profile + malformed blob stays hidden (no over-share).
	db2 := profileTestDB(t)
	seedUserSettings(t, db2, "target", true, false, "{broken json")
	if status, body := doGET(t, newPrivacyRouteApp("stranger"), "/v1/users/target/history"); status != http.StatusForbidden {
		t.Errorf("malformed blob + private: want 403, got %d (%s)", status, body)
	}
}
