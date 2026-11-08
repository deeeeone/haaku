package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The "me" alias is part of the contract of every user-scoped route: a client
// that knows only its own session can ask for /v1/users/me/<segment> instead of
// threading its own id through the UI. It is honoured by resolving the ":id"
// path parameter through auth.ResolveTargetID, which returns the caller's id
// for "me" and the parameter verbatim otherwise.
//
// A handler that reads c.Params("id") directly instead looks up a user whose id
// literally is "me", finds nothing, and answers
// `{"error":{"code":"not_found","message":"user not found"}}` to a signed-in
// caller asking for their OWN data — which reads exactly like "your account was
// never created". That is how every /v1/users/me/{followers,following} request
// behaved for a signed-in local account, and the profile page calls both.
//
// This is the class guard: it walks the REAL registered routes, so a new
// /v1/users/:id/... route that forgets the alias fails here instead of in
// someone's browser. Reading the response body is deliberate — the defect is in
// the *answer* to a correct request, so nothing short of a request can see it.

// meAliasUserID is the caller the sweep signs in as.
const meAliasUserID = "local_mealias000000000000000000"

// userNotFoundMessage is the envelope message that means "the account you asked
// about does not exist" — the one answer a resolved "me" can never produce,
// because the caller's own row exists by definition.
const userNotFoundMessage = "user not found"

// meAliasDB builds a sqlite database with the tables the swept routes touch,
// seeds the account the sweep signs in as, and returns it installed as the
// process-wide database.
func meAliasDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	err = db.AutoMigrate(
		&database.User{},
		&database.UserFollow{},
		&database.UserBlock{},
		&database.UserStats{},
		&database.List{},
		&database.ListLike{},
		&database.SmartList{},
		&database.ListCollaborator{},
		&database.UserHistory{},
		&database.UserRating{},
		&database.UserWatchlist{},
		&database.UserCollection{},
		&database.UserFavorite{},
		&database.UserHidden{},
		&database.UserNote{},
		&database.Comment{},
		&database.ActivityFeed{},
		&database.Notification{},
		&database.SavedFilter{},
		&database.EventsOutbox{},
		&database.LocalSession{},
		&database.LocalCredential{},
		&database.LocalAuthToken{},
	)
	if err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	if err := db.Create(&database.User{
		ID:       meAliasUserID,
		Username: "mealias",
		Email:    "mealias@example.test",
		Status:   database.UserStatusActive,
		Plan:     "free",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// concreteMePath turns a registered route pattern into a requestable path with
// the user id set to the "me" alias. Other parameters get a placeholder value:
// the sweep is about which user the route resolves, not about those ids.
func concreteMePath(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		switch {
		case part == ":id":
			parts[i] = "me"
		case strings.HasPrefix(part, ":"):
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

// errorEnvelope is the only part of a response this sweep reads.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// TestUserScopedRoutesResolveMeAlias is the class guard described above.
func TestUserScopedRoutesResolveMeAlias(t *testing.T) {
	// Session tokens are blind-indexed with a key derived from the root key, so
	// the token this test mints needs one. Built at runtime (never a literal:
	// a secret-shaped constant on any branch fails the repo's gitleaks job).
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}

	db := meAliasDB(t)
	token, _, err := auth.CreateSession(db, meAliasUserID, "", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	app := fiber.New()
	RegisterRoutes(app)

	// Every route whose path carries a user id at ":id". DELETE /v1/users/me
	// is the account-deletion route: it removes the very account this sweep
	// signs in as, so it is swept last (see below) rather than mid-flight.
	var swept int
	seen := map[string]int{}
	for _, r := range app.GetRoutes() {
		if !strings.Contains(r.Path, "/users/:id") || strings.Contains(r.Path, "*") {
			continue
		}
		path := concreteMePath(r.Path)
		if r.Method == http.MethodDelete && path == "/v1/users/me" {
			continue
		}
		swept++

		req := httptest.NewRequest(r.Method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if r.Method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s %s: request failed: %v", r.Method, path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		var env errorEnvelope
		_ = json.Unmarshal(body, &env)
		if env.Error.Message == userNotFoundMessage {
			t.Errorf("%s %s answered 404 %q for the CALLER'S OWN id — the \":id\" parameter is read without auth.ResolveTargetID",
				r.Method, path, userNotFoundMessage)
		}
		seen[r.Method+" "+path] = resp.StatusCode
	}

	// The sweep must actually cover the surface, or it passes vacuously.
	const minUserScopedRoutes = 12
	if swept < minUserScopedRoutes {
		t.Fatalf("swept only %d user-scoped routes, want at least %d — is RegisterRoutes still wiring them?",
			swept, minUserScopedRoutes)
	}

	// Pin the routes this guard was written for, so a future 500 or 401 on them
	// cannot hide behind the message check above. 200/204 mean the caller's own
	// row was found; 400 is the correct answer where the request is pointed at
	// yourself and no handler may allow it (follow/unfollow/block/unblock).
	pinned := map[string][]int{
		"GET /v1/users/me/followers": {http.StatusOK},
		"GET /v1/users/me/following": {http.StatusOK},
		"GET /v1/users/me/friends":   {http.StatusOK},
		"GET /v1/users/me/lists":     {http.StatusOK},
		"POST /v1/users/me/follow":   {http.StatusBadRequest},
		"POST /v1/users/me/block":    {http.StatusBadRequest},
		"DELETE /v1/users/me/block":  {http.StatusBadRequest},
		"DELETE /v1/users/me/follow": {http.StatusBadRequest},
	}
	for route, wantAny := range pinned {
		got, ok := seen[route]
		if !ok {
			t.Errorf("%s was not swept — it is not registered any more (or its path changed)", route)
			continue
		}
		if !containsInt(wantAny, got) {
			t.Errorf("%s = %d, want one of %v", route, got, wantAny)
		}
	}
}

// TestSignedOutMeAliasIsUnauthorizedNotMissing pins the other half of the
// contract: "me" with nobody signed in is an authentication problem (401), not
// a missing account (404). A signed-out client that reads 404 concludes its
// account was deleted.
func TestSignedOutMeAliasIsUnauthorizedNotMissing(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	meAliasDB(t)

	app := fiber.New()
	RegisterRoutes(app)

	for _, path := range []string{
		"/v1/users/me/followers",
		"/v1/users/me/following",
		"/v1/users/me/friends",
		"/v1/users/me/lists",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s (signed out) = %d, want 401", path, resp.StatusCode)
		}
		var env errorEnvelope
		_ = json.Unmarshal(body, &env)
		if env.Error.Message == userNotFoundMessage {
			t.Errorf("GET %s (signed out) answered %q — 401 authentication required is the honest answer",
				path, userNotFoundMessage)
		}
	}
}

func containsInt(haystack []int, needle int) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
