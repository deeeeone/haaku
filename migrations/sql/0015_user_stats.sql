-- +goose Up
-- Per-user computed stats (T3.8). Populated by stats.ComputeUserStats.
-- Cached via user_version keying; recomputed on demand if stale > 1h.

CREATE TABLE IF NOT EXISTS user_stats (
    user_id         TEXT PRIMARY KEY,
    plays           BIGINT NOT NULL DEFAULT 0,
    minutes         BIGINT NOT NULL DEFAULT 0,
    unique_movies   INT NOT NULL DEFAULT 0,
    unique_shows    INT NOT NULL DEFAULT 0,
    unique_episodes INT NOT NULL DEFAULT 0,
    ratings_count   INT NOT NULL DEFAULT 0,
    computed_at     TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS user_stats;
