-- +goose Up
-- The watch-now provider catalogue, refreshed from TMDB's
-- /watch/providers/{movie,tv} payloads (issue #69). One row per provider per
-- country per media type: TMDB's catalogue carries a provider's per-country
-- display priority, which is exactly the country availability the
-- /watchnow/sources routes filter on.

CREATE TABLE IF NOT EXISTS watchnow_sources (
    id BIGSERIAL PRIMARY KEY,
    media_type TEXT NOT NULL,
    provider_id INTEGER NOT NULL,
    source TEXT NOT NULL,
    name TEXT NOT NULL,
    country TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    logo_path TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_watchnow_sources_unique
    ON watchnow_sources(media_type, provider_id, country);
CREATE INDEX IF NOT EXISTS idx_watchnow_sources_country
    ON watchnow_sources(country, priority);

-- +goose Down
DROP TABLE IF EXISTS watchnow_sources;
