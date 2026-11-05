-- +goose Up
-- User follows and blocks tables (T4.4). See 02-architecture/data-model.md.

CREATE TABLE IF NOT EXISTS user_follows (
    follower_id  TEXT NOT NULL,
    following_id TEXT NOT NULL,
    approved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, following_id)
);

CREATE TABLE IF NOT EXISTS user_blocks (
    blocker_id TEXT NOT NULL,
    blocked_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id)
);

-- +goose Down
DROP TABLE IF EXISTS user_blocks;
DROP TABLE IF EXISTS user_follows;
