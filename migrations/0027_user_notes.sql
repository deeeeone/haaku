-- 0027_user_notes: Personal notes on media items (Phase 5: T5.x)
-- Users can attach private text notes to any media item. One note per user per media.

CREATE TABLE IF NOT EXISTS user_notes (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    media_type TEXT NOT NULL,
    media_id BIGINT NOT NULL,
    note TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_notes_user_media ON user_notes(user_id, media_type, media_id);
CREATE INDEX IF NOT EXISTS idx_user_notes_user_created ON user_notes(user_id, created_at DESC);
