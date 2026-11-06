package stats

import (
	"io"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Harness: the route is mounted through the REAL RegisterRoutes so the test
// exercises the wired middleware chain, not a copy of it. A stub group
// middleware pins the caller's user_id before PrivacyFilter runs (the stats
// route has no auth.AuthRequired — anonymous callers are allowed for public
// profiles), and an in-memory sqlite DB backs the handlers.
// ---------------------------------------------------------------------------

func statsPrivacyDB(t *testing.T) *gorm.DB {
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
		&database.UserStats{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	for _, table := range []string{"users", "user_follows", "user_blocks", "user_stats"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	database.SetDBForTest(t, db)
	return db
}

func newStatsApp(callerID string) *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if callerID != "" {
			c.Locals("user_id", callerID)
		}
		return c.Next()
	})
	RegisterRoutes(v1)
	return app
}

func statsDoGET(t *testing.T, app *fiber.App, path string) (int, string) {
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

func seedStatsUser(t *testing.T, db *gorm.DB, id string, private, admin bool) {
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

// seedFreshStats writes a user_stats row computed "now" so the handler's 200
// path serves it instead of falling into the recompute branch (which takes a
// pg advisory lock sqlite cannot provide). This exercises the real handler.
func seedFreshStats(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	now := time.Now()
	if err := db.Create(&database.UserStats{
		UserID: userID, Plays: 4, Minutes: 120, RatingsCount: 2, ComputedAt: &now,
	}).Error; err != nil {
		t.Fatalf("seed stats %s: %v", userID, err)
	}
}

// ---------------------------------------------------------------------------
// Visibility
// ---------------------------------------------------------------------------

func TestStatsPrivateProfileForbiddenForStranger(t *testing.T) {
	db := statsPrivacyDB(t)
	seedStatsUser(t, db, "target", true, false)
	seedFreshStats(t, db, "target")

	for _, caller := range []string{"stranger", ""} { // authenticated stranger + anonymous
		status, body := statsDoGET(t, newStatsApp(caller), "/v1/users/target/stats")
		if status != http.StatusForbidden {
			t.Errorf("caller=%q: private stats: want 403, got %d (%s)", caller, status, body)
		}
	}
}

func TestStatsPrivateProfileAllowedForSelfFollowerAdmin(t *testing.T) {
	db := statsPrivacyDB(t)
	seedStatsUser(t, db, "target", true, false)
	seedStatsUser(t, db, "follower", false, false)
	seedStatsUser(t, db, "admin", false, true)
	seedFreshStats(t, db, "target")
	if err := db.Create(&database.UserFollow{
		FollowerID: "follower", FollowingID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed follow: %v", err)
	}

	for _, caller := range []string{"target", "follower", "admin"} {
		status, body := statsDoGET(t, newStatsApp(caller), "/v1/users/target/stats")
		if status != http.StatusOK {
			t.Errorf("caller=%q: private stats: want 200, got %d (%s)", caller, status, body)
		}
	}
}

func TestStatsPublicProfileOkForAnyone(t *testing.T) {
	db := statsPrivacyDB(t)
	seedStatsUser(t, db, "target", false, false)
	seedFreshStats(t, db, "target")

	for _, caller := range []string{"stranger", ""} {
		status, body := statsDoGET(t, newStatsApp(caller), "/v1/users/target/stats")
		if status != http.StatusOK {
			t.Errorf("caller=%q: public stats: want 200, got %d (%s)", caller, status, body)
		}
	}
}

func TestStatsBlockedInEitherDirectionIs404(t *testing.T) {
	db := statsPrivacyDB(t)
	seedStatsUser(t, db, "target", false, false)
	seedStatsUser(t, db, "stranger", false, false)
	seedFreshStats(t, db, "target")

	if err := db.Create(&database.UserBlock{
		BlockerID: "stranger", BlockedID: "target", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed block: %v", err)
	}
	status, body := statsDoGET(t, newStatsApp("stranger"), "/v1/users/target/stats")
	if status != http.StatusNotFound {
		t.Errorf("blocker -> blocked: want 404, got %d (%s)", status, body)
	}

	// Reverse direction: target blocked the stranger.
	if err := db.Create(&database.UserBlock{
		BlockerID: "target", BlockedID: "stranger", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed reverse block: %v", err)
	}
	status, body = statsDoGET(t, newStatsApp("stranger"), "/v1/users/target/stats")
	if status != http.StatusNotFound {
		t.Errorf("blocked -> blocker: want 404, got %d (%s)", status, body)
	}
}

func TestStatsUnknownUserIs404(t *testing.T) {
	statsPrivacyDB(t)
	status, body := statsDoGET(t, newStatsApp("stranger"), "/v1/users/nobody/stats")
	if status != http.StatusNotFound {
		t.Errorf("unknown user: want 404, got %d (%s)", status, body)
	}
}

// ---------------------------------------------------------------------------
// Regression: the wired middleware chain must keep the privacy gate
// ---------------------------------------------------------------------------

func handlerFuncName(h fiber.Handler) string {
	f := runtime.FuncForPC(reflect.ValueOf(h).Pointer())
	if f == nil {
		return ""
	}
	return f.Name()
}

func TestStatsRouteIsPrivacyGated(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))

	var found bool
	for _, r := range app.GetRoutes() {
		if r.Method != http.MethodGet || r.Path != "/v1/users/:id/stats" {
			continue
		}
		found = true
		var names []string
		gated := false
		for _, h := range r.Handlers {
			n := handlerFuncName(h)
			names = append(names, n)
			if strings.Contains(n, "PrivacyFilter") {
				gated = true
			}
		}
		if !gated {
			t.Errorf("GET /v1/users/:id/stats is not privacy-gated — handlers=%v; "+
				"a refactor dropped auth.PrivacyFilter", names)
		}
	}
	if !found {
		t.Fatal("GET /v1/users/:id/stats is not registered at all")
	}
}
