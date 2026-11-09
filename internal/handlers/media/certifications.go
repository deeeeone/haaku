package media

// Per-country show certifications (issue #68): TMDB content_ratings rendered as
// the contract's {certification, country} list. The list is stored in the
// show's metadata blob; an older row is hydrated on its first read.

import (
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/shows/{id}/certifications",
		Summary:     "Get all show certifications, one per country",
		Tags:        []string{"Shows"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Certifications"},
	})
}

// GET /v1/shows/{id}/certifications
func handleShowCertifications(c *fiber.Ctx) error {
	show, db, err := loadShowRow(c)
	if err != nil {
		return err
	}
	return serveStoredMetadata(c, tmdbClientOrNil(), show.Metadata, metadata.KeyCertifications,
		hydrateShow(db, show))
}
