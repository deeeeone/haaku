package media

// Contract-shaped "all translations" reads for a movie and an episode.
//
// Trakt serves every language under /translations and takes the language as an
// optional ?language= query; the existing /{lang} routes filter TMDB live and do
// not store anything. These routes store the contract-shaped translation array
// in the row's metadata once (the 5a hydration pattern) and serve it on every
// later read, so a title's translations cost at most one provider call.

import (
	"log/slog"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
)

func init() {
	routes := []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/movies/{id}/translations",
			Summary:     "Get all movie translations, filtered by the optional ?language=",
			Tags:        []string{"Movies"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Translations", 502: "Provider unavailable"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/seasons/{season}/episodes/{episode}/translations",
			Summary:     "Get all episode translations, filtered by the optional ?language=",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "Translations", 502: "Provider unavailable"},
		},
	}
	for _, route := range routes {
		openapi.Register(route)
	}
}

// registerContractTranslationAllRoutes mounts the all-languages reads.
func registerContractTranslationAllRoutes(api fiber.Router) {
	api.Get("/movies/:id/translations", handleContractMovieTranslations)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/translations", handleContractEpisodeTranslations)
}

// handleContractMovieTranslations serves GET /v1/movies/{id}/translations.
func handleContractMovieTranslations(c *fiber.Ctx) error {
	movie, db, err := loadMovieRow(c)
	if err != nil {
		return err
	}
	client := tmdbClientOrNil()
	if !metadata.HasKey(movie.Metadata, metadata.KeyTranslations) && client != nil {
		updated, herr := metadata.HydrateMovieTranslations(db, client, movie)
		if herr != nil {
			return translationHydrationError(c, herr)
		}
		movie.Metadata = updated
	}
	return c.JSON(filterStoredTranslations(movie.Metadata, c.Query("language")))
}

// handleContractEpisodeTranslations serves
// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/translations.
func handleContractEpisodeTranslations(c *fiber.Ctx) error {
	show, season, episode, db, err := loadEpisodeRow(c)
	if err != nil {
		return err
	}
	client := tmdbClientOrNil()
	if !metadata.HasKey(episode.Metadata, metadata.KeyTranslations) && client != nil {
		updated, herr := metadata.HydrateEpisodeTranslations(db, client, show, season, episode)
		if herr != nil {
			return translationHydrationError(c, herr)
		}
		episode.Metadata = updated
	}
	return c.JSON(filterStoredTranslations(episode.Metadata, c.Query("language")))
}

// filterStoredTranslations reads the stored translation array and applies the
// contract's optional language filter. An empty lang means every language; a
// value matches either the language or the country code, which is how Trakt's
// regional variants are addressed.
func filterStoredTranslations(meta database.JSONMap, lang string) []map[string]any {
	all := metadata.StoredObjects(meta, metadata.KeyTranslations)
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return all
	}
	out := make([]map[string]any, 0, len(all))
	for _, item := range all {
		language, _ := item["language"].(string)
		country, _ := item["country"].(string)
		if strings.ToLower(language) == lang || strings.ToLower(country) == lang {
			out = append(out, item)
		}
	}
	return out
}

// translationHydrationError logs the provider failure and returns the same 502
// the rest of the metadata reads use. A provider outage is not "no
// translations".
func translationHydrationError(c *fiber.Ctx, err error) error {
	slog.Debug("[media] translations hydration failed", "error", err)
	return response.WriteError(c, fiber.StatusBadGateway, "provider_unavailable",
		"the metadata provider could not be reached")
}
