-- +migrate Up
-- Media tables (movies, shows, seasons, episodes, people, credits, external_ids, genres, networks).
-- See 02-architecture/data-model.md §2.

CREATE TABLE IF NOT EXISTS movies (
    id              BIGSERIAL PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE NOT NULL,
    imdb_id         TEXT,
    title           TEXT NOT NULL,
    original_title  TEXT,
    tagline         TEXT,
    overview        TEXT,
    released        DATE,
    runtime         INTEGER,
    certification   TEXT,
    genres          JSONB DEFAULT '[]'::jsonb,
    language        TEXT,
    status          TEXT,
    popularity      DOUBLE PRECISION DEFAULT 0,
    rating_avg      DOUBLE PRECISION DEFAULT 0,
    rating_count    BIGINT DEFAULT 0,
    trailer_url     TEXT,
    images          JSONB DEFAULT '{}'::jsonb,
    metadata        JSONB DEFAULT '{}'::jsonb,
    adult           BOOLEAN DEFAULT false,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS shows (
    id              BIGSERIAL PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE NOT NULL,
    imdb_id         TEXT,
    title           TEXT NOT NULL,
    original_title  TEXT,
    overview        TEXT,
    first_aired     DATE,
    last_aired      DATE,
    episode_count   INTEGER DEFAULT 0,
    season_count    INTEGER DEFAULT 0,
    network         TEXT,
    status          TEXT,
    popularity      DOUBLE PRECISION DEFAULT 0,
    rating_avg      DOUBLE PRECISION DEFAULT 0,
    rating_count    BIGINT DEFAULT 0,
    genres          JSONB DEFAULT '[]'::jsonb,
    language        TEXT,
    certification   TEXT,
    images          JSONB DEFAULT '{}'::jsonb,
    metadata        JSONB DEFAULT '{}'::jsonb,
    adult           BOOLEAN DEFAULT false,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS seasons (
    id              BIGSERIAL PRIMARY KEY,
    show_id         BIGINT NOT NULL REFERENCES shows(id),
    tmdb_show_id    INTEGER,
    season_number   INTEGER NOT NULL,
    episode_count   INTEGER DEFAULT 0,
    aired_at        DATE,
    images          JSONB DEFAULT '{}'::jsonb,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (show_id, season_number)
);

CREATE TABLE IF NOT EXISTS episodes (
    id              BIGSERIAL PRIMARY KEY,
    season_id       BIGINT NOT NULL REFERENCES seasons(id),
    show_id         BIGINT NOT NULL REFERENCES shows(id),
    episode_number  INTEGER NOT NULL,
    absolute_number INTEGER,
    title           TEXT,
    overview        TEXT,
    runtime         INTEGER,
    aired_at        DATE,
    images          JSONB DEFAULT '{}'::jsonb,
    last_synced_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (show_id, season_id, episode_number)
);

CREATE INDEX idx_seasons_show ON seasons (show_id);
CREATE INDEX idx_episodes_season ON episodes (season_id);
CREATE INDEX idx_episodes_show_aired ON episodes (show_id, aired_at);

CREATE TABLE IF NOT EXISTS people (
    id                   BIGSERIAL PRIMARY KEY,
    tmdb_id              INTEGER UNIQUE NOT NULL,
    imdb_id              TEXT,
    name                 TEXT NOT NULL,
    biography            TEXT,
    birthday             DATE,
    deathday             DATE,
    gender               INTEGER,
    known_for_department TEXT,
    images               JSONB DEFAULT '{}'::jsonb,
    adult                BOOLEAN DEFAULT false,
    last_synced_at       TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS credits (
    id              BIGSERIAL PRIMARY KEY,
    person_id       BIGINT NOT NULL REFERENCES people(id),
    media_type      TEXT NOT NULL,
    media_id        BIGINT NOT NULL,
    credit_type     TEXT NOT NULL,
    job             TEXT,
    character_name  TEXT,
    department      TEXT,
    "order"         INTEGER DEFAULT 0,
    series_regular  BOOLEAN DEFAULT false,
    episode_count   INTEGER DEFAULT 0
);

CREATE INDEX idx_credits_person ON credits (person_id);
CREATE INDEX idx_credits_media ON credits (media_type, media_id);

CREATE TABLE IF NOT EXISTS external_ids (
    id           BIGSERIAL PRIMARY KEY,
    media_type   TEXT NOT NULL,
    media_id     BIGINT NOT NULL,
    source       TEXT NOT NULL,
    external_id  TEXT NOT NULL,
    UNIQUE (media_type, source, external_id)
);

CREATE INDEX idx_external_ids_media ON external_ids (media_type, media_id);

CREATE TABLE IF NOT EXISTS genres (
    id   BIGINT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS networks (
    id   BIGINT PRIMARY KEY,
    name TEXT NOT NULL
);

-- +migrate Down
DROP TABLE IF EXISTS external_ids;
DROP TABLE IF EXISTS credits;
DROP TABLE IF EXISTS people;
DROP TABLE IF EXISTS episodes;
DROP TABLE IF EXISTS seasons;
DROP TABLE IF EXISTS shows;
DROP TABLE IF EXISTS movies;
DROP TABLE IF EXISTS networks;
DROP TABLE IF EXISTS genres;
