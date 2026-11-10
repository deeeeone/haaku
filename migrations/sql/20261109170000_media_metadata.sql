-- +goose Up
-- Per-item provider metadata that is not a first-class column.
--
-- Issue #68 serves Trakt's per-item sub-resources (videos, releases,
-- translations, studios, season info) from data we did not store. Each is a
-- single upstream payload for one (media_type, media_id, kind) tuple, so they
-- share one cache table rather than one table per resource. A row is written
-- the first time an item is asked for (fetch -> store) and every later request
-- is served from here, so the provider is hit at most once per item per kind
-- instead of on every request.
--
-- media_type is movie | show | season | episode and media_id is our own row id
-- for that table. kind names the sub-resource. payload holds the already
-- contract-shaped JSON, so serving a row is a decode-and-emit, not a second
-- mapping pass.
CREATE TABLE IF NOT EXISTS media_metadata (
    id BIGSERIAL PRIMARY KEY,
    media_type TEXT NOT NULL,
    media_id BIGINT NOT NULL,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT media_metadata_scope_key UNIQUE (media_type, media_id, kind)
);

-- +goose Down
DROP TABLE IF EXISTS media_metadata;