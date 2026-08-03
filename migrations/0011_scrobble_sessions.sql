-- +migrate Up
-- Scrobble sessions (T3.4): tracks watching sessions — start, pause, stop, scrobble.
-- Advisory lock: pg_advisory_xact_lock(hashtext(user_id), media_id) before writes.
CREATE TABLE IF NOT EXISTS scrobble_sessions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    media_type      TEXT NOT NULL,
    media_id        BIGINT NOT NULL,
    season_id       BIGINT,
    episode_id      BIGINT,
    action          TEXT NOT NULL CHECK (action IN ('start', 'pause', 'stop', 'scrobble')),
    progress        DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_scrobble_user_media ON scrobble_sessions (user_id, media_type, media_id);
CREATE INDEX IF NOT EXISTS idx_scrobble_user_action ON scrobble_sessions (user_id, action);
CREATE INDEX IF NOT EXISTS idx_scrobble_created_at ON scrobble_sessions (created_at);

-- +migrate Down
DROP TABLE IF EXISTS scrobble_sessions;