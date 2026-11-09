-- +goose Up
-- lists.sort_order: the position of a list among its owner's lists.
--
-- The Trakt contract's POST /users/{id}/lists/reorder sets the order of a
-- user's lists. Our lists table had no order column, so the endpoint could only
-- accept and ignore the body. This column lets the reorder persist, and it is
-- not serialised in our own list JSON (the Go field is json:"-"), so it is not
-- a change to the public list shape.

ALTER TABLE lists ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_lists_user_sort_order ON lists (user_id, sort_order);

-- +goose Down
DROP INDEX IF EXISTS idx_lists_user_sort_order;
ALTER TABLE lists DROP COLUMN IF EXISTS sort_order;
