package media

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
)

// Contract-shaped season translations.
//
// Trakt's /shows/{id}/seasons/{season}/translations takes the language as an
// optional ?language= query param (all languages when omitted); our route
// requires it as a {lang} path segment. This route resolves the same TMDB
// season translations and emits the contract's array shape.

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/shows/{id}/seasons/{season}/translations",
		Summary: "Season translations, all languages unless ?language= is set (contract path)",
		Tags:    []string{"Seasons"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Translations",
			500: "Upstream provider error",
		},
	})
}

// handleContractSeasonTranslations serves GET /v1/shows/{id}/seasons/{season}/translations.
func handleContractSeasonTranslations(c *fiber.Ctx) error {
	tmdbID, err := resolveShowTmdbID(c)
	if err != nil {
		return err
	}
	if tmdbID == 0 {
		// resolveShowTmdbID reports the 404 itself; calling TMDB with id 0 would
		// replace that honest answer with an upstream error.
		return response.ErrHandled
	}
	seasonNum := c.Params("season")
	lang := strings.ToLower(strings.TrimSpace(c.Query("language")))

	raw, err := callTMDB(fmt.Sprintf("/tv/%d/season/%s/translations", tmdbID, seasonNum))
	if err != nil {
		return tmdbUpstreamError(c, "season translations", tmdbID, err)
	}
	return c.JSON(parseContractTranslations(raw, lang))
}

// parseContractTranslations decodes TMDB's translations payload into the
// contract's {title, overview, tagline, language, country} array. An empty lang
// returns every language.
func parseContractTranslations(raw []byte, lang string) []map[string]any {
	var resp tmdbTranslationsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(resp.Translations))
	for _, t := range resp.Translations {
		if lang != "" && strings.ToLower(t.ISO639) != lang && strings.ToLower(t.ISO3166) != lang {
			continue
		}
		title := t.Data.Title
		if title == "" {
			title = t.Data.Name
		}
		out = append(out, map[string]any{
			"title":    nullableString(title),
			"overview": nullableString(t.Data.Overview),
			"tagline":  nullableString(t.Data.Tagline),
			"language": t.ISO639,
			"country":  nullableString(t.ISO3166),
		})
	}
	return out
}

// nullableString / ContractTranslations imports kept local to avoid a package
// cycle with contractshape (which does not depend on TMDB types).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
