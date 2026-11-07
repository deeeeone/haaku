package integrations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"trakt-api/internal/database"

	"gorm.io/gorm"
)

const (
	// backfillBatchSize bounds how many rows are read into memory per pass, so
	// the sweep is O(batch) in memory rather than O(table).
	backfillBatchSize = 500
	// backfillMaxRows caps one run so a first start on a very large table cannot
	// hold the advisory lock indefinitely. Anything left over converts on the
	// next start (the sweep is resumable by construction), and the run logs that
	// it stopped early.
	backfillMaxRows = 100000
	// backfillLockKey names the advisory lock that serialises the sweep. Two
	// servers starting at once must not both rewrite the same rows; the second
	// waits, then finds nothing left to do.
	backfillLockKey = "tkiller:integration-token-backfill"
)

// BackfillPlaintextTokens encrypts every integration token still stored as
// plaintext (issue #16 §2: EncryptIfPlaintext was tested but never wired).
//
// Properties this guarantees, each of which the tests exercise:
//
//   - refuse without a key: with no usable root key the process binary already
//     refuses to start (config.Load), and this function refuses too, returning
//     an error rather than writing unreadable/partial values;
//   - idempotent: a row is converted only when its token column is not already
//     an enc:v1: envelope, so a second run converts nothing;
//   - resumable: the sweep is driven by "which rows are still plaintext", so an
//     interrupted run picks up exactly where it stopped with no cursor state;
//   - concurrent-safe: the whole sweep runs under a PostgreSQL advisory lock
//     (database.WithAdvisoryLockStr), so two servers starting at once serialise
//     and the later one finds nothing to do; each update is additionally
//     optimistic (WHERE id = ? AND token = <old>) so a racing writer cannot be
//     clobbered;
//   - bounded: rows are read in batches of backfillBatchSize and a run stops at
//     backfillMaxRows, logging when it does.
//
// It runs in the migrate/api startup path (cmd/api, after database.Migrate) and
// never in worker mode — a worker must not touch the schema or sweep it.
func BackfillPlaintextTokens(ctx context.Context, db *gorm.DB, logger *slog.Logger) (int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if db == nil {
		return 0, errors.New("integrations: no database handle for the token backfill")
	}

	// Build the keys first: a missing or too-short root key must stop the sweep
	// before it writes anything, not midway through a batch.
	ts, err := loadTokenSecrets()
	if err != nil {
		return 0, fmt.Errorf("integration token backfill: refusing to run without a usable root key: %w", err)
	}

	converted := 0
	err = database.WithAdvisoryLockStr(db, func(tx *gorm.DB) error {
		for converted < backfillMaxRows {
			var rows []database.IntegrationToken
			if err := tx.WithContext(ctx).
				Where("token <> '' AND token NOT LIKE ?", "enc:v1:%").
				Order("id ASC").
				Limit(backfillBatchSize).
				Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				return nil
			}
			batchChanged := 0
			for _, r := range rows {
				enc, err := ts.cipher.Encrypt(r.Token, tokenAAD(r.ID))
				if err != nil {
					return err
				}
				res := tx.WithContext(ctx).Model(&database.IntegrationToken{}).
					Where("id = ? AND token = ?", r.ID, r.Token).
					Updates(map[string]any{"token": enc, "token_hash": ts.hash(r.Token)})
				if res.Error != nil {
					return res.Error
				}
				batchChanged += int(res.RowsAffected)
			}
			converted += batchChanged
			if batchChanged == 0 {
				// A non-empty batch that changed nothing means a row's plaintext
				// moved under us. Stop rather than spin forever; a later run
				// resumes against the new value.
				logger.WarnContext(ctx, "integration token backfill made no progress on a batch; stopping so a later run can resume")
				return nil
			}
		}
		return nil
	}, backfillLockKey)

	if err != nil {
		return converted, err
	}
	if converted > 0 {
		logger.InfoContext(ctx, "integration tokens encrypted at rest", "rows", converted)
	}
	if converted >= backfillMaxRows {
		logger.WarnContext(ctx, "integration token backfill hit its per-run cap; the remaining rows will convert on the next start",
			"converted", converted, "cap", backfillMaxRows)
	}
	return converted, nil
}
