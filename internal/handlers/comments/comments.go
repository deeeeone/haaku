package comments

import (
	"errors"
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
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
func RegisterRoutes(api fiber.Router) {
	cg := api.Group("/comments")

	cg.Get("/trending", handleTrending)
	cg.Get("/recent", handleRecent)
	cg.Get("/updates", handleUpdates)
	cg.Post("/", requireAuth, handleCreateComment)
	cg.Get("/:id", handleGetComment)
	cg.Put("/:id", requireAuth, handleEditComment)
	cg.Delete("/:id", requireAuth, handleDeleteComment)
	cg.Get("/:id/replies", handleGetReplies)
	cg.Post("/:id/replies", requireAuth, handleCreateReply)
	cg.Post("/:id/like", requireAuth, handleLikeComment)
	cg.Delete("/:id/like", requireAuth, handleUnlikeComment)
	cg.Get("/:id/likes", handleGetLikes)
	cg.Get("/:id/reactions", handleGetReactions)
	cg.Post("/:id/reactions", requireAuth, handleAddReaction)
	cg.Delete("/:id/reactions", requireAuth, handleRemoveReaction)
	cg.Post("/:id/report", requireAuth, handleReportComment)
	cg.Get("/:id/item", handleGetCommentItem)
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func requireAuth(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return writeError(c, fiber.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return c.Next()
}

// ---------------------------------------------------------------------------
// Request/Response types
// ---------------------------------------------------------------------------

type commentCreateRequest struct {
	Comment string `json:"comment"`
	Spoiler *bool  `json:"spoiler,omitempty"`
	Item    *struct {
		Movie   *int64 `json:"movie,omitempty"`
		Show    *int64 `json:"show,omitempty"`
		Season  *int64 `json:"season,omitempty"`
		Episode *int64 `json:"episode,omitempty"`
		List    *int64 `json:"list,omitempty"`
	} `json:"item,omitempty"`
}

type commentEditRequest struct {
	Comment string `json:"comment"`
	Spoiler *bool  `json:"spoiler,omitempty"`
}

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
	ID          int64      `json:"id"`
	UserID      string     `json:"user_id"`
	ParentID    *int64     `json:"parent_id,omitempty"`
	MediaType   string     `json:"media_type,omitempty"`
	MediaID     *int64     `json:"media_id,omitempty"`
	ListID      *int64     `json:"list_id,omitempty"`
	CommentType string     `json:"comment_type"`
	Body        string     `json:"body"`
	Spoiler     bool       `json:"spoiler"`
	LikeCount   int        `json:"like_count"`
	ReplyCount  int        `json:"reply_count"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func toCommentResponse(c *database.Comment) commentResponse {
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
		EditedAt:    c.EditedAt,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

type paginatedResponse struct {
	Data       []commentResponse `json:"data"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	TotalCount int64             `json:"total_count"`
}

type reactionsResponse struct {
	Reactions map[string]int64 `json:"reactions"`
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
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := queryInt(c, "page", 1)
	limit := clampLimit(queryInt(c, "limit", 20), 1, 100)

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
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := queryInt(c, "page", 1)
	limit := clampLimit(queryInt(c, "limit", 20), 1, 100)

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
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	commentType := c.Query("comment_type", "shout,review")
	mediaType := c.Query("media_type", "movie,show")

	page := queryInt(c, "page", 1)
	limit := clampLimit(queryInt(c, "limit", 20), 1, 100)

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

// GET /v1/comments/:id
func handleGetComment(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(toCommentResponse(&comment))
}

// POST /v1/comments
func handleCreateComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req commentCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if req.Comment == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "comment body is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	comment := database.Comment{
		UserID:      userID,
		Body:        req.Comment,
		CommentType: "shout",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if req.Spoiler != nil {
		comment.Spoiler = *req.Spoiler
	}

	// Determine media type and ID from the item block.
	if req.Item != nil {
		if req.Item.Movie != nil {
			comment.MediaType = "movie"
			comment.MediaID = req.Item.Movie
		} else if req.Item.Show != nil {
			comment.MediaType = "show"
			comment.MediaID = req.Item.Show
		} else if req.Item.Season != nil {
			comment.MediaType = "season"
			comment.MediaID = req.Item.Season
		} else if req.Item.Episode != nil {
			comment.MediaType = "episode"
			comment.MediaID = req.Item.Episode
		} else if req.Item.List != nil {
			comment.ListID = req.Item.List
		}
	}

	if err := db.Create(&comment).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to create comment")
	}

	return c.Status(fiber.StatusCreated).JSON(toCommentResponse(&comment))
}

// PUT /v1/comments/:id
func handleEditComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req commentEditRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if req.Comment == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "comment body is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if comment.UserID != userID {
		return writeError(c, fiber.StatusForbidden, "forbidden", "you can only edit your own comments")
	}

	now := time.Now()
	updates := map[string]interface{}{
		"body":       req.Comment,
		"edited_at":  now,
		"updated_at": now,
	}
	if req.Spoiler != nil {
		updates["spoiler"] = *req.Spoiler
	}

	if err := db.Model(&comment).Updates(updates).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to update comment")
	}

	// Reload to get updated values
	db.First(&comment, id)
	return c.JSON(toCommentResponse(&comment))
}

// DELETE /v1/comments/:id
func handleDeleteComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	if comment.UserID != userID {
		// Moderators and admins can delete any comment
		// Check user role from DB
		var user database.User
		db.First(&user, "id = ?", userID)
		if !user.IsModerator && !user.IsAdmin {
			return writeError(c, fiber.StatusForbidden, "forbidden", "you can only delete your own comments")
		}
	}

	// Only allow delete if < 14 days old OR 0 replies
	age := time.Since(comment.CreatedAt)
	if age > 14*24*time.Hour && comment.ReplyCount > 0 {
		return writeError(c, fiber.StatusConflict, "conflict", "comment is older than 14 days and has replies — cannot delete")
	}

	if err := db.Delete(&comment).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to delete comment")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /v1/comments/:id/replies
func handleGetReplies(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var parent database.Comment
	if err := db.First(&parent, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	page := queryInt(c, "page", 1)
	limit := clampLimit(queryInt(c, "limit", 20), 1, 100)

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
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if req.Comment == "" {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "comment body is required")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var parent database.Comment
	if err := db.First(&parent, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// 404 if parent is itself a reply (no nested replies)
	if parent.ParentID != nil {
		return writeError(c, fiber.StatusNotFound, "not_found", "cannot reply to a reply")
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

	if err := db.Create(&reply).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to create reply")
	}

	// Increment parent's reply count
	db.Model(&parent).UpdateColumn("reply_count", gorm.Expr("reply_count + 1"))

	return c.Status(fiber.StatusCreated).JSON(toCommentResponse(&reply))
}

// POST /v1/comments/:id/like
func handleLikeComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Upsert the like
	like := database.CommentLike{
		CommentID: id,
		UserID:    userID,
		CreatedAt: time.Now(),
	}

	// Try to create; if conflict, that's fine (idempotent)
	result := db.Where("comment_id = ? AND user_id = ?", id, userID).FirstOrCreate(&like)
	if result.Error != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to like comment")
	}

	// Update like_count to be accurate
	var count int64
	db.Model(&database.CommentLike{}).Where("comment_id = ?", id).Count(&count)
	db.Model(&comment).UpdateColumn("like_count", count)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "liked"})
}

// DELETE /v1/comments/:id/like
func handleUnlikeComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	db.Where("comment_id = ? AND user_id = ?", id, userID).Delete(&database.CommentLike{})

	// Update like_count
	var count int64
	db.Model(&database.CommentLike{}).Where("comment_id = ?", id).Count(&count)
	db.Model(&comment).UpdateColumn("like_count", count)

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /v1/comments/:id/likes
func handleGetLikes(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	var likes []database.CommentLike
	db.Where("comment_id = ?", id).Find(&likes)

	users := make([]string, len(likes))
	for i, l := range likes {
		users[i] = l.UserID
	}

	return c.JSON(likesResponse{Users: users})
}

// GET /v1/comments/:id/reactions
func handleGetReactions(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	var results []struct {
		ReactionType string
		Count        int64
	}
	db.Model(&database.CommentReaction{}).
		Select("reaction_type, count(*) as count").
		Where("comment_id = ?", id).
		Group("reaction_type").
		Scan(&results)

	reactions := make(map[string]int64)
	for _, r := range results {
		reactions[r.ReactionType] = r.Count
	}

	return c.JSON(reactionsResponse{Reactions: reactions})
}

// POST /v1/comments/:id/reactions
func handleAddReaction(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reactionRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if !allowedReactions[req.Reaction] {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid reaction type; allowed: 👍🔥😂😮😢😡")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	reaction := database.CommentReaction{
		CommentID:    id,
		UserID:       userID,
		ReactionType: req.Reaction,
		CreatedAt:    time.Now(),
	}

	result := db.Where("comment_id = ? AND user_id = ? AND reaction_type = ?",
		id, userID, req.Reaction).FirstOrCreate(&reaction)
	if result.Error != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to add reaction")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "reaction_added"})
}

// DELETE /v1/comments/:id/reactions
func handleRemoveReaction(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reactionRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	db.Where("comment_id = ? AND user_id = ? AND reaction_type = ?",
		id, userID, req.Reaction).Delete(&database.CommentReaction{})

	return c.SendStatus(fiber.StatusNoContent)
}

// POST /v1/comments/:id/report
func handleReportComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req reportRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if !allowedReportReasons[req.Reason] {
		return writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid report reason")
	}

	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	// Prevent duplicate reports from the same user
	var existing database.CommentReport
	if err := db.Where("comment_id = ? AND reporter_id = ?", id, userID).First(&existing).Error; err == nil {
		return writeError(c, fiber.StatusConflict, "conflict", "you have already reported this comment")
	}

	report := database.CommentReport{
		CommentID:  id,
		ReporterID: userID,
		Reason:     req.Reason,
		CreatedAt:  time.Now(),
	}

	if err := db.Create(&report).Error; err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "failed to submit report")
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"status": "reported"})
}

// GET /v1/comments/:id/item
func handleGetCommentItem(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	id, parseErr := parseID(c)
	if parseErr != nil {
		return parseErr
	}

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return writeError(c, fiber.StatusNotFound, "not_found", "comment not found")
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "database query error")
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

func parseID(c *fiber.Ctx) (int64, error) {
	idStr := c.Params("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, writeError(c, fiber.StatusBadRequest, "invalid_request", "invalid comment ID")
	}
	return id, nil
}

func queryInt(c *fiber.Ctx, key string, defaultVal int) int {
	v := c.Query(key)
	if v == "" {
		return defaultVal
	}
	iv, err := strconv.Atoi(v)
	if err != nil || iv < 1 {
		return defaultVal
	}
	return iv
}

func clampLimit(limit, min, max int) int {
	if limit < min {
		return min
	}
	if limit > max {
		return max
	}
	return limit
}

func parseCSV(s string) []string {
	var parts []string
	for _, p := range splitByComma(s) {
		trimmed := trimSpace(p)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return []string{s}
	}
	return parts
}

func splitByComma(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func toCommentResponses(comments []database.Comment) []commentResponse {
	out := make([]commentResponse, len(comments))
	for i, c := range comments {
		out[i] = toCommentResponse(&c)
	}
	return out
}

func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    code,
			"message": message,
		},
	})
}
