package watchnow

// GET /watchnow/sources and /watchnow/sources/{countryCode} (issue #69).
//
// The contract returns an ARRAY of records keyed by lower-case country code,
// each holding that country's provider list. The catalogue is stored in the
// watchnow_sources table and refreshed from TMDB by the metadata worker, so a
// read is a table query: no provider call, and an unknown country is an honest
// empty array rather than a 404.
//
// Field honesty: TMDB's catalogue names a provider, its logo and its per-country
// display priority. It does not say whether a provider is free, a cinema, or
// Amazon, and it has no channel image or link count, so those fields carry the
// contract's neutral values (false / "" / 0 / null) rather than invented ones.

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/external/tmdb"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
)

func init() {
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/watchnow/sources",
		Summary:     "Watch-now sources for every country, keyed by country code",
		Tags:        []string{"Watch Now"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "Source records keyed by country"},
	})
	openapi.Register(openapi.RouteDef{
		Method:      "GET",
		Path:        "/v1/watchnow/sources/{countryCode}",
		Summary:     "Watch-now sources for one country",
		Tags:        []string{"Watch Now"},
		Auth:        "none",
		StatusCodes: map[int]string{200: "One source record keyed by country"},
	})
}

// RegisterRoutes mounts the catalogue reads.
func RegisterRoutes(api fiber.Router) {
	api.Get("/watchnow/sources", handleSources)
	api.Get("/watchnow/sources/:countryCode", handleSourcesByCountry)
}

// handleSources returns one record containing every country's sources.
func handleSources(c *fiber.Ctx) error {
	return serveSources(c, "")
}

// handleSourcesByCountry returns one record for the requested country, or an
// empty array when the catalogue has nothing for it. A non-2-letter code is
// treated the same way: the contract has no error shape for this read, and an
// unknown country genuinely has no sources.
func handleSourcesByCountry(c *fiber.Ctx) error {
	code := strings.ToLower(strings.TrimSpace(c.Params("countryCode")))
	if len(code) != 2 {
		return c.JSON([]map[string][]map[string]any{})
	}
	return serveSources(c, code)
}

func serveSources(c *fiber.Ctx, country string) error {
	db, dbErr := database.GetDB()
	if dbErr != nil {
		return response.ServiceUnavailable(c, "database unavailable")
	}

	query := db.Order("country ASC, priority ASC, name ASC")
	if country != "" {
		query = query.Where("country = ?", country)
	}
	var rows []database.WatchNowSource
	if err := query.Find(&rows).Error; err != nil {
		return response.InternalError(c)
	}

	// A provider can appear in both the movie and the TV catalogue; the
	// contract lists it once per country at its best priority (the query is
	// priority-ordered, so the first occurrence wins).
	grouped := make(map[string][]map[string]any, 32)
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := row.Country + "|" + strconv.Itoa(row.ProviderID)
		if seen[key] {
			continue
		}
		seen[key] = true
		grouped[row.Country] = append(grouped[row.Country], sourceBody(row))
	}

	if len(grouped) == 0 {
		return c.JSON([]map[string][]map[string]any{})
	}
	return c.JSON([]map[string][]map[string]any{grouped})
}

// sourceBody maps one stored provider row onto the contract's source entry.
func sourceBody(row database.WatchNowSource) map[string]any {
	var logo any
	if row.LogoPath != "" {
		logo = tmdb.ImageURL(row.LogoPath, tmdb.ImageSizePoster)
	}
	return map[string]any{
		"source":     row.Source,
		"name":       row.Name,
		"type":       nil,
		"free":       false,
		"cinema":     false,
		"amazon":     strings.Contains(strings.ToLower(row.Name), "amazon"),
		"color":      "",
		"link_count": 0,
		"images": map[string]any{
			"logo":    logo,
			"channel": nil,
		},
	}
}
