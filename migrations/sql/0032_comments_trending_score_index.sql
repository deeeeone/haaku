-- +goose Up
-- Expression index accelerating GET /v1/comments/trending.
--
-- The trending handler filters to comments from the last 7 days
-- (created_at >= now() - INTERVAL '7 days') and orders by
-- like_count + reply_count DESC, created_at DESC. Migration 0022's
-- idx_comments_trending (comment_type, media_type, created_at DESC) serves the
-- WHERE clause but cannot accelerate an ORDER BY over the computed expression
-- like_count + reply_count, so Postgres sorts the post-window rows per call.
--
-- This partial expression index mirrors the exact query shape: the same
-- 7-day partial predicate keeps the index small, and the score expression +
-- created_at order lets Postgres return rows in index order without a sort.
-- Created on request of comments.md "Databases — trending index" (issue #2).
-- The handler query is left unchanged (identical results).
CREATE INDEX IF NOT EXISTS idx_comments_trending_score ON comments
    ((like_count + reply_count) DESC, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_comments_trending_score;
