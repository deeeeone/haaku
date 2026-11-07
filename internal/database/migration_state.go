package database

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"sync"

	"github.com/haaku-tv/haaku/migrations"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// Migration-state reporting for /readyz (issue #14).
//
// /readyz used to answer "ready" whenever a *gorm.DB handle existed, which is a
// liveness statement dressed up as readiness: a binary pointed at a database it
// never migrated, or one whose migrations have not run yet, still answered 200.
// This file reads the Goose ledger (goose_db_version) and compares it with the
// migration files the binary actually carries, so /readyz can say whether the
// schema is the one this binary expects.
//
// The expected version is DERIVED from the embedded migration filenames at
// runtime — never a hardcoded number. This is the same derivation
// scripts/check-migration-ledger.sh and the docker smoke job use; a stale
// constant in one place would fail the moment a migration lands anywhere.

// MigrationState describes how the schema recorded in the Goose ledger compares
// with the migrations this binary ships. Every field is reported on /readyz so
// an operator can see *why* a deployment is not ready, not just that it is not.
type MigrationState struct {
	// Version is the newest applied migration version (0 = nothing applied).
	Version int64 `json:"version"`
	// Newest is the newest migration version shipped in this binary.
	Newest int64 `json:"newest"`
	// Applied is the number of applied ledger rows (Goose's version-0 marker
	// excluded).
	Applied int `json:"applied"`
	// Expected is the number of migration files this binary ships.
	Expected int `json:"expected"`
	// Pending is how many shipped migrations are newer than Version.
	Pending int `json:"pending"`
	// Migrating is true when another session holds the migration advisory lock,
	// i.e. a migration is being applied right now (or a run died holding it).
	Migrating bool `json:"migrating"`
	// UpToDate is true only when the ledger matches this binary exactly: the
	// newest applied version is the newest shipped version AND there is exactly
	// one applied row per shipped file. A rolled-back or orphaned row makes this
	// false, so /readyz does not report ready against a schema it cannot explain.
	UpToDate bool `json:"up_to_date"`
}

// MigrationVersions returns the migration versions this binary ships, ascending.
// It reads the embedded files once and caches the result; the returned slice is
// a copy, so a caller cannot corrupt the cache.
func MigrationVersions() []int64 {
	migrationVersionsOnce.Do(loadMigrationVersions)
	return append([]int64(nil), migrationVersions...)
}

// LatestMigrationVersion returns the highest migration version shipped in this
// binary, or 0 when there are none.
func LatestMigrationVersion() int64 {
	versions := MigrationVersions()
	if len(versions) == 0 {
		return 0
	}
	return versions[len(versions)-1]
}

var (
	migrationVersionsOnce sync.Once
	migrationVersions     []int64
)

func loadMigrationVersions() {
	names, err := fs.Glob(migrations.FS, "sql/*.sql")
	if err != nil {
		return
	}
	seen := make(map[int64]struct{}, len(names))
	for _, name := range names {
		v, err := goose.NumericComponent(path.Base(name))
		if err != nil {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		migrationVersions = append(migrationVersions, v)
	}
	sort.Slice(migrationVersions, func(i, j int) bool { return migrationVersions[i] < migrationVersions[j] })
}

// ReadMigrationState reports how the database's Goose ledger compares with the
// migrations this binary ships.
//
// It returns an error only when the database cannot be read at all — an
// unreachable server, a closed pool, a ledger query that fails for a reason
// other than the table not existing yet. A reachable database with no ledger
// (a fresh install before its first migration) is not an error: it is reported
// as Version 0, behind, which is the honest answer. The caller must treat an
// error as "not ready", never as "assume current".
func ReadMigrationState(ctx context.Context, db *gorm.DB) (MigrationState, error) {
	versions := MigrationVersions()
	state := MigrationState{Newest: LatestMigrationVersion(), Expected: len(versions)}
	if db == nil {
		return state, fmt.Errorf("database handle is nil")
	}

	q := db.WithContext(ctx)

	// Probe connectivity first: a *gorm.DB handle outlives the connection it was
	// opened over, so the handle existing says nothing about the server being
	// reachable. A closed pool surfaces here, before any ledger query, so an
	// unreachable database is reported as such rather than mistaken for a fresh
	// one.
	var probe int
	if err := q.Raw("SELECT 1").Scan(&probe).Error; err != nil {
		return state, fmt.Errorf("probing the database: %w", err)
	}

	// Read the ledger if it exists. A database without one has applied nothing,
	// which the zero values below already describe — not an error, and not
	// "current".
	if q.Migrator().HasTable(gooseVersionTable) {
		// COALESCE keeps the scan target a plain int64: on an empty ledger max()
		// is NULL, which would need sql.NullInt64 otherwise.
		var version int64
		if err := q.Raw("SELECT COALESCE(max(version_id), 0) FROM " + gooseVersionTable +
			" WHERE is_applied AND version_id > 0").Scan(&version).Error; err != nil {
			return state, fmt.Errorf("reading the migration ledger: %w", err)
		}
		var applied int64
		if err := q.Raw("SELECT count(*) FROM " + gooseVersionTable +
			" WHERE is_applied AND version_id > 0").Scan(&applied).Error; err != nil {
			return state, fmt.Errorf("reading the migration ledger: %w", err)
		}
		state.Version = version
		state.Applied = int(applied)
	}

	state.Pending = countNewerVersions(versions, state.Version)
	state.UpToDate = state.Version == state.Newest && state.Applied == state.Expected

	// Only worth asking "is a migration running?" when the ledger does not match
	// this binary; on a current schema the answer cannot change the verdict.
	// (A migration's first act is to create the ledger table, so this fires even
	// in the window before the first row lands.)
	if !state.UpToDate {
		state.Migrating = migrationInProgress(ctx, db)
	}
	return state, nil
}

// countNewerVersions returns how many shipped versions are strictly newer than
// applied. versions is ascending.
func countNewerVersions(versions []int64, applied int64) int {
	i := sort.Search(len(versions), func(i int) bool { return versions[i] > applied })
	return len(versions) - i
}

// migrationInProgress reports whether any session holds the migration advisory
// lock — that is, a migration is being applied right now.
//
// The check is best-effort and dialect-agnostic: pg_locks exists on PostgreSQL
// but not on sqlite (the handler-test database), where the query simply errors
// and this reports "not migrating". That is the correct answer there — nothing
// migrates a sqlite test fixture — and "migrating" only ever downgrades a
// verdict that is already not-ready, so a missed detection cannot produce a
// lying 200.
func migrationInProgress(ctx context.Context, db *gorm.DB) bool {
	if db == nil {
		return false
	}
	// A 64-bit advisory lock is stored as two 32-bit OIDs: classid holds the high
	// half, objid the low half. Reassemble the key and compare with the lock the
	// migration runner takes (see migrationLockID in migrate.go).
	var held int64
	err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND ((classid::bigint << 32) | objid::bigint) = ?`,
		migrationLockID,
	).Scan(&held).Error
	return err == nil && held > 0
}
