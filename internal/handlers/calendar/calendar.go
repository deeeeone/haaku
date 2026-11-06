package calendar

import (
	"fmt"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/auth"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

// registerOpenAPIRoutes registers calendar endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/calendars/my/shows/{start_date}/{days}",
			Summary: "Get your upcoming show calendar (shows you've watched or watchlisted)",
			Tags:    []string{"Calendars"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Calendar entries",
				400: "Invalid parameters",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/all/shows/{start_date}/{days}",
			Summary: "Get all upcoming episodes airing within a date range",
			Tags:    []string{"Calendars"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Calendar entries",
				400: "Invalid start_date or days parameter",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/friends/shows/{start_date}/{days}",
			Summary: "Get upcoming episodes from shows your mutual follows have watched",
			Tags:    []string{"Calendars"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Calendar entries",
				400: "Invalid parameters",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/favorites/shows/{start_date}/{days}",
			Summary: "Get upcoming episodes from your favorited shows",
			Tags:    []string{"Calendars"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Calendar entries",
				400: "Invalid parameters",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/watched/shows/{start_date}/{days}",
			Summary: "Get upcoming episodes from shows you've watched",
			Tags:    []string{"Calendars"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Calendar entries",
				400: "Invalid parameters",
				401: "Unauthorized",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/all/movies/{start_date}/{days}",
			Summary: "Get all movies releasing within a date range",
			Tags:    []string{"Calendars"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Calendar movie entries",
				400: "Invalid start_date or days parameter",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/calendars/my/movies/{start_date}/{days}",
			Summary: "Get movies releasing within a date range from your watchlist or history",
			Tags:    []string{"Calendars"},
			Auth:    "required",
			StatusCodes: map[int]string{
				200: "Calendar movie entries",
				400: "Invalid parameters",
				401: "Unauthorized",
			},
		},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires calendar route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	calendars := api.Group("/calendars")

	// Public: all shows
	calendars.Get("/all/shows/:start_date/:days", handleCalendarShows)

	// Public: all movies
	calendars.Get("/all/movies/:start_date/:days", handleCalendarMovies)

	// Auth-required: my, friends, favorites, watched
	calendars.Get("/my/shows/:start_date/:days", auth.AuthRequired(), handleCalendarShows)
	calendars.Get("/friends/shows/:start_date/:days", auth.AuthRequired(), handleCalendarShows)
	calendars.Get("/favorites/shows/:start_date/:days", auth.AuthRequired(), handleCalendarShows)
	calendars.Get("/watched/shows/:start_date/:days", auth.AuthRequired(), handleCalendarShows)

	// Auth-required: my movies (watchlist + history, hidden excluded)
	calendars.Get("/my/movies/:start_date/:days", auth.AuthRequired(), handleCalendarMovies)
}

// ---------------------------------------------------------------------------
// Auth middleware wrapper
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// CalendarEntry is the response shape for a calendar item.
type CalendarEntry struct {
	Episode media.EpisodeResponse `json:"episode"`
	Show    CalendarShow          `json:"show"`
	Season  CalendarSeason        `json:"season"`
}

// CalendarShow is the minimal show info within a calendar entry.
type CalendarShow struct {
	Title string    `json:"title"`
	IDs   media.IDs `json:"ids"`
}

// CalendarSeason is the minimal season info within a calendar entry.
type CalendarSeason struct {
	Number int `json:"number"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleCalendarShows is the unified calendar handler for all targets.
// Path params: start_date (YYYY-MM-DD), days (1-30, default 7)
// Query params: type (new|premieres|finale), page, limit
// Target is derived from the route path via c.Params.
func handleCalendarShows(c *fiber.Ctx) error {
	// Derive target from the URL path.
	target := deriveTarget(c.Path())

	startDateStr := c.Params("start_date")
	daysStr := c.Params("days")
	typeFilter := c.Query("type", "")

	// Validate start_date.
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	// Parse and clamp days.
	days := clampDays(daysStr)
	endDate := startDate.AddDate(0, 0, days)

	// Validate type filter.
	if typeFilter != "" && typeFilter != "new" && typeFilter != "premieres" && typeFilter != "finale" {
		return response.BadRequest(c, "invalid type filter, expected new, premieres, or finale")
	}

	// Auth is enforced at route registration for non-"all" targets; the
	// public /all route just gets an empty userID here.
	userID, _ := c.Locals("user_id").(string)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)

	// Build the base episode query.
	query := db.Table("episodes e").
		Select("e.*").
		Where("e.aired_at >= ? AND e.aired_at < ?",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"))

	// Apply type filter (requires season join for new/premieres).
	switch typeFilter {
	case "new":
		query = query.Joins("JOIN seasons s ON s.id = e.season_id").
			Where("s.season_number = 1 AND e.episode_number = 1")
	case "premieres":
		query = query.Joins("JOIN seasons s ON s.id = e.season_id").
			Where("s.season_number > 1 AND e.episode_number = 1")
	case "finale":
		query = query.Where(`(e.season_id, e.episode_number) IN (
			SELECT season_id, MAX(episode_number) FROM episodes GROUP BY season_id
		)`)
	}

	// Apply target filter.
	if target != "all" {
		showIDs := getShowIDsForTarget(db, target, userID)
		if len(showIDs) == 0 {
			return c.JSON([]CalendarEntry{})
		}
		query = query.Where("e.show_id IN ?", showIDs)
	}

	// Count, then paginate.
	var total int64
	query.Count(&total)

	var episodes []database.Episode
	if err := query.
		Order("e.aired_at ASC, e.episode_number ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&episodes).Error; err != nil {
		fmt.Printf("calendar: database query error: %v\n", err)
		return response.InternalError(c)
	}

	// Batch-fetch the shows and seasons referenced by this page in two
	// `WHERE id IN (...)` queries, then map in memory — replacing the previous
	// per-episode `db.First` (N+1) lookups for show and season.
	var showIDs, seasonIDs []int64
	for _, ep := range episodes {
		showIDs = append(showIDs, ep.ShowID)
		seasonIDs = append(seasonIDs, ep.SeasonID)
	}

	showMap := make(map[int64]database.Show, len(showIDs))
	if len(showIDs) > 0 {
		var shows []database.Show
		if err := db.Where("id IN ?", showIDs).Find(&shows).Error; err == nil {
			for _, s := range shows {
				showMap[s.ID] = s
			}
		}
	}

	seasonMap := make(map[int64]database.Season, len(seasonIDs))
	if len(seasonIDs) > 0 {
		var seasons []database.Season
		if err := db.Where("id IN ?", seasonIDs).Find(&seasons).Error; err == nil {
			for _, s := range seasons {
				seasonMap[s.ID] = s
			}
		}
	}

	// Build response, merging season_number from seasons table.
	resp := make([]CalendarEntry, 0, len(episodes))
	for _, ep := range episodes {
		show, ok := showMap[ep.ShowID]
		if !ok {
			continue
		}

		seasonNumber := 0
		if season, ok := seasonMap[ep.SeasonID]; ok {
			seasonNumber = season.SeasonNumber
		}

		entry := CalendarEntry{
			Episode: buildEpisode(&ep),
			Show: CalendarShow{
				Title: show.Title,
				IDs:   buildShowIDs(&show),
			},
			Season: CalendarSeason{
				Number: seasonNumber,
			},
		}
		resp = append(resp, entry)
	}

	// Return with pagination envelope.
	return c.JSON(fiber.Map{
		"items":     resp,
		"page":      page,
		"limit":     limit,
		"row_count": total,
	})
}

// deriveTarget extracts the calendar target from the request path.
// e.g. "/v1/calendars/my/shows/2026-01-01/7" → "my"
func deriveTarget(path string) string {
	// Path format: /v1/calendars/{target}/shows/...
	// Simple extraction between "calendars/" and "/shows"
	idx := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			idx++
			if idx == 3 { // after /v1/calendars/
				start := i + 1
				// Find next /
				end := start
				for end < len(path) && path[end] != '/' {
					end++
				}
				return path[start:end]
			}
		}
	}
	return "all"
}

// getShowIDsForTarget returns the set of show IDs for a given calendar target.
func getShowIDsForTarget(db *gorm.DB, target, userID string) []int64 {
	var showIDs []int64

	switch target {
	case "my":
		// Shows user has watched (via episodes) OR on watchlist.
		// Exclude hidden (section='calendar').

		// Subquery 1: show IDs from history (via episodes).
		var historyShowIDs []int64
		db.Table("user_history uh").
			Joins("JOIN episodes e ON e.id = uh.media_id AND uh.media_type = 'episode'").
			Where("uh.user_id = ?", userID).
			Distinct("e.show_id").
			Pluck("e.show_id", &historyShowIDs)

		// Subquery 2: show IDs from watchlist.
		var watchlistShowIDs []int64
		db.Table("user_watchlist").
			Where("user_id = ? AND media_type = ?", userID, "show").
			Pluck("media_id", &watchlistShowIDs)

		// Union uniquely.
		showIDMap := make(map[int64]bool)
		for _, id := range historyShowIDs {
			showIDMap[id] = true
		}
		for _, id := range watchlistShowIDs {
			showIDMap[id] = true
		}

		// Exclude hidden.
		excludeHiddenShowIDs(db, userID, "calendar", "show", showIDMap)

		for id := range showIDMap {
			showIDs = append(showIDs, id)
		}

	case "friends":
		// Mutual follows: A follows B AND B follows A, both approved.
		// Get friends who the user mutually follows.
		var friendIDs []string
		db.Raw(`
			SELECT f1.following_id FROM user_follows f1
			JOIN user_follows f2 ON f1.following_id = f2.follower_id AND f2.following_id = f1.follower_id
			WHERE f1.follower_id = ?
			  AND f1.approved_at IS NOT NULL
			  AND f2.approved_at IS NOT NULL
		`, userID).Pluck("following_id", &friendIDs)

		if len(friendIDs) == 0 {
			return nil
		}

		// Shows those friends have watched in the last 90 days.
		cutoff := time.Now().AddDate(0, 0, -90)
		showIDMap := make(map[int64]bool)
		var friendShowIDs []int64
		db.Table("user_history uh").
			Joins("JOIN episodes e ON e.id = uh.media_id AND uh.media_type = 'episode'").
			Where("uh.user_id IN ? AND uh.watched_at >= ?", friendIDs, cutoff).
			Distinct("e.show_id").
			Pluck("e.show_id", &friendShowIDs)
		for _, id := range friendShowIDs {
			showIDMap[id] = true
		}

		// Exclude user's hidden items.
		excludeHiddenShowIDs(db, userID, "calendar", "show", showIDMap)

		for id := range showIDMap {
			showIDs = append(showIDs, id)
		}

	case "favorites":
		// Shows the user has favorited.
		var favShowIDs []int64
		db.Table("user_favorites").
			Where("user_id = ? AND media_type = ?", userID, "show").
			Pluck("media_id", &favShowIDs)

		showIDMap := make(map[int64]bool)
		for _, id := range favShowIDs {
			showIDMap[id] = true
		}

		// Apply the same hidden-show exclusion as the other user-scoped
		// targets: hiding a show in the calendar section means it should not
		// surface in any of the user's calendar views, favorites included.
		excludeHiddenShowIDs(db, userID, "calendar", "show", showIDMap)

		for id := range showIDMap {
			showIDs = append(showIDs, id)
		}

	case "watched":
		// Shows user has watched (same as my but no watchlist).
		var historyShowIDs []int64
		db.Table("user_history uh").
			Joins("JOIN episodes e ON e.id = uh.media_id AND uh.media_type = 'episode'").
			Where("uh.user_id = ?", userID).
			Distinct("e.show_id").
			Pluck("e.show_id", &historyShowIDs)

		showIDMap := make(map[int64]bool)
		for _, id := range historyShowIDs {
			showIDMap[id] = true
		}

		// Hidden exclusion is applied consistently across every user-scoped
		// target (my/friends/favorites/watched): a show hidden in the calendar
		// section must not reappear in the watched calendar view either.
		excludeHiddenShowIDs(db, userID, "calendar", "show", showIDMap)

		for id := range showIDMap {
			showIDs = append(showIDs, id)
		}
	}

	return showIDs
}

// excludeHiddenShowIDs removes show IDs that are hidden in a given section.
func excludeHiddenShowIDs(db *gorm.DB, userID, section, mediaType string, showIDMap map[int64]bool) {
	var hiddenIDs []int64
	db.Table("user_hidden").
		Where("user_id = ? AND section = ? AND media_type = ?", userID, section, mediaType).
		Pluck("media_id", &hiddenIDs)
	for _, id := range hiddenIDs {
		delete(showIDMap, id)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// clampDays parses the days parameter, enforcing bounds 1-30 and default 7.
func clampDays(input string) int {
	if input == "" {
		return 7
	}
	d := 0
	for _, c := range input {
		if c >= '0' && c <= '9' {
			d = d*10 + int(c-'0')
		} else {
			return 7 // invalid
		}
	}
	if d < 1 {
		return 1
	}
	if d > 30 {
		return 30
	}
	return d
}

// buildEpisode constructs a media.EpisodeResponse from a database.Episode.
func buildEpisode(e *database.Episode) media.EpisodeResponse {
	resp := media.EpisodeResponse{
		IDs:            media.IDs{ID: e.ID},
		SeasonID:       e.SeasonID,
		ShowID:         e.ShowID,
		EpisodeNumber:  e.EpisodeNumber,
		Title:          e.Title,
		Overview:       e.Overview,
		Runtime:        e.Runtime,
		Images:         e.Images,
		AbsoluteNumber: e.AbsoluteNumber,
	}
	if e.AiredAt != nil {
		resp.AiredAt = e.AiredAt.String()
	}
	if resp.Images == nil {
		resp.Images = make(map[string]any)
	}
	return resp
}

// buildShowIDs builds a media.IDs object for a show.
func buildShowIDs(s *database.Show) media.IDs {
	ids := media.IDs{ID: s.ID}
	if s.TmdbID != 0 {
		ids.TMDB = s.TmdbID
	}
	if s.ImdbID != "" {
		ids.IMDB = s.ImdbID
	}
	return ids
}

// ---------------------------------------------------------------------------
// Movie calendars (Trakt: /calendars/{target}/movies/{start_date}/{days})
// ---------------------------------------------------------------------------

// CalendarMovieEntry is one movie calendar item. It matches Trakt's calendar
// movie shape — {"released": "YYYY-MM-DD", "movie": {...}} — rather than the
// {items, page, limit, row_count} envelope the pre-existing show calendars use,
// which is a section-B divergence tracked for the compatibility layer.
type CalendarMovieEntry struct {
	Released string              `json:"released"`
	Movie    media.MovieResponse `json:"movie"`
}

// handleCalendarMovies serves the movie calendars: /calendars/all/movies (public)
// and /calendars/my/movies (auth). Path params: start_date (YYYY-MM-DD), days
// (1-30, default 7). The window is [start_date, start_date+days).
//
// "my" is restricted to movies the caller has watched or watchlisted (movies
// hidden in the "calendar" section are excluded); "all" is public.
func handleCalendarMovies(c *fiber.Ctx) error {
	target := deriveTarget(c.Path())

	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}
	days := clampDays(c.Params("days"))
	endDate := startDate.AddDate(0, 0, days)

	// Auth is enforced at route registration for "my"; the public "all" route
	// just gets an empty userID here.
	userID, _ := c.Locals("user_id").(string)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)

	// Resolve the per-user movie filter once, then build count and page queries
	// from it (GORM finishers mutate the statement, so they must not share one).
	var movieIDs []int64
	if target != "all" {
		movieIDs = getMovieIDsForUser(db, userID)
		if len(movieIDs) == 0 {
			response.SetPaginationHeaders(c, page, limit, 0, 0)
			return c.JSON([]CalendarMovieEntry{})
		}
	}

	build := func() *gorm.DB {
		q := db.Model(&database.Movie{}).
			Where("released >= ? AND released < ?",
				startDate.Format("2006-01-02"),
				endDate.Format("2006-01-02"))
		if movieIDs != nil {
			q = q.Where("id IN ?", movieIDs)
		}
		return q
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		fmt.Printf("calendar: movie count error: %v\n", err)
		return response.InternalError(c)
	}

	var movies []database.Movie
	if err := build().
		Order("released ASC, id ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&movies).Error; err != nil {
		fmt.Printf("calendar: movie query error: %v\n", err)
		return response.InternalError(c)
	}

	extended := media.GetExtended(c)
	resp := make([]CalendarMovieEntry, 0, len(movies))
	for i := range movies {
		m := movies[i]
		entry := CalendarMovieEntry{Movie: media.BuildMovieResponse(&m, extended)}
		if m.Released != nil {
			entry.Released = m.Released.String()
		}
		resp = append(resp, entry)
	}

	response.SetPaginationHeaders(c, page, limit, total, len(resp))
	return c.JSON(resp)
}

// getMovieIDsForUser returns the canonical movie IDs the user has watched or
// watchlisted, excluding movies hidden in the "calendar" section. It mirrors
// getShowIDsForTarget's "my" target (history ∪ watchlist, hidden removed).
func getMovieIDsForUser(db *gorm.DB, userID string) []int64 {
	idSet := make(map[int64]bool)

	var historyMovieIDs []int64
	db.Table("user_history").
		Where("user_id = ? AND media_type = ?", userID, "movie").
		Distinct("media_id").
		Pluck("media_id", &historyMovieIDs)

	var watchlistMovieIDs []int64
	db.Table("user_watchlist").
		Where("user_id = ? AND media_type = ?", userID, "movie").
		Pluck("media_id", &watchlistMovieIDs)

	for _, id := range historyMovieIDs {
		idSet[id] = true
	}
	for _, id := range watchlistMovieIDs {
		idSet[id] = true
	}

	excludeHiddenMovieIDs(db, userID, idSet)

	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	return ids
}

// excludeHiddenMovieIDs removes movie IDs hidden in the "calendar" section.
func excludeHiddenMovieIDs(db *gorm.DB, userID string, idSet map[int64]bool) {
	var hiddenIDs []int64
	db.Table("user_hidden").
		Where("user_id = ? AND section = ? AND media_type = ?", userID, "calendar", "movie").
		Pluck("media_id", &hiddenIDs)
	for _, id := range hiddenIDs {
		delete(idSet, id)
	}
}
