-- +goose Up
-- Rankings (issue #66) and Trakt-style progress resets.
--
-- A reset is a wall-clock point, not a deletion: watched progress only counts
-- plays at or after reset_at, and the caller's user_history rows are never
-- removed. DELETE /shows/{id}/progress/watched/reset drops the row, restoring
-- the previous progress.
CREATE TABLE IF NOT EXISTS user_show_progress_resets (
    id         BIGSERIAL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    show_id    BIGINT NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    reset_at   TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, show_id)
);

CREATE INDEX IF NOT EXISTS idx_user_show_progress_resets_show
    ON user_show_progress_resets (show_id);

-- Period rankings aggregate by media type inside a watched_at window; the
-- existing indexes lead with user_id, so they cannot serve that scan.
CREATE INDEX IF NOT EXISTS idx_user_history_type_watched
    ON user_history (media_type, watched_at);

-- collected/{period} and favorited/{period} filter the same way.
CREATE INDEX IF NOT EXISTS idx_user_collection_type_collected
    ON user_collection (media_type, collected_at);

CREATE INDEX IF NOT EXISTS idx_user_favorites_type_favorited
    ON user_favorites (media_type, favorited_at);

-- hot ranks watchlist additions in the last 24 hours.
CREATE INDEX IF NOT EXISTS idx_user_watchlist_type_listed
    ON user_watchlist (media_type, listed_at);

-- hot also counts checkins in the last 24 hours; only the expiry index exists.
CREATE INDEX IF NOT EXISTS idx_checkins_created_at
    ON user_checkins (created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_checkins_created_at;
DROP INDEX IF EXISTS idx_user_watchlist_type_listed;
DROP INDEX IF EXISTS idx_user_favorites_type_favorited;
DROP INDEX IF EXISTS idx_user_collection_type_collected;
DROP INDEX IF EXISTS idx_user_history_type_watched;
DROP INDEX IF EXISTS idx_user_show_progress_resets_show;
DROP TABLE IF EXISTS user_show_progress_resets;
