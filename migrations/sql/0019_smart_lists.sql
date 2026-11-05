-- +goose Up
-- Smart lists (T4.2). Dynamic lists resolved from filter DSL against media tables.

CREATE TABLE IF NOT EXISTS smart_lists (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    filters     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_smart_lists_user_id ON smart_lists (user_id);

-- List likes
CREATE TABLE IF NOT EXISTS list_likes (
    list_id     BIGINT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (list_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_list_likes_list_id ON list_likes (list_id);
CREATE INDEX IF NOT EXISTS idx_list_likes_user_id ON list_likes (user_id);

-- List comments
CREATE TABLE IF NOT EXISTS list_comments (
    id          BIGSERIAL PRIMARY KEY,
    list_id     BIGINT NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_list_comments_list_id ON list_comments (list_id);

-- +goose Down
DROP TABLE IF EXISTS list_comments;
DROP TABLE IF EXISTS list_likes;
DROP TABLE IF EXISTS smart_lists;
