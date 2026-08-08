package social

import (
	"errors"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"
	"trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "POST", Path: "/v1/users/{id}/follow", Summary: "Follow a user", Tags: []string{"Social"}, Auth: "required", StatusCodes: map[int]string{201: "Followed (or pending approval)", 400: "Invalid request", 404: "User not found", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/users/{id}/follow", Summary: "Unfollow a user", Tags: []string{"Social"}, Auth: "required", StatusCodes: map[int]string{204: "Unfollowed", 404: "Not found", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/users/{id}/followers", Summary: "List followers", Tags: []string{"Social"}, Auth: "optional", StatusCodes: map[int]string{200: "Followers list", 404: "User not found"}},
		{Method: "GET", Path: "/v1/users/{id}/following", Summary: "List following", Tags: []string{"Social"}, Auth: "optional", StatusCodes: map[int]string{200: "Following list", 404: "User not found"}},
		{Method: "GET", Path: "/v1/users/{id}/friends", Summary: "List mutual follows", Tags: []string{"Social"}, Auth: "optional", StatusCodes: map[int]string{200: "Friends list", 404: "User not found"}},
		{Method: "GET", Path: "/v1/users/me/follow_requests", Summary: "List pending incoming follow requests", Tags: []string{"Social"}, Auth: "required", StatusCodes: map[int]string{200: "Follow requests", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/users/me/follow_requests/{follower_id}/approve", Summary: "Approve a follow request", Tags: []string{"Social"}, Auth: "required", StatusCodes: map[int]string{200: "Approved", 404: "Request not found", 401: "Unauthorized"}},
		{Method: "DELETE", Path: "/v1/users/me/follow_requests/{follower_id}", Summary: "Deny a follow request", Tags: []string{"Social"}, Auth: "required", StatusCodes: map[int]string{204: "Denied", 404: "Request not found", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires social routes.
func RegisterRoutes(api fiber.Router) {
	// /v1/users/:id/follow
	api.Post("/users/:id/follow", auth.AuthRequired(), handleFollow)
	api.Delete("/users/:id/follow", auth.AuthRequired(), handleUnfollow)

	// /v1/users/:id/followers, /following, /friends (public reads)
	api.Get("/users/:id/followers", handleListFollowers)
	api.Get("/users/:id/following", handleListFollowing)
	api.Get("/users/:id/friends", handleListFriends)

	// /v1/users/me/follow_requests
	api.Get("/users/me/follow_requests", auth.AuthRequired(), handleListFollowRequests)
	api.Post("/users/me/follow_requests/:follower_id/approve", auth.AuthRequired(), handleApproveFollowRequest)
	api.Delete("/users/me/follow_requests/:follower_id", auth.AuthRequired(), handleDenyFollowRequest)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// FollowResponse is the API shape for a follow action.
type FollowResponse struct {
	FollowerID  string  `json:"follower_id"`
	FollowingID string  `json:"following_id"`
	Status      string  `json:"status"` // approved | pending
	CreatedAt   string  `json:"created_at"`
	ApprovedAt  *string `json:"approved_at,omitempty"`
}

// UserProfileStub is a minimal user reference for social responses.
type UserProfileStub struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// FollowRequestResponse is the API shape for a pending follow request.
type FollowRequestResponse struct {
	FollowerID string `json:"follower_id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Handlers — Follow / Unfollow
// ---------------------------------------------------------------------------

// queueFollowInvalidation bumps the version counters of both users involved,
// so cached follower/following/friend/request responses are invalidated in O(1).
func queueFollowInvalidation(cacheTx *cache.CacheTransaction, userA, userB string) {
	for _, uid := range []string{userA, userB} {
		uvk := redis.BuildUserVersionKey(uid)
		cacheTx.AddInvalidation(uvk, map[string]interface{}{
			"user_id":      uid,
			"version_key":  uvk,
			"version_only": true,
		})
	}
}

// handleFollow follows a user. If target has private profile, it creates a pending follow.
func handleFollow(c *fiber.Ctx) error {
	followerID, ok := c.Locals("user_id").(string)
	if !ok || followerID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	targetID := c.Params("id")

	if targetID == "" || targetID == followerID {
		return badRequest(c, "invalid target user id")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify target exists.
	var target database.User
	if err := db.First(&target, "id = ?", targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "user not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Check if already following.
	var existing database.UserFollow
	if err := db.Where("follower_id = ? AND following_id = ?", followerID, targetID).First(&existing).Error; err == nil {
		// Already following — return current status.
		status := "approved"
		if existing.IsPending() {
			status = "pending"
		}
		return c.Status(fiber.StatusCreated).JSON(buildFollowResponse(&existing, status))
	}

	var resp FollowResponse
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		now := time.Now()
		follow := database.UserFollow{
			FollowerID:  followerID,
			FollowingID: targetID,
			CreatedAt:   now,
		}

		status := "approved"
		if target.IsPrivate {
			// Private profile — follow is pending.
			follow.ApprovedAt = nil
			status = "pending"
		} else {
			follow.ApprovedAt = &now
		}

		if err := tx.Create(&follow).Error; err != nil {
			return nil, err
		}

		queueFollowInvalidation(cacheTx, followerID, targetID)
		if err := outbox.PublishEvent(tx, outbox.EventFollowCreated, followerID, map[string]any{
			"actor":        followerID,
			"target":       targetID,
			"following_id": targetID,
			"status":       status,
		}); err != nil {
			return nil, err
		}

		resp = buildFollowResponse(&follow, status)
		return cacheTx, nil
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to follow user")
	}

	return c.Status(fiber.StatusCreated).JSON(resp)
}

// handleUnfollow unfollows a user.
func handleUnfollow(c *fiber.Ctx) error {
	followerID, ok := c.Locals("user_id").(string)
	if !ok || followerID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	targetID := c.Params("id")

	if targetID == "" {
		return badRequest(c, "invalid target user id")
	}

	if _, err := database.GetDB(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		result := tx.Where("follower_id = ? AND following_id = ?", followerID, targetID).Delete(&database.UserFollow{})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, errNotFollowing
		}

		queueFollowInvalidation(cacheTx, followerID, targetID)
		if err := outbox.PublishEvent(tx, outbox.EventFollowRemoved, followerID, map[string]any{
			"actor":         followerID,
			"target":        targetID,
			"unfollowed_id": targetID,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errNotFollowing) {
			return notFound(c, "not following this user")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to unfollow user")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Handlers — Followers / Following / Friends
// ---------------------------------------------------------------------------

// handleListFollowers lists followers of a user.
func handleListFollowers(c *fiber.Ctx) error {
	targetID := c.Params("id")
	return listFollowRelations(c, targetID, "followers")
}

// handleListFollowing lists users a user is following.
func handleListFollowing(c *fiber.Ctx) error {
	targetID := c.Params("id")
	return listFollowRelations(c, targetID, "following")
}

// handleListFriends lists mutual follows (friends).
func handleListFriends(c *fiber.Ctx) error {
	targetID := c.Params("id")

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Mutual follows: people that target follows AND who follow target.
	var rows []database.UserFollow
	if err := db.Raw(`
		SELECT f1.* FROM user_follows f1
		INNER JOIN user_follows f2 ON f1.follower_id = f2.following_id AND f1.following_id = f2.follower_id
		WHERE f1.follower_id = ? AND f1.approved_at IS NOT NULL AND f2.approved_at IS NOT NULL`,
		targetID).Scan(&rows).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to fetch friends")
	}

	userIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		userIDs = append(userIDs, r.FollowingID)
	}

	users := resolveUserStubs(db, userIDs)
	return c.JSON(users)
}

// listFollowRelations returns followers or following for a user.
func listFollowRelations(c *fiber.Ctx, targetID, direction string) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Verify user exists.
	var count int64
	db.Model(&database.User{}).Where("id = ?", targetID).Count(&count)
	if count == 0 {
		return notFound(c, "user not found")
	}

	var userIDs []string
	if direction == "followers" {
		var follows []database.UserFollow
		db.Where("following_id = ? AND approved_at IS NOT NULL", targetID).Find(&follows)
		for _, f := range follows {
			userIDs = append(userIDs, f.FollowerID)
		}
	} else {
		var follows []database.UserFollow
		db.Where("follower_id = ? AND approved_at IS NOT NULL", targetID).Find(&follows)
		for _, f := range follows {
			userIDs = append(userIDs, f.FollowingID)
		}
	}

	users := resolveUserStubs(db, userIDs)
	return c.JSON(users)
}

// ---------------------------------------------------------------------------
// Handlers — Follow Requests (pending approval)
// ---------------------------------------------------------------------------

// handleListFollowRequests lists pending incoming follow requests for the current user.
func handleListFollowRequests(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var follows []database.UserFollow
	if err := db.Where("following_id = ? AND approved_at IS NULL", userID).Find(&follows).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to fetch follow requests")
	}

	// Collect follower IDs.
	followerIDs := make([]string, 0, len(follows))
	idToCreatedAt := make(map[string]time.Time)
	for _, f := range follows {
		followerIDs = append(followerIDs, f.FollowerID)
		idToCreatedAt[f.FollowerID] = f.CreatedAt
	}

	// Resolve to user profiles.
	var users []database.User
	if len(followerIDs) > 0 {
		db.Where("id IN ?", followerIDs).Find(&users)
	}

	resp := make([]FollowRequestResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, FollowRequestResponse{
			FollowerID: u.ID,
			Username:   u.Username,
			Name:       u.Name,
			AvatarURL:  u.AvatarURL,
			CreatedAt:  idToCreatedAt[u.ID].Format("2006-01-02T15:04:05.000Z"),
		})
	}

	return c.JSON(resp)
}

// handleApproveFollowRequest approves a pending follow request.
func handleApproveFollowRequest(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	followerID := c.Params("follower_id")

	if followerID == "" {
		return badRequest(c, "invalid follower id")
	}

	if _, err := database.GetDB(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		now := time.Now()
		result := tx.Model(&database.UserFollow{}).
			Where("follower_id = ? AND following_id = ? AND approved_at IS NULL", followerID, userID).
			Update("approved_at", now)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, errRequestNotFound
		}

		queueFollowInvalidation(cacheTx, followerID, userID)
		if err := outbox.PublishEvent(tx, outbox.EventFollowApproved, userID, map[string]any{
			"actor":       followerID,
			"target":      userID,
			"follower_id": followerID,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errRequestNotFound) {
			return notFound(c, "follow request not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to approve follow request")
	}

	return c.JSON(fiber.Map{"status": "approved"})
}

// handleDenyFollowRequest denies a pending follow request.
func handleDenyFollowRequest(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	followerID := c.Params("follower_id")

	if followerID == "" {
		return badRequest(c, "invalid follower id")
	}

	if _, err := database.GetDB(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		result := tx.Where("follower_id = ? AND following_id = ? AND approved_at IS NULL", followerID, userID).
			Delete(&database.UserFollow{})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, errRequestNotFound
		}

		queueFollowInvalidation(cacheTx, followerID, userID)
		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errRequestNotFound) {
			return notFound(c, "follow request not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to deny follow request")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	errNotFollowing    = errors.New("not following this user")
	errRequestNotFound = errors.New("follow request not found")
)

func buildFollowResponse(f *database.UserFollow, status string) FollowResponse {
	resp := FollowResponse{
		FollowerID:  f.FollowerID,
		FollowingID: f.FollowingID,
		Status:      status,
		CreatedAt:   f.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
	}
	if f.ApprovedAt != nil {
		ts := f.ApprovedAt.Format("2006-01-02T15:04:05.000Z")
		resp.ApprovedAt = &ts
	}
	return resp
}

// resolveUserStubs fetches user profiles in bulk.
func resolveUserStubs(db *gorm.DB, userIDs []string) []UserProfileStub {
	if len(userIDs) == 0 {
		return []UserProfileStub{}
	}

	var users []database.User
	db.Where("id IN ?", userIDs).Find(&users)

	// Preserve order.
	userMap := make(map[string]database.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	stubs := make([]UserProfileStub, 0, len(userIDs))
	for _, id := range userIDs {
		if u, ok := userMap[id]; ok {
			stubs = append(stubs, UserProfileStub{
				ID:        u.ID,
				Username:  u.Username,
				Name:      u.Name,
				AvatarURL: u.AvatarURL,
			})
		}
	}
	return stubs
}

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusBadRequest, "invalid_request", msg)
}

func notFound(c *fiber.Ctx, msg string) error {
	return writeError(c, fiber.StatusNotFound, "not_found", msg)
}
