-- +goose Up
-- T3.1: User watch history with deduplication.
CREATE TABLE IF NOT EXISTS user_history (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type  TEXT NOT NULL,            -- 'movie' or 'episode'
    media_id    BIGINT NOT NULL,           -- canonical movie or episode ID
    season_id   BIGINT,                   -- NULL for movies
    episode_id  BIGINT,                   -- NULL for movies
    watched_at  TIMESTAMPTZ NOT NULL,
    source      TEXT NOT NULL DEFAULT 'api',
    source_id   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Dedupe: same user+same media+same source = one entry.
-- A unique expression index, not a table UNIQUE constraint: Postgres rejects
-- an expression such as COALESCE(source_id, '') inside a UNIQUE table
-- constraint ("syntax error at or near (").
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_history_dedupe
    ON user_history (user_id, media_type, media_id, source, COALESCE(source_id, ''));

CREATE INDEX IF NOT EXISTS idx_user_history_user_watched
    ON user_history (user_id, watched_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_history_user_media
    ON user_history (user_id, media_type, media_id);

-- +goose Down
DROP TABLE IF EXISTS user_history;
