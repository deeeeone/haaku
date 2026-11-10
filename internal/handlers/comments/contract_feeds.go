package comments

import (
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped recent/updated comment feeds.
//
// Trakt carries the comment kind and media type as path segments
// (/comments/recent/{comment_type}/{type}); our /v1/comments/recent takes them
// as CSV query params and answers a paginated object envelope. These routes
// pin the segments onto the same query and emit the contract's bare array.
//
// They are registered BEFORE the /comments/:id routes in RegisterRoutes: the
// literal "recent"/"updates" segments sit at the same depth as a comment id and
// Fiber matches in registration order.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/comments/recent/{comment_type}/{type}",
		Summary: "Recently created comments (contract path)",
		Tags:    []string{"Comments"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Recent comments",
			400: "Invalid comment_type or type",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/comments/updates/{comment_type}/{type}",
		Summary: "Recently updated comments (contract path)",
		Tags:    []string{"Comments"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Updated comments",
			400: "Invalid comment_type or type",
		},
	})
}

// handleContractRecent serves GET /v1/comments/recent/{comment_type}/{type}.
func handleContractRecent(c *fiber.Ctx) error {
	return contractFeed(c, false)
}

// handleContractUpdates serves GET /v1/comments/updates/{comment_type}/{type}.
func handleContractUpdates(c *fiber.Ctx) error {
	return contractFeed(c, true)
}

// contractFeed is the one implementation behind both feeds. updates selects the
// edited stream (edited_at IS NOT NULL, newest edit first); recent selects the
// created stream.
func contractFeed(c *fiber.Ctx, updates bool) error {
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

	page := response.QueryInt(c, "page", 1)
	if page < 1 {
		page = 1
	}
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	// include_replies returns replies in the same stream alongside top-level
	// comments instead of only top-level rows.
	includeReplies := c.QueryBool("include_replies", false)

	base := func() *gorm.DB {
		q := db.Model(&database.Comment{}).
			Where("comment_type IN ?", commentTypes)
		if mediaType == "list" {
			// List comments carry list_id and no media_type.
			q = q.Where("list_id IS NOT NULL")
		} else {
			q = q.Where("media_type = ?", mediaType)
		}
		if !includeReplies {
			q = q.Where("parent_id IS NULL")
		}
		if updates {
			q = q.Where("edited_at IS NOT NULL")
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	order := "created_at DESC, id DESC"
	if updates {
		order = "edited_at DESC, id DESC"
	}

	var rows []database.Comment
	if err := base().
		Order(order).
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	response.SetPaginationHeaders(c, page, limit, total, len(rows))

	return c.JSON(contractCommentItems(db, rows))
}
