package database

import (
	"fmt"

	"gorm.io/gorm"
)

// userScopedTables maps tables that carry a plain user_id column to that
// column, so DeleteUserData can wipe every owned row even where no FK
// cascade exists. Kept as a slice of pairs to preserve deterministic order.
var userScopedTables = []struct{ table, column string }{
	{"oauth_clients", "user_id"},
	{"oauth_grants", "user_id"},
	{"oauth_tokens", "user_id"},
	{"user_history", "user_id"},
	{"user_collection", "user_id"},
	{"user_watchlist", "user_id"},
	{"user_ratings", "user_id"},
	{"user_favorites", "user_id"},
	{"scrobble_sessions", "user_id"},
	{"user_playback", "user_id"},
	{"user_checkins", "user_id"},
	{"events_outbox", "user_id"},
	{"user_stats", "user_id"},
	{"sync_sessions", "user_id"},
	{"user_hidden", "user_id"},
	{"lists", "user_id"},
	{"smart_lists", "user_id"},
	{"list_likes", "user_id"},
	{"list_collaborators", "user_id"},
	{"comments", "user_id"},
	{"comment_likes", "user_id"},
	{"comment_reactions", "user_id"},
	{"notifications", "user_id"},
	{"webhook_endpoints", "user_id"},
	{"integration_tokens", "user_id"},
	{"user_notes", "user_id"},
	{"saved_filters", "user_id"},
}

// DeleteUserData hard-deletes a user and every row they own, in one
// transaction. Identity lives in Clerk; when the Clerk account is deleted
// (user.deleted webhook) or the user deletes their account in-app, the local
// mirror and all user-owned content are wiped.
//
// The wipe is explicit per table rather than relying on FK cascades — most
// content tables (lists, comments, activity, tokens, ...) have no
// ON DELETE CASCADE from users. The users row itself is deleted with raw SQL
// to bypass GORM soft delete (User embeds gorm.DeletedAt).
//
// Community-scoped tables (media, credits, genres) are global and untouched.
// list_items/list_comments/webhook_deliveries are reached through their
// parents (lists, webhook_endpoints) which are deleted here.
func DeleteUserData(db *gorm.DB, userID string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. Directly user-scoped tables.
		for _, t := range userScopedTables {
			if err := tx.Exec(
				fmt.Sprintf("DELETE FROM %s WHERE %s = ?", t.table, t.column),
				userID,
			).Error; err != nil {
				return fmt.Errorf("delete %s: %w", t.table, err)
			}
		}

		// 2. Social graph — both directions so neither side references a
		//    deleted user.
		if err := tx.Exec(
			"DELETE FROM user_follows WHERE follower_id = ? OR followed_id = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete user_follows: %w", err)
		}
		if err := tx.Exec(
			"DELETE FROM user_blocks WHERE blocker_id = ? OR blocked_id = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete user_blocks: %w", err)
		}

		// 3. Rows where the user is the actor/reporter, not the owner.
		if err := tx.Exec("DELETE FROM activity_feed WHERE actor_id = ?", userID).Error; err != nil {
			return fmt.Errorf("delete activity_feed: %w", err)
		}
		if err := tx.Exec("DELETE FROM content_reports WHERE reporter_id = ?", userID).Error; err != nil {
			return fmt.Errorf("delete content_reports: %w", err)
		}
		// Notifications the user sent (actor) as well as received (owner).
		if err := tx.Exec(
			"DELETE FROM notifications WHERE user_id = ? OR actor_id = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete notifications: %w", err)
		}

		// 4. Children of the user's lists (lists deleted in step 1; these
		//    reference lists with ON DELETE CASCADE, kept explicit anyway)
		//    plus the user's own reactions on OTHER users' lists (no cascade
		//    from users, so they'd orphan).
		if err := tx.Exec(
			"DELETE FROM list_items WHERE list_id IN (SELECT id FROM lists WHERE user_id = ?) OR added_by = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete list_items: %w", err)
		}
		if err := tx.Exec(
			"DELETE FROM list_comments WHERE list_id IN (SELECT id FROM lists WHERE user_id = ?) OR user_id = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete list_comments: %w", err)
		}
		if err := tx.Exec(
			"DELETE FROM list_likes WHERE list_id IN (SELECT id FROM lists WHERE user_id = ?) OR user_id = ?",
			userID, userID,
		).Error; err != nil {
			return fmt.Errorf("delete list_likes: %w", err)
		}

		// 5. The user row itself (raw SQL — bypasses GORM soft delete).
		if err := tx.Exec("DELETE FROM users WHERE id = ?", userID).Error; err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		return nil
	})
}
