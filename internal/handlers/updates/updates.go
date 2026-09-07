package updates

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
	projredis "trakt-api/redis"

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

	page, limit := response.ParsePagination(c, 25)
	extended := media.GetExtended(c)

	cacheKey := updatesFeedCacheKey("movies", startDate, page, limit, extended)
	if enc, ok := updatesGetCache(cacheKey); ok {
		setPaginationHeaders(c, page, limit, enc.Total, enc.Count)
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
	setPaginationHeaders(c, page, limit, total, len(resp))
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
		setPaginationHeaders(c, page, limit, enc.Total, enc.Count)
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
	setPaginationHeaders(c, page, limit, total, len(resp))
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
		setPaginationHeaders(c, page, limit, enc.Total, enc.Count)
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
	setPaginationHeaders(c, page, limit, total, len(resp))
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

// setPaginationHeaders emits the §7 pagination headers (api-conventions.md)
// plus a Link: rel="next" header when more pages exist.
func setPaginationHeaders(c *fiber.Ctx, page, limit int, total int64, itemsCount int) {
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(itemsCount))
	if page < int(totalPages) {
		c.Set("Link", `<`+c.Path()+`?page=`+strconv.Itoa(page+1)+`&limit=`+strconv.Itoa(limit)+`>; rel="next"`)
	}
}
