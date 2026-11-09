package sync

// Write surface: POST reorder, PUT /sync/watchlist, and the per-item PUTs.
// Reorders write real 1-based ranks from the item ids a read returns; ids the
// caller does not own are reported in skipped_ids rather than ranked.

import (
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ============================================================================
// POST /sync/{favorites,watchlist}/reorder
// ============================================================================

type reorderRequest struct {
	Rank []int64 `json:"rank"`
}

type reorderListInfo struct {
	UpdatedAt string `json:"updated_at"`
	ItemCount int    `json:"item_count"`
}

type reorderResponse struct {
	Updated    int             `json:"updated"`
	SkippedIDs []int64         `json:"skipped_ids"`
	List       reorderListInfo `json:"list"`
}

func handleReorderFavorites(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var req reorderRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	var rows []database.UserFavorite
	if err := db.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	resp := reorderResponse{SkippedIDs: []int64{}, List: reorderListInfo{ItemCount: len(rows)}}
	owned := make(map[int64]bool, len(rows))
	for _, r := range rows {
		owned[r.MediaID] = true
	}

	txErr := db.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.Rank {
			if !owned[id] {
				resp.SkippedIDs = append(resp.SkippedIDs, id)
				continue
			}
			if err := tx.Model(&database.UserFavorite{}).
				Where("user_id = ? AND media_id = ?", userID, id).
				Update("rank", i+1).Error; err != nil {
				return err
			}
			resp.Updated++
		}
		return nil
	})
	if txErr != nil {
		return response.InternalError(c)
	}

	resp.List.UpdatedAt = contractshape.Timestamp(time.Now())
	return c.JSON(resp)
}

func handleReorderWatchlistContract(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var req reorderRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	var rows []database.UserWatchlist
	if err := db.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	resp := reorderResponse{SkippedIDs: []int64{}, List: reorderListInfo{ItemCount: len(rows)}}
	owned := make(map[int64]bool, len(rows))
	for _, r := range rows {
		owned[r.MediaID] = true
	}

	txErr := db.Transaction(func(tx *gorm.DB) error {
		for i, id := range req.Rank {
			if !owned[id] {
				resp.SkippedIDs = append(resp.SkippedIDs, id)
				continue
			}
			if err := tx.Model(&database.UserWatchlist{}).
				Where("user_id = ? AND media_id = ?", userID, id).
				Update("rank", i+1).Error; err != nil {
				return err
			}
			resp.Updated++
		}
		return nil
	})
	if txErr != nil {
		return response.InternalError(c)
	}

	resp.List.UpdatedAt = contractshape.Timestamp(time.Now())
	return c.JSON(resp)
}

// ============================================================================
// PUT /sync/favorites/{list_item_id}
// ============================================================================

func handleUpdateFavoriteItem(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	id, err := strconv.ParseInt(c.Params("list_item_id"), 10, 64)
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid list_item_id")
	}

	var req struct {
		Notes *string `json:"notes"`
		Rank  *int    `json:"rank"`
	}
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if req.Notes != nil {
		return response.BadRequest(c, "favorites do not store notes")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	updates := map[string]any{}
	if req.Rank != nil {
		updates["rank"] = *req.Rank
	}
	if len(updates) == 0 {
		return response.BadRequest(c, "no updatable fields supplied")
	}

	res := db.Model(&database.UserFavorite{}).
		Where("user_id = ? AND media_id = ?", userID, id).
		Updates(updates)
	if res.Error != nil {
		return response.InternalError(c)
	}
	if res.RowsAffected == 0 {
		return response.NotFound(c, "favorite item not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ============================================================================
// PUT /sync/watchlist/{list_item_id}
// ============================================================================

func handleUpdateWatchlistItem(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	id, err := strconv.ParseInt(c.Params("list_item_id"), 10, 64)
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid list_item_id")
	}

	var req struct {
		Notes *string `json:"notes"`
		Rank  *int    `json:"rank"`
	}
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	updates := map[string]any{}
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}
	if req.Rank != nil {
		updates["rank"] = *req.Rank
	}
	if len(updates) == 0 {
		return response.BadRequest(c, "no updatable fields supplied")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	res := db.Model(&database.UserWatchlist{}).
		Where("user_id = ? AND media_id = ?", userID, id).
		Updates(updates)
	if res.Error != nil {
		return response.InternalError(c)
	}
	if res.RowsAffected == 0 {
		return response.NotFound(c, "watchlist item not found")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ============================================================================
// PUT /sync/watchlist
// ============================================================================

type watchlistUpdateResponse struct {
	Added    typeCounts `json:"added"`
	Existing typeCounts `json:"existing"`
	NotFound typeLists  `json:"not_found"`
}

type typeCounts struct {
	Movies   int `json:"movies"`
	Shows    int `json:"shows"`
	Seasons  int `json:"seasons"`
	Episodes int `json:"episodes"`
}

type typeLists struct {
	Movies   []any `json:"movies"`
	Shows    []any `json:"shows"`
	Seasons  []any `json:"seasons"`
	Episodes []any `json:"episodes"`
}

// handleUpdateWatchlist adds watchlist items that are not already present and
// reports which already existed. user_watchlist stores movie and show rows
// only; seasons/episodes are not storable (#71), so a request that includes
// them is rejected rather than silently dropped.
func handleUpdateWatchlist(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []struct {
			IDs   idsObj  `json:"ids"`
			Notes *string `json:"notes"`
		} `json:"movies,omitempty"`
		Shows []struct {
			IDs   idsObj  `json:"ids"`
			Notes *string `json:"notes"`
		} `json:"shows,omitempty"`
		Seasons  []jsonRaw `json:"seasons,omitempty"`
		Episodes []jsonRaw `json:"episodes,omitempty"`
	}
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}
	if len(req.Seasons) > 0 || len(req.Episodes) > 0 {
		return response.BadRequest(c, "watchlist seasons/episodes are not storable; only movies and shows are served")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	resp := watchlistUpdateResponse{
		NotFound: typeLists{Movies: []any{}, Shows: []any{}, Seasons: []any{}, Episodes: []any{}},
	}
	now := time.Now()

	var idRefs []media.IDRef
	for _, m := range req.Movies {
		idRefs = append(idRefs, idRefFor(m.IDs).WithType("movie"))
	}
	for _, s := range req.Shows {
		idRefs = append(idRefs, idRefFor(s.IDs).WithType("show"))
	}

	refs, resolveErr := media.ResolveIDsBatch(db, idRefs)
	if resolveErr != nil {
		return response.InternalError(c)
	}

	var maxRank int
	if err := db.Model(&database.UserWatchlist{}).Where("user_id = ?", userID).
		Select("COALESCE(MAX(rank), 0)").Scan(&maxRank).Error; err != nil {
		return response.InternalError(c)
	}

	txErr := db.Transaction(func(tx *gorm.DB) error {
		add := func(ids idsObj, notes *string, mediaType string) error {
			ref, ok := refs[idRefFor(ids).WithType(mediaType)]
			if !ok || ref.MediaType != mediaType {
				if mediaType == "movie" {
					resp.NotFound.Movies = append(resp.NotFound.Movies, map[string]any{"ids": ids})
				} else {
					resp.NotFound.Shows = append(resp.NotFound.Shows, map[string]any{"ids": ids})
				}
				return nil
			}
			maxRank++
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserWatchlist{
				UserID: userID, MediaType: mediaType, MediaID: ref.MediaID, Rank: maxRank,
				Notes: notes, ListedAt: now,
			})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if mediaType == "movie" {
					resp.Added.Movies++
				} else {
					resp.Added.Shows++
				}
			} else {
				maxRank--
				if mediaType == "movie" {
					resp.Existing.Movies++
				} else {
					resp.Existing.Shows++
				}
			}
			return nil
		}
		for _, m := range req.Movies {
			if err := add(m.IDs, m.Notes, "movie"); err != nil {
				return err
			}
		}
		for _, s := range req.Shows {
			if err := add(s.IDs, s.Notes, "show"); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(resp)
}

// jsonRaw keeps an unhandled array element shape without pinning it to a struct.
type jsonRaw = map[string]any

// queryBool reports whether a query param parses as true.
func queryBool(c *fiber.Ctx, key string) bool {
	v := c.Query(key)
	return v == "true" || v == "1"
}
