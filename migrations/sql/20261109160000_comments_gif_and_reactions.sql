-- +goose Up
-- 20261109160000_comments_gif_and_reactions: comment GIF attachments and a
-- covering index for the contract reaction reads (issue #107).
--
-- Trakt's comment write body and comment response both carry an optional GIF
-- ({url, slug}); we had nowhere to put it, so a client that sent one had it
-- silently dropped. These two columns store it.
--
-- The reaction list (/comments/{id}/reactions/) is paginated newest-first for
-- one comment; the existing comment_id index alone still needs a sort of every
-- reaction on the comment.

ALTER TABLE comments ADD COLUMN IF NOT EXISTS gif_url TEXT;
ALTER TABLE comments ADD COLUMN IF NOT EXISTS gif_slug TEXT;

CREATE INDEX IF NOT EXISTS idx_comment_reactions_comment_created
    ON comment_reactions (comment_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_comment_reactions_comment_created;
ALTER TABLE comments DROP COLUMN IF EXISTS gif_slug;
ALTER TABLE comments DROP COLUMN IF EXISTS gif_url;
