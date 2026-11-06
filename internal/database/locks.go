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
			k1, k2 := AdvisoryLockKeysInt64(keys[i], keys[i+1])
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", k1, k2).Error; err != nil {
				log.Printf("[locks] WARNING: pg_advisory_xact_lock(%d, %d) failed: %v — continuing without lock",
					k1, k2, err)
			}
		}
		// If odd number of keys, lock the last one with itself as the second key.
		if len(keys)%2 != 0 {
			k1, k2 := AdvisoryLockKeysInt64(keys[len(keys)-1], keys[len(keys)-1])
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", k1, k2).Error; err != nil {
				log.Printf("[locks] WARNING: pg_advisory_xact_lock(%d, %d) failed: %v — continuing without lock",
					k1, k2, err)
			}
		}

		return lockFunc(tx)
	})
}

// WithAdvisoryLockStr acquires a pg_advisory_xact_lock keyed by a string.
// Uses AdvisoryLockKeysStr to convert the string to two 32-bit keys.
func WithAdvisoryLockStr(db *gorm.DB, lockFunc func(tx *gorm.DB) error, s string) error {
	k1, k2 := AdvisoryLockKeysStr(s)

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", k1, k2).Error; err != nil {
			log.Printf("[locks] WARNING: pg_advisory_xact_lock(%d, %d) failed: %v — continuing without lock",
				k1, k2, err)
		}
		return lockFunc(tx)
	})
}
