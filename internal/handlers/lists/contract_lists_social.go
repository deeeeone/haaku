package lists

import (
	"strings"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ============================================================================
// Likes / comments / report
// ============================================================================

func handleContractListLikes(c *fiber.Ctx) error {
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := contractLoadList(db, c.Params("list_id"))
	if err != nil {
		return listNotFound(c, err)
	}
	if !canViewList(list, userID(c)) {
		return response.Forbidden(c, "this list is private")
	}

	var likes []database.ListLike
	if err := db.Where("list_id = ?", list.ID).Order("created_at DESC").Find(&likes).Error; err != nil {
		return response.InternalError(c)
	}
	userIDs := make([]string, 0, len(likes))
	for i := range likes {
		userIDs = append(userIDs, likes[i].UserID)
	}
	users, err := contractshape.UserByIDBatch(db, userIDs)
	if err != nil {
		return response.InternalError(c)
	}
	out := make([]map[string]any, 0, len(likes))
	for i := range likes {
		out = append(out, map[string]any{
			"liked_at": contractshape.Timestamp(likes[i].CreatedAt),
			"user":     contractshape.User(users[likes[i].UserID]),
		})
	}
	return c.JSON(out)
}

func handleContractLikeList(c *fiber.Ctx) error {
	return contractSetLike(c, true)
}

func handleContractUnlikeList(c *fiber.Ctx) error {
	return contractSetLike(c, false)
}

func contractSetLike(c *fiber.Ctx, like bool) error {
	uid := userID(c)
	if uid == "" {
		return response.Unauthorized(c)
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := contractLoadList(db, c.Params("list_id"))
	if err != nil {
		return listNotFound(c, err)
	}
	if !canViewList(list, uid) {
		return response.Forbidden(c, "this list is private")
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if like {
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.ListLike{ListID: list.ID, UserID: uid})
			if res.Error != nil {
				return nil, res.Error
			}
			if res.RowsAffected > 0 {
				if err := tx.Model(&database.List{}).Where("id = ?", list.ID).
					UpdateColumn("like_count", gorm.Expr("like_count + 1")).Error; err != nil {
					return nil, err
				}
				bumpListVersion(cacheTx, list.ID)
			}
		} else {
			res := tx.Where("list_id = ? AND user_id = ?", list.ID, uid).Delete(&database.ListLike{})
			if res.Error != nil {
				return nil, res.Error
			}
			if res.RowsAffected > 0 {
				if err := tx.Model(&database.List{}).Where("id = ?", list.ID).
					UpdateColumn("like_count", gorm.Expr("GREATEST(like_count - 1, 0)")).Error; err != nil {
					return nil, err
				}
				bumpListVersion(cacheTx, list.ID)
			}
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// contractCommentSort orders list comments. Only newest/oldest change the
// order; likes/replies/reviews are accepted but fall back to newest (list
// comments carry no like/reply/review counts), which is why those rows are
// graded divergent.
func contractCommentSort(sort string) string {
	if strings.EqualFold(sort, "oldest") {
		return "created_at ASC, id ASC"
	}
	return "created_at DESC, id DESC"
}

func handleContractListComments(c *fiber.Ctx) error {
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	var list *database.List
	if ownerParam := c.Params("list_id"); ownerParam != "" {
		owner := auth.ResolveTargetID(c, "id")
		if owner == "" {
			return response.Unauthorized(c)
		}
		list, err = contractLoadList(db, ownerParam)
		if err != nil {
			return listNotFound(c, err)
		}
		if list.UserID != owner {
			return response.NotFound(c, "list not found")
		}
	} else {
		list, err = contractLoadList(db, c.Params("id"))
		if err != nil {
			return listNotFound(c, err)
		}
	}
	if !canViewList(list, userID(c)) {
		return response.Forbidden(c, "this list is private")
	}

	page, limit := response.ParsePagination(c, 25)
	var total int64
	if err := db.Model(&database.ListComment{}).Where("list_id = ?", list.ID).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var comments []database.ListComment
	if err := db.Where("list_id = ?", list.ID).
		Order(contractCommentSort(c.Params("sort"))).
		Offset((page - 1) * limit).Limit(limit).Find(&comments).Error; err != nil {
		return response.InternalError(c)
	}

	userIDs := make([]string, 0, len(comments))
	for i := range comments {
		userIDs = append(userIDs, comments[i].UserID)
	}
	users, err := contractshape.UserByIDBatch(db, userIDs)
	if err != nil {
		return response.InternalError(c)
	}
	out := make([]map[string]any, 0, len(comments))
	for i := range comments {
		cm := &comments[i]
		out = append(out, map[string]any{
			"id":          cm.ID,
			"parent_id":   0,
			"created_at":  contractshape.Timestamp(cm.CreatedAt),
			"updated_at":  contractshape.Timestamp(cm.UpdatedAt),
			"comment":     cm.Body,
			"gif":         nil,
			"spoiler":     false,
			"review":      false,
			"replies":     0,
			"likes":       0,
			"language":    nil,
			"user_rating": nil,
			"user_stats":  map[string]any{"rating": nil, "play_count": 0, "completed_count": 0},
			"user":        contractshape.User(users[cm.UserID]),
		})
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

var contractReportReasons = map[string]bool{
	"duplicate": true, "remove": true, "metadata": true, "adult": true,
	"language": true, "spam": true, "other": true,
}

type contractReportBody struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func handleContractReportList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return response.Unauthorized(c)
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	var list *database.List
	if ownerParam := c.Params("list_id"); ownerParam != "" {
		list, err = contractLoadList(db, ownerParam)
	} else {
		list, err = contractLoadList(db, c.Params("id"))
	}
	if err != nil {
		return listNotFound(c, err)
	}

	var req contractReportBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	if !contractReportReasons[req.Reason] {
		return response.BadRequest(c, "reason must be one of duplicate, remove, metadata, adult, language, spam, other")
	}

	// One report per reporter per list.
	var existing int64
	if err := db.Model(&database.ContentReport{}).
		Where("reporter_id = ? AND media_type = ? AND media_id = ?", uid, "list", list.ID).
		Count(&existing).Error; err != nil {
		return response.InternalError(c)
	}
	if existing > 0 {
		return response.Conflict(c, "you have already reported this list")
	}

	report := database.ContentReport{
		ReporterID:  uid,
		MediaType:   "list",
		MediaID:     list.ID,
		Reason:      req.Reason,
		Description: req.Message,
		Status:      "open",
	}
	if err := db.Create(&report).Error; err != nil {
		return response.InternalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{})
}
