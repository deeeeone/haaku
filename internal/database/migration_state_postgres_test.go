//go:build postgres

// The real-PostgreSQL half of the /readyz migration-state reporting (issue #14).
//
// The handler tests in internal/httpapi cover the wiring on sqlite. Two things
// can only be proven here, against a real server:
//
//  1. ReadMigrationState reads the real Goose ledger the migration runner
//     writes, and agrees with the files on disk.
//  2. The "migrating" branch: migrationInProgress reads pg_locks, which sqlite
//     does not have, so only PostgreSQL can show it detecting a held lock.
//
// It is part of the `postgres` suite (`make test-db`, and CI's postgres job), so
// `go test ./...` stays fast and service-free.

package database

import (
	"context"
	"testing"
)

// TestReadMigrationStateOnMigratedDatabase is the honest-200 case against a real
// server: after the runner applies every migration, the reported state matches
// the files this binary ships.
func TestReadMigrationStateOnMigratedDatabase(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)

	if err := runMigrate(t, g, testOptions(t)); err != nil {
		t.Fatalf("migrate the test database: %v", err)
	}

	state, err := ReadMigrationState(context.Background(), g)
	if err != nil {
		t.Fatalf("ReadMigrationState: %v", err)
	}
	want := migrationFileVersions(t)
	if !state.UpToDate {
		t.Errorf("up_to_date = false on a freshly migrated database (state %+v)", state)
	}
	if state.Version != want[len(want)-1] || state.Newest != want[len(want)-1] {
		t.Errorf("version/newest = %d/%d, want both %d", state.Version, state.Newest, want[len(want)-1])
	}
	if state.Applied != len(want) || state.Expected != len(want) {
		t.Errorf("applied/expected = %d/%d, want %d", state.Applied, state.Expected, len(want))
	}
	if state.Pending != 0 {
		t.Errorf("pending = %d, want 0", state.Pending)
	}
	if state.Migrating {
		t.Errorf("migrating = true on a database nobody is migrating")
	}
}

// TestReadMigrationStateOnFreshDatabase is the honest-503 case against a real
// server: a database that exists but has never been migrated reads as behind by
// every migration, not as an error and not as current.
func TestReadMigrationStateOnFreshDatabase(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)

	state, err := ReadMigrationState(context.Background(), g)
	if err != nil {
		t.Fatalf("ReadMigrationState on a fresh database: %v", err)
	}
	if state.UpToDate {
		t.Errorf("up_to_date = true on an unmigrated database (state %+v)", state)
	}
	if state.Version != 0 {
		t.Errorf("version = %d, want 0", state.Version)
	}
	if state.Pending != state.Expected || state.Expected == 0 {
		t.Errorf("pending/expected = %d/%d, want pending == expected > 0", state.Pending, state.Expected)
	}
}

// TestReadMigrationStateDetectsMigrationInProgress holds the migration advisory
// lock on a separate session and asserts ReadMigrationState reports "migrating"
// rather than pretending the schema is current. This is the pg_locks path sqlite
// cannot exercise.
func TestReadMigrationStateDetectsMigrationInProgress(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)

	sqlDB, err := g.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	ctx := context.Background()
	holder, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("opening holder connection: %v", err)
	}
	defer func() { _ = holder.Close() }()

	if migrationInProgress(ctx, g) {
		t.Fatal("migrationInProgress reported a lock before one was taken")
	}

	var locked bool
	if err := holder.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&locked); err != nil {
		t.Fatalf("taking the migration lock by hand: %v", err)
	}
	if !locked {
		t.Fatal("could not take the migration lock by hand")
	}
	defer func() {
		if _, err := holder.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			t.Logf("releasing the hand-held lock: %v", err)
		}
	}()

	if !migrationInProgress(ctx, g) {
		t.Fatal("migrationInProgress did not see the held advisory lock")
	}

	state, err := ReadMigrationState(ctx, g)
	if err != nil {
		t.Fatalf("ReadMigrationState while the lock is held: %v", err)
	}
	if !state.Migrating {
		t.Errorf("migrating = false while the migration lock is held (state %+v)", state)
	}
	if state.UpToDate {
		t.Errorf("up_to_date = true on an unmigrated database with the lock held (state %+v)", state)
	}
}
