-- +migrate Up
-- User playback positions (T3.3): resume points for paused content.
CREATE TABLE IF NOT EXISTS user_playback (
    user_id         TEXT NOT NULL,
    media_type      TEXT NOT NULL,
    media_id        BIGINT NOT NULL,
    progress        DOUBLE PRECISION NOT NULL DEFAULT 0,
    paused_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_playback_user ON user_playback (user_id);

-- +migrate Down
DROP TABLE IF EXISTS user_playback;