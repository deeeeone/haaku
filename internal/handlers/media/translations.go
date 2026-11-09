package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
	"gorm.io/gorm"
)

// tmdbClientSingleton holds the lazily-initialized TMDB client.
var (
	tmdbOnce            sync.Once
	tmdbClientSingleton *tmdb.Client
)

func getTMDB() (*tmdb.Client, error) {
	var err error
	tmdbOnce.Do(func() {
		tmdbClientSingleton, err = tmdb.New()
	})
	return tmdbClientSingleton, err
}

// ---------------------------------------------------------------------------
// OpenAPI registration
// ---------------------------------------------------------------------------

func init() {
	registerTranslationOpenAPIRoutes()
}

func registerTranslationOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/movies/{id}/translations/{lang}", Summary: "Movie translations for a language", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/translations/{lang}", Summary: "Show translations for a language", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/translations/{lang}", Summary: "Season translations for a language", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/translations/{lang}", Summary: "Episode translations for a language", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Not found"}},
		{Method: "GET", Path: "/v1/movies/{id}/aliases", Summary: "Movie alternative titles", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Aliases", 404: "Not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/aliases", Summary: "Show alternative titles", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Aliases", 404: "Not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterTranslationRoutes wires translation and alias routes.
func RegisterTranslationRoutes(api fiber.Router) {
	api.Get("/movies/:id/translations/:lang", handleMovieTranslations)
	api.Get("/shows/:id/translations/:lang", handleShowTranslations)
	api.Get("/shows/:id/seasons/:season/translations/:lang", handleShowSeasonTranslations)
	api.Get("/shows/:id/seasons/:season/translations", handleContractSeasonTranslations)
	api.Get("/shows/:id/seasons/:season/episodes/:episode/translations/:lang", handleShowEpisodeTranslations)
	api.Get("/movies/:id/aliases", handleMovieAliases)
	api.Get("/shows/:id/aliases", handleShowAliases)
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// TranslationResponse is the simplified translation response.
type TranslationResponse struct {
	Language string `json:"language"`
	Title    string `json:"title"`
	Overview string `json:"overview"`
	Tagline  string `json:"tagline,omitempty"`
}

// AliasResponse is a simplified alternative title.
type AliasResponse struct {
	Title   string `json:"title"`
	Country string `json:"country"`
}

// TMDB raw response types for translations.
type tmdbTranslationData struct {
	Title    string `json:"title"`
	Overview string `json:"overview"`
	Name     string `json:"name"`
	Tagline  string `json:"tagline"`
}

type tmdbTranslation struct {
	ISO3166 string              `json:"iso_3166_1"`
	ISO639  string              `json:"iso_639_1"`
	Name    string              `json:"name"`
	English string              `json:"english_name"`
	Data    tmdbTranslationData `json:"data"`
}

type tmdbTranslationsResponse struct {
	Translations []tmdbTranslation `json:"translations"`
}

// TMDB raw response types for alternative titles.
type tmdbAlternativeTitle struct {
	ISO3166 string `json:"iso_3166_1"`
	Title   string `json:"title"`
	Type    string `json:"type"`
}

type tmdbAlternativeTitlesResponse struct {
	Titles []tmdbAlternativeTitle `json:"titles"`
}

// ---------------------------------------------------------------------------
// Translation handlers
// ---------------------------------------------------------------------------

// GET /v1/movies/{id}/translations/{lang}
func handleMovieTranslations(c *fiber.Ctx) error {
	tmdbID, err := resolveMovieTmdbID(c)
	if err != nil {
		return err
	}
	lang := c.Params("lang")
	cacheKey := fmt.Sprintf("translations:movie:%d:%s", tmdbID, lang)

	return proxyTranslations(c, cacheKey, lang, fmt.Sprintf("/movie/%d/translations", tmdbID))
}

// GET /v1/shows/{id}/translations/{lang}
func handleShowTranslations(c *fiber.Ctx) error {
	tmdbID, err := resolveShowTmdbID(c)
	if err != nil {
		return err
	}
	lang := c.Params("lang")
	cacheKey := fmt.Sprintf("translations:show:%d:%s", tmdbID, lang)

	return proxyTranslations(c, cacheKey, lang, fmt.Sprintf("/tv/%d/translations", tmdbID))
}

// GET /v1/shows/{id}/seasons/{season}/translations/{lang}
func handleShowSeasonTranslations(c *fiber.Ctx) error {
	tmdbID, err := resolveShowTmdbID(c)
	if err != nil {
		return err
	}
	seasonNum := c.Params("season")
	lang := c.Params("lang")
	cacheKey := fmt.Sprintf("translations:season:%d:%s:%s", tmdbID, seasonNum, lang)

	return proxyTranslations(c, cacheKey, lang,
		fmt.Sprintf("/tv/%d/season/%s/translations", tmdbID, seasonNum))
}

// GET /v1/shows/{id}/seasons/{season}/episodes/{episode}/translations/{lang}
func handleShowEpisodeTranslations(c *fiber.Ctx) error {
	tmdbID, err := resolveShowTmdbID(c)
	if err != nil {
		return err
	}
	seasonNum := c.Params("season")
	episodeNum := c.Params("episode")
	lang := c.Params("lang")
	cacheKey := fmt.Sprintf("translations:episode:%d:%s:%s:%s", tmdbID, seasonNum, episodeNum, lang)

	return proxyTranslations(c, cacheKey, lang,
		fmt.Sprintf("/tv/%d/season/%s/episode/%s/translations", tmdbID, seasonNum, episodeNum))
}

// ---------------------------------------------------------------------------
// Alias handlers
// ---------------------------------------------------------------------------

// GET /v1/movies/{id}/aliases
func handleMovieAliases(c *fiber.Ctx) error {
	tmdbID, err := resolveMovieTmdbID(c)
	if err != nil {
		return err
	}
	cacheKey := fmt.Sprintf("aliases:movie:%d", tmdbID)

	if cached := getCachedAliases(cacheKey); cached != nil {
		return c.JSON(cached)
	}

	raw, err := callTMDB(fmt.Sprintf("/movie/%d/alternative_titles", tmdbID))
	if err != nil {
		return tmdbUpstreamError(c, "aliases", tmdbID, err)
	}

	aliases := parseAliases(raw)
	setCachedAliases(cacheKey, aliases)
	return c.JSON(aliases)
}

// GET /v1/shows/{id}/aliases
func handleShowAliases(c *fiber.Ctx) error {
	tmdbID, err := resolveShowTmdbID(c)
	if err != nil {
		return err
	}
	cacheKey := fmt.Sprintf("aliases:show:%d", tmdbID)

	if cached := getCachedAliases(cacheKey); cached != nil {
		return c.JSON(cached)
	}

	raw, err := callTMDB(fmt.Sprintf("/tv/%d/alternative_titles", tmdbID))
	if err != nil {
		return tmdbUpstreamError(c, "aliases", tmdbID, err)
	}

	aliases := parseAliases(raw)
	setCachedAliases(cacheKey, aliases)
	return c.JSON(aliases)
}

// ---------------------------------------------------------------------------
// Core proxy logic
// ---------------------------------------------------------------------------

func proxyTranslations(c *fiber.Ctx, cacheKey, lang, tmdbPath string) error {
	if cached := getCachedTranslations(cacheKey, lang); cached != nil {
		return c.JSON(cached)
	}

	raw, err := callTMDB(tmdbPath)
	if err != nil {
		return tmdbUpstreamError(c, tmdbPath, 0, err)
	}

	trans, err := filterTranslations(raw, lang)
	if err != nil {
		return response.InternalError(c, "failed to parse translations")
	}

	setCachedTranslations(cacheKey, trans)
	return c.JSON(trans)
}

// callTMDB wraps the TMDB v3 call with client init.
func callTMDB(path string) ([]byte, error) {
	client, err := getTMDB()
	if err != nil {
		return nil, fmt.Errorf("tmdb client unavailable: %w", err)
	}
	return client.GetV3(path)
}

// tmdbUpstreamError logs the raw upstream TMDB failure server-side and returns
// a generic 500 internal message, so verbose/leaky TMDB text never reaches the
// client. tmdbID is informational; pass 0 when unknown.
func tmdbUpstreamError(c *fiber.Ctx, tmdbPath string, tmdbID int, err error) error {
	if tmdbID != 0 {
		fmt.Printf("media: TMDB upstream error tmdb_id=%d path=%s: %v\n", tmdbID, tmdbPath, err)
	} else {
		fmt.Printf("media: TMDB upstream error path=%s: %v\n", tmdbPath, err)
	}
	return response.InternalError(c)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func filterTranslations(raw []byte, lang string) ([]TranslationResponse, error) {
	var resp tmdbTranslationsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}

	var out []TranslationResponse
	langLower := strings.ToLower(lang)

	for _, t := range resp.Translations {
		if strings.ToLower(t.ISO639) != langLower && strings.ToLower(t.ISO3166) != langLower {
			continue
		}
		title := t.Data.Title
		if title == "" {
			title = t.Data.Name
		}
		out = append(out, TranslationResponse{
			Language: t.ISO639,
			Title:    title,
			Overview: t.Data.Overview,
			Tagline:  t.Data.Tagline,
		})
	}

	if out == nil {
		out = make([]TranslationResponse, 0)
	}
	return out, nil
}

func parseAliases(raw []byte) []AliasResponse {
	var resp tmdbAlternativeTitlesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return []AliasResponse{}
	}

	out := make([]AliasResponse, 0, len(resp.Titles))
	for _, t := range resp.Titles {
		out = append(out, AliasResponse{
			Title:   t.Title,
			Country: t.ISO3166,
		})
	}

	if out == nil {
		out = make([]AliasResponse, 0)
	}
	return out
}

// ---------------------------------------------------------------------------
// ID resolution — internal ID → TMDB ID
// ---------------------------------------------------------------------------

func resolveMovieTmdbID(c *fiber.Ctx) (int, error) {
	_, internalID, err := ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return 0, err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return 0, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var movie database.Movie
	if err := db.First(&movie, internalID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, response.NotFound(c, "movie not found")
		}
		return 0, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if movie.TmdbID == 0 {
		return 0, response.NotFound(c, "movie has no TMDB ID")
	}
	return movie.TmdbID, nil
}

func resolveShowTmdbID(c *fiber.Ctx) (int, error) {
	_, internalID, err := ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return 0, err
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return 0, fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, internalID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, response.NotFound(c, "show not found")
		}
		return 0, fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if show.TmdbID == 0 {
		return 0, response.NotFound(c, "show has no TMDB ID")
	}
	return show.TmdbID, nil
}

// ---------------------------------------------------------------------------
// Redis cache helpers
// ---------------------------------------------------------------------------

const translationsTTL = 24 * time.Hour
const aliasesTTL = 24 * time.Hour

func getCachedTranslations(key string, lang string) []TranslationResponse {
	if rdb := redis.GetClient(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		data, err := rdb.Get(ctx, key).Bytes()
		if err == nil {
			var trans []TranslationResponse
			if json.Unmarshal(data, &trans) == nil {
				return trans
			}
		}
	}
	return nil
}

func setCachedTranslations(key string, trans []TranslationResponse) {
	if rdb := redis.GetClient(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		data, _ := json.Marshal(trans)
		rdb.Set(ctx, key, data, translationsTTL)
	}
}

func getCachedAliases(key string) []AliasResponse {
	if rdb := redis.GetClient(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		data, err := rdb.Get(ctx, key).Bytes()
		if err == nil {
			var aliases []AliasResponse
			if json.Unmarshal(data, &aliases) == nil {
				return aliases
			}
		}
	}
	return nil
}

func setCachedAliases(key string, aliases []AliasResponse) {
	if rdb := redis.GetClient(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		data, _ := json.Marshal(aliases)
		rdb.Set(ctx, key, data, aliasesTTL)
	}
}

// ---------------------------------------------------------------------------
// Error helpers
// ---------------------------------------------------------------------------
