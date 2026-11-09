-- +goose Up
-- Seasons and episodes carry their own TMDB sub-resources now (issue #68):
-- videos for both, and their one-time credit fetch in the existing credits
-- table under media_type 'season' / 'episode'. The `credits` key in metadata
-- only records that the fetch happened, so a season or episode whose TMDB
-- credits are empty is not re-fetched on every read.

ALTER TABLE seasons ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE episodes ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE episodes DROP COLUMN IF EXISTS metadata;
ALTER TABLE seasons DROP COLUMN IF EXISTS metadata;
