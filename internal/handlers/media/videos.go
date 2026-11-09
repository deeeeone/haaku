package media

// Video reads (issue #68): the four contract paths that serve TMDB videos.
//
// A title's videos are stored in its metadata blob by the ingest; a season's and
// an episode's videos are stored on their first read (TMDB has no all-episodes
// video endpoint, so an episode costs one request the first time). Nothing here
// calls TMDB once the data is stored.

import (
	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/movies/{id}/videos",
			Summary:     "Get all videos for a movie",
			Tags:        []string{"Movies"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Videos"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/videos",
			Summary:     "Get all videos for a show",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Videos"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/videos",
			Summary:     "Get all videos for a season",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Videos"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/episodes/{episode}/videos",
			Summary:     "Get all videos for an episode",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Videos"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// GET /v1/movies/{id}/videos
func handleMovieVideos(c *fiber.Ctx) error {
	movie, db, err := loadMovieRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), movie.Metadata, metadata.KeyVideos,
		hydrateMovie(db, movie))
}

// GET /v1/shows/{id}/videos
func handleShowVideos(c *fiber.Ctx) error {
	show, db, err := loadShowRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), show.Metadata, metadata.KeyVideos,
		hydrateShow(db, show))
}

// GET /v1/shows/{id}/seasons/{season}/videos
func handleShowSeasonVideos(c *fiber.Ctx) error {
	show, season, db, err := loadSeasonRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), season.Metadata, metadata.KeyVideos,
		func(client *tmdb.Client) (database.JSONMap, error) {
			return metadata.HydrateSeasonExtras(db, client, show, season)
		})
}

// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/videos
func handleShowEpisodeVideos(c *fiber.Ctx) error {
	show, season, episode, db, err := loadEpisodeRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), episode.Metadata, metadata.KeyVideos,
		func(client *tmdb.Client) (database.JSONMap, error) {
			return metadata.HydrateEpisodeExtras(db, client, show, season, episode)
		})
}
