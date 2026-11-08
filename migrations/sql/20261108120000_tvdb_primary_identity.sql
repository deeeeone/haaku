-- +goose Up
-- TVDB becomes the primary media identity.
--
-- Why: media rows were keyed on a MANDATORY tmdb_id, which cannot express a
-- title TMDB does not carry (and cannot express "we know this show by its TVDB
-- id first", which is what every media server and every TVDB-keyed client
-- speaks). The row keeps tmdb_id as a supplementary LINK; the TVDB id becomes
-- the identity the ingest is keyed on, and slug becomes a first-class column so
-- /movies/{slug} resolution stops depending on an external_ids row.
--
-- tmdb_id is also relaxed from NOT NULL to 0-means-unknown: TMDB coverage of
-- TVDB's catalogue is not complete, and a missing link must not block an ingest.
-- The uniqueness that mattered is preserved as a PARTIAL unique index over the
-- rows that actually carry an id, so any number of titles may have tmdb_id = 0.
--
-- Nothing here rewrites existing rows: a row without a tvdb_id keeps working
-- (external_ids still resolves it), and the columns are the fast path the new
-- ingest writes in addition to those rows.

ALTER TABLE movies ADD COLUMN IF NOT EXISTS tvdb_id INTEGER;
ALTER TABLE movies ADD COLUMN IF NOT EXISTS slug TEXT;
ALTER TABLE shows  ADD COLUMN IF NOT EXISTS tvdb_id INTEGER;
ALTER TABLE shows  ADD COLUMN IF NOT EXISTS slug TEXT;

ALTER TABLE movies ALTER COLUMN tmdb_id DROP NOT NULL;
ALTER TABLE shows  ALTER COLUMN tmdb_id DROP NOT NULL;
ALTER TABLE movies ALTER COLUMN tmdb_id SET DEFAULT 0;
ALTER TABLE shows  ALTER COLUMN tmdb_id SET DEFAULT 0;

-- The inline UNIQUE from 0002 became a constraint-backed index; drop it and
-- replace it with a partial one.
ALTER TABLE movies DROP CONSTRAINT IF EXISTS movies_tmdb_id_key;
ALTER TABLE shows  DROP CONSTRAINT IF EXISTS shows_tmdb_id_key;
DROP INDEX IF EXISTS movies_tmdb_id_key;
DROP INDEX IF EXISTS shows_tmdb_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_movies_tmdb_id ON movies (tmdb_id) WHERE tmdb_id IS NOT NULL AND tmdb_id > 0;
CREATE UNIQUE INDEX IF NOT EXISTS uq_shows_tmdb_id  ON shows  (tmdb_id) WHERE tmdb_id IS NOT NULL AND tmdb_id > 0;
CREATE UNIQUE INDEX IF NOT EXISTS uq_movies_tvdb_id ON movies (tvdb_id) WHERE tvdb_id IS NOT NULL AND tvdb_id > 0;
CREATE UNIQUE INDEX IF NOT EXISTS uq_shows_tvdb_id  ON shows  (tvdb_id) WHERE tvdb_id IS NOT NULL AND tvdb_id > 0;

CREATE INDEX IF NOT EXISTS idx_movies_slug ON movies (slug) WHERE slug IS NOT NULL AND slug <> '';
CREATE INDEX IF NOT EXISTS idx_shows_slug  ON shows  (slug) WHERE slug IS NOT NULL AND slug <> '';

-- external_ids already carries UNIQUE (media_type, source, external_id) from
-- 0002, which is the key the TVDB ingest upserts on — no change needed here.

-- +goose Down
DROP INDEX IF EXISTS idx_shows_slug;
DROP INDEX IF EXISTS idx_movies_slug;
DROP INDEX IF EXISTS uq_shows_tvdb_id;
DROP INDEX IF EXISTS uq_movies_tvdb_id;
DROP INDEX IF EXISTS uq_shows_tmdb_id;
DROP INDEX IF EXISTS uq_movies_tmdb_id;

ALTER TABLE shows  ALTER COLUMN tmdb_id SET NOT NULL;
ALTER TABLE movies ALTER COLUMN tmdb_id SET NOT NULL;
ALTER TABLE shows  DROP COLUMN IF EXISTS slug;
ALTER TABLE shows  DROP COLUMN IF EXISTS tvdb_id;
ALTER TABLE movies DROP COLUMN IF EXISTS slug;
ALTER TABLE movies DROP COLUMN IF EXISTS tvdb_id;
