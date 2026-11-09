package episodes

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped nested episode reads.
//
// Trakt nests episode ratings and stats under the show:
// /shows/{id}/seasons/{season}/episodes/{episode}/{ratings,stats}. Our flat
// equivalents (/v1/episodes/{id}/ratings, /v1/episodes/{id}/stats) use our
// internal episode id; these routes resolve show+season+episode numbers to that
// id and emit the contract's shapes. Every count is computed from stored rows —
// ratings live in user_ratings, plays in user_history, collection in
// user_collection, comments in comments, lists in list_items, favourites in
// user_favorites.

func init() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/ratings", Summary: "Episode rating distribution (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Ratings", 400: "Invalid season or episode number", 404: "Show, season or episode not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/stats", Summary: "Episode stats (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Stats", 400: "Invalid season or episode number", 404: "Show, season or episode not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// registerContractEpisodeStatsRoutes mounts the contract-shaped nested episode
// reads. Route order matters: these are static leaf segments under the same
// :episode parameter as the detail route, so they are registered on the same
// prefix the episodes package already owns.
func registerContractEpisodeStatsRoutes(api fiber.Router) {
	api.Get("/shows/:id/seasons/:season/episodes/:episode/ratings", handleContractEpisodeRatings)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/stats", handleContractEpisodeStats)
}

// resolveNestedEpisode turns the contract's show+season+episode segments into
// our episode row. The show id type is known (show), so ResolveIDAs is used —
// a type-blind resolver would resolve a colliding movie id instead.
func resolveNestedEpisode(c *fiber.Ctx) (*gorm.DB, *database.Episode, error) {
	_, showID, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, err
	}

	seasonNum, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil || seasonNum < 0 {
		_ = response.BadRequest(c, "invalid season number")
		return nil, nil, response.ErrHandled
	}
	episodeNum, aerr := strconv.Atoi(c.Params("episode"))
	if aerr != nil || episodeNum < 0 {
		_ = response.BadRequest(c, "invalid episode number")
		return nil, nil, response.ErrHandled
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		_ = response.InternalError(c)
		return nil, nil, response.ErrHandled
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.NotFound(c, "season not found")
			return nil, nil, response.ErrHandled
		}
		_ = response.InternalError(c)
		return nil, nil, response.ErrHandled
	}

	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNum).First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = response.NotFound(c, "episode not found")
			return nil, nil, response.ErrHandled
		}
		_ = response.InternalError(c)
		return nil, nil, response.ErrHandled
	}
	return db, &episode, nil
}

// handleContractEpisodeStats serves
// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/stats.
//
// The contract requires watchers, plays, collectors, comments, lists and votes.
// All six are counts over rows we store; none is a constant. (Recommended and
// favorited belong to the movie/show Stats object, not the episode one.)
func handleContractEpisodeStats(c *fiber.Ctx) error {
	db, episode, err := resolveNestedEpisode(c)
	if err != nil {
		return err
	}

	var watchers, plays, collectors, comments, lists, votes int64
	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Distinct("user_id").Count(&watchers).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.UserHistory{}).
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Count(&plays).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.UserCollection{}).
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Distinct("user_id").Count(&collectors).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.Comment{}).
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Count(&comments).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.ListItem{}).
		Joins("JOIN lists ON lists.id = list_items.list_id").
		Where("list_items.media_type = ? AND list_items.media_id = ?", "episode", episode.ID).
		Where("lists.privacy IN ?", []string{"public", "link_only"}).
		Count(&lists).Error; err != nil {
		return response.InternalError(c)
	}
	if err := db.Model(&database.UserRating{}).
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Count(&votes).Error; err != nil {
		return response.InternalError(c)
	}

	return c.JSON(map[string]any{
		"watchers":   watchers,
		"plays":      plays,
		"collectors": collectors,
		"comments":   comments,
		"lists":      lists,
		"votes":      votes,
	})
}

// handleContractEpisodeRatings serves
// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/ratings.
//
// The distribution is the share of votes in each 1..10 bucket, as the contract
// specifies (the flat /v1/episodes/{id}/ratings route predates the contract and
// serves raw counts). rating is the stored average, or null when nobody has
// rated the episode: null says "no rating", whereas 0.0 would claim a real
// one-star verdict.
func handleContractEpisodeRatings(c *fiber.Ctx) error {
	db, episode, err := resolveNestedEpisode(c)
	if err != nil {
		return err
	}

	var rows []struct {
		Rating int
		Count  int64
	}
	if err := db.Model(&database.UserRating{}).
		Select("rating, count(*) as count").
		Where("media_type = ? AND media_id = ?", "episode", episode.ID).
		Group("rating").
		Scan(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	var total int64
	for _, r := range rows {
		total += r.Count
	}

	distribution := make(map[string]float64, 10)
	for i := 1; i <= 10; i++ {
		distribution[strconv.Itoa(i)] = 0
	}
	for _, r := range rows {
		if total > 0 {
			distribution[strconv.Itoa(r.Rating)] = float64(r.Count) / float64(total) * 100
		}
	}

	var rating *float64
	if total > 0 {
		var avg float64
		if err := db.Model(&database.UserRating{}).
			Where("media_type = ? AND media_id = ?", "episode", episode.ID).
			Select("COALESCE(AVG(rating), 0)").
			Scan(&avg).Error; err != nil {
			return response.InternalError(c)
		}
		rating = &avg
	}

	return c.JSON(map[string]any{
		"rating":       rating,
		"votes":        total,
		"distribution": distribution,
	})
}
