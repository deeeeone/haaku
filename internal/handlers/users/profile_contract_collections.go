package users

import (
	"errors"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped favorites and watchlist reads (issue #103). They read
// user_favorites / user_watchlist / comments and run the same PrivacyFilter as
// the rest of the profile surface.
//
// /users/{id}/favorites/comments/{sort} is deliberately NOT registered: haaku
// stores comment likes and reactions but no "favorite a comment" relation, so
// the row stays out-of-scope rather than serving a fabricated empty array.

func registerContractCollectionReads(users fiber.Router) {
	users.Get("/:id/favorites/movies/:sort", auth.PrivacyFilter("id"), handleContractFavorites("movie"))
	users.Get("/:id/favorites/shows/:sort", auth.PrivacyFilter("id"), handleContractFavorites("show"))
	users.Get("/:id/favorites/media/:sort", auth.PrivacyFilter("id"), handleContractFavorites(""))
	users.Get("/:id/favorites/comments/:sort", auth.PrivacyFilter("id"), handleContractFavoritesComments)
	users.Get("/:id/favorites/:type/:sort_by/:sort_how", auth.PrivacyFilter("id"), handleContractFavoritesTyped)

	users.Get("/:id/watchlist/movies/:sort", auth.PrivacyFilter("id"), handleContractWatchlist("movie"))
	users.Get("/:id/watchlist/shows/:sort", auth.PrivacyFilter("id"), handleContractWatchlist("show"))
	users.Get("/:id/watchlist/movie,show/:sort", auth.PrivacyFilter("id"), handleContractWatchlist(""))
	users.Get("/:id/watchlist/comments/:sort", auth.PrivacyFilter("id"), handleContractWatchlistComments)
	registerContractSmartListDraftRoutes(users)
}

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/{id}/favorites/movies/{sort}", Summary: "A user's favorite movies (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Favorites", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/favorites/shows/{sort}", Summary: "A user's favorite shows (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Favorites", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/favorites/media/{sort}", Summary: "A user's favorite media, all types (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Favorites", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/favorites/comments/{sort}", Summary: "Comments on a user's favorites (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/favorites/{type}/{sort_by}/{sort_how}", Summary: "A user's favorites for one type, sorted (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Favorites", 400: "Invalid type or sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watchlist/movies/{sort}", Summary: "A user's movie watchlist, sorted (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watchlist", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watchlist/shows/{sort}", Summary: "A user's show watchlist, sorted (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watchlist", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watchlist/movie,show/{sort}", Summary: "A user's movie and show watchlist, sorted (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watchlist", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watchlist/comments/{sort}", Summary: "Comments on items in a user's watchlist (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Comments", 400: "Invalid sort", 403: "Private profile", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// mediaListOrder maps a favorites/watchlist sort onto SQL. timeCol is the
// collection's own timestamp column (favorited_at / listed_at). Only the values
// we can actually order by are accepted; anything else is a 400 rather than a
// silently ignored sort.
func mediaListOrder(sortBy, how, timeCol string) (string, error) {
	if how == "" {
		how = "asc"
	}
	if how != "asc" && how != "desc" {
		return "", errInvalidSort
	}
	title := `COALESCE(
		(SELECT m.title FROM movies m WHERE m.id = media_id AND media_type = 'movie'),
		(SELECT s.title FROM shows s WHERE s.id = media_id AND media_type = 'show')
	)`
	switch sortBy {
	case "rank":
		return "rank " + how + ", id " + how, nil
	case "added", "listed":
		return timeCol + " " + how + ", id " + how, nil
	case "title":
		return title + " " + how + ", media_type " + how + ", media_id " + how, nil
	case "released":
		return `COALESCE(
			(SELECT m.released FROM movies m WHERE m.id = media_id AND media_type = 'movie'),
			(SELECT s.first_aired FROM shows s WHERE s.id = media_id AND media_type = 'show')
		) ` + how + `, media_type ` + how + `, media_id ` + how, nil
	default:
		return "", errInvalidSort
	}
}

// collectionSort resolves a `{sort}` path segment with the contract's optional
// sort_by/sort_how query overrides. The query value wins when present, since
// Trakt lets a client re-sort a sorted path.
func collectionSort(c *fiber.Ctx) (sortBy, how string) {
	sortBy = c.Params("sort")
	how = "asc"
	if q := c.Query("sort_by"); q != "" {
		sortBy = q
	}
	if q := c.Query("sort_how"); q != "" {
		how = q
	}
	return sortBy, how
}

// handleContractFavorites serves /favorites/{movies,shows,media}/{sort}.
// kind "" means all storable favorite types.
func handleContractFavorites(kind string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := validateExtended(c); err != nil {
			return err
		}
		target, db, ok := profileTarget(c)
		if !ok {
			return nil
		}
		sortBy, how := collectionSort(c)
		order, err := mediaListOrder(sortBy, how, "favorited_at")
		if err != nil {
			return response.BadRequest(c, "invalid sort")
		}
		page, limit := contractPage(c)

		items, total, err := readFavorites(c, db, target.ID, kind, order, page, limit)
		if err != nil {
			return err
		}
		return writeContractPage(c, items, page, limit, total, len(items))
	}
}

// handleContractFavoritesTyped serves /favorites/{type}/{sort_by}/{sort_how}.
func handleContractFavoritesTyped(c *fiber.Ctx) error {
	if err := validateExtended(c); err != nil {
		return err
	}
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	kind, ok := profileMediaType(c.Params("type"))
	if !ok {
		return response.BadRequest(c, "invalid type")
	}
	order, err := mediaListOrder(c.Params("sort_by"), c.Params("sort_how"), "favorited_at")
	if err != nil {
		return response.BadRequest(c, "invalid sort")
	}
	page, limit := contractPage(c)
	items, total, err := readFavorites(c, db, target.ID, kind, order, page, limit)
	if err != nil {
		return err
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

func readFavorites(c *fiber.Ctx, db *gorm.DB, userID, kind, order string, page, limit int) ([]map[string]any, int64, error) {
	q := db.Where("user_id = ?", userID)
	if kind != "" {
		q = q.Where("media_type = ?", kind)
	} else {
		q = q.Where("media_type IN ?", []string{"movie", "show", "season", "episode"})
	}
	var total int64
	if err := q.Model(&database.UserFavorite{}).Count(&total).Error; err != nil {
		return nil, 0, response.InternalError(c)
	}
	var rows []database.UserFavorite
	if err := q.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, response.InternalError(c)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		media, ok := contractMediaRow(db, rows[i].MediaType, rows[i].MediaID)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"rank":            rows[i].Rank,
			"id":              rows[i].MediaID,
			"listed_at":       contractshape.Timestamp(rows[i].FavoritedAt),
			"type":            rows[i].MediaType,
			"notes":           nil,
			rows[i].MediaType: media,
		})
	}
	return items, total, nil
}

// handleContractWatchlist serves /watchlist/{movies,shows,movie,show}/{sort}.
func handleContractWatchlist(kind string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := validateExtended(c); err != nil {
			return err
		}
		target, db, ok := profileTarget(c)
		if !ok {
			return nil
		}
		sortBy, how := collectionSort(c)
		order, err := mediaListOrder(sortBy, how, "listed_at")
		if err != nil {
			return response.BadRequest(c, "invalid sort")
		}
		page, limit := contractPage(c)

		q := db.Where("user_id = ?", target.ID)
		if kind != "" {
			q = q.Where("media_type = ?", kind)
		} else {
			q = q.Where("media_type IN ?", []string{"movie", "show"})
		}
		var total int64
		if err := q.Model(&database.UserWatchlist{}).Count(&total).Error; err != nil {
			return response.InternalError(c)
		}
		var rows []database.UserWatchlist
		if err := q.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}
		items := make([]map[string]any, 0, len(rows))
		for i := range rows {
			media, ok := contractMediaRow(db, rows[i].MediaType, rows[i].MediaID)
			if !ok {
				continue
			}
			var notes any
			if rows[i].Notes != nil {
				notes = *rows[i].Notes
			}
			items = append(items, map[string]any{
				"rank":            rows[i].Rank,
				"id":              rows[i].MediaID,
				"listed_at":       contractshape.Timestamp(rows[i].ListedAt),
				"type":            rows[i].MediaType,
				"notes":           notes,
				rows[i].MediaType: media,
			})
		}
		return writeContractPage(c, items, page, limit, total, len(items))
	}
}

// handleContractWatchlistComments serves /watchlist/comments/{sort}: comments
// on the movies/shows in the user's watchlist.
func handleContractWatchlistComments(c *fiber.Ctx) error {
	sort := c.Params("sort")
	var order string
	switch sort {
	case "newest":
		order = "created_at DESC, id DESC"
	case "oldest":
		order = "created_at ASC, id ASC"
	case "likes":
		order = "like_count DESC, id DESC"
	case "replies":
		order = "reply_count DESC, id DESC"
	default:
		return response.BadRequest(c, "sort must be one of newest, oldest, likes, replies")
	}

	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	page, limit := contractPage(c)

	var watchlist []database.UserWatchlist
	if err := db.Where("user_id = ? AND media_type IN ?", target.ID, []string{"movie", "show"}).
		Find(&watchlist).Error; err != nil {
		return response.InternalError(c)
	}
	movies := make([]int64, 0)
	shows := make([]int64, 0)
	for _, w := range watchlist {
		if w.MediaType == "movie" {
			movies = append(movies, w.MediaID)
		} else {
			shows = append(shows, w.MediaID)
		}
	}
	if len(movies) == 0 && len(shows) == 0 {
		return writeContractPage(c, []map[string]any{}, page, limit, 0, 0)
	}

	q := db.Model(&database.Comment{})
	switch {
	case len(movies) > 0 && len(shows) > 0:
		q = q.Where("(media_type = ? AND media_id IN ?) OR (media_type = ? AND media_id IN ?)", "movie", movies, "show", shows)
	case len(movies) > 0:
		q = q.Where("media_type = ? AND media_id IN ?", "movie", movies)
	default:
		q = q.Where("media_type = ? AND media_id IN ?", "show", shows)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var rows []database.Comment
	if err := q.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		if item, ok := contractComment(db, &rows[i]); ok {
			items = append(items, item)
		}
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// handleContractFavoritesComments serves /favorites/comments/{sort}: top-level
// comments on the movies and shows in the user's favorites. It is the same
// stored relation the watchlist-comments read uses, scoped to user_favorites.
func handleContractFavoritesComments(c *fiber.Ctx) error {
	sort := c.Params("sort")
	var order string
	switch sort {
	case "newest":
		order = "created_at DESC, id DESC"
	case "oldest":
		order = "created_at ASC, id ASC"
	case "likes":
		order = "like_count DESC, id DESC"
	case "replies":
		order = "reply_count DESC, id DESC"
	default:
		return response.BadRequest(c, "sort must be one of newest, oldest, likes, replies")
	}

	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	page, limit := contractPage(c)

	var favorites []database.UserFavorite
	if err := db.Where("user_id = ? AND media_type IN ?", target.ID, []string{"movie", "show"}).
		Find(&favorites).Error; err != nil {
		return response.InternalError(c)
	}
	movies := make([]int64, 0)
	shows := make([]int64, 0)
	for _, f := range favorites {
		if f.MediaType == "movie" {
			movies = append(movies, f.MediaID)
		} else {
			shows = append(shows, f.MediaID)
		}
	}
	if len(movies) == 0 && len(shows) == 0 {
		return writeContractPage(c, []map[string]any{}, page, limit, 0, 0)
	}

	q := db.Model(&database.Comment{}).Where("parent_id IS NULL")
	switch {
	case len(movies) > 0 && len(shows) > 0:
		q = q.Where("(media_type = ? AND media_id IN ?) OR (media_type = ? AND media_id IN ?)", "movie", movies, "show", shows)
	case len(movies) > 0:
		q = q.Where("media_type = ? AND media_id IN ?", "movie", movies)
	default:
		q = q.Where("media_type = ? AND media_id IN ?", "show", shows)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var rows []database.Comment
	if err := q.Order(order).Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		if item, ok := contractComment(db, &rows[i]); ok {
			items = append(items, item)
		}
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// errInvalidSort marks a sort value this collection cannot order by.
var errInvalidSort = errors.New("invalid sort")
