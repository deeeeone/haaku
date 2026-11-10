// Package tmdbmeta serves the per-item provider metadata that haaku did not
// store before issue #68: a title's videos, per-country releases, translations,
// studios and a season's info. Each endpoint follows the same order the lane
// exists to enforce — fetch from the provider, store it, serve it from the
// store — so the second request for an item is answered without a provider
// call and the provider is hit at most once per item per kind.
//
// Everything served here comes from a stored row or a stored provider payload.
// A title the provider has no data for answers the contract's empty array (or
// a 404 when the item itself is unknown), never a placeholder.
package tmdbmeta

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// tmdbClient is lazily initialized on first use, like the other handler
// packages' provider clients. It is the same external/tmdb.Client the ingest
// uses — one client, one HTTP client with a timeout, no second retry loop.
var (
	tmdbOnce      sync.Once
	tmdbClient    *tmdb.Client
	tmdbClientErr error
)

func getTMDBClient() (*tmdb.Client, error) {
	tmdbOnce.Do(func() {
		tmdbClient, tmdbClientErr = tmdb.New()
	})
	return tmdbClient, tmdbClientErr
}

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{Method: "GET", Path: "/v1/movies/{id}/videos", Summary: "Get videos for a movie (contract path)", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Videos", 404: "Movie not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/movies/{id}/releases/{country}", Summary: "Get per-country releases for a movie (contract path)", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Releases", 400: "Invalid country code", 404: "Movie not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/movies/{id}/studios", Summary: "Get studios for a movie (contract path)", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Studios", 404: "Movie not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/movies/{id}/translations", Summary: "Get translations for a movie (contract path)", Tags: []string{"Movies"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Movie not found", 503: "Provider data temporarily unavailable"}},

		{Method: "GET", Path: "/v1/shows/{id}/videos", Summary: "Get videos for a show (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Videos", 404: "Show not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/studios", Summary: "Get studios for a show (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Studios", 404: "Show not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/certifications", Summary: "Get content certifications for a show (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Certification", 404: "Show not found"}},
		{Method: "GET", Path: "/v1/shows/{id}/translations", Summary: "Get translations for a show (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 404: "Show not found", 503: "Provider data temporarily unavailable"}},

		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/videos", Summary: "Get videos for a season (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Videos", 400: "Invalid season number", 404: "Show or season not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/info", Summary: "Get a single season for a show (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Season", 400: "Invalid season number", 404: "Show or season not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/translations", Summary: "Get translations for a season (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 400: "Invalid season number", 404: "Show or season not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/watching", Summary: "Users watching a season right now (contract path)", Tags: []string{"Shows"}, Auth: "optional", StatusCodes: map[int]string{200: "Watching users", 400: "Invalid season number", 404: "Show or season not found"}},

		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/videos", Summary: "Get videos for an episode (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Videos", 400: "Invalid season or episode number", 404: "Show, season or episode not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/translations", Summary: "Get translations for an episode (contract path)", Tags: []string{"Shows"}, Auth: "none", StatusCodes: map[int]string{200: "Translations", 400: "Invalid season or episode number", 404: "Show, season or episode not found", 503: "Provider data temporarily unavailable"}},
		{Method: "GET", Path: "/v1/shows/{id}/seasons/{season}/episodes/{episode}/watching", Summary: "Users watching an episode right now (contract path)", Tags: []string{"Shows"}, Auth: "optional", StatusCodes: map[int]string{200: "Watching users", 400: "Invalid season or episode number", 404: "Show, season or episode not found"}},
	}
	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires the contract-shaped per-item metadata reads.
func RegisterRoutes(api fiber.Router) {
	movies := api.Group("/movies")
	movies.Get("/:id/videos", handleMovieVideos)
	movies.Get("/:id/releases/:country", handleMovieReleases)
	movies.Get("/:id/studios", handleMovieStudios)
	movies.Get("/:id/translations", handleMovieTranslations)

	shows := api.Group("/shows")
	shows.Get("/:id/videos", handleShowVideos)
	shows.Get("/:id/studios", handleShowStudios)
	shows.Get("/:id/certifications", handleShowCertifications)
	shows.Get("/:id/translations", handleShowTranslations)
	shows.Get("/:id/seasons/:season/videos", handleSeasonVideos)
	shows.Get("/:id/seasons/:season/info", handleSeasonInfo)
	shows.Get("/:id/seasons/:season/translations", handleSeasonTranslations)
	shows.Get("/:id/seasons/:season/watching", auth.OptionalAuth(), handleSeasonWatching)
	shows.Get("/:id/seasons/:season/episodes/:episode/videos", handleEpisodeVideos)
	shows.Get("/:id/seasons/:season/episodes/:episode/translations", handleEpisodeTranslations)
	shows.Get("/:id/seasons/:season/episodes/:episode/watching", auth.OptionalAuth(), handleEpisodeWatching)
}

// ---------------------------------------------------------------------------
// Row resolution
// ---------------------------------------------------------------------------

// handled turns an envelope writer's nil-on-success return into ErrHandled so a
// resolver's caller branch fires on it (see response.ErrHandled). Without it a
// helper that writes its own error hands the caller a nil error and the handler
// runs on with a nil row.
func handled(_ error) error { return response.ErrHandled }

// resolveMovie resolves the route's movie param and loads the row.
func resolveMovie(c *fiber.Ctx) (*gorm.DB, *database.Movie, error) {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return nil, nil, err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, handled(response.InternalError(c))
	}
	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, handled(response.NotFound(c, "movie not found"))
		}
		return nil, nil, handled(response.InternalError(c))
	}
	return db, &movie, nil
}

// resolveShow resolves the route's show param and loads the row.
func resolveShow(c *fiber.Ctx) (*gorm.DB, *database.Show, error) {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return nil, nil, err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return nil, nil, handled(response.InternalError(c))
	}
	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, handled(response.NotFound(c, "show not found"))
		}
		return nil, nil, handled(response.InternalError(c))
	}
	return db, &show, nil
}

// resolveShowSeason resolves show + season number to our season row.
func resolveShowSeason(c *fiber.Ctx) (*gorm.DB, *database.Show, *database.Season, error) {
	db, show, err := resolveShow(c)
	if err != nil {
		return nil, nil, nil, err
	}
	seasonNum, aerr := strconv.Atoi(c.Params("season"))
	if aerr != nil || seasonNum < 0 {
		return nil, nil, nil, handled(response.BadRequest(c, "invalid season number"))
	}
	var season database.Season
	if err := db.Where("show_id = ? AND season_number = ?", show.ID, seasonNum).First(&season).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, handled(response.NotFound(c, "season not found"))
		}
		return nil, nil, nil, handled(response.InternalError(c))
	}
	return db, show, &season, nil
}

// resolveShowEpisode resolves show + season + episode number to our episode row.
func resolveShowEpisode(c *fiber.Ctx) (*gorm.DB, *database.Show, *database.Season, *database.Episode, error) {
	db, show, season, err := resolveShowSeason(c)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	epNum, aerr := strconv.Atoi(c.Params("episode"))
	if aerr != nil || epNum < 0 {
		return nil, nil, nil, nil, handled(response.BadRequest(c, "invalid episode number"))
	}
	var episode database.Episode
	if err := db.Where("show_id = ? AND season_id = ? AND episode_number = ?", show.ID, season.ID, epNum).
		First(&episode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, handled(response.NotFound(c, "episode not found"))
		}
		return nil, nil, nil, nil, handled(response.InternalError(c))
	}
	return db, show, season, &episode, nil
}

// ---------------------------------------------------------------------------
// Fetch -> store -> serve
// ---------------------------------------------------------------------------

// fetchOrStored returns the stored payload for one (media_type, media_id, kind)
// tuple, fetching and storing it via fetch on a cache miss. This is the one
// path every read below uses, so the provider is called at most once per item
// per kind.
func fetchOrStored(
	c *fiber.Ctx,
	db *gorm.DB,
	mediaType string,
	mediaID int64,
	kind string,
	fetch func() (database.JSONMap, error),
) (database.JSONMap, error) {
	if stored, ok, err := database.GetMediaMetadata(db, mediaType, mediaID, kind); err != nil {
		return nil, handled(response.InternalError(c))
	} else if ok {
		return stored, nil
	}
	payload, err := fetch()
	if err != nil {
		if errors.Is(err, tmdb.ErrNotFound) {
			// The item row exists but the provider has no record of it: serve
			// the contract's empty result rather than inventing one.
			return database.JSONMap{}, nil
		}
		return nil, fiber.NewError(fiber.StatusServiceUnavailable, "TMDB metadata unavailable")
	}
	if err := database.PutMediaMetadata(db, mediaType, mediaID, kind, payload); err != nil {
		return nil, handled(response.InternalError(c))
	}
	return payload, nil
}

// toPayload marshals a typed payload into the JSONMap the store holds.
func toPayload(v any) (database.JSONMap, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m database.JSONMap
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// arrayValue returns payload[key] as a slice, or an empty slice when absent, so
// an item with no data answers [] rather than null.
func arrayValue(payload database.JSONMap, key string) []any {
	if payload == nil {
		return []any{}
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return []any{}
	}
	arr, ok := v.([]any)
	if !ok {
		return []any{}
	}
	return arr
}

// writeList emits a list-shaped contract response together with the §7
// pagination convention. Every list operation carries the five X-Pagination-*
// headers even when the contract declares no page/limit of its own; clients page
// deterministically only when they are present. These lists are not paginated
// upstream — a call always returns the full stored set — so the whole result is
// reported as a single page. A requested ?page/?limit therefore cannot advertise
// a next page that would just replay the same rows.
func writeList(c *fiber.Ctx, items []any) error {
	limit := len(items)
	if limit < 1 {
		limit = 1
	}
	response.SetPaginationHeaders(c, 1, limit, int64(len(items)), len(items))
	return c.JSON(items)
}
