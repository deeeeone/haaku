package users

import (
	"errors"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped history / ratings / watched / activities reads (issue #103).
// Every route is a literal path segment, so it is registered before the generic
// per-type profile read and cannot be shadowed by it; each runs the same
// PrivacyFilter as the already-landed reads.

func registerContractMediaReads(users fiber.Router) {
	users.Get("/:id/history/movies", auth.PrivacyFilterSection("id", "history"), handleContractHistoryList("movie"))
	users.Get("/:id/history/shows", auth.PrivacyFilterSection("id", "history"), handleContractHistoryList("show"))
	users.Get("/:id/history/episodes", auth.PrivacyFilterSection("id", "history"), handleContractHistoryList("episode"))
	users.Get("/:id/history/movies/:item_id", auth.PrivacyFilterSection("id", "history"), handleContractHistoryItem("movie"))
	users.Get("/:id/history/shows/:item_id", auth.PrivacyFilterSection("id", "history"), handleContractHistoryItem("show"))
	users.Get("/:id/history/episodes/:item_id", auth.PrivacyFilterSection("id", "history"), handleContractHistoryItem("episode"))

	users.Get("/:id/ratings/movies", auth.PrivacyFilterSection("id", "ratings"), handleContractRatingsList("movie"))
	users.Get("/:id/ratings/shows", auth.PrivacyFilterSection("id", "ratings"), handleContractRatingsList("show"))
	users.Get("/:id/ratings/episodes", auth.PrivacyFilterSection("id", "ratings"), handleContractRatingsList("episode"))
	users.Get("/:id/ratings/:type/:rating", auth.PrivacyFilterSection("id", "ratings"), handleContractRatingsByValue)

	users.Get("/:id/watched/movies", auth.PrivacyFilterSection("id", "history"), handleContractWatchedMovies)
	users.Get("/:id/watched/shows", auth.PrivacyFilterSection("id", "history"), handleContractWatchedShows)
	users.Get("/:id/watched/:type", auth.PrivacyFilterSection("id", "history"), handleContractWatchedType)

	users.Get("/:id/:type/activities", auth.PrivacyFilterSection("id", "activity"), handleContractActivities)
}

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/users/{id}/history/movies", Summary: "A user's movie watch history (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 400: "Invalid extended value", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/shows", Summary: "A user's show/episode watch history (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 400: "Invalid extended value", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/episodes", Summary: "A user's episode watch history (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 400: "Invalid extended value", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/movies/{item_id}", Summary: "A user's history for one movie (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/shows/{item_id}", Summary: "A user's history for one show (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/history/episodes/{item_id}", Summary: "A user's history for one episode (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "History", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/ratings/movies", Summary: "A user's movie ratings (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Ratings", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/ratings/shows", Summary: "A user's show ratings (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Ratings", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/ratings/episodes", Summary: "A user's episode ratings (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Ratings", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/ratings/{type}/{rating}", Summary: "A user's ratings for one type at one rating value (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Ratings", 400: "Invalid type or rating", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watched/movies", Summary: "A user's watched movies (contract map shape)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watched movies", 400: "Invalid extended value", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watched/shows", Summary: "A user's watched shows (contract map shape)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watched shows", 400: "Invalid extended value", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/watched/{type}", Summary: "A user's watched media aggregated per item (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Watched items", 400: "Invalid type", 403: "Private profile", 404: "Not found"}},
		{Method: "GET", Path: "/v1/users/{id}/{type}/activities", Summary: "Recent activity of a user's friends/followers/following (contract path)", Tags: []string{"Users"}, Auth: "optional", StatusCodes: map[int]string{200: "Activities", 400: "Invalid type", 403: "Private profile", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// applyHistoryWindow applies the contract's start_at/end_at (RFC3339) and
// start_date/end_date (date) filters to a history query.
func applyHistoryWindow(q *gorm.DB, c *fiber.Ctx) *gorm.DB {
	parse := func(raw string, dateOnly bool) (time.Time, bool) {
		if raw == "" {
			return time.Time{}, false
		}
		if dateOnly {
			if t, err := time.Parse("2006-01-02", raw); err == nil {
				return t, true
			}
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t, true
		}
		return time.Time{}, false
	}
	if t, ok := parse(c.Query("start_at"), false); ok {
		q = q.Where("watched_at >= ?", t)
	}
	if t, ok := parse(c.Query("end_at"), false); ok {
		q = q.Where("watched_at <= ?", t)
	}
	if t, ok := parse(c.Query("start_date"), true); ok {
		q = q.Where("watched_at >= ?", t)
	}
	if t, ok := parse(c.Query("end_date"), true); ok {
		q = q.Where("watched_at <= ?", t.Add(24*time.Hour))
	}
	return q
}

// handleContractHistoryList serves the per-type history arrays.
func handleContractHistoryList(kind string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := validateExtended(c); err != nil {
			return err
		}
		target, db, ok := profileTarget(c)
		if !ok {
			return nil
		}
		page, limit := contractPage(c)

		q := db.Where("user_id = ?", target.ID)
		switch kind {
		case "movie":
			q = q.Where("media_type = ?", "movie")
		default: // show and episode histories are the stored episode rows
			q = q.Where("media_type = ?", "episode")
		}
		q = applyHistoryWindow(q, c)

		var total int64
		if err := q.Model(&database.UserHistory{}).Count(&total).Error; err != nil {
			return response.InternalError(c)
		}
		var rows []database.UserHistory
		if err := q.Order("watched_at DESC, id DESC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}

		items := make([]map[string]any, 0, len(rows))
		for i := range rows {
			row, ok := contractHistoryRow(db, &rows[i])
			if !ok {
				continue
			}
			if kind == "show" {
				row["type"] = "episode"
			}
			items = append(items, row)
		}
		return writeContractPage(c, items, page, limit, total, len(items))
	}
}

// handleContractHistoryItem serves one item's history rows.
func handleContractHistoryItem(kind string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		target, db, ok := profileTarget(c)
		if !ok {
			return nil
		}
		page, limit := contractPage(c)

		q := db.Where("user_id = ?", target.ID)
		switch kind {
		case "movie":
			_, id, err := media.ResolveIDAs(c, c.Params("item_id"), "movie")
			if err != nil {
				if errors.Is(err, response.ErrHandled) {
					return nil
				}
				return err
			}
			q = q.Where("media_type = ? AND media_id = ?", "movie", id)
		case "episode":
			id, ok := resolveEpisodeID(db, c)
			if !ok {
				return response.WriteError(c, fiber.StatusNotFound, "not_found", "episode not found")
			}
			q = q.Where("media_type = ? AND (media_id = ? OR episode_id = ?)", "episode", id, id)
		case "show":
			_, id, err := media.ResolveIDAs(c, c.Params("item_id"), "show")
			if err != nil {
				if errors.Is(err, response.ErrHandled) {
					return nil
				}
				return err
			}
			ids := episodeIDsForShow(db, id)
			if len(ids) == 0 {
				return writeContractPage(c, []map[string]any{}, page, limit, 0, 0)
			}
			q = q.Where("media_type = ? AND (media_id IN ? OR episode_id IN ?)", "episode", ids, ids)
		}
		q = applyHistoryWindow(q, c)

		var total int64
		if err := q.Model(&database.UserHistory{}).Count(&total).Error; err != nil {
			return response.InternalError(c)
		}
		var rows []database.UserHistory
		if err := q.Order("watched_at DESC, id DESC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}
		items := make([]map[string]any, 0, len(rows))
		for i := range rows {
			if row, ok := contractHistoryRow(db, &rows[i]); ok {
				items = append(items, row)
			}
		}
		return writeContractPage(c, items, page, limit, total, len(items))
	}
}

// resolveEpisodeID resolves {item_id} for an episode route. media.ResolveIDAs
// covers movies/shows only, so an episode id is a numeric row id here.
func resolveEpisodeID(db *gorm.DB, c *fiber.Ctx) (int64, bool) {
	raw := c.Params("item_id")
	if idType := c.Query("id_type"); idType != "" && idType != "trakt" {
		return 0, false // external episode ids are not stored
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	var count int64
	if err := db.Model(&database.Episode{}).Where("id = ?", id).Count(&count).Error; err != nil || count == 0 {
		return 0, false
	}
	return id, true
}

func episodeIDsForShow(db *gorm.DB, showID int64) []int64 {
	var ids []int64
	_ = db.Model(&database.Episode{}).Where("show_id = ?", showID).Pluck("id", &ids).Error
	return ids
}

// handleContractRatingsList serves the per-type rating arrays.
func handleContractRatingsList(kind string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := validateExtended(c); err != nil {
			return err
		}
		target, db, ok := profileTarget(c)
		if !ok {
			return nil
		}
		page, limit := contractPage(c)

		q := db.Where("user_id = ? AND media_type = ?", target.ID, kind)
		var total int64
		if err := q.Model(&database.UserRating{}).Count(&total).Error; err != nil {
			return response.InternalError(c)
		}
		var rows []database.UserRating
		if err := q.Order("rated_at DESC, id DESC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
			return response.InternalError(c)
		}

		items := make([]map[string]any, 0, len(rows))
		for i := range rows {
			if item, ok := contractRatingRow(db, &rows[i]); ok {
				items = append(items, item)
			}
		}
		return writeContractPage(c, items, page, limit, total, len(items))
	}
}

// handleContractRatingsByValue serves /ratings/{type}/{rating}.
func handleContractRatingsByValue(c *fiber.Ctx) error {
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
	rating, err := strconv.Atoi(c.Params("rating"))
	if err != nil || rating < 1 || rating > 10 {
		return response.BadRequest(c, "rating must be an integer from 1 to 10")
	}
	page, limit := contractPage(c)

	q := db.Where("user_id = ? AND rating = ?", target.ID, rating)
	if kind != "" {
		q = q.Where("media_type = ?", kind)
	}
	var total int64
	if err := q.Model(&database.UserRating{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var rows []database.UserRating
	if err := q.Order("rated_at DESC, id DESC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		if item, ok := contractRatingRow(db, &rows[i]); ok {
			items = append(items, item)
		}
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// contractRatingRow renders a rating row. The contract does NOT require the
// media object on a rating, so a rating whose media row is gone is still served
// (its media, and only that, is omitted) rather than dropped.
func contractRatingRow(db *gorm.DB, r *database.UserRating) (map[string]any, bool) {
	out := map[string]any{
		"rated_at": contractshape.Timestamp(r.RatedAt),
		"rating":   r.Rating,
		"type":     r.MediaType,
	}
	if media, ok := contractMediaRow(db, r.MediaType, r.MediaID); ok {
		out[r.MediaType] = media
	}
	return out, true
}

// ---------------------------------------------------------------------------
// Watched
// ---------------------------------------------------------------------------

// handleContractWatchedMovies serves /watched/movies: a map of movie slug to
// the watched dates, the contract's non-extended shape.
func handleContractWatchedMovies(c *fiber.Ctx) error {
	if err := validateExtended(c); err != nil {
		return err
	}
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	var rows []database.UserHistory
	if err := db.Where("user_id = ? AND media_type = ?", target.ID, "movie").
		Order("watched_at ASC, id ASC").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	out := map[string][]string{}
	for i := range rows {
		var m database.Movie
		if db.First(&m, rows[i].MediaID).Error != nil {
			continue
		}
		slug, _ := contractshape.MovieIDs(&m)["slug"].(string)
		if slug == "" {
			continue
		}
		out[slug] = append(out[slug], contractshape.Timestamp(rows[i].WatchedAt))
	}
	return c.JSON(out)
}

// handleContractWatchedShows serves /watched/shows: show slug -> season ->
// episode -> watched dates.
func handleContractWatchedShows(c *fiber.Ctx) error {
	if err := validateExtended(c); err != nil {
		return err
	}
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}

	var rows []database.UserHistory
	if err := db.Where("user_id = ? AND media_type = ?", target.ID, "episode").
		Order("watched_at ASC, id ASC").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}
	episodes := loadEpisodes(db, rows)
	out := map[string]map[string]map[string][]string{}
	for i := range rows {
		ep, found := episodes[episodeKey(&rows[i])]
		if !found {
			continue
		}
		var show database.Show
		if db.First(&show, ep.ShowID).Error != nil {
			continue
		}
		showSlug, _ := contractshape.ShowIDs(&show)["slug"].(string)
		if showSlug == "" {
			continue
		}
		var season database.Season
		if db.First(&season, ep.SeasonID).Error != nil {
			continue
		}
		if out[showSlug] == nil {
			out[showSlug] = map[string]map[string][]string{}
		}
		seasonKey := strconv.Itoa(season.SeasonNumber)
		if out[showSlug][seasonKey] == nil {
			out[showSlug][seasonKey] = map[string][]string{}
		}
		epKey := strconv.Itoa(ep.EpisodeNumber)
		out[showSlug][seasonKey][epKey] = append(out[showSlug][seasonKey][epKey], contractshape.Timestamp(rows[i].WatchedAt))
	}
	return c.JSON(out)
}

func episodeKey(h *database.UserHistory) int64 {
	if h.EpisodeID != nil {
		return *h.EpisodeID
	}
	return h.MediaID
}

func loadEpisodes(db *gorm.DB, rows []database.UserHistory) map[int64]database.Episode {
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, episodeKey(&rows[i]))
	}
	out := map[int64]database.Episode{}
	if len(ids) == 0 {
		return out
	}
	var eps []database.Episode
	if db.Where("id IN ?", ids).Find(&eps).Error != nil {
		return out
	}
	for i := range eps {
		out[eps[i].ID] = eps[i]
	}
	return out
}

// watchedAggregate is one group of history rows for the same media item.
type watchedAggregate struct {
	key        int64
	plays      int
	lastAt     time.Time
	mediaType  string
	showID     int64
	seasonID   int64
	episodeNum int
	seasonNum  int
}

// handleContractWatchedType serves /watched/{type} as the contract's aggregated
// array (plays, last_watched_at, last_updated_at, and the media object).
func handleContractWatchedType(c *fiber.Ctx) error {
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
	page, limit := contractPage(c)

	q := db.Where("user_id = ?", target.ID)
	switch kind {
	case "movie":
		q = q.Where("media_type = ?", "movie")
	case "episode":
		q = q.Where("media_type = ?", "episode")
	case "show", "season":
		q = q.Where("media_type = ?", "episode")
	default:
		return response.BadRequest(c, "invalid type")
	}

	var rows []database.UserHistory
	if err := q.Order("watched_at DESC, id DESC").Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	eps := loadEpisodes(db, rows)
	order := make([]int64, 0)
	agg := map[int64]*watchedAggregate{}
	for i := range rows {
		key := rows[i].MediaID
		mt := rows[i].MediaType
		a := &watchedAggregate{mediaType: mt}
		switch kind {
		case "show":
			ep, found := eps[episodeKey(&rows[i])]
			if !found {
				continue
			}
			key, a.showID, a.mediaType = ep.ShowID, ep.ShowID, "show"
		case "season":
			ep, found := eps[episodeKey(&rows[i])]
			if !found {
				continue
			}
			var season database.Season
			if db.First(&season, ep.SeasonID).Error != nil {
				continue
			}
			key, a.seasonID, a.seasonNum, a.mediaType = ep.SeasonID, ep.SeasonID, season.SeasonNumber, "season"
		case "episode":
			ep, found := eps[episodeKey(&rows[i])]
			if found {
				a.seasonID, a.episodeNum = ep.SeasonID, ep.EpisodeNumber
			}
		}
		if a.key == 0 {
			a.key = key
		}
		existing, seen := agg[key]
		if !seen {
			a.plays = 1
			a.lastAt = rows[i].WatchedAt
			agg[key] = a
			order = append(order, key)
			continue
		}
		existing.plays++
		if rows[i].WatchedAt.After(existing.lastAt) {
			existing.lastAt = rows[i].WatchedAt
		}
	}

	items := make([]map[string]any, 0, len(order))
	for _, key := range order {
		a := agg[key]
		media, ok := contractMediaRow(db, a.mediaType, key)
		if !ok {
			continue
		}
		items = append(items, map[string]any{
			"plays":           a.plays,
			"last_watched_at": contractshape.Timestamp(a.lastAt),
			"last_updated_at": contractshape.Timestamp(a.lastAt),
			a.mediaType:       media,
		})
	}

	total := int64(len(items))
	start := pageOffset(page, limit)
	if start > len(items) {
		start = len(items)
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return writeContractPage(c, items[start:end], page, limit, total, end-start)
}

// ---------------------------------------------------------------------------
// Activities
// ---------------------------------------------------------------------------

// handleContractActivities serves /users/{id}/{type}/activities: recent watch
// activity by the target's friends (mutual follows), followers, or the accounts
// they follow. Each row is a real user_history event from one of those accounts.
func handleContractActivities(c *fiber.Ctx) error {
	if err := validateExtended(c); err != nil {
		return err
	}
	target, db, ok := profileTarget(c)
	if !ok {
		return nil
	}
	kind := c.Params("type")
	if kind != "friends" && kind != "followers" && kind != "following" {
		return response.BadRequest(c, "type must be one of friends, followers, following")
	}
	page, limit := contractPage(c)

	userIDs, err := activityUserIDs(db, target.ID, kind)
	if err != nil {
		return response.InternalError(c)
	}
	if len(userIDs) == 0 {
		return writeContractPage(c, []map[string]any{}, page, limit, 0, 0)
	}

	q := db.Where("user_id IN ?", userIDs)
	var total int64
	if err := q.Model(&database.UserHistory{}).Count(&total).Error; err != nil {
		return response.InternalError(c)
	}
	var rows []database.UserHistory
	if err := q.Order("watched_at DESC, id DESC").Offset(pageOffset(page, limit)).Limit(limit).Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	usersByID, err := contractshape.UserByIDBatch(db, userIDs)
	if err != nil {
		return response.InternalError(c)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		row, ok := contractHistoryRow(db, &rows[i])
		if !ok {
			continue
		}
		mediaType, _ := row["type"].(string)
		items = append(items, map[string]any{
			"id":          rows[i].ID,
			"activity_at": contractshape.Timestamp(rows[i].WatchedAt),
			"action":      syncHistoryAction(rows[i].Source),
			"user":        contractshape.User(usersByID[rows[i].UserID]),
			"type":        mediaType,
			mediaType:     row[mediaType],
		})
	}
	return writeContractPage(c, items, page, limit, total, len(items))
}

// activityUserIDs resolves the account set an activities read covers.
func activityUserIDs(db *gorm.DB, targetID, kind string) ([]string, error) {
	var ids []string
	switch kind {
	case "followers":
		if err := db.Model(&database.UserFollow{}).Where("following_id = ?", targetID).Pluck("follower_id", &ids).Error; err != nil {
			return nil, err
		}
	case "following":
		if err := db.Model(&database.UserFollow{}).Where("follower_id = ?", targetID).Pluck("following_id", &ids).Error; err != nil {
			return nil, err
		}
	case "friends":
		// mutual follows: target follows X and X follows target.
		var following []string
		var followers []string
		if err := db.Model(&database.UserFollow{}).Where("follower_id = ?", targetID).Pluck("following_id", &following).Error; err != nil {
			return nil, err
		}
		if err := db.Model(&database.UserFollow{}).Where("following_id = ?", targetID).Pluck("follower_id", &followers).Error; err != nil {
			return nil, err
		}
		set := map[string]bool{}
		for _, f := range followers {
			set[f] = true
		}
		for _, f := range following {
			if set[f] {
				ids = append(ids, f)
			}
		}
	}
	return ids, nil
}
