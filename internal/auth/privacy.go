package auth

import (
	"errors"

	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// PrivacyFilter returns middleware that enforces profile visibility rules.
// targetUserIDParam is the URL param name holding the target user ID (e.g. "id").
//
// Rules:
//   - Private profile → only self, admins, and followers can see.
//   - Blocked users are invisible to the blocker (returns 404, not 403).
//   - Unauthenticated users can only see public profiles.
func PrivacyFilter(targetUserIDParam string) fiber.Handler {
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

		// Look up target user.
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

		// Public profile — anyone can see.
		if !targetUser.IsPrivate {
			return c.Next()
		}

		// Private profile — check permissions.
		// Self always allowed.
		if currentUserID == targetUserID {
			return c.Next()
		}

		// Admin always allowed.
		if currentUserID != "" {
			var currentUser database.User
			if err := db.First(&currentUser, "id = ?", currentUserID).Error; err == nil {
				if currentUser.IsAdmin {
					return c.Next()
				}
			}
		}

		// Followers can see private profiles.
		if currentUserID != "" {
			var followCount int64
			db.Model(&database.UserFollow{}).
				Where("follower_id = ? AND followed_id = ?", currentUserID, targetUserID).
				Count(&followCount)
			if followCount > 0 {
				return c.Next()
			}
		}

		// Unauthenticated or not allowed — 403.
		return writeEnvelope(c, fiber.StatusForbidden, "forbidden", "this profile is private")
	}
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
