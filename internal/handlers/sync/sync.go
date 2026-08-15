package sync

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"trakt-api/internal/cache"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
	"trakt-api/internal/outbox"
	"trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"trakt-api/internal/auth"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// History
		{Method: "GET", Path: "/v1/sync/history", Summary: "Get user watch history", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "History items", 401: "Unauthorized"}},
		{Method: "GET", Path: "/v1/sync/history/{media_type}/{media_id}", Summary: "Get history for a single item", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "History items", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/history", Summary: "Add history items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Add result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/history/remove", Summary: "Remove history items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Remove result", 401: "Unauthorized"}},
		// Collection
		{Method: "GET", Path: "/v1/sync/collection", Summary: "Get user collection", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Collection items", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/collection", Summary: "Add collection items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Add result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/collection/remove", Summary: "Remove collection items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Remove result", 401: "Unauthorized"}},
		// Watchlist
		{Method: "GET", Path: "/v1/sync/watchlist", Summary: "Get user watchlist", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Watchlist items", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/watchlist", Summary: "Add watchlist items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Add result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/watchlist/remove", Summary: "Remove watchlist items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Remove result", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/sync/watchlist/reorder", Summary: "Reorder watchlist", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Success", 401: "Unauthorized"}},
		// Ratings
		{Method: "GET", Path: "/v1/sync/ratings", Summary: "Get user ratings", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Rating items", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/ratings", Summary: "Add/update ratings", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Add result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/ratings/remove", Summary: "Remove ratings", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Remove result", 401: "Unauthorized"}},
		// Favorites
		{Method: "GET", Path: "/v1/sync/favorites", Summary: "Get user favorites", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Favorite items", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/favorites", Summary: "Add favorites", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Add result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/favorites/remove", Summary: "Remove favorites", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Remove result", 401: "Unauthorized"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all sync route handlers.
func RegisterRoutes(api fiber.Router) {
	sync := api.Group("/sync")
	sync.Use(auth.AuthRequired())

	// History
	sync.Get("/history", handleGetHistory)
	sync.Get("/history/:media_type/:media_id", handleGetHistoryItem)
	sync.Post("/history", handleAddHistory)
	sync.Post("/history/remove", handleRemoveHistory)

	// Collection
	sync.Get("/collection", handleGetCollection)
	sync.Post("/collection", handleAddCollection)
	sync.Post("/collection/remove", handleRemoveCollection)

	// Watchlist
	sync.Get("/watchlist", handleGetWatchlist)
	sync.Post("/watchlist", handleAddWatchlist)
	sync.Post("/watchlist/remove", handleRemoveWatchlist)
	sync.Put("/watchlist/reorder", handleReorderWatchlist)

	// Ratings
	sync.Get("/ratings", handleGetRatings)
	sync.Post("/ratings", handleAddRatings)
	sync.Post("/ratings/remove", handleRemoveRatings)

	// Favorites
	sync.Get("/favorites", handleGetFavorites)
	sync.Post("/favorites", handleAddFavorites)
	sync.Post("/favorites/remove", handleRemoveFavorites)
}

// ============================================================================
// HISTORY
// ============================================================================

// handleGetHistory returns paginated watch history, most-recent-first.
func handleGetHistory(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	mediaType := c.Query("type", "") // movie,episode or empty=all
	page, limit := response.ParsePagination(c, 25)
	startAt := c.Query("start_at")
	endAt := c.Query("end_at")

	query := db.Where("user_id = ?", userID)
	if mediaType == "movie" {
		query = query.Where("media_type = ?", "movie")
	} else if mediaType == "episode" {
		query = query.Where("media_type = ?", "episode")
	}
	if startAt != "" {
		if t, err := time.Parse(time.RFC3339, startAt); err == nil {
			query = query.Where("watched_at >= ?", t)
		}
	}
	if endAt != "" {
		if t, err := time.Parse(time.RFC3339, endAt); err == nil {
			query = query.Where("watched_at <= ?", t)
		}
	}

	var total int64
	query.Model(&database.UserHistory{}).Count(&total)

	var items []database.UserHistory
	if err := query.Order("watched_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := make([]historyResponse, 0, len(items))
	for _, item := range items {
		results = append(results, buildHistoryResponse(&item))
	}
	if results == nil {
		results = make([]historyResponse, 0)
	}

	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// handleGetHistoryItem returns history for a specific media item.
func handleGetHistoryItem(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	mt := c.Params("media_type")
	mediaParam := c.Params("media_id")

	resolvedMT, mediaID, err := media.ResolveID(c, mediaParam)
	if err != nil {
		return err
	}
	if resolvedMT != mt {
		return response.BadRequest(c, "media_type does not match the resolved media id")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var items []database.UserHistory
	if err := db.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mt, mediaID).
		Order("watched_at DESC").
		Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := make([]historyResponse, 0, len(items))
	for _, item := range items {
		results = append(results, buildHistoryResponse(&item))
	}
	if results == nil {
		results = make([]historyResponse, 0)
	}

	return c.JSON(results)
}

// historyAddRequest is the body for POST /v1/sync/history.
type historyAddRequest struct {
	Movies   []historyMovie   `json:"movies,omitempty"`
	Shows    []historyShow    `json:"shows,omitempty"`
	Episodes []historyEpisode `json:"episodes,omitempty"`
}

type historyMovie struct {
	IDs       idsObj `json:"ids"`
	WatchedAt string `json:"watched_at,omitempty"`
}

type historyShow struct {
	IDs     idsObj          `json:"ids"`
	Seasons []historySeason `json:"seasons,omitempty"`
}

type historySeason struct {
	Number   int              `json:"number"`
	Episodes []historyEpisode `json:"episodes,omitempty"`
}

type historyEpisode struct {
	IDs       idsObj `json:"ids"`
	Season    int    `json:"season,omitempty"`
	Number    int    `json:"number,omitempty"`
	WatchedAt string `json:"watched_at,omitempty"`
}

type idsObj struct {
	TMDB int    `json:"tmdb"`
	TVDB int    `json:"tvdb,omitempty"`
	IMDB string `json:"imdb,omitempty"`
}

type historyAddResponse struct {
	Added struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"added"`
	NotFound struct {
		Movies   []map[string]interface{} `json:"movies"`
		Shows    []map[string]interface{} `json:"shows"`
		Seasons  []map[string]interface{} `json:"seasons"`
		Episodes []map[string]interface{} `json:"episodes"`
	} `json:"not_found"`
	Duplicates struct {
		Movies   int `json:"movies"`
		Episodes int `json:"episodes"`
	} `json:"duplicates"`
}

// handleAddHistory adds watch history items with show/season expansion.
func handleAddHistory(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req historyAddRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	resp := historyAddResponse{}
	resp.NotFound.Movies = make([]map[string]interface{}, 0)
	resp.NotFound.Shows = make([]map[string]interface{}, 0)
	resp.NotFound.Seasons = make([]map[string]interface{}, 0)
	resp.NotFound.Episodes = make([]map[string]interface{}, 0)

	now := time.Now()
	var movieIDs []int64
	var episodes []database.Episode

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Process movies
		for _, m := range req.Movies {
			mediaType, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil || mediaType != "movie" {
				resp.NotFound.Movies = append(resp.NotFound.Movies, map[string]interface{}{"ids": m.IDs})
				continue
			}
			watchedAt, err := parseWatchedAt(m.WatchedAt, now, nil)
			if err != nil {
				resp.NotFound.Movies = append(resp.NotFound.Movies, map[string]interface{}{
					"ids": m.IDs, "error": "invalid watched_at",
				})
				continue
			}
			added, dup := insertHistory(tx, userID, "movie", mediaID, nil, nil, watchedAt)
			if dup {
				resp.Duplicates.Movies++
			} else if added {
				resp.Added.Movies++
				movieIDs = append(movieIDs, mediaID)
			}
		}

		// Process shows — expand to episodes
		for _, s := range req.Shows {
			mediaType, mediaID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil || mediaType != "show" {
				resp.NotFound.Shows = append(resp.NotFound.Shows, map[string]interface{}{"ids": s.IDs})
				continue
			}

			if len(s.Seasons) > 0 {
				// Expand specific seasons
				for _, season := range s.Seasons {
					seasonRow, found := findSeason(tx, mediaID, season.Number)
					if !found {
						resp.NotFound.Seasons = append(resp.NotFound.Seasons, map[string]interface{}{
							"ids": s.IDs, "season": season.Number,
						})
						continue
					}

					if len(season.Episodes) > 0 {
						// Specific episodes within season
						for _, ep := range season.Episodes {
							epRow, found := findEpisode(tx, seasonRow.ID, ep.Number)
							if !found {
								resp.NotFound.Episodes = append(resp.NotFound.Episodes, map[string]interface{}{
									"ids": s.IDs, "season": season.Number, "episode": ep.Number,
								})
								continue
							}
							wt, _ := parseWatchedAt("unknown", now, epRow.AiredAt)
							added, dup := insertHistory(tx, userID, "episode", epRow.ID, &seasonRow.ID, &epRow.ID, wt)
							if dup {
								resp.Duplicates.Episodes++
							} else if added {
								resp.Added.Episodes++
								episodes = append(episodes, *epRow)
							}
						}
					} else {
						// All episodes in this season
						eps := findAllEpisodesInSeason(tx, seasonRow.ID)
						for i := range eps {
							wt, _ := parseWatchedAt("unknown", now, eps[i].AiredAt)
							added, dup := insertHistory(tx, userID, "episode", eps[i].ID, &seasonRow.ID, &eps[i].ID, wt)
							if dup {
								resp.Duplicates.Episodes++
							} else if added {
								resp.Added.Episodes++
								episodes = append(episodes, eps[i])
							}
						}
					}
				}
			} else {
				// All aired episodes of the show
				eps := findAllEpisodesInShow(tx, mediaID)
				for i := range eps {
					wt, _ := parseWatchedAt("unknown", now, eps[i].AiredAt)
					added, dup := insertHistory(tx, userID, "episode", eps[i].ID, &eps[i].SeasonID, &eps[i].ID, wt)
					if dup {
						resp.Duplicates.Episodes++
					} else if added {
						resp.Added.Episodes++
						episodes = append(episodes, eps[i])
					}
				}
			}
		}

		// Process standalone episodes (with show TMDB ID, season, and episode number)
		for _, ep := range req.Episodes {
			mediaType, showID, err := resolveByTMDB(c, tx, ep.IDs.TMDB)
			if err != nil || mediaType != "show" {
				resp.NotFound.Episodes = append(resp.NotFound.Episodes, map[string]interface{}{
					"ids": ep.IDs, "season": ep.Season, "number": ep.Number,
				})
				continue
			}

			seasonRow, found := findSeason(tx, showID, ep.Season)
			if !found {
				resp.NotFound.Episodes = append(resp.NotFound.Episodes, map[string]interface{}{
					"ids": ep.IDs, "season": ep.Season, "number": ep.Number,
				})
				continue
			}

			epRow, found := findEpisode(tx, seasonRow.ID, ep.Number)
			if !found {
				resp.NotFound.Episodes = append(resp.NotFound.Episodes, map[string]interface{}{
					"ids": ep.IDs, "season": ep.Season, "number": ep.Number,
				})
				continue
			}

			wt, err := parseWatchedAt(ep.WatchedAt, now, epRow.AiredAt)
			if err != nil {
				resp.NotFound.Episodes = append(resp.NotFound.Episodes, map[string]interface{}{
					"ids": ep.IDs, "season": ep.Season, "number": ep.Number, "error": "invalid watched_at",
				})
				continue
			}
			added, dup := insertHistory(tx, userID, "episode", epRow.ID, &seasonRow.ID, &epRow.ID, wt)
			if dup {
				resp.Duplicates.Episodes++
			} else if added {
				resp.Added.Episodes++
				episodes = append(episodes, *epRow)
			}
		}

		// Commit-side effects: invalidate caches and emit the outbox event
		// only when something actually changed.
		if resp.Added.Movies+resp.Added.Episodes > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, ep := range episodes {
				queueEpisodeInvalidations(tx, cacheTx, ep)
			}
			if err := outbox.PublishEvent(tx, outbox.EventHistoryAdded, userID, map[string]any{
				"movies_added":   resp.Added.Movies,
				"episodes_added": resp.Added.Episodes,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(resp)
}

// handleRemoveHistory removes history items by IDs or media objects.
func handleRemoveHistory(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		IDs      []int64          `json:"ids,omitempty"`
		Movies   []historyMovie   `json:"movies,omitempty"`
		Shows    []historyShow    `json:"shows,omitempty"`
		Episodes []historyEpisode `json:"episodes,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	deleted := 0
	var movieIDs []int64
	var episodes []database.Episode

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Remove by raw IDs
		if len(req.IDs) > 0 {
			result := tx.Where("user_id = ? AND id IN ?", userID, req.IDs).Delete(&database.UserHistory{})
			deleted += int(result.RowsAffected)
		}

		// Remove by media objects (delete all history for those items)
		for _, m := range req.Movies {
			_, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "movie", mediaID).
				Delete(&database.UserHistory{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				movieIDs = append(movieIDs, mediaID)
			}
		}
		for _, s := range req.Shows {
			_, showID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil {
				continue
			}
			if len(s.Seasons) == 0 {
				// Remove all episodes for this show
				eps := findAllEpisodesInShow(tx, showID)
				epIDs := make([]int64, 0, len(eps))
				for i := range eps {
					epIDs = append(epIDs, eps[i].ID)
				}
				if len(epIDs) > 0 {
					result := tx.Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", epIDs).
						Delete(&database.UserHistory{})
					if result.RowsAffected > 0 {
						deleted += int(result.RowsAffected)
						episodes = append(episodes, eps...)
					}
				}
			} else {
				for _, season := range s.Seasons {
					seasonRow, found := findSeason(tx, showID, season.Number)
					if !found {
						continue
					}
					if len(season.Episodes) > 0 {
						for _, ep := range season.Episodes {
							epRow, found := findEpisode(tx, seasonRow.ID, ep.Number)
							if !found {
								continue
							}
							result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "episode", epRow.ID).
								Delete(&database.UserHistory{})
							if result.RowsAffected > 0 {
								deleted += int(result.RowsAffected)
								episodes = append(episodes, *epRow)
							}
						}
					} else {
						eps := findAllEpisodesInSeason(tx, seasonRow.ID)
						epIDs := make([]int64, 0, len(eps))
						for i := range eps {
							epIDs = append(epIDs, eps[i].ID)
						}
						if len(epIDs) > 0 {
							result := tx.Where("user_id = ? AND media_type = ? AND media_id IN ?", userID, "episode", epIDs).
								Delete(&database.UserHistory{})
							if result.RowsAffected > 0 {
								deleted += int(result.RowsAffected)
								episodes = append(episodes, eps...)
							}
						}
					}
				}
			}
		}
		for _, ep := range req.Episodes {
			_, showID, err := resolveByTMDB(c, tx, ep.IDs.TMDB)
			if err != nil {
				continue
			}
			seasonRow, found := findSeason(tx, showID, ep.Season)
			if !found {
				continue
			}
			epRow, found := findEpisode(tx, seasonRow.ID, ep.Number)
			if !found {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "episode", epRow.ID).
				Delete(&database.UserHistory{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				episodes = append(episodes, *epRow)
			}
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, ep := range episodes {
				queueEpisodeInvalidations(tx, cacheTx, ep)
			}
			if err := outbox.PublishEvent(tx, outbox.EventHistoryRemoved, userID, map[string]any{
				"deleted": deleted,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted})
}

// ============================================================================
// COLLECTION
// ============================================================================

func handleGetCollection(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	var items []database.UserCollection
	query := db.Where("user_id = ?", userID).Order("collected_at DESC")
	if mt := c.Query("type"); mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	page, limit := response.ParsePagination(c, 25)
	var total int64
	query.Model(&database.UserCollection{}).Count(&total)

	if err := query.Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := buildCollectionResponses(items)
	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleAddCollection(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []historyMovie `json:"movies,omitempty"`
		Shows  []historyShow  `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	added, notFound, duplicates := 0, 0, 0
	var movieIDs []int64
	var showIDs []int64
	now := time.Now()

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		for _, m := range req.Movies {
			mediaType, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil || mediaType != "movie" {
				notFound++
				continue
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserCollection{
				UserID: userID, MediaType: "movie", MediaID: mediaID, CollectedAt: now,
			})
			if result.RowsAffected > 0 {
				added++
				movieIDs = append(movieIDs, mediaID)
			} else {
				duplicates++
			}
		}
		for _, s := range req.Shows {
			mediaType, mediaID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil || mediaType != "show" {
				notFound++
				continue
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserCollection{
				UserID: userID, MediaType: "show", MediaID: mediaID, CollectedAt: now,
			})
			if result.RowsAffected > 0 {
				added++
				showIDs = append(showIDs, mediaID)
			} else {
				duplicates++
			}
		}

		if added > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			if err := outbox.PublishEvent(tx, outbox.EventCollectionAdded, userID, map[string]any{
				"movies_added": len(movieIDs),
				"shows_added":  len(showIDs),
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{
		"added":      fiber.Map{"movies": added, "episodes": 0},
		"not_found":  fiber.Map{"movies": make([]interface{}, 0, notFound), "shows": make([]interface{}, 0)},
		"duplicates": fiber.Map{"movies": duplicates, "episodes": 0},
	})
}

func handleRemoveCollection(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []historyMovie `json:"movies,omitempty"`
		Shows  []historyShow  `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	deleted := 0
	var movieIDs []int64
	var showIDs []int64

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		for _, m := range req.Movies {
			_, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "movie", mediaID).
				Delete(&database.UserCollection{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				movieIDs = append(movieIDs, mediaID)
			}
		}
		for _, s := range req.Shows {
			_, showID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "show", showID).
				Delete(&database.UserCollection{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				showIDs = append(showIDs, showID)
			}
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			// EventCollectionAdded's wire value is "collection.updated",
			// which correctly covers both adds and removals.
			if err := outbox.PublishEvent(tx, outbox.EventCollectionAdded, userID, map[string]any{
				"deleted": deleted,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted})
}

// ============================================================================
// WATCHLIST
// ============================================================================

func handleGetWatchlist(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	sortBy := c.Query("sort_by", "rank")
	sortHow := c.Query("sort_how", "asc")
	if sortHow != "asc" && sortHow != "desc" {
		sortHow = "asc"
	}

	var order string
	switch sortBy {
	case "rank":
		order = "rank " + sortHow
	case "listed", "added":
		order = "listed_at " + sortHow
	case "title":
		order = "media_type " + sortHow + ", media_id " + sortHow
	default:
		order = "rank ASC"
	}

	page, limit := response.ParsePagination(c, 25)

	var items []database.UserWatchlist
	query := db.Where("user_id = ?", userID)
	if mt := c.Query("type"); mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserWatchlist{}).Count(&total)

	if err := query.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := buildWatchlistResponses(items)
	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleAddWatchlist(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []struct {
			IDs   idsObj  `json:"ids"`
			Notes *string `json:"notes,omitempty"`
		} `json:"movies,omitempty"`
		Shows []struct {
			IDs   idsObj  `json:"ids"`
			Notes *string `json:"notes,omitempty"`
		} `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	addedMovies, addedShows := 0, 0
	dupMovies, dupShows := 0, 0
	notFound := 0
	var movieIDs []int64
	var showIDs []int64
	now := time.Now()

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Serialize rank assignment per user (advisory lock), then read the
		// current max so concurrent adds can't produce duplicate ranks.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('watchlist'), hashtext(?))", userID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}
		var maxRank int
		if err := tx.Model(&database.UserWatchlist{}).Where("user_id = ?", userID).
			Select("COALESCE(MAX(rank), 0)").Scan(&maxRank).Error; err != nil {
			return nil, err
		}

		for _, m := range req.Movies {
			mediaType, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil || mediaType != "movie" {
				notFound++
				continue
			}
			maxRank++
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserWatchlist{
				UserID: userID, MediaType: "movie", MediaID: mediaID, Rank: maxRank,
				Notes: m.Notes, ListedAt: now,
			})
			if result.RowsAffected > 0 {
				addedMovies++
				movieIDs = append(movieIDs, mediaID)
			} else {
				dupMovies++
				maxRank-- // rollback rank increment
			}
		}
		for _, s := range req.Shows {
			mediaType, mediaID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil || mediaType != "show" {
				notFound++
				continue
			}
			maxRank++
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserWatchlist{
				UserID: userID, MediaType: "show", MediaID: mediaID, Rank: maxRank,
				Notes: s.Notes, ListedAt: now,
			})
			if result.RowsAffected > 0 {
				addedShows++
				showIDs = append(showIDs, mediaID)
			} else {
				dupShows++
				maxRank--
			}
		}

		if addedMovies+addedShows > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			if err := outbox.PublishEvent(tx, outbox.EventWatchlistAdded, userID, map[string]any{
				"movies_added": addedMovies,
				"shows_added":  addedShows,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{
		"added":      fiber.Map{"movies": addedMovies, "shows": addedShows},
		"not_found":  fiber.Map{"movies": make([]interface{}, 0), "shows": make([]interface{}, 0)},
		"duplicates": fiber.Map{"movies": dupMovies, "shows": dupShows},
	})
}

func handleRemoveWatchlist(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []historyMovie `json:"movies,omitempty"`
		Shows  []historyShow  `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	deleted := 0
	var movieIDs []int64
	var showIDs []int64

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		for _, m := range req.Movies {
			_, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "movie", mediaID).
				Delete(&database.UserWatchlist{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				movieIDs = append(movieIDs, mediaID)
			}
		}
		for _, s := range req.Shows {
			_, mediaID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "show", mediaID).
				Delete(&database.UserWatchlist{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				showIDs = append(showIDs, mediaID)
			}
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			if err := outbox.PublishEvent(tx, outbox.EventWatchlistRemoved, userID, map[string]any{
				"deleted": deleted,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted})
}

func handleReorderWatchlist(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Rank []int64 `json:"rank"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Serialize rank mutations per user so a concurrent add/reorder can't
		// interleave and produce duplicate or gapped ranks.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('watchlist'), hashtext(?))", userID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}

		// The payload must be the complete set of the user's watchlist items
		// (Trakt semantics): same length, every id owned by the user, no dupes.
		var items []database.UserWatchlist
		if err := tx.Select("id").Where("user_id = ?", userID).Find(&items).Error; err != nil {
			return nil, err
		}
		owned := make([]int64, len(items))
		for i, it := range items {
			owned[i] = it.ID
		}
		if err := validateReorder(req.Rank, owned); err != nil {
			return nil, err
		}
		for i, id := range req.Rank {
			// Ranks are 1-based (Trakt convention).
			if err := tx.Model(&database.UserWatchlist{}).
				Where("id = ? AND user_id = ?", id, userID).
				Update("rank", i+1).Error; err != nil {
				return nil, err
			}
		}

		if len(req.Rank) > 0 {
			queueUserInvalidation(cacheTx, userID)
			// No event: the webhook catalog has no reorder event, and
			// "watchlist.added"/"watchlist.removed" would be misleading.
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		if errors.Is(dbErr, errIncompleteReorder) {
			return response.BadRequest(c, "rank must contain exactly the current watchlist items, without duplicates")
		}
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"success": true, "count": len(req.Rank)})
}

// ============================================================================
// RATINGS
// ============================================================================

func handleGetRatings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ?", userID)
	if mt := c.Query("type"); mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserRating{}).Count(&total)

	var items []database.UserRating
	if err := query.Order("rated_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := buildRatingResponses(items)
	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleAddRatings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []struct {
			IDs    idsObj `json:"ids"`
			Rating int    `json:"rating"`
		} `json:"movies,omitempty"`
		Shows []struct {
			IDs    idsObj `json:"ids"`
			Rating int    `json:"rating"`
		} `json:"shows,omitempty"`
		Seasons []struct {
			IDs    idsObj `json:"ids"`
			Rating int    `json:"rating"`
		} `json:"seasons,omitempty"`
		Episodes []struct {
			IDs    idsObj `json:"ids"`
			Rating int    `json:"rating"`
		} `json:"episodes,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	addedMovies, addedShows, addedSeasons, addedEpisodes := 0, 0, 0, 0
	notFound := 0
	var mediaRefs []mediaRef
	now := time.Now()

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		upsertRating := func(ids idsObj, rating int) {
			if rating < 1 || rating > 10 {
				notFound++
				return
			}
			mediaTypeResolved, mediaID, err := resolveByTMDB(c, tx, ids.TMDB)
			if err != nil || mediaTypeResolved == "" {
				notFound++
				return
			}
			tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}, {Name: "media_type"}, {Name: "media_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"rating", "rated_at", "updated_at"}),
			}).Create(&database.UserRating{
				UserID: userID, MediaType: mediaTypeResolved, MediaID: mediaID,
				Rating: rating, RatedAt: now, UpdatedAt: now,
			})
			switch mediaTypeResolved {
			case "movie":
				addedMovies++
			case "show":
				addedShows++
			case "season":
				addedSeasons++
			case "episode":
				addedEpisodes++
			}
			mediaRefs = append(mediaRefs, mediaRef{mediaType: mediaTypeResolved, mediaID: mediaID})
		}

		for _, m := range req.Movies {
			upsertRating(m.IDs, m.Rating)
		}
		for _, s := range req.Shows {
			upsertRating(s.IDs, s.Rating)
		}
		for _, s := range req.Seasons {
			upsertRating(s.IDs, s.Rating)
		}
		for _, e := range req.Episodes {
			upsertRating(e.IDs, e.Rating)
		}

		if addedMovies+addedShows+addedSeasons+addedEpisodes > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, r := range mediaRefs {
				queueResolvedInvalidation(tx, cacheTx, r.mediaType, r.mediaID)
			}
			if err := outbox.PublishEvent(tx, outbox.EventRatingAdded, userID, map[string]any{
				"added": addedMovies + addedShows + addedSeasons + addedEpisodes,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{
		"added":     fiber.Map{"movies": addedMovies, "shows": addedShows, "seasons": addedSeasons, "episodes": addedEpisodes},
		"not_found": fiber.Map{"movies": make([]interface{}, 0)},
	})
}

func handleRemoveRatings(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies   []historyMovie   `json:"movies,omitempty"`
		Shows    []historyShow    `json:"shows,omitempty"`
		Seasons  []historyMovie   `json:"seasons,omitempty"`
		Episodes []historyEpisode `json:"episodes,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	deleted := 0
	var mediaRefs []mediaRef

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		removeByMovie := func(ids idsObj, mediaType string) {
			_, mediaID, err := resolveByTMDB(c, tx, ids.TMDB)
			if err != nil {
				return
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, mediaType, mediaID).
				Delete(&database.UserRating{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				mediaRefs = append(mediaRefs, mediaRef{mediaType: mediaType, mediaID: mediaID})
			}
		}

		for _, m := range req.Movies {
			removeByMovie(m.IDs, "movie")
		}
		for _, s := range req.Shows {
			removeByMovie(s.IDs, "show")
		}
		for _, s := range req.Seasons {
			removeByMovie(s.IDs, "season")
		}
		for _, e := range req.Episodes {
			_, showID, err := resolveByTMDB(c, tx, e.IDs.TMDB)
			if err != nil {
				continue
			}
			seasonRow, found := findSeason(tx, showID, e.Season)
			if !found {
				continue
			}
			epRow, found := findEpisode(tx, seasonRow.ID, e.Number)
			if !found {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "episode", epRow.ID).
				Delete(&database.UserRating{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				mediaRefs = append(mediaRefs, mediaRef{mediaType: "episode", mediaID: epRow.ID})
			}
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, r := range mediaRefs {
				queueResolvedInvalidation(tx, cacheTx, r.mediaType, r.mediaID)
			}
			// No event: the webhook catalog has no "rating.removed", and
			// publishing "rating.added" on a removal would be misleading.
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted})
}

// ============================================================================
// FAVORITES
// ============================================================================

func handleGetFavorites(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	db, err := database.GetDB()
	if err != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ?", userID)
	if mt := c.Query("type"); mt != "" {
		query = query.Where("media_type = ?", mt)
	}

	var total int64
	query.Model(&database.UserFavorite{}).Count(&total)

	var items []database.UserFavorite
	if err := query.Order("rank ASC, favorited_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return response.InternalError(c)
	}

	results := buildFavoriteResponses(items)
	return c.JSON(fiber.Map{
		"items":     results,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

func handleAddFavorites(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []historyMovie `json:"movies,omitempty"`
		Shows  []historyShow  `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	addedMovies, addedShows := 0, 0
	dupMovies, dupShows := 0, 0
	notFound := 0
	var movieIDs []int64
	var showIDs []int64
	now := time.Now()

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		// Serialize rank assignment per user (advisory lock), then read the
		// current max so concurrent adds can't produce duplicate ranks.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('favorites'), hashtext(?))", userID).Error; err != nil {
			return nil, fmt.Errorf("advisory lock: %w", err)
		}
		var maxRank int
		if err := tx.Model(&database.UserFavorite{}).Where("user_id = ?", userID).
			Select("COALESCE(MAX(rank), 0)").Scan(&maxRank).Error; err != nil {
			return nil, err
		}

		addFavorite := func(tmdbID int, mediaType string) {
			mtResolved, mediaID, err := resolveByTMDB(c, tx, tmdbID)
			if err != nil || mtResolved != mediaType {
				notFound++
				return
			}
			maxRank++
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserFavorite{
				UserID: userID, MediaType: mediaType, MediaID: mediaID,
				Rank: maxRank, FavoritedAt: now,
			})
			if result.RowsAffected > 0 {
				if mediaType == "movie" {
					addedMovies++
					movieIDs = append(movieIDs, mediaID)
				} else {
					addedShows++
					showIDs = append(showIDs, mediaID)
				}
			} else {
				if mediaType == "movie" {
					dupMovies++
				} else {
					dupShows++
				}
				maxRank--
			}
		}

		for _, m := range req.Movies {
			addFavorite(m.IDs.TMDB, "movie")
		}
		for _, s := range req.Shows {
			addFavorite(s.IDs.TMDB, "show")
		}

		if addedMovies+addedShows > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			if err := outbox.PublishEvent(tx, outbox.EventFavoriteAdded, userID, map[string]any{
				"added": addedMovies + addedShows,
			}); err != nil {
				return nil, err
			}
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{
		"added":      fiber.Map{"movies": addedMovies, "shows": addedShows},
		"not_found":  fiber.Map{"movies": make([]interface{}, 0), "shows": make([]interface{}, 0)},
		"duplicates": fiber.Map{"movies": dupMovies, "shows": dupShows},
	})
}

func handleRemoveFavorites(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req struct {
		Movies []historyMovie `json:"movies,omitempty"`
		Shows  []historyShow  `json:"shows,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "invalid JSON body")
	}

	deleted := 0
	var movieIDs []int64
	var showIDs []int64

	dbErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()

		for _, m := range req.Movies {
			_, mediaID, err := resolveByTMDB(c, tx, m.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "movie", mediaID).
				Delete(&database.UserFavorite{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				movieIDs = append(movieIDs, mediaID)
			}
		}
		for _, s := range req.Shows {
			_, mediaID, err := resolveByTMDB(c, tx, s.IDs.TMDB)
			if err != nil {
				continue
			}
			result := tx.Where("user_id = ? AND media_type = ? AND media_id = ?", userID, "show", mediaID).
				Delete(&database.UserFavorite{})
			if result.RowsAffected > 0 {
				deleted += int(result.RowsAffected)
				showIDs = append(showIDs, mediaID)
			}
		}

		if deleted > 0 {
			queueUserInvalidation(cacheTx, userID)
			for _, id := range movieIDs {
				queueMediaInvalidation(cacheTx, "movie", id)
			}
			for _, id := range showIDs {
				queueMediaInvalidation(cacheTx, "show", id)
			}
			// No event: the webhook catalog has no "favorite.removed", and
			// publishing "favorite.added" on a removal would be misleading.
		}

		return cacheTx, nil
	})
	if dbErr != nil {
		return response.InternalError(c)
	}

	return c.JSON(fiber.Map{"deleted": deleted})
}

// ============================================================================
// RESPONSE BUILDERS
// ============================================================================

type historyResponse struct {
	ID        int64       `json:"id"`
	MediaType string      `json:"media_type"`
	MediaID   int64       `json:"media_id"`
	SeasonID  *int64      `json:"season_id,omitempty"`
	EpisodeID *int64      `json:"episode_id,omitempty"`
	WatchedAt string      `json:"watched_at"`
	Source    string      `json:"source,omitempty"`
	Media     interface{} `json:"media,omitempty"`
}

func buildHistoryResponse(h *database.UserHistory) historyResponse {
	resp := historyResponse{
		ID:        h.ID,
		MediaType: h.MediaType,
		MediaID:   h.MediaID,
		SeasonID:  h.SeasonID,
		EpisodeID: h.EpisodeID,
		WatchedAt: h.WatchedAt.Format(time.RFC3339),
		Source:    h.Source,
	}
	return resp
}

type collectionResponse struct {
	ID          int64  `json:"id"`
	MediaType   string `json:"media_type"`
	MediaID     int64  `json:"media_id"`
	CollectedAt string `json:"collected_at"`
}

func buildCollectionResponses(items []database.UserCollection) []collectionResponse {
	results := make([]collectionResponse, 0, len(items))
	for _, i := range items {
		results = append(results, collectionResponse{
			ID: i.ID, MediaType: i.MediaType, MediaID: i.MediaID,
			CollectedAt: i.CollectedAt.Format(time.RFC3339),
		})
	}
	if results == nil {
		results = make([]collectionResponse, 0)
	}
	return results
}

type watchlistResponse struct {
	ID        int64   `json:"id"`
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	Rank      int     `json:"rank"`
	Notes     *string `json:"notes,omitempty"`
	ListedAt  string  `json:"listed_at"`
}

func buildWatchlistResponses(items []database.UserWatchlist) []watchlistResponse {
	results := make([]watchlistResponse, 0, len(items))
	for _, i := range items {
		results = append(results, watchlistResponse{
			ID: i.ID, MediaType: i.MediaType, MediaID: i.MediaID,
			Rank: i.Rank, Notes: i.Notes,
			ListedAt: i.ListedAt.Format(time.RFC3339),
		})
	}
	if results == nil {
		results = make([]watchlistResponse, 0)
	}
	return results
}

type ratingResponse struct {
	ID        int64  `json:"id"`
	MediaType string `json:"media_type"`
	MediaID   int64  `json:"media_id"`
	Rating    int    `json:"rating"`
	RatedAt   string `json:"rated_at"`
}

func buildRatingResponses(items []database.UserRating) []ratingResponse {
	results := make([]ratingResponse, 0, len(items))
	for _, i := range items {
		results = append(results, ratingResponse{
			ID: i.ID, MediaType: i.MediaType, MediaID: i.MediaID,
			Rating: i.Rating, RatedAt: i.RatedAt.Format(time.RFC3339),
		})
	}
	if results == nil {
		results = make([]ratingResponse, 0)
	}
	return results
}

type favoriteResponse struct {
	ID          int64  `json:"id"`
	MediaType   string `json:"media_type"`
	MediaID     int64  `json:"media_id"`
	Rank        int    `json:"rank"`
	FavoritedAt string `json:"favorited_at"`
}

func buildFavoriteResponses(items []database.UserFavorite) []favoriteResponse {
	results := make([]favoriteResponse, 0, len(items))
	for _, i := range items {
		results = append(results, favoriteResponse{
			ID: i.ID, MediaType: i.MediaType, MediaID: i.MediaID,
			Rank: i.Rank, FavoritedAt: i.FavoritedAt.Format(time.RFC3339),
		})
	}
	if results == nil {
		results = make([]favoriteResponse, 0)
	}
	return results
}

// ============================================================================
// HELPERS
// ============================================================================

// resolveByTMDB resolves a TMDB ID to a canonical (mediaType, mediaID).
func resolveByTMDB(c *fiber.Ctx, db *gorm.DB, tmdbID int) (string, int64, error) {
	c.Set("id_type", "tmdb")
	c.Set("id_type_param", strconv.Itoa(tmdbID))
	// Direct TMDB lookup via external_ids
	var ext database.ExternalID
	if err := db.Where("source = ? AND external_id = ?", "tmdb", strconv.Itoa(tmdbID)).First(&ext).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, fmt.Errorf("not found")
		}
		return "", 0, err
	}
	// Verify the media exists
	found, err := verifyMediaExists(db, ext.MediaType, ext.MediaID)
	if err != nil || !found {
		return "", 0, fmt.Errorf("not found")
	}
	return ext.MediaType, ext.MediaID, nil
}

func verifyMediaExists(db *gorm.DB, mediaType string, mediaID int64) (bool, error) {
	var count int64
	switch mediaType {
	case "movie":
		if err := db.Model(&database.Movie{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
			return false, err
		}
	case "show":
		if err := db.Model(&database.Show{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
			return false, err
		}
	case "season":
		if err := db.Model(&database.Season{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
			return false, err
		}
	case "episode":
		if err := db.Model(&database.Episode{}).Where("id = ?", mediaID).Count(&count).Error; err != nil {
			return false, err
		}
	default:
		return false, nil
	}
	return count > 0, nil
}

// parseWatchedAt handles "released", "unknown", and explicit timestamps.
// An explicit timestamp that isn't valid RFC3339 is an error — it must never
// silently become "now" (that would corrupt watch history).
func parseWatchedAt(val string, now time.Time, airedAt *database.Date) (time.Time, error) {
	switch val {
	case "released":
		if airedAt != nil && !airedAt.IsZero() {
			return airedAt.Time, nil
		}
		return now, nil
	case "", "unknown":
		return now, nil
	default:
		if t, err := time.Parse(time.RFC3339, val); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid watched_at %q", val)
	}
}

func insertHistory(db *gorm.DB, userID, mediaType string, mediaID int64, seasonID, episodeID *int64, watchedAt time.Time) (added, dup bool) {
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&database.UserHistory{
		UserID: userID, MediaType: mediaType, MediaID: mediaID,
		SeasonID: seasonID, EpisodeID: episodeID, WatchedAt: watchedAt,
	})
	if result.RowsAffected > 0 {
		return true, false
	}
	return false, true // duplicate
}

func findSeason(db *gorm.DB, showID int64, seasonNum int) (*database.Season, bool) {
	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).First(&season).Error; err != nil {
		return nil, false
	}
	return &season, true
}

func findEpisode(db *gorm.DB, seasonID int64, epNum int) (*database.Episode, bool) {
	var ep database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", seasonID, epNum).First(&ep).Error; err != nil {
		return nil, false
	}
	return &ep, true
}

func findAllEpisodesInSeason(db *gorm.DB, seasonID int64) []database.Episode {
	var episodes []database.Episode
	db.Where("season_id = ?", seasonID).Order("episode_number ASC").Find(&episodes)
	return episodes
}

func findAllEpisodesInShow(db *gorm.DB, showID int64) []database.Episode {
	var episodes []database.Episode
	db.Where("show_id = ?", showID).Order("season_id ASC, episode_number ASC").Find(&episodes)
	return episodes
}

// ============================================================================
// CACHE INVALIDATION + OUTBOX (engineering-patterns.md)
// ============================================================================

// mediaRef identifies a media entity for cache invalidation.
type mediaRef struct {
	mediaType string
	mediaID   int64
}

// queueResolvedInvalidation bumps the version counter for a resolved media
// entity. Episodes additionally bump their show's episode slot.
func queueResolvedInvalidation(tx *gorm.DB, cacheTx *cache.CacheTransaction, mediaType string, mediaID int64) {
	if mediaType == "episode" {
		var ep database.Episode
		if err := tx.First(&ep, mediaID).Error; err == nil {
			queueEpisodeInvalidations(tx, cacheTx, ep)
			return
		}
	}
	queueMediaInvalidation(cacheTx, mediaType, mediaID)
}

// queueUserInvalidation bumps the user's version counter so all cached
// user-scoped responses (history, watchlist, collection, ratings, favorites)
// are invalidated in O(1).
func queueUserInvalidation(cacheTx *cache.CacheTransaction, userID string) {
	vk := redis.BuildUserVersionKey(userID)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"user_id":      userID,
		"version_key":  vk,
		"version_only": true,
	})
}

// queueMediaInvalidation bumps a media entity's version counter (O(1)).
func queueMediaInvalidation(cacheTx *cache.CacheTransaction, mediaType string, mediaID int64) {
	vk := redis.BuildMediaVersionKey(mediaType, mediaID, nil, nil)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"media_id":     mediaID,
		"media_type":   mediaType,
		"version_key":  vk,
		"version_only": true,
	})
}

// queueEpisodeInvalidations bumps the episode entity version and the show's
// slot for that specific episode, so both the episode page and the show's
// episode slot are invalidated without blowing away the whole show.
func queueEpisodeInvalidations(tx *gorm.DB, cacheTx *cache.CacheTransaction, ep database.Episode) {
	queueMediaInvalidation(cacheTx, "episode", ep.ID)

	seasonNum := 0
	var season database.Season
	if err := tx.Select("season_number").Where("id = ?", ep.SeasonID).First(&season).Error; err == nil {
		seasonNum = season.SeasonNumber
	}
	epNum := ep.EpisodeNumber
	vk := redis.BuildMediaVersionKey("show", ep.ShowID, &seasonNum, &epNum)
	cacheTx.AddInvalidation(vk, map[string]interface{}{
		"media_id":     ep.ShowID,
		"media_type":   "show",
		"version_key":  vk,
		"version_only": true,
	})
}

// ============================================================================
// ERROR HELPERS (match project conventions)
// ============================================================================

// errIncompleteReorder marks a watchlist reorder whose payload is not the
// complete set of the user's items (missing/foreign/duplicate ids).
var errIncompleteReorder = errors.New("reorder payload must be the complete watchlist")

// validateReorder reports whether ids is exactly the set of ownedIDs — same
// length, every id owned, no duplicates (Trakt reorder semantics).
func validateReorder(ids, ownedIDs []int64) error {
	if len(ids) != len(ownedIDs) {
		return errIncompleteReorder
	}
	owned := make(map[int64]bool, len(ownedIDs))
	for _, id := range ownedIDs {
		owned[id] = true
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if !owned[id] || seen[id] {
			return errIncompleteReorder
		}
		seen[id] = true
	}
	return nil
}
