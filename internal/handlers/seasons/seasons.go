package seasons

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	projredis "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/seasons/{id}", Summary: "Get a season with show embedded and stats", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Season detail", 404: "Not found"}},
		{Method: "GET", Path: "/v1/seasons/{id}/stats", Summary: "Season stats (watchers, plays, ratings)", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Stats", 404: "Not found"}},
		{Method: "GET", Path: "/v1/seasons/{id}/ratings", Summary: "Season rating distribution", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Ratings", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires all season route handlers.
func RegisterRoutes(api fiber.Router) {
	sg := api.Group("/seasons")

	sg.Get("/:id", handleGetSeason)
	sg.Get("/:id/stats", handleGetSeasonStats)
	sg.Get("/:id/ratings", handleGetSeasonRatings)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SeasonDetailResponse is the full season detail with embedded show.
type SeasonDetailResponse struct {
	media.SeasonResponse `json:",inline"`

	Show  *media.ShowResponse  `json:"show"`
	Stats *SeasonStatsResponse `json:"stats"`
}

// SeasonStatsResponse holds public stats for a season.
type SeasonStatsResponse struct {
	Watchers      int64   `json:"watchers"`
	Plays         int64   `json:"plays"`
	EpisodesCount int64   `json:"episodes_count"`
	Rating        float64 `json:"rating"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// GET /v1/seasons/{id}
func handleGetSeason(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "season")
	if err != nil {
		return err
	}

	extended := media.GetExtended(c)
	cacheKey := seasonCacheKey("detail", id, extended)

	var cached SeasonDetailResponse
	if hit, _ := seasonGetCache(cacheKey, &cached); hit {
		return c.JSON(cached)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var season database.Season
	if err := db.First(&season, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	resp := SeasonDetailResponse{
		SeasonResponse: media.BuildSeasonResponse(&season),
	}

	// Fetch show.
	var show database.Show
	if err := db.First(&show, season.ShowID).Error; err == nil {
		sr := media.BuildShowResponse(&show, extended)
		resp.Show = &sr
	}

	// Stats.
	stats, qerr := querySeasonStats(db, season.ID)
	if qerr != nil {
		fmt.Printf("seasons: query stats for season %d failed: %v\n", season.ID, qerr)
		return response.InternalError(c)
	}
	resp.Stats = stats

	seasonSetCache(cacheKey, resp, seasonDetailTTL)
	return c.JSON(resp)
}

// GET /v1/seasons/{id}/stats
func handleGetSeasonStats(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "season")
	if err != nil {
		return err
	}

	cacheKey := seasonCacheKey("stats", id, "")

	var cachedStats SeasonStatsResponse
	if hit, _ := seasonGetCache(cacheKey, &cachedStats); hit {
		return c.JSON(cachedStats)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var season database.Season
	if err := db.First(&season, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	stats, qerr := querySeasonStats(db, id)
	if qerr != nil {
		fmt.Printf("seasons: query stats for season %d failed: %v\n", id, qerr)
		return response.InternalError(c)
	}
	seasonSetCache(cacheKey, stats, seasonStatsTTL)
	return c.JSON(stats)
}

// GET /v1/seasons/{id}/ratings
func handleGetSeasonRatings(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "season")
	if err != nil {
		return err
	}

	cacheKey := seasonCacheKey("ratings", id, "")

	var cachedDist media.RatingSummary
	if hit, _ := seasonGetCache(cacheKey, &cachedDist); hit {
		return c.JSON(cachedDist)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	var season database.Season
	if err := db.First(&season, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "season not found")
		}
		return response.WriteError(c, fiber.StatusInternalServerError, "internal", "database query error")
	}

	dist, qerr := queryRatingDistribution(db, "season", id)
	if qerr != nil {
		fmt.Printf("seasons: query rating distribution for season %d failed: %v\n", id, qerr)
		return response.InternalError(c)
	}
	seasonSetCache(cacheKey, dist, seasonRatingsTTL)
	return c.JSON(dist)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func querySeasonStats(db *gorm.DB, seasonID int64) (*SeasonStatsResponse, error) {
	// Watchers: distinct users who watched episodes in this season.
	var watchers int64
	if err := db.Model(&database.UserHistory{}).Where("season_id = ?", seasonID).
		Distinct("user_id").Count(&watchers).Error; err != nil {
		return nil, fmt.Errorf("count watchers: %w", err)
	}

	// Plays: total watches of episodes in this season.
	var plays int64
	if err := db.Model(&database.UserHistory{}).Where("season_id = ?", seasonID).
		Count(&plays).Error; err != nil {
		return nil, fmt.Errorf("count plays: %w", err)
	}

	// Episodes count in this season.
	var episodeCount int64
	if err := db.Model(&database.Episode{}).Where("season_id = ?", seasonID).
		Count(&episodeCount).Error; err != nil {
		return nil, fmt.Errorf("count episodes: %w", err)
	}

	// Average rating for this season.
	var avgRating float64
	if err := db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", "season", seasonID).
		Select("COALESCE(AVG(rating), 0)").Scan(&avgRating).Error; err != nil {
		return nil, fmt.Errorf("avg rating: %w", err)
	}

	return &SeasonStatsResponse{
		Watchers:      watchers,
		Plays:         plays,
		EpisodesCount: episodeCount,
		Rating:        avgRating,
	}, nil
}

func queryRatingDistribution(db *gorm.DB, mediaType string, mediaID int64) (*media.RatingSummary, error) {
	var avgRating float64
	if err := db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Select("COALESCE(AVG(rating), 0)").Scan(&avgRating).Error; err != nil {
		return nil, fmt.Errorf("avg rating: %w", err)
	}

	var count int64
	if err := db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("count ratings: %w", err)
	}

	distribution := make(map[string]int64)
	var rows []struct {
		Rating int
		Count  int64
	}
	if err := db.Model(&database.UserRating{}).
		Select("rating, count(*) as count").
		Where("media_type = ? AND media_id = ?", mediaType, mediaID).
		Group("rating").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("group ratings: %w", err)
	}
	for _, r := range rows {
		distribution[strconv.Itoa(r.Rating)] = r.Count
	}

	return &media.RatingSummary{
		Rating:       avgRating,
		Votes:        count,
		Distribution: distribution,
	}, nil
}

// ---------------------------------------------------------------------------
// Redis TTL cache for stable season reads
// ---------------------------------------------------------------------------

// Detail responses (season + embedded show + stats) are stable enough to cache
// longer; stats/ratings recompute from user activity so use a shorter TTL.
const (
	seasonDetailTTL  = 30 * time.Minute
	seasonStatsTTL   = 15 * time.Minute
	seasonRatingsTTL = 15 * time.Minute
)

// seasonCacheKey builds a stable Redis key. Detail keyed by extended level
// (affects the embedded show); stats/ratings keyed by id only.
func seasonCacheKey(kind string, id int64, extended string) string {
	if extended != "" {
		return fmt.Sprintf("season:%s:%d:extended%s", kind, id, extended)
	}
	return fmt.Sprintf("season:%s:%d", kind, id)
}

// seasonGetCache unmarshals a cached JSON value into dst. Returns (true, nil) on
// a hit; (false, err) on a miss or Redis unavailability so callers fall through
// to the DB. The cache is best-effort: every caller ignores the error and reads
// through, so a Redis failure can never fail the request.
func seasonGetCache(key string, dst interface{}) (bool, error) {
	if projredis.GetClient() == nil {
		return false, nil
	}
	data, err := projredis.GetCacheData(key)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		log.Printf("[seasons] cache unmarshal error for key %s: %v", key, err)
		return false, nil
	}
	return true, nil
}

// seasonSetCache marshals v and stores it under key with the given TTL.
// Best-effort: a Redis failure only logs.
func seasonSetCache(key string, v interface{}, ttl time.Duration) {
	if projredis.GetClient() == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[seasons] cache marshal error for key %s: %v", key, err)
		return
	}
	if err := projredis.SetCacheData(key, data, ttl); err != nil {
		log.Printf("[seasons] cache set error for key %s: %v", key, err)
	}
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------
