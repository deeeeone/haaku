package media

// Per-country movie releases (issue #68): TMDB's release_dates flattened into
// the contract's release rows. `{country}` is a two-letter code (case
// insensitive) or `all`, which returns every country TMDB carries.

import (
	"strings"

	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/movies/{id}/releases/{country}",
		Summary:     "Get all movie releases for a country (or all)",
		Tags:        []string{"Movies"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Releases"},
	})
}

// GET /v1/movies/{id}/releases/{country}
func handleMovieReleases(c *fiber.Ctx) error {
	movie, db, err := loadMovieRow(c)
	if err != nil {
		return err
	}

	country := strings.ToLower(strings.TrimSpace(c.Params("country")))
	client := tmdbClientOrNil()
	meta := movie.Metadata

	if !metadata.HasKey(meta, metadata.KeyReleases) {
		if client == nil {
			return c.JSON(metadata.StoredObjects(meta, metadata.KeyReleases))
		}
		meta, err = metadata.HydrateMovieExtras(db, client, movie)
		if err != nil {
			return providerUnavailable(c, metadata.KeyReleases, err)
		}
	}

	releases := metadata.StoredObjects(meta, metadata.KeyReleases)
	if country == "all" {
		return c.JSON(releases)
	}

	out := make([]map[string]any, 0, len(releases))
	for _, release := range releases {
		if strings.EqualFold(releaseCountry(release), country) {
			out = append(out, release)
		}
	}
	return c.JSON(out)
}

// releaseCountry reads the stored country field defensively: the value comes
// out of a JSONB blob, so it is a string on a normal read but a fmt.Sprint is
// enough here and keeps a malformed row from panicking the handler.
func releaseCountry(release map[string]any) string {
	if value, ok := release["country"].(string); ok {
		return value
	}
	return ""
}
