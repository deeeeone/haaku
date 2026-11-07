//go:build postgres

// Moved out of the untagged suite with the rest of the DB-backed tests (issue
// #14 §4): TestInitDBDoesNotCreateSchema opens a real PostgreSQL and applies the
// embedded migrations, so it cannot run — and must not silently skip — without a
// server. See postgres_migrate_test.go for the suite's rationale.

package database

import (
	"context"
	"sync"
	"testing"
)

// TestInitDBDoesNotCreateSchema is the regression test for the schema-ownership
// fix: InitDB opens and pools the connection but must not create a single
// table. GORM's AutoMigrate used to run here, and because it ran before the
// SQL migrations it fabricated GORM-flavoured column types (search_vector as
// text, users.username as text) that then broke migration 0003's GIN index.
//
// The test fails if anyone reintroduces AutoMigrate into InitDB: after InitDB
// on a fresh database there must be nothing but the migration runner's own
// bookkeeping, and Migrate is the only thing allowed to build the schema.
//
// Runs against a real PostgreSQL (the `postgres` build tag); it needs a server
// and does not skip — see the suite note in postgres_migrate_test.go.
func TestInitDBDoesNotCreateSchema(t *testing.T) {
	dsn := newTestDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("HAAKU_MIGRATION_BACKUP_DIR", t.TempDir())

	// InitDB is guarded by a package-level sync.Once and writes a package-level
	// singleton. Reset both so this test starts from a clean slate, and restore
	// them afterwards so the surrounding tests are unaffected.
	dbOnce = sync.Once{}
	db = nil
	t.Cleanup(func() {
		_ = CloseDB()
		db = nil
		dbOnce = sync.Once{}
	})

	g, err := InitDB()
	if err != nil {
		t.Fatalf("InitDB on a fresh database: %v", err)
	}

	// Nothing the migrations own may exist yet — otherwise the schema is not
	// migration-owned and the ordering bug is back.
	for _, table := range []string{
		"users", "movies", "shows", "episodes", "people",
		"oauth_tokens", "lists", "notifications", "webhook_endpoints",
		"goose_db_version",
	} {
		if relationExists(t, g, table) {
			t.Errorf("InitDB created relation %q; InitDB must not touch the schema — Migrate owns it", table)
		}
	}

	// Migrate is what builds the schema, and it must work on the pooled handle
	// InitDB just opened (the API-mode startup sequence: InitDB then Migrate).
	if err := Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate after InitDB: %v", err)
	}
	if !relationExists(t, g, "users") {
		t.Error("users table missing after Migrate")
	}
	// The index that the AutoMigrate-first ordering used to make impossible.
	if !relationExists(t, g, "idx_movies_search") {
		t.Error("idx_movies_search missing after Migrate; search_vector is not the migration's tsvector")
	}
}
