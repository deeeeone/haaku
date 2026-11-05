-- +goose Up
-- Notifications (T4.7). Per-user notification inbox.

CREATE TABLE IF NOT EXISTS notifications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('follow','follow_request','reply','list_invite','import_complete')),
    actor_id    TEXT,
    payload     JSONB NOT NULL DEFAULT '{}',
    read        BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_created ON notifications (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON notifications (user_id, read, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS notifications;
