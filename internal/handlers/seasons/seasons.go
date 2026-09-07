package seasons

import (
	"errors"
	"fmt"
	"strconv"

	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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
		extended := media.GetExtended(c)
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

	return c.JSON(resp)
}

// GET /v1/seasons/{id}/stats
func handleGetSeasonStats(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "season")
	if err != nil {
		return err
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
	return c.JSON(stats)
}

// GET /v1/seasons/{id}/ratings
func handleGetSeasonRatings(c *fiber.Ctx) error {
	id, err := response.ParseIDParam(c, "season")
	if err != nil {
		return err
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
// Error helpers
// ---------------------------------------------------------------------------
