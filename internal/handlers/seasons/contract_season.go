package seasons

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped nested season reads.
//
// Trakt nests season ratings, stats and translations under the show:
// /shows/{id}/seasons/{season}/{ratings,stats,translations}. Our equivalents
// are flat (/v1/seasons/{id}/ratings, /v1/seasons/{id}/stats) and use our
// internal season id. These routes resolve show+season_number to that id and
// emit the contract's shapes.

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/ratings", Summary: "Season rating distribution (contract path)", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Ratings", 400: "Invalid season number", 404: "Show or season not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/stats", Summary: "Season stats (contract path)", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Stats", 400: "Invalid season number", 404: "Show or season not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/info", Summary: "Single season summary (contract path)", Tags: []string{"Seasons"}, Auth: "none", StatusCodes: map[int]string{200: "Season"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// registerContractSeasonRoutes mounts the contract-shaped nested season reads.
func registerContractSeasonRoutes(api fiber.Router) {
	api.Get("/shows/:id/seasons/:season/ratings", handleContractSeasonRatings)
	api.Get("/shows/:id/seasons/:season/stats", handleContractSeasonStats)
	api.Get("/shows/:id/seasons/:season/info", handleContractSeasonInfo)
}

// handleContractSeasonInfo serves GET /v1/shows/{id}/seasons/{season}/info in
// the contract's season shape. Every full-extended field the row can answer is
// emitted; fields it does not hold are null, as the schema allows.
func handleContractSeasonInfo(c *fiber.Ctx) error {
	_, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}
	if season == nil {
		return nil
	}
	return c.JSON(contractshape.Season(season))
}

// resolveShowSeason turns the contract's show+season segment into our season
// row. The show's id type is known (show), so ResolveIDAs is used — a type-blind
// resolver would deaden one media type's pages.
func resolveShowSeason(c *fiber.Ctx) (*gorm.DB, *database.Season, error) {
	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, err
	}
	seasonNum, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil || seasonNum < 0 {
		_ = response.BadRequest(c, "invalid season number")
		return nil, nil, response.ErrHandled
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, response.InternalError(c)
	}
	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.NotFound(c, "season not found")
			return nil, nil, response.ErrHandled
		}
		return nil, nil, response.InternalError(c)
	}
	return db, &season, nil
}

// handleContractSeasonRatings serves GET /v1/shows/{id}/seasons/{season}/ratings.
func handleContractSeasonRatings(c *fiber.Ctx) error {
	db, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}

	distribution := map[string]float64{}
	for i := 1; i <= 10; i++ {
		distribution[strconv.Itoa(i)] = 0
	}

	var rows []struct {
		Rating int
		Count  int64
	}
	if err := db.Model(&database.UserRating{}).
		Select("rating, count(*) as count").
		Where("media_type = ? AND media_id = ?", "season", season.ID).
		Group("rating").
		Scan(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	var total int64
	for _, r := range rows {
		total += r.Count
	}
	for _, r := range rows {
		if total > 0 {
			distribution[strconv.Itoa(r.Rating)] = float64(r.Count) / float64(total) * 100
		}
	}

	return c.JSON(map[string]any{
		"rating":       nil,
		"votes":        total,
		"distribution": distribution,
	})
}

// handleContractSeasonStats serves GET /v1/shows/{id}/seasons/{season}/stats.
//
// Every count is computed from stored rows. collectors is 0 because
// user_collection stores movie/show rows only, not seasons — there is no
// season collection row that could exist.
func handleContractSeasonStats(c *fiber.Ctx) error {
	db, season, err := resolveShowSeason(c)
	if err != nil {
		return err
	}

	var watchers, plays int64
	if err := db.Model(&database.UserHistory{}).Where("season_id = ?", season.ID).
		Distinct("user_id").Count(&watchers).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.UserHistory{}).Where("season_id = ?", season.ID).
		Count(&plays).Error; err != nil {
		return response.InternalError(c)
	}

	var comments, lists, votes int64
	if err := db.Model(&database.Comment{}).Where("media_type = ? AND media_id = ?", "season", season.ID).
		Count(&comments).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.ListItem{}).Where("media_type = ? AND media_id = ?", "season", season.ID).
		Count(&lists).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.UserRating{}).Where("media_type = ? AND media_id = ?", "season", season.ID).
		Count(&votes).Error; err != nil {
		return response.InternalError(c)
	}

	return c.JSON(map[string]any{
		"watchers":   watchers,
		"plays":      plays,
		"collectors": 0,
		"comments":   comments,
		"lists":      lists,
		"votes":      votes,
	})
}
