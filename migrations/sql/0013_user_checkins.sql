-- +goose Up
-- User checkins (T3.5): one active checkin per user at a time.
-- Advisory lock: pg_advisory_xact_lock(hashtext(user_id)).
CREATE TABLE IF NOT EXISTS user_checkins (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    media_type      TEXT NOT NULL,
    media_id        BIGINT NOT NULL,
    season_id       BIGINT,
    episode_id      BIGINT,
    message         TEXT,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_user_checkins UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_checkins_user ON user_checkins (user_id);
CREATE INDEX IF NOT EXISTS idx_checkins_expires ON user_checkins (expires_at);

-- +goose Down
DROP TABLE IF EXISTS user_checkins;