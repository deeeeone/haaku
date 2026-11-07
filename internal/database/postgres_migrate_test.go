//go:build postgres

// The real-PostgreSQL test suite for this package (issue #14).
//
// It is behind the `postgres` build tag for three reasons:
//
//  1. `go test ./...` (and `make test-go`) is the fast, dependency-free suite a
//     contributor runs on every save; it must never need a database server.
//  2. A DATABASE_URL-gated t.Skip() reads as a pass. These tests prove the
//     schema, the advisory lock and the ledger, so "not run" has to be visible
//     as "not run" — an untagged suite that silently skips on an unset variable
//     is exactly the failure mode issue #14 exists to close.
//  3. It makes the DB-backed suite an explicit thing a reviewer can name:
//     `make test-db` is `go test -tags postgres ./...`, and that is the same
//     command CI runs against its postgres service container.
//
// Run it locally with `make pg-up && export DATABASE_URL=... && make test-db`
// (or `scripts/dev-pg.sh start -q`, which needs no Docker).
//
// These tests run against a REAL PostgreSQL, never sqlite: the advisory lock,
// the jsonb/partial-index DDL and the ledger bootstrap have no sqlite
// equivalent, and a sqlite-backed test would prove nothing about the schema a
// deployment actually gets.
//
// Each test creates its own throwaway database on the same server so tests
// never share a schema, and drops it afterwards.

package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const migrationDir = "sql"

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is not set; the postgres suite needs a server — run `./scripts/dev-pg.sh start -q` and export DATABASE_URL, or `make test-db` with it set")
	}
	return dsn
}

// newTestDatabase creates a uniquely named database on the server named by
// DATABASE_URL and returns its DSN. Cleanup drops it.
func newTestDatabase(t *testing.T) string {
	t.Helper()
	base := testDatabaseURL(t)
	name := "tkiller_mig_" + randomSuffix(t)

	adminDSN, err := withDatabaseName(base, "postgres")
	if err != nil {
		t.Fatalf("deriving admin DSN: %v", err)
	}
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connecting to the admin database: %v", err)
	}
	if err := admin.Exec(`CREATE DATABASE "` + name + `"`).Error; err != nil {
		t.Fatalf("creating test database %s: %v", name, err)
	}

	dsn, err := withDatabaseName(base, name)
	if err != nil {
		t.Fatalf("deriving test DSN: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error; err != nil {
			t.Logf("could not drop test database %s: %v", name, err)
		}
	})
	return dsn
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func withDatabaseName(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func openGorm(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	g, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("opening %s: %v", dsn, err)
	}
	sqlDB, err := g.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return g
}

// testOptions is migrateOptions pointed at the real embedded migrations, with a
// short wait/timeout and a temporary backup directory so a test never writes to
// the checkout.
func testOptions(t *testing.T) migrateOptions {
	t.Helper()
	return migrateOptions{
		Wait:      30 * time.Second,
		Timeout:   2 * time.Minute,
		BackupDir: t.TempDir(),
	}
}

func runMigrate(t *testing.T, g *gorm.DB, opts migrateOptions) error {
	t.Helper()
	return migrateTo(context.Background(), g, opts)
}

// appliedVersions returns the applied migration versions, excluding Goose's
// version-0 "initial" row, which is bookkeeping rather than a migration.
func appliedVersions(t *testing.T, g *gorm.DB) []int64 {
	t.Helper()
	var versions []int64
	if err := g.Raw(`SELECT version_id FROM goose_db_version WHERE is_applied AND version_id > 0 ORDER BY version_id`).
		Scan(&versions).Error; err != nil {
		t.Fatalf("reading the goose ledger: %v", err)
	}
	return versions
}

func migrationFileVersions(t *testing.T) []int64 {
	t.Helper()
	// The embedded FS is the same set of files the binary carries.
	sub, err := migrationSubFS(defaultMigrateOptions().FS, migrationDir)
	if err != nil {
		t.Fatalf("opening embedded migrations: %v", err)
	}
	names, err := fs.Glob(sub, "*.sql")
	if err != nil {
		t.Fatalf("listing embedded migrations: %v", err)
	}
	versions := make([]int64, 0, len(names))
	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		t.Fatal("no embedded migrations found")
	}
	slices.Sort(versions)
	return versions
}

func relationExists(t *testing.T, g *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := g.Raw(`SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&exists).Error; err != nil {
		t.Fatalf("checking relation %s: %v", name, err)
	}
	return exists
}

// TestMigrateAppliesEveryMigration is the headline test: a fresh database, the
// public Migrate entry point, and every embedded file applied with the ledger
// matching the files on disk. It fails if Migrate is reverted to the stub.
func TestMigrateAppliesEveryMigration(t *testing.T) {
	dsn := newTestDatabase(t)
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("TKILLER_MIGRATION_BACKUP_DIR", t.TempDir())
	g := openGorm(t, dsn)
	SetDBForTest(t, g)

	if err := Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate on a fresh database: %v", err)
	}

	got := appliedVersions(t, g)
	want := migrationFileVersions(t)
	if len(got) != len(want) {
		t.Fatalf("goose ledger has %d applied versions, want one row per migration file (%d):\n got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ledger version[%d]=%d, want %d (got=%v want=%v)", i, got[i], want[i], got, want)
		}
	}

	// A representative slice of the schema the migrations define: tables from
	// the first, the search migration and the later numbered ones, plus the
	// indexes those migrations create.
	for _, table := range []string{
		"users", "movies", "shows", "episodes", "people", "oauth_tokens",
		"lists", "comments", "notifications", "webhook_endpoints",
		"integration_tokens", "user_notes", "saved_filters", "content_reports",
	} {
		if !relationExists(t, g, table) {
			t.Errorf("table %q missing after migrations", table)
		}
	}
	for _, index := range []string{
		"idx_movies_search", "idx_users_clerk_synced_at",
		"idx_movies_genres_gin", "idx_comments_trending_score",
	} {
		if !relationExists(t, g, index) {
			t.Errorf("index %q missing after migrations", index)
		}
	}

	// The citext-dependent column proves the SQL ran (GORM's AutoMigrate never
	// creates citext), not just that tables exist.
	var dataType string
	if err := g.Raw(`SELECT data_type FROM information_schema.columns
		WHERE table_name='users' AND column_name='username'`).Scan(&dataType).Error; err != nil {
		t.Fatalf("reading users.username type: %v", err)
	}
	if dataType != "USER-DEFINED" && dataType != "citext" {
		t.Errorf("users.username data_type = %q, want citext (migration 0001's CREATE EXTENSION citext did not run)", dataType)
	}
}

// TestMigrateIsIdempotent asserts a second run is a no-op: no error, no new
// ledger rows, and no re-application (the recorded timestamps do not move).
func TestMigrateIsIdempotent(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)
	opts := testOptions(t)

	if err := runMigrate(t, g, opts); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	before := appliedVersions(t, g)
	var firstStamp time.Time
	if err := g.Raw(`SELECT tstamp FROM goose_db_version WHERE version_id=1 AND is_applied`).Scan(&firstStamp).Error; err != nil {
		t.Fatalf("reading version 1 timestamp: %v", err)
	}

	time.Sleep(1100 * time.Millisecond) // a replay would write a newer tstamp
	if err := runMigrate(t, g, opts); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	after := appliedVersions(t, g)
	if len(after) != len(before) {
		t.Fatalf("ledger row count changed on the second run: %d -> %d", len(before), len(after))
	}
	var secondStamp time.Time
	if err := g.Raw(`SELECT tstamp FROM goose_db_version WHERE version_id=1 AND is_applied`).Scan(&secondStamp).Error; err != nil {
		t.Fatalf("reading version 1 timestamp again: %v", err)
	}
	if !secondStamp.Equal(firstStamp) {
		t.Fatalf("migration 0001 was re-applied: tstamp %s -> %s", firstStamp, secondStamp)
	}
}

// TestMigrateWaitsForAnotherServer holds the migration lock by hand and asserts
// a concurrent run blocks until it is free, rather than racing and applying the
// same SQL twice.
func TestMigrateWaitsForAnotherServer(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)
	opts := testOptions(t)

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
	var locked bool
	if err := holder.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&locked); err != nil {
		t.Fatalf("taking the migration lock by hand: %v", err)
	}
	if !locked {
		t.Fatal("could not take the migration lock by hand")
	}

	done := make(chan error, 1)
	go func() { done <- runMigrate(t, g, opts) }()

	select {
	case err := <-done:
		t.Fatalf("Migrate returned (%v) while another session held the lock", err)
	case <-time.After(700 * time.Millisecond):
		// Still waiting, as intended.
	}

	if _, err := holder.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
		t.Fatalf("releasing the hand-held lock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Migrate after the lock was released: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Migrate did not proceed after the lock was released")
	}

	if got := len(appliedVersions(t, g)); got == 0 {
		t.Fatal("no migrations applied")
	}
}

// TestMigrateConcurrentRunsApplyOnce starts two migration runs at the same
// moment. Exactly one applies; the other waits and finds nothing to do. Neither
// errors, and the ledger has one row per file — no duplicates, no corruption.
func TestMigrateConcurrentRunsApplyOnce(t *testing.T) {
	dsn := newTestDatabase(t)
	g1 := openGorm(t, dsn)
	g2 := openGorm(t, dsn)
	opts := testOptions(t)

	errs := make(chan error, 2)
	go func() { errs <- runMigrate(t, g1, opts) }()
	go func() { errs <- runMigrate(t, g2, opts) }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Migrate run %d: %v", i+1, err)
		}
	}

	got := appliedVersions(t, g1)
	want := migrationFileVersions(t)
	if len(got) != len(want) {
		t.Fatalf("ledger has %d rows after two concurrent runs, want %d (duplicates would exceed it): %v", len(got), len(want), got)
	}
}

// TestMigrateFailingMigrationKeepsBackupAndRefuses seeds a schema, then adds a
// migration that cannot succeed. The run must fail, leave the pre-migration
// dump in place, and not record the failed version — a half-applied schema that
// reported success would be the worst outcome.
func TestMigrateFailingMigrationKeepsBackupAndRefuses(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)
	backupDir := t.TempDir()

	good := "-- +goose Up\nCREATE TABLE mig_probe (id int);\n-- +goose Down\nDROP TABLE mig_probe;\n"
	fs1 := fstest.MapFS{"0001_ok.sql": &fstest.MapFile{Data: []byte(good)}}
	if err := runMigrate(t, g, migrateOptions{
		FS: fs1, Dir: ".", Wait: 30 * time.Second, Timeout: time.Minute, BackupDir: backupDir,
	}); err != nil {
		t.Fatalf("seeding migration: %v", err)
	}
	if got := appliedVersions(t, g); len(got) != 1 {
		t.Fatalf("expected version 1 applied, got %v", got)
	}

	bad := "-- +goose Up\nCREATE TABLE mig_bad (id int;\n-- +goose Down\nDROP TABLE mig_bad;\n"
	fs2 := fstest.MapFS{
		"0001_ok.sql":  &fstest.MapFile{Data: []byte(good)},
		"0002_bad.sql": &fstest.MapFile{Data: []byte(bad)},
	}
	err := runMigrate(t, g, migrateOptions{
		FS: fs2, Dir: ".", Wait: 30 * time.Second, Timeout: time.Minute, BackupDir: backupDir,
	})
	if err == nil {
		t.Fatal("Migrate reported success on a deliberately failing migration")
	}

	dumps, _ := filepath.Glob(filepath.Join(backupDir, "*.dump"))
	if len(dumps) == 0 {
		t.Fatalf("no pre-migration backup left in %s after the failed run (err=%v)", backupDir, err)
	}
	fi, statErr := os.Stat(dumps[0])
	if statErr != nil || fi.Size() == 0 {
		t.Fatalf("pre-migration backup %s is missing or empty", dumps[0])
	}

	if got := appliedVersions(t, g); len(got) != 1 || got[0] != 1 {
		t.Fatalf("ledger = %v, want only version 1 (the failed 0002 must not be recorded)", got)
	}
}

// TestMigrateRefusesWithoutPgDump covers the safety net's escape hatch: with a
// usable pg_dump absent, a run against an existing schema refuses; only the
// documented TKILLER_SKIP_BACKUP_BEFORE_MIGRATE=true proceeds.
func TestMigrateRefusesWithoutPgDump(t *testing.T) {
	dsn := newTestDatabase(t)
	g := openGorm(t, dsn)

	good := "-- +goose Up\nCREATE TABLE mig_probe (id int);\n-- +goose Down\nDROP TABLE mig_probe;\n"
	next := "-- +goose Up\nCREATE TABLE mig_next (id int);\n-- +goose Down\nDROP TABLE mig_next;\n"
	base := migrateOptions{Dir: ".", Wait: 30 * time.Second, Timeout: time.Minute, BackupDir: t.TempDir()}
	base.FS = fstest.MapFS{"0001_ok.sql": &fstest.MapFile{Data: []byte(good)}}
	if err := runMigrate(t, g, base); err != nil {
		t.Fatalf("seeding migration: %v", err)
	}

	both := fstest.MapFS{
		"0001_ok.sql":   &fstest.MapFile{Data: []byte(good)},
		"0002_next.sql": &fstest.MapFile{Data: []byte(next)},
	}
	t.Setenv("TKILLER_PG_DUMP", "/nonexistent/definitely-not-pg_dump")

	opts := base
	opts.FS = both
	if err := runMigrate(t, g, opts); err == nil {
		t.Fatal("Migrate proceeded although pg_dump was unavailable and no skip was set")
	} else if got := appliedVersions(t, g); len(got) != 1 {
		t.Fatalf("ledger = %v, want only version 1 (must refuse before applying)", got)
	}

	t.Setenv("TKILLER_SKIP_BACKUP_BEFORE_MIGRATE", "true")
	opts.SkipBackup = skipBackupBeforeMigrate() // the env is read when the options are built
	if err := runMigrate(t, g, opts); err != nil {
		t.Fatalf("Migrate with the skip escape hatch set: %v", err)
	}
	if got := appliedVersions(t, g); len(got) != 2 {
		t.Fatalf("ledger = %v, want versions 1 and 2", got)
	}
}
