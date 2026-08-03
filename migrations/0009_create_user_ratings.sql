-- +migrate Up
-- T3.2: User ratings (1-10 scale).
CREATE TABLE IF NOT EXISTS user_ratings (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type TEXT NOT NULL,             -- 'movie' or 'show' or 'season' or 'episode'
    media_id   BIGINT NOT NULL,            -- canonical media ID
    rating     INT NOT NULL CHECK (rating >= 1 AND rating <= 10),
    rated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(user_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_user_ratings_user_rated
    ON user_ratings (user_id, rated_at DESC);

-- +migrate Down
DROP TABLE IF EXISTS user_ratings;
