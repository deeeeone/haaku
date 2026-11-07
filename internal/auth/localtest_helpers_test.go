package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Shared harness for the local account module tests (issue #53).
//
// Every test runs against an in-memory sqlite handle injected through
// database.SetDBForTest, with the real routes registered on a Fiber app — so
// the requests go through the same handlers, the same middleware and the same
// envelopes the server does.

// testRootKey is the root key for this test binary. It is assembled by
// repetition rather than written as a literal so nothing that looks like a
// credential is ever committed: gitleaks scans history, and a "leak" in a
// test file fails the security gate just as loudly as a real one.
var testRootKey = strings.Repeat("haaku-test-key-", 4) // 60 chars, over MinMasterKeyLen

// setupLocalAuth returns a migrated DB and an app with the local module
// registered in local/approval mode (the documented default).
func setupLocalAuth(t *testing.T) (*gorm.DB, *fiber.App) {
	t.Helper()
	t.Setenv("HAAKU_SECRET_KEY", testRootKey)
	if err := InitAuthMode("", ""); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	t.Cleanup(func() {
		if err := InitAuthMode("", ""); err != nil {
			t.Fatalf("restore auth mode: %v", err)
		}
		InitModerationAllowlists(nil, nil)
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.User{},
		&database.LocalCredential{},
		&database.LocalSession{},
		&database.LocalAuthToken{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	database.SetDBForTest(t, db)

	app := fiber.New()
	api := app.Group("/v1")
	RegisterLocalAuthConfig(api)
	RegisterLocalAuthRoutes(api)
	return db, app
}

type httpResult struct {
	status int
	body   map[string]any
	raw    string
}

// call performs one request against the app with an optional bearer token.
func call(t *testing.T, app *fiber.App, method, path, token, body string) httpResult {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "203.0.113.7:4242"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := httpResult{status: resp.StatusCode, raw: string(raw)}
	var parsed map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parsed)
	}
	out.body = parsed
	return out
}

// errorCode digs the envelope code out of a response body.
func errorCode(t *testing.T, r httpResult) string {
	t.Helper()
	errObj, ok := r.body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope in %q (status %d)", r.raw, r.status)
	}
	code, _ := errObj["code"].(string)
	return code
}

// signup performs a signup and returns the parsed body.
func signup(t *testing.T, app *fiber.App, email, password string) httpResult {
	t.Helper()
	return call(t, app, "POST", "/v1/auth/local/signup", "",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}

// signupToken ensures an owner with this email exists (via the production
// bootstrap path, the way the server creates it at startup) and returns a live
// session token for it. Issue #53 follow-up removed "whoever signs up first
// becomes the owner", so a test that needs the owner now seeds it the same way
// a real instance does, then logs in.
func signupToken(t *testing.T, app *fiber.App, email string) string {
	t.Helper()
	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("get test db: %v", err)
	}
	if err := BootstrapOwner(db, email, "correct-horse-battery"); err != nil {
		t.Fatalf("bootstrap owner %s: %v", email, err)
	}
	return loginToken(t, app, email)
}

// seedOwner creates the owner row the way the server does at startup (via
// auth.BootstrapOwner), so a test instance looks like a real running one, which
// always has an owner before it serves. It fails the test on error.
func seedOwner(t *testing.T, email string) *database.User {
	t.Helper()
	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("get test db: %v", err)
	}
	if err := BootstrapOwner(db, email, "correct-horse-battery"); err != nil {
		t.Fatalf("bootstrap owner %s: %v", email, err)
	}
	var u database.User
	if err := db.First(&u, "email = ?", email).Error; err != nil {
		t.Fatalf("seeded owner %s: %v", email, err)
	}
	return &u
}

// loginToken logs an account in and returns its session token.
func loginToken(t *testing.T, app *fiber.App, email string) string {
	t.Helper()
	r := login(t, app, email, "correct-horse-battery")
	if r.status != 200 {
		t.Fatalf("login %s: status %d (%s)", email, r.status, r.raw)
	}
	sess, _ := r.body["session"].(map[string]any)
	token, _ := sess["token"].(string)
	if token == "" {
		t.Fatalf("login %s returned no session token: %s", email, r.raw)
	}
	return token
}

// login performs a login and returns the parsed body.
func login(t *testing.T, app *fiber.App, email, password string) httpResult {
	t.Helper()
	return call(t, app, "POST", "/v1/auth/local/login", "",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
}
