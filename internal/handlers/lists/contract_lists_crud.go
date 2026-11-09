package lists

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/outbox"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// List CRUD
// ============================================================================

type contractListBody struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	Privacy        *string `json:"privacy"`
	DisplayNumbers *bool   `json:"display_numbers"`
	AllowComments  *bool   `json:"allow_comments"`
	SortBy         *string `json:"sort_by"`
	SortHow        *string `json:"sort_how"`
}

func handleContractCreateList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}

	var req contractListBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return response.BadRequest(c, "name is required")
	}

	privacy := "public"
	if req.Privacy != nil && *req.Privacy != "" {
		privacy = *req.Privacy
	}
	if privacy != "public" && privacy != "link_only" && privacy != "private" {
		return response.BadRequest(c, "privacy must be public, link_only, or private")
	}
	sortBy, sortHow := "rank", "asc"
	if req.SortBy != nil && *req.SortBy != "" {
		sortBy = *req.SortBy
	}
	if req.SortHow != nil && *req.SortHow != "" {
		sortHow = *req.SortHow
	}
	allowComments := true
	if req.AllowComments != nil {
		allowComments = *req.AllowComments
	}
	isRanked := false
	if req.DisplayNumbers != nil {
		isRanked = *req.DisplayNumbers
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}

	slug := generateSlug(*req.Name, uid)
	list := database.List{
		UserID:        uid,
		Name:          strings.TrimSpace(*req.Name),
		Description:   description,
		Slug:          slug,
		Privacy:       privacy,
		SortBy:        sortBy,
		SortHow:       sortHow,
		IsRanked:      isRanked,
		AllowComments: allowComments,
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := createListWithUniqueSlug(tx, &list, slug); err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		if err := contractPublish(tx, outbox.EventListCreated, uid, map[string]any{
			"list_id": list.ID, "slug": list.Slug, "name": list.Name, "privacy": list.Privacy,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	obj, err := contractListObject(db, &list)
	if err != nil {
		return response.InternalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(obj)
}

// contractListSlugAttempts bounds the duplicate-slug retry. A collision means
// the same user already has a list with this name; a random suffix resolves it,
// and five tries is far past the point where a real conflict could survive.
const contractListSlugAttempts = 5

// isDuplicateSlugError reports whether err is a uniqueness violation. Both
// drivers are matched by message so this stays portable between sqlite (the
// handler suite) and PostgreSQL (production).
func isDuplicateSlugError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique")
}

// slugSuffix returns a short random suffix. A timestamp is not enough: two
// creates in the same second would pick the same one and collide again.
func slugSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// createListWithUniqueSlug inserts list, retrying with a random slug suffix on a
// duplicate. Every attempt runs under its own savepoint because PostgreSQL
// aborts the whole transaction on a failed statement: without rolling back to a
// savepoint taken before the insert, the retry always fails with "current
// transaction is aborted", which sqlite (no such state) never shows. list.ID is
// reset so the retry inserts a fresh row rather than reusing a stale id.
func createListWithUniqueSlug(tx *gorm.DB, list *database.List, base string) error {
	for attempt := 0; ; attempt++ {
		savepoint := "create_list_slug_" + strconv.Itoa(attempt)
		if err := tx.SavePoint(savepoint).Error; err != nil {
			return err
		}
		list.ID = 0
		err := tx.Create(list).Error
		if err == nil {
			return nil
		}
		if !isDuplicateSlugError(err) || attempt >= contractListSlugAttempts {
			return err
		}
		if rerr := tx.RollbackTo(savepoint).Error; rerr != nil {
			return rerr
		}
		list.Slug = base + "-" + slugSuffix()
	}
}

func handleContractGetList(c *fiber.Ctx) error {
	target, err := contractListOwnerID(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, lerr := contractLoadList(db, c.Params("list_id"))
	if lerr != nil {
		return listNotFound(c, lerr)
	}
	if list.UserID != target {
		return response.NotFound(c, "list not found")
	}
	if !canViewList(list, userID(c)) {
		return response.Forbidden(c, "this list is private")
	}
	obj, err := contractListObject(db, list)
	if err != nil {
		return response.InternalError(c)
	}
	return c.JSON(obj)
}

func handleContractUpdateList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, lerr := contractLoadList(db, c.Params("list_id"))
	if lerr != nil {
		return listNotFound(c, lerr)
	}
	if list.UserID != uid {
		return response.Forbidden(c, "you do not have permission to edit this list")
	}

	var req contractListBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	updates := map[string]any{}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return response.BadRequest(c, "name cannot be empty")
		}
		updates["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Privacy != nil {
		if *req.Privacy != "public" && *req.Privacy != "link_only" && *req.Privacy != "private" {
			return response.BadRequest(c, "privacy must be public, link_only, or private")
		}
		updates["privacy"] = *req.Privacy
	}
	if req.SortBy != nil {
		updates["sort_by"] = *req.SortBy
	}
	if req.SortHow != nil {
		updates["sort_how"] = *req.SortHow
	}
	if req.DisplayNumbers != nil {
		updates["is_ranked"] = *req.DisplayNumbers
	}
	if req.AllowComments != nil {
		updates["allow_comments"] = *req.AllowComments
	}
	if len(updates) == 0 {
		return response.BadRequest(c, "no fields to update")
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := advisoryListLock(tx, list.ID); err != nil {
			return nil, err
		}
		if err := tx.Model(list).Updates(updates).Error; err != nil {
			return nil, err
		}
		bumpListVersion(cacheTx, list.ID)
		bumpUserVersion(cacheTx, uid)
		if err := contractPublish(tx, outbox.EventListUpdated, uid, map[string]any{
			"list_id": list.ID, "slug": list.Slug, "name": list.Name, "privacy": list.Privacy,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	_ = db.First(list, list.ID)
	obj, err := contractListObject(db, list)
	if err != nil {
		return response.InternalError(c)
	}
	return c.JSON(obj)
}

func handleContractDeleteList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	list, lerr := contractLoadList(db, c.Params("list_id"))
	if lerr != nil {
		return listNotFound(c, lerr)
	}
	if list.UserID != uid {
		return response.Forbidden(c, "you do not have permission to delete this list")
	}

	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := advisoryListLock(tx, list.ID); err != nil {
			return nil, err
		}
		if err := tx.Delete(list).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		if err := contractPublish(tx, outbox.EventListDeleted, uid, map[string]any{
			"list_id": list.ID, "slug": list.Slug, "name": list.Name,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

type contractRankBody struct {
	Rank []int64 `json:"rank"`
}

// handleContractReorderUserLists sets the order of the caller's own lists. The
// order persists in lists.sort_order; ids that are not the caller's lists are
// reported in skipped_ids rather than silently ignored.
func handleContractReorderUserLists(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
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

	var owned []database.List
	if err := db.Where("user_id = ?", uid).Find(&owned).Error; err != nil {
		return response.InternalError(c)
	}
	ownedSet := map[int64]bool{}
	for i := range owned {
		ownedSet[owned[i].ID] = true
	}

	updated := 0
	skipped := make([]int64, 0)
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		for pos, id := range req.Rank {
			if !ownedSet[id] {
				skipped = append(skipped, id)
				continue
			}
			if err := tx.Model(&database.List{}).Where("id = ? AND user_id = ?", id, uid).
				Update("sort_order", pos).Error; err != nil {
				return nil, err
			}
			updated++
		}
		if updated > 0 {
			bumpUserVersion(cacheTx, uid)
		}
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"updated": updated, "skipped_ids": skipped})
}

// handleContractCollaborations returns the lists the caller collaborates on.
func handleContractCollaborations(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}

	var collabs []database.ListCollaborator
	if err := db.Where("user_id = ?", uid).Find(&collabs).Error; err != nil {
		return response.InternalError(c)
	}
	listIDs := make([]int64, 0, len(collabs))
	for i := range collabs {
		listIDs = append(listIDs, collabs[i].ListID)
	}
	out := make([]map[string]any, 0)
	if len(listIDs) == 0 {
		return c.JSON(out)
	}
	var lists []database.List
	if err := db.Where("id IN ?", listIDs).Order("sort_order ASC, created_at DESC").Find(&lists).Error; err != nil {
		return response.InternalError(c)
	}
	for i := range lists {
		obj, oerr := contractListObject(db, &lists[i])
		if oerr != nil {
			return response.InternalError(c)
		}
		out = append(out, obj)
	}
	return c.JSON(out)
}
