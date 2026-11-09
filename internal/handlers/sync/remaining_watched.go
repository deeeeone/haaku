package sync

// GET /sync/watched/{type}: the per-type watched list, folded from user_history
// (episode rows aggregated into their show/season). Aggregation is done in Go
// because sqlite returns MAX(timestamp) as an undecodable string.

import (
	"sort"
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ============================================================================
// GET /sync/watched/{type}
// ============================================================================

type watchedGroup struct {
	Key     int64
	Plays   int
	LastAt  time.Time
	Season  int
	ShowID  int64
	MediaID int64
}

func handleWatchedType(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	switch c.Params("type") {
	case "movies":
		return watchedMovies(c, db, userID)
	case "episodes":
		return watchedEpisodes(c, db, userID)
	case "shows":
		return watchedShows(c, db, userID)
	case "seasons":
		return watchedSeasons(c, db, userID)
	default:
		return response.BadRequest(c, "invalid type: must be one of movies, shows, seasons, episodes")
	}
}

// loadEpisodeIndex loads the episode→season→show facts for a set of episode
// ids in a bounded number of queries.
type episodeFact struct {
	Episode database.Episode
	Season  database.Season
	Show    database.Show
}

func loadEpisodeFacts(db *gorm.DB, epIDs []int64) (map[int64]*episodeFact, error) {
	out := make(map[int64]*episodeFact, len(epIDs))
	if len(epIDs) == 0 {
		return out, nil
	}
	var eps []database.Episode
	if err := db.Where("id IN ?", epIDs).Find(&eps).Error; err != nil {
		return nil, err
	}
	seasonIDs := make([]int64, 0, len(eps))
	showIDs := make([]int64, 0, len(eps))
	for _, e := range eps {
		seasonIDs = append(seasonIDs, e.SeasonID)
		showIDs = append(showIDs, e.ShowID)
	}
	var seasons []database.Season
	if err := db.Where("id IN ?", seasonIDs).Find(&seasons).Error; err != nil {
		return nil, err
	}
	var shows []database.Show
	if err := db.Where("id IN ?", showIDs).Find(&shows).Error; err != nil {
		return nil, err
	}
	seasonByID := make(map[int64]database.Season, len(seasons))
	for _, s := range seasons {
		seasonByID[s.ID] = s
	}
	showByID := make(map[int64]database.Show, len(shows))
	for _, s := range shows {
		showByID[s.ID] = s
	}
	for _, e := range eps {
		f := &episodeFact{Episode: e, Season: seasonByID[e.SeasonID], Show: showByID[e.ShowID]}
		out[e.ID] = f
	}
	return out, nil
}

type histRow struct {
	MediaID   int64
	WatchedAt time.Time
}

// loadTypedHistory returns the caller's raw history rows of one media type.
// Aggregation happens in Go: sqlite strips the declared type from MAX(timestamp)
// and hands back an undecodable string, so MAX() is never used over a time column.
func loadTypedHistory(db *gorm.DB, userID, mediaType string) ([]histRow, error) {
	var rows []histRow
	err := db.Model(&database.UserHistory{}).
		Select("media_id, watched_at").
		Where("user_id = ? AND media_type = ?", userID, mediaType).
		Scan(&rows).Error
	return rows, err
}

// aggregateHistory folds raw history rows into per-media plays + last watched.
func aggregateHistory(rows []histRow) map[int64]*watchedGroup {
	agg := make(map[int64]*watchedGroup)
	for _, r := range rows {
		g := agg[r.MediaID]
		if g == nil {
			g = &watchedGroup{Key: r.MediaID}
			agg[r.MediaID] = g
		}
		g.Plays++
		if r.WatchedAt.After(g.LastAt) {
			g.LastAt = r.WatchedAt
		}
	}
	return agg
}

func sortedKeys(m map[int64]*watchedGroup) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func watchedMovies(c *fiber.Ctx, db *gorm.DB, userID string) error {
	rows, err := loadTypedHistory(db, userID, "movie")
	if err != nil {
		return response.InternalError(c)
	}
	agg := aggregateHistory(rows)
	ids := sortedKeys(agg)

	movies := make(map[int64]database.Movie, len(ids))
	if len(ids) > 0 {
		var ms []database.Movie
		if err := db.Where("id IN ?", ids).Find(&ms).Error; err != nil {
			return response.InternalError(c)
		}
		for _, m := range ms {
			movies[m.ID] = m
		}
	}

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		m, ok := movies[id]
		if !ok {
			continue
		}
		g := agg[id]
		out = append(out, map[string]any{
			"plays":           g.Plays,
			"last_watched_at": contractshape.Timestamp(g.LastAt),
			"type":            "movie",
			"movie":           contractshape.Movie(&m),
		})
	}
	return c.JSON(out)
}

func watchedEpisodes(c *fiber.Ctx, db *gorm.DB, userID string) error {
	rows, err := loadTypedHistory(db, userID, "episode")
	if err != nil {
		return response.InternalError(c)
	}
	agg := aggregateHistory(rows)
	ids := sortedKeys(agg)

	facts, err := loadEpisodeFacts(db, ids)
	if err != nil {
		return response.InternalError(c)
	}

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		f, ok := facts[id]
		if !ok {
			continue
		}
		g := agg[id]
		out = append(out, map[string]any{
			"plays":           g.Plays,
			"last_watched_at": contractshape.Timestamp(g.LastAt),
			"type":            "episode",
			"episode":         contractshape.Episode(&f.Episode, f.Season.SeasonNumber),
			"show":            contractshape.Show(&f.Show),
		})
	}
	return c.JSON(out)
}

func watchedShows(c *fiber.Ctx, db *gorm.DB, userID string) error {
	rows, err := loadTypedHistory(db, userID, "episode")
	if err != nil {
		return response.InternalError(c)
	}
	epIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		epIDs = append(epIDs, r.MediaID)
	}
	facts, err := loadEpisodeFacts(db, epIDs)
	if err != nil {
		return response.InternalError(c)
	}

	// Fold episodes into their show: plays sum, newest watched_at.
	byShow := make(map[int64]*watchedGroup)
	for _, r := range rows {
		f, ok := facts[r.MediaID]
		if !ok {
			continue
		}
		g := byShow[f.Show.ID]
		if g == nil {
			g = &watchedGroup{Key: f.Show.ID}
			byShow[f.Show.ID] = g
		}
		g.Plays++
		if r.WatchedAt.After(g.LastAt) {
			g.LastAt = r.WatchedAt
		}
	}
	ids := sortedKeys(byShow)

	var shows []database.Show
	if err := db.Where("id IN ?", ids).Find(&shows).Error; err != nil {
		return response.InternalError(c)
	}
	showByID := make(map[int64]database.Show, len(shows))
	for _, s := range shows {
		showByID[s.ID] = s
	}

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		s, ok := showByID[id]
		if !ok {
			continue
		}
		g := byShow[id]
		out = append(out, map[string]any{
			"plays":           g.Plays,
			"last_watched_at": contractshape.Timestamp(g.LastAt),
			"type":            "show",
			"show":            contractshape.Show(&s),
		})
	}
	return c.JSON(out)
}

func watchedSeasons(c *fiber.Ctx, db *gorm.DB, userID string) error {
	rows, err := loadTypedHistory(db, userID, "episode")
	if err != nil {
		return response.InternalError(c)
	}
	epIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		epIDs = append(epIDs, r.MediaID)
	}
	facts, err := loadEpisodeFacts(db, epIDs)
	if err != nil {
		return response.InternalError(c)
	}

	bySeason := make(map[int64]*watchedGroup)
	for _, r := range rows {
		f, ok := facts[r.MediaID]
		if !ok {
			continue
		}
		g := bySeason[f.Season.ID]
		if g == nil {
			g = &watchedGroup{Key: f.Season.ID, Season: f.Season.SeasonNumber, ShowID: f.Show.ID}
			bySeason[f.Season.ID] = g
		}
		g.Plays++
		if r.WatchedAt.After(g.LastAt) {
			g.LastAt = r.WatchedAt
		}
	}

	ids := sortedKeys(bySeason)
	showIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		showIDs = append(showIDs, bySeason[id].ShowID)
	}
	var shows []database.Show
	if err := db.Where("id IN ?", showIDs).Find(&shows).Error; err != nil {
		return response.InternalError(c)
	}
	showByID := make(map[int64]database.Show, len(shows))
	for _, s := range shows {
		showByID[s.ID] = s
	}
	var seasons []database.Season
	if err := db.Where("id IN ?", ids).Find(&seasons).Error; err != nil {
		return response.InternalError(c)
	}
	seasonByID := make(map[int64]database.Season, len(seasons))
	for _, s := range seasons {
		seasonByID[s.ID] = s
	}

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		season, ok := seasonByID[id]
		if !ok {
			continue
		}
		g := bySeason[id]
		item := map[string]any{
			"plays":           g.Plays,
			"last_watched_at": contractshape.Timestamp(g.LastAt),
			"type":            "season",
			"season": map[string]any{
				"number":         season.SeasonNumber,
				"ids":            contractshape.SeasonIDs(&season),
				"episode_count":  season.EpisodeCount,
				"aired_episodes": season.EpisodeCount,
				"title":          nil,
				"overview":       nil,
				"images":         map[string]any{},
			},
		}
		if s, ok := showByID[g.ShowID]; ok {
			item["show"] = contractshape.Show(&s)
		}
		out = append(out, item)
	}
	return c.JSON(out)
}
