package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for the lock session
)

// migrationAcquirePoll is how often a waiting server retries the advisory lock.
const migrationAcquirePoll = 500 * time.Millisecond

// migrationLock holds the session-level advisory lock that serialises the whole
// migration sequence. It lives on a dedicated connection opened outside the GORM
// pool: the work under it (Goose, the backup) uses pool connections, and taking
// the lock from the same pool could deadlock a small pool.
type migrationLock struct {
	db   *sql.DB
	conn *sql.Conn
}

// acquireMigrationLock opens a connection and takes pg_advisory_lock, waiting up
// to wait for another server that is already migrating. wait <= 0 waits
// indefinitely. It returns an error rather than blocking forever when the other
// server does not finish in time.
func acquireMigrationLock(ctx context.Context, dsn string, wait time.Duration, logger *slog.Logger) (*migrationLock, error) {
	if dsn == "" {
		return nil, fmt.Errorf("migrate: no database connection string available for the migration lock")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening the migration lock session: %w", err)
	}
	// One connection, held for the life of the migration: the lock is
	// session-scoped, so it is released when this session ends.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("opening the migration lock connection: %w", err)
	}

	lock := &migrationLock{db: db, conn: conn}
	acquired, err := lock.try(ctx)
	if err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, err
	}
	if acquired {
		return lock, nil
	}

	waitFor := "indefinitely"
	if wait > 0 {
		waitFor = wait.String()
	}
	logger.InfoContext(ctx, "another server is migrating the database; waiting before starting",
		"wait", waitFor, "env", migrateWaitEnv)

	deadline := time.Now().Add(wait) // only used when wait > 0
	ticker := time.NewTicker(migrationAcquirePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			lock.release()
			return nil, fmt.Errorf("waiting for the migration lock: %w", ctx.Err())
		case <-ticker.C:
		}
		if wait > 0 && time.Now().After(deadline) {
			lock.release()
			return nil, fmt.Errorf(
				"another server held the database migration lock for longer than %s; raise %s if its backup and migrations need longer",
				wait, migrateWaitEnv)
		}
		acquired, err := lock.try(ctx)
		if err != nil {
			lock.release()
			return nil, err
		}
		if acquired {
			logger.InfoContext(ctx, "the other server's migration sequence finished; proceeding")
			return lock, nil
		}
	}
}

// try attempts the lock without blocking.
func (l *migrationLock) try(ctx context.Context) (bool, error) {
	var acquired bool
	if err := l.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", migrationLockID).Scan(&acquired); err != nil {
		return false, fmt.Errorf("taking the database migration lock: %w", err)
	}
	return acquired, nil
}

// release unlocks and closes the session. Closing releases the lock even if the
// explicit unlock fails, which is why it is the fallback. Safe to call twice.
func (l *migrationLock) release() {
	if l == nil {
		return
	}
	if l.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = l.conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
		cancel()
		_ = l.conn.Close()
		l.conn = nil
	}
	if l.db != nil {
		_ = l.db.Close()
		l.db = nil
	}
}
