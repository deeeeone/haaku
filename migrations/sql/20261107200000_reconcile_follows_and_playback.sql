-- +goose Up
-- Reconcile two tables whose applied schema drifted from the models and the
-- queries that read them.
--
-- user_follows: 0005 created the table with `followed_id` and no `approved_at`.
-- 0021 then used CREATE TABLE IF NOT EXISTS, which was a no-op on every
-- database that had already run 0005, so the `following_id` / `approved_at`
-- columns the social handlers use never existed.
--
-- user_playback: 0012 created the table without the `id` column the playback
-- handlers use as a stable pagination tie-breaker (the same `id` the tests
-- document as coming from the SQL migrations).

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_follows'
          AND column_name = 'followed_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_follows'
          AND column_name = 'following_id'
    ) THEN
        ALTER TABLE user_follows RENAME COLUMN followed_id TO following_id;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE user_follows ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ;

-- Follows created before the approval flow existed were effective immediately.
UPDATE user_follows SET approved_at = created_at WHERE approved_at IS NULL;

ALTER INDEX IF EXISTS idx_user_follows_followed RENAME TO idx_user_follows_following;

-- The playback handlers order by this column. Existing rows are backfilled by
-- the sequence when the column is added.
ALTER TABLE user_playback ADD COLUMN IF NOT EXISTS id BIGSERIAL PRIMARY KEY;

-- +goose Down
ALTER TABLE user_playback DROP COLUMN IF EXISTS id;

ALTER INDEX IF EXISTS idx_user_follows_following RENAME TO idx_user_follows_followed;

ALTER TABLE user_follows DROP COLUMN IF EXISTS approved_at;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_follows'
          AND column_name = 'following_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'user_follows'
          AND column_name = 'followed_id'
    ) THEN
        ALTER TABLE user_follows RENAME COLUMN following_id TO followed_id;
    END IF;
END $$;
-- +goose StatementEnd
