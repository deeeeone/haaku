package reference

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"trakt-api/external/tmdb"
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	redisPkg "trakt-api/redis"

	"github.com/gofiber/fiber/v2"
	"trakt-api/internal/response"
)

func init() {
	registerOpenAPIRoutes()
}

func registerOpenAPIRoutes() {
	routes := []openapi.RouteDef{
		{
			Method:  "GET",
			Path:    "/v1/genres/{type}",
			Summary: "List all genres for movies or shows",
			Tags:    []string{"Reference"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "List of genres",
				400: "Invalid type parameter",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/languages/{type}",
			Summary: "List all supported languages for movies or shows",
			Tags:    []string{"Reference"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "List of languages",
				400: "Invalid type parameter",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/countries/{type}",
			Summary: "List all supported countries for movies or shows",
			Tags:    []string{"Reference"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "List of countries",
				400: "Invalid type parameter",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/networks",
			Summary: "List all distinct TV networks",
			Tags:    []string{"Reference"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "List of networks",
			},
		},
		{
			Method:  "GET",
			Path:    "/v1/certifications/{type}",
			Summary: "List certifications grouped by country for movies or shows",
			Tags:    []string{"Reference"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Certifications grouped by country",
				400: "Invalid type parameter",
			},
		},
	}

	for _, r := range routes {
		openapi.Register(r)
	}
}

// RegisterRoutes wires reference data route handlers to the Fiber router.
func RegisterRoutes(api fiber.Router) {
	ref := api.Group("/")

	ref.Get("/genres/:type", handleGenres)
	ref.Get("/languages/:type", handleLanguages)
	ref.Get("/countries/:type", handleCountries)
	ref.Get("/networks", handleNetworks)
	ref.Get("/certifications/:type", handleCertifications)
}

// ---------------------------------------------------------------------------
// TMDB client lazy init
// ---------------------------------------------------------------------------

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

// GenreItem is the response shape for genre entries.
type GenreItem struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// LanguageItem is the response shape for language entries.
type LanguageItem struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// CountryItem is the response shape for country entries.
type CountryItem struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// NetworkItem is the response shape for network entries.
type NetworkItem struct {
	Name string `json:"name"`
}

// CertItem is a certification entry within a country group.
type CertItem struct {
	Name    string `json:"name"`
	Meaning string `json:"meaning,omitempty"`
	Order   int    `json:"order"`
}

// CertGroup is a country group of certifications.
type CertGroup struct {
	Country        string     `json:"country"`
	Certifications []CertItem `json:"certifications"`
}

// ---------------------------------------------------------------------------
// Slug helper
// ---------------------------------------------------------------------------

// slugify converts a name to a lowercase, hyphenated slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "&", "and")
	return s
}

// ---------------------------------------------------------------------------
// Redis caching
// ---------------------------------------------------------------------------

const referenceTTL = 24 * time.Hour

func cacheKey(category, subtype string) string {
	return "reference:" + category + ":" + subtype
}

func getCached(c *fiber.Ctx, key string, v interface{}) (bool, error) {
	rdb := redisPkg.GetClient()
	if rdb == nil {
		return false, nil // Redis unavailable — fall through to fresh call
	}

	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	data, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		return false, nil // Cache miss or Redis error
	}

	if err := json.Unmarshal(data, v); err != nil {
		log.Printf("[reference] cache unmarshal error for key %s: %v", key, err)
		return false, nil
	}
	return true, nil
}

func setCache(c *fiber.Ctx, key string, v interface{}) {
	rdb := redisPkg.GetClient()
	if rdb == nil {
		return
	}

	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("[reference] cache marshal error for key %s: %v", key, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Set(ctx, key, data, referenceTTL).Err(); err != nil {
		log.Printf("[reference] cache set error for key %s: %v", key, err)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleGenres returns movie or TV genres from TMDB.
func handleGenres(c *fiber.Ctx) error {
	refType := strings.ToLower(c.Params("type"))
	if refType != "movies" && refType != "shows" {
		return response.BadRequest(c, "type must be 'movies' or 'shows'")
	}

	key := cacheKey("genres", refType)

	// Check cache
	var items []GenreItem
	if hit, _ := getCached(c, key, &items); hit {
		return c.JSON(items)
	}

	client, err := getTMDBClient()
	if err != nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "tmdb_unavailable", "TMDB client unavailable")
	}

	if refType == "movies" {
		gl, err := client.GetMovieGenres()
		if err != nil {
			log.Printf("[reference] TMDB genres/movies error: %v", err)
			return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch genres from TMDB")
		}
		items = make([]GenreItem, len(gl.Genres))
		for i, g := range gl.Genres {
			items[i] = GenreItem{Name: g.Name, Slug: slugify(g.Name)}
		}
	} else {
		gl, err := client.GetTVGenres()
		if err != nil {
			log.Printf("[reference] TMDB genres/shows error: %v", err)
			return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch genres from TMDB")
		}
		items = make([]GenreItem, len(gl.Genres))
		for i, g := range gl.Genres {
			items[i] = GenreItem{Name: g.Name, Slug: slugify(g.Name)}
		}
	}

	setCache(c, key, items)
	return c.JSON(items)
}

// handleLanguages returns configured languages from TMDB.
func handleLanguages(c *fiber.Ctx) error {
	refType := strings.ToLower(c.Params("type"))
	if refType != "movies" && refType != "shows" {
		return response.BadRequest(c, "type must be 'movies' or 'shows'")
	}

	key := cacheKey("languages", refType)

	var items []LanguageItem
	if hit, _ := getCached(c, key, &items); hit {
		return c.JSON(items)
	}

	client, err := getTMDBClient()
	if err != nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "tmdb_unavailable", "TMDB client unavailable")
	}

	langs, err := client.GetConfigLanguages()
	if err != nil {
		log.Printf("[reference] TMDB languages error: %v", err)
		return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch languages from TMDB")
	}

	items = make([]LanguageItem, len(langs))
	for i, l := range langs {
		items[i] = LanguageItem{Name: l.EnglishName, Code: l.ISOCode}
	}

	setCache(c, key, items)
	return c.JSON(items)
}

// handleCountries returns configured countries from TMDB.
func handleCountries(c *fiber.Ctx) error {
	refType := strings.ToLower(c.Params("type"))
	if refType != "movies" && refType != "shows" {
		return response.BadRequest(c, "type must be 'movies' or 'shows'")
	}

	key := cacheKey("countries", refType)

	var items []CountryItem
	if hit, _ := getCached(c, key, &items); hit {
		return c.JSON(items)
	}

	client, err := getTMDBClient()
	if err != nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "tmdb_unavailable", "TMDB client unavailable")
	}

	countries, err := client.GetConfigCountries()
	if err != nil {
		log.Printf("[reference] TMDB countries error: %v", err)
		return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch countries from TMDB")
	}

	items = make([]CountryItem, len(countries))
	for i, ct := range countries {
		items[i] = CountryItem{Name: ct.EnglishName, Code: ct.ISOCode}
	}

	setCache(c, key, items)
	return c.JSON(items)
}

// handleNetworks returns distinct TV networks from the shows table.
func handleNetworks(c *fiber.Ctx) error {
	key := cacheKey("networks", "tv")

	var items []NetworkItem
	if hit, _ := getCached(c, key, &items); hit {
		return c.JSON(items)
	}

	db, dbErr := database.GetDB()
	if dbErr != nil || db == nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "db_unavailable", "Database unavailable")
	}

	var networks []string
	if err := db.Raw("SELECT DISTINCT network FROM shows WHERE network != ''").Scan(&networks).Error; err != nil {
		log.Printf("[reference] DB networks query error: %v", err)
		return response.WriteError(c, fiber.StatusInternalServerError, "db_error", "Failed to query networks")
	}

	items = make([]NetworkItem, len(networks))
	for i, n := range networks {
		items[i] = NetworkItem{Name: n}
	}

	setCache(c, key, items)
	return c.JSON(items)
}

// handleCertifications returns certifications grouped by country from TMDB.
func handleCertifications(c *fiber.Ctx) error {
	refType := strings.ToLower(c.Params("type"))
	if refType != "movies" && refType != "shows" {
		return response.BadRequest(c, "type must be 'movies' or 'shows'")
	}

	key := cacheKey("certifications", refType)

	var groups []CertGroup
	if hit, _ := getCached(c, key, &groups); hit {
		return c.JSON(groups)
	}

	client, err := getTMDBClient()
	if err != nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "tmdb_unavailable", "TMDB client unavailable")
	}

	var resp *tmdb.CertificationsResponse
	if refType == "movies" {
		resp, err = client.GetMovieCertifications()
	} else {
		resp, err = client.GetTVCertifications()
	}
	if err != nil {
		log.Printf("[reference] TMDB certifications error: %v", err)
		return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch certifications from TMDB")
	}

	groups = make([]CertGroup, 0, len(resp.Certifications))
	for country, certs := range resp.Certifications {
		items := make([]CertItem, len(certs))
		for i, ct := range certs {
			items[i] = CertItem{
				Name:    ct.Certification,
				Meaning: ct.Meaning,
				Order:   ct.Order,
			}
		}
		groups = append(groups, CertGroup{
			Country:        country,
			Certifications: items,
		})
	}

	setCache(c, key, groups)
	return c.JSON(groups)
}

// ---------------------------------------------------------------------------
// Local error helpers (mirror httpapi exported variants for convenience)
// ---------------------------------------------------------------------------
