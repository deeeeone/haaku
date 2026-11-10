package discovery

// Period rankings and the 24-hour "hot" feeds (issue #66).
//
// Every value is a count over rows we already store. The period window is the
// contract's enum (daily|weekly|monthly|yearly|all), ending now:
//
//	watched   distinct users with history in the window
//	played    history rows in the window
//	collected distinct collectors with collected_at in the window
//	favorited distinct users who favorited in the window
//
// A show counts its episodes' history and collection. Every count field the
// contract item carries is computed for every row, not just the one the list is
// ranked by, so the payload never reports a fabricated zero.
//
// hot ranks titles by activity in the last 24 hours:
//
//	score = history rows + checkin rows + scrobble rows (start/scrobble)
//	      + watchlist adds
//
// The contract's item field for hot is list_count, which we report as the
// number of distinct users who added the title to their watchlist in that same
// window. Ties fall back to popularity DESC, then id ASC.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"gorm.io/gorm"
)

// rankingsNow is a seam so tests can pin the window.
var rankingsNow = func() time.Time { return time.Now().UTC() }

// periodLengths is the contract's period enum mapped onto a look-back window.
// "all" is the absence of a lower bound and is handled separately.
var periodLengths = map[string]time.Duration{
	"daily":   24 * time.Hour,
	"weekly":  7 * 24 * time.Hour,
	"monthly": 30 * 24 * time.Hour,
	"yearly":  365 * 24 * time.Hour,
}

// periodStart returns the lower bound of a period. A zero start means "all"
// history. ok is false for any value outside the contract enum.
func periodStart(period string, now time.Time) (time.Time, bool) {
	if period == "all" {
		return time.Time{}, true
	}
	length, ok := periodLengths[period]
	if !ok {
		return time.Time{}, false
	}
	return now.UTC().Add(-length), true
}

// rankingCounts is every counter a contract ranking item can carry.
type rankingCounts struct {
	WatcherCount   int64
	PlayCount      int64
	CollectedCount int64
	CollectorCount int64
	UserCount      int64
}

func (c *rankingCounts) metric(kind string) int64 {
	switch kind {
	case "watched":
		return c.WatcherCount
	case "played":
		return c.PlayCount
	case "collected":
		return c.CollectedCount
	case "favorited":
		return c.UserCount
	default:
		return 0
	}
}

func countsFor(counts map[int64]*rankingCounts, id int64) *rankingCounts {
	entry := counts[id]
	if entry == nil {
		entry = &rankingCounts{}
		counts[id] = entry
	}
	return entry
}

func init() {
	kinds := []string{"watched", "played", "collected", "favorited"}
	for _, typ := range []string{"movies", "shows"} {
		for _, kind := range kinds {
			openapi.Register(openapi.RouteDef{
				Method:      "GET",
				Path:        "/v1/" + typ + "/" + kind + "/{period}",
				Summary:     "Most " + kind + " " + typ + " in a period (daily|weekly|monthly|yearly|all)",
				Tags:        []string{titleOf(typ)},
				Auth:        "none",
				StatusCodes: map[int]string{200: "Ranked items"},
			})
		}
		openapi.Register(openapi.RouteDef{
			Method:      "GET",
			Path:        "/v1/" + typ + "/hot",
			Summary:     "Currently hot " + typ + " (activity in the last 24 hours)",
			Tags:        []string{titleOf(typ)},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Hot items"},
		})
	}
}

// registerRankingRoutes mounts the contract's period and hot paths. They are
// registered before media's parameterised /movies/:id and /shows/:id routes, so
// static segments such as "hot" are never read as an id.
func registerRankingRoutes(api fiber.Router) {
	movies := api.Group("/movies")
	movies.Get("/watched/:period", func(c *fiber.Ctx) error { return serveMovieRanking(c, "watched") })
	movies.Get("/played/:period", func(c *fiber.Ctx) error { return serveMovieRanking(c, "played") })
	movies.Get("/collected/:period", func(c *fiber.Ctx) error { return serveMovieRanking(c, "collected") })
	movies.Get("/favorited/:period", func(c *fiber.Ctx) error { return serveMovieRanking(c, "favorited") })
	movies.Get("/hot", func(c *fiber.Ctx) error { return serveHot(c, "movie") })

	shows := api.Group("/shows")
	shows.Get("/watched/:period", func(c *fiber.Ctx) error { return serveShowRanking(c, "watched") })
	shows.Get("/played/:period", func(c *fiber.Ctx) error { return serveShowRanking(c, "played") })
	shows.Get("/collected/:period", func(c *fiber.Ctx) error { return serveShowRanking(c, "collected") })
	shows.Get("/favorited/:period", func(c *fiber.Ctx) error { return serveShowRanking(c, "favorited") })
	shows.Get("/hot", func(c *fiber.Ctx) error { return serveHot(c, "show") })
}

// rankedRow is one candidate in a period ranking before pagination.
type rankedRow struct {
	id     int64
	counts rankingCounts
}

// sortRanked orders a period ranking by the endpoint's metric, then by id so
// equal counts are stable across requests.
func sortRanked(rows []rankedRow, kind string) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i].counts.metric(kind)
		right := rows[j].counts.metric(kind)
		if left != right {
			return left > right
		}
		return rows[i].id < rows[j].id
	})
}

// pageBounds clamps a page window to the candidate set.
func pageBounds(page, limit, total int) (int, int) {
	first := (page - 1) * limit
	if first > total {
		first = total
	}
	last := first + limit
	if last > total {
		last = total
	}
	return first, last
}

func serveMovieRanking(c *fiber.Ctx, kind string) error {
	period := c.Params("period")
	start, ok := periodStart(period, rankingsNow())
	if !ok {
		return response.BadRequest(c, "invalid period, expected daily, weekly, monthly, yearly, or all")
	}
	page, limit := response.ParsePagination(c, 25)

	cacheKey := fmt.Sprintf("rankings:movies:%s:%s:page%d:limit%d", kind, period, page, limit)
	if data, ok := discoveryGetCache(cacheKey); ok {
		c.Set("Content-Type", "application/json")
		return c.Send(data)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	counts, err := movieRankingCounts(db, start)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	rows := make([]rankedRow, 0, len(counts))
	for id, entry := range counts {
		if entry.metric(kind) > 0 {
			rows = append(rows, rankedRow{id: id, counts: *entry})
		}
	}
	sortRanked(rows, kind)

	first, last := pageBounds(page, limit, len(rows))
	window := rows[first:last]

	ids := make([]int64, 0, len(window))
	for _, row := range window {
		ids = append(ids, row.id)
	}
	movies := loadMovieMap(db, ids)

	out := make([]map[string]any, 0, len(window))
	for _, row := range window {
		movie, found := movies[row.id]
		if !found {
			continue
		}
		out = append(out, map[string]any{
			"watcher_count":   row.counts.WatcherCount,
			"play_count":      row.counts.PlayCount,
			"collected_count": row.counts.CollectedCount,
			"movie":           contractshape.Movie(&movie),
		})
	}

	response.SetPaginationHeaders(c, page, limit, int64(len(rows)), len(out))
	discoverySetCache(cacheKey, out)
	return c.JSON(out)
}

func serveShowRanking(c *fiber.Ctx, kind string) error {
	period := c.Params("period")
	start, ok := periodStart(period, rankingsNow())
	if !ok {
		return response.BadRequest(c, "invalid period, expected daily, weekly, monthly, yearly, or all")
	}
	page, limit := response.ParsePagination(c, 25)

	cacheKey := fmt.Sprintf("rankings:shows:%s:%s:page%d:limit%d", kind, period, page, limit)
	if data, ok := discoveryGetCache(cacheKey); ok {
		c.Set("Content-Type", "application/json")
		return c.Send(data)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	counts, err := showRankingCounts(db, start)
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	rows := make([]rankedRow, 0, len(counts))
	for id, entry := range counts {
		if entry.metric(kind) > 0 {
			rows = append(rows, rankedRow{id: id, counts: *entry})
		}
	}
	sortRanked(rows, kind)

	first, last := pageBounds(page, limit, len(rows))
	window := rows[first:last]

	ids := make([]int64, 0, len(window))
	for _, row := range window {
		ids = append(ids, row.id)
	}
	shows := loadShowMap(db, ids)

	out := make([]map[string]any, 0, len(window))
	for _, row := range window {
		show, found := shows[row.id]
		if !found {
			continue
		}
		out = append(out, map[string]any{
			"watcher_count":   row.counts.WatcherCount,
			"play_count":      row.counts.PlayCount,
			"collected_count": row.counts.CollectedCount,
			"collector_count": row.counts.CollectorCount,
			"show":            contractshape.Show(&show),
		})
	}

	response.SetPaginationHeaders(c, page, limit, int64(len(rows)), len(out))
	discoverySetCache(cacheKey, out)
	return c.JSON(out)
}

// ---------------------------------------------------------------------------
// Period aggregates
// ---------------------------------------------------------------------------

func movieRankingCounts(db *gorm.DB, start time.Time) (map[int64]*rankingCounts, error) {
	counts := make(map[int64]*rankingCounts)

	history := db.Table("user_history").
		Select("media_id, COUNT(*) AS play_count, COUNT(DISTINCT user_id) AS watcher_count").
		Where("media_type = ?", "movie")
	if !start.IsZero() {
		history = history.Where("watched_at >= ?", start)
	}
	var historyRows []struct {
		MediaID      int64
		PlayCount    int64
		WatcherCount int64
	}
	if err := history.Group("media_id").Scan(&historyRows).Error; err != nil {
		return nil, err
	}
	for _, row := range historyRows {
		entry := countsFor(counts, row.MediaID)
		entry.PlayCount = row.PlayCount
		entry.WatcherCount = row.WatcherCount
	}

	collection := db.Table("user_collection").
		Select("media_id, COUNT(*) AS collected_count, COUNT(DISTINCT user_id) AS collector_count").
		Where("media_type = ?", "movie")
	if !start.IsZero() {
		collection = collection.Where("collected_at >= ?", start)
	}
	var collectionRows []struct {
		MediaID        int64
		CollectedCount int64
		CollectorCount int64
	}
	if err := collection.Group("media_id").Scan(&collectionRows).Error; err != nil {
		return nil, err
	}
	for _, row := range collectionRows {
		entry := countsFor(counts, row.MediaID)
		entry.CollectedCount = row.CollectedCount
		entry.CollectorCount = row.CollectorCount
	}

	favorites := db.Table("user_favorites").
		Select("media_id, COUNT(DISTINCT user_id) AS user_count").
		Where("media_type = ?", "movie")
	if !start.IsZero() {
		favorites = favorites.Where("favorited_at >= ?", start)
	}
	var favoriteRows []struct {
		MediaID   int64
		UserCount int64
	}
	if err := favorites.Group("media_id").Scan(&favoriteRows).Error; err != nil {
		return nil, err
	}
	for _, row := range favoriteRows {
		countsFor(counts, row.MediaID).UserCount = row.UserCount
	}

	return counts, nil
}

func showRankingCounts(db *gorm.DB, start time.Time) (map[int64]*rankingCounts, error) {
	counts := make(map[int64]*rankingCounts)

	history := db.Table("user_history AS h").
		Select("e.show_id AS media_id, COUNT(*) AS play_count, COUNT(DISTINCT h.user_id) AS watcher_count").
		Joins("JOIN episodes e ON e.id = h.media_id").
		Where("h.media_type = ?", "episode")
	if !start.IsZero() {
		history = history.Where("h.watched_at >= ?", start)
	}
	var historyRows []struct {
		MediaID      int64
		PlayCount    int64
		WatcherCount int64
	}
	if err := history.Group("e.show_id").Scan(&historyRows).Error; err != nil {
		return nil, err
	}
	for _, row := range historyRows {
		entry := countsFor(counts, row.MediaID)
		entry.PlayCount = row.PlayCount
		entry.WatcherCount = row.WatcherCount
	}

	// A show is collected when the user collected the show itself or any of its
	// episodes; the LEFT JOIN collapses both forms onto the show id and the
	// COALESCE/dedup keeps one collector per user.
	collection := db.Table("user_collection AS uc").
		Select("COALESCE(e.show_id, uc.media_id) AS media_id, COUNT(*) AS collected_count, COUNT(DISTINCT uc.user_id) AS collector_count").
		Joins("LEFT JOIN episodes e ON uc.media_type = 'episode' AND e.id = uc.media_id").
		Where("uc.media_type = ? OR e.show_id IS NOT NULL", "show")
	if !start.IsZero() {
		collection = collection.Where("uc.collected_at >= ?", start)
	}
	var collectionRows []struct {
		MediaID        int64
		CollectedCount int64
		CollectorCount int64
	}
	if err := collection.Group("COALESCE(e.show_id, uc.media_id)").Scan(&collectionRows).Error; err != nil {
		return nil, err
	}
	for _, row := range collectionRows {
		entry := countsFor(counts, row.MediaID)
		entry.CollectedCount = row.CollectedCount
		entry.CollectorCount = row.CollectorCount
	}

	favorites := db.Table("user_favorites").
		Select("media_id, COUNT(DISTINCT user_id) AS user_count").
		Where("media_type = ?", "show")
	if !start.IsZero() {
		favorites = favorites.Where("favorited_at >= ?", start)
	}
	var favoriteRows []struct {
		MediaID   int64
		UserCount int64
	}
	if err := favorites.Group("media_id").Scan(&favoriteRows).Error; err != nil {
		return nil, err
	}
	for _, row := range favoriteRows {
		countsFor(counts, row.MediaID).UserCount = row.UserCount
	}

	return counts, nil
}

// ---------------------------------------------------------------------------
// Hot (last 24 hours)
// ---------------------------------------------------------------------------

type hotCounts struct {
	Score     int64
	ListCount int64
}

func hotFor(scores map[int64]*hotCounts, id int64) *hotCounts {
	entry := scores[id]
	if entry == nil {
		entry = &hotCounts{}
		scores[id] = entry
	}
	return entry
}

func serveHot(c *fiber.Ctx, mediaType string) error {
	now := rankingsNow()
	start := now.Add(-24 * time.Hour)
	page, limit := response.ParsePagination(c, 25)

	cacheKey := fmt.Sprintf("rankings:hot:%s:page%d:limit%d", mediaType, page, limit)
	if data, ok := discoveryGetCache(cacheKey); ok {
		c.Set("Content-Type", "application/json")
		return c.Send(data)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var scores map[int64]*hotCounts
	var err error
	if mediaType == "movie" {
		scores, err = movieHotScores(db, start)
	} else {
		scores, err = showHotScores(db, start)
	}
	if err != nil {
		return response.InternalError(c, "database query error")
	}

	type hotRow struct {
		id         int64
		score      int64
		listCount  int64
		popularity float64
	}

	candidates := make([]hotRow, 0, len(scores))
	for id, entry := range scores {
		candidates = append(candidates, hotRow{id: id, score: entry.Score, listCount: entry.ListCount})
	}

	// Tie-break needs popularity, so load the candidate rows before ranking.
	ids := make([]int64, 0, len(candidates))
	for _, row := range candidates {
		ids = append(ids, row.id)
	}

	var popularity map[int64]float64
	if mediaType == "movie" {
		movies := loadMovieMap(db, ids)
		popularity = make(map[int64]float64, len(movies))
		for id, movie := range movies {
			popularity[id] = movie.Popularity
		}
	} else {
		shows := loadShowMap(db, ids)
		popularity = make(map[int64]float64, len(shows))
		for id, show := range shows {
			popularity[id] = show.Popularity
		}
	}
	for i := range candidates {
		candidates[i].popularity = popularity[candidates[i].id]
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].popularity != candidates[j].popularity {
			return candidates[i].popularity > candidates[j].popularity
		}
		return candidates[i].id < candidates[j].id
	})

	first, last := pageBounds(page, limit, len(candidates))
	window := candidates[first:last]

	windowIDs := make([]int64, 0, len(window))
	for _, row := range window {
		windowIDs = append(windowIDs, row.id)
	}

	out := make([]map[string]any, 0, len(window))
	if mediaType == "movie" {
		movies := loadMovieMap(db, windowIDs)
		for _, row := range window {
			movie, found := movies[row.id]
			if !found {
				continue
			}
			out = append(out, map[string]any{
				"list_count": row.listCount,
				"movie":      contractshape.Movie(&movie),
			})
		}
	} else {
		shows := loadShowMap(db, windowIDs)
		for _, row := range window {
			show, found := shows[row.id]
			if !found {
				continue
			}
			out = append(out, map[string]any{
				"list_count": row.listCount,
				"show":       contractshape.Show(&show),
			})
		}
	}

	response.SetPaginationHeaders(c, page, limit, int64(len(candidates)), len(out))
	discoverySetCache(cacheKey, out)
	return c.JSON(out)
}

func movieHotScores(db *gorm.DB, start time.Time) (map[int64]*hotCounts, error) {
	scores := make(map[int64]*hotCounts)

	if err := addHotActivity(db, scores, "user_history",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'movie' AND watched_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "user_checkins",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'movie' AND created_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "scrobble_sessions",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'movie' AND action IN ('start','scrobble') AND created_at >= ?", start); err != nil {
		return nil, err
	}

	var watchlist []struct {
		MediaID   int64
		N         int64
		UserCount int64
	}
	if err := db.Table("user_watchlist").
		Select("media_id, COUNT(*) AS n, COUNT(DISTINCT user_id) AS user_count").
		Where("media_type = 'movie' AND listed_at >= ?", start).
		Group("media_id").Scan(&watchlist).Error; err != nil {
		return nil, err
	}
	for _, row := range watchlist {
		entry := hotFor(scores, row.MediaID)
		entry.Score += row.N
		entry.ListCount = row.UserCount
	}

	return scores, nil
}

func showHotScores(db *gorm.DB, start time.Time) (map[int64]*hotCounts, error) {
	scores := make(map[int64]*hotCounts)

	// Episode history, rolled up to the show.
	if err := addHotActivity(db, scores, "user_history AS h",
		"e.show_id AS media_id, COUNT(*) AS n", "JOIN episodes e ON e.id = h.media_id",
		"h.media_type = 'episode' AND h.watched_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "user_history",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'show' AND watched_at >= ?", start); err != nil {
		return nil, err
	}

	// Checkins may name the show, an episode or a season.
	if err := addHotActivity(db, scores, "user_checkins",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'show' AND created_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "user_checkins AS c",
		"e.show_id AS media_id, COUNT(*) AS n",
		"JOIN episodes e ON e.id = CASE WHEN c.episode_id IS NOT NULL THEN c.episode_id ELSE c.media_id END",
		"c.media_type = 'episode' AND c.created_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "user_checkins AS c",
		"s.show_id AS media_id, COUNT(*) AS n",
		"JOIN seasons s ON s.id = CASE WHEN c.season_id IS NOT NULL THEN c.season_id ELSE c.media_id END",
		"c.media_type = 'season' AND c.created_at >= ?", start); err != nil {
		return nil, err
	}

	// Scrobbles name the show or one of its episodes.
	if err := addHotActivity(db, scores, "scrobble_sessions",
		"media_id, COUNT(*) AS n", "",
		"media_type = 'show' AND action IN ('start','scrobble') AND created_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "scrobble_sessions AS ss",
		"e.show_id AS media_id, COUNT(*) AS n",
		"JOIN episodes e ON e.id = CASE WHEN ss.episode_id IS NOT NULL THEN ss.episode_id ELSE ss.media_id END",
		"ss.media_type = 'episode' AND ss.action IN ('start','scrobble') AND ss.created_at >= ?", start); err != nil {
		return nil, err
	}
	if err := addHotActivity(db, scores, "scrobble_sessions AS ss",
		"s.show_id AS media_id, COUNT(*) AS n",
		"JOIN seasons s ON s.id = CASE WHEN ss.season_id IS NOT NULL THEN ss.season_id ELSE ss.media_id END",
		"ss.media_type = 'season' AND ss.action IN ('start','scrobble') AND ss.created_at >= ?", start); err != nil {
		return nil, err
	}

	var watchlist []struct {
		MediaID   int64
		N         int64
		UserCount int64
	}
	if err := db.Table("user_watchlist").
		Select("media_id, COUNT(*) AS n, COUNT(DISTINCT user_id) AS user_count").
		Where("media_type = 'show' AND listed_at >= ?", start).
		Group("media_id").Scan(&watchlist).Error; err != nil {
		return nil, err
	}
	for _, row := range watchlist {
		entry := hotFor(scores, row.MediaID)
		entry.Score += row.N
		entry.ListCount = row.UserCount
	}

	return scores, nil
}

// addHotActivity folds one signal's per-media row counts into scores. table,
// selectClause and joins are fixed strings from this file (never user input);
// where takes a single start-time argument.
func addHotActivity(db *gorm.DB, scores map[int64]*hotCounts, table, selectClause, joins, where string, start time.Time) error {
	query := db.Table(table).Select(selectClause)
	if joins != "" {
		query = query.Joins(joins)
	}
	var rows []struct {
		MediaID int64
		N       int64
	}
	// Group by the SELECT expression, not its alias. When a join rolls a child
	// row up to its parent (e.show_id AS media_id), the input relation also has
	// a media_id column, and PostgreSQL resolves GROUP BY media_id to that input
	// column — so e.show_id is left ungrouped and PG rejects the query (42803).
	// SQLite is lenient, which is why this only showed up on the DB suite.
	groupExpr := "media_id"
	if i := strings.Index(selectClause, " AS media_id"); i >= 0 {
		groupExpr = selectClause[:i]
	}
	if err := query.Where(where, start).Group(groupExpr).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		hotFor(scores, row.MediaID).Score += row.N
	}
	return nil
}

// ---------------------------------------------------------------------------
// Row loaders shared by the ranking and media feeds
// ---------------------------------------------------------------------------

func loadMovieMap(db *gorm.DB, ids []int64) map[int64]database.Movie {
	out := make(map[int64]database.Movie, len(ids))
	if len(ids) == 0 {
		return out
	}
	var movies []database.Movie
	if err := db.Where("id IN ?", ids).Find(&movies).Error; err != nil {
		return out
	}
	for _, movie := range movies {
		out[movie.ID] = movie
	}
	return out
}

func loadShowMap(db *gorm.DB, ids []int64) map[int64]database.Show {
	out := make(map[int64]database.Show, len(ids))
	if len(ids) == 0 {
		return out
	}
	var shows []database.Show
	if err := db.Where("id IN ?", ids).Find(&shows).Error; err != nil {
		return out
	}
	for _, show := range shows {
		out[show.ID] = show
	}
	return out
}
