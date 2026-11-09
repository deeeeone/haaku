package reference

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	redisPkg "github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/response"
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
			Method:       "GET",
			Path:         "/v1/certifications/movies",
			Summary:      "List movie certifications grouped by country",
			Tags:         []string{"Reference"},
			Auth:         "none",
			ResponseType: certificationCatalogSchema(),
			StatusCodes: map[int]string{
				200: "Movie certifications keyed by country code",
				502: "TMDB returned no usable catalog",
				503: "TMDB client unavailable",
			},
		},
		{
			Method:       "GET",
			Path:         "/v1/certifications/shows",
			Summary:      "List show certifications grouped by country",
			Tags:         []string{"Reference"},
			Auth:         "none",
			ResponseType: certificationCatalogSchema(),
			StatusCodes: map[int]string{
				200: "Show certifications keyed by country code",
				502: "TMDB returned no usable catalog",
				503: "TMDB client unavailable",
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
	// Static segments must be registered before the parameterised route at the
	// same depth, or /certifications/:type shadows both and Fiber never reaches
	// them (TestNoStaticRouteIsShadowed guards the class).
	ref.Get("/certifications/movies", handleMovieCertifications)
	ref.Get("/certifications/shows", handleShowCertifications)
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
	// `shows.network` is a scalar TEXT column (migration 0002; GORM
	// Show.Network string), so a plain DISTINCT is correct — no unnest/
	// jsonb_each_text needed. ORDER BY gives a deterministic, stable list.
	if err := db.Raw("SELECT DISTINCT network FROM shows WHERE network != '' ORDER BY network").Scan(&networks).Error; err != nil {
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

	// Sort deterministically so the response order does not depend on map
	// iteration order (which is random in Go and would otherwise serialize a
	// nondeterministic order into the 24h cache). Country groups sort by ISO
	// code; each country's certifications retain TMDB's own `order` field.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Country < groups[j].Country
	})

	setCache(c, key, groups)
	return c.JSON(groups)
}

// ---------------------------------------------------------------------------
// Certification enums served at Trakt's literal paths
// ---------------------------------------------------------------------------
//
// Trakt serves /certifications/movies and /certifications/shows as an object
// keyed by lowercase country code, each holding the certification values a
// client offers in a filter UI: {name, slug, description}. The values come from
// the SAME source the /certifications/{type} reference handler already reads —
// TMDB's /certification/{movie,tv}/list — so nothing is hardcoded here. TMDB
// supplies the per-country enum (`certification`) and its human explanation
// (`meaning`, which is Trakt's `description`); the slug is a deterministic
// lowercase/hyphenated form of the name, exactly as Trakt's own slugs are.

// CertificationOption is one certification value at Trakt's literal-path shape.
type CertificationOption struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// CertificationCatalog is Trakt's response envelope: country code (lowercase)
// mapped to that country's certification values.
type CertificationCatalog map[string][]CertificationOption

// certificationCatalogSchema is the OpenAPI shape of CertificationCatalog: an
// object keyed by a lowercase country code, each value an array of
// {name, slug, description} options. It matches what the handler emits so the
// generated spec does not have to describe the response as untyped.
func certificationCatalogSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"additionalProperties": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type":     "object",
				"required": []string{"name", "slug", "description"},
				"properties": map[string]interface{}{
					"name":        map[string]interface{}{"type": "string"},
					"slug":        map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

// certificationCatalog converts TMDB's certifications response into Trakt's
// country-keyed catalog. Country codes are lowercased (`US` → `us`) and each
// entry carries TMDB's certification as name, its meaning as description, and a
// slug derived from the name. Empty certification names are skipped so a client
// never sees a value it cannot filter by.
func certificationCatalog(resp *tmdb.CertificationsResponse) CertificationCatalog {
	out := make(CertificationCatalog, len(resp.Certifications))
	for country, certs := range resp.Certifications {
		opts := make([]CertificationOption, 0, len(certs))
		for _, ct := range certs {
			name := strings.TrimSpace(ct.Certification)
			if name == "" {
				continue
			}
			opts = append(opts, CertificationOption{
				Name:        name,
				Slug:        slugify(name),
				Description: ct.Meaning,
			})
		}
		out[strings.ToLower(country)] = opts
	}
	return out
}

// handleMovieCertifications serves GET /v1/certifications/movies.
func handleMovieCertifications(c *fiber.Ctx) error {
	return serveCertificationCatalog(c, "movies")
}

// handleShowCertifications serves GET /v1/certifications/shows.
func handleShowCertifications(c *fiber.Ctx) error {
	return serveCertificationCatalog(c, "shows")
}

// serveCertificationCatalog fetches one media type's certifications from TMDB,
// shapes them as Trakt's country-keyed catalog, and caches the result. It is a
// sibling of handleCertifications (the {type} route) rather than a shared path
// because the two routes deliberately expose different envelopes: the generic
// route returns the pre-existing array of country groups, while the literal
// Trakt routes return the contract's country-keyed object.
func serveCertificationCatalog(c *fiber.Ctx, mediaType string) error {
	key := cacheKey("certification-catalog", mediaType)

	var catalog CertificationCatalog
	if hit, _ := getCached(c, key, &catalog); hit {
		return c.JSON(catalog)
	}

	client, err := getTMDBClient()
	if err != nil {
		return response.WriteError(c, fiber.StatusServiceUnavailable, "tmdb_unavailable", "TMDB client unavailable")
	}

	var resp *tmdb.CertificationsResponse
	if mediaType == "movies" {
		resp, err = client.GetMovieCertifications()
	} else {
		resp, err = client.GetTVCertifications()
	}
	if err != nil {
		log.Printf("[reference] TMDB certification/%s error: %v", mediaType, err)
		return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "Failed to fetch certifications from TMDB")
	}

	catalog = certificationCatalog(resp)
	// The contract's catalog requires the `us` entry, so a response without it
	// is not a catalog we can serve. Answer 502 and cache nothing: caching it
	// would keep serving an unsupported document for the whole TTL.
	if _, ok := catalog["us"]; !ok {
		log.Printf("[reference] TMDB certification/%s response has no US entry; refusing to cache", mediaType)
		return response.WriteError(c, fiber.StatusBadGateway, "upstream_error", "TMDB returned no usable certification catalog")
	}
	setCache(c, key, catalog)
	return c.JSON(catalog)
}
