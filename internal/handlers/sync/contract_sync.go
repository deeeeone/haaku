package sync

import (
	"strconv"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped sync reads.
//
// Trakt carries the media type (and, for some collections, a sort and a rating
// filter) as path segments; our /v1/sync/{collection} routes take them as query
// params and return a {items,page,limit,row_count} envelope. These routes pin
// the segments and emit the contract's bare array of rows.
//
// A media type the backing table cannot store is answered as an empty array
// only where no such row could ever exist (episode collection rows); the
// watchlist's seasons/episodes types are not storable at all (#71), so that
// path stays divergent in the manifest rather than pretending.

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/sync/collection/{type}", Summary: "Collection rows for one media type (contract path)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Collection items", 401: "Unauthorized", 400: "Invalid type"}},
		{Method: "GET", Path: "/v1/sync/favorites/{type}/{sort_by}/{sort_how}", Summary: "Favorites for one media type, sorted (contract path)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Favorite items", 401: "Unauthorized", 400: "Invalid type or sort"}},
		{Method: "GET", Path: "/v1/sync/ratings/{type}/{rating}", Summary: "Ratings for one media type filtered by rating (contract path)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Rating items", 401: "Unauthorized", 400: "Invalid type or rating"}},
		{Method: "GET", Path: "/v1/sync/watchlist/{type}/{sort_by}/{sort_how}", Summary: "Watchlist for one media type, sorted (contract path)", Tags: []string{"Sync"}, Auth: "required", StatusCodes: map[int]string{200: "Watchlist items", 401: "Unauthorized", 400: "Invalid type or sort"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterContractRoutes mounts the contract-shaped sync reads. routes.go calls
// it after sync.RegisterRoutes, so every static per-type route is registered
// first and a :type segment cannot capture "movies"/"shows".
func RegisterContractRoutes(api fiber.Router) {
	api.Get("/sync/collection/:type", auth.AuthRequired(), handleContractCollection)
	api.Get("/sync/favorites/:type/:sort_by/:sort_how", auth.AuthRequired(), handleContractFavorites)
	api.Get("/sync/ratings/:type/:rating", auth.AuthRequired(), handleContractRatings)
	api.Get("/sync/watchlist/:type/:sort_by/:sort_how", auth.AuthRequired(), handleContractWatchlist)
}

// syncType maps Trakt's plural type segment onto our media_type.
var syncType = map[string]string{
	"movies": "movie", "shows": "show", "seasons": "season", "episodes": "episode",
}

func wantSyncType(c *fiber.Ctx, allowed ...string) (string, error) {
	mt, ok := syncType[c.Params("type")]
	if !ok {
		return "", response.BadRequest(c, "invalid type: must be one of movies, shows, seasons, episodes")
	}
	for _, a := range allowed {
		if mt == a {
			return mt, nil
		}
	}
	return "", response.BadRequest(c, "unsupported type for this collection")
}

// handleContractCollection serves /v1/sync/collection/{type}.
func handleContractCollection(c *fiber.Ctx) error {
	mt, err := wantSyncType(c, "movie", "show", "episode")
	if err != nil {
		return err
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ? AND media_type = ?", userID, mt)
	var total int64
	query.Model(&database.UserCollection{}).Count(&total)

	var rows []database.UserCollection
	if err := query.Order("collected_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		item, ok := collectionItem(db, &rows[i])
		if ok {
			out = append(out, item)
		}
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

func collectionItem(db *gorm.DB, row *database.UserCollection) (map[string]any, bool) {
	item := map[string]any{
		"type":         row.MediaType,
		"collected_at": contractshape.Timestamp(row.CollectedAt),
		"updated_at":   contractshape.Timestamp(row.CollectedAt),
	}
	switch row.MediaType {
	case "movie":
		var m database.Movie
		if err := db.First(&m, row.MediaID).Error; err != nil {
			return nil, false
		}
		item["movie"] = contractshape.Movie(&m)
	case "show":
		var s database.Show
		if err := db.First(&s, row.MediaID).Error; err != nil {
			return nil, false
		}
		item["show"] = contractshape.Show(&s)
	default:
		return nil, false
	}
	return item, true
}

// handleContractFavorites serves /v1/sync/favorites/{type}/{sort_by}/{sort_how}.
func handleContractFavorites(c *fiber.Ctx) error {
	mt, err := wantSyncType(c, "movie", "show")
	if err != nil {
		return err
	}
	order, oerr := ContractMediaOrder(c, "favorited_at")
	if oerr != nil {
		return oerr
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ? AND media_type = ?", userID, mt)
	var total int64
	query.Model(&database.UserFavorite{}).Count(&total)

	var rows []database.UserFavorite
	if err := query.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"rank":      rows[i].Rank,
			"id":        rows[i].MediaID,
			"listed_at": contractshape.Timestamp(rows[i].FavoritedAt),
			"type":      rows[i].MediaType,
		})
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// handleContractWatchlist serves /v1/sync/watchlist/{type}/{sort_by}/{sort_how}.
func handleContractWatchlist(c *fiber.Ctx) error {
	mt, err := wantSyncType(c, "movie", "show")
	if err != nil {
		return err
	}
	order, oerr := ContractMediaOrder(c, "listed_at")
	if oerr != nil {
		return oerr
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ? AND media_type = ?", userID, mt)
	var total int64
	query.Model(&database.UserWatchlist{}).Count(&total)

	var rows []database.UserWatchlist
	if err := query.Order(order).Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"rank":      rows[i].Rank,
			"id":        rows[i].MediaID,
			"listed_at": contractshape.Timestamp(rows[i].ListedAt),
			"type":      rows[i].MediaType,
		})
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// handleContractRatings serves /v1/sync/ratings/{type}/{rating}.
func handleContractRatings(c *fiber.Ctx) error {
	mt, err := wantSyncType(c, "movie", "show", "season", "episode")
	if err != nil {
		return err
	}
	rating, rerr := strconv.Atoi(c.Params("rating"))
	if rerr != nil || rating < 1 || rating > 10 {
		return response.BadRequest(c, "rating must be an integer from 1 to 10")
	}
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	page, limit := response.ParsePagination(c, 25)
	query := db.Where("user_id = ? AND media_type = ? AND rating = ?", userID, mt, rating)
	var total int64
	query.Model(&database.UserRating{}).Count(&total)

	var rows []database.UserRating
	if err := query.Order("rated_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, map[string]any{
			"rated_at": contractshape.Timestamp(rows[i].RatedAt),
			"rating":   rows[i].Rating,
			"type":     rows[i].MediaType,
		})
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// contractMediaOrder maps the contract's sort_by/sort_how segments onto SQL
// ordering. Only values the store can actually order by are accepted; anything
// else is a 400 rather than a silently ignored segment.
func ContractMediaOrder(c *fiber.Ctx, timeCol string) (string, error) {
	sortBy := c.Params("sort_by")
	how := c.Params("sort_how")
	if how != "asc" && how != "desc" {
		return "", response.BadRequest(c, "sort_how must be asc or desc")
	}

	movie := func(col string) string {
		return "(SELECT m." + col + " FROM movies m WHERE m.id = media_id)"
	}
	show := func(col string) string {
		return "(SELECT s." + col + " FROM shows s WHERE s.id = media_id)"
	}
	switch sortBy {
	case "rank":
		return "rank " + how + ", id " + how, nil
	case "added", "listed":
		return timeCol + " " + how + ", id " + how, nil
	case "title":
		return "COALESCE(" + movie("title") + "," + show("title") + ") " + how + ", media_type " + how + ", media_id " + how, nil
	case "popularity":
		return "COALESCE(" + movie("popularity") + "," + show("popularity") + ") " + how, nil
	case "released":
		return "COALESCE(" + movie("released") + "," + show("first_aired") + ") " + how, nil
	case "runtime":
		return "COALESCE(" + movie("runtime") + ",0) " + how, nil
	case "votes":
		return "COALESCE(" + movie("rating_count") + "," + show("rating_count") + ") " + how, nil
	case "percentage":
		return "COALESCE(" + movie("rating_avg") + "," + show("rating_avg") + ") " + how, nil
	case "random":
		return "RANDOM()", nil
	default:
		return "", response.BadRequest(c, "invalid sort_by")
	}
}
