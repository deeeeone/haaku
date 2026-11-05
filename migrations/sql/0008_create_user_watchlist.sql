-- +goose Up
-- T3.2: User watchlist (items the user wants to watch).
CREATE TABLE IF NOT EXISTS user_watchlist (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type TEXT NOT NULL,             -- 'movie' or 'show'
    media_id   BIGINT NOT NULL,            -- canonical movie or show ID
    rank       INT NOT NULL DEFAULT 0,
    notes      TEXT,
    listed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(user_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_user_watchlist_user_rank
    ON user_watchlist (user_id, rank ASC);

CREATE INDEX IF NOT EXISTS idx_user_watchlist_user_listed
    ON user_watchlist (user_id, listed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS user_watchlist;
