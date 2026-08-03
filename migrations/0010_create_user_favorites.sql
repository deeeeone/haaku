-- +migrate Up
-- T3.2: User favorites.
CREATE TABLE IF NOT EXISTS user_favorites (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type   TEXT NOT NULL,           -- 'movie' or 'show'
    media_id     BIGINT NOT NULL,          -- canonical media ID
    rank         INT NOT NULL DEFAULT 0,
    favorited_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(user_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_user_favorites_user_rank
    ON user_favorites (user_id, rank ASC);

CREATE INDEX IF NOT EXISTS idx_user_favorites_user_favorited
    ON user_favorites (user_id, favorited_at DESC);

-- +migrate Down
DROP TABLE IF EXISTS user_favorites;
