package database

// The parts of the migration test surface that need no database: the
// duration/boolean env parsing and the backup-pruning rule. They stay in the
// default, untagged suite so `go test ./...` still covers the runner's
// configuration code without a PostgreSQL server.
//
// Everything that needs a real server lives in postgres_migrate_test.go behind
// the `postgres` build tag (issue #14).

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPrunePreMigrationBackups is a unit test for the "keep the two newest"
// rule — the reason a long-lived server does not fill its disk one upgrade at a
// time.
func TestPrunePreMigrationBackups(t *testing.T) {
	dir := t.TempDir()
	names := []string{"pre-migrate-old.dump", "pre-migrate-mid.dump", "pre-migrate-new.dump", "pre-migrate-newest.dump"}
	for i, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("dump"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", p, err)
		}
		mod := time.Now().Add(time.Duration(i-10) * time.Hour) // oldest first
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}

	prunePreMigrationBackups(dir, 2, discardLogger())

	left, _ := filepath.Glob(filepath.Join(dir, "*.dump"))
	if len(left) != 2 {
		t.Fatalf("after prune, %d dumps remain, want 2: %v", len(left), left)
	}
	for _, want := range []string{"pre-migrate-newest.dump", "pre-migrate-new.dump"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("newest dump %s was pruned: %v", want, err)
		}
	}
}

// TestMigrationDurationEnv covers the timeout/wait parsing, including the
// fail-safe: a typo must fall back to the default, never silently remove the
// cap.
func TestMigrationDurationEnv(t *testing.T) {
	t.Setenv(migrateTimeoutEnv, "")
	if got := migrationTimeout(); got != defaultMigrateTimeout {
		t.Errorf("unset timeout = %s, want default %s", got, defaultMigrateTimeout)
	}
	t.Setenv(migrateTimeoutEnv, "45s")
	if got := migrationTimeout(); got != 45*time.Second {
		t.Errorf("timeout = %s, want 45s", got)
	}
	t.Setenv(migrateTimeoutEnv, "not-a-duration")
	if got := migrationTimeout(); got != defaultMigrateTimeout {
		t.Errorf("unparseable timeout = %s, want default %s", got, defaultMigrateTimeout)
	}
	t.Setenv(migrateTimeoutEnv, "0")
	if got := migrationTimeout(); got != 0 {
		t.Errorf("zero timeout = %s, want 0 (disabled)", got)
	}

	t.Setenv(migrateWaitEnv, "")
	if got := migrationWait(); got != defaultMigrateWait {
		t.Errorf("unset wait = %s, want default %s", got, defaultMigrateWait)
	}
	t.Setenv(migrateWaitEnv, "90m")
	if got := migrationWait(); got != 90*time.Minute {
		t.Errorf("wait = %s, want 90m", got)
	}
}

// TestSkipBackupEnvParsing pins the boolean parsing of the escape hatch, so a
// "yes"/"1" does not silently disable — or fail to disable — the safety net.
func TestSkipBackupEnvParsing(t *testing.T) {
	t.Setenv(skipBackupEnv, "true")
	if !skipBackupBeforeMigrate() {
		t.Error("true did not enable the skip")
	}
	t.Setenv(skipBackupEnv, "yes")
	if skipBackupBeforeMigrate() {
		t.Error("yes must not be accepted; only strconv.ParseBool values are")
	}
	t.Setenv(skipBackupEnv, "TRUE")
	if !skipBackupBeforeMigrate() {
		t.Error("TRUE did not enable the skip")
	}
}
