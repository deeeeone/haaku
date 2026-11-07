-- +goose Up
-- sync_sessions.errors becomes the typed object the engine writes (issue #6).
--
-- 0016_sync_sessions.sql created the column JSONB DEFAULT '[]'::jsonb (an
-- array), which cannot hold the per-item report a worker must be able to write:
--
--   {
--     "counts": { "added": 3, "not_found": 1, "duplicate": 1, "error": 0 },
--     "items":  [ { "item_id": 7, "seq": 2, "code": "not_found",
--                   "message": "...", "ids": { "tmdb": 550 } } ]
--   }
--
-- Normalize the default and any legacy placeholder array to an empty object.
-- Forward-only and lossless: every existing row held only the empty-array
-- placeholder, because nothing could write the column before the engine landed.
ALTER TABLE sync_sessions ALTER COLUMN errors SET DEFAULT '{}'::jsonb;

UPDATE sync_sessions
   SET errors = '{}'::jsonb
 WHERE errors IS NULL OR jsonb_typeof(errors) = 'array';

-- +goose Down
ALTER TABLE sync_sessions ALTER COLUMN errors SET DEFAULT '[]'::jsonb;
