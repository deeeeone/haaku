-- +migrate Up
-- Activity feed (T4.6). Fan-out activity entries from Redis Stream events.

CREATE TABLE IF NOT EXISTS activity_feed (
    id          BIGSERIAL PRIMARY KEY,
    actor_id    TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    media_type  TEXT,
    media_id    BIGINT,
    payload     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_activity_actor_created ON activity_feed (actor_id, created_at DESC);

-- +migrate Down
DROP TABLE IF EXISTS activity_feed;
