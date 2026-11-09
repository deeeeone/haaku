package comments

import (
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped trending comments.
//
// Trakt's /comments/trending/{comment_type}/{type} carries the comment kind and
// the media type as path segments; our /v1/comments/trending takes them as CSV
// query params. This route reuses the same window (7 days) and the same
// ordering, pinning the two segments onto the query and emitting the contract's
// comment shape as a bare array.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/comments/trending/{comment_type}/{type}",
		Summary: "Trending comments for one media type and comment kind (contract path)",
		Tags:    []string{"Comments"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Trending comments",
			400: "Invalid comment_type or type",
		},
	})
}

// singularMediaType maps Trakt's plural type segment onto our media_type value.
var singularMediaType = map[string]string{
	"movies":   "movie",
	"shows":    "show",
	"episodes": "episode",
	"seasons":  "season",
	"lists":    "list",
}

// commentTypeFilter maps Trakt's comment_type segment onto our comment_type
// values (shout|review).
var commentTypeFilter = map[string][]string{
	"all":     {"shout", "review"},
	"shouts":  {"shout"},
	"reviews": {"review"},
}

// RegisterContractRoutes mounts the contract-shaped trending route. Called from
// routes.go after comments.RegisterRoutes so /comments/trending is registered.
func RegisterContractRoutes(api fiber.Router) {
	api.Get("/comments/trending/:comment_type/:type", handleContractTrending)
}

// handleContractTrending serves GET /v1/comments/trending/{comment_type}/{type}.
func handleContractTrending(c *fiber.Ctx) error {
	mediaType, ok := singularMediaType[c.Params("type")]
	if !ok {
		return response.BadRequest(c, "invalid type: must be one of movies, shows, episodes, seasons, lists")
	}
	commentTypes, ok := commentTypeFilter[c.Params("comment_type")]
	if !ok {
		return response.BadRequest(c, "invalid comment_type: must be one of all, shouts, reviews")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	// Clamp the page: a page of 0 (or negative) makes the offset negative, which
	// PostgreSQL rejects — a bad query parameter must not become a 500.
	page := response.QueryInt(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	var comments []database.Comment
	if err := db.Model(&database.Comment{}).
		Where("comment_type IN ? AND media_type = ?", commentTypes, mediaType).
		Where("created_at >= ?", time.Now().Add(-7*24*time.Hour)).
		Order("like_count + reply_count DESC, created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments).Error; err != nil {
		return response.InternalError(c)
	}

	return c.JSON(contractCommentItems(db, comments))
}

// contractCommentItems shapes comment rows as contract comment objects,
// resolving each author in one batched query.
func contractCommentItems(db *gorm.DB, comments []database.Comment) []map[string]any {
	userIDs := make([]string, 0, len(comments))
	seen := map[string]bool{}
	for _, cm := range comments {
		if !seen[cm.UserID] {
			seen[cm.UserID] = true
			userIDs = append(userIDs, cm.UserID)
		}
	}

	users := map[string]database.User{}
	if len(userIDs) > 0 {
		var rows []database.User
		if err := db.Where("id IN ?", userIDs).Find(&rows).Error; err == nil {
			for i := range rows {
				users[rows[i].ID] = rows[i]
			}
		}
	}

	out := make([]map[string]any, 0, len(comments))
	for i := range comments {
		cm := &comments[i]
		var user *database.User
		if u, ok := users[cm.UserID]; ok {
			user = &u
		}
		out = append(out, contractComment(cm, user, playCount(db, cm)))
	}
	return out
}

// playCount is the comment author's watch count for the commented item, read
// from user_history. It is 0 when the comment is not attached to a single media
// row we can count (e.g. list comments).
func playCount(db *gorm.DB, cm *database.Comment) int {
	if cm.MediaID == nil || cm.MediaType == "" {
		return 0
	}
	var n int64
	db.Model(&database.UserHistory{}).
		Where("user_id = ? AND media_type = ? AND media_id = ?", cm.UserID, cm.MediaType, *cm.MediaID).
		Count(&n)
	return int(n)
}

// contractComment renders one comment in the contract's shape.
func contractComment(cm *database.Comment, user *database.User, plays int) map[string]any {
	completed := 0
	if cm.MediaType == "movie" || cm.MediaType == "episode" {
		if plays > 0 {
			completed = 1
		}
	}
	return map[string]any{
		"id":          cm.ID,
		"parent_id":   cm.ParentID,
		"created_at":  contractshape.Timestamp(cm.CreatedAt),
		"updated_at":  contractshape.Timestamp(cm.UpdatedAt),
		"comment":     cm.Body,
		"gif":         nil,
		"spoiler":     cm.Spoiler,
		"review":      cm.CommentType == "review",
		"replies":     cm.ReplyCount,
		"likes":       cm.LikeCount,
		"language":    nil,
		"user_rating": nil,
		"user_stats": map[string]any{
			"rating":          nil,
			"play_count":      plays,
			"completed_count": completed,
		},
		"user": contractshape.User(user),
	}
}
