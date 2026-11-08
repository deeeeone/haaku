package media

import (
	"errors"
	"strconv"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/comments"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Media-scoped comment lists (Trakt: GET /{media}/comments/{sort})
//
// Each handler resolves its parent chain with real lookups and 404s on a
// missing parent. Note that the response helpers can return a nil error while
// still having written the status (Fiber's JSON write returns nil), so every
// "not found" branch returns its response directly from the handler and the
// shared season lookup reports found=false instead of relying on the error.
// ---------------------------------------------------------------------------

// GET /v1/movies/{id}/comments/{sort}
func handleGetMovieComments(c *fiber.Ctx) error {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	return comments.ListMediaComments(c, db, "movie", id, c.Params("sort"))
}

// GET /v1/shows/{id}/comments/{sort}
func handleGetShowComments(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	return comments.ListMediaComments(c, db, "show", showID, c.Params("sort"))
}

// GET /v1/shows/{id}/seasons/{season}/comments/{sort}
func handleGetShowSeasonComments(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	season, found, err := lookupShowSeason(c, db, showID)
	if !found {
		return err
	}

	return comments.ListMediaComments(c, db, "season", season.ID, c.Params("sort"))
}

// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/comments/{sort}
func handleGetShowEpisodeComments(c *fiber.Ctx) error {
	_, showID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.InternalError(c, "database unavailable")
	}

	season, found, err := lookupShowSeason(c, db, showID)
	if !found {
		return err
	}

	episodeNum, parseErr := strconv.Atoi(c.Params("episode"))
	if parseErr != nil {
		return response.BadRequest(c, "invalid episode number")
	}

	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNum).
		First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "episode not found")
		}
		return response.InternalError(c, "database query error")
	}

	return comments.ListMediaComments(c, db, "episode", episode.ID, c.Params("sort"))
}

// lookupShowSeason resolves {season} as a season number on the show. On any
// failure it writes the response itself and returns found=false; callers must
// then return the (possibly nil) error directly.
func lookupShowSeason(c *fiber.Ctx, db *gorm.DB, showID int64) (*database.Season, bool, error) {
	seasonNum, err := strconv.Atoi(c.Params("season"))
	if err != nil {
		return nil, false, response.BadRequest(c, "invalid season number")
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", showID, seasonNum).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, response.NotFound(c, "season not found")
		}
		return nil, false, response.InternalError(c, "database query error")
	}
	return &season, true, nil
}
