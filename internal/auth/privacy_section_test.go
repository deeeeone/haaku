package auth

import (
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Harness: in-memory sqlite (real PostgreSQL when DATABASE_URL is set) plus a
// route that pins the caller's user_id before PrivacyFilterSection runs and a
// downstream handler that always returns 200. The test therefore isolates the
// gate: a 403 means the middleware denied, a 200 means it let the request through.
// ---------------------------------------------------------------------------

func privacyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if os.Getenv("DATABASE_URL") != "" {
		db, err = gorm.Open(postgres.Open(os.Getenv("DATABASE_URL")), &gorm.Config{})
	} else {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	}
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.AutoMigrate(&database.User{}, &database.UserFollow{}, &database.UserBlock{}); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	for _, table := range []string{"users", "user_follows", "user_blocks"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	database.SetDBForTest(t, db)
	return db
}

func seedPrivacyUser(t *testing.T, db *gorm.DB, id string, private, admin bool, settings string) {
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

func newSectionApp(callerID, section string) *fiber.App {
	app := fiber.New()
	app.Get("/v1/users/:id/x", func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	}, PrivacyFilterSection("id", section), func(c *fiber.Ctx) error {
		return c.Status(http.StatusOK).JSON(fiber.Map{"ok": true})
	})
	return app
}

func doSectionGET(t *testing.T, app *fiber.App, path string) int {
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
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

// ---------------------------------------------------------------------------
// sectionFlagValue — the fail-safe parser
// ---------------------------------------------------------------------------

func TestSectionFlagValue(t *testing.T) {
	cases := []struct {
		name     string
		blob     string
		section  string
		expected string
	}{
		{"empty blob", "", "history", ""},
		{"empty object", "{}", "history", ""},
		{"null blob", "null", "history", ""},
		{"malformed", "not-json", "history", ""},
		{"absent section", `{"privacy":{"profile":"private"}}`, "history", ""},
		{"empty section arg", `{"privacy":{"history":"friends"}}`, "", ""},
		{"public", `{"privacy":{"history":"public"}}`, "history", "public"},
		{"friends", `{"privacy":{"history":"friends"}}`, "history", "friends"},
		{"private", `{"privacy":{"history":"private"}}`, "history", "private"},
		{"wrong type", `{"privacy":{"history":5}}`, "history", ""},
		{"privacy null", `{"privacy":null}`, "history", ""},
		{"privacy not object", `{"privacy":"x"}`, "history", ""},
		{"nested read other key", `{"privacy":{"ratings":"private"}}`, "ratings", "private"},
	}
	for _, tc := range cases {
		if got := sectionFlagValue(tc.blob, tc.section); got != tc.expected {
			t.Errorf("%s: sectionFlagValue(%q, %q) = %q, want %q",
				tc.name, tc.blob, tc.section, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Per-flag matrix: the flagged section is 403 to a stranger (and anonymous),
// 200 to self / a follower / an admin.
// ---------------------------------------------------------------------------

func TestSectionFlagForbiddenForStranger(t *testing.T) {
	for _, section := range []string{"profile", "history", "ratings", "activity"} {
		for _, value := range []string{"friends", "private"} {
			db := privacyTestDB(t)
			seedPrivacyUser(t, db, "target", false, false,
				`{"privacy":{"`+section+`":"`+value+`"}}`)

			for _, caller := range []string{"stranger", ""} { // authenticated stranger + anonymous
				status := doSectionGET(t, newSectionApp(caller, section), "/v1/users/target/x")
				if status != http.StatusForbidden {
					t.Errorf("%s=%s caller=%q: want 403, got %d", section, value, caller, status)
				}
			}
		}
	}
}

func TestSectionFlagAllowedForSelfFollowerAdmin(t *testing.T) {
	for _, section := range []string{"profile", "history", "ratings", "activity"} {
		db := privacyTestDB(t)
		seedPrivacyUser(t, db, "target", false, false,
			`{"privacy":{"`+section+`":"friends"}}`)
		seedPrivacyUser(t, db, "follower", false, false, "{}")
		seedPrivacyUser(t, db, "admin", false, true, "{}")
		if err := db.Create(&database.UserFollow{
			FollowerID: "follower", FollowingID: "target", CreatedAt: time.Now(),
		}).Error; err != nil {
			t.Fatalf("seed follow: %v", err)
		}

		for _, caller := range []string{"target", "follower", "admin"} {
			status := doSectionGET(t, newSectionApp(caller, section), "/v1/users/target/x")
			if status != http.StatusOK {
				t.Errorf("%s=friends caller=%q: want 200, got %d", section, caller, status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// No accidental global lock-out: tightening one flag must not hide the others.
// ---------------------------------------------------------------------------

func TestTighteningOneSectionDoesNotHideOthers(t *testing.T) {
	db := privacyTestDB(t)
	// target tightened history only; profile/ratings/activity left unset.
	seedPrivacyUser(t, db, "target", false, false, `{"privacy":{"history":"private"}}`)
	app := newSectionApp("stranger", "")

	for _, section := range []string{"profile", "ratings", "activity"} {
		// The middleware is mounted per-route; simulate the other route by
		// building an app for that section against the same target.
		status := doSectionGET(t, newSectionApp("stranger", section), "/v1/users/target/x")
		if status != http.StatusOK {
			t.Errorf("history=private must not hide the %s section: want 200, got %d", section, status)
		}
	}
	// And the collection-style routes (section="") stay public too.
	status := doSectionGET(t, app, "/v1/users/target/x")
	if status != http.StatusOK {
		t.Errorf("no-section route must stay public: want 200, got %d", status)
	}
	// The tightened one is still hidden from the stranger.
	if status := doSectionGET(t, newSectionApp("stranger", "history"), "/v1/users/target/x"); status != http.StatusForbidden {
		t.Errorf("history=private: want 403, got %d", status)
	}
}

// ---------------------------------------------------------------------------
// is_private is the master override: it hides everything even when the
// sub-flags say public.
// ---------------------------------------------------------------------------

func TestIsPrivateOverridesPublicSubFlags(t *testing.T) {
	db := privacyTestDB(t)
	seedPrivacyUser(t, db, "target", true, false,
		`{"privacy":{"profile":"public","history":"public","ratings":"public","activity":"public"}}`)
	seedPrivacyUser(t, db, "follower", false, false, "{}")
	seedPrivacyUser(t, db, "admin", false, true, "{}")
	if err := db.Create(&database.UserFollow{
		FollowerID: "follower", FollowingID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed follow: %v", err)
	}

	for _, section := range []string{"", "profile", "history", "ratings", "activity"} {
		if status := doSectionGET(t, newSectionApp("stranger", section), "/v1/users/target/x"); status != http.StatusForbidden {
			t.Errorf("private + %s=public, stranger: want 403, got %d", section, status)
		}
		if status := doSectionGET(t, newSectionApp("", section), "/v1/users/target/x"); status != http.StatusForbidden {
			t.Errorf("private + %s=public, anonymous: want 403, got %d", section, status)
		}
		for _, caller := range []string{"target", "follower", "admin"} {
			if status := doSectionGET(t, newSectionApp(caller, section), "/v1/users/target/x"); status != http.StatusOK {
				t.Errorf("private + %s=public, %s: want 200, got %d", section, caller, status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Unset / default: behaves exactly as before (visible to everyone).
// ---------------------------------------------------------------------------

func TestUnsetFlagsPreservePreviousBehaviour(t *testing.T) {
	db := privacyTestDB(t)
	seedPrivacyUser(t, db, "target", false, false, "") // users.settings default is '{}'

	for _, section := range []string{"", "profile", "history", "ratings", "activity"} {
		for _, caller := range []string{"stranger", ""} {
			if status := doSectionGET(t, newSectionApp(caller, section), "/v1/users/target/x"); status != http.StatusOK {
				t.Errorf("unset %s caller=%q: want 200, got %d", section, caller, status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Malformed blob: never 500, never over-share.
// ---------------------------------------------------------------------------

func TestMalformedSettingsFailsSafe(t *testing.T) {
	if os.Getenv("DATABASE_URL") != "" {
		t.Skip("users.settings is jsonb on PostgreSQL — it cannot hold a malformed blob " +
			"(the write is rejected with 22P02), so this at-rest scenario is unreachable there. " +
			"The fail-safe parser path is covered by TestSectionFlagValue on every driver, and the " +
			"is_private override by TestIsPrivateOverridesPublicSubFlags.")
	}
	// Public profile + malformed blob → today's default (200, no 500).
	db := privacyTestDB(t)
	seedPrivacyUser(t, db, "target", false, false, "{not valid json")
	for _, section := range []string{"profile", "history", "ratings", "activity"} {
		if status := doSectionGET(t, newSectionApp("stranger", section), "/v1/users/target/x"); status != http.StatusOK {
			t.Errorf("malformed blob, %s: want 200 (no 500, today's default), got %d", section, status)
		}
	}

	// Private profile + malformed blob → still hidden (no over-share).
	db2 := privacyTestDB(t)
	seedPrivacyUser(t, db2, "target", true, false, "{not valid json")
	if status := doSectionGET(t, newSectionApp("stranger", "history"), "/v1/users/target/x"); status != http.StatusForbidden {
		t.Errorf("malformed blob + private profile: want 403 (no over-share), got %d", status)
	}
}

// ---------------------------------------------------------------------------
// The empty-section gate (PrivacyFilter) ignores the sub-flags entirely, so the
// routes no flag names keep their pre-feature behaviour.
// ---------------------------------------------------------------------------

func TestEmptySectionIgnoresSubFlags(t *testing.T) {
	db := privacyTestDB(t)
	seedPrivacyUser(t, db, "target", false, false,
		`{"privacy":{"profile":"private","history":"private","ratings":"private","activity":"private"}}`)
	// PrivacyFilter("id") == PrivacyFilterSection("id", "").
	if status := doSectionGET(t, newSectionApp("stranger", ""), "/v1/users/target/x"); status != http.StatusOK {
		t.Errorf("empty section must ignore sub-flags: want 200, got %d", status)
	}
}
