package lists

import (
	"errors"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// Smart lists
// ============================================================================

type contractSmartListBody struct {
	Name      string                 `json:"name"`
	Source    string                 `json:"source"`
	MediaType string                 `json:"media_type"`
	Filters   map[string]interface{} `json:"filters"`
}

// contractSmartListFilters folds source/media_type into the stored filters map
// so the read can round-trip them (our smart_lists table stores one filters
// blob and has no source/media_type columns).
func contractSmartListFilters(body contractSmartListBody) database.JSONMap {
	filters := database.JSONMap{}
	for k, v := range body.Filters {
		filters[k] = v
	}
	if body.Source != "" {
		filters["source"] = body.Source
	}
	if body.MediaType != "" {
		filters["media_type"] = body.MediaType
	}
	return filters
}

func contractSmartListObject(sl *database.SmartList) map[string]any {
	source := "library"
	mediaType := "media"
	filters := map[string]any{}
	for k, v := range sl.Filters {
		switch k {
		case "source":
			if s, ok := v.(string); ok && s != "" {
				source = s
			}
		case "media_type":
			if s, ok := v.(string); ok && s != "" {
				mediaType = s
			}
		default:
			filters[k] = v
		}
	}
	return map[string]any{
		"name":       sl.Name,
		"privacy":    "private",
		"created_at": contractshape.Timestamp(sl.CreatedAt),
		"updated_at": contractshape.Timestamp(sl.UpdatedAt),
		"ids":        map[string]any{"trakt": sl.ID, "slug": "smart-list-" + strconv.FormatInt(sl.ID, 10)},
		"images":     map[string]any{"posters": []string{}},
		"source":     source,
		"media_type": mediaType,
		"filters":    filters,
	}
}

func handleContractGetSmartLists(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	var lists []database.SmartList
	if err := db.Where("user_id = ?", uid).Order("created_at DESC").Find(&lists).Error; err != nil {
		return response.InternalError(c)
	}
	out := make([]map[string]any, 0, len(lists))
	for i := range lists {
		out = append(out, contractSmartListObject(&lists[i]))
	}
	return c.JSON(out)
}

func handleContractCreateSmartList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	if _, err := contractDB(c); err != nil {
		return err
	}
	var req contractSmartListBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	if strings.TrimSpace(req.Name) == "" {
		return response.BadRequest(c, "name is required")
	}
	sl := database.SmartList{UserID: uid, Name: strings.TrimSpace(req.Name), Filters: contractSmartListFilters(req)}
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Create(&sl).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"ids": map[string]any{"trakt": sl.ID, "slug": "smart-list-" + strconv.FormatInt(sl.ID, 10)},
	})
}

func contractLoadSmartList(c *fiber.Ctx, db *gorm.DB, uid string) (*database.SmartList, error) {
	id, perr := strconv.ParseInt(c.Params("list_id"), 10, 64)
	if perr != nil {
		_ = response.BadRequest(c, "invalid smart list id")
		return nil, response.ErrHandled
	}
	var sl database.SmartList
	if err := db.First(&sl, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.NotFound(c, "smart list not found")
		} else {
			_ = response.InternalError(c)
		}
		return nil, response.ErrHandled
	}
	if sl.UserID != uid {
		_ = response.Forbidden(c, "you do not have access to this smart list")
		return nil, response.ErrHandled
	}
	return &sl, nil
}

func handleContractGetSmartList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	sl, err := contractLoadSmartList(c, db, uid)
	if err != nil {
		return err
	}
	return c.JSON(contractSmartListObject(sl))
}

func handleContractUpdateSmartList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	sl, err := contractLoadSmartList(c, db, uid)
	if err != nil {
		return err
	}
	var req contractSmartListBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	updates := map[string]any{}
	if strings.TrimSpace(req.Name) != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.Filters != nil || req.Source != "" || req.MediaType != "" {
		// Merge into the stored blob instead of replacing it: a request that
		// changes only source must not wipe media_type (or every other filter),
		// and omitting source/media_type must preserve the stored values.
		next := database.JSONMap{}
		for k, v := range sl.Filters {
			next[k] = v
		}
		for k, v := range contractSmartListFilters(req) {
			next[k] = v
		}
		updates["filters"] = next
	}
	if len(updates) == 0 {
		return response.BadRequest(c, "no fields to update")
	}
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Model(&database.SmartList{}).Where("id = ?", sl.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	_ = db.First(sl, sl.ID)
	return c.JSON(contractSmartListObject(sl))
}

func handleContractDeleteSmartList(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	sl, err := contractLoadSmartList(c, db, uid)
	if err != nil {
		return err
	}
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Delete(sl).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// smartListMediaType maps the contract's {type} onto the single media type
// resolveSmartListQuery understands. "movies"/"shows"/"media" and the singular
// forms are all accepted; "media" resolves to both.
func smartListMediaType(t string) (string, bool) {
	switch t {
	case "movie", "movies":
		return "movie", true
	case "show", "shows":
		return "show", true
	case "media", "":
		return "", true
	}
	return "", false
}

// intersectSmartListMediaType intersects the type a client asked for with the
// type the smart list was saved with. "" means "both"; a conflict between two
// specific types is reported as incompatible rather than silently widened.
func intersectSmartListMediaType(requested, saved string) (string, bool) {
	switch {
	case requested == "":
		return saved, true
	case saved == "" || saved == requested:
		return requested, true
	default:
		return "", false
	}
}

func handleContractSmartListItems(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return response.Unauthorized(c)
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	id, perr := strconv.ParseInt(c.Params("list_id"), 10, 64)
	if perr != nil {
		return response.BadRequest(c, "invalid smart list id")
	}
	var sl database.SmartList
	if err := db.First(&sl, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "smart list not found")
		}
		return response.InternalError(c)
	}
	if sl.UserID != uid {
		return response.Forbidden(c, "you do not have access to this smart list")
	}

	requested, ok := smartListMediaType(c.Params("type"))
	if !ok {
		return response.BadRequest(c, "type must be one of movie, show, movies, shows, media")
	}
	// The saved media_type is part of the smart list's definition: a request
	// for the broad "media" form must not widen a movies-only list into shows.
	saved := ""
	if raw, ok := sl.Filters["media_type"]; ok {
		if s, ok := raw.(string); ok {
			if mt, ok := smartListMediaType(s); ok {
				saved = mt
			}
		}
	}
	mediaType, compatible := intersectSmartListMediaType(requested, saved)
	page, limit := response.ParsePagination(c, 25)
	if !compatible {
		// The two constraints have no common media type: an empty page is the
		// honest answer, not a 400 (the request itself is valid).
		response.SetPaginationHeaders(c, page, limit, 0, 0)
		return c.JSON([]map[string]any{})
	}
	results, total, qerr := resolveSmartListQuery(db, sl.Filters, uid, mediaType, c.Params("sort_by"), page, limit)
	if qerr != nil {
		return response.InternalError(c)
	}
	response.SetPaginationHeaders(c, page, limit, total, len(results))
	return c.JSON(results)
}

// handleContractSmartListDraft serves a draft derived from a list's stored
// items. A list's items are explicit rows, not a filter expression, so the
// draft's media_type is derived from the item types and its filters are empty;
// no filter set can be derived without inventing one. That is why the row is
// graded divergent.
func handleContractSmartListDraft(c *fiber.Ctx) error {
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

	var types []string
	if err := db.Model(&database.ListItem{}).Where("list_id = ?", list.ID).
		Distinct().Pluck("media_type", &types).Error; err != nil {
		return response.InternalError(c)
	}
	hasMovie, hasShow := false, false
	for _, t := range types {
		if t == "movie" {
			hasMovie = true
		}
		if t == "show" {
			hasShow = true
		}
	}
	mediaType := "media"
	if hasMovie && !hasShow {
		mediaType = "movies"
	}
	if hasShow && !hasMovie {
		mediaType = "shows"
	}

	return c.JSON(fiber.Map{
		"list":       map[string]any{"ids": contractshape.ListIDs(list)},
		"origin_url": nil,
		"source":     "library",
		"media_type": mediaType,
		"filters":    map[string]any{},
	})
}

func handleContractConvertDraft(c *fiber.Ctx) error {
	uid, err := contractTargetUser(c)
	if err != nil {
		return err
	}
	db, err := contractDB(c)
	if err != nil {
		return err
	}
	// The source list must exist and belong to the caller.
	if _, err := loadListForWrite(c, db, uid); err != nil {
		return err
	}
	var req contractSmartListBody
	if perr := response.ParseBody(c, &req); perr != nil {
		return perr
	}
	if strings.TrimSpace(req.Name) == "" {
		return response.BadRequest(c, "name is required")
	}
	sl := database.SmartList{UserID: uid, Name: strings.TrimSpace(req.Name), Filters: contractSmartListFilters(req)}
	if err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		if err := tx.Create(&sl).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	}); err != nil {
		return response.InternalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"ids": map[string]any{"trakt": sl.ID, "slug": "smart-list-" + strconv.FormatInt(sl.ID, 10)},
	})
}
