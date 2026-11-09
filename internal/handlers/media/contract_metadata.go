package media

// Shared plumbing for the TMDB metadata sub-resources (issue #68): videos,
// studios, releases, certifications and season/episode people.
//
// Every route resolves media the same way the rest of the media package does
// (numeric id, slug or ?id_type=external), then serves the data STORED in the
// row. A row with no stored key is hydrated once from TMDB and the fetched
// shape is written back, so the second read makes no provider call. With TMDB
// not configured the route serves a computed empty result — never a fake
// payload.

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// RegisterMetadataRoutes wires the contract paths that issue #68 closes.
func RegisterMetadataRoutes(api fiber.Router) {
	api.Get("/movies/:id/videos", handleMovieVideos)
	api.Get("/shows/:id/videos", handleShowVideos)
	api.Get("/shows/:id/seasons/:season/videos", handleShowSeasonVideos)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/videos", handleShowEpisodeVideos)

	api.Get("/movies/:id/studios", handleMovieStudios)
	api.Get("/shows/:id/studios", handleShowStudios)

	api.Get("/movies/:id/releases/:country", handleMovieReleases)
	api.Get("/shows/:id/certifications", handleShowCertifications)

	api.Get("/shows/:id/seasons/:season/people", handleShowSeasonPeople)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/people", handleShowEpisodePeople)
}

// tmdbClientOrNil returns the process-wide TMDB client, or nil when TMDB is not
// configured. A nil answer makes the read paths serve the stored data (or a
// computed empty result) instead of failing.
func tmdbClientOrNil() *tmdb.Client {
	client, err := getTMDB()
	if err != nil {
		return nil
	}
	return client
}

// hydrateFunc fetches one row's missing sub-resources and returns the updated
// metadata blob.
type hydrateFunc func(client *tmdb.Client) (database.JSONMap, error)

// hydrateMovie and hydrateShow are the title-level hydrators, bound to the row
// they update.
func hydrateMovie(db *gorm.DB, movie *database.Movie) hydrateFunc {
	return func(client *tmdb.Client) (database.JSONMap, error) {
		return metadata.HydrateMovieExtras(db, client, movie)
	}
}

func hydrateShow(db *gorm.DB, show *database.Show) hydrateFunc {
	return func(client *tmdb.Client) (database.JSONMap, error) {
		return metadata.HydrateShowExtras(db, client, show)
	}
}

// stopWith converts an envelope writer's nil-on-success into the sentinel that
// stops a loader. `return response.NotFound(c, ...)` from a loader used to hand
// nil back, so the caller's `if err != nil` never fired and the handler ran on
// with a nil row (a panic on the first field read).
func stopWith(err error) error {
	if err != nil {
		return err
	}
	return response.ErrHandled
}

// serveStoredMetadata serves one stored metadata array, hydrating it once when
// the key is absent.
func serveStoredMetadata(c *fiber.Ctx, client *tmdb.Client, meta database.JSONMap, key string, hydrate hydrateFunc) error {
	if metadata.HasKey(meta, key) || client == nil {
		return c.JSON(metadata.StoredObjects(meta, key))
	}

	updated, err := hydrate(client)
	if err != nil {
		slog.Debug("[media] metadata hydration failed", "key", key, "error", err)
		return response.WriteError(c, fiber.StatusBadGateway, "provider_unavailable",
			"the metadata provider could not be reached")
	}
	return c.JSON(metadata.StoredObjects(updated, key))
}

// --- row resolution ---

func loadMovieRow(c *fiber.Ctx) (*database.Movie, *gorm.DB, error) {
	_, id, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return nil, nil, err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, stopWith(response.InternalError(c, "database unavailable"))
	}

	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, stopWith(response.NotFound(c, "movie not found"))
		}
		return nil, nil, stopWith(response.InternalError(c, "database query error"))
	}
	return &movie, db, nil
}

func loadShowRow(c *fiber.Ctx) (*database.Show, *gorm.DB, error) {
	_, id, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, stopWith(response.InternalError(c, "database unavailable"))
	}

	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, stopWith(response.NotFound(c, "show not found"))
		}
		return nil, nil, stopWith(response.InternalError(c, "database query error"))
	}
	return &show, db, nil
}

// loadSeasonRow resolves a show and one of its seasons. A season number that is
// not numeric and a number the show does not carry are both "no such season",
// which is the contract's answer for a bad season segment.
func loadSeasonRow(c *fiber.Ctx) (*database.Show, *database.Season, *gorm.DB, error) {
	show, db, err := loadShowRow(c)
	if err != nil {
		return nil, nil, nil, err
	}

	seasonNumber, parseErr := strconv.Atoi(c.Params("season"))
	if parseErr != nil {
		return nil, nil, nil, stopWith(response.NotFound(c, "season not found"))
	}

	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, seasonNumber).
		First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, stopWith(response.NotFound(c, "season not found"))
		}
		return nil, nil, nil, stopWith(response.InternalError(c, "database query error"))
	}
	return show, &season, db, nil
}

func loadEpisodeRow(c *fiber.Ctx) (*database.Show, *database.Season, *database.Episode, *gorm.DB, error) {
	show, season, db, err := loadSeasonRow(c)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	episodeNumber, parseErr := strconv.Atoi(c.Params("episode"))
	if parseErr != nil {
		return nil, nil, nil, nil, stopWith(response.NotFound(c, "episode not found"))
	}

	var episode database.Episode
	if err := db.Where("season_id = ? AND episode_number = ?", season.ID, episodeNumber).
		First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, stopWith(response.NotFound(c, "episode not found"))
		}
		return nil, nil, nil, nil, stopWith(response.InternalError(c, "database query error"))
	}
	return show, season, &episode, db, nil
}
