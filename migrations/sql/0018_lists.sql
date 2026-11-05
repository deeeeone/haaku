-- +goose Up
-- Personal lists (T4.1). Unlimited lists with items, notes/rank, privacy model.

CREATE TABLE IF NOT EXISTS lists (
    id              BIGSERIAL PRIMARY KEY,
    user_id         TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT DEFAULT '',
    slug            TEXT NOT NULL UNIQUE,
    privacy         TEXT NOT NULL DEFAULT 'public' CHECK (privacy IN ('public', 'link_only', 'private')),
    sort_by         TEXT DEFAULT 'rank',
    sort_how        TEXT DEFAULT 'asc',
    is_ranked       BOOLEAN NOT NULL DEFAULT false,
    cover_url       TEXT DEFAULT '',
    allow_comments  BOOLEAN NOT NULL DEFAULT true,
    like_count      INT NOT NULL DEFAULT 0,
    comment_count   INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lists_user_id ON lists (user_id);
CREATE INDEX IF NOT EXISTS idx_lists_slug ON lists (slug);
CREATE INDEX IF NOT EXISTS idx_lists_privacy ON lists (privacy);
CREATE INDEX IF NOT EXISTS idx_lists_like_count ON lists (like_count DESC);

CREATE TABLE IF NOT EXISTS list_items (
    id          BIGSERIAL PRIMARY KEY,
    list_id     BIGINT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    media_type  TEXT NOT NULL,
    media_id    BIGINT NOT NULL,
    rank        INT NOT NULL DEFAULT 0,
    notes       TEXT DEFAULT '',
    added_by    TEXT NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (list_id, media_type, media_id)
);

CREATE INDEX IF NOT EXISTS idx_list_items_list_id ON list_items (list_id);
CREATE INDEX IF NOT EXISTS idx_list_items_media ON list_items (media_type, media_id);

-- +goose Down
DROP TABLE IF EXISTS list_items;
DROP TABLE IF EXISTS lists;
