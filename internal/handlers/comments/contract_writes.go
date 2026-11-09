package comments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped comment writes.
//
// Trakt's POST /comments/, PUT /comments/{id}/ and DELETE /comments/{id}/ are
// served at the same paths our own create/edit/delete routes already answer
// (Fiber does not set StrictRouting, so the contract's trailing slash and our
// slash-less registration are the same route). These handlers therefore replace
// the legacy bodies rather than sitting beside them: one route cannot have two
// behaviours.
//
// The write body is accepted in three shapes, so the contract works and the
// web client keeps working:
//
//	contract: {"item":{"movie":{"ids":{"trakt":5}}},"comment":"...","spoiler":false}
//	legacy:   {"item":{"movie":5},"comment":"...","spoiler":false}
//	web:      {"item":{"type":"movie","id":5},"comment":"...","spoiler":false}
//
// The response is the contract's comment object. The legacy response field set
// (body/media_type/...) was never read by the web client, which only invalidates
// its comment queries on success.

// commentWriteRequest is the union of the contract, legacy and web bodies. The
// item and gif blocks are raw so one struct can accept all three item spellings
// and the tri-state gif (absent / object / explicit null to remove).
type commentWriteRequest struct {
	Comment string          `json:"comment"`
	Spoiler *bool           `json:"spoiler"`
	Review  bool            `json:"review"`
	Item    json.RawMessage `json:"item"`
	Gif     json.RawMessage `json:"gif"`
}

// errNoItemID is returned when an item block names a media kind but carries no
// usable id.
var errNoItemID = errors.New("item is missing an id")

// parseCommentItem resolves the request's item block to a media target or a
// list target. Exactly one of mediaID/listID is set on success.
func parseCommentItem(raw json.RawMessage) (mediaType string, mediaID, listID *int64, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", nil, nil, fmt.Errorf("item must be an object")
	}

	// Web shape: {"type":"movie","id":5}.
	if rawType, ok := obj["type"]; ok {
		var typ string
		if err := json.Unmarshal(rawType, &typ); err != nil {
			return "", nil, nil, fmt.Errorf("item.type must be a string")
		}
		id, err := parseItemIDValue(obj["id"])
		if err != nil {
			return "", nil, nil, err
		}
		return applyItemTarget(typ, id)
	}

	for _, typ := range []string{"movie", "show", "season", "episode", "list"} {
		value, ok := obj[typ]
		if !ok {
			continue
		}
		id, err := parseItemIDValue(value)
		if err != nil {
			return "", nil, nil, err
		}
		return applyItemTarget(typ, id)
	}
	return "", nil, nil, fmt.Errorf("item must name one of movie, show, season, episode, list")
}

func applyItemTarget(typ string, id int64) (string, *int64, *int64, error) {
	if typ == "list" {
		return "", nil, &id, nil
	}
	switch typ {
	case "movie", "show", "season", "episode":
		return typ, &id, nil, nil
	}
	return "", nil, nil, fmt.Errorf("unknown item type %q", typ)
}

// parseItemIDValue accepts a JSON number, a numeric string, or the contract's
// object form {"ids":{"trakt":N}} (also {"trakt":N}).
func parseItemIDValue(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, errNoItemID
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		if n == 0 {
			return 0, errNoItemID
		}
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		parsed, convErr := strconv.ParseInt(s, 10, 64)
		if convErr != nil || parsed == 0 {
			return 0, errNoItemID
		}
		return parsed, nil
	}
	var obj struct {
		IDs struct {
			Trakt int64 `json:"trakt"`
		} `json:"ids"`
		Trakt int64 `json:"trakt"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, errNoItemID
	}
	if obj.IDs.Trakt != 0 {
		return obj.IDs.Trakt, nil
	}
	if obj.Trakt != 0 {
		return obj.Trakt, nil
	}
	return 0, errNoItemID
}

// parseCommentGif returns the gif to store and whether the request carried the
// gif key at all. A JSON null clears it (the contract's "send null to remove").
func parseCommentGif(raw json.RawMessage) (url, slug *string, present bool, err error) {
	if len(raw) == 0 {
		return nil, nil, false, nil
	}
	if string(raw) == "null" {
		return nil, nil, true, nil
	}
	var g struct {
		URL  string  `json:"url"`
		Slug *string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, nil, true, fmt.Errorf("gif must be an object or null")
	}
	if strings.TrimSpace(g.URL) == "" {
		return nil, nil, true, fmt.Errorf("gif.url is required")
	}
	return &g.URL, g.Slug, true, nil
}

// POST /v1/comments (contract POST /comments/)
func handleCreateComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req commentWriteRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if err := validateCommentBody(c, req.Comment); err != nil {
		return err
	}

	mediaType, mediaID, listID, err := parseCommentItem(req.Item)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	if mediaID == nil && listID == nil {
		return response.BadRequest(c, "item is required: one of movie, show, season, episode, list")
	}
	gifURL, gifSlug, _, err := parseCommentGif(req.Gif)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	commentType := "shout"
	if req.Review {
		commentType = "review"
	}
	comment := database.Comment{
		UserID:      userID,
		MediaType:   mediaType,
		MediaID:     mediaID,
		ListID:      listID,
		CommentType: commentType,
		Body:        req.Comment,
		Spoiler:     req.Spoiler != nil && *req.Spoiler,
		GIFURL:      gifURL,
		GIFSlug:     gifSlug,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Create(&comment).Error; err != nil {
			return nil, fmt.Errorf("create comment: %w", err)
		}
		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	}); err != nil {
		fmt.Printf("comments: create comment failed: %v\n", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to create comment")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	return c.Status(fiber.StatusCreated).JSON(contractComment(&comment, loadCommentAuthor(comment.UserID), playCount(db, &comment)))
}

// PUT /v1/comments/{id} (contract PUT /comments/{id}/)
func handleEditComment(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req commentWriteRequest
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

	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	allowed, err := commentWriteAllowed(c, db, &comment, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return response.Forbidden(c, "you can only edit your own comments")
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
	if req.Review {
		updates["comment_type"] = "review"
	}
	gifURL, gifSlug, present, gifErr := parseCommentGif(req.Gif)
	if gifErr != nil {
		return response.BadRequest(c, gifErr.Error())
	}
	if present {
		updates["gif_url"] = gifURL
		updates["gif_slug"] = gifSlug
	}

	if err := db.Model(&comment).Updates(updates).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update comment")
	}
	if err := db.First(&comment, id).Error; err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	return c.JSON(contractComment(&comment, loadCommentAuthor(comment.UserID), playCount(db, &comment)))
}

// DELETE /v1/comments/{id} (contract DELETE /comments/{id}/)
func handleDeleteComment(c *fiber.Ctx) error {
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
			return response.NotFound(c, "comment not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	allowed, err := commentWriteAllowed(c, db, &comment, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return response.Forbidden(c, "you can only delete your own comments")
	}

	// Only allow delete if < 14 days old OR 0 replies.
	if time.Since(comment.CreatedAt) > 14*24*time.Hour && comment.ReplyCount > 0 {
		return response.Conflict(c, "comment is older than 14 days and has replies — cannot delete")
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Delete(&comment).Error; err != nil {
			return nil, fmt.Errorf("delete comment: %w", err)
		}
		bumpCommentTarget(cacheTx, &comment)
		return cacheTx, nil
	}); err != nil {
		fmt.Printf("comments: delete comment %d failed: %v\n", id, err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to delete comment")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// commentWriteAllowed reports whether the caller may edit or delete the comment:
// the author, or a moderator/admin. A caller with no user row is denied rather
// than treated as a non-moderator silently.
func commentWriteAllowed(c *fiber.Ctx, db *gorm.DB, comment *database.Comment, userID string) (bool, error) {
	if comment.UserID == userID {
		return true, nil
	}
	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	return user.IsModerator || user.IsAdmin, nil
}

// loadCommentAuthor reads the author row for the contract comment envelope.
// A missing row renders as the contract's deleted user rather than failing the
// write: the comment was created and the caller must not see a 500.
func loadCommentAuthor(userID string) *database.User {
	db, err := database.GetDB()
	if err != nil {
		return nil
	}
	var user database.User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		return nil
	}
	return &user
}

// contractGif renders the stored gif in the contract's shape, or nil.
func contractGif(cm *database.Comment) any {
	if cm.GIFURL == nil || *cm.GIFURL == "" {
		return nil
	}
	var slug any
	if cm.GIFSlug != nil && *cm.GIFSlug != "" {
		slug = *cm.GIFSlug
	}
	return map[string]any{"url": *cm.GIFURL, "slug": slug}
}
