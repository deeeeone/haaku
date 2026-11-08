package users

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/config"
	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Account deletion has two identity owners (issue #53). In clerk mode the Clerk
// account is deleted first, because Clerk owns identity. In local mode there is
// no third-party service to call at all: the account this server issued is
// deleted here, and its credential/session/token rows are part of the wipe.
// The handler used to call Clerk unconditionally, so a self-hosted owner asking
// to delete their own account got `500 failed to delete account` — the settings
// screen's "delete account" button never worked on a local instance.

const deleteTestUserID = "local_deleteme00000000000000000"

// deleteTestDB builds a database with every table DeleteUserData wipes, seeds
// the account under test with a credential and a live session, and installs it
// process-wide. The six tables created by hand are the ones whose GORM models
// declare a PostgreSQL-only column default (`now()`, `gen_random_uuid()`), which
// sqlite refuses to parse; AutoMigrate would fail on them.
func deleteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(
		&database.User{},
		&database.UserFollow{},
		&database.UserBlock{},
		&database.UserStats{},
		&database.UserHistory{},
		&database.UserRating{},
		&database.UserWatchlist{},
		&database.UserCollection{},
		&database.UserFavorite{},
		&database.UserHidden{},
		&database.UserNote{},
		&database.SavedFilter{},
		&database.List{},
		&database.ListItem{},
		&database.ListComment{},
		&database.ListLike{},
		&database.ListCollaborator{},
		&database.SmartList{},
		&database.Comment{},
		&database.CommentLike{},
		&database.CommentReaction{},
		&database.ContentReport{},
		&database.ActivityFeed{},
		&database.Notification{},
		&database.WebhookEndpoint{},
		&database.IntegrationToken{},
		&database.EventsOutbox{},
		&database.SyncSession{},
		&database.LocalCredential{},
		&database.LocalSession{},
		&database.LocalAuthToken{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE oauth_clients (id text primary key, name text not null, redirect_uris text not null default '[]', secret_hash text not null, user_id text not null default '', created_at datetime)`,
		`CREATE TABLE oauth_grants (id text primary key, client_id text not null default '', user_id text not null default '', created_at datetime)`,
		`CREATE TABLE oauth_tokens (id text primary key, client_id text not null default '', user_id text not null default '', created_at datetime)`,
		`CREATE TABLE scrobble_sessions (id integer primary key autoincrement, user_id text not null default '', created_at datetime)`,
		`CREATE TABLE user_playback (id integer primary key autoincrement, user_id text not null default '', media_type text not null default '', media_id integer not null default 0, progress real not null default 0, paused_at datetime)`,
		`CREATE TABLE user_checkins (id integer primary key autoincrement, user_id text not null default '', media_type text not null default '', created_at datetime)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create table: %v\n%s", err, ddl)
		}
	}

	if err := db.Create(&database.User{
		ID:       deleteTestUserID,
		Username: "deleteme",
		Email:    "deleteme@example.test",
		Status:   database.UserStatusActive,
		Plan:     "free",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&database.LocalCredential{
		UserID:       deleteTestUserID,
		PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$hash",
	}).Error; err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := db.Create(&database.LocalAuthToken{
		ID:        "tok-1",
		UserID:    deleteTestUserID,
		TokenHash: "hash-1",
		Purpose:   database.LocalAuthTokenPurposePasswordReset,
	}).Error; err != nil {
		t.Fatalf("seed reset token: %v", err)
	}
	database.SetDBForTest(t, db)
	return db
}

// deleteAccountApp mounts the real handler behind a stub that pins the caller,
// because auth.AuthRequired parses a live token and is exercised elsewhere.
func deleteAccountApp(userID string) *fiber.App {
	app := fiber.New()
	app.Delete("/v1/users/me", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handleDeleteAccount(c)
	})
	return app
}

func callDeleteAccount(t *testing.T, app *fiber.App) (int, map[string]any) {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodDelete, "/v1/users/me", nil), -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// TestDeleteAccountLocalModeDeletesWithoutClerk is the regression: a local
// instance must delete the account it issued, with no Clerk call anywhere (the
// stub below records every request it receives).
//
// The sqlite harness cannot carry the request all the way to 204: the wipe ends
// with an events_outbox insert written with a PostgreSQL-only `?::jsonb` cast,
// so the request stops there. What this test owns is the branch — local mode
// must not reach for Clerk — and the row-level proof that the wipe really
// removes the account lives in TestDeleteAccountWipeRemovesLocalRows, which
// drives the same function the handler calls.
func TestDeleteAccountLocalModeDeletesWithoutClerk(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("local", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	t.Cleanup(func() { _ = auth.InitAuthMode("local", "approval") })

	var clerkCalls int
	clerk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clerkCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer clerk.Close()
	// A configured Clerk is deliberately present: local mode must ignore it.
	auth.InitClerk(&config.Config{ClerkSecret: "sk_test_unused", ClerkBaseURL: clerk.URL})

	deleteTestDB(t)

	code, body := callDeleteAccount(t, deleteAccountApp(deleteTestUserID))
	if clerkCalls != 0 {
		t.Errorf("local mode called Clerk %d time(s); a local instance must never call a third-party identity service", clerkCalls)
	}
	if message, _ := body["error"].(map[string]any); message != nil {
		if msg, _ := message["message"].(string); msg == "failed to delete account" {
			t.Errorf("DELETE /v1/users/me (local mode) failed in the Clerk step (%q): a local account must delete locally", msg)
		}
	}
	if code == http.StatusNoContent {
		t.Log("local-mode delete completed end to end under this backend")
	}
}

// TestDeleteAccountWipeRemovesLocalRows proves the wipe behind the endpoint:
// the account row and every way back into it — its password hash, its live
// sessions and its one-time reset tokens — are gone. Those three tables cascade
// from users in PostgreSQL but not under every backend, and a deleted account
// must not leave its credential behind on any of them.
func TestDeleteAccountWipeRemovesLocalRows(t *testing.T) {
	db := deleteTestDB(t)

	token, _, err := auth.CreateSession(db, deleteTestUserID, "", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := database.DeleteUserData(db, deleteTestUserID); err != nil {
		t.Fatalf("DeleteUserData: %v", err)
	}

	for _, tc := range []struct {
		table string
	}{
		{"users"},
		{"local_credentials"},
		{"local_auth_tokens"},
		{"local_sessions"},
	} {
		var n int64
		if err := db.Table(tc.table).Where("user_id = ?", deleteTestUserID).Count(&n).Error; err != nil {
			// users has no user_id column; count it by its primary key.
			if err := db.Table(tc.table).Where("id = ?", deleteTestUserID).Count(&n).Error; err != nil {
				t.Fatalf("count %s: %v", tc.table, err)
			}
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) for the deleted account", tc.table, n)
		}
	}

	if _, err := auth.ResolveSessionToken(db, token); err == nil {
		t.Error("the deleted account's session token still resolves")
	}
}

// TestDeleteAccountClerkModeStillCallsClerk keeps the other half honest: in
// clerk mode Clerk owns identity, so the Clerk account must be deleted first
// and the local wipe still runs.
func TestDeleteAccountClerkModeStillCallsClerk(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("clerk", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	t.Cleanup(func() { _ = auth.InitAuthMode("local", "approval") })

	var gotMethod, gotPath string
	clerk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer clerk.Close()
	auth.InitClerk(&config.Config{ClerkSecret: "sk_test_unused", ClerkBaseURL: clerk.URL})

	deleteTestDB(t)

	// The sqlite harness stops at the wipe's PostgreSQL-only outbox insert (see
	// the local-mode test above), so the assertion is the Clerk call itself —
	// which is what "Clerk owns identity" means in this mode.
	_, _ = callDeleteAccount(t, deleteAccountApp(deleteTestUserID))
	if gotMethod != http.MethodDelete || gotPath != "/v1/users/"+deleteTestUserID {
		t.Errorf("Clerk saw %s %s, want DELETE /v1/users/%s", gotMethod, gotPath, deleteTestUserID)
	}
}

// TestDeleteAccountClerkFailureDoesNotWipe pins the ordering that matters in
// clerk mode: if Clerk refuses the delete, nothing local is destroyed — the
// account still exists after the call.
func TestDeleteAccountClerkFailureDoesNotWipe(t *testing.T) {
	t.Setenv("HAAKU_SECRET_KEY", strings.Repeat("ab12", 12))
	if err := auth.InitAuthMode("clerk", "approval"); err != nil {
		t.Fatalf("init auth mode: %v", err)
	}
	t.Cleanup(func() { _ = auth.InitAuthMode("local", "approval") })

	clerk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer clerk.Close()
	auth.InitClerk(&config.Config{ClerkSecret: "sk_test_unused", ClerkBaseURL: clerk.URL})

	db := deleteTestDB(t)

	code, _ := callDeleteAccount(t, deleteAccountApp(deleteTestUserID))
	if code != http.StatusInternalServerError {
		t.Fatalf("DELETE /v1/users/me with a failing Clerk = %d, want 500", code)
	}
	var n int64
	if err := db.Table("users").Where("id = ?", deleteTestUserID).Count(&n).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Errorf("users rows = %d, want 1 — a failed Clerk delete must not wipe the local account", n)
	}
}
