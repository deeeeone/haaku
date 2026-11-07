package database

import (
	"fmt"
	"hash/fnv"
	"log"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Advisory lock key helpers
// ---------------------------------------------------------------------------

// AdvisoryLockKeysInt64 splits an int64 into two 32-bit unsigned integers
// suitable for pg_advisory_xact_lock(k1, k2). Uses the upper and lower
// 32 bits of the absolute value to avoid sign-extension issues.
func AdvisoryLockKeysInt64(a, b int64) (uint32, uint32) {
	return splitInt64(a), splitInt64(b)
}

// AdvisoryLockKeysStr converts a string into two 32-bit keys using FNV-1a
// hashing. The string is hashed twice: once with a seed of 0 and once with
// a seed of the first hash's value, producing two distinct keys.
//
// NOTE: these are unsigned halves. They are NOT directly passable to
// pg_advisory_xact_lock(int4, int4), which takes signed 32-bit arguments — see
// AdvisoryLockKeyStr for the composable form.
func AdvisoryLockKeysStr(s string) (uint32, uint32) {
	h1 := fnv.New32a()
	h1.Write([]byte(s))
	k1 := h1.Sum32()

	h2 := fnv.New32a()
	_, _ = fmt.Fprintf(h2, "%s:%d", s, k1)
	k2 := h2.Sum32()

	return k1, k2
}

// splitInt64 splits an int64 into two uint32 values representing its
// upper and lower 32 bits. Uses absolute value to keep results in the
// positive uint32 range.
func splitInt64(v int64) uint32 {
	if v < 0 {
		v = -v
	}
	return uint32(v>>32) ^ uint32(v)
}

// AdvisoryLockKeyStr composes a string's two 32-bit halves into the single
// int64 key that the bigint form of pg_advisory_xact_lock expects.
//
// This exists because the two-argument form is a trap: pg_advisory_xact_lock(k1
// int4, k2 int4) takes SIGNED 32-bit arguments, while the halves above are
// uint32. Roughly half of all FNV-1a outputs exceed math.MaxInt32, and passing
// one made the driver fail to encode the parameter:
//
//	ERROR: unable to encode ... into binary format for int4 (OID 23):
//	       2442879765 is greater than maximum value for int4
//
// The old code logged a warning and continued WITHOUT the lock, so the mutual
// exclusion every caller relies on (the migration-time sweeps, the sync engine's
// session claim) was silently absent for those keys. Composing into the bigint
// form removes the signedness question entirely.
func AdvisoryLockKeyStr(s string) int64 {
	k1, k2 := AdvisoryLockKeysStr(s)
	return int64(k1)<<32 | int64(k2)
}

// AdvisoryLockKeyInt64 composes an int64 pair into the single int64 key for the
// bigint form of pg_advisory_xact_lock.
func AdvisoryLockKeyInt64(a, b int64) int64 {
	k1, k2 := AdvisoryLockKeysInt64(a, b)
	return int64(k1)<<32 | int64(k2)
}

// ---------------------------------------------------------------------------
// WithAdvisoryLock — transaction-scoped advisory lock
// ---------------------------------------------------------------------------

// WithAdvisoryLock acquires a pg_advisory_xact_lock inside a transaction
// and runs the provided function. The lock is automatically released when
// the transaction ends (commit or rollback) — no explicit unlock is needed.
//
// Keys are passed as int64 pairs. Each pair is converted into two 32-bit
// keys via AdvisoryLockKeysInt64.
//
// Graceful degradation: if the lock acquisition fails (e.g., Postgres not
// supporting advisory locks in the current context), we log a warning and
// continue — transaction isolation still protects consistency.
func WithAdvisoryLock(db *gorm.DB, lockFunc func(tx *gorm.DB) error, keys ...int64) error {
	if len(keys) == 0 {
		return lockFunc(db)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for i := 0; i+1 < len(keys); i += 2 {
			if err := acquire(tx, AdvisoryLockKeyInt64(keys[i], keys[i+1])); err != nil {
				return err
			}
		}
		// If odd number of keys, lock the last one with itself as the second key.
		if len(keys)%2 != 0 {
			if err := acquire(tx, AdvisoryLockKeyInt64(keys[len(keys)-1], keys[len(keys)-1])); err != nil {
				return err
			}
		}

		return lockFunc(tx)
	})
}

// WithAdvisoryLockStr acquires a pg_advisory_xact_lock keyed by a string.
// Uses AdvisoryLockKeysStr to convert the string to two 32-bit keys.
func WithAdvisoryLockStr(db *gorm.DB, lockFunc func(tx *gorm.DB) error, s string) error {
	key := AdvisoryLockKeyStr(s)

	return db.Transaction(func(tx *gorm.DB) error {
		if err := acquire(tx, key); err != nil {
			return err
		}
		return lockFunc(tx)
	})
}

// acquire takes the transaction-scoped advisory lock on PostgreSQL and FAILS
// CLOSED there.
//
// It used to log a warning and carry on with no lock at all, which conflated two
// very different situations:
//
//  1. the lock statement FAILED on PostgreSQL (the int4 encoding bug above, a
//     permission problem, a connection issue) — proceeding unlocked turns a
//     benign failure into a silent correctness bug in code that exists purely to
//     serialise work; and
//  2. advisory locks DO NOT EXIST on the driver in use (SQLite, which the handler
//     and engine suites run on) — a statement that cannot succeed by construction.
//
// Case 1 returns an error. Case 2 runs the work unlocked and says so at debug
// level: on a dialect without advisory locks the caller is single-process by
// contract (a test binary), and pretending to lock would be the lie the old code
// told. PostgreSQL is the only production driver, and there the lock is real.
func acquire(tx *gorm.DB, key int64) error {
	if !hasAdvisoryLocks(tx) {
		log.Printf("[locks] %s has no advisory locks: running key=%d unlocked (single-process contract)",
			dialectName(tx), key)
		return nil
	}
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error; err != nil {
		return fmt.Errorf("advisory lock %d: %w", key, err)
	}
	return nil
}

// hasAdvisoryLocks reports whether the connection's driver implements
// pg_advisory_xact_lock. Nil-safe so a caller with an oddly-constructed handle
// gets the conservative answer (no locks claimed).
func hasAdvisoryLocks(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres"
}

// dialectName is for log messages only.
func dialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil {
		return "unknown driver"
	}
	return db.Dialector.Name()
}
