-- +goose Up
-- Comments (T4.5). Threaded comments with reactions, likes, and reporting.

CREATE TABLE IF NOT EXISTS comments (
    id          BIGSERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    parent_id   BIGINT,
    media_type  TEXT,
    media_id    BIGINT,
    list_id     BIGINT,
    comment_type TEXT NOT NULL DEFAULT 'shout' CHECK (comment_type IN ('shout', 'review')),
    body        TEXT NOT NULL,
    spoiler     BOOLEAN NOT NULL DEFAULT false,
    like_count  INT NOT NULL DEFAULT 0,
    reply_count INT NOT NULL DEFAULT 0,
    edited_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_comments_user_created ON comments (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_comments_media ON comments (media_type, media_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_comments_list ON comments (list_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_comments_parent ON comments (parent_id);
CREATE INDEX IF NOT EXISTS idx_comments_type_created ON comments (comment_type, created_at DESC);
-- Not a partial index: a predicate of `created_at >= now() - INTERVAL '7 days'`
-- is rejected because now() is STABLE, not IMMUTABLE ("functions in index
-- predicate must be marked IMMUTABLE"). The rolling window lives in the query.
CREATE INDEX IF NOT EXISTS idx_comments_trending ON comments (comment_type, media_type, created_at DESC);

CREATE TABLE IF NOT EXISTS comment_likes (
    comment_id  BIGINT NOT NULL,
    user_id     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_comment_likes_comment ON comment_likes (comment_id);

CREATE TABLE IF NOT EXISTS comment_reactions (
    comment_id    BIGINT NOT NULL,
    user_id       TEXT NOT NULL,
    reaction_type TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id, reaction_type)
);

CREATE INDEX IF NOT EXISTS idx_comment_reactions_comment ON comment_reactions (comment_id);

CREATE TABLE IF NOT EXISTS comment_reports (
    id          BIGSERIAL PRIMARY KEY,
    comment_id  BIGINT NOT NULL,
    reporter_id TEXT NOT NULL,
    reason      TEXT NOT NULL CHECK (reason IN ('spoilers','language','abusive','spam','bigotry','political','offtopic','duplicate','too_short','other')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (comment_id, reporter_id)
);

CREATE INDEX IF NOT EXISTS idx_comment_reports_comment ON comment_reports (comment_id);

-- +goose Down
DROP TABLE IF EXISTS comment_reports;
DROP TABLE IF EXISTS comment_reactions;
DROP TABLE IF EXISTS comment_likes;
DROP TABLE IF EXISTS comments;
