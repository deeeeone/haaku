package auth

import (
	"errors"
	"sync"

	"github.com/haaku-tv/haaku/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Moderation authorization (issue #10 §2)
// ---------------------------------------------------------------------------
//
// There is no distinct "admin role" system in this codebase beyond the
// users.is_admin / users.is_moderator columns, which the comment-delete
// moderator override (internal/handlers/comments) and the PrivacyFilter admin
// bypass already trust. Nothing in the app currently writes those columns, so
// RequireModerator authorizes on EITHER of the two mechanisms the codebase can
// honestly support today:
//
//  1. the env allowlists ADMIN_USER_IDS / MOD_USER_IDS (parsed by
//     internal/config since the beginning but, until this change, never
//     consulted), so an operator can grant moderation on a self-hosted box
//     without database surgery; or
//  2. the existing users.is_moderator / users.is_admin role columns, so a row
//     already flagged gets in.
//
// The two env vars collapse into one moderator set: the report queue needs a
// single "may action reports" gate, and an admin is a moderator by definition.
// This is a deliberate choice flagged for the owner's review in the PR.

var (
	moderationAllowMu sync.RWMutex
	// moderationAllowlist holds user ids granted moderation from the env
	// allowlists. Empty until InitModerationAllowlists runs at startup.
	moderationAllowlist = map[string]bool{}
	// adminAllowlist holds the ADMIN_USER_IDS env allowlist on its own, for the
	// stricter RequireAdmin gate (issue #53's admin panel). A moderator is not
	// an admin.
	adminAllowlist = map[string]bool{}
)

// InitModerationAllowlists records the admin/mod user ids from configuration.
// Call once at startup (server.go). Safe to call again — the last call wins,
// which is what tests rely on to reset the set.
func InitModerationAllowlists(adminIDs, modIDs []string) {
	set := make(map[string]bool, len(adminIDs)+len(modIDs))
	admins := make(map[string]bool, len(adminIDs))
	for _, id := range adminIDs {
		if id != "" {
			set[id] = true
			admins[id] = true
		}
	}
	for _, id := range modIDs {
		if id != "" {
			set[id] = true
		}
	}
	moderationAllowMu.Lock()
	moderationAllowlist = set
	adminAllowlist = admins
	moderationAllowMu.Unlock()
}

// IsModerationAllowlisted reports whether userID is in the ADMIN_USER_IDS /
// MOD_USER_IDS env allowlists. It is in-memory, so it is cheap enough to call
// per request — which is what the rate limiter does to exempt the operator
// (internal/ratelimit).
func IsModerationAllowlisted(userID string) bool {
	if userID == "" {
		return false
	}
	moderationAllowMu.RLock()
	defer moderationAllowMu.RUnlock()
	return moderationAllowlist[userID]
}

// RequireModerator is a Fiber middleware that requires the authenticated caller
// to be a moderator or admin. It must run after AuthRequired (it reads
// c.Locals("user_id")); if the caller is not authenticated it returns 401 to
// match AuthRequired rather than leaking a 403 to anonymous callers.
//
// Authorization is the env allowlist OR the users.is_admin/is_moderator column.
// A DB failure fails closed with 500 rather than granting access.
func RequireModerator() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals(ctxUserID).(string)
		if userID == "" {
			return writeUnauthorized(c)
		}

		if IsModerationAllowlisted(userID) {
			return c.Next()
		}

		db, err := database.GetDB()
		if err != nil {
			return writeEnvelope(c, fiber.StatusInternalServerError, "internal", "database unavailable")
		}

		var user database.User
		err = db.Select("is_admin", "is_moderator").First(&user, "id = ?", userID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Authenticated principal has no user row — deny (safe default).
				return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "moderator access required")
			}
			return writeEnvelope(c, fiber.StatusInternalServerError, "internal", "database query error")
		}
		if user.IsAdmin || user.IsModerator {
			return c.Next()
		}

		return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "moderator access required")
	}
}

// RequireAdmin is a Fiber middleware that requires the authenticated caller to
// be an admin (issue #53's account panel). It is the stricter sibling of
// RequireModerator: authorization is the ADMIN_USER_IDS env allowlist OR
// users.is_admin, and a moderator who is not an admin gets 403.
//
// It must run after AuthRequired. An unauthenticated caller gets 401, matching
// AuthRequired, rather than a 403 that would confirm the route exists.
func RequireAdmin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals(ctxUserID).(string)
		if userID == "" {
			return writeUnauthorized(c)
		}

		moderationAllowMu.RLock()
		allowed := adminAllowlist[userID]
		moderationAllowMu.RUnlock()
		if allowed {
			return c.Next()
		}

		db, err := database.GetDB()
		if err != nil {
			return writeEnvelope(c, fiber.StatusInternalServerError, "internal", "database unavailable")
		}

		var user database.User
		err = db.Select("is_admin").First(&user, "id = ?", userID).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "admin access required")
			}
			return writeEnvelope(c, fiber.StatusInternalServerError, "internal", "database query error")
		}
		if user.IsAdmin {
			return c.Next()
		}

		return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "admin access required")
	}
}
