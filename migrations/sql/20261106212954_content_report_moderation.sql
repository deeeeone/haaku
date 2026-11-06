-- +goose Up
-- Give a content report a terminal state and an audit record of the moderation
-- action (issue #10 §2). Migration 0029 created content_reports with no
-- lifecycle columns, so a report could be filed but never actioned — the
-- moderation queue that /v1/reports serves has nothing to move a row out of
-- "open" without these.
--
-- status          open | resolved | dismissed. Defaults 'open' so every
--                 pre-existing row (all of them unanswered) is open.
-- resolution_note optional free-form note from the acting moderator.
-- resolved_by     user id of the acting moderator (the audit trail).
-- resolved_at     when the terminal state was set.
-- updated_at      last touch: creation, or a reopen after a dismissal.
--
-- No column is added for a reported-content state (hidden/removed): takedown
-- is deliberately out of scope here (see reports.md), and inventing a content
-- state the schema does not model would be a change to the content tables, not
-- the report lifecycle.
ALTER TABLE content_reports ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open';
ALTER TABLE content_reports ADD COLUMN IF NOT EXISTS resolution_note TEXT NOT NULL DEFAULT '';
ALTER TABLE content_reports ADD COLUMN IF NOT EXISTS resolved_by TEXT;
ALTER TABLE content_reports ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ;
ALTER TABLE content_reports ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- The moderation queue filters by status; keep that lookup cheap.
CREATE INDEX IF NOT EXISTS idx_content_reports_status ON content_reports(status);

-- +goose Down
DROP INDEX IF EXISTS idx_content_reports_status;
ALTER TABLE content_reports DROP COLUMN IF EXISTS updated_at;
ALTER TABLE content_reports DROP COLUMN IF EXISTS resolved_at;
ALTER TABLE content_reports DROP COLUMN IF EXISTS resolved_by;
ALTER TABLE content_reports DROP COLUMN IF EXISTS resolution_note;
ALTER TABLE content_reports DROP COLUMN IF EXISTS status;
