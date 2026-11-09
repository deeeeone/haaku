-- +goose Up
-- 20261109120000_user_reports: account reports (Trakt POST /users/{id}/report).
--
-- ContentReport covers media items and keys the subject by (media_type,
-- media_id BIGINT); a user subject is a string account id, so it needs its own
-- table rather than an overload of content_reports.

CREATE TABLE IF NOT EXISTS user_reports (
    id BIGSERIAL PRIMARY KEY,
    reporter_id TEXT NOT NULL,
    reported_user_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    message TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_reports_reporter_target
    ON user_reports(reporter_id, reported_user_id);
CREATE INDEX IF NOT EXISTS idx_user_reports_target ON user_reports(reported_user_id);

-- +goose Down
DROP TABLE IF EXISTS user_reports;