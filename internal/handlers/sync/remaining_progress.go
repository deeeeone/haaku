package sync

// Shared progress computation plus the three /sync/progress reads.
// Every value is derived from user_history and the episodes/seasons tables; a
// value the store cannot derive is null/absent, and an unsupported narrowing
// parameter is rejected rather than ignored.

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
// Progress computation (shared by up_next, up_next_nitro, watched)
// ============================================================================

// histMarker is one episode's watch state: the most recent watched_at and how
// many history rows point at it (plays).
type histMarker struct {
	last  time.Time
	plays int
}

// showProgressRow is the computed progress for one show.
type showProgressRow struct {
	show           database.Show
	aired          int
	completed      int
	lastWatchedAt  *time.Time
	next           *database.Episode
	nextSeason     int
	last           *database.Episode
	lastSeason     int
	playCount      int
	minutesWatched int
	minutesLeft    int
}

// historyShowIDs returns the distinct show ids the caller has episode history
// for.
func historyShowIDs(db *gorm.DB, userID string) ([]int64, error) {
	var epIDs []int64
	if err := db.Model(&database.UserHistory{}).
		Where("user_id = ? AND media_type = ?", userID, "episode").
		Pluck("media_id", &epIDs).Error; err != nil {
		return nil, err
	}
	if len(epIDs) == 0 {
		return nil, nil
	}
	var showIDs []int64
	if err := db.Model(&database.Episode{}).Where("id IN ?", epIDs).
		Distinct().Pluck("show_id", &showIDs).Error; err != nil {
		return nil, err
	}
	return showIDs, nil
}

// computeProgressForShows computes per-show progress for showIDs from real
// rows: the caller's episode history plus the episodes/seasons tables. One
// query per table family (history, episodes, seasons, shows) plus per-show
// grouping in Go — no per-episode queries.
func computeProgressForShows(db *gorm.DB, userID string, showIDs []int64) ([]*showProgressRow, error) {
	if len(showIDs) == 0 {
		return nil, nil
	}

	// The caller's episode history, grouped by episode id.
	var hist []database.UserHistory
	if err := db.Where("user_id = ? AND media_type = ?", userID, "episode").Find(&hist).Error; err != nil {
		return nil, err
	}
	marks := make(map[int64]*histMarker, len(hist))
	for i := range hist {
		m := marks[hist[i].MediaID]
		if m == nil {
			m = &histMarker{}
			marks[hist[i].MediaID] = m
		}
		m.plays++
		if hist[i].WatchedAt.After(m.last) {
			m.last = hist[i].WatchedAt
		}
	}

	var shows []database.Show
	if err := db.Where("id IN ?", showIDs).Find(&shows).Error; err != nil {
		return nil, err
	}

	var seasons []database.Season
	if err := db.Where("show_id IN ?", showIDs).Find(&seasons).Error; err != nil {
		return nil, err
	}
	seasonNum := make(map[int64]int, len(seasons))
	for _, s := range seasons {
		seasonNum[s.ID] = s.SeasonNumber
	}

	var allEps []database.Episode
	if err := db.Where("show_id IN ?", showIDs).Find(&allEps).Error; err != nil {
		return nil, err
	}
	byShow := make(map[int64][]database.Episode, len(showIDs))
	for _, e := range allEps {
		byShow[e.ShowID] = append(byShow[e.ShowID], e)
	}

	today := time.Now().UTC().Format("2006-01-02")

	out := make([]*showProgressRow, 0, len(shows))
	for i := range shows {
		sh := shows[i]
		eps := byShow[sh.ID]
		sort.SliceStable(eps, func(a, b int) bool {
			sa, sb := seasonNum[eps[a].SeasonID], seasonNum[eps[b].SeasonID]
			if sa != sb {
				return sa < sb
			}
			return eps[a].EpisodeNumber < eps[b].EpisodeNumber
		})

		row := &showProgressRow{show: sh}
		for j := range eps {
			ep := &eps[j]
			if !episodeAired(ep, today) {
				continue
			}
			row.aired++
			if m, ok := marks[ep.ID]; ok {
				row.completed++
				row.playCount += m.plays
				row.minutesWatched += ep.Runtime * m.plays
				if row.lastWatchedAt == nil || m.last.After(*row.lastWatchedAt) {
					t := m.last
					row.lastWatchedAt = &t
					row.last = ep
					row.lastSeason = seasonNum[ep.SeasonID]
				}
			} else {
				row.minutesLeft += ep.Runtime
				if row.next == nil {
					row.next = ep
					row.nextSeason = seasonNum[ep.SeasonID]
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func episodeAired(ep *database.Episode, today string) bool {
	return ep.AiredAt != nil && !ep.AiredAt.IsZero() && ep.AiredAt.Format("2006-01-02") <= today
}

// progressObject renders a contract progress object for a computed row.
func progressObject(row *showProgressRow, includeStats bool) map[string]any {
	prog := map[string]any{
		"aired":           row.aired,
		"completed":       row.completed,
		"last_watched_at": contractshape.TimestampPtr(row.lastWatchedAt),
		"reset_at":        nil,
		"next_episode":    nil,
		"last_episode":    nil,
	}
	if row.next != nil {
		prog["next_episode"] = contractshape.Episode(row.next, row.nextSeason)
	}
	if row.last != nil {
		prog["last_episode"] = contractshape.Episode(row.last, row.lastSeason)
	}
	if includeStats {
		prog["stats"] = map[string]any{
			"play_count":      row.playCount,
			"minutes_watched": row.minutesWatched,
			"minutes_left":    row.minutesLeft,
		}
	}
	return prog
}

func progressItem(row *showProgressRow, includeStats bool) map[string]any {
	return map[string]any{
		"show":     contractshape.Show(&row.show),
		"progress": progressObject(row, includeStats),
	}
}

// progressSort validates the contract's sort_by/sort_how pair against the
// values we can actually order by and sorts rows in place. Anything else is a
// 400 rather than a silently ignored sort.
func progressSort(c *fiber.Ctx, rows []*showProgressRow) error {
	sortBy := c.Query("sort_by", "last_watched_at")
	how := c.Query("sort_how", "desc")
	if how != "asc" && how != "desc" {
		return response.BadRequest(c, "sort_how must be asc or desc")
	}
	less := func(a, b *showProgressRow) bool { return false }
	switch sortBy {
	case "last_watched_at":
		less = func(a, b *showProgressRow) bool {
			at, bt := a.lastWatchedAt, b.lastWatchedAt
			if at == nil && bt == nil {
				return a.show.ID < b.show.ID
			}
			if at == nil {
				return false
			}
			if bt == nil {
				return true
			}
			if at.Equal(*bt) {
				return a.show.ID < b.show.ID
			}
			return at.After(*bt)
		}
	case "title":
		less = func(a, b *showProgressRow) bool { return a.show.Title < b.show.Title }
	case "aired":
		less = func(a, b *showProgressRow) bool { return a.aired < b.aired }
	default:
		return response.BadRequest(c, "unsupported sort_by; supported: last_watched_at, title, aired")
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if how == "desc" {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
	return nil
}

// paginateRows slices computed rows to the requested page. The total is the
// full filtered count.
func paginateRows(c *fiber.Ctx, rows []*showProgressRow) ([]*showProgressRow, int, int, int64) {
	page, limit := response.ParsePagination(c, 25)
	total := int64(len(rows))
	start := (page - 1) * limit
	if start > len(rows) {
		start = len(rows)
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], page, limit, total
}

// ============================================================================
// GET /sync/progress/up_next
// ============================================================================

func handleUpNext(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	showIDs, err := historyShowIDs(db, userID)
	if err != nil {
		return response.InternalError(c)
	}
	rows, err := computeProgressForShows(db, userID, showIDs)
	if err != nil {
		return response.InternalError(c)
	}

	// Up next means there is a next aired episode to watch; a caught-up show is
	// excluded (the acceptance criterion for this row).
	withNext := make([]*showProgressRow, 0, len(rows))
	for _, r := range rows {
		if r.next != nil {
			withNext = append(withNext, r)
		}
	}
	if err := progressSort(c, withNext); err != nil {
		return err
	}

	pageRows, page, limit, total := paginateRows(c, withNext)
	includeStats := queryBool(c, "include_stats") || queryBool(c, "lifetime_stats")

	out := make([]map[string]any, 0, len(pageRows))
	for _, r := range pageRows {
		out = append(out, progressItem(r, includeStats))
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// ============================================================================
// GET /sync/progress/up_next_nitro
// ============================================================================

// nitroUnsupportedFilters are query params the contract documents that our
// store cannot narrow by. Rather than accept-and-ignore them we reject the
// request (the manifest grades this row divergent, naming what is served).
var nitroUnsupportedFilters = []string{
	"watchnow", "genres", "subgenres", "years", "ratings", "start_date",
	"end_date", "runtimes", "countries", "statuses", "certifications",
	"parental_nudity", "parental_violence", "parental_profanity",
	"parental_alcohol", "parental_frightening", "parental_include_unrated",
}

func handleUpNextNitro(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	for _, q := range nitroUnsupportedFilters {
		if c.Query(q) != "" {
			return response.BadRequest(c, "filter "+q+" is not supported")
		}
	}

	intent := c.Query("intent", "all")
	switch intent {
	case "all", "continue", "start", "completed":
	default:
		return response.BadRequest(c, "intent must be one of all, continue, start, completed")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	var showIDs []int64
	var err error
	if intent == "start" {
		// "To start": shows on the caller's watchlist they have not watched.
		var wl []int64
		if err := db.Model(&database.UserWatchlist{}).
			Where("user_id = ? AND media_type = ?", userID, "show").
			Pluck("media_id", &wl).Error; err != nil {
			return response.InternalError(c)
		}
		watched, err := historyShowIDs(db, userID)
		if err != nil {
			return response.InternalError(c)
		}
		seen := make(map[int64]bool, len(watched))
		for _, id := range watched {
			seen[id] = true
		}
		for _, id := range wl {
			if !seen[id] {
				showIDs = append(showIDs, id)
			}
		}
	} else {
		showIDs, err = historyShowIDs(db, userID)
		if err != nil {
			return response.InternalError(c)
		}
	}

	rows, err := computeProgressForShows(db, userID, showIDs)
	if err != nil {
		return response.InternalError(c)
	}

	filtered := rows[:0]
	for _, r := range rows {
		switch intent {
		case "continue":
			if r.next != nil && r.completed > 0 && r.completed < r.aired {
				filtered = append(filtered, r)
			}
		case "completed":
			if r.aired > 0 && r.completed >= r.aired {
				filtered = append(filtered, r)
			}
		case "start":
			if r.completed == 0 && r.next != nil {
				filtered = append(filtered, r)
			}
		default: // all
			filtered = append(filtered, r)
		}
	}

	if err := progressSort(c, filtered); err != nil {
		return err
	}

	pageRows, page, limit, total := paginateRows(c, filtered)
	out := make([]map[string]any, 0, len(pageRows))
	for _, r := range pageRows {
		out = append(out, progressItem(r, false))
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// ============================================================================
// GET /sync/progress/watched
// ============================================================================

func handleProgressWatched(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	if queryBool(c, "only_rewatching") {
		return response.BadRequest(c, "only_rewatching is not supported: rewatch counts are not stored")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c)
	}

	showIDs, err := historyShowIDs(db, userID)
	if err != nil {
		return response.InternalError(c)
	}
	rows, err := computeProgressForShows(db, userID, showIDs)
	if err != nil {
		return response.InternalError(c)
	}

	hideCompleted := queryBool(c, "hide_completed")
	hideNotCompleted := queryBool(c, "hide_not_completed")

	filtered := make([]*showProgressRow, 0, len(rows))
	for _, r := range rows {
		complete := r.aired > 0 && r.completed >= r.aired
		if hideCompleted && complete {
			continue
		}
		if hideNotCompleted && !complete {
			continue
		}
		filtered = append(filtered, r)
	}

	if err := progressSort(c, filtered); err != nil {
		return err
	}

	pageRows, page, limit, total := paginateRows(c, filtered)
	includeStats := queryBool(c, "lifetime_stats")

	out := make([]map[string]any, 0, len(pageRows))
	for _, r := range pageRows {
		out = append(out, progressItem(r, includeStats))
	}
	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}
