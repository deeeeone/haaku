package updates

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
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
		{
			Method:  "GET",
			Path:    "/v1/movies/updates/id/{start_date}",
			Summary: "Get IDs of movies updated since a date",
			Tags:    []string{"Updates"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Updated movie IDs",
				400: "Invalid start_date format",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/shows/updates/id/{start_date}",
			Summary: "Get IDs of shows updated since a date",
			Tags:    []string{"Updates"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Updated show IDs",
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
	movies.Get("/updates/id/:start_date", handleMovieUpdatedIDs) // static segment first
	movies.Get("/updates/:start_date", handleMovieUpdates)

	shows := api.Group("/shows")
	shows.Get("/updates/id/:start_date", handleShowUpdatedIDs) // static segment first
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

	page, limit := response.ParsePagination(c, 25)
	extended := media.GetExtended(c)

	cacheKey := updatesFeedCacheKey("movies", startDate, page, limit, extended)
	if enc, ok := updatesGetCache(cacheKey); ok {
		response.SetPaginationHeaders(c, page, limit, enc.Total, enc.Count)
		c.Set("Content-Type", "application/json")
		return c.Send(enc.Body)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var total int64
	if err := db.Model(&database.Movie{}).Where("updated_at >= ?", startDate).Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database count error")
	}

	var movies []database.Movie
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&movies).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.MovieResponse, 0, len(movies))
	for _, m := range movies {
		resp = append(resp, media.BuildMovieResponse(&m, extended))
	}

	updatesSetCache(cacheKey, updatesCacheEnvelope{Total: total, Count: len(resp), Body: mustMarshal(resp)})
	response.SetPaginationHeaders(c, page, limit, total, len(resp))
	return c.JSON(resp)
}

// handleShowUpdates returns shows updated since start_date.
func handleShowUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	page, limit := response.ParsePagination(c, 25)
	extended := media.GetExtended(c)

	cacheKey := updatesFeedCacheKey("shows", startDate, page, limit, extended)
	if enc, ok := updatesGetCache(cacheKey); ok {
		response.SetPaginationHeaders(c, page, limit, enc.Total, enc.Count)
		c.Set("Content-Type", "application/json")
		return c.Send(enc.Body)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var total int64
	if err := db.Model(&database.Show{}).Where("updated_at >= ?", startDate).Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database count error")
	}

	var shows []database.Show
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&shows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.ShowResponse, 0, len(shows))
	for _, s := range shows {
		resp = append(resp, media.BuildShowResponse(&s, extended))
	}

	updatesSetCache(cacheKey, updatesCacheEnvelope{Total: total, Count: len(resp), Body: mustMarshal(resp)})
	response.SetPaginationHeaders(c, page, limit, total, len(resp))
	return c.JSON(resp)
}

// handlePeopleUpdates returns people updated since start_date.
func handlePeopleUpdates(c *fiber.Ctx) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	page, limit := response.ParsePagination(c, 25)

	cacheKey := updatesFeedCacheKey("people", startDate, page, limit, "")
	if enc, ok := updatesGetCache(cacheKey); ok {
		response.SetPaginationHeaders(c, page, limit, enc.Total, enc.Count)
		c.Set("Content-Type", "application/json")
		return c.Send(enc.Body)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var total int64
	if err := db.Model(&database.Person{}).Where("updated_at >= ?", startDate).Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database count error")
	}

	var people []database.Person
	if err := db.Where("updated_at >= ?", startDate).
		Order("updated_at DESC, id DESC"). // stable order for pagination
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&people).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	resp := make([]media.PersonResponse, 0, len(people))
	for _, p := range people {
		resp = append(resp, media.BuildPersonResponse(&p))
	}

	updatesSetCache(cacheKey, updatesCacheEnvelope{Total: total, Count: len(resp), Body: mustMarshal(resp)})
	response.SetPaginationHeaders(c, page, limit, total, len(resp))
	return c.JSON(resp)
}

// ---------------------------------------------------------------------------
// Redis TTL cache for update feeds
// ---------------------------------------------------------------------------

// updatesFeedTTL keeps the sync-scraper public feeds reasonably fresh while
// absorbing bursts of polling traffic.
const updatesFeedTTL = 90 * time.Second

// updatesCacheEnvelope stores everything a cached update feed needs to rebuild
// both the pagination headers and the JSON body on a cache hit.
type updatesCacheEnvelope struct {
	Total int64           `json:"total"`
	Count int             `json:"count"`
	Body  json.RawMessage `json:"body"`
}

func updatesFeedCacheKey(mediaType string, startDate time.Time, page, limit int, extended string) string {
	date := startDate.Format("2006-01-02")
	if extended != "" {
		return fmt.Sprintf("updates:%s:%s:page%d:limit%d:extended%s", mediaType, date, page, limit, extended)
	}
	return fmt.Sprintf("updates:%s:%s:page%d:limit%d", mediaType, date, page, limit)
}

// updatesGetCache returns a cached envelope on a hit; (zero, false) on miss or
// Redis unavailability so callers fall through to the DB.
func updatesGetCache(key string) (updatesCacheEnvelope, bool) {
	var enc updatesCacheEnvelope
	if projredis.GetClient() == nil {
		return enc, false
	}
	data, err := projredis.GetCacheData(key)
	if err != nil {
		return enc, false
	}
	if err := json.Unmarshal(data, &enc); err != nil {
		log.Printf("[updates] cache unmarshal error for key %s: %v", key, err)
		return enc, false
	}
	return enc, true
}

// updatesSetCache stores an envelope with updatesFeedTTL. Best-effort.
func updatesSetCache(key string, enc updatesCacheEnvelope) {
	if projredis.GetClient() == nil {
		return
	}
	data, err := json.Marshal(enc)
	if err != nil {
		log.Printf("[updates] cache marshal error for key %s: %v", key, err)
		return
	}
	if err := projredis.SetCacheData(key, data, updatesFeedTTL); err != nil {
		log.Printf("[updates] cache set error for key %s: %v", key, err)
	}
}

// mustMarshal serializes v to JSON; falls back to "[]" on error so caching never
// panics on an unmarshalable response slice.
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("[]")
	}
	return data
}

// ---------------------------------------------------------------------------
// Updated-ID feeds
//
// Trakt's GET /movies/updates/id/:start_date and /shows/updates/id/:start_date
// return a bare array of Trakt IDs — the cheap "what changed" poll a sync
// client uses before fetching only the entities it does not already have.
// Thin read over the same `updated_at` column the full feeds use; no engine.
// ---------------------------------------------------------------------------

// handleMovieUpdatedIDs returns the IDs of movies updated since start_date.
func handleMovieUpdatedIDs(c *fiber.Ctx) error {
	return handleUpdatedIDs(c, &database.Movie{})
}

// handleShowUpdatedIDs returns the IDs of shows updated since start_date.
func handleShowUpdatedIDs(c *fiber.Ctx) error {
	return handleUpdatedIDs(c, &database.Show{})
}

// handleUpdatedIDs is the shared body for the two updated-ID feeds. It parses
// start_date, paginates, and returns the matching canonical IDs as a bare JSON
// array (never null — an empty result is []).
func handleUpdatedIDs(c *fiber.Ctx, model interface{}) error {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}

	page, limit := response.ParsePagination(c, 25)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	var total int64
	if err := db.Model(model).Where("updated_at >= ?", startDate).Count(&total).Error; err != nil {
		return response.InternalError(c, "database count error")
	}

	ids := make([]int64, 0, limit)
	if err := db.Model(model).
		Where("updated_at >= ?", startDate).
		Order("updated_at ASC, id ASC"). // stable order for pagination
		Offset((page-1)*limit).
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return response.InternalError(c, "database query error")
	}

	response.SetPaginationHeaders(c, page, limit, total, len(ids))
	return c.JSON(ids)
}
