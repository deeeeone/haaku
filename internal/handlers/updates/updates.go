package updates

import (
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"trakt-api/internal/response"
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
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)
	extended := media.GetExtended(c)

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
		resp = append(resp, media.BuildMovieResponse(&m, extended))
	}

	return c.JSON(resp)
}

// handleShowUpdates returns shows updated since start_date.
func handleShowUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)
	extended := media.GetExtended(c)

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
		resp = append(resp, media.BuildShowResponse(&s, extended))
	}

	return c.JSON(resp)
}

// handlePeopleUpdates returns people updated since start_date.
func handlePeopleUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	page, limit := response.ParsePagination(c, 25)

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
		resp = append(resp, media.BuildPersonResponse(&p))
	}

	return c.JSON(resp)
}

// --- Error helpers ---
