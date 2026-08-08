package lists

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"
	"trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Lists
		{Method: "GET", Path: "/v1/users/{id}/lists", Summary: "Get a user's lists", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Lists", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/users/me/lists", Summary: "Create a list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Created list", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/lists/{id}", Summary: "Get a list", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "List", 403: "Private", 404: "Not found"}},
		{Method: "PUT", Path: "/v1/lists/{id}", Summary: "Update a list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Updated list", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "DELETE", Path: "/v1/lists/{id}", Summary: "Delete a list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Deleted", 401: "Unauthorized", 403: "Forbidden"}},
		// List items
		{Method: "GET", Path: "/v1/lists/{id}/items", Summary: "Get list items", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Items", 403: "Private", 404: "Not found"}},
		{Method: "POST", Path: "/v1/lists/{id}/items", Summary: "Add items to list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Items added", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "POST", Path: "/v1/lists/{id}/items/remove", Summary: "Remove items from list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Items removed", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "PUT", Path: "/v1/lists/{id}/items/reorder", Summary: "Reorder list items", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Reordered", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		// Likes
		{Method: "GET", Path: "/v1/lists/{id}/likes", Summary: "Get who liked a list", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Likes", 404: "Not found"}},
		{Method: "POST", Path: "/v1/lists/{id}/like", Summary: "Like a list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Liked", 401: "Unauthorized", 404: "Not found"}},
		{Method: "DELETE", Path: "/v1/lists/{id}/like", Summary: "Unlike a list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Unliked", 401: "Unauthorized", 404: "Not found"}},
		// Comments
		{Method: "GET", Path: "/v1/lists/{id}/comments", Summary: "Get list comments", Tags: []string{"Lists"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 404: "Not found"}},
		// Popular / Trending
		{Method: "GET", Path: "/v1/lists/popular", Summary: "Get popular lists", Tags: []string{"Lists"}, Auth: "none", StatusCodes: map[int]string{200: "Popular lists"}},
		{Method: "GET", Path: "/v1/lists/trending", Summary: "Get trending lists", Tags: []string{"Lists"}, Auth: "none", StatusCodes: map[int]string{200: "Trending lists"}},
		// Smart lists
		{Method: "GET", Path: "/v1/users/me/smart-lists", Summary: "Get user smart lists", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Smart lists", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/users/me/smart-lists", Summary: "Create smart list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{201: "Created", 400: "Invalid request", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/smart-lists/{id}", Summary: "Get a smart list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Smart list", 401: "Unauthorized", 404: "Not found"}},
		{Method: "PUT", Path: "/v1/smart-lists/{id}", Summary: "Update smart list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Updated", 400: "Invalid request", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "DELETE", Path: "/v1/smart-lists/{id}", Summary: "Delete smart list", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{204: "Deleted", 401: "Unauthorized", 403: "Forbidden"}},
		{Method: "GET", Path: "/v1/smart-lists/{id}/items", Summary: "Get resolved smart list items", Tags: []string{"Lists"}, Auth: "required", StatusCodes: map[int]string{200: "Items", 400: "Invalid request", 401: "Unauthorized", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all list route handlers.
func RegisterRoutes(api fiber.Router) {
	// User lists
	api.Get("/users/:id/lists", requireAuth, handleGetUserLists)
	api.Post("/users/me/lists", requireAuth, handleCreateList)

	// Individual list CRUD
	api.Get("/lists/:id", handleGetList)
	api.Put("/lists/:id", requireAuth, handleUpdateList)
	api.Delete("/lists/:id", requireAuth, handleDeleteList)

	// List items
	api.Get("/lists/:id/items", handleGetListItems)
	api.Post("/lists/:id/items", requireAuth, handleAddListItems)
	api.Post("/lists/:id/items/remove", requireAuth, handleRemoveListItems)
	api.Put("/lists/:id/items/reorder", requireAuth, handleReorderListItems)

	// Likes
	api.Get("/lists/:id/likes", handleGetListLikes)
	api.Post("/lists/:id/like", requireAuth, handleLikeList)
	api.Delete("/lists/:id/like", requireAuth, handleUnlikeList)

	// Comments
	api.Get("/lists/:id/comments", handleGetListComments)

	// Popular / Trending
	api.Get("/lists/popular", handleGetPopularLists)
	api.Get("/lists/trending", handleGetTrendingLists)

	// Smart lists
	api.Get("/users/me/smart-lists", requireAuth, handleGetSmartLists)
	api.Post("/users/me/smart-lists", requireAuth, handleCreateSmartList)
	api.Get("/smart-lists/:id", requireAuth, handleGetSmartList)
	api.Put("/smart-lists/:id", requireAuth, handleUpdateSmartList)
	api.Delete("/smart-lists/:id", requireAuth, handleDeleteSmartList)
	api.Get("/smart-lists/:id/items", requireAuth, handleGetSmartListItems)
}

// ============================================================================
// MIDDLEWARE / HELPERS
// ============================================================================

func requireAuth(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(string)
	if !ok || userID == "" {
		return authRequired(c)
	}
	return c.Next()
}

func userID(c *fiber.Ctx) string {
	uid, _ := c.Locals("user_id").(string)
	return uid
}

// bumpListVersion queues a version-counter bump for a list's cached data.
func bumpListVersion(cacheTx *cache.CacheTransaction, listID int64) {
	lvk := redis.BuildListVersionKey(listID)
	cacheTx.AddInvalidation(lvk, map[string]interface{}{
		"list_id":      listID,
		"version_key":  lvk,
		"version_only": true,
	})
}

// bumpUserVersion queues a version-counter bump for a user's cached lists.
func bumpUserVersion(cacheTx *cache.CacheTransaction, userID string) {
	uvk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(uvk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  uvk,
		"version_only": true,
	})
}

// advisoryListLock serializes concurrent writes to a list.
func advisoryListLock(tx *gorm.DB, listID int64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", fmt.Sprintf("list:%d", listID)).Error
}

// ============================================================================
// LIST CRUD
// ============================================================================

// handleGetUserLists returns lists for a user, respecting privacy.
func handleGetUserLists(c *fiber.Ctx) error {
	targetUserID := c.Params("id")
	currentUserID := userID(c)

	db, err := database.GetDB()
	if err != nil {
		return internalError(c)
	}

	query := db.Where("user_id = ?", targetUserID)

	// Privacy filtering: if viewer is not the owner, only show public + link_only lists.
	if currentUserID != targetUserID {
		query = query.Where("privacy IN ?", []string{"public", "link_only"})
	}

	var lists []database.List
	if err := query.Order("created_at DESC").Find(&lists).Error; err != nil {
		return internalError(c)
	}
	if lists == nil {
		lists = make([]database.List, 0)
	}

	return c.JSON(lists)
}

type createListRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Privacy       string `json:"privacy,omitempty"`
	SortBy        string `json:"sort_by,omitempty"`
	SortHow       string `json:"sort_how,omitempty"`
	IsRanked      *bool  `json:"is_ranked,omitempty"`
	CoverURL      string `json:"cover_url,omitempty"`
	AllowComments *bool  `json:"allow_comments,omitempty"`
}

// slugger generates a URL-safe slug from a name.
var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

func generateSlug(name, userID string) string {
	slug := strings.ToLower(name)
	slug = slugRegex.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "list"
	}
	return fmt.Sprintf("%s-%s", slug, userID[0:8])
}

func handleCreateList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	var req createListRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" {
		return badRequest(c, "name is required")
	}

	privacy := "public"
	if req.Privacy != "" {
		privacy = req.Privacy
	}
	if privacy != "public" && privacy != "link_only" && privacy != "private" {
		return badRequest(c, "privacy must be public, link_only, or private")
	}

	allowComments := true
	if req.AllowComments != nil {
		allowComments = *req.AllowComments
	}

	isRanked := false
	if req.IsRanked != nil {
		isRanked = *req.IsRanked
	}

	sortBy := "rank"
	if req.SortBy != "" {
		sortBy = req.SortBy
	}
	sortHow := "asc"
	if req.SortHow != "" {
		sortHow = req.SortHow
	}

	slug := generateSlug(req.Name, uid)

	list := database.List{
		UserID:        uid,
		Name:          strings.TrimSpace(req.Name),
		Description:   req.Description,
		Slug:          slug,
		Privacy:       privacy,
		SortBy:        sortBy,
		SortHow:       sortHow,
		IsRanked:      isRanked,
		CoverURL:      req.CoverURL,
		AllowComments: allowComments,
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Create(&list).Error; err != nil {
			// If slug collision, append timestamp
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				list.Slug = fmt.Sprintf("%s-%d", slug, time.Now().Unix())
				if err := tx.Create(&list).Error; err != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}

		bumpUserVersion(cacheTx, uid)
		if err := outbox.PublishEvent(tx, outbox.EventListCreated, uid, map[string]any{
			"list_id": list.ID,
			"slug":    list.Slug,
			"name":    list.Name,
			"privacy": list.Privacy,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(list)
}

// handleGetList returns a single list by ID (or by owner-slug).
// Public endpoint.
func handleGetList(c *fiber.Ctx) error {
	idParam := c.Params("id")

	db, err := database.GetDB()
	if err != nil {
		return internalError(c)
	}

	var list database.List
	// Try numeric ID first, then slug
	if id, parseErr := strconv.ParseInt(idParam, 10, 64); parseErr == nil {
		if err := db.First(&list, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return notFound(c, "list not found")
			}
			return internalError(c)
		}
	} else {
		if err := db.Where("slug = ?", idParam).First(&list).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return notFound(c, "list not found")
			}
			return internalError(c)
		}
	}

	// Privacy check
	currentUserID := userID(c)
	if list.Privacy == "private" && currentUserID != list.UserID {
		return forbidden(c, "this list is private")
	}

	return c.JSON(list)
}

type updateListRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Privacy       *string `json:"privacy,omitempty"`
	SortBy        *string `json:"sort_by,omitempty"`
	SortHow       *string `json:"sort_how,omitempty"`
	IsRanked      *bool   `json:"is_ranked,omitempty"`
	CoverURL      *string `json:"cover_url,omitempty"`
	AllowComments *bool   `json:"allow_comments,omitempty"`
}

func handleUpdateList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, parseErr := strconv.ParseInt(idParam, 10, 64)
	if parseErr != nil {
		return badRequest(c, "invalid list id")
	}

	db, err := database.GetDB()
	if err != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	// Ownership check
	if list.UserID != uid {
		return forbidden(c, "you do not have permission to edit this list")
	}

	var req updateListRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	updates := map[string]interface{}{}
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return badRequest(c, "name cannot be empty")
		}
		updates["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Privacy != nil {
		if *req.Privacy != "public" && *req.Privacy != "link_only" && *req.Privacy != "private" {
			return badRequest(c, "privacy must be public, link_only, or private")
		}
		updates["privacy"] = *req.Privacy
	}
	if req.SortBy != nil {
		updates["sort_by"] = *req.SortBy
	}
	if req.SortHow != nil {
		updates["sort_how"] = *req.SortHow
	}
	if req.IsRanked != nil {
		updates["is_ranked"] = *req.IsRanked
	}
	if req.CoverURL != nil {
		updates["cover_url"] = *req.CoverURL
	}
	if req.AllowComments != nil {
		updates["allow_comments"] = *req.AllowComments
	}

	if len(updates) == 0 {
		return badRequest(c, "no fields to update")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := advisoryListLock(tx, listID); err != nil {
			return nil, err
		}
		if err := tx.Model(&list).Updates(updates).Error; err != nil {
			return nil, err
		}

		bumpListVersion(cacheTx, listID)
		bumpUserVersion(cacheTx, uid)
		if err := outbox.PublishEvent(tx, outbox.EventListUpdated, uid, map[string]any{
			"list_id": listID,
			"slug":    list.Slug,
			"name":    list.Name,
			"privacy": list.Privacy,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	// Re-fetch
	db.First(&list, listID)
	return c.JSON(list)
}

func handleDeleteList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, parseErr := strconv.ParseInt(idParam, 10, 64)
	if parseErr != nil {
		return badRequest(c, "invalid list id")
	}

	db, err := database.GetDB()
	if err != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	if list.UserID != uid {
		return forbidden(c, "you do not have permission to delete this list")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := advisoryListLock(tx, listID); err != nil {
			return nil, err
		}
		if err := tx.Delete(&list).Error; err != nil {
			return nil, err
		}

		bumpUserVersion(cacheTx, uid)
		if err := outbox.PublishEvent(tx, outbox.EventListDeleted, uid, map[string]any{
			"list_id": listID,
			"slug":    list.Slug,
			"name":    list.Name,
		}); err != nil {
			return nil, err
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ============================================================================
// LIST ITEMS
// ============================================================================

type listItemResponse struct {
	ID        int64  `json:"id"`
	ListID    int64  `json:"list_id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Rank      int    `json:"rank"`
	Notes     string `json:"notes,omitempty"`
	AddedBy   string `json:"added_by"`
	AddedAt   string `json:"added_at"`
}

func buildListItemResponse(item *database.ListItem) listItemResponse {
	return listItemResponse{
		ID:        item.ID,
		ListID:    item.ListID,
		MediaType: item.MediaType,
		MediaID:   item.MediaID,
		Rank:      item.Rank,
		Notes:     item.Notes,
		AddedBy:   item.AddedBy,
		AddedAt:   item.AddedAt.Format(time.RFC3339),
	}
}

// mediaRef is a reference to a media item by TMDB ID.
type mediaRef struct {
	IDs struct {
		TMDB int `json:"tmdb"`
	} `json:"ids"`
	Rank  *int   `json:"rank,omitempty"`
	Notes string `json:"notes,omitempty"`
}

type addItemsRequest struct {
	Movies   []mediaRef `json:"movies,omitempty"`
	Shows    []mediaRef `json:"shows,omitempty"`
	Seasons  []mediaRef `json:"seasons,omitempty"`
	Episodes []mediaRef `json:"episodes,omitempty"`
}

type addItemsResponse struct {
	Added     map[string]int           `json:"added"`
	NotFound  []map[string]interface{} `json:"not_found"`
	Duplicate map[string]int           `json:"duplicate"`
}

type removeItemsRequest struct {
	IDs    []int64    `json:"ids,omitempty"`
	Movies []mediaRef `json:"movies,omitempty"`
	Shows  []mediaRef `json:"shows,omitempty"`
}

type removeItemsResponse struct {
	Removed int `json:"removed"`
}

type reorderRequest struct {
	Order []int64 `json:"order"`
}

func handleGetListItems(c *fiber.Ctx) error {
	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	// Verify list exists and check privacy
	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	currentUserID := userID(c)
	if list.Privacy == "private" && currentUserID != list.UserID {
		return forbidden(c, "this list is private")
	}

	// Parse filters
	mediaType := c.Query("type", "")
	page, limit := parsePagination(c)

	query := db.Where("list_id = ?", listID)
	if mediaType != "" {
		query = query.Where("media_type = ?", mediaType)
	}

	var total int64
	query.Model(&database.ListItem{}).Count(&total)

	// Determine sort order via allowlist — never interpolate user input
	// (List.SortBy) directly into ORDER BY.
	sortCol, sortDir := resolveListItemSort(list.SortBy, list.SortHow)

	var items []database.ListItem
	if err := query.Order(fmt.Sprintf("%s %s", sortCol, sortDir)).
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&items).Error; err != nil {
		return internalError(c)
	}
	if items == nil {
		items = make([]database.ListItem, 0)
	}

	results := make([]listItemResponse, 0, len(items))
	for _, item := range items {
		results = append(results, buildListItemResponse(&item))
	}

	extended := c.Query("extended", "") != ""

	resp := fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	}
	if extended {
		resp["list"] = list
	}

	return c.JSON(resp)
}

// listItemSortColumns maps user-facing sort keys to allowlisted SQL columns
// on list_items. Never interpolate user input into ORDER BY directly.
var listItemSortColumns = map[string]string{
	"rank":       "rank",
	"added":      "added_at",
	"media_type": "media_type",
	"media_id":   "media_id",
	"notes":      "notes",
}

// resolveListItemSort maps a user-supplied sort key to an allowlisted SQL
// column and direction. Unknown keys fall back to "rank"; sort_how is
// normalized to ASC/DESC.
func resolveListItemSort(sortBy, sortHow string) (string, string) {
	col, ok := listItemSortColumns[sortBy]
	if !ok {
		col = "rank"
	}
	dir := "ASC"
	if strings.ToUpper(sortHow) == "DESC" {
		dir = "DESC"
	}
	return col, dir
}

func resolveItemByTMDB(db *gorm.DB, mediaType string, tmdbID int) (int64, error) {
	var ext database.ExternalID
	if err := db.Where("source = ? AND external_id = ? AND media_type = ?", "tmdb", strconv.Itoa(tmdbID), mediaType).First(&ext).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("not found")
		}
		return 0, err
	}
	return ext.MediaID, nil
}

func handleAddListItems(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	// Only owner can add items
	if list.UserID != uid {
		return forbidden(c, "you do not have permission to modify this list")
	}

	var req addItemsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	result := addItemsResponse{
		Added:     make(map[string]int),
		NotFound:  make([]map[string]interface{}, 0),
		Duplicate: make(map[string]int),
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := advisoryListLock(tx, listID); err != nil {
			return nil, err
		}

		processMediaItems := func(items []mediaRef, mediaType string) {
			for _, ref := range items {
				mediaID, err := resolveItemByTMDB(tx, mediaType, ref.IDs.TMDB)
				if err != nil {
					result.NotFound = append(result.NotFound, map[string]interface{}{
						"type":    mediaType,
						"tmdb_id": ref.IDs.TMDB,
					})
					continue
				}

				rank := 0
				if ref.Rank != nil {
					rank = *ref.Rank
				}

				item := database.ListItem{
					ListID:    listID,
					MediaType: mediaType,
					MediaID:   mediaID,
					Rank:      rank,
					Notes:     ref.Notes,
					AddedBy:   uid,
				}

				res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
				if res.RowsAffected > 0 {
					result.Added[mediaType]++
				} else {
					result.Duplicate[mediaType]++
				}
			}
		}

		processMediaItems(req.Movies, "movie")
		processMediaItems(req.Shows, "show")
		processMediaItems(req.Seasons, "season")
		processMediaItems(req.Episodes, "episode")

		added := 0
		for _, n := range result.Added {
			added += n
		}
		if added > 0 {
			bumpListVersion(cacheTx, listID)
			if err := outbox.PublishEvent(tx, outbox.EventListItemsAdded, uid, map[string]any{
				"list_id": listID,
				"items":   result.Added,
			}); err != nil {
				return nil, err
			}
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(result)
}

func handleRemoveListItems(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	if list.UserID != uid {
		return forbidden(c, "you do not have permission to modify this list")
	}

	var req removeItemsRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	removed := 0

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := advisoryListLock(tx, listID); err != nil {
			return nil, err
		}

		// Remove by item IDs
		if len(req.IDs) > 0 {
			res := tx.Where("list_id = ? AND id IN ?", listID, req.IDs).Delete(&database.ListItem{})
			removed += int(res.RowsAffected)
		}

		// Remove by media references
		removeByMedia := func(items []mediaRef, mediaType string) {
			for _, ref := range items {
				mediaID, err := resolveItemByTMDB(tx, mediaType, ref.IDs.TMDB)
				if err != nil {
					continue
				}
				res := tx.Where("list_id = ? AND media_type = ? AND media_id = ?", listID, mediaType, mediaID).Delete(&database.ListItem{})
				removed += int(res.RowsAffected)
			}
		}
		removeByMedia(req.Movies, "movie")
		removeByMedia(req.Shows, "show")

		if removed > 0 {
			bumpListVersion(cacheTx, listID)
			if err := outbox.PublishEvent(tx, outbox.EventListItemsRemoved, uid, map[string]any{
				"list_id": listID,
				"removed": removed,
			}); err != nil {
				return nil, err
			}
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.JSON(removeItemsResponse{Removed: removed})
}

func handleReorderListItems(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	if list.UserID != uid {
		return forbidden(c, "you do not have permission to modify this list")
	}

	var req reorderRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	if len(req.Order) == 0 {
		return badRequest(c, "order array must not be empty")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := advisoryListLock(tx, listID); err != nil {
			return nil, err
		}

		// Assign rank based on position in the order array
		for i, itemID := range req.Order {
			if err := tx.Model(&database.ListItem{}).Where("id = ? AND list_id = ?", itemID, listID).Update("rank", i).Error; err != nil {
				return nil, err
			}
		}

		// Mark as ranked
		if err := tx.Model(&list).Update("is_ranked", true).Error; err != nil {
			return nil, err
		}

		bumpListVersion(cacheTx, listID)
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.JSON(fiber.Map{"status": "reordered", "count": len(req.Order)})
}

// ============================================================================
// LIKES
// ============================================================================

type likeUserResponse struct {
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

func handleGetListLikes(c *fiber.Ctx) error {
	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	var likes []database.ListLike
	if err := db.Where("list_id = ?", listID).Order("created_at DESC").Find(&likes).Error; err != nil {
		return internalError(c)
	}
	if likes == nil {
		likes = make([]database.ListLike, 0)
	}

	results := make([]likeUserResponse, 0, len(likes))
	for _, l := range likes {
		results = append(results, likeUserResponse{
			UserID:    l.UserID,
			CreatedAt: l.CreatedAt.Format(time.RFC3339),
		})
	}

	return c.JSON(results)
}

func handleLikeList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Insert like (ignore duplicates)
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.ListLike{
			ListID: listID,
			UserID: uid,
		})

		if res.RowsAffected > 0 {
			if err := tx.Model(&list).UpdateColumn("like_count", gorm.Expr("like_count + 1")).Error; err != nil {
				return nil, err
			}
			bumpListVersion(cacheTx, listID)
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.JSON(fiber.Map{"liked": true})
}

func handleUnlikeList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		res := tx.Where("list_id = ? AND user_id = ?", listID, uid).Delete(&database.ListLike{})
		if res.RowsAffected > 0 {
			if err := tx.Model(&database.List{}).Where("id = ?", listID).UpdateColumn("like_count", gorm.Expr("GREATEST(like_count - 1, 0)")).Error; err != nil {
				return nil, err
			}
			bumpListVersion(cacheTx, listID)
		}
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.JSON(fiber.Map{"liked": false})
}

// ============================================================================
// COMMENTS
// ============================================================================

func handleGetListComments(c *fiber.Ctx) error {
	idParam := c.Params("id")
	listID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var list database.List
	if err := db.First(&list, listID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "list not found")
		}
		return internalError(c)
	}

	var comments []database.ListComment
	if err := db.Where("list_id = ?", listID).Order("created_at ASC").Find(&comments).Error; err != nil {
		return internalError(c)
	}
	if comments == nil {
		comments = make([]database.ListComment, 0)
	}

	return c.JSON(comments)
}

// ============================================================================
// POPULAR / TRENDING
// ============================================================================

func handleGetPopularLists(c *fiber.Ctx) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	period := c.Query("period", "all")
	page, limit := parsePagination(c)

	query := db.Where("privacy = ?", "public")

	switch period {
	case "weekly":
		since := time.Now().AddDate(0, 0, -7)
		query = query.Where("created_at >= ?", since)
	case "monthly":
		since := time.Now().AddDate(0, -1, 0)
		query = query.Where("created_at >= ?", since)
	}

	var total int64
	query.Model(&database.List{}).Count(&total)

	var lists []database.List
	if err := query.Order("like_count DESC, created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&lists).Error; err != nil {
		return internalError(c)
	}
	if lists == nil {
		lists = make([]database.List, 0)
	}

	return c.JSON(fiber.Map{
		"items":     lists,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleGetTrendingLists(c *fiber.Ctx) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	page, limit := parsePagination(c)

	// Trending = recently created public lists with most likes (momentum).
	// Simplified: sorts by recent created_at with some like weight.
	var total int64
	db.Model(&database.List{}).Where("privacy = ?", "public").Count(&total)

	var lists []database.List
	if err := db.Where("privacy = ?", "public").
		Order("(like_count * 10 + EXTRACT(EPOCH FROM created_at) / 86400)::bigint DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&lists).Error; err != nil {
		return internalError(c)
	}
	if lists == nil {
		lists = make([]database.List, 0)
	}

	return c.JSON(fiber.Map{
		"items":     lists,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// ============================================================================
// SMART LISTS (T4.2)
// ============================================================================

type smartListRequest struct {
	Name    string                 `json:"name"`
	Filters map[string]interface{} `json:"filters"`
}

func handleGetSmartLists(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	db, err := database.GetDB()
	if err != nil {
		return internalError(c)
	}

	var lists []database.SmartList
	if err := db.Where("user_id = ?", uid).Order("created_at DESC").Find(&lists).Error; err != nil {
		return internalError(c)
	}
	if lists == nil {
		lists = make([]database.SmartList, 0)
	}

	return c.JSON(lists)
}

func handleCreateSmartList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	var req smartListRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" {
		return badRequest(c, "name is required")
	}

	sl := database.SmartList{
		UserID: uid,
		Name:   strings.TrimSpace(req.Name),
	}

	if req.Filters != nil {
		sl.Filters = database.JSONMap(req.Filters)
	} else {
		sl.Filters = database.JSONMap{}
	}

	err := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Create(&sl).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.Status(fiber.StatusCreated).JSON(sl)
}

func handleGetSmartList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	slID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid smart list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var sl database.SmartList
	if err := db.First(&sl, slID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "smart list not found")
		}
		return internalError(c)
	}

	if sl.UserID != uid {
		return forbidden(c, "you do not have access to this smart list")
	}

	return c.JSON(sl)
}

func handleUpdateSmartList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	slID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid smart list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var sl database.SmartList
	if err := db.First(&sl, slID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "smart list not found")
		}
		return internalError(c)
	}

	if sl.UserID != uid {
		return forbidden(c, "you do not have access to this smart list")
	}

	var req smartListRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = strings.TrimSpace(req.Name)
	}
	if req.Filters != nil {
		updates["filters"] = database.JSONMap(req.Filters)
	}

	if len(updates) == 0 {
		return badRequest(c, "no fields to update")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Model(&sl).Updates(updates).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	db.First(&sl, slID)
	return c.JSON(sl)
}

func handleDeleteSmartList(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	slID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid smart list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var sl database.SmartList
	if err := db.First(&sl, slID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "smart list not found")
		}
		return internalError(c)
	}

	if sl.UserID != uid {
		return forbidden(c, "you do not have access to this smart list")
	}

	err = cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		if err := tx.Delete(&sl).Error; err != nil {
			return nil, err
		}
		bumpUserVersion(cacheTx, uid)
		return cacheTx, nil
	})
	if err != nil {
		return internalError(c)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// handleGetSmartListItems resolves the smart list filter DSL dynamically.
func handleGetSmartListItems(c *fiber.Ctx) error {
	uid := userID(c)
	if uid == "" {
		return authRequired(c)
	}

	idParam := c.Params("id")
	slID, err := strconv.ParseInt(idParam, 10, 64)
	if err != nil {
		return badRequest(c, "invalid smart list id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return internalError(c)
	}

	var sl database.SmartList
	if err := db.First(&sl, slID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "smart list not found")
		}
		return internalError(c)
	}

	if sl.UserID != uid {
		return forbidden(c, "you do not have access to this smart list")
	}

	mediaTypeFilter := c.Query("type", "")
	sortBy := c.Query("sort_by", "popularity")
	page, limit := parsePagination(c)

	// Build query from filter DSL
	results, total, err := resolveSmartListQuery(db, sl.Filters, uid, mediaTypeFilter, sortBy, page, limit)
	if err != nil {
		return internalError(c)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// resolveSmartListQuery builds a GORM query from the filter DSL.
func resolveSmartListQuery(db *gorm.DB, filters database.JSONMap, userID, mediaTypeFilter, sortBy string, page, limit int) ([]map[string]interface{}, int64, error) {
	// Determine which media types to query.
	allowedTypes := []string{"movie", "show"}
	if rawTypes, ok := filters["type"]; ok {
		if typeArr, ok := rawTypes.([]interface{}); ok {
			allowedTypes = nil
			for _, t := range typeArr {
				if ts, ok := t.(string); ok {
					allowedTypes = append(allowedTypes, ts)
				}
			}
		}
	}

	// If a specific type is requested in the query, intersect
	if mediaTypeFilter != "" {
		found := false
		for _, t := range allowedTypes {
			if t == mediaTypeFilter {
				found = true
				break
			}
		}
		if found {
			allowedTypes = []string{mediaTypeFilter}
		} else {
			return []map[string]interface{}{}, 0, nil
		}
	}

	var allResults []map[string]interface{}
	var totalCount int64

	for _, mediaType := range allowedTypes {
		switch mediaType {
		case "movie":
			query := db.Table("movies")
			query = applyFilterDSL(query, filters, "movies")
			query = applyUserFilters(query, filters, userID, "movie")
			var count int64
			query.Count(&count)
			totalCount += count

			var movies []database.Movie
			q := db.Table("movies")
			q = applyFilterDSL(q, filters, "movies")
			q = applyUserFilters(q, filters, userID, "movie")

			if err := q.Order(fmt.Sprintf("%s DESC", sanitizeSortColumn(sortBy))).
				Offset((page - 1) * limit).
				Limit(limit).
				Find(&movies).Error; err != nil {
				return nil, 0, err
			}
			for _, m := range movies {
				year := 0
				if m.Released != nil {
					year = m.Released.Year()
				}
				allResults = append(allResults, map[string]interface{}{
					"type":     "movie",
					"movie_id": m.ID,
					"title":    m.Title,
					"year":     year,
					"rating":   m.RatingAvg,
				})
			}
		case "show":
			query := db.Table("shows")
			query = applyFilterDSL(query, filters, "shows")
			query = applyUserFilters(query, filters, userID, "show")
			var count int64
			query.Count(&count)
			totalCount += count

			var shows []database.Show
			q := db.Table("shows")
			q = applyFilterDSL(q, filters, "shows")
			q = applyUserFilters(q, filters, userID, "show")

			if err := q.Order(fmt.Sprintf("%s DESC", sanitizeSortColumn(sortBy))).
				Offset((page - 1) * limit).
				Limit(limit).
				Find(&shows).Error; err != nil {
				return nil, 0, err
			}
			for _, s := range shows {
				year := 0
				if s.FirstAired != nil {
					year = s.FirstAired.Year()
				}
				allResults = append(allResults, map[string]interface{}{
					"type":    "show",
					"show_id": s.ID,
					"title":   s.Title,
					"year":    year,
					"rating":  s.RatingAvg,
				})
			}
		}
	}

	if allResults == nil {
		allResults = make([]map[string]interface{}, 0)
	}

	return allResults, totalCount, nil
}

// applyFilterDSL applies the core filter DSL to a GORM query.
func applyFilterDSL(query *gorm.DB, filters database.JSONMap, tableAlias string) *gorm.DB {
	// Genre filter
	if rawGenres, ok := filters["genres"]; ok {
		if genres, ok := rawGenres.([]interface{}); ok && len(genres) > 0 {
			for _, g := range genres {
				if gs, ok := g.(string); ok {
					query = query.Where(fmt.Sprintf("%s.genres::jsonb ? ?", tableAlias), gs)
				}
			}
		}
	}

	// Year range filter
	if rawYear, ok := filters["year"]; ok {
		if yearStr, ok := rawYear.(string); ok {
			parts := strings.SplitN(yearStr, "-", 2)
			if len(parts) == 2 {
				startYear := parts[0]
				endYear := parts[1]
				if tableAlias == "movies" {
					if startYear != "" {
						query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.released) >= ?", tableAlias), startYear)
					}
					if endYear != "" {
						query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.released) <= ?", tableAlias), endYear)
					}
				} else {
					if startYear != "" {
						query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.first_aired) >= ?", tableAlias), startYear)
					}
					if endYear != "" {
						query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.first_aired) <= ?", tableAlias), endYear)
					}
				}
			} else if yearStr != "" {
				if tableAlias == "movies" {
					query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.released) = ?", tableAlias), yearStr)
				} else {
					query = query.Where(fmt.Sprintf("EXTRACT(YEAR FROM %s.first_aired) = ?", tableAlias), yearStr)
				}
			}
		}
	}

	// Rating minimum
	if rawRating, ok := filters["rating_min"]; ok {
		if rating, ok := toFloat64(rawRating); ok {
			query = query.Where(fmt.Sprintf("%s.rating_avg >= ?", tableAlias), rating)
		}
	}

	// Vote count minimum
	if rawVotes, ok := filters["votes_min"]; ok {
		if votes, ok := toFloat64(rawVotes); ok {
			query = query.Where(fmt.Sprintf("%s.rating_count >= ?", tableAlias), int64(votes))
		}
	}

	// Status filter (shows only)
	if rawStatus, ok := filters["status"]; ok {
		if statuses, ok := rawStatus.([]interface{}); ok && len(statuses) > 0 && tableAlias == "shows" {
			statusStrs := make([]string, 0, len(statuses))
			for _, s := range statuses {
				if ss, ok := s.(string); ok {
					statusStrs = append(statusStrs, ss)
				}
			}
			if len(statusStrs) > 0 {
				query = query.Where(fmt.Sprintf("%s.status IN ?", tableAlias), statusStrs)
			}
		}
	}

	return query
}

// applyUserFilters applies watched/collected filters against user_* tables.
func applyUserFilters(query *gorm.DB, filters database.JSONMap, userID, mediaType string) *gorm.DB {
	// Watched filter
	if rawWatched, ok := filters["watched"]; ok {
		if watched, ok := rawWatched.(string); ok {
			subQuery := fmt.Sprintf(
				"SELECT 1 FROM user_history WHERE user_history.user_id = ? AND user_history.media_type = ? AND user_history.media_id = %s.id", mediaType)
			switch watched {
			case "all":
				query = query.Where(fmt.Sprintf("EXISTS (%s)", subQuery), userID, mediaType)
			case "none":
				query = query.Where(fmt.Sprintf("NOT EXISTS (%s)", subQuery), userID, mediaType)
			}
		}
	}

	// Collected filter
	if rawCollected, ok := filters["collected"]; ok {
		if collected, ok := rawCollected.(string); ok {
			subQuery := fmt.Sprintf(
				"SELECT 1 FROM user_collection WHERE user_collection.user_id = ? AND user_collection.media_type = ? AND user_collection.media_id = %s.id", mediaType)
			switch collected {
			case "all":
				query = query.Where(fmt.Sprintf("EXISTS (%s)", subQuery), userID, mediaType)
			case "none":
				query = query.Where(fmt.Sprintf("NOT EXISTS (%s)", subQuery), userID, mediaType)
			}
		}
	}

	return query
}

func sanitizeSortColumn(col string) string {
	allowed := map[string]bool{
		"popularity":   true,
		"rating_avg":   true,
		"rating_count": true,
		"released":     true,
		"first_aired":  true,
		"created_at":   true,
		"title":        true,
	}
	if allowed[col] {
		return col
	}
	return "popularity"
}

// ============================================================================
// HELPERS
// ============================================================================

func parsePagination(c *fiber.Ctx) (page, limit int) {
	page, limit = 1, 25
	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}
	return
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// ============================================================================
// ERROR HELPERS
// ============================================================================

func authRequired(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": fiber.Map{"code": "unauthorized", "message": "invalid or expired token"},
	})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{"code": "invalid_request", "message": msg},
	})
}

func notFound(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{"code": "not_found", "message": msg},
	})
}

func forbidden(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error": fiber.Map{"code": "forbidden", "message": msg},
	})
}

func internalError(c *fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": fiber.Map{"code": "internal", "message": "internal server error"},
	})
}
