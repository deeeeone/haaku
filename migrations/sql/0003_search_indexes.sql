-- +goose Up
-- Full-text search indexes for movies, shows, episodes, and people.
-- Uses PostgreSQL tsvector + GIN for fast relevance-ranked search.

-- Movies: searchable on title + original_title.
ALTER TABLE movies ADD COLUMN search_vector tsvector;
UPDATE movies SET search_vector =
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(original_title, '')), 'B');
CREATE INDEX idx_movies_search ON movies USING GIN (search_vector);

-- Trigger to keep movies.search_vector updated on INSERT/UPDATE.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION movies_search_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.original_title, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_movies_search
    BEFORE INSERT OR UPDATE OF title, original_title ON movies
    FOR EACH ROW EXECUTE FUNCTION movies_search_update();

-- Shows: searchable on title + original_title.
ALTER TABLE shows ADD COLUMN search_vector tsvector;
UPDATE shows SET search_vector =
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(original_title, '')), 'B');
CREATE INDEX idx_shows_search ON shows USING GIN (search_vector);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION shows_search_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.original_title, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_shows_search
    BEFORE INSERT OR UPDATE OF title, original_title ON shows
    FOR EACH ROW EXECUTE FUNCTION shows_search_update();

-- Episodes: searchable on title + overview.
ALTER TABLE episodes ADD COLUMN search_vector tsvector;
UPDATE episodes SET search_vector =
    setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(overview, '')), 'B');
CREATE INDEX idx_episodes_search ON episodes USING GIN (search_vector);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION episodes_search_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.overview, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_episodes_search
    BEFORE INSERT OR UPDATE OF title, overview ON episodes
    FOR EACH ROW EXECUTE FUNCTION episodes_search_update();

-- People: searchable on name + biography.
ALTER TABLE people ADD COLUMN search_vector tsvector;
UPDATE people SET search_vector =
    setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(biography, '')), 'B');
CREATE INDEX idx_people_search ON people USING GIN (search_vector);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION people_search_update() RETURNS trigger AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', coalesce(NEW.name, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(NEW.biography, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_people_search
    BEFORE INSERT OR UPDATE OF name, biography ON people
    FOR EACH ROW EXECUTE FUNCTION people_search_update();

-- +goose Down
DROP TRIGGER IF EXISTS trg_movies_search ON movies;
DROP FUNCTION IF EXISTS movies_search_update();
DROP INDEX IF EXISTS idx_movies_search;
ALTER TABLE movies DROP COLUMN IF EXISTS search_vector;

DROP TRIGGER IF EXISTS trg_shows_search ON shows;
DROP FUNCTION IF EXISTS shows_search_update();
DROP INDEX IF EXISTS idx_shows_search;
ALTER TABLE shows DROP COLUMN IF EXISTS search_vector;

DROP TRIGGER IF EXISTS trg_episodes_search ON episodes;
DROP FUNCTION IF EXISTS episodes_search_update();
DROP INDEX IF EXISTS idx_episodes_search;
ALTER TABLE episodes DROP COLUMN IF EXISTS search_vector;

DROP TRIGGER IF EXISTS trg_people_search ON people;
DROP FUNCTION IF EXISTS people_search_update();
DROP INDEX IF EXISTS idx_people_search;
ALTER TABLE people DROP COLUMN IF EXISTS search_vector;
