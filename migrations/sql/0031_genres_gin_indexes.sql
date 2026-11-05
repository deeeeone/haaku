-- +goose Up
-- GIN genre-containment indexes for personalized recommendations.
--
-- The personalized /v1/recommendations/movies|shows queries match media having
-- ANY of the user's top genres. Without a GIN index on movies.genres /
-- shows.genres (jsonb arrays, created in migration 0002), each request expands
-- the full table via JOIN jsonb_array_elements_text(genres) before filtering.
-- These GIN indexes let the queries use the array-contains operator (?|) so
-- Postgres filters directly against the indexed jsonb array.
--
-- Created on request of recommendations.md "Databases — genre containment
-- index" (issue #1). The queries are rewritten to use movies.genres ?| ? /
-- shows.genres ?| ? in the same commit.
CREATE INDEX IF NOT EXISTS idx_movies_genres_gin ON movies USING GIN (genres);
CREATE INDEX IF NOT EXISTS idx_shows_genres_gin  ON shows  USING GIN (genres);

-- +goose Down
DROP INDEX IF EXISTS idx_shows_genres_gin;
DROP INDEX IF EXISTS idx_movies_genres_gin;
