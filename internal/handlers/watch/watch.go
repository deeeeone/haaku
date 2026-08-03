package watch

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	registerOpenAPIRoutes()
}

// registerOpenAPIRoutes registers watch providers endpoints in the OpenAPI registry.
func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/movies/{id}/watch",
			Summary: "Get watch providers for a movie",
			Tags:    []string{"Movies"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Watch providers",
				400: "Invalid country code",
				404: "Movie not found",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/shows/{id}/watch",
			Summary: "Get watch providers for a TV show",
			Tags:    []string{"Shows"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Watch providers",
				400: "Invalid country code",
				404: "Show not found",
			},
		},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires watch providers route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	movies := api.Group("/movies")
	movies.Get("/:id/watch", handleMovieWatch)

	shows := api.Group("/shows")
	shows.Get("/:id/watch", handleShowWatch)
}

// tmdbClient is lazily initialized on first use.
var (
	tmdbClient     *tmdb.Client
	tmdbClientErr  error
)

func getTMDBClient() (*tmdb.Client, error) {
	if tmdbClient == nil && tmdbClientErr == nil {
		tmdbClient, tmdbClientErr = tmdb.New()
	}
	return tmdbClient, tmdbClientErr
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// WatchResponse is the API response shape for watch providers.
type WatchResponse struct {
	Link     string               `json:"link,omitempty"`
	Flatrate []ProviderSummary    `json:"flatrate,omitempty"`
	Rent     []ProviderSummary    `json:"rent,omitempty"`
	Buy      []ProviderSummary    `json:"buy,omitempty"`
}

// ProviderSummary is a single streaming provider entry.
type ProviderSummary struct {
	ProviderID int    `json:"provider_id"`
	Name       string `json:"name"`
	LogoPath   string `json:"logo_path"`
}

// cachedWatchProviders is the shape we store in the metadata JSONB column.
type cachedWatchProviders struct {
	SyncedAt string                         `json:"_synced_at"`
	Results  map[string]cachedCountryResult `json:"results"`
}

type cachedCountryResult struct {
	Link     string             `json:"link"`
	Flatrate []tmdb.Provider    `json:"flatrate,omitempty"`
	Rent     []tmdb.Provider    `json:"rent,omitempty"`
	Buy      []tmdb.Provider    `json:"buy,omitempty"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleMovieWatch(c *fiber.Ctx) error {
	mediaType, id, err := media.ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "movie" {
		return notFound(c, "not a movie")
	}

	country, cerr := parseCountry(c)
	if cerr != nil {
		return cerr
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var movie database.Movie
	if err := db.First(&movie, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if movie.TmdbID == 0 {
		return notFound(c, "movie has no TMDB ID")
	}

	return serveWatchProviders(c, movie.Metadata, country, func() (*tmdb.WatchProvidersResponse, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetMovieWatchProviders(movie.TmdbID)
	})
}

func handleShowWatch(c *fiber.Ctx) error {
	mediaType, id, err := media.ResolveID(c, c.Params("id"))
	if err != nil {
		return err
	}
	if mediaType != "show" {
		return notFound(c, "not a show")
	}

	country, cerr := parseCountry(c)
	if cerr != nil {
		return cerr
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database unavailable")
	}

	var show database.Show
	if err := db.First(&show, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if show.TmdbID == 0 {
		return notFound(c, "show has no TMDB ID")
	}

	return serveWatchProviders(c, show.Metadata, country, func() (*tmdb.WatchProvidersResponse, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetShowWatchProviders(show.TmdbID)
	})
}

// serveWatchProviders handles cache lookup and fallback to TMDB for watch providers.
func serveWatchProviders(
	c *fiber.Ctx,
	metadata database.JSONMap,
	country string,
	fetchFresh func() (*tmdb.WatchProvidersResponse, error),
) error {
	// Try cache first.
	if cached, ok := loadCachedProviders(metadata); ok {
		if countryResult, found := cached.Results[strings.ToUpper(country)]; found {
			return c.JSON(convertToResponse(countryResult))
		}
		// Country not in cache — fall through to fetch fresh.
	}

	// Fetch from TMDB.
	resp, err := fetchFresh()
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "TMDB watch providers unavailable")
	}

	// Build cache entry.
	cached := cachedWatchProviders{
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
		Results:  make(map[string]cachedCountryResult),
	}
	for code, cp := range resp.Results {
		cached.Results[code] = cachedCountryResult{
			Link:     cp.Link,
			Flatrate: cp.Flatrate,
			Rent:     cp.Rent,
			Buy:      cp.Buy,
		}
	}

	// Store in metadata.
	storeCachedProviders(metadata, cached)

	// Return requested country.
	countryResult, ok := cached.Results[strings.ToUpper(country)]
	if !ok {
		return c.JSON(defaultResponse(cpLink(resp.ID, "movie", country)))
	}

	return c.JSON(convertToResponse(countryResult))
}

// loadCachedProviders reads cached watch provider data from metadata.
// Returns nil if cache is missing or stale (>7 days).
func loadCachedProviders(metadata database.JSONMap) (*cachedWatchProviders, bool) {
	if metadata == nil {
		return nil, false
	}

	raw, ok := metadata["watch_providers"]
	if !ok {
		return nil, false
	}

	// Re-marshal to decode into typed struct.
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}

	var cached cachedWatchProviders
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}

	// Check staleness (7 days).
	if cached.SyncedAt != "" {
		t, err := time.Parse(time.RFC3339, cached.SyncedAt)
		if err == nil && time.Since(t) > 7*24*time.Hour {
			return nil, false
		}
	}

	return &cached, true
}

// storeCachedProviders stores watch provider data in metadata.
func storeCachedProviders(metadata database.JSONMap, cached cachedWatchProviders) {
	if metadata == nil {
		return
	}

	// Re-marshal through JSON for clean storage.
	data, _ := json.Marshal(cached)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	metadata["watch_providers"] = raw
}

// convertToResponse transforms TMDB country providers into the API response shape.
func convertToResponse(cp cachedCountryResult) WatchResponse {
	resp := WatchResponse{Link: cp.Link}

	if len(cp.Flatrate) > 0 {
		resp.Flatrate = make([]ProviderSummary, len(cp.Flatrate))
		for i, p := range cp.Flatrate {
			resp.Flatrate[i] = ProviderSummary{
				ProviderID: p.ProviderID,
				Name:       p.ProviderName,
				LogoPath:   p.LogoPath,
			}
		}
	}

	if len(cp.Rent) > 0 {
		resp.Rent = make([]ProviderSummary, len(cp.Rent))
		for i, p := range cp.Rent {
			resp.Rent[i] = ProviderSummary{
				ProviderID: p.ProviderID,
				Name:       p.ProviderName,
				LogoPath:   p.LogoPath,
			}
		}
	}

	if len(cp.Buy) > 0 {
		resp.Buy = make([]ProviderSummary, len(cp.Buy))
		for i, p := range cp.Buy {
			resp.Buy[i] = ProviderSummary{
				ProviderID: p.ProviderID,
				Name:       p.ProviderName,
				LogoPath:   p.LogoPath,
			}
		}
	}

	return resp
}

// cpLink returns the TMDB watch link for a given item and country.
func cpLink(tmdbID int, mediaType, country string) string {
	country = strings.ToUpper(country)
	if mediaType == "show" {
		return "https://www.themoviedb.org/tv/" + itoa(tmdbID) + "/watch?locale=" + country
	}
	return "https://www.themoviedb.org/movie/" + itoa(tmdbID) + "/watch?locale=" + country
}

// defaultResponse returns an empty watch providers response with just the link.
func defaultResponse(link string) WatchResponse {
	return WatchResponse{Link: link}
}

// parseCountry extracts and validates the country query parameter.
func parseCountry(c *fiber.Ctx) (string, error) {
	country := strings.TrimSpace(c.Query("country", "us"))
	country = strings.ToUpper(country)
	if len(country) != 2 {
		return "", badRequest(c, "invalid country code: must be a 2-letter ISO code")
	}
	return country, nil
}

// --- Helpers ---

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// --- Error helpers ---

func notFound(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    "not_found",
			"message": msg,
		},
	})
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    "invalid_request",
			"message": msg,
		},
	})
}
