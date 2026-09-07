package activity

import (
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"trakt-api/internal/auth"
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
	me.Get("/activity", auth.AuthRequired(), handleGetActivity)
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
	CreatedAt string                 `json:"created_at"`
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
		CreatedAt: response.FormatTime(a.CreatedAt),
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

	page, limit := response.ParsePagination(c, 25)
	feedType := c.Query("type", "friends")
	switch feedType {
	case "following", "followers", "friends", "":
	default:
		return response.BadRequest(c, "unknown activity feed type")
	}

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
		if err != nil {
			return response.BadRequest(c, "invalid start_at: must be an RFC3339 timestamp")
		}
		startTime = &t
	}

	if len(actorIDs) == 0 {
		// No social graph — return empty
		setPaginationHeaders(c, page, limit, 0, 0)
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
	query.Order("created_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&activities)

	data := make([]activityResponse, len(activities))
	for i, a := range activities {
		data[i] = toActivityResponse(&a)
	}

	setPaginationHeaders(c, page, limit, totalCount, len(data))

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

// setPaginationHeaders emits the §7 pagination headers (api-conventions.md)
// plus a Link: rel="next" header when more pages exist.
func setPaginationHeaders(c *fiber.Ctx, page, limit int, total int64, itemsCount int) {
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(itemsCount))
	if page < int(totalPages) {
		c.Set("Link", `<`+c.Path()+`?page=`+strconv.Itoa(page+1)+`&limit=`+strconv.Itoa(limit)+`>; rel="next"`)
	}
}
