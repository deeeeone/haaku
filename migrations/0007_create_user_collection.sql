-- +migrate Up
-- T3.2: User collection (movies/shows/episodes the user owns).
CREATE TABLE IF NOT EXISTS user_collection (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type   TEXT NOT NULL,           -- 'movie' or 'show'
    media_id     BIGINT NOT NULL,          -- canonical movie or show ID
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(user_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_user_collection_user_media
    ON user_collection (user_id, media_type, media_id);

CREATE INDEX IF NOT EXISTS idx_user_collection_user_collected
    ON user_collection (user_id, collected_at DESC);

-- +migrate Down
DROP TABLE IF EXISTS user_collection;
