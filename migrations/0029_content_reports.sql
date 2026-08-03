-- 0029_content_reports: Content reporting for moderation (Phase 5: T5.x)
-- Users can report movies, shows, seasons, and episodes with a reason.

CREATE TABLE IF NOT EXISTS content_reports (
    id BIGSERIAL PRIMARY KEY,
    reporter_id TEXT NOT NULL,
    media_type TEXT NOT NULL,
    media_id BIGINT NOT NULL,
    reason TEXT NOT NULL,
    description TEXT DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_reports_reporter_media ON content_reports(reporter_id, media_type, media_id);
CREATE INDEX IF NOT EXISTS idx_content_reports_created ON content_reports(created_at DESC);
