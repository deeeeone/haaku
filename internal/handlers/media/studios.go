package media

// Studio reads (issue #68): TMDB production_companies rendered as the
// contract's studio object. The list is stored in the title's metadata blob at
// ingest time; a title ingested before this change is hydrated on its first
// read.

import (
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/movies/{id}/studios",
			Summary:     "Get all studios for a movie",
			Tags:        []string{"Movies"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Studios"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/studios",
			Summary:     "Get all studios for a show",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Studios"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// GET /v1/movies/{id}/studios
func handleMovieStudios(c *fiber.Ctx) error {
	movie, db, err := loadMovieRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), movie.Metadata, metadata.KeyStudios,
		hydrateMovie(db, movie))
}

// GET /v1/shows/{id}/studios
func handleShowStudios(c *fiber.Ctx) error {
	show, db, err := loadShowRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), show.Metadata, metadata.KeyStudios,
		hydrateShow(db, show))
}
