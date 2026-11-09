package httpapi

import (
	"strings"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

// Contract-shaped search routes.
//
// Trakt spells the media type (and the external-id pair) as path segments; our
// /v1/search takes them as query params. These routes accept the contract's
// shapes and emit the contract's search-item array ({score, type, movie|show}).

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/{type}",
		Summary: "Search one media type (contract path)",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Search results",
			400: "Invalid type or query",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/{id_type}/{id}",
		Summary: "Look up media by external id (contract path)",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Media found",
			404: "No media found",
		},
	})
}

// contractSearchItem is one entry in the contract's search array.
type contractSearchItem struct {
	Score float64        `json:"score"`
	Type  string         `json:"type"`
	Movie map[string]any `json:"movie,omitempty"`
	Show  map[string]any `json:"show,omitempty"`
}

// handleContractSearchType serves GET /v1/search/{type}.
func handleContractSearchType(c *fiber.Ctx) error {
	mediaType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if !validSearchTypes[mediaType] {
		return badRequest(c, "type must be one of: movie, show, episode, person, list")
	}

	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		return badRequest(c, "query parameter is required")
	}

	page, err := parsePageLimit(c)
	if err != nil {
		return badRequest(c, err.Error())
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	local, err := database.SearchMulti(db, database.SearchParams{
		Query: query, Types: []string{mediaType}, Page: page.page, Limit: page.limit,
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
	}

	out := make([]contractSearchItem, 0, len(local.Results))
	for i := range local.Results {
		r := &local.Results[i]
		item := contractSearchItem{Score: 0, Type: r.Type}
		if r.Movie != nil {
			item.Movie = contractshape.Movie(r.Movie)
		}
		if r.Show != nil {
			item.Show = contractshape.Show(r.Show)
		}
		out = append(out, item)
	}
	setSearchPagination(c, page.page, page.limit, local.TotalItems, len(out))
	return c.JSON(out)
}

// handleContractSearchLookup serves GET /v1/search/{id_type}/{id}.
func handleContractSearchLookup(c *fiber.Ctx) error {
	idType := strings.ToLower(strings.TrimSpace(c.Params("id_type")))
	externalID := strings.TrimSpace(c.Params("id"))
	if idType == "" || externalID == "" {
		return badRequest(c, "id_type and id path segments are required")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	result, err := database.LookupByExternal(db, idType, externalID)
	if err != nil {
		return notFound(c, "no media found for "+idType+":"+externalID)
	}

	// The contract's optional ?type narrows the lookup to one media kind.
	if want := strings.ToLower(strings.TrimSpace(c.Query("type"))); want != "" && want != result.Type {
		return notFound(c, "no "+want+" found for "+idType+":"+externalID)
	}

	item := contractSearchItem{Score: 0, Type: result.Type}
	if result.Movie != nil {
		item.Movie = contractshape.Movie(result.Movie)
	}
	if result.Show != nil {
		item.Show = contractshape.Show(result.Show)
	}
	return c.JSON([]contractSearchItem{item})
}

// pageLimit is the validated pagination the contract's search ops accept.
type pageLimit struct{ page, limit int }

func parsePageLimit(c *fiber.Ctx) (pageLimit, error) {
	p := pageLimit{page: 1, limit: 25}
	if v := c.Query("page"); v != "" {
		n, err := atoi(v)
		if err != nil || n < 1 {
			return p, errBadPage
		}
		p.page = n
	}
	if v := c.Query("limit"); v != "" {
		n, err := atoi(v)
		if err != nil || n < 1 {
			return p, errBadLimit
		}
		if n > 100 {
			n = 100
		}
		p.limit = n
	}
	return p, nil
}

var (
	errBadPage  = errString("page must be a positive integer")
	errBadLimit = errString("limit must be a positive integer")
)

type errString string

func (e errString) Error() string { return string(e) }

func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errBadPage
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

func setSearchPagination(c *fiber.Ctx, page, limit int, total int64, count int) {
	c.Set("X-Pagination-Page", itoa(page))
	c.Set("X-Pagination-Limit", itoa(limit))
	c.Set("X-Pagination-Total-Pages", itoa(pageCount(total, limit)))
	c.Set("X-Pagination-Total-Items", itoa64(total))
	c.Set("X-Pagination-Items-Count", itoa(count))
}
