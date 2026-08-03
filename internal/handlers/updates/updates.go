package updates

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

// registerOpenAPIRoutes registers updates endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/movies/updates/{start_date}",
			Summary: "Get movies updated since a date",
			Tags:    []string{"Updates"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Updated movies",
				400: "Invalid start_date format",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/shows/updates/{start_date}",
			Summary: "Get shows updated since a date",
			Tags:    []string{"Updates"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Updated shows",
				400: "Invalid start_date format",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/people/updates/{start_date}",
			Summary: "Get people updated since a date",
			Tags:    []string{"Updates"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Updated people",
				400: "Invalid start_date format",
			},
		},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires updates route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	movies := api.Group("/movies")
	movies.Get("/updates/:start_date", handleMovieUpdates)

	shows := api.Group("/shows")
	shows.Get("/updates/:start_date", handleShowUpdates)

	people := api.Group("/people")
	people.Get("/updates/:start_date", handlePeopleUpdates)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleMovieUpdates returns movies updated since start_date.
func handleMovieUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return badRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)
	extended := getExtended(c)

	var movies []database.Movie
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&movies).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.MovieResponse, 0, len(movies))
	for _, m := range movies {
		resp = append(resp, buildMovieResponse(&m, extended))
	}

	return c.JSON(resp)
}

// handleShowUpdates returns shows updated since start_date.
func handleShowUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return badRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)
	extended := getExtended(c)

	var shows []database.Show
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&shows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.ShowResponse, 0, len(shows))
	for _, s := range shows {
		resp = append(resp, buildShowResponse(&s, extended))
	}

	return c.JSON(resp)
}

// handlePeopleUpdates returns people updated since start_date.
func handlePeopleUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return badRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)

	var people []database.Person
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&people).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.PersonResponse, 0, len(people))
	for _, p := range people {
		resp = append(resp, buildPersonResponse(&p))
	}

	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parsePagination extracts page and limit from query params.
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

// getExtended returns the extended level from query params, defaulting to "min".
func getExtended(c *fiber.Ctx) string {
	ext := c.Query("extended", "min")
	switch ext {
	case "min", "full", "metadata", "images", "all":
		return ext
	default:
		return "min"
	}
}

// buildMovieResponse builds a MovieResponse from a database Movie.
func buildMovieResponse(m *database.Movie, extended string) media.MovieResponse {
	year := 0
	if m.Released != nil && !m.Released.IsZero() {
		year = m.Released.Year()
	}

	resp := media.MovieResponse{
		IDs:    buildMovieIDs(m),
		Title:  m.Title,
		Year:   year,
		Images: m.Images,
		RatingSummary: &media.RatingSummary{
			Rating:       m.RatingAvg,
			Votes:        m.RatingCount,
			Distribution: make(map[string]int64),
		},
	}

	switch extended {
	case "full", "all":
		resp.Overview = m.Overview
		resp.Tagline = m.Tagline
		resp.Runtime = m.Runtime
		resp.Genres = m.Genres
		resp.Certification = m.Certification
		resp.Language = m.Language
		resp.TrailerURL = m.TrailerURL
		resp.Status = m.Status
		fallthrough
	case "metadata":
		resp.Metadata = m.Metadata
	case "images":
	}

	if extended == "all" {
		resp.Overview = m.Overview
		resp.Tagline = m.Tagline
		resp.Runtime = m.Runtime
		resp.Genres = m.Genres
		resp.Certification = m.Certification
		resp.Language = m.Language
		resp.TrailerURL = m.TrailerURL
		resp.Status = m.Status
		resp.Metadata = m.Metadata
	}

	return resp
}

// buildShowResponse builds a ShowResponse from a database Show.
func buildShowResponse(s *database.Show, extended string) media.ShowResponse {
	year := 0
	if s.FirstAired != nil && !s.FirstAired.IsZero() {
		year = s.FirstAired.Year()
	}

	resp := media.ShowResponse{
		IDs:    buildShowIDs(s),
		Title:  s.Title,
		Year:   year,
		Images: s.Images,
		RatingSummary: &media.RatingSummary{
			Rating:       s.RatingAvg,
			Votes:        s.RatingCount,
			Distribution: make(map[string]int64),
		},
		Seasons: make([]media.SeasonResponse, 0),
	}

	switch extended {
	case "full", "all":
		resp.Overview = s.Overview
		resp.Genres = s.Genres
		resp.Certification = s.Certification
		resp.Language = s.Language
		resp.Network = s.Network
		resp.Status = s.Status
		resp.EpisodeCount = s.EpisodeCount
		resp.SeasonCount = s.SeasonCount
		if s.FirstAired != nil {
			resp.FirstAired = s.FirstAired.String()
		}
		if s.LastAired != nil {
			resp.LastAired = s.LastAired.String()
		}
		fallthrough
	case "metadata":
		resp.Metadata = s.Metadata
	case "images":
	}

	if extended == "all" {
		resp.Overview = s.Overview
		resp.Genres = s.Genres
		resp.Certification = s.Certification
		resp.Language = s.Language
		resp.Network = s.Network
		resp.Status = s.Status
		resp.EpisodeCount = s.EpisodeCount
		resp.SeasonCount = s.SeasonCount
		if s.FirstAired != nil {
			resp.FirstAired = s.FirstAired.String()
		}
		if s.LastAired != nil {
			resp.LastAired = s.LastAired.String()
		}
		resp.Metadata = s.Metadata
	}

	return resp
}

// buildPersonResponse builds a PersonResponse from a database Person.
func buildPersonResponse(p *database.Person) media.PersonResponse {
	resp := media.PersonResponse{
		IDs:                media.IDs{ID: p.ID, TMDB: p.TmdbID, IMDB: p.ImdbID},
		Name:               p.Name,
		Biography:          p.Biography,
		Gender:             p.Gender,
		KnownForDepartment: p.KnownForDepartment,
		Images:             p.Images,
	}
	if p.Birthday != nil {
		resp.Birthday = p.Birthday.String()
	}
	if p.Deathday != nil {
		resp.Deathday = p.Deathday.String()
	}
	if resp.Images == nil {
		resp.Images = make(map[string]any)
	}
	return resp
}

// buildMovieIDs builds a media.IDs object for a movie.
func buildMovieIDs(m *database.Movie) media.IDs {
	ids := media.IDs{ID: m.ID}
	if m.TmdbID != 0 {
		ids.TMDB = m.TmdbID
	}
	if m.ImdbID != "" {
		ids.IMDB = m.ImdbID
	}
	return ids
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
