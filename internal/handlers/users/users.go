package users

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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
	me.Get("/", requireAuth, handleGetMe)
	me.Put("/", requireAuth, handleUpdateMe)
	me.Get("/settings", requireAuth, handleGetSettings)
	me.Put("/settings", requireAuth, handleUpdateSettings)
	me.Delete("/", requireAuth, handleDeleteAccount)

	// /users/:id routes (public, privacy-filtered)
	users.Get("/:id", auth.PrivacyFilter("id"), handleGetUser)

	// Block endpoints (stub — 501)
	users.Post("/:id/block", requireAuth, handleBlockUser)
	users.Delete("/:id/block", requireAuth, handleUnblockUser)
}

// ---------------------------------------------------------------------------
// Auth helper middleware
// ---------------------------------------------------------------------------

// requireAuth is a local middleware that requires authentication.
// Returns 401 if c.Locals("user_id") is not set.
func requireAuth(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return response.WriteError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return c.Next()
}

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

// StatsSummary is a stub for user stats.
type StatsSummary struct {
	Watched   int64 `json:"watched"`
	Lists     int64 `json:"lists"`
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
	db.First(&user, "id = ?", userID)

	return c.JSON(buildProfileResponse(&user))
}

// handleGetUser returns a public profile for the given user ID.
func handleGetUser(c *fiber.Ctx) error {
	userID := c.Params("id")

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

	// Deleted/pending-deletion accounts are gone for everyone (rows from the
	// pre-wipe soft-delete era; DeleteUserData hard-deletes now).
	if user.Status == "deleted" || user.Status == "pending_deletion" {
		return response.WriteError(c, fiber.StatusNotFound, "not_found", "user not found")
	}

	resp := buildPublicProfileResponse(&user)

	// Populate stats stubs.
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

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	if err := database.DeleteUserData(db, userID); err != nil {
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

	block := database.UserBlock{
		BlockerID: userID,
		BlockedID: targetID,
		CreatedAt: time.Now(),
	}

	result := db.Where(database.UserBlock{
		BlockerID: userID,
		BlockedID: targetID,
	}).FirstOrCreate(&block)

	if result.Error != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to block user")
	}

	// Remove any existing follow relationship in both directions.
	db.Where("follower_id = ? AND following_id = ?", userID, targetID).Delete(&database.UserFollow{})
	db.Where("follower_id = ? AND following_id = ?", targetID, userID).Delete(&database.UserFollow{})

	// Publish outbox event.
	_ = outbox.PublishEvent(db, "user.blocked", userID, map[string]any{"blocked_id": targetID})

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

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	result := db.Where("blocker_id = ? AND blocked_id = ?", userID, targetID).Delete(&database.UserBlock{})
	if result.Error != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to unblock user")
	}
	if result.RowsAffected == 0 {
		return response.WriteError(c, fiber.StatusNotFound, "not_found", "block not found")
	}

	_ = outbox.PublishEvent(db, "user.unblocked", userID, map[string]any{"unblocked_id": targetID})

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Settings helpers
// ---------------------------------------------------------------------------

// defaultSettings returns the default settings shape.
func defaultSettings() SettingsResponse {
	return SettingsResponse{
		Privacy: PrivacySettings{
			Profile:  "public",
			History:  "friends",
			Ratings:  "public",
			Activity: "friends",
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
		CreatedAt:   u.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
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
