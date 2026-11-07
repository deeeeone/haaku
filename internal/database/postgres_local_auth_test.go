//go:build postgres

// Real-PostgreSQL proof for the local-account schema (issue #53).
//
// The email-claim rule is a *partial* unique index on lower(email) restricted
// to local rows, and the cascade/revocation rules are foreign keys. sqlite has
// no partial indexes, so a sqlite test would prove nothing about the DDL a
// deployment actually gets; this test runs the real migrations on a throwaway
// database and then exercises the constraints.

package database

import (
	"context"
	"strings"
	"testing"
)

// TestLocalAuthSchemaOnPostgres applies every migration to a fresh PostgreSQL
// and then proves the three properties the module depends on:
//
//  1. two local accounts cannot claim the same email, case-insensitively;
//  2. a Clerk-mirrored row (id 'user_...') may hold an address a local account
//     already claimed — the two id spaces stay independent;
//  3. a soft-deleted local account releases its claim, and deleting a user
//     cascades its sessions away.
func TestLocalAuthSchemaOnPostgres(t *testing.T) {
	dsn := newTestDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("HAAKU_MIGRATION_BACKUP_DIR", t.TempDir())
	g := openGorm(t, dsn)
	SetDBForTest(t, g)

	if err := Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"local_credentials", "local_sessions", "local_auth_tokens"} {
		if !relationExists(t, g, table) {
			t.Fatalf("table %q missing after migrations", table)
		}
	}
	for _, index := range []string{
		"idx_users_local_email_unique",
		"idx_local_sessions_user_id",
		"idx_local_sessions_expires_at",
		"idx_local_auth_tokens_user_id",
	} {
		if !relationExists(t, g, index) {
			t.Errorf("index %q missing after migrations", index)
		}
	}

	insertUser := func(id, email string) error {
		return g.Exec(`INSERT INTO users (id, username, email) VALUES (?, ?, ?)`, id, id, email).Error
	}

	// 1. The first local claim is accepted.
	if err := insertUser("local_aaaa", "Same@Example.com"); err != nil {
		t.Fatalf("first local user: %v", err)
	}
	// 2. The same address in different case is rejected.
	err := insertUser("local_bbbb", "same@example.com")
	if err == nil {
		t.Fatal("the partial unique index accepted a second local claim on the same email")
	}
	if !strings.Contains(err.Error(), "idx_users_local_email_unique") &&
		!strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("duplicate local email failed for the wrong reason: %v", err)
	}
	// 3. A Clerk-mirrored row may hold the same address.
	if err := insertUser("user_cccc", "same@example.com"); err != nil {
		t.Fatalf("a clerk row could not hold an address a local account claimed: %v", err)
	}
	// 4. A soft-deleted local row releases the claim.
	if err := g.Exec(`UPDATE users SET deleted_at = now() WHERE id = 'local_aaaa'`).Error; err != nil {
		t.Fatalf("soft-delete local user: %v", err)
	}
	if err := insertUser("local_dddd", "same@example.com"); err != nil {
		t.Fatalf("a soft-deleted local account kept its email claim: %v", err)
	}

	// 5. Sessions require a live user row and go away with it.
	if err := g.Exec(`INSERT INTO local_credentials (user_id, password_hash) VALUES ('local_dddd', 'x')`).Error; err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	if err := g.Exec(`INSERT INTO local_sessions (id, user_id, token_hash, expires_at)
		VALUES ('sess_1', 'local_dddd', 'hash_1', now() + interval '1 day')`).Error; err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := g.Exec(`INSERT INTO local_sessions (id, user_id, token_hash, expires_at)
		VALUES ('sess_2', 'local_missing', 'hash_2', now() + interval '1 day')`).Error; err == nil {
		t.Fatal("a session row was created for a user that does not exist")
	}
	if err := g.Exec(`DELETE FROM users WHERE id = 'local_dddd'`).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var remaining int64
	if err := g.Table("local_sessions").Where("id = ?", "sess_1").Count(&remaining).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("deleting a user left %d session row(s) behind", remaining)
	}
}
