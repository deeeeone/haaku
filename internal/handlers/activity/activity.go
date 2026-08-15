package activity

import (
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/me/activity", Summary: "Recent activity from social graph", Tags: []string{"Users"}, Auth: "required", StatusCodes: map[int]string{200: "Activity feed", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires activity route handlers.
func RegisterRoutes(api fiber.Router) {
	me := api.Group("/users/me")
	me.Get("/activity", requireAuth, handleGetActivity)
}

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

type activityResponse struct {
	ID        int64                  `json:"id"`
	ActorID   string                 `json:"actor_id"`
	EventType string                 `json:"event_type"`
	MediaType string                 `json:"media_type,omitempty"`
	MediaID   *int64                 `json:"media_id,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}

type paginatedActivityResponse struct {
	Data       []activityResponse `json:"data"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	TotalCount int64              `json:"total_count"`
}

func toActivityResponse(a *database.ActivityFeed) activityResponse {
	payload := make(map[string]interface{})
	if a.Payload != nil {
		for k, v := range a.Payload {
			payload[k] = v
		}
	}
	return activityResponse{
		ID:        a.ID,
		ActorID:   a.ActorID,
		EventType: a.EventType,
		MediaType: a.MediaType,
		MediaID:   a.MediaID,
		Payload:   payload,
		CreatedAt: a.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// GET /v1/users/me/activity
func handleGetActivity(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)
	feedType := c.Query("type", "friends")

	// Collect actor IDs from the social graph
	var actorIDs []string

	switch feedType {
	case "following":
		// Only users the current user follows
		var follows []database.UserFollow
		db.Where("follower_id = ?", userID).Find(&follows)
		for _, f := range follows {
			actorIDs = append(actorIDs, f.FollowingID)
		}
	case "followers":
		// Only users who follow the current user
		var follows []database.UserFollow
		db.Where("following_id = ?", userID).Find(&follows)
		for _, f := range follows {
			actorIDs = append(actorIDs, f.FollowerID)
		}
	case "friends", "":
		// Mutual follows (friends) — both follower and following
		var following []database.UserFollow
		db.Where("follower_id = ?", userID).Find(&following)
		var followers []database.UserFollow
		db.Where("following_id = ?", userID).Find(&followers)

		followingSet := make(map[string]bool)
		for _, f := range following {
			followingSet[f.FollowingID] = true
		}
		for _, f := range followers {
			if followingSet[f.FollowerID] {
				actorIDs = append(actorIDs, f.FollowerID)
			}
		}
	}

	// Start at filter
	startAt := c.Query("start_at")
	var startTime *time.Time
	if startAt != "" {
		t, err := time.Parse(time.RFC3339, startAt)
		if err == nil {
			startTime = &t
		}
	}

	if len(actorIDs) == 0 {
		// No social graph — return empty
		return c.JSON(paginatedActivityResponse{
			Data:       []activityResponse{},
			Page:       page,
			Limit:      limit,
			TotalCount: 0,
		})
	}

	var totalCount int64
	query := db.Model(&database.ActivityFeed{}).Where("actor_id IN ?", actorIDs)
	if startTime != nil {
		query = query.Where("created_at >= ?", *startTime)
	}
	query.Count(&totalCount)

	var activities []database.ActivityFeed
	query.Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&activities)

	data := make([]activityResponse, len(activities))
	for i, a := range activities {
		data[i] = toActivityResponse(&a)
	}

	return c.JSON(paginatedActivityResponse{
		Data:       data,
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
