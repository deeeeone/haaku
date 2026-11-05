package database

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Pre-migration safety copy.
//
// A migration that goes wrong on a database with data is the one failure a
// backup actually saves you from, so the runner takes a pg_dump in Postgres's
// custom format before applying anything and refuses to migrate if it cannot.
// A backup that silently does not happen is worse than no backup at all, so
// every failure path here is fatal unless the operator opts out explicitly with
// TKILLER_SKIP_BACKUP_BEFORE_MIGRATE=true.
//
// Only the two newest copies are kept, so a long-lived server does not fill its
// disk one upgrade at a time.

const (
	// pgDumpEnv overrides the pg_dump binary (e.g. a wrapper or a versioned
	// path). Tests use it to prove the "no pg_dump" path refuses.
	pgDumpEnv = "TKILLER_PG_DUMP"
	// keepPreMigrationBackups is how many pre-migration dumps survive a run.
	keepPreMigrationBackups = 2
)

// takePreMigrationBackup writes a custom-format pg_dump of dsn into dir and
// returns its path. It prunes older copies to keepPreMigrationBackups.
func takePreMigrationBackup(ctx context.Context, dsn, dir string, logger *slog.Logger) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", fmt.Errorf("pre-migration backup: no database connection string available")
	}
	dumpBin := strings.TrimSpace(os.Getenv(pgDumpEnv))
	if dumpBin == "" {
		dumpBin = "pg_dump"
	}
	if _, err := exec.LookPath(dumpBin); err != nil {
		return "", fmt.Errorf(
			"pre-migration backup: %q not found on PATH; install the matching postgresql-client, "+
				"or set %s=true to migrate without a backup: %w", dumpBin, skipBackupEnv, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("pre-migration backup: creating %s: %w", dir, err)
	}

	path := filepath.Join(dir, fmt.Sprintf("pre-migrate-%s.dump", time.Now().UTC().Format("20060102T150405Z")))
	args := []string{
		"--format=custom",
		"--no-password",
		"--file=" + path,
		"--dbname=" + dsn,
	}
	cmd := exec.CommandContext(ctx, dumpBin, args...) //nolint:gosec // the discovered pg_dump, arguments built here, no shell
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("pre-migration backup failed (refusing to migrate): %w: %s",
			err, strings.TrimSpace(stderr.String()))
	}

	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("pre-migration backup: stat %s: %w", path, err)
	}
	if fi.Size() == 0 {
		_ = os.Remove(path)
		return "", fmt.Errorf("pre-migration backup produced an empty archive; refusing to migrate")
	}
	if err := verifyDump(ctx, path, logger); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("pre-migration backup failed verification (refusing to migrate): %w", err)
	}

	prunePreMigrationBackups(dir, keepPreMigrationBackups, logger)
	return path, nil
}

// verifyDump checks the archive has a readable table of contents, so an empty
// or truncated dump cannot masquerade as a backup. It is skipped with a warning
// when pg_restore is not installed.
func verifyDump(ctx context.Context, path string, logger *slog.Logger) error {
	restoreBin, err := exec.LookPath("pg_restore")
	if err != nil {
		logger.Warn("pg_restore not found; skipping backup archive verification",
			"path", path, "error", err)
		return nil
	}
	out, err := exec.CommandContext(ctx, restoreBin, "--list", path).Output() //nolint:gosec // path built here
	if err != nil {
		return fmt.Errorf("reading the archive's table of contents: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("the archive's table of contents is empty")
	}
	return nil
}

// prunePreMigrationBackups removes every *.dump in dir beyond the newest keep.
func prunePreMigrationBackups(dir string, keep int, logger *slog.Logger) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.dump"))
	if err != nil || len(matches) <= keep {
		return
	}
	type entry struct {
		path string
		mod  time.Time
	}
	entries := make([]entry, 0, len(matches))
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		entries = append(entries, entry{path: m, mod: fi.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })
	for _, e := range entries[keep:] {
		if err := os.Remove(e.path); err != nil {
			logger.Warn("could not prune an old pre-migration backup", "path", e.path, "error", err)
			continue
		}
		logger.Info("pruned an old pre-migration backup", "path", e.path)
	}
}
