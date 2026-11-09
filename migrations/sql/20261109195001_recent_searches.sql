-- +goose Up
-- recent_searches: Trakt's global recent-search trend store (issue #70).
--
-- This is deliberately NOT per-user history: Trakt's POST /search/recent/ and
-- GET /search/recent_by_id/global/{type} aggregate what everyone searched for.
-- A row is one (item, query) pair; a repeat search increments count and bumps
-- last_searched_at rather than inserting a second row.

CREATE TABLE IF NOT EXISTS recent_searches (
    id BIGSERIAL PRIMARY KEY,
    media_type TEXT NOT NULL,
    media_id BIGINT NOT NULL,
    query TEXT NOT NULL,
    count INT NOT NULL DEFAULT 1,
    last_searched_at TIMESTAMPTZ NOT NULL
);

-- The upsert key: one counter per item and normalised query.
CREATE UNIQUE INDEX IF NOT EXISTS uq_recent_searches_item_query
    ON recent_searches (media_type, media_id, query);

-- The trending read orders by summed count within a media type.
CREATE INDEX IF NOT EXISTS idx_recent_searches_trending
    ON recent_searches (media_type, count DESC);

-- +goose Down
DROP TABLE IF EXISTS recent_searches;
