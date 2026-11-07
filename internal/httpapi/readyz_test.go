package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// readyzBody is the decoded /readyz response. It is defined here rather than
// reusing the production struct so a rename in the handler cannot silently keep
// these tests green: the JSON tags are asserted against a fresh copy of the
// documented shape.
type readyzBody struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
	Redis  string `json:"redis"`
	// Embedded so an absent "migration" object decodes to the zero value and the
	// assertions below fail loudly rather than panicking.
	Migration struct {
		Version   int64 `json:"version"`
		Newest    int64 `json:"newest"`
		Applied   int   `json:"applied"`
		Expected  int   `json:"expected"`
		Pending   int   `json:"pending"`
		Migrating bool  `json:"migrating"`
		UpToDate  bool  `json:"up_to_date"`
	} `json:"migration"`
}

// newLedgerDB opens an in-memory sqlite database with a Goose-shaped ledger and
// the supplied applied versions (Goose's version-0 marker is added). sqlite is
// enough for the handler wiring: the ledger is read as plain SQL, and the only
// PostgreSQL-specific branch (advisory-lock detection) is exercised against real
// PostgreSQL in the postgres-tagged database suite.
func newLedgerDB(t *testing.T, applied []int64) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	// One connection, so the in-memory database is not re-created per goroutine.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec(`CREATE TABLE goose_db_version (version_id INTEGER, is_applied BOOLEAN)`).Error; err != nil {
		t.Fatalf("create goose ledger: %v", err)
	}
	if err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, true)`).Error; err != nil {
		t.Fatalf("seed goose marker: %v", err)
	}
	for _, v := range applied {
		if err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, true)`, v).Error; err != nil {
			t.Fatalf("insert applied version %d: %v", v, err)
		}
	}
	return db
}

// getReadyz injects db as the process singleton, builds the real router, and
// calls GET /readyz through Fiber's test harness.
func getReadyz(t *testing.T, db *gorm.DB) (int, readyzBody) {
	t.Helper()
	database.SetDBForTest(t, db)
	app := fiber.New()
	RegisterRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil))
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body readyzBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /readyz body: %v", err)
	}
	return resp.StatusCode, body
}

// TestReadyzCurrentSchemaIsReady is the happy path: the ledger matches the
// migrations this binary ships, so /readyz reports ready and the migration block
// says so.
func TestReadyzCurrentSchemaIsReady(t *testing.T) {
	versions := database.MigrationVersions()
	if len(versions) == 0 {
		t.Fatal("no embedded migrations found — the test would pass vacuously")
	}
	db := newLedgerDB(t, versions)

	code, body := getReadyz(t, db)

	if code != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 (body %+v)", code, body)
	}
	if body.Status != "ready" {
		t.Errorf("status word = %q, want %q", body.Status, "ready")
	}
	if body.Reason != "" {
		t.Errorf("reason = %q, want empty on a ready response", body.Reason)
	}
	if !body.Migration.UpToDate {
		t.Errorf("migration.up_to_date = false, want true")
	}
	if body.Migration.Version != database.LatestMigrationVersion() || body.Migration.Newest != database.LatestMigrationVersion() {
		t.Errorf("version/newest = %d/%d, want both %d", body.Migration.Version, body.Migration.Newest, database.LatestMigrationVersion())
	}
	if body.Migration.Pending != 0 {
		t.Errorf("migration.pending = %d, want 0", body.Migration.Pending)
	}
	if body.Migration.Applied != body.Migration.Expected || body.Migration.Expected != len(versions) {
		t.Errorf("applied/expected = %d/%d, want %d/%d", body.Migration.Applied, body.Migration.Expected, len(versions), len(versions))
	}
	// Redis is not configured in tests, so it must be reported and must not have
	// failed readiness.
	if body.Redis != "unavailable" {
		t.Errorf("redis = %q, want %q (soft degradation, not a failure)", body.Redis, "unavailable")
	}
}

// TestReadyzSchemaBehindIsNotReady is the "honest 503" case: the database is
// reachable and healthy, but its schema is missing migrations this binary ships.
func TestReadyzSchemaBehindIsNotReady(t *testing.T) {
	versions := database.MigrationVersions()
	if len(versions) < 2 {
		t.Fatal("need at least two embedded migrations for a behind case")
	}
	behind := versions[:len(versions)-1] // everything but the newest
	db := newLedgerDB(t, behind)

	code, body := getReadyz(t, db)

	if code != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %+v)", code, body)
	}
	if body.Status != "degraded" {
		t.Errorf("status word = %q, want %q", body.Status, "degraded")
	}
	if body.Reason != "database schema is behind" {
		t.Errorf("reason = %q, want %q", body.Reason, "database schema is behind")
	}
	if body.Migration.UpToDate {
		t.Errorf("migration.up_to_date = true on a behind schema")
	}
	if body.Migration.Version != behind[len(behind)-1] {
		t.Errorf("migration.version = %d, want %d", body.Migration.Version, behind[len(behind)-1])
	}
	if body.Migration.Newest != database.LatestMigrationVersion() {
		t.Errorf("migration.newest = %d, want %d", body.Migration.Newest, database.LatestMigrationVersion())
	}
	if body.Migration.Pending != len(versions)-len(behind) {
		t.Errorf("migration.pending = %d, want %d", body.Migration.Pending, len(versions)-len(behind))
	}
}

// TestReadyzFreshDatabaseIsNotReady covers a database that has never been
// migrated: no ledger table at all. It is reachable but behind by every
// migration — a 200 here would be exactly the lie this change removes.
func TestReadyzFreshDatabaseIsNotReady(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	code, body := getReadyz(t, db)

	if code != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on an unmigrated database (body %+v)", code, body)
	}
	if body.Status != "degraded" || body.Reason != "database schema is behind" {
		t.Errorf("status/reason = %q/%q, want degraded/database schema is behind", body.Status, body.Reason)
	}
	if body.Migration.Version != 0 {
		t.Errorf("migration.version = %d, want 0", body.Migration.Version)
	}
	if body.Migration.Pending != body.Migration.Expected || body.Migration.Expected == 0 {
		t.Errorf("migration.pending/expected = %d/%d, want pending == expected > 0", body.Migration.Pending, body.Migration.Expected)
	}
}

// TestReadyzSchemaAheadIsNotReady covers the binary being older than the
// database: the ledger holds a version this binary does not ship. Reporting
// ready here would have the old binary serve against a future schema.
func TestReadyzSchemaAheadIsNotReady(t *testing.T) {
	versions := database.MigrationVersions()
	ahead := append(append([]int64(nil), versions...), database.LatestMigrationVersion()+1)
	db := newLedgerDB(t, ahead)

	code, body := getReadyz(t, db)

	if code != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when the schema is ahead of the binary (body %+v)", code, body)
	}
	if body.Reason != "database schema is ahead of this binary" {
		t.Errorf("reason = %q, want %q", body.Reason, "database schema is ahead of this binary")
	}
	if body.Migration.Version <= body.Migration.Newest {
		t.Errorf("migration.version/newest = %d/%d, want version > newest", body.Migration.Version, body.Migration.Newest)
	}
}

// TestReadyzUnreachableDatabaseIsNotReady is the case the old handler got wrong:
// a *gorm.DB handle exists, so it answered 200, but the database behind it is
// gone. Closing the pool makes every query fail the way an unreachable server
// does, and readiness must follow.
func TestReadyzUnreachableDatabaseIsNotReady(t *testing.T) {
	db := newLedgerDB(t, database.MigrationVersions())

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("underlying sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	code, body := getReadyz(t, db)

	if code != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on an unreachable database (body %+v)", code, body)
	}
	if body.Status != "degraded" {
		t.Errorf("status word = %q, want %q", body.Status, "degraded")
	}
	if body.Reason != "database unreachable" {
		t.Errorf("reason = %q, want %q", body.Reason, "database unreachable")
	}
}

// TestReadyzMissingHandleIsNotReady pins the pre-existing behaviour that must
// survive the change: no handle at all is still 503 "database unavailable".
func TestReadyzMissingHandleIsNotReady(t *testing.T) {
	// No SetDBForTest: the singleton is nil for this test.
	app := fiber.New()
	RegisterRoutes(app)

	resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil))
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body readyzBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /readyz body: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if body.Status != "degraded" || body.Reason != "database unavailable" {
		t.Errorf("status/reason = %q/%q, want degraded/database unavailable", body.Status, body.Reason)
	}
}

// TestReadinessResultMigrating pins the branch that only a live advisory lock
// can reach end to end: a migration in flight is 503 "migrating", not ready.
func TestReadinessResultMigrating(t *testing.T) {
	code, status, _ := readinessResult(database.MigrationState{
		Version: 1, Newest: 2, Applied: 1, Expected: 2, Pending: 1,
		Migrating: true, UpToDate: false,
	})
	if code != fiber.StatusServiceUnavailable || status != "migrating" {
		t.Fatalf("migrating state mapped to %d/%q, want 503/migrating", code, status)
	}
}
