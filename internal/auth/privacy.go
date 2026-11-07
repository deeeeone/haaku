package auth

import (
	"encoding/json"
	"errors"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Per-section privacy flag values stored in users.settings.privacy.<section>.
// "public" (and an absent/unknown value) means "no additional restriction";
// "friends" and "private" are the opt-in tightening values. See
// internal/handlers/users/users.md ("Per-section privacy flags").
const (
	privacyPublic  = "public"
	privacyFriends = "friends"
	privacyPrivate = "private"
)

// PrivacyFilter returns middleware that enforces profile visibility rules
// (users.is_private) and blocks only — the behaviour every /users/:id route had
// before per-section flags were enforced. It is PrivacyFilterSection with an
// empty section and is kept for routes no per-section flag names (collection,
// watchlist, comments, notes, likes, and the social graph).
//
// targetUserIDParam is the URL param name holding the target user ID (e.g. "id").
//
// Rules:
//   - Private profile → only self, admins, and followers can see.
//   - Blocked users are invisible to the blocker (returns 404, not 403).
//   - Unauthenticated users can only see public profiles.
func PrivacyFilter(targetUserIDParam string) fiber.Handler {
	return PrivacyFilterSection(targetUserIDParam, "")
}

// PrivacyFilterSection returns middleware that enforces the profile visibility
// rules above AND, when section is non-empty, the matching
// users.settings.privacy.<section> flag. It reads the target user row — and so
// the target's settings — exactly once per request; there is no second query in
// the handler.
//
// section is one of "profile", "history", "ratings", "activity" (the flag names
// in users.settings.privacy). Mapping to routes is documented in
// internal/handlers/users/users.md.
//
// Rules:
//   - is_private (users.is_private) is a MASTER OVERRIDE: a private profile
//     hides every section from everyone but self/admins/followers, regardless of
//     the sub-flags. The sub-flags never loosen it.
//   - Per-section flag, applied on top of is_private:
//     "friends" → self, admins, and followers.
//     "private" → self and admins only.
//     "public", absent, unknown, or a malformed settings blob → no
//     restriction (today's behaviour). The flag is opt-in privacy tightening;
//     unset flags preserve the pre-feature behaviour and never flip an
//     existing row from public to private.
//   - A malformed/absent settings blob must never 500 and never over-share
//     beyond the is_private gate; it fails safe to today's default.
//   - Blocked users are invisible to the blocker (404, not 403).
//   - Unknown target user → 404.
func PrivacyFilterSection(targetUserIDParam, section string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		targetUserID := c.Params(targetUserIDParam)
		if targetUserID == "" {
			// No target user ID in URL — pass through (e.g. /users/me)
			return c.Next()
		}

		db, err := database.GetDB()
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
		}

		// Look up target user. The row carries users.settings, so the section
		// flag is read from this same query — no second round-trip.
		var targetUser database.User
		if err := db.First(&targetUser, "id = ?", targetUserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return writeEnvelope(c, fiber.StatusNotFound, "not_found", "user not found")
			}
			return fiber.NewError(fiber.StatusInternalServerError, "database query error")
		}

		// Block check: if current user has blocked target, or target has blocked current user,
		// return 404 (invisible, don't leak existence).
		currentUserID, _ := c.Locals("user_id").(string)
		if currentUserID != "" {
			var blockCount int64
			db.Model(&database.UserBlock{}).
				Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)",
					currentUserID, targetUserID, targetUserID, currentUserID).
				Count(&blockCount)
			if blockCount > 0 {
				return writeEnvelope(c, fiber.StatusNotFound, "not_found", "user not found")
			}
		}

		// The per-section flag only ever tightens; it never loosens is_private.
		// A malformed or absent blob yields "" here, which is non-restricting.
		flag := sectionFlagValue(targetUser.Settings, section)
		flagRestricts := flag == privacyFriends || flag == privacyPrivate

		// Fast path (identical to the pre-feature behaviour for the section=""
		// routes): a public profile whose section flag imposes no restriction is
		// visible to anyone, including anonymous callers.
		if !targetUser.IsPrivate && !flagRestricts {
			return c.Next()
		}

		// Self always allowed.
		if currentUserID != "" && currentUserID == targetUserID {
			return c.Next()
		}

		// Admin always allowed.
		isAdmin := false
		if currentUserID != "" {
			var currentUser database.User
			if err := db.First(&currentUser, "id = ?", currentUserID).Error; err == nil {
				isAdmin = currentUser.IsAdmin
			}
		}
		if isAdmin {
			return c.Next()
		}

		// Followers can see private profiles.
		isFollower := false
		if currentUserID != "" {
			var followCount int64
			db.Model(&database.UserFollow{}).
				Where("follower_id = ? AND following_id = ?", currentUserID, targetUserID).
				Count(&followCount)
			isFollower = followCount > 0
		}

		// is_private master override: a private profile hides every section from
		// anyone but a follower, no matter what the sub-flags say.
		if targetUser.IsPrivate && !isFollower {
			return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "this profile is private")
		}

		// Per-section flag. At this point the caller is a follower (or the
		// profile is public): "friends" is satisfied by a follower, "private"
		// is not, and a non-restricting value imposes nothing.
		if flag == privacyFriends && isFollower {
			return c.Next()
		}
		if flag != privacyFriends && flag != privacyPrivate {
			return c.Next()
		}
		return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "this section is private")
	}
}

// sectionFlagValue extracts users.settings.privacy.<section> from the stored
// jsonb string. It returns "" — "no restriction" — when section is empty, the
// blob is empty/null, the value is absent, the value is not a string, or the
// blob cannot be parsed. A corrupt settings blob therefore fails safe to
// today's behaviour (visible) and never 500s.
func sectionFlagValue(settingsJSON, section string) string {
	if section == "" || settingsJSON == "" || settingsJSON == "null" {
		return ""
	}
	var parsed struct {
		Privacy map[string]string `json:"privacy"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &parsed); err != nil {
		return ""
	}
	return parsed.Privacy[section]
}

// writeEnvelope writes a JSON error envelope directly (no import cycle).
func writeEnvelope(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}
