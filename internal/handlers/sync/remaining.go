package sync

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/cache"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	routes := []openapi.RouteDef{
		{Method: "DELETE", Path: "/v1/sync/playback/{id}", Summary: "Remove a playback item (contract path)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{204: "Removed", 401: "Unauthorized", 404: "Not found"}},
		{Method: "GET", Path: "/v1/sync/collection/media", Summary: "Collection media (movies, shows, episodes) with media objects", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Collection items", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/collection/minimal/movies", Summary: "Minimal movie collection (id -> collected_at)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Minimal movie collection", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/collection/minimal/shows", Summary: "Minimal show collection (show -> season -> episode -> collected_at)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Minimal show collection", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/collection/minimal/episodes", Summary: "Minimal episode collection (id -> collected_at)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Minimal episode collection", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/progress/up_next", Summary: "Up next progress across the caller's history", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Up next progress", 401: "Unauthorized", 400: "Unsupported sort"}},
		{Method: "GET", Path: "/v1/sync/progress/up_next_nitro", Summary: "Up next progress with intent filtering", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Up next progress (nitro)", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/progress/watched", Summary: "Watched progress across the caller's shows", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Watched progress", 401: "Unauthorized", 400: "Unsupported filter"}},
		{Method: "GET", Path: "/v1/sync/watched/{type}", Summary: "Watched list for one media type", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Watched items", 401: "Unauthorized", 400: "Invalid type"}},
		{Method: "POST", Path: "/v1/sync/favorites/reorder", Summary: "Reorder favorite items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Reorder result", 401: "Unauthorized"}},
		{Method: "POST", Path: "/v1/sync/watchlist/reorder", Summary: "Reorder watchlist items", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Reorder result", 401: "Unauthorized"}},
		{Method: "PUT", Path: "/v1/sync/favorites/{list_item_id}", Summary: "Update a favorite item", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{204: "Updated", 401: "Unauthorized", 404: "Not found", 400: "Unsupported field"}},
		{Method: "PUT", Path: "/v1/sync/watchlist", Summary: "Update watchlist (add missing items)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Update result", 401: "Unauthorized", 400: "Unsupported media type"}},
		{Method: "PUT", Path: "/v1/sync/watchlist/{list_item_id}", Summary: "Update a watchlist item", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{204: "Updated", 401: "Unauthorized", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRemainingRoutes mounts the issue-#106 sync surface. Call it after
// sync.RegisterRoutes (so /watchlist/reorder is registered before
// /watchlist/:list_item_id) and before sync.RegisterContractRoutes (so the
// static /collection/media is registered before /collection/:type).
func RegisterRemainingRoutes(api fiber.Router) {
	sync := api.Group("/sync")
	mw := auth.AuthRequired()

	// Collection reads. Registration order matters: the static
	// /collection/media must precede the parameterised /collection/:type that
	// RegisterContractRoutes adds, or ":type" would swallow "media".
	sync.Get("/collection/media", mw, handleCollectionMedia)
	sync.Get("/collection/minimal/movies", mw, handleCollectionMinimalMovies)
	sync.Get("/collection/minimal/shows", mw, handleCollectionMinimalShows)
	sync.Get("/collection/minimal/episodes", mw, handleCollectionMinimalEpisodes)

	// Watched list, per media type.
	sync.Get("/watched/:type", mw, handleWatchedType)

	// Progress reads.
	sync.Get("/progress/up_next", mw, handleUpNext)
	sync.Get("/progress/up_next_nitro", mw, handleUpNextNitro)
	sync.Get("/progress/watched", mw, handleProgressWatched)

	// Writes.
	sync.Post("/favorites/reorder", mw, handleReorderFavorites)
	sync.Post("/watchlist/reorder", mw, handleReorderWatchlistContract)
	sync.Put("/watchlist", mw, handleUpdateWatchlist)
	sync.Put("/favorites/:list_item_id", mw, handleUpdateFavoriteItem)
	// Registered after /watchlist/reorder (above and in RegisterRoutes) so the
	// static "reorder" segment is not swallowed by this parameter.
	sync.Put("/watchlist/:list_item_id", mw, handleUpdateWatchlistItem)

	// Playback delete at the contract's single-segment path.
	sync.Delete("/playback/:id", mw, handleDeletePlaybackContract)
}

// ============================================================================
// DELETE /sync/playback/{id}
// ============================================================================

// handleDeletePlaybackContract removes a playback entry by the id a client got
// from GET /sync/playback (our playback rows expose media_id as the item id).
// A 404 is returned when the caller owns no such entry, matching the contract.
// The path carries no media_type, so an id that matches rows of more than one
// media_type is answered 400 (ambiguous) rather than deleting several kinds.
func handleDeletePlaybackContract(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return response.BadRequest(c, "invalid playback id")
	}

	var deleted int64
	txErr := cache.WithCacheTransaction(func(tx *gorm.DB) (*cache.CacheTransaction, error) {
		cacheTx := cache.NewCacheTransaction()
		// The path carries a single id and no media_type. The same id can name
		// rows of more than one media_type (movie and episode ids come from
		// separate sequences), so when the caller's matches span several types
		// the id is ambiguous: reject rather than silently deleting a kind the
		// caller did not name.
		var matchedTypes []string
		if err := tx.Model(&database.UserPlayback{}).
			Where("user_id = ? AND media_id = ?", userID, id).
			Distinct().Pluck("media_type", &matchedTypes).Error; err != nil {
			return nil, err
		}
		if len(matchedTypes) > 1 {
			return nil, errPlaybackAmbiguous
		}
		res := tx.Where("user_id = ? AND media_id = ?", userID, id).Delete(&database.UserPlayback{})
		if res.Error != nil {
			return nil, res.Error
		}
		deleted = res.RowsAffected
		if deleted == 0 {
			return nil, errPlaybackEntryNotFound
		}
		queueUserInvalidation(cacheTx, userID)
		return cacheTx, nil
	})
	if txErr != nil {
		switch txErr {
		case errPlaybackEntryNotFound:
			return response.NotFound(c, "playback position not found")
		case errPlaybackAmbiguous:
			return response.BadRequest(c, "playback id matches more than one media type; the contract path carries no media_type to disambiguate")
		}
		return response.InternalError(c)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

var errPlaybackEntryNotFound = gorm.ErrRecordNotFound

// errPlaybackAmbiguous marks a playback id that matches rows of more than one
// media_type, which DELETE /sync/playback/{id} cannot address.
var errPlaybackAmbiguous = errors.New("playback id matches more than one media type")

// ============================================================================
// GET /sync/collection/media
// ============================================================================

func handleCollectionMedia(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	// available_on narrows by streaming availability, which we do not store.
	if c.Query("available_on") != "" {
		return response.BadRequest(c, "available_on filtering is not supported")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)

	var total int64
	if err := db.Model(&database.UserCollection{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}

	var rows []database.UserCollection
	if err := db.Where("user_id = ?", userID).
		Order("collected_at DESC, id DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		item, ok := collectionMediaItem(db, &rows[i])
		if ok {
			out = append(out, item)
		}
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// collectionMediaItem renders one user_collection row in the contract's
// movie/show/episode media-collection shape. It returns false when the row
// points at media that no longer exists (never emit a media-less item).
func collectionMediaItem(db *gorm.DB, row *database.UserCollection) (map[string]any, bool) {
	ts := contractshape.Timestamp(row.CollectedAt)
	switch row.MediaType {
	case "movie":
		var m database.Movie
		if err := db.First(&m, row.MediaID).Error; err != nil {
			return nil, false
		}
		return map[string]any{
			"type":         "movie",
			"movie":        contractshape.Movie(&m),
			"collected_at": ts,
			"updated_at":   ts,
		}, true
	case "show":
		var s database.Show
		if err := db.First(&s, row.MediaID).Error; err != nil {
			return nil, false
		}
		return map[string]any{
			"type":              "show",
			"show":              contractshape.Show(&s),
			"last_collected_at": ts,
			"last_updated_at":   ts,
			"seasons":           []any{},
		}, true
	case "episode":
		var ep database.Episode
		if err := db.First(&ep, row.MediaID).Error; err != nil {
			return nil, false
		}
		seasonNo, show, ok := episodeSeasonShow(db, &ep)
		if !ok {
			return nil, false
		}
		return map[string]any{
			"type":         "episode",
			"episode":      contractshape.Episode(&ep, seasonNo),
			"show":         contractshape.Show(show),
			"collected_at": ts,
			"updated_at":   ts,
		}, true
	default:
		return nil, false
	}
}

// episodeSeasonShow resolves an episode's season number and parent show.
func episodeSeasonShow(db *gorm.DB, ep *database.Episode) (int, *database.Show, bool) {
	var season database.Season
	if err := db.First(&season, ep.SeasonID).Error; err != nil {
		return 0, nil, false
	}
	var show database.Show
	if err := db.First(&show, ep.ShowID).Error; err != nil {
		return 0, nil, false
	}
	return season.SeasonNumber, &show, true
}

// ============================================================================
// GET /sync/collection/minimal/{movies,shows,episodes}
// ============================================================================

func handleCollectionMinimalMovies(c *fiber.Ctx) error {
	if c.Query("available_on") != "" {
		return response.BadRequest(c, "available_on filtering is not supported")
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}
	var rows []database.UserCollection
	if err := db.Where("user_id = ? AND media_type = ?", userID, "movie").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[strconv.FormatInt(r.MediaID, 10)] = contractshape.Timestamp(r.CollectedAt)
	}
	return c.JSON(out)
}

func handleCollectionMinimalEpisodes(c *fiber.Ctx) error {
	if c.Query("available_on") != "" {
		return response.BadRequest(c, "available_on filtering is not supported")
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}
	var rows []database.UserCollection
	if err := db.Where("user_id = ? AND media_type = ?", userID, "episode").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[strconv.FormatInt(r.MediaID, 10)] = contractshape.Timestamp(r.CollectedAt)
	}
	return c.JSON(out)
}

// handleCollectionMinimalShows builds the contract's
// {show: {season: {episode: collected_at}}} map from episode-level collection
// rows. Show-level collection rows (what POST /sync/collection writes) carry no
// per-episode date and are not representable here; see the manifest's compat
// note and issue #71.
func handleCollectionMinimalShows(c *fiber.Ctx) error {
	if c.Query("available_on") != "" {
		return response.BadRequest(c, "available_on filtering is not supported")
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var rows []database.UserCollection
	if err := db.Where("user_id = ? AND media_type = ?", userID, "episode").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make(map[string]map[string]map[string]string)
	for _, r := range rows {
		var ep database.Episode
		if err := db.First(&ep, r.MediaID).Error; err != nil {
			continue
		}
		seasonNo, _, ok := episodeSeasonShow(db, &ep)
		if !ok {
			continue
		}
		showKey := strconv.FormatInt(ep.ShowID, 10)
		if out[showKey] == nil {
			out[showKey] = make(map[string]map[string]string)
		}
		seasonKey := strconv.Itoa(seasonNo)
		if out[showKey][seasonKey] == nil {
			out[showKey][seasonKey] = make(map[string]string)
		}
		out[showKey][seasonKey][strconv.Itoa(ep.EpisodeNumber)] = contractshape.Timestamp(r.CollectedAt)
	}
	return c.JSON(out)
}
