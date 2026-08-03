package calendar

import (
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	registerOpenAPIRoutes()
}

// registerOpenAPIRoutes registers calendar endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/calendars/my/shows",
			Summary: "Get your upcoming show calendar (stub — requires user scrobble data in Phase 2+)",
			Tags:    []string{"Calendars"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Calendar entries (empty for now)",
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
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires calendar route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	calendars := api.Group("/calendars")
	calendars.Get("/my/shows", handleMyShows)
	calendars.Get("/all/shows/:start_date/:days", handleAllShows)
}

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

// handleMyShows returns the user's personal calendar. Stub until user scrobble
// data is available in Phase 2+.
func handleMyShows(c *fiber.Ctx) error {
	return c.JSON([]CalendarEntry{})
}

// handleAllShows returns all episodes airing between start_date and
// start_date+days, ordered by air date then episode number.
//
// Query params: page, limit
// Path params: start_date (YYYY-MM-DD), days (1-30, default 7)
func handleAllShows(c *fiber.Ctx) error {
	startDateStr := c.Params("start_date")
	daysStr := c.Params("days")

	// Parse start_date.
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return badRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	// Parse and clamp days.
	days := clampDays(daysStr)
	endDate := startDate.AddDate(0, 0, days)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)

	// Query episodes airing in [startDate, endDate).
	var episodes []database.Episode
	if err := db.Where("aired_at >= ? AND aired_at < ?",
		startDate.Format("2006-01-02"),
		endDate.Format("2006-01-02"),
	).
		Order("aired_at ASC, episode_number ASC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&episodes).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]CalendarEntry, 0, len(episodes))
	for _, ep := range episodes {
		// Fetch the show for title + IDs.
		var show database.Show
		if err := db.First(&show, ep.ShowID).Error; err != nil {
			continue // skip if show deleted
		}

		// Fetch the season for the season number.
		var season database.Season
		if err := db.First(&season, ep.SeasonID).Error; err != nil {
			season.SeasonNumber = 0 // fallback
		}

		entry := CalendarEntry{
			Episode: buildEpisode(&ep),
			Show: CalendarShow{
				Title: show.Title,
				IDs:   buildShowIDs(&show),
			},
			Season: CalendarSeason{
				Number: season.SeasonNumber,
			},
		}
		resp = append(resp, entry)
	}

	return c.JSON(resp)
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

// parsePagination extracts page and limit from query params (same as media package).
func parsePagination(c *fiber.Ctx) (page, limit int) {
	page = 1
	limit = 25

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

// --- Error helpers ---

func notFound(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    "not_found",
			"message": msg,
		},
	})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    "invalid_request",
			"message": msg,
		},
	})
}
