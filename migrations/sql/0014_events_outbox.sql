-- +goose Up
-- Events outbox for transactional event-driven writes (T3.6).
-- Writes go here in the same transaction as the data mutation, then
-- a background publisher pushes them to Redis Streams.

CREATE TABLE IF NOT EXISTS events_outbox (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    user_id     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    published   BOOLEAN NOT NULL DEFAULT false
);

CREATE INDEX IF NOT EXISTS idx_events_outbox_published_created
    ON events_outbox (published, created_at);

-- +goose Down
DROP TABLE IF EXISTS events_outbox;
