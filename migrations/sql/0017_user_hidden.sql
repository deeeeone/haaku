-- +goose Up
-- Hidden items (T3.10). Per-user hidden media across sections:
-- calendar, progress_watched, progress_collected, recommendations, comments, dropped.

CREATE TABLE IF NOT EXISTS user_hidden (
    user_id     TEXT NOT NULL,
    section     TEXT NOT NULL,
    media_type  TEXT NOT NULL,
    media_id    BIGINT NOT NULL,
    hidden_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, section, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_user_hidden_user_section ON user_hidden (user_id, section);

-- +goose Down
DROP TABLE IF EXISTS user_hidden;
