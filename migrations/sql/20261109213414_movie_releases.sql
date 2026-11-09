-- +goose Up
-- Dated movie releases, materialised from TMDB's release_dates payload so the
-- DVD/streaming calendars can range-query by date instead of scanning every
-- movie's metadata JSONB (issue #69). movies.metadata["releases"] stays the
-- canonical stored payload; this table is the same data, one row per release.
--
-- release_type is Trakt's vocabulary, not TMDB's numeric code:
-- theatrical | digital | physical | premiere | limited | tv | unknown.

CREATE TABLE IF NOT EXISTS movie_releases (
    id BIGSERIAL PRIMARY KEY,
    movie_id BIGINT NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    country TEXT NOT NULL,
    release_type TEXT NOT NULL,
    release_date DATE NOT NULL,
    certification TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_releases_unique
    ON movie_releases(movie_id, country, release_type, release_date);
CREATE INDEX IF NOT EXISTS idx_movie_releases_type_date
    ON movie_releases(release_type, release_date);

-- +goose Down
DROP TABLE IF EXISTS movie_releases;
