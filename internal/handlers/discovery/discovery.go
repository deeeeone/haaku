package discovery

import (
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	registerOpenAPIRoutes()
}

// registerOpenAPIRoutes registers all discovery endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		// Movies discovery
		{Method: "GET", Path: "/v1/movies/trending", Summary: "Get trending movies", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Trending movies"}},
		{Method: "GET", Path: "/v1/movies/popular", Summary: "Get popular movies", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Popular movies"}},
		{Method: "GET", Path: "/v1/movies/recommendations/{id}", Summary: "Get movie recommendations", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Recommendations"}},

		// Shows discovery
		{Method: "GET", Path: "/v1/shows/trending", Summary: "Get trending shows", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Trending shows"}},
		{Method: "GET", Path: "/v1/shows/popular", Summary: "Get popular shows", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Popular shows"}},
		{Method: "GET", Path: "/v1/shows/recommendations/{id}", Summary: "Get show recommendations", Tags: []string{"Discovery"}, Auth: "none", StatusCodes: map[int]string{200: "Recommendations"}},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all discovery route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	// --- Movies ---
	api.Get("/movies/trending", handleMoviesTrending)
	api.Get("/movies/popular", handleMoviesPopular)
	api.Get("/movies/recommendations/:id", handleMoviesRecommendations)

	// --- Shows ---
	api.Get("/shows/trending", handleShowsTrending)
	api.Get("/shows/popular", handleShowsPopular)
	api.Get("/shows/recommendations/:id", handleShowsRecommendations)
}

// ---------------------------------------------------------------------------
// Movies — Trending / Popular
// ---------------------------------------------------------------------------

func handleMoviesTrending(c *fiber.Ctx) error {
	return moviesList(c, "trending")
}

func handleMoviesPopular(c *fiber.Ctx) error {
	return moviesList(c, "popular")
}

func handleShowsTrending(c *fiber.Ctx) error {
	return showsList(c, "trending")
}

func handleShowsPopular(c *fiber.Ctx) error {
	return showsList(c, "popular")
}

// moviesList queries movies ordered by popularity DESC with pagination.
func moviesList(c *fiber.Ctx, _ string) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)
	extended := media.GetExtended(c)

	var movies []database.Movie
	if err := db.Order("popularity DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&movies).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.MovieResponse, 0, len(movies))
	for i := range movies {
		resp = append(resp, media.BuildMovieResponse(&movies[i], extended))
	}

	return c.JSON(resp)
}

// showsList queries shows ordered by popularity DESC with pagination.
func showsList(c *fiber.Ctx, _ string) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := parsePagination(c)
	extended := media.GetExtended(c)

	var shows []database.Show
	if err := db.Order("popularity DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&shows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.ShowResponse, 0, len(shows))
	for i := range shows {
		resp = append(resp, media.BuildShowResponse(&shows[i], extended))
	}

	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// Recommendations — stubs (Phase 2+)
// ---------------------------------------------------------------------------

func handleMoviesRecommendations(c *fiber.Ctx) error {
	return c.JSON(make([]media.MovieResponse, 0))
}

func handleShowsRecommendations(c *fiber.Ctx) error {
	return c.JSON(make([]media.ShowResponse, 0))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parsePagination extracts page and limit from query params.
// Defaults: page=1, limit=20, max limit=100.
func parsePagination(c *fiber.Ctx) (page, limit int) {
	page = 1
	limit = 20

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
