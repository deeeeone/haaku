package comments

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/comments/trending", Summary: "Trending comments (most liked/replied in 7 days)", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Trending comments"}},
		{Method: "GET", Path: "/v1/comments/recent", Summary: "Most recent comments", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Recent comments"}},
		{Method: "GET", Path: "/v1/comments/updates", Summary: "Recently edited comments", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Recently edited comments"}},
		{Method: "GET", Path: "/v1/comments/{id}", Summary: "Single comment with reply count", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Comment", 404: "Not found"}},
		{Method: "GET", Path: "/v1/comments/{id}/replies", Summary: "Replies paginated", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Replies", 404: "Not found"}},
		{Method: "POST", Path: "/v1/comments/{id}/replies", Summary: "Reply to a comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{201: "Created", 400: "Bad request", 404: "Parent not found"}},
		{Method: "POST", Path: "/v1/comments", Summary: "Create a new comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{201: "Created", 400: "Bad request"}},
		{Method: "PUT", Path: "/v1/comments/{id}", Summary: "Edit own comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{200: "Updated", 403: "Not your comment", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/comments/{id}", Summary: "Delete own comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{204: "Deleted", 403: "Not allowed", 404: "Not found", 409: "Too old or has replies"}},
		{Method: "POST", Path: "/v1/comments/{id}/like", Summary: "Like a comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{201: "Liked", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/comments/{id}/like", Summary: "Unlike a comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{204: "Unliked", 404: "Not found"}},
		{Method: "GET", Path: "/v1/comments/{id}/likes", Summary: "Who liked a comment", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Likes list", 404: "Not found"}},
		{Method: "GET", Path: "/v1/comments/{id}/reactions", Summary: "Reaction totals on a comment", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Reactions", 404: "Not found"}},
		{Method: "POST", Path: "/v1/comments/{id}/reactions", Summary: "Add a reaction", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{201: "Reaction added", 400: "Bad request", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/comments/{id}/reactions", Summary: "Remove a reaction", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{204: "Removed", 404: "Not found"}},
		{Method: "POST", Path: "/v1/comments/{id}/report", Summary: "Report a comment", Tags: []string{"Comments"}, Auth: "required", StatusCodes: map[int]string{201: "Reported", 400: "Bad request", 404: "Not found"}},
		{Method: "GET", Path: "/v1/comments/{id}/item", Summary: "Media/list the comment is on", Tags: []string{"Comments"}, Auth: "optional", StatusCodes: map[int]string{200: "Item", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all comment route handlers.
//
// Registration order is load-bearing: Fiber matches in registration order, so
// the literal segments at a depth must precede a :id at the same depth.
// /comments/recent/... and /comments/updates/... therefore register before
// /comments/:id/..., and /comments/:id/reactions/summary (static) registers
// before /comments/:id/reactions/:reaction_type (parameterised).
func RegisterRoutes(api fiber.Router) {
	cg := api.Group("/comments")

	cg.Get("/trending", handleTrending)
	cg.Get("/recent", handleRecent)
	cg.Get("/updates", handleUpdates)
	// Contract-shaped feeds: comment kind and media type as path segments.
	cg.Get("/recent/:comment_type/:type", handleContractRecent)
	cg.Get("/updates/:comment_type/:type", handleContractUpdates)

	cg.Post("/", auth.AuthRequired(), handleCreateComment)
	cg.Get("/:id", handleGetComment)
	cg.Put("/:id", auth.AuthRequired(), handleEditComment)
	cg.Delete("/:id", auth.AuthRequired(), handleDeleteComment)
	cg.Get("/:id/replies", handleGetReplies)
	cg.Post("/:id/replies", auth.AuthRequired(), handleCreateReply)
	cg.Post("/:id/like", auth.AuthRequired(), handleLikeComment)
	cg.Delete("/:id/like", auth.AuthRequired(), handleUnlikeComment)
	cg.Get("/:id/likes", handleGetLikes)
	// Contract reaction reads. "summary" is static and must precede the
	// parameterised reaction_type write routes below.
	cg.Get("/:id/reactions", handleGetReactions)
	cg.Get("/:id/reactions/summary", handleReactionSummary)
	cg.Post("/:id/reactions/:reaction_type", auth.AuthRequired(), handleAddContractReaction)
	cg.Delete("/:id/reactions/:reaction_type", auth.AuthRequired(), handleRemoveContractReaction)
	// Legacy body-based reaction writes (emoji), kept for the web client.
	cg.Post("/:id/reactions", auth.AuthRequired(), handleAddReaction)
	cg.Delete("/:id/reactions", auth.AuthRequired(), handleRemoveReaction)
	cg.Post("/:id/report", auth.AuthRequired(), handleReportComment)
	cg.Get("/:id/item", handleGetCommentItem)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type replyCreateRequest struct {
	Comment string `json:"comment"`
	Spoiler *bool  `json:"spoiler,omitempty"`
}

type reportRequest struct {
	Reason string `json:"reason"`
}

type reactionRequest struct {
	Reaction string `json:"reaction"`
}

type commentResponse struct {
	ID          int64   `json:"id"`
	UserID      string  `json:"user_id"`
	ParentID    *int64  `json:"parent_id,omitempty"`
	MediaType   string  `json:"media_type,omitempty"`
	MediaID     *int64  `json:"media_id,omitempty"`
	ListID      *int64  `json:"list_id,omitempty"`
	CommentType string  `json:"comment_type"`
	Body        string  `json:"body"`
	Spoiler     bool    `json:"spoiler"`
	LikeCount   int     `json:"like_count"`
	ReplyCount  int     `json:"reply_count"`
	EditedAt    *string `json:"edited_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func toCommentResponse(c *database.Comment) commentResponse {
	var editedAt *string
	if c.EditedAt != nil {
		ts := response.FormatTime(*c.EditedAt)
		editedAt = &ts
	}
	return commentResponse{
		ID:          c.ID,
		UserID:      c.UserID,
		ParentID:    c.ParentID,
		MediaType:   c.MediaType,
		MediaID:     c.MediaID,
		ListID:      c.ListID,
		CommentType: c.CommentType,
		Body:        c.Body,
		Spoiler:     c.Spoiler,
		LikeCount:   c.LikeCount,
		ReplyCount:  c.ReplyCount,
		EditedAt:    editedAt,
		CreatedAt:   response.FormatTime(c.CreatedAt),
		UpdatedAt:   response.FormatTime(c.UpdatedAt),
	}
}

type paginatedResponse struct {
	Data       []commentResponse `json:"data"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	TotalCount int64             `json:"total_count"`
}

type likesResponse struct {
	Users []string `json:"users"`
}

type itemResponse struct {
	MediaType string `json:"media_type,omitempty"`
	MediaID   *int64 `json:"media_id,omitempty"`
	ListID    *int64 `json:"list_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Allowed emoji reactions
// ---------------------------------------------------------------------------

// maxCommentLength caps comment/reply bodies at 5000 characters — Trakt's
// comment limit, kept for parity. The API spec does not state a separate cap.
const maxCommentLength = 5000

var allowedReactions = map[string]bool{
	"\U0001f44d": true, // 👍
	"\U0001f525": true, // 🔥
	"\U0001f602": true, // 😂
	"\U0001f62e": true, // 😮
	"\U0001f622": true, // 😢
	"\U0001f620": true, // 😡
}

var allowedReportReasons = map[string]bool{
	"spoilers":  true,
	"language":  true,
	"abusive":   true,
	"spam":      true,
	"bigotry":   true,
	"political": true,
	"offtopic":  true,
	"duplicate": true,
	"too_short": true,
	"other":     true,
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/comments/trending
func handleTrending(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	var totalCount int64
	query := db.Model(&database.Comment{}).
		Where("comment_type IN ? AND media_type IN ?", parseCSV(commentType), parseCSV(mediaType)).
		Where("created_at >= ?", time.Now().Add(-7*24*time.Hour))

	query.Count(&totalCount)

	var comments []database.Comment
	query.Order("like_count + reply_count DESC, created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments)

	return c.JSON(paginatedResponse{
		Data:       toCommentResponses(comments),
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// GET /v1/comments/recent
func handleRecent(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	var totalCount int64
	query := db.Model(&database.Comment{}).
		Where("comment_type IN ? AND media_type IN ?", parseCSV(commentType), parseCSV(mediaType))

	query.Count(&totalCount)

	var comments []database.Comment
	query.Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments)

	return c.JSON(paginatedResponse{
		Data:       toCommentResponses(comments),
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// GET /v1/comments/updates
func handleUpdates(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	var totalCount int64
	query := db.Model(&database.Comment{}).
		Where("comment_type IN ? AND media_type IN ?", parseCSV(commentType), parseCSV(mediaType)).
		Where("edited_at IS NOT NULL")

	query.Count(&totalCount)

	var comments []database.Comment
	query.Order("edited_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&comments)

	return c.JSON(paginatedResponse{
		Data:       toCommentResponses(comments),
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// validateCommentBody rejects an empty body and one over the length cap,
// matching the 5000-char comment limit.
func validateCommentBody(c *fiber.Ctx, body string) error {
	if body == "" {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "comment body is required")
	}
	if len(body) > maxCommentLength {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request",
			fmt.Sprintf("comment body exceeds maximum length of %d characters", maxCommentLength))
	}
	return nil
}

// GET /v1/comments/:id
func handleGetComment(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(toCommentResponse(&comment))
}

// GET /v1/comments/:id/replies
func handleGetReplies(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var parent database.Comment
	if err := db.First(&parent, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page := response.QueryInt(c, "page", 1)
	limit := response.ClampLimit(response.QueryInt(c, "limit", 20), 1, 100)

	var totalCount int64
	db.Model(&database.Comment{}).Where("parent_id = ?", id).Count(&totalCount)

	var replies []database.Comment
	db.Where("parent_id = ?", id).
		Order("created_at ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&replies)

	return c.JSON(paginatedResponse{
		Data:       toCommentResponses(replies),
		Page:       page,
		Limit:      limit,
		TotalCount: totalCount,
	})
}

// POST /v1/comments/:id/replies
func handleCreateReply(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req replyCreateRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if err := validateCommentBody(c, req.Comment); err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var parent database.Comment
	if err := db.First(&parent, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// 404 if parent is itself a reply (no nested replies)
	if parent.ParentID != nil {
		return response.WriteError(c, fiber.StatusNotFound, "not_found", "cannot reply to a reply")
	}

	reply := database.Comment{
		UserID:      userID,
		ParentID:    &id,
		MediaType:   parent.MediaType,
		MediaID:     parent.MediaID,
		ListID:      parent.ListID,
		CommentType: "shout",
		Body:        req.Comment,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if req.Spoiler != nil {
		reply.Spoiler = *req.Spoiler
	}

	var resultReply *database.Comment
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Create(&reply).Error; err != nil {
			return nil, fmt.Errorf("create reply: %w", err)
		}
		// Increment parent's reply count atomically with the insert.
		if err := tx.Model(&parent).UpdateColumn("reply_count", gorm.Expr("reply_count + 1")).Error; err != nil {
			return nil, fmt.Errorf("increment parent reply_count: %w", err)
		}
		resultReply = &reply
		bumpCommentTarget(cacheTx, &parent)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("comments: create reply failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create reply")
	}

	return c.Status(fiber.StatusCreated).JSON(toCommentResponse(resultReply))
}

// POST /v1/comments/:id/like
func handleLikeComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Try to create; if conflict, that's fine (idempotent)
	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		like := database.CommentLike{
			CommentID: id,
			UserID:    userID,
			CreatedAt: time.Now(),
		}
		result := tx.Where("comment_id = ? AND user_id = ?", id, userID).FirstOrCreate(&like)
		if result.Error != nil {
			return nil, fmt.Errorf("like comment: %w", result.Error)
		}

		// Update like_count to be accurate
		var count int64
		if err := tx.Model(&database.CommentLike{}).Where("comment_id = ?", id).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("count likes: %w", err)
		}
		if err := tx.Model(&comment).UpdateColumn("like_count", count).Error; err != nil {
			return nil, fmt.Errorf("update like_count: %w", err)
		}

		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("comments: like comment failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to like comment")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "liked"})
}

// DELETE /v1/comments/:id/like
func handleUnlikeComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Where("comment_id = ? AND user_id = ?", id, userID).Delete(&database.CommentLike{}).Error; err != nil {
			return nil, fmt.Errorf("unlike comment: %w", err)
		}

		// Update like_count
		var count int64
		if err := tx.Model(&database.CommentLike{}).Where("comment_id = ?", id).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("count likes: %w", err)
		}
		if err := tx.Model(&comment).UpdateColumn("like_count", count).Error; err != nil {
			return nil, fmt.Errorf("update like_count: %w", err)
		}

		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("comments: unlike comment failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to unlike comment")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /v1/comments/:id/likes
func handleGetLikes(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page, limit := response.ParsePagination(c, 25)

	var totalCount int64
	db.Model(&database.CommentLike{}).Where("comment_id = ?", id).Count(&totalCount)

	var likes []database.CommentLike
	db.Where("comment_id = ?", id).
		Order("created_at DESC, user_id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&likes)

	users := make([]string, len(likes))
	for i, l := range likes {
		users[i] = l.UserID
	}

	response.SetPaginationHeaders(c, page, limit, totalCount, len(users))

	return c.JSON(likesResponse{Users: users})
}

// POST /v1/comments/:id/reactions
func handleAddReaction(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reactionRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if !allowedReactions[req.Reaction] {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid reaction type; allowed: 👍🔥😂😮😢😡")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		reaction := database.CommentReaction{
			CommentID:    id,
			UserID:       userID,
			ReactionType: req.Reaction,
			CreatedAt:    time.Now(),
		}
		result := tx.Where("comment_id = ? AND user_id = ? AND reaction_type = ?",
			id, userID, req.Reaction).FirstOrCreate(&reaction)
		if result.Error != nil {
			return nil, fmt.Errorf("add reaction: %w", result.Error)
		}

		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("comments: add reaction failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to add reaction")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "reaction_added"})
}

// DELETE /v1/comments/:id/reactions
func handleRemoveReaction(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reactionRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Where("comment_id = ? AND user_id = ? AND reaction_type = ?",
			id, userID, req.Reaction).Delete(&database.CommentReaction{}).Error; err != nil {
			return nil, fmt.Errorf("remove reaction: %w", err)
		}

		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	})
	if err != nil {
		fmt.Printf("comments: remove reaction failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to remove reaction")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// POST /v1/comments/:id/report
func handleReportComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reportRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if !allowedReportReasons[req.Reason] {
		return response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "invalid report reason")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Prevent duplicate reports from the same user (checked inside the tx).
		var existing database.CommentReport
		if err := tx.Where("comment_id = ? AND reporter_id = ?", id, userID).First(&existing).Error; err == nil {
			return nil, errAlreadyReported
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("check existing report: %w", err)
		}

		report := database.CommentReport{
			CommentID:  id,
			ReporterID: userID,
			Reason:     req.Reason,
			CreatedAt:  time.Now(),
		}
		if err := tx.Create(&report).Error; err != nil {
			return nil, fmt.Errorf("submit report: %w", err)
		}

		return cacheTx, nil
	})
	if err != nil {
		if errors.Is(err, errAlreadyReported) {
			return response.WriteError(c, fiber.StatusConflict, "conflict", "you have already reported this comment")
		}
		fmt.Printf("comments: submit report failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to submit report")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "reported"})
}

// GET /v1/comments/:id/item
func handleGetCommentItem(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(itemResponse{
		MediaType: comment.MediaType,
		MediaID:   comment.MediaID,
		ListID:    comment.ListID,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	errAlreadyReported = errors.New("comment already reported by this user")
)

// bumpCommentTarget queues version-counter bumps for the media/list a comment
// is attached to, so cached media/list responses (which embed comments, likes
// and reactions) are invalidated in O(1) after any comment write. The webhook
// catalog defines no comment/reaction events, so no outbox event is emitted.
func bumpCommentTarget(cacheTx *cache.CacheTransaction, comment *database.Comment) {
	if comment == nil {
		return
	}
	if comment.MediaType != "" && comment.MediaID != nil && *comment.MediaID != 0 {
		mvk := redis.BuildMediaVersionKey(comment.MediaType, *comment.MediaID, nil, nil)
		cacheTx.AddInvalidation(mvk, map[string]interface{}{
			"media_id":     *comment.MediaID,
			"media_type":   comment.MediaType,
			"version_key":  mvk,
			"version_only": true,
		})
	}
	if comment.ListID != nil && *comment.ListID != 0 {
		lvk := redis.BuildListVersionKey(*comment.ListID)
		cacheTx.AddInvalidation(lvk, map[string]interface{}{
			"list_id":      *comment.ListID,
			"version_key":  lvk,
			"version_only": true,
		})
	}
}

func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func toCommentResponses(comments []database.Comment) []commentResponse {
	out := make([]commentResponse, len(comments))
	for i, c := range comments {
		out[i] = toCommentResponse(&c)
	}
	return out
}
