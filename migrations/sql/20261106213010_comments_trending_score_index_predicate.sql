-- +goose Up
-- Issue #2 asked for a partial expression index for GET /v1/comments/trending:
--
--   CREATE INDEX idx_comments_trending_score ON comments
--       ((like_count + reply_count) DESC, created_at DESC)
--       WHERE created_at >= now() - INTERVAL '7 days';
--
-- PostgreSQL rejects that statement outright:
--
--   ERROR:  functions in index predicate must be marked IMMUTABLE
--
-- now() is STABLE, not IMMUTABLE, so a rolling "last 7 days" window can never
-- be baked into an index predicate. Migration 0022 already recorded the same
-- finding for idx_comments_trending, and it is why neither index carries a
-- predicate. The 7-day window therefore has to stay in the query, where
-- Postgres applies it as an Index Cond on the created_at key of this index.
--
-- Migration 0032 shipped this index without a predicate but its file comment
-- wrongly claims "the same 7-day partial predicate keeps the index small". A
-- merged migration must never be edited, so this migration rebuilds the index
-- with its only achievable definition — the exact query shape minus the
-- impossible predicate — and records the real rationale on the index itself.
-- The handler query is unchanged (identical results); the ordered index already
-- lets it return without a Sort.
DROP INDEX IF EXISTS idx_comments_trending_score;
CREATE INDEX idx_comments_trending_score ON comments
    ((like_count + reply_count) DESC, created_at DESC);

COMMENT ON INDEX idx_comments_trending_score IS
    'Expression index for GET /v1/comments/trending: ((like_count + reply_count) DESC, created_at DESC). Not partial: a WHERE created_at >= now() - INTERVAL ''7 days'' predicate is rejected (now() is STABLE, not IMMUTABLE), so the 7-day window stays in the query and Postgres applies it as an Index Cond on created_at. Issue #2.';

-- +goose Down
-- 0032 created this index too, so down only removes the rationale comment and
-- leaves the (identical) index in place.
COMMENT ON INDEX idx_comments_trending_score IS NULL;
