package comments

import (
	"errors"
	"fmt"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Contract-shaped comment reactions.
//
// Trakt names its reaction in the path as one of a closed set of words
// (like|dislike|love|laugh|shocked|bravo|spoiler) and answers 204. The reaction
// store already exists (comment_reactions, keyed by comment/user/type) and is
// shared with the older emoji reaction routes, so a summary computed from it
// covers every reaction row the server has stored, whichever route wrote it.
//
// Comment likes stay in their own table: /comments/{id}/like is a distinct
// contract operation with its own row, and folding likes into reactions would
// make one write change two published counts.

var contractReactionTypes = []string{
	"like", "dislike", "love", "laugh", "shocked", "bravo", "spoiler",
}

var contractReactionSet = func() map[string]bool {
	m := make(map[string]bool, len(contractReactionTypes))
	for _, t := range contractReactionTypes {
		m[t] = true
	}
	return m
}()

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/comments/{id}/reactions",
		Summary: "Users and reaction details for every reaction on a comment",
		Tags:    []string{"Comments"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Reactions",
			404: "Not found",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/comments/{id}/reactions/summary",
		Summary: "Reaction totals for a comment, grouped by type",
		Tags:    []string{"Comments"},
		Auth:    "optional",
		StatusCodes: map[int]string{
			200: "Reaction summary",
			404: "Not found",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "POST",
		Path:    "/v1/comments/{id}/reactions/{reaction_type}",
		Summary: "Add a named reaction to a comment",
		Tags:    []string{"Comments"},
		Auth:    "required",
		StatusCodes: map[int]string{
			204: "Reaction added",
			400: "Invalid reaction type",
			404: "Not found",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "DELETE",
		Path:    "/v1/comments/{id}/reactions/{reaction_type}",
		Summary: "Remove a named reaction from a comment",
		Tags:    []string{"Comments"},
		Auth:    "required",
		StatusCodes: map[int]string{
			204: "Reaction removed",
			400: "Invalid reaction type",
			404: "Not found",
		},
	})
}

// GET /v1/comments/:id/reactions (contract GET /comments/{id}/reactions/)
func handleGetReactions(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	comment, err := loadReactionComment(c, db)
	if err != nil {
		return err
	}

	page, limit := response.ParsePagination(c, 25)

	var total int64
	if err := db.Model(&database.CommentReaction{}).Where("comment_id = ?", comment.ID).Count(&total).Error; err != nil {
		return response.InternalError(c, "database query error")
	}
	// The contract allows limit=all: one page containing every reaction.
	if c.Query("limit") == "all" {
		limit = int(total)
		if limit < 1 {
			limit = 1
		}
	}

	var rows []database.CommentReaction
	if err := db.Where("comment_id = ?", comment.ID).
		Order("created_at DESC, user_id ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	userIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		userIDs = append(userIDs, r.UserID)
	}
	users, err := contractshape.UserByIDBatch(db, userIDs)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"reacted_at": contractshape.Timestamp(r.CreatedAt),
			"reaction":   map[string]any{"type": r.ReactionType},
			"user":       contractshape.User(users[r.UserID]),
		})
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))

	return c.JSON(out)
}

// GET /v1/comments/:id/reactions/summary
func handleReactionSummary(c *fiber.Ctx) error {
	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	comment, err := loadReactionComment(c, db)
	if err != nil {
		return err
	}

	var groups []struct {
		ReactionType string
		Count        int64
	}
	if err := db.Model(&database.CommentReaction{}).
		Select("reaction_type, count(*) as count").
		Where("comment_id = ?", comment.ID).
		Group("reaction_type").
		Scan(&groups).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	distribution := make(map[string]int64, len(groups))
	var reactionCount int64
	for _, g := range groups {
		distribution[g.ReactionType] = g.Count
		reactionCount += g.Count
	}

	var userCount int64
	if err := db.Model(&database.CommentReaction{}).
		Where("comment_id = ?", comment.ID).
		Distinct("user_id").
		Count(&userCount).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	return c.JSON(map[string]any{
		"reaction_count": reactionCount,
		"user_count":     userCount,
		"distribution":   distribution,
	})
}

// POST /v1/comments/:id/reactions/:reaction_type
func handleAddContractReaction(c *fiber.Ctx) error {
	return writeContractReaction(c, true)
}

// DELETE /v1/comments/:id/reactions/:reaction_type
func handleRemoveContractReaction(c *fiber.Ctx) error {
	return writeContractReaction(c, false)
}

func writeContractReaction(c *fiber.Ctx, add bool) error {
	userID := c.Locals("user_id").(string)

	reactionType := c.Params("reaction_type")
	if !contractReactionSet[reactionType] {
		return response.BadRequest(c, "invalid reaction_type: must be one of like, dislike, love, laugh, shocked, bravo, spoiler")
	}

	db, err := database.GetDB()
	if err != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}
	comment, err := loadReactionComment(c, db)
	if err != nil {
		return err
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if add {
			reaction := database.CommentReaction{
				CommentID:    comment.ID,
				UserID:       userID,
				ReactionType: reactionType,
				CreatedAt:    time.Now(),
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&reaction).Error; err != nil {
				return nil, fmt.Errorf("add reaction: %w", err)
			}
		} else if err := tx.Where("comment_id = ? AND user_id = ? AND reaction_type = ?",
			comment.ID, userID, reactionType).Delete(&database.CommentReaction{}).Error; err != nil {
			return nil, fmt.Errorf("remove reaction: %w", err)
		}
		bumpCommentTarget(cacheTx, comment)
		return cacheTx, nil
	}); err != nil {
		fmt.Printf("comments: %s reaction on comment %d failed: %v\n", map[bool]string{true: "add", false: "remove"}[add], comment.ID, err)
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "failed to update reaction")
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// loadReactionComment parses :id and returns the comment, writing 404 when it
// does not exist. A reaction on a comment the caller cannot see is a 404, not a
// silent success.
func loadReactionComment(c *fiber.Ctx, db *gorm.DB) (*database.Comment, error) {
	id, parseErr := response.ParseIDParam(c, "comment")
	if parseErr != nil {
		return nil, parseErr
	}
	var comment database.Comment
	if err := db.First(&comment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.NotFound(c, "comment not found")
			return nil, response.ErrHandled
		}
		return nil, response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}
	return &comment, nil
}
