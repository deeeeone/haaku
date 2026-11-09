package lists

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ============================================================================
// List items
// ============================================================================

type contractIDs struct {
	Trakt response.FlexibleID `json:"trakt"`
	Tmdb  response.FlexibleID `json:"tmdb"`
	Imdb  string              `json:"imdb"`
	Slug  string              `json:"slug"`
}

type contractMediaRef struct {
	IDs   contractIDs `json:"ids"`
	Rank  *int        `json:"rank"`
	Notes string      `json:"notes"`
}

type contractItemsBody struct {
	Movies   []contractMediaRef `json:"movies"`
	Shows    []contractMediaRef `json:"shows"`
	Seasons  []contractMediaRef `json:"seasons"`
	Episodes []contractMediaRef `json:"episodes"`
}

type contractUpdateItemBody struct {
	Notes *string `json:"notes"`
	Rank  *int    `json:"rank"`
}

// contractMediaTable maps a list item media type to its table name.
var contractMediaTable = map[string]string{
	"movie":   "movies",
	"show":    "shows",
	"season":  "seasons",
	"episode": "episodes",
}

// resolveContractMedia turns a media reference into our local media id, trying
// the trakt id (our own row id), then tmdb, then imdb, all against stored rows.
func resolveContractMedia(db *gorm.DB, mediaType string, ids contractIDs) (int64, bool) {
	table, ok := contractMediaTable[mediaType]
	if !ok {
		return 0, false
	}
	if ids.Trakt.Int() != 0 {
		var n int64
		if err := db.Table(table).Where("id = ?", ids.Trakt.Int()).Count(&n).Error; err == nil && n > 0 {
			return int64(ids.Trakt.Int()), true
		}
	}
	sources := []struct {
		source string
		value  string
	}{
		{"tmdb", strconv.Itoa(ids.Tmdb.Int())},
		{"imdb", ids.Imdb},
	}
	for _, s := range sources {
		if s.value == "" || s.value == "0" {
			continue
		}
		var ext database.ExternalID
		if err := db.Where("source = ? AND external_id = ? AND media_type = ?", s.source, s.value, mediaType).First(&ext).Error; err == nil {
			return ext.MediaID, true
		}
	}
	return 0, false
}

// loadListForWrite loads the list named by {list_id}, verifies it belongs to
// the caller, and answers 404/403 appropriately.
func loadListForWrite(c *fiber.Ctx, db *gorm.DB, uid string) (*database.List, error) {
	list, err := contractLoadList(db, c.Params("list_id"))
	if err != nil {
		return nil, listNotFound(c, err)
	}
	if list.UserID != uid {
		_ = response.Forbidden(c, "you do not have permission to modify this list")
		return nil, response.ErrHandled
	}
	return list, nil
}

func handleContractAddItems(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := loadListForWrite(c, db, uid)
	if err != nil {
		return err
	}

	var req contractItemsBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}

	added := map[string]int{"movies": 0, "shows": 0, "seasons": 0, "episodes": 0}
	existing := map[string]int{"movies": 0, "shows": 0, "seasons": 0, "episodes": 0}
	notFound := map[string][]map[string]any{"movies": {}, "shows": {}, "seasons": {}, "episodes": {}}

	plural := map[string]string{"movie": "movies", "show": "shows", "season": "seasons", "episode": "episodes"}

	process := func(tx *gorm.DB, items []contractMediaRef, mediaType string) {
		for _, ref := range items {
			mediaID, ok := resolveContractMedia(tx, mediaType, ref.IDs)
			if !ok {
				notFound[plural[mediaType]] = append(notFound[plural[mediaType]], map[string]any{
					"ids": ref.IDs,
				})
				continue
			}
			rank := 0
			if ref.Rank != nil {
				rank = *ref.Rank
			}
			item := database.ListItem{
				ListID:    list.ID,
				MediaType: mediaType,
				MediaID:   mediaID,
				Rank:      rank,
				Notes:     ref.Notes,
				AddedBy:   uid,
			}
			res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
			if res.Error != nil {
				notFound[plural[mediaType]] = append(notFound[plural[mediaType]], map[string]any{"ids": ref.IDs})
				continue
			}
			if res.RowsAffected > 0 {
				added[plural[mediaType]]++
			} else {
				existing[plural[mediaType]]++
			}
		}
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := advisoryListLock(tx, list.ID); err != nil {
			return nil, err
		}
		process(tx, req.Movies, "movie")
		process(tx, req.Shows, "show")
		process(tx, req.Seasons, "season")
		process(tx, req.Episodes, "episode")
		total := 0
		for _, n := range added {
			total += n
		}
		if total > 0 {
			bumpListVersion(cacheTx, list.ID)
			if err := contractPublish(tx, outbox.EventListItemsAdded, uid, map[string]any{
				"list_id": list.ID, "items": added,
			}); err != nil {
				return nil, err
			}
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	_ = db.First(list, list.ID)
	var itemCount int64
	db.Model(&database.ListItem{}).Where("list_id = ?", list.ID).Count(&itemCount)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"added":     added,
		"existing":  existing,
		"not_found": notFound,
		"list": fiber.Map{
			"updated_at": contractshape.Timestamp(list.UpdatedAt),
			"item_count": itemCount,
		},
	})
}

func handleContractRemoveItems(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := loadListForWrite(c, db, uid)
	if err != nil {
		return err
	}

	var req contractItemsBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}

	deleted := map[string]int{"movies": 0, "shows": 0, "seasons": 0, "episodes": 0}
	notFound := map[string][]map[string]any{"movies": {}, "shows": {}, "seasons": {}, "episodes": {}}
	plural := map[string]string{"movie": "movies", "show": "shows", "season": "seasons", "episode": "episodes"}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := advisoryListLock(tx, list.ID); err != nil {
			return nil, err
		}
		remove := func(items []contractMediaRef, mediaType string) {
			for _, ref := range items {
				mediaID, ok := resolveContractMedia(tx, mediaType, ref.IDs)
				if !ok {
					notFound[plural[mediaType]] = append(notFound[plural[mediaType]], map[string]any{"ids": ref.IDs})
					continue
				}
				res := tx.Where("list_id = ? AND media_type = ? AND media_id = ?", list.ID, mediaType, mediaID).Delete(&database.ListItem{})
				if res.RowsAffected == 0 {
					notFound[plural[mediaType]] = append(notFound[plural[mediaType]], map[string]any{"ids": ref.IDs})
					continue
				}
				deleted[plural[mediaType]] += int(res.RowsAffected)
			}
		}
		remove(req.Movies, "movie")
		remove(req.Shows, "show")
		remove(req.Seasons, "season")
		remove(req.Episodes, "episode")
		total := 0
		for _, n := range deleted {
			total += n
		}
		if total > 0 {
			bumpListVersion(cacheTx, list.ID)
			if err := contractPublish(tx, outbox.EventListItemsRemoved, uid, map[string]any{
				"list_id": list.ID, "deleted": deleted,
			}); err != nil {
				return nil, err
			}
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted, "not_found": notFound})
}

// handleContractReorderItems applies a {rank: [list_item_id...]} order to the
// list's items, counting the ones that moved and reporting ids that are not in
// this list as skipped.
func handleContractReorderItems(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := loadListForWrite(c, db, uid)
	if err != nil {
		return err
	}

	var req contractRankBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	if len(req.Rank) == 0 {
		return response.BadRequest(c, "rank array must not be empty")
	}

	var items []database.ListItem
	if err := db.Where("list_id = ?", list.ID).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}
	itemSet := map[int64]bool{}
	for i := range items {
		itemSet[items[i].ID] = true
	}

	updated := 0
	skipped := make([]int64, 0)
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := advisoryListLock(tx, list.ID); err != nil {
			return nil, err
		}
		for pos, itemID := range req.Rank {
			if !itemSet[itemID] {
				skipped = append(skipped, itemID)
				continue
			}
			if err := tx.Model(&database.ListItem{}).Where("id = ? AND list_id = ?", itemID, list.ID).
				Update("rank", pos).Error; err != nil {
				return nil, err
			}
			updated++
		}
		if updated > 0 {
			if err := tx.Model(&database.List{}).Where("id = ?", list.ID).Update("is_ranked", true).Error; err != nil {
				return nil, err
			}
			bumpListVersion(cacheTx, list.ID)
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	_ = db.First(list, list.ID)
	var itemCount int64
	db.Model(&database.ListItem{}).Where("list_id = ?", list.ID).Count(&itemCount)

	return c.JSON(fiber.Map{
		"updated":     updated,
		"skipped_ids": skipped,
		"list": fiber.Map{
			"updated_at": contractshape.Timestamp(list.UpdatedAt),
			"item_count": itemCount,
		},
	})
}

func handleContractUpdateItem(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, err := loadListForWrite(c, db, uid)
	if err != nil {
		return err
	}

	itemID, perr := strconv.ParseInt(c.Params("list_item_id"), 10, 64)
	if perr != nil {
		return response.BadRequest(c, "invalid list item id")
	}

	var item database.ListItem
	if err := db.Where("id = ? AND list_id = ?", itemID, list.ID).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "list item not found")
		}
		return response.InternalError(c)
	}

	var req contractUpdateItemBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	updates := map[string]any{}
	if req.Notes != nil {
		updates["notes"] = *req.Notes
	}
	if req.Rank != nil {
		updates["rank"] = *req.Rank
	}
	if len(updates) == 0 {
		return response.BadRequest(c, "no fields to update")
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Model(&database.ListItem{}).Where("id = ? AND list_id = ?", itemID, list.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		bumpListVersion(cacheTx, list.ID)
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
