package watch

import (
	"errors"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Contract-shaped provider refreshes (issue #68).
//
// Trakt's POST /{movies,shows}/{id}/refresh/{source} asks the server to re-fetch
// that title's watch-provider links. Ours does exactly that — it re-reads the
// provider set from TMDB and rewrites the cached copy in movies/shows.metadata —
// then answers the contract's 201 with an empty body.
//
// "justwatch" in the path is Trakt's source name. We re-fetch TMDB's watch
// providers, which is the provider data this instance actually holds; the
// response is the empty 201 the contract specifies, not a JustWatch payload.

func init() {
	for _, path := range []string{"/v1/movies/{id}/refresh/justwatch", "/v1/shows/{id}/refresh/justwatch"} {
		openapi.Register(openapi.RouteDef{
			Method:      "POST",
			Path:        path,
			Summary:     "Re-fetch a title's watch providers (contract path)",
			Tags:        []string{"Watch Now"},
			Auth:        "required",
			StatusCodes: map[int]string{201: "Refresh queued", 401: "Unauthorized", 404: "Media not found", 503: "Provider data temporarily unavailable"},
		})
	}
}

// registerContractRefreshRoutes mounts the contract-shaped provider refreshes.
func registerContractRefreshRoutes(api fiber.Router) {
	api.Post("/movies/:id/refresh/justwatch", auth.AuthRequired(), handleRefreshMovieProviders)
	api.Post("/shows/:id/refresh/justwatch", auth.AuthRequired(), handleRefreshShowProviders)
}

func handleRefreshMovieProviders(c *fiber.Ctx) error {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "movie")
	if err != nil {
		return err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}
	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if movie.TmdbID == 0 {
		return response.NotFound(c, "movie has no TMDB ID")
	}
	client, cerr := getTMDBClient()
	if cerr != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}
	resp, ferr := client.GetMovieWatchProviders(movie.TmdbID)
	if ferr != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}
	return storeRefreshedProviders(c, movie.Metadata, resp, func() error {
		return db.Model(&movie).Update("metadata", movie.Metadata).Error
	})
}

func handleRefreshShowProviders(c *fiber.Ctx) error {
	_, id, err := media.ResolveIDAs(c, c.Params("id"), "show")
	if err != nil {
		return err
	}
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}
	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.NotFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}
	if show.TmdbID == 0 {
		return response.NotFound(c, "show has no TMDB ID")
	}
	client, cerr := getTMDBClient()
	if cerr != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}
	resp, ferr := client.GetShowWatchProviders(show.TmdbID)
	if ferr != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}
	return storeRefreshedProviders(c, show.Metadata, resp, func() error {
		return db.Model(&show).Update("metadata", show.Metadata).Error
	})
}

// storeRefreshedProviders rewrites the cached provider set for one title and
// answers 201 {}.
func storeRefreshedProviders(
	c *fiber.Ctx,
	metadata database.JSONMap,
	resp *tmdb.WatchProvidersResponse,
	persist func() error,
) error {
	if metadata == nil {
		return response.InternalError(c)
	}
	cached := cachedWatchProviders{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Results:  make(map[string]cachedCountryResult, len(resp.Results)),
	}
	for code, cp := range resp.Results {
		cached.Results[code] = cachedCountryResult{Link: cp.Link, Flatrate: cp.Flatrate, Rent: cp.Rent, Buy: cp.Buy}
	}
	if err := storeCachedProviders(metadata, cached); err != nil {
		return response.InternalError(c)
	}
	// A full refresh re-read every country, so negative-cache markers from the
	// previous set are stale.
	delete(metadata, metadataAbsenceKey)
	if err := persist(); err != nil {
		return response.InternalError(c)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{})
}
