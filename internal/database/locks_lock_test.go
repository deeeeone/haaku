package database

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// These tests need a real PostgreSQL: advisory locks do not exist in sqlite, and
// the defect they guard was an argument-encoding failure only the real driver
// exhibits. They skip when DATABASE_URL is unset, like the migration tests beside
// them. (When the `postgres` build tag convention lands under issue #14, these
// move behind it.)

func pgTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set: skipping real-PostgreSQL advisory-lock test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	return db
}

// TestAdvisoryLockStrActuallyLocks is the regression test for the int4 overflow.
//
// The helper used to pass two uint32 halves to pg_advisory_xact_lock(int4, int4).
// Roughly half of all FNV-1a keys exceed math.MaxInt32, which made the driver
// fail to encode the parameter; the helper logged a warning and returned success
// WITHOUT holding any lock. Nothing noticed, because every caller was content to
// run unlocked.
//
// So this test does not assert "the helper returned nil" — it asserts the lock is
// observable from another session, which is the only thing callers care about.
// The fixture name is checked to still produce an overflowing key, so the test
// fails reliably against the old implementation rather than sometimes.
func TestAdvisoryLockStrActuallyLocks(t *testing.T) {
	db := pgTestDB(t)

	name := "tkiller:test:advisory-lock-overflow"
	held := AdvisoryLockKeyStr(name)

	// The raw halves are what the old code passed as int4s; at least one must be
	// out of signed-32-bit range or this name is not the reproducer.
	k1, k2 := AdvisoryLockKeysStr(name)
	if k1 <= 1<<31-1 && k2 <= 1<<31-1 {
		t.Fatalf("fixture no longer reproduces the int4 overflow (k1=%d k2=%d); pick another name", k1, k2)
	}

	other := pgTestDB(t) // an independent connection is the observer

	taken := false
	err := WithAdvisoryLockStr(db, func(tx *gorm.DB) error {
		var got bool
		if err := other.Raw("SELECT pg_try_advisory_lock(?)", held).Scan(&got).Error; err != nil {
			return err
		}
		// pg_try_advisory_lock would SUCCEED if the helper had not really taken
		// the lock — the exact failure mode being guarded.
		if got {
			taken = true
		}
		return nil
	}, name)
	if err != nil {
		t.Fatalf("WithAdvisoryLockStr: %v", err)
	}
	if taken {
		t.Fatal("the advisory lock was acquirable from another session while held: the helper did not take it")
	}

	// It must be released once the transaction is over, and the probe must not
	// leave a lock of its own behind.
	var free bool
	if err := other.Raw("SELECT pg_try_advisory_lock(?)", held).Scan(&free).Error; err != nil {
		t.Fatalf("post-release probe: %v", err)
	}
	if !free {
		t.Fatal("the advisory lock was still held after the transaction ended")
	}
	if err := other.Exec("SELECT pg_advisory_unlock(?)", held).Error; err != nil {
		t.Fatalf("unlock probe: %v", err)
	}
}

// TestAdvisoryLockStrFailsClosed pins the failure MODE. The old helper logged a
// warning and ran the work unlocked, which turns a benign inability to lock into
// a silent correctness bug. A closed pool makes the lock statement fail for real,
// so this asserts both halves: the error surfaces, and the protected work does
// not run.
func TestAdvisoryLockStrFailsClosed(t *testing.T) {
	db := pgTestDB(t)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}

	called := false
	err = WithAdvisoryLockStr(db, func(tx *gorm.DB) error {
		called = true
		return nil
	}, "tkiller:test:fails-closed")
	if err == nil {
		t.Fatal("WithAdvisoryLockStr returned nil with an unusable database: it must fail closed")
	}
	if called {
		t.Fatal("the locked work ran even though the lock could not be taken")
	}
}
