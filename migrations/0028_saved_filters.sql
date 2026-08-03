-- 0028_saved_filters: Saved UI filters per user per section (Phase 5: T5.x)
-- Users save custom filter configurations for discovery, lists, search, etc.

CREATE TABLE IF NOT EXISTS saved_filters (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    section TEXT NOT NULL,
    name TEXT NOT NULL,
    filter JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_saved_filters_user_section ON saved_filters(user_id, section);
