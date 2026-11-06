package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"trakt-api/migrations"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The migration runner.
//
// The binary carries its own schema (migrations/embed.go) and applies it at
// startup, so a fresh deployment never serves against an empty database and no
// human has to remember to run a tool. The sequence is:
//
//  1. take a PostgreSQL advisory lock, so two servers starting at once do not
//     race (the second waits for the first rather than applying the same SQL);
//  2. seed the Goose ledger from the legacy numbered `schema_versions` table, so
//     an existing install bootstraps instead of replaying old migrations;
//  3. check whether anything is pending (a no-op when it is not);
//  4. on a database that already has a schema, take a pg_dump safety copy and
//     refuse to continue if it fails;
//  5. apply every pending migration with Goose, under a timeout.
//
// Only the API and migrate run modes call this (see internal/mode); a worker
// never touches the schema.

// migrationLockID serialises the whole migration sequence. It wraps the legacy
// bootstrap, the pending check, the backup and Goose's own Up, and it has to
// survive across connections, so it is a session-level advisory lock held on a
// dedicated connection outside the pool.
//
// The value spells "TKMIGR" so a stray lock in pg_locks is identifiable.
const migrationLockID int64 = 0x544B4D494752

// gooseLockID is Goose's own session lock ("TKGOOSE"), so an operator running
// the Goose CLI by hand cannot interleave with a server that is migrating.
const gooseLockID int64 = 0x544B474F4F53

const (
	// migrateTimeoutEnv bounds how long applying migrations may take.
	migrateTimeoutEnv = "TKILLER_MIGRATE_TIMEOUT"
	// migrateWaitEnv bounds how long a server waits for another server's
	// migration sequence before giving up.
	migrateWaitEnv = "TKILLER_MIGRATION_WAIT"
	// skipBackupEnv is the documented escape hatch for a machine without a
	// matching pg_dump (a dev box, a scratch database).
	skipBackupEnv = "TKILLER_SKIP_BACKUP_BEFORE_MIGRATE"
	// backupDirEnv relocates the pre-migration dumps.
	backupDirEnv = "TKILLER_MIGRATION_BACKUP_DIR"

	defaultMigrateTimeout = 20 * time.Minute
	defaultMigrateWait    = 60 * time.Minute
	// defaultBackupDir is relative to the process working directory. Override
	// with TKILLER_MIGRATION_BACKUP_DIR to put dumps on durable storage — a
	// pre-migration copy is only useful if it outlives the container.
	defaultBackupDir = "pre-migrate-backups"
)

// ErrMigrationsNotImplemented is kept as a sentinel so the callers that named it
// keep compiling; Migrate no longer returns it. It is unused by the runner.
//
// Deprecated: the runner is implemented; this remains only for older call sites.
var ErrMigrationsNotImplemented = errors.New("schema migrations are not implemented yet (see issue #14)")

// Migrate applies every pending schema migration to the configured database.
//
// It is the entry point `cmd/api` calls for `TKILLER_MODE=migrate` and
// `--migrate-only`, and the API serves only after it returns nil. A returned
// error means the schema is not the one this binary expects, so the caller must
// refuse to start rather than serve against an unknown database.
func Migrate(ctx context.Context) error {
	db, err := GetDB()
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return migrateTo(ctx, db, defaultMigrateOptions())
}

// migrateOptions carries the knobs the runner needs. Most come from the
// environment; tests override them, in particular FS (a failing migration) and
// BackupDir (a temporary directory).
type migrateOptions struct {
	FS         fs.FS
	Dir        string
	Wait       time.Duration
	Timeout    time.Duration
	BackupDir  string
	SkipBackup bool
	Logger     *slog.Logger
}

func defaultMigrateOptions() migrateOptions {
	return migrateOptions{
		FS:         migrations.FS,
		Dir:        "sql",
		Wait:       migrationWait(),
		Timeout:    migrationTimeout(),
		BackupDir:  migrationBackupDir(),
		SkipBackup: skipBackupBeforeMigrate(),
		Logger:     slog.Default(),
	}
}

// migrateTo is Migrate's body with an explicit database handle. It is not
// exported: production goes through Migrate with the singleton, and tests build
// their own handle so a test never has to run InitDB's AutoMigrate first.
func migrateTo(ctx context.Context, db *gorm.DB, opts migrateOptions) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if opts.FS == nil {
		opts.FS = migrations.FS
	}
	if opts.Dir == "" {
		opts.Dir = "sql"
	}

	dsn := dsnFromDB(db)
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}

	// 1. Serialise the entire sequence under one advisory lock.
	wrap, err := acquireMigrationLock(ctx, dsn, opts.Wait, logger)
	if err != nil {
		return err
	}
	defer wrap.release()

	// 2. Seed the Goose ledger from the legacy numbering before anything reads
	//    the ledger, so an old install does not replay what it already applied.
	if err := bootstrapLegacyLedger(ctx, wrap.conn, logger); err != nil {
		return err
	}

	// 3. Anything pending?
	migrationFS, err := migrationSubFS(opts.FS, opts.Dir)
	if err != nil {
		return err
	}
	provider, err := newGooseProvider(dsn, migrationFS)
	if err != nil {
		return err
	}
	// Closing the provider closes its own connection pool, never the caller's.
	defer func() { _ = provider.Close() }()

	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("checking for pending migrations: %w", err)
	}
	if !pending {
		logger.InfoContext(ctx, "database schema is up to date")
		return nil
	}

	// 4. Back up a database that already has a schema before changing it. A
	//    brand-new database has nothing to lose and no backup is taken.
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("reading the current schema version: %w", err)
	}
	switch {
	case current == 0:
		logger.InfoContext(ctx, "new database; no pre-migration backup needed")
	case opts.SkipBackup:
		logger.WarnContext(ctx, "skipping the pre-migration backup",
			"env", skipBackupEnv, "reason", "explicitly requested")
	default:
		path, err := takePreMigrationBackup(ctx, dsn, opts.BackupDir, logger)
		if err != nil {
			return err
		}
		logger.InfoContext(ctx, "pre-migration backup written", "path", path)
	}

	// 5. Apply, bounded by the migration timeout.
	upCtx, cancel := withTimeout(ctx, opts.Timeout)
	defer cancel()
	if _, err := provider.Up(upCtx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	logger.InfoContext(ctx, "database migrations applied", "to_version", latestPending(migrationFS))
	return nil
}

// dsnFromDB recovers the connection string from a GORM handle opened with
// gorm.io/driver/postgres, so the lock session and pg_dump do not have to
// re-read the environment. Falls back to DATABASE_URL when the dialector is not
// a Postgres one.
func dsnFromDB(db *gorm.DB) string {
	if db == nil || db.Config == nil {
		return ""
	}
	if d, ok := db.Dialector.(*pgdriver.Dialector); ok {
		return d.DSN
	}
	return ""
}

func migrationSubFS(fsys fs.FS, dir string) (fs.FS, error) {
	if dir == "" || dir == "." {
		return fsys, nil
	}
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("opening migration directory %q: %w", dir, err)
	}
	return sub, nil
}

// newGooseProvider builds a Goose provider over migrationFS on its own
// connection pool, opened from dsn.
//
// It must not borrow the caller's *sql.DB: Goose's Provider.Close closes the
// database it was given, which would rip the pool out from under GORM and every
// later query would fail with "database is closed". It is given a session
// locker so the Goose CLI cannot interleave with a running server.
func newGooseProvider(dsn string, migrationFS fs.FS) (*goose.Provider, error) {
	if dsn == "" {
		return nil, fmt.Errorf("migrate: no database connection string available for the migration provider")
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the migration provider connection: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(gooseLockID))
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("creating the goose migration lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		sqlDB,
		migrationFS,
		goose.WithSessionLocker(locker),
		// Legacy numbers and timestamped numbers coexist; a migration applied
		// out of tag order must not be treated as an error.
		goose.WithAllowOutofOrder(true),
	)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("creating the goose migration provider: %w", err)
	}
	return provider, nil
}

// latestPending reports the highest migration version on disk, for logging.
func latestPending(fsys fs.FS) int64 {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return 0
	}
	var max int64
	for _, name := range names {
		v, err := goose.NumericComponent(name)
		if err != nil {
			continue
		}
		if v > max {
			max = v
		}
	}
	return max
}

// --- environment ---

// migrationTimeout returns how long applying migrations may take, from
// TKILLER_MIGRATE_TIMEOUT. Zero or negative disables the deadline. An
// unparseable value falls back to the default so a typo cannot silently remove
// the cap.
func migrationTimeout() time.Duration {
	return durationEnv(migrateTimeoutEnv, defaultMigrateTimeout, true)
}

// migrationWait returns how long a server waits for another server's migration
// sequence, from TKILLER_MIGRATION_WAIT. Zero or negative waits indefinitely.
func migrationWait() time.Duration {
	return durationEnv(migrateWaitEnv, defaultMigrateWait, false)
}

func migrationBackupDir() string {
	if v := strings.TrimSpace(os.Getenv(backupDirEnv)); v != "" {
		return v
	}
	return defaultBackupDir
}

func skipBackupBeforeMigrate() bool {
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(skipBackupEnv)))
	return err == nil && v
}

func durationEnv(key string, def time.Duration, defaultWhenUnset bool) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		if defaultWhenUnset {
			return def
		}
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("invalid duration; using default",
			"env", key, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return d
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d > 0 {
		return context.WithTimeout(ctx, d)
	}
	return context.WithCancel(ctx)
}

// queryer is the subset of *sql.Conn / *sql.DB the ledger helpers use.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
