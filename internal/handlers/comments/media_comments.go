package comments

import (
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// mediaCommentSorts is the closed set of sort segments the media-scoped
// comment endpoints accept. Trakt exposes exactly these five on
// /movies/{id}/comments/{sort}, /shows/{id}/comments/{sort} and the nested
// season/episode variants; anything else is a 400 rather than a silent
// default, so a client typo does not silently return the wrong order.
var mediaCommentSorts = map[string]string{
	"newest":   "comments.created_at DESC, comments.id DESC",
	"oldest":   "comments.created_at ASC, comments.id ASC",
	"likes":    "comments.like_count DESC, comments.created_at DESC, comments.id DESC",
	"replies":  "comments.reply_count DESC, comments.created_at DESC, comments.id DESC",
	"trending": "(comments.like_count + comments.reply_count) DESC, comments.created_at DESC, comments.id DESC",
}

// ListMediaComments writes the paginated comment list for one media item.
//
// It is the single implementation behind the four media-scoped comment routes
// (movie, show, season, episode). Callers resolve the parent chain first and
// return 404 for a missing parent; this function only reads the comments table.
//
// Items are rendered with this package's own toCommentResponse, so a comment
// looks identical here and on the /v1/comments endpoints.
//
// Shape: a bare JSON array of commentResponse (Trakt's shape for these routes)
// plus the api-conventions §7 pagination headers and a Link: rel="next" header
// when another page exists.
//
// Only top-level comments are listed (parent_id IS NULL); replies stay on
// GET /v1/comments/{id}/replies. Every comment_type is included — the sort
// segment does not narrow type, matching the table's two types (shout, review).
func ListMediaComments(c *fiber.Ctx, db *gorm.DB, mediaType string, mediaID int64, sort string) error {
	order, ok := mediaCommentSorts[sort]
	if !ok {
		return response.BadRequest(c, "invalid sort: must be one of newest, oldest, likes, replies, trending")
	}

	page, limit := response.ParsePagination(c, 25)

	// Build the filtered query twice rather than reusing a chain that already
	// ran Count — GORM's Count mutates the statement's SELECT.
	base := func() *gorm.DB {
		return db.Model(&database.Comment{}).
			Where("media_type = ? AND media_id = ? AND parent_id IS NULL", mediaType, mediaID)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	var rows []database.Comment
	if err := base().
		Order(order).
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	out := toCommentResponses(rows)
	response.SetPaginationHeaders(c, page, limit, total, len(out))

	return c.JSON(out)
}
