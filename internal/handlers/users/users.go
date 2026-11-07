package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/me", Summary: "Get current user profile", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "User profile", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/users/me", Summary: "Update current user profile (bio/location/timezone/language/privacy — name & avatar are Clerk-managed)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Updated profile", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/{id}", Summary: "Get public user profile", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Public profile", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/me/settings", Summary: "Get user settings", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Settings", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/users/me/settings", Summary: "Update user settings", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Updated settings", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/users/me", Summary: "Delete account (deletes Clerk user; local data wiped)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{204: "Deletion scheduled", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/me/export", Summary: "Export the caller's complete data archive (streamed JSON, versioned)", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Streamed JSON archive", 401: "Unauthorized", 503: "Database unavailable"}},
		{Method: "POST", Path: "/v1/users/{id}/block", Summary: "Block a user", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{204: "Blocked", 400: "Invalid request", 404: "User not found", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/users/{id}/block", Summary: "Unblock a user", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{204: "Unblocked", 404: "Block not found", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all user route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	users := api.Group("/users")

	// /users/me routes (require auth)
	me := users.Group("/me")
	me.Get("/", auth.AuthRequired(), handleGetMe)
	me.Put("/", auth.AuthRequired(), handleUpdateMe)
	me.Get("/settings", auth.AuthRequired(), handleGetSettings)
	me.Put("/settings", auth.AuthRequired(), handleUpdateSettings)
	me.Get("/export", auth.AuthRequired(), handleExport)
	me.Delete("/", auth.AuthRequired(), handleDeleteAccount)

	// /users/:id routes (public, privacy-filtered). The profile read is gated by
	// users.settings.privacy.profile in addition to is_private/blocks.
	users.Get("/:id", auth.PrivacyFilterSection("id", "profile"), handleGetUser)

	// Block endpoints (stub — 501)
	users.Post("/:id/block", auth.AuthRequired(), handleBlockUser)
	users.Delete("/:id/block", auth.AuthRequired(), handleUnblockUser)
}

// ---------------------------------------------------------------------------
// Auth helper middleware
// ---------------------------------------------------------------------------

// auth.AuthRequired() is a local middleware that requires authentication.
// Returns 401 if c.Locals("user_id") is not set.

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// ProfileResponse is the full profile for /users/me.
type ProfileResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Bio         string `json:"bio,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Location    string `json:"location,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Language    string `json:"language,omitempty"`
	IsPrivate   bool   `json:"is_private"`
	IsAdmin     bool   `json:"is_admin"`
	IsModerator bool   `json:"is_moderator"`
	Status      string `json:"status"`
	Plan        string `json:"plan"`
	CreatedAt   string `json:"created_at"`
}

// PublicProfileResponse is the public profile for /users/:id.
type PublicProfileResponse struct {
	ID             string        `json:"id"`
	Username       string        `json:"username"`
	Name           string        `json:"name"`
	Bio            string        `json:"bio,omitempty"`
	AvatarURL      string        `json:"avatar_url,omitempty"`
	Location       string        `json:"location,omitempty"`
	IsPrivate      bool          `json:"is_private"`
	StatsSummary   *StatsSummary `json:"stats_summary,omitempty"`
	RecentActivity []interface{} `json:"recent_activity,omitempty"`
}

// StatsSummary is the stats block on a public profile.
// Watched/Lists are omitempty: they are genuinely unimplemented (they must be
// filled by the stats engine, which is not landed) and hardcoded 0s must never
// be presented as real aggregate counts. Only Followers/Following carry real,
// DB-computed values. RecentActivity on PublicProfileResponse is likewise
// omitted (empty) until the stats engine provides it.
type StatsSummary struct {
	Watched   int64 `json:"watched,omitempty"`
	Lists     int64 `json:"lists,omitempty"`
	Followers int64 `json:"followers"`
	Following int64 `json:"following"`
}

// UpdateProfileRequest is the body for PUT /users/me.
// NOTE: name and avatar_url are intentionally absent — they are mirrored
// from the Clerk account (webhook/lazy sync) and never edited through the
// app. Bio/location/timezone/language/privacy are user-authored content.
type UpdateProfileRequest struct {
	Bio       *string `json:"bio,omitempty"`
	Location  *string `json:"location,omitempty"`
	Timezone  *string `json:"timezone,omitempty"`
	Language  *string `json:"language,omitempty"`
	IsPrivate *bool   `json:"is_private,omitempty"`
}

// SettingsResponse is the shape of user settings.
type SettingsResponse struct {
	Privacy       PrivacySettings      `json:"privacy"`
	Notifications NotificationSettings `json:"notifications"`
}

// PrivacySettings controls visibility of profile sections.
type PrivacySettings struct {
	Profile  string `json:"profile"`
	History  string `json:"history"`
	Ratings  string `json:"ratings"`
	Activity string `json:"activity"`
}

// NotificationSettings controls notification preferences.
type NotificationSettings struct {
	Follows        bool `json:"follows"`
	Replies        bool `json:"replies"`
	ImportComplete bool `json:"import_complete"`
	WeeklyDigest   bool `json:"weekly_digest"`
}

// UpdateSettingsRequest is the body for PUT /users/me/settings.
type UpdateSettingsRequest struct {
	Privacy       *PrivacySettings      `json:"privacy,omitempty"`
	Notifications *NotificationSettings `json:"notifications,omitempty"`
}

// AvatarResponse is no longer used — avatars come from Clerk. Removed with
// the /users/me/avatar endpoint (2026-08, Clerk-managed identity).

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleGetMe returns the current user's full profile.
func handleGetMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(buildProfileResponse(&user))
}

// handleUpdateMe updates the current user's profile.
func handleUpdateMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req UpdateProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	updates := map[string]interface{}{}
	if req.Bio != nil {
		updates["bio"] = *req.Bio
	}
	if req.Location != nil {
		updates["location"] = *req.Location
	}
	if req.Timezone != nil {
		updates["timezone"] = *req.Timezone
	}
	if req.Language != nil {
		updates["language"] = *req.Language
	}
	if req.IsPrivate != nil {
		updates["is_private"] = *req.IsPrivate
	}

	if len(updates) == 0 {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "no fields to update")
	}

	if err := db.Model(&database.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update profile")
	}

	// Fetch updated user.
	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		// The update already committed, but we can't read the fresh row back.
		// Don't silently return the pre-update/stale row with a 200.
		slog.Error("handleUpdateMe: re-fetch after update failed", "user_id", userID, "error", err)
		return response.InternalError(c)
	}

	return c.JSON(buildProfileResponse(&user))
}

// handleGetUser returns a public profile for the given user ID.
func handleGetUser(c *fiber.Ctx) error {
	userID := c.Params("id")

	// Unknown user and deleted/pending-deletion accounts are 404 for everyone;
	// the block/private gates already ran in auth.PrivacyFilter. profileTarget
	// is the single implementation of that status gate — the rest of the
	// public-profile read surface uses it too.
	user, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	resp := buildPublicProfileResponse(user)

	// Followers/Following are real DB counts. Watched/Lists stay 0 (omitempty,
	// so not serialized) and RecentActivity stays empty (omitempty) because they
	// require the stats engine that isn't landed — we don't fabricate counts.
	var followersCount, followingCount int64
	db.Model(&database.UserFollow{}).Where("following_id = ?", userID).Count(&followersCount)
	db.Model(&database.UserFollow{}).Where("follower_id = ?", userID).Count(&followingCount)

	resp.StatsSummary = &StatsSummary{
		Watched:   0,
		Lists:     0,
		Followers: followersCount,
		Following: followingCount,
	}
	resp.RecentActivity = []interface{}{}

	return c.JSON(resp)
}

// handleGetSettings returns the current user's settings.
func handleGetSettings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	settings := parseSettings(user.Settings)
	return c.JSON(settings)
}

// handleUpdateSettings updates the current user's settings.
func handleUpdateSettings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req UpdateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	current := parseSettings(user.Settings)

	if req.Privacy != nil {
		current.Privacy = *req.Privacy
	}
	if req.Notifications != nil {
		current.Notifications = *req.Notifications
	}

	settingsJSON, err := json.Marshal(current)
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to serialize settings")
	}

	if err := db.Model(&user).Update("settings", string(settingsJSON)).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update settings")
	}

	return c.JSON(current)
}

// handleDeleteAccount deletes the user's Clerk account (identity source of
// truth) and wipes the local mirror + all owned data in one transaction.
// The user.deleted webhook would do the same if it fires later — idempotent.
func handleDeleteAccount(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	clerk := auth.GetClerk()
	if clerk == nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "account deletion unavailable")
	}
	if err := clerk.DeleteUser(userID); err != nil {
		slog.Error("delete account: Clerk delete failed", "user_id", userID, "error", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to delete account")
	}

	// Wipe the local mirror + all owned data, then emit the catalog's
	// user.deleted event and invalidate cached profiles. The event is inserted
	// after the wipe (DeleteUserData removes this user's events_outbox rows) so
	// it survives, and all of it commits/rolls back together. Cache ops run
	// async after commit.
	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := database.DeleteUserData(tx, userID); err != nil {
			return nil, fmt.Errorf("delete user data: %w", err)
		}
		if err := outbox.PublishEvent(tx, outbox.EventUserDeleted, userID, map[string]any{
			"user_id": userID,
		}); err != nil {
			return nil, fmt.Errorf("publish user.deleted: %w", err)
		}

		// Invalidate the user's cached profiles across the cache layer.
		queueUserVersion(cacheTx, userID)
		return cacheTx, nil
	})
	if err != nil {
		slog.Error("delete account: local wipe failed", "user_id", userID, "error", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to delete account data")
	}

	slog.Info("account deleted (Clerk + local wipe)", "user_id", userID)
	return c.SendStatus(fiber.StatusNoContent)
}

// handleBlockUser blocks a user.
func handleBlockUser(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}

	targetID := c.Params("id")
	if targetID == "" || targetID == userID {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid target user id")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify target exists.
	var target database.User
	if err := db.First(&target, "id = ?", targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		block := database.UserBlock{
			BlockerID: userID,
			BlockedID: targetID,
			CreatedAt: time.Now(),
		}
		result := tx.Where(database.UserBlock{
			BlockerID: userID,
			BlockedID: targetID,
		}).FirstOrCreate(&block)
		if result.Error != nil {
			return nil, fmt.Errorf("create block: %w", result.Error)
		}

		// Remove any existing follow relationship in both directions (blocking
		// severs follows), atomically with the block row.
		var removedFollow bool
		r1 := tx.Where("follower_id = ? AND following_id = ?", userID, targetID).Delete(&database.UserFollow{})
		if r1.Error != nil {
			return nil, fmt.Errorf("remove follow (out): %w", r1.Error)
		}
		if r1.RowsAffected > 0 {
			removedFollow = true
		}
		r2 := tx.Where("follower_id = ? AND following_id = ?", targetID, userID).Delete(&database.UserFollow{})
		if r2.Error != nil {
			return nil, fmt.Errorf("remove follow (in): %w", r2.Error)
		}
		if r2.RowsAffected > 0 {
			removedFollow = true
		}

		// Blocking a followed user severs that follow — emit the catalog's
		// typed follow-removal event in-tx. The catalog has no user.blocked
		// event; a follow removal is the external signal a block produces.
		if removedFollow {
			if err := outbox.PublishEvent(tx, outbox.EventFollowRemoved, userID, map[string]any{
				"actor":      userID,
				"target":     targetID,
				"blocked_id": targetID,
			}); err != nil {
				return nil, fmt.Errorf("publish follow-removed: %w", err)
			}
		}

		// Blocking changes both users' follower/following views and the blocked
		// user's profile relationship — bump both version counters.
		queueUserVersion(cacheTx, userID)
		queueUserVersion(cacheTx, targetID)
		return cacheTx, nil
	})
	if err != nil {
		slog.Error("block user failed", "user_id", userID, "target_id", targetID, "error", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to block user")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// handleUnblockUser unblocks a user.
func handleUnblockUser(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}

	targetID := c.Params("id")
	if targetID == "" {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid target user id")
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		result := tx.Where("blocker_id = ? AND blocked_id = ?", userID, targetID).Delete(&database.UserBlock{})
		if result.Error != nil {
			return nil, fmt.Errorf("unblock user: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil, errBlockNotFound
		}

		queueUserVersion(cacheTx, userID)
		queueUserVersion(cacheTx, targetID)
		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errBlockNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "block not found")
		}
		slog.Error("unblock user failed", "user_id", userID, "target_id", targetID, "error", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to unblock user")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Cache/version helpers
// ---------------------------------------------------------------------------

var errBlockNotFound = errors.New("block not found")

// queueUserVersion queues a version-counter bump for a user so cached
// follower/following/profile responses are invalidated in O(1) after a
// block/unblock (blocking severs follows and changes relationship views).
func queueUserVersion(cacheTx *cache.CacheTransaction, userID string) {
	uvk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(uvk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  uvk,
		"version_only": true,
	})
}

// ---------------------------------------------------------------------------
// Settings helpers
// ---------------------------------------------------------------------------

// defaultSettings returns the default settings shape.
//
// The privacy defaults are the NON-tightening values: history/activity default
// to "public", matching the pre-feature behaviour (with is_private=false every
// section was already public). The per-section flags are opt-in tightening, so
// the displayed default must agree with enforcement — a row that never set a
// flag stays public, and a default of "friends" here would both lie about
// enforcement and let a settings save silently flip an existing row private.
func defaultSettings() SettingsResponse {
	return SettingsResponse{
		Privacy: PrivacySettings{
			Profile:  "public",
			History:  "public",
			Ratings:  "public",
			Activity: "public",
		},
		Notifications: NotificationSettings{
			Follows:        true,
			Replies:        true,
			ImportComplete: true,
			WeeklyDigest:   false,
		},
	}
}

// parseSettings unmarshals the settings JSON string, falling back to defaults.
func parseSettings(raw string) SettingsResponse {
	if raw == "" || raw == "{}" || raw == "null" {
		return defaultSettings()
	}
	var s SettingsResponse
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return defaultSettings()
	}
	// Backfill missing fields with defaults.
	def := defaultSettings()
	if s.Privacy.Profile == "" {
		s.Privacy.Profile = def.Privacy.Profile
	}
	if s.Privacy.History == "" {
		s.Privacy.History = def.Privacy.History
	}
	if s.Privacy.Ratings == "" {
		s.Privacy.Ratings = def.Privacy.Ratings
	}
	if s.Privacy.Activity == "" {
		s.Privacy.Activity = def.Privacy.Activity
	}
	return s
}

// ---------------------------------------------------------------------------
// Response builders
// ---------------------------------------------------------------------------

func buildProfileResponse(u *database.User) ProfileResponse {
	return ProfileResponse{
		ID:          u.ID,
		Username:    u.Username,
		Name:        u.Name,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		Location:    u.Location,
		Timezone:    u.Timezone,
		Language:    u.Language,
		IsPrivate:   u.IsPrivate,
		IsAdmin:     u.IsAdmin,
		IsModerator: u.IsModerator,
		Status:      u.Status,
		Plan:        u.Plan,
		CreatedAt:   response.FormatTime(u.CreatedAt),
	}
}

func buildPublicProfileResponse(u *database.User) PublicProfileResponse {
	return PublicProfileResponse{
		ID:        u.ID,
		Username:  u.Username,
		Name:      u.Name,
		Bio:       u.Bio,
		AvatarURL: u.AvatarURL,
		Location:  u.Location,
		IsPrivate: u.IsPrivate,
	}
}

// ---------------------------------------------------------------------------
// Error helper
// ---------------------------------------------------------------------------
