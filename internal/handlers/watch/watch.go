package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"trakt-api/internal/response"
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

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// WatchResponse is the API response shape for watch providers.
type WatchResponse struct {
	Link     string            `json:"link,omitempty"`
	Flatrate []ProviderSummary `json:"flatrate,omitempty"`
	Rent     []ProviderSummary `json:"rent,omitempty"`
	Buy      []ProviderSummary `json:"buy,omitempty"`
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
	Link     string          `json:"link"`
	Flatrate []tmdb.Provider `json:"flatrate,omitempty"`
	Rent     []tmdb.Provider `json:"rent,omitempty"`
	Buy      []tmdb.Provider `json:"buy,omitempty"`
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
		return response.NotFound(c, "not a movie")
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
			return response.NotFound(c, "movie not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if movie.TmdbID == 0 {
		return response.NotFound(c, "movie has no TMDB ID")
	}

	return serveWatchProviders(c, movie.Metadata, country, func() error {
		return db.Model(&movie).Update("metadata", movie.Metadata).Error
	}, func() (*tmdb.WatchProvidersResponse, error) {
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
		return response.NotFound(c, "not a show")
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
			return response.NotFound(c, "show not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "database query error")
	}

	if show.TmdbID == 0 {
		return response.NotFound(c, "show has no TMDB ID")
	}

	return serveWatchProviders(c, show.Metadata, country, func() error {
		return db.Model(&show).Update("metadata", show.Metadata).Error
	}, func() (*tmdb.WatchProvidersResponse, error) {
		client, cerr := getTMDBClient()
		if cerr != nil {
			return nil, cerr
		}
		return client.GetShowWatchProviders(show.TmdbID)
	})
}

// serveWatchProviders handles cache lookup and fallback to TMDB for watch providers.
// persist persists an updated metadata map back to the DB row (best-effort) after
// a fresh TMDB fetch.
func serveWatchProviders(
	c *fiber.Ctx,
	metadata database.JSONMap,
	country string,
	persist func() error,
	fetchFresh func() (*tmdb.WatchProvidersResponse, error),
) error {
	country = strings.ToUpper(country)

	// Try cache first.
	if cached, ok := loadCachedProviders(metadata); ok {
		if countryResult, found := cached.Results[country]; found {
			return c.JSON(convertToResponse(countryResult))
		}
		// The requested country has no providers in the cached set. If we already
		// confirmed that absence recently, serve the negative result instead of
		// refetching the whole provider set on every request for this country.
		if countryAbsenceFresh(metadata, country) {
			return c.JSON(WatchResponse{})
		}
		// Absence not (recently) confirmed — fall through to one refetch so
		// newly-added providers are picked up.
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
	if err := storeCachedProviders(metadata, cached); err != nil {
		log.Printf("[watch] failed to cache watch providers: %v", err)
	}

	// Return requested country.
	countryResult, found := cached.Results[country]

	if found {
		// Country now has providers — clear any stale negative and persist.
		clearCountryAbsence(metadata, country)
		if err := persist(); err != nil {
			log.Printf("[watch] failed to persist watch providers cache: %v", err)
		}
		return c.JSON(convertToResponse(countryResult))
	}

	// No providers for this country — record the absence (short TTL negative
	// cache) so repeated requests for it don't each trigger a full TMDB refetch,
	// then persist both the provider set and the absence marker.
	recordCountryAbsence(metadata, country)
	if err := persist(); err != nil {
		log.Printf("[watch] failed to persist watch providers cache: %v", err)
	}
	return c.JSON(defaultResponse(cpLink(resp.ID, "movie", country)))
}

// metadataAbsenceKey is the metadata JSONB key holding country → last-confirmed
// "no providers" timestamps for negative caching.
const metadataAbsenceKey = "watch_providers_absence"

// countryAbsenceTTL bounds how long a confirmed "no providers" negative is
// honored before the country is re-checked against TMDB.
const countryAbsenceTTL = time.Hour

// loadAbsenceMap decodes the stored absence map (country → RFC3339 timestamp).
func loadAbsenceMap(metadata database.JSONMap) map[string]string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[metadataAbsenceKey]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

// countryAbsenceFresh reports whether we recently confirmed that the country has
// no providers (within countryAbsenceTTL), so the caller can serve the negative
// result without refetching TMDB.
func countryAbsenceFresh(metadata database.JSONMap, country string) bool {
	ts, ok := loadAbsenceMap(metadata)[country]
	if !ok {
		return false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(t) <= countryAbsenceTTL
}

// recordCountryAbsence stamps a confirmed "no providers" negative for country.
func recordCountryAbsence(metadata database.JSONMap, country string) {
	m := loadAbsenceMap(metadata)
	if m == nil {
		m = make(map[string]string)
	}
	m[country] = time.Now().UTC().Format(time.RFC3339)
	storeAbsenceMap(metadata, m)
}

// clearCountryAbsence drops a stale negative once a country gains providers.
func clearCountryAbsence(metadata database.JSONMap, country string) {
	m := loadAbsenceMap(metadata)
	if m == nil {
		return
	}
	if _, ok := m[country]; !ok {
		return
	}
	delete(m, country)
	storeAbsenceMap(metadata, m)
}

// storeAbsenceMap writes the absence map back into the metadata JSONB, keeping
// the same clean any-typed shape storeCachedProviders uses.
func storeAbsenceMap(metadata database.JSONMap, m map[string]string) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	metadata[metadataAbsenceKey] = raw
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

// --- Helpers ---

func storeCachedProviders(metadata database.JSONMap, cached cachedWatchProviders) error {
	if metadata == nil {
		return nil
	}

	// Re-marshal through JSON for clean storage.
	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("marshal cached watch providers: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal cached watch providers: %w", err)
	}
	metadata["watch_providers"] = raw
	return nil
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
		return "https://www.themoviedb.org/tv/" + strconv.Itoa(tmdbID) + "/watch?locale=" + country
	}
	return "https://www.themoviedb.org/movie/" + strconv.Itoa(tmdbID) + "/watch?locale=" + country
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
		return "", response.BadRequest(c, "invalid country code: must be a 2-letter ISO code")
	}
	return country, nil
}

// --- Error helpers ---
