package httpapi

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/metadata"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// Default search types (movie, show) when none specified.
var defaultSearchTypes = []string{"movie", "show"}

// Valid search types.
var validSearchTypes = map[string]bool{
	"movie":   true,
	"show":    true,
	"episode": true,
	"person":  true,
	"list":    true,
}

// validExactTypes are the types supported by /search/exact.
var validExactTypes = map[string]bool{
	"movie": true,
	"show":  true,
}

// parseSearchParams extracts and validates common search query parameters.
func parseSearchParams(c *fiber.Ctx) (database.SearchParams, error) {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		return database.SearchParams{}, fiber.NewError(fiber.StatusBadRequest, "query parameter is required")
	}

	typeStr := c.Query("type", "movie,show")
	types := parseTypeList(typeStr)

	page, err := strconv.Atoi(c.Query("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}

	limit, err := strconv.Atoi(c.Query("limit", "25"))
	if err != nil || limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	fieldsStr := c.Query("fields", "")
	var fields []string
	if fieldsStr != "" {
		fields = parseCSV(fieldsStr)
	}

	return database.SearchParams{
		Query:  query,
		Types:  types,
		Page:   page,
		Limit:  limit,
		Fields: fields,
	}, nil
}

// parseTypeList splits a comma-separated type string and validates each type.
func parseTypeList(s string) []string {
	parts := parseCSV(s)
	var types []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if validSearchTypes[p] {
			types = append(types, p)
		}
	}
	if len(types) == 0 {
		return defaultSearchTypes
	}
	return types
}

// parseCSV splits a comma-separated string, trimming whitespace from each part.
func parseCSV(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// handleSearch handles GET /v1/search.
//
// It answers from the local catalogue FIRST and fills the rest of the page from
// TVDB's live name search. The catalogue is a cache of the titles this instance
// has actually ingested, so on a fresh install it is empty — which is exactly
// why the page used to look broken: it was querying a database that had never
// been populated. Live results carry their TVDB id and in_catalog=false; a
// client resolves one through GET /v1/media/resolve, which ingests it.
//
// The provider is optional at runtime: with no TVDB client configured, or when
// TVDB is unreachable, the endpoint still answers with catalogue results.
func handleSearch(c *fiber.Ctx) error {
	params, err := parseSearchParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	local, err := database.SearchMulti(db, params)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
	}

	items := catalogSearchItems(local.Results)
	total := local.TotalItems

	if providerTypes, ok := metadata.NormalizeMediaTypes(params.Types); ok {
		if resolver, resolverErr := metadataResolver(); resolverErr != nil {
			slog.Debug("[search] metadata provider unavailable, answering from the catalogue only", "error", resolverErr)
		} else if hits, providerTotal, providerErr := resolver.SearchProvider(c.UserContext(), params.Query, providerTypes, params.Page, params.Limit); providerErr != nil {
			slog.Warn("[search] provider search failed, answering from the catalogue only", "error", providerErr)
		} else {
			total += int64(providerTotal)
			items = appendProviderItems(items, hits, params.Limit)
		}
	}

	// Set pagination headers per api-conventions.md §7.
	c.Set("X-Pagination-Page", itoa(params.Page))
	c.Set("X-Pagination-Limit", itoa(params.Limit))
	c.Set("X-Pagination-Total-Pages", itoa(pageCount(total, params.Limit)))
	c.Set("X-Pagination-Total-Items", itoa64(total))
	c.Set("X-Pagination-Items-Count", itoa(len(items)))

	return c.JSON(items)
}

// handleSearchExact handles GET /v1/search/exact.
func handleSearchExact(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		return badRequest(c, "query parameter is required")
	}

	typeStr := strings.TrimSpace(strings.ToLower(c.Query("type", "")))
	if !validExactTypes[typeStr] {
		return badRequest(c, "type must be one of: movie, show")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	switch typeStr {
	case "movie":
		movie, err := database.SearchExactMovie(db, query)
		if err == gorm.ErrRecordNotFound {
			candidates, _ := database.SuggestCandidates(db, "movie", query, 5)
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"type":       "movie",
				"not_found":  true,
				"candidates": candidates,
			})
		}
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
		}
		return c.JSON(fiber.Map{"type": "movie", "movie": movie})

	case "show":
		show, err := database.SearchExactShow(db, query)
		if err == gorm.ErrRecordNotFound {
			candidates, _ := database.SuggestCandidates(db, "show", query, 5)
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"type":       "show",
				"not_found":  true,
				"candidates": candidates,
			})
		}
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
		}
		return c.JSON(fiber.Map{"type": "show", "show": show})

	default:
		return badRequest(c, "type must be one of: movie, show")
	}
}

// handleSearchLookup handles GET /v1/search/lookup.
func handleSearchLookup(c *fiber.Ctx) error {
	idType := strings.TrimSpace(strings.ToLower(c.Query("id_type", "")))
	if idType == "" {
		return badRequest(c, "id_type parameter is required (e.g. imdb, tmdb)")
	}

	externalID := strings.TrimSpace(c.Query("id", ""))
	if externalID == "" {
		return badRequest(c, "id parameter is required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	result, err := database.LookupByExternal(db, idType, externalID)
	if err == gorm.ErrRecordNotFound {
		return notFound(c, "no media found for "+idType+":"+externalID)
	}
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "lookup failed")
	}

	return c.JSON(result)
}
