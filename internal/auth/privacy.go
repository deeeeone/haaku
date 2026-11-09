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

// ResolveTargetID resolves the "me" alias in a :id path parameter to the
// caller's own user id. Every /users/{id}/<segment> route accepts "me" as the
// id, so /v1/users/me/watchlist means "the caller's watchlist" the same way
// /v1/users/me does. It returns "" when the id is "me" but nobody is
// authenticated — the caller must then answer 401, not 404, so a signed-out
// client does not mistake "me" for a deleted account.
func ResolveTargetID(c *fiber.Ctx, param string) string {
	id := c.Params(param)
	if id != "me" {
		return id
	}
	current, _ := c.Locals(ctxUserID).(string)
	return current
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
		targetUserID := ResolveTargetID(c, targetUserIDParam)
		if c.Params(targetUserIDParam) == "me" && targetUserID == "" {
			return writeUnauthorized(c)
		}
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

		// Fast path (identical to the pre-feature behaviour for the section=""
		// routes): a public profile whose section flag imposes no restriction is
		// visible to anyone, including anonymous callers. Keeping it ahead of the
		// admin/follower lookups means a public read still costs one query.
		flag := sectionFlagValue(targetUser.Settings, section)
		if !targetUser.IsPrivate && !sectionFlagRestricts(flag) {
			return c.Next()
		}

		// Self always allowed.
		if currentUserID != "" && currentUserID == targetUserID {
			return c.Next()
		}

		viewer := viewerFacts{ID: currentUserID}
		if viewer.ID != "" {
			var currentUser database.User
			if err := db.First(&currentUser, "id = ?", viewer.ID).Error; err == nil {
				viewer.IsAdmin = currentUser.IsAdmin
			}
		}
		isFollower := false
		if viewer.ID != "" {
			var followCount int64
			db.Model(&database.UserFollow{}).
				Where("follower_id = ? AND following_id = ?", viewer.ID, targetUserID).
				Count(&followCount)
			isFollower = followCount > 0
		}

		switch visibilityDecision(&targetUser, viewer, false, isFollower, section) {
		case VisibilityAllowed:
			return c.Next()
		case VisibilityBlocked:
			// Already handled above; kept so the decision stays the single
			// source of the rule.
			return writeEnvelope(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		if targetUser.IsPrivate && !isFollower {
			return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "this profile is private")
		}
		return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "this section is private")
	}
}

// ---------------------------------------------------------------------------
// The shared visibility decision
// ---------------------------------------------------------------------------

// VisibilityDecision is the outcome of the profile/section visibility rule. It
// is shared by PrivacyFilterSection (which maps it to 200/403/404) and by
// callers that must apply the same rule to accounts other than the URL target,
// such as the activities feed (VisibleUserIDs).
type VisibilityDecision int

const (
	// VisibilityAllowed means the caller may read the target's section.
	VisibilityAllowed VisibilityDecision = iota
	// VisibilityBlocked means a block exists in either direction; the target
	// must behave as if it did not exist (404).
	VisibilityBlocked
	// VisibilityPrivate means the profile or the section is restricted to the
	// account itself, its followers and admins (403).
	VisibilityPrivate
)

// viewerFacts are the caller-side inputs of the decision. They do not depend on
// the target, so a batch read resolves them once for every candidate.
type viewerFacts struct {
	ID      string
	IsAdmin bool
}

// sectionFlagRestricts reports whether a per-section privacy value names a
// restriction. Anything else (public, absent, unknown) never restricts.
func sectionFlagRestricts(flag string) bool {
	return flag == privacyFriends || flag == privacyPrivate
}

// visibilityDecision applies the rules documented on PrivacyFilterSection to a
// target the caller has already loaded. blocked and isFollower are the
// per-target inputs; the caller must pass blocked=false for an anonymous caller
// (there is no block edge to evaluate).
func visibilityDecision(target *database.User, viewer viewerFacts, blocked, isFollower bool, section string) VisibilityDecision {
	if viewer.ID != "" && blocked {
		return VisibilityBlocked
	}
	flag := sectionFlagValue(target.Settings, section)
	if !target.IsPrivate && !sectionFlagRestricts(flag) {
		return VisibilityAllowed
	}
	if viewer.ID != "" && viewer.ID == target.ID {
		return VisibilityAllowed
	}
	if viewer.ID != "" && viewer.IsAdmin {
		return VisibilityAllowed
	}
	// is_private master override: hides every section from anyone but a follower.
	if target.IsPrivate && !isFollower {
		return VisibilityPrivate
	}
	// "friends" is satisfied by a follower, "private" is not, and a
	// non-restricting value imposes nothing.
	if flag == privacyFriends && isFollower {
		return VisibilityAllowed
	}
	if !sectionFlagRestricts(flag) {
		return VisibilityAllowed
	}
	return VisibilityPrivate
}

// VisibleUserIDs filters candidateIDs down to the accounts whose section the
// caller may read, applying exactly the rule PrivacyFilterSection enforces for
// a URL target: soft-deleted and deleted/pending_deletion accounts are dropped,
// an account is dropped when it and the caller block each other, and a private
// profile or a restricting section flag drops the account unless the caller is
// that account, an admin, or a follower. The candidates' own order is preserved.
//
// It exists so a feed that renders other accounts' rows (the activities read)
// cannot expose rows the per-account route would refuse.
func VisibleUserIDs(db *gorm.DB, callerID string, candidateIDs []string, section string) ([]string, error) {
	ids := make([]string, 0, len(candidateIDs))
	seen := make(map[string]bool, len(candidateIDs))
	for _, id := range candidateIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	var users []database.User
	if err := db.Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]*database.User, len(users))
	for i := range users {
		byID[users[i].ID] = &users[i]
	}

	blocked := map[string]bool{}
	following := map[string]bool{}
	viewer := viewerFacts{ID: callerID}
	if callerID != "" {
		var blocks []database.UserBlock
		if err := db.Where(
			"(blocker_id = ? AND blocked_id IN ?) OR (blocker_id IN ? AND blocked_id = ?)",
			callerID, ids, ids, callerID,
		).Find(&blocks).Error; err != nil {
			return nil, err
		}
		for i := range blocks {
			if blocks[i].BlockerID == callerID {
				blocked[blocks[i].BlockedID] = true
			} else {
				blocked[blocks[i].BlockerID] = true
			}
		}

		var follows []database.UserFollow
		if err := db.Where("follower_id = ? AND following_id IN ?", callerID, ids).
			Find(&follows).Error; err != nil {
			return nil, err
		}
		for i := range follows {
			following[follows[i].FollowingID] = true
		}

		var caller database.User
		if err := db.First(&caller, "id = ?", callerID).Error; err == nil {
			viewer.IsAdmin = caller.IsAdmin
		}
	}

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		u := byID[id]
		if u == nil {
			continue // no row (unknown or soft-deleted)
		}
		if u.Status == "deleted" || u.Status == "pending_deletion" {
			continue
		}
		if visibilityDecision(u, viewer, blocked[id], following[id], section) != VisibilityAllowed {
			continue
		}
		out = append(out, id)
	}
	return out, nil
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
