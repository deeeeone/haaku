-- +migrate Up
-- Add settings column and social tables (blocks, follows) for T2.6/T2.7.

ALTER TABLE users ADD COLUMN IF NOT EXISTS settings JSONB DEFAULT '{}'::jsonb;

-- User blocks (for privacy enforcement)
CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);

-- User follows (for privacy enforcement)
CREATE TABLE IF NOT EXISTS user_follows (
    follower_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followed_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followed_id)
);

CREATE INDEX IF NOT EXISTS idx_user_blocks_blocked ON user_blocks (blocked_id);
CREATE INDEX IF NOT EXISTS idx_user_follows_followed ON user_follows (followed_id);

-- +migrate Down
ALTER TABLE users DROP COLUMN IF EXISTS settings;
DROP TABLE IF EXISTS user_follows;
DROP TABLE IF EXISTS user_blocks;
