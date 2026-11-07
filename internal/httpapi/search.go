package httpapi

import (
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/database"

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
func handleSearch(c *fiber.Ctx) error {
	params, err := parseSearchParams(c)
	if err != nil {
		return badRequest(c, err.Error())
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	resp, err := database.SearchMulti(db, params)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
	}

	// Set pagination headers per api-conventions.md §7.
	c.Set("X-Pagination-Page", strconv.Itoa(resp.Page))
	c.Set("X-Pagination-Limit", strconv.Itoa(resp.Limit))
	c.Set("X-Pagination-Total-Pages", strconv.Itoa(resp.TotalPages))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(resp.TotalItems, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(len(resp.Results)))

	return c.JSON(resp.Results)
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
