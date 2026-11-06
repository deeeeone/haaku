package lists

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Harness
//
// The list route is registered as auth.AuthRequired() then
// auth.PrivacyFilter("id") — the profile gate is in addition to the auth gate.
// auth.AuthRequired parses a live Clerk/API-key token and cannot be satisfied
// offline, so the behavioural tests mount the same PrivacyFilter gate over a
// stub that pins the caller's user_id, and TestUserListsRouteIsPrivacyGated
// inspects the REAL registered chain (regression). Together they prove both
// the semantics and the wiring.
// ---------------------------------------------------------------------------

func listsPrivacyDB(t *testing.T) *gorm.DB {
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
	if err := db.AutoMigrate(
		&database.User{},
		&database.UserFollow{},
		&database.UserBlock{},
		&database.List{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	for _, table := range []string{"users", "user_follows", "user_blocks", "lists"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	database.SetDBForTest(t, db)
	return db
}

func newUserListsApp(callerID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	})
	// Same middleware the real route uses (minus the offline-unsatisfiable
	// AuthRequired), then the real handler.
	v1.Get("/users/:id/lists", auth.PrivacyFilter("id"), handleGetUserLists)
	return app
}

func listsDoGET(t *testing.T, app *fiber.App, path string) (int, string) {
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
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(raw)
}

func seedListsUser(t *testing.T, db *gorm.DB, id string, private, admin bool) {
	t.Helper()
	u := database.User{
		ID:        id,
		Username:  "u-" + id,
		Name:      "User " + id,
		IsPrivate: private,
		IsAdmin:   admin,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}

func seedList(t *testing.T, db *gorm.DB, owner, slug, privacy string) {
	t.Helper()
	if err := db.Create(&database.List{
		UserID: owner, Name: slug, Slug: slug, Privacy: privacy, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed list %s: %v", slug, err)
	}
}

// ---------------------------------------------------------------------------
// Visibility
// ---------------------------------------------------------------------------

func TestUserListsPrivateProfileForbiddenForStranger(t *testing.T) {
	db := listsPrivacyDB(t)
	seedListsUser(t, db, "target", true, false)
	seedList(t, db, "target", "public-list", "public")
	seedList(t, db, "target", "private-list", "private")

	status, body := listsDoGET(t, newUserListsApp("stranger"), "/v1/users/target/lists")
	if status != http.StatusForbidden {
		t.Errorf("private lists to stranger: want 403, got %d (%s)", status, body)
	}
	if strings.Contains(body, "public-list") || strings.Contains(body, "private-list") {
		t.Errorf("private profile leaked list data to a stranger: %s", body)
	}
}

func TestUserListsPrivateProfileAllowedForSelfFollowerAdmin(t *testing.T) {
	db := listsPrivacyDB(t)
	seedListsUser(t, db, "target", true, false)
	seedListsUser(t, db, "follower", false, false)
	seedListsUser(t, db, "admin", false, true)
	seedList(t, db, "target", "public-list", "public")
	if err := db.Create(&database.UserFollow{
		FollowerID: "follower", FollowingID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed follow: %v", err)
	}

	for _, caller := range []string{"target", "follower", "admin"} {
		status, body := listsDoGET(t, newUserListsApp(caller), "/v1/users/target/lists")
		if status != http.StatusOK {
			t.Errorf("caller=%q: private lists: want 200, got %d (%s)", caller, status, body)
		}
	}
}

func TestUserListsPublicProfileStrangerSeesOnlyVisibleLists(t *testing.T) {
	db := listsPrivacyDB(t)
	seedListsUser(t, db, "target", false, false)
	seedList(t, db, "target", "public-list", "public")
	seedList(t, db, "target", "link-list", "link_only")
	seedList(t, db, "target", "private-list", "private")

	status, body := listsDoGET(t, newUserListsApp("stranger"), "/v1/users/target/lists")
	if status != http.StatusOK {
		t.Fatalf("public lists: want 200, got %d (%s)", status, body)
	}
	if strings.Contains(body, "private-list") {
		t.Errorf("private list leaked to a stranger: %s", body)
	}
	if !strings.Contains(body, "public-list") || !strings.Contains(body, "link-list") {
		t.Errorf("public/link_only lists missing: %s", body)
	}
}

func TestUserListsBlockedInEitherDirectionIs404(t *testing.T) {
	db := listsPrivacyDB(t)
	seedListsUser(t, db, "target", false, false)
	seedListsUser(t, db, "stranger", false, false)

	if err := db.Create(&database.UserBlock{
		BlockerID: "stranger", BlockedID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed block: %v", err)
	}
	status, body := listsDoGET(t, newUserListsApp("stranger"), "/v1/users/target/lists")
	if status != http.StatusNotFound {
		t.Errorf("blocker -> blocked: want 404, got %d (%s)", status, body)
	}

	if err := db.Create(&database.UserBlock{
		BlockerID: "target", BlockedID: "stranger", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed reverse block: %v", err)
	}
	status, body = listsDoGET(t, newUserListsApp("stranger"), "/v1/users/target/lists")
	if status != http.StatusNotFound {
		t.Errorf("blocked -> blocker: want 404, got %d (%s)", status, body)
	}
}

func TestUserListsUnknownUserIs404(t *testing.T) {
	listsPrivacyDB(t)
	status, body := listsDoGET(t, newUserListsApp("stranger"), "/v1/users/nobody/lists")
	if status != http.StatusNotFound {
		t.Errorf("unknown user: want 404, got %d (%s)", status, body)
	}
}

// ---------------------------------------------------------------------------
// Regression: the wired middleware chain must keep the privacy gate
// ---------------------------------------------------------------------------

func listsHandlerFuncName(h fiber.Handler) string {
	f := runtime.FuncForPC(reflect.ValueOf(h).Pointer())
	if f == nil {
		return ""
	}
	return f.Name()
}

func TestUserListsRouteIsPrivacyGated(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))

	var found bool
	for _, r := range app.GetRoutes() {
		if r.Method != http.MethodGet || r.Path != "/v1/users/:id/lists" {
			continue
		}
		found = true
		var names []string
		authIdx, privacyIdx := -1, -1
		for i, h := range r.Handlers {
			n := listsHandlerFuncName(h)
			names = append(names, n)
			if authIdx < 0 && strings.Contains(n, "AuthRequired") {
				authIdx = i
			}
			if privacyIdx < 0 && strings.Contains(n, "PrivacyFilter") {
				privacyIdx = i
			}
		}
		if privacyIdx < 0 {
			t.Errorf("GET /v1/users/:id/lists is not privacy-gated — handlers=%v; "+
				"a refactor dropped auth.PrivacyFilter", names)
		}
		if authIdx < 0 {
			t.Errorf("GET /v1/users/:id/lists lost auth.AuthRequired — handlers=%v", names)
		}
		if authIdx >= 0 && privacyIdx >= 0 && authIdx > privacyIdx {
			t.Errorf("GET /v1/users/:id/lists: AuthRequired must precede PrivacyFilter — handlers=%v", names)
		}
	}
	if !found {
		t.Fatal("GET /v1/users/:id/lists is not registered at all")
	}
}
