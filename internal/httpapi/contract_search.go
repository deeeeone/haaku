package httpapi

import (
	"errors"
	"strconv"
	"strings"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/{type}/exact",
		Summary: "Exact title match for one media type (contract path)",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Search results",
			400: "Invalid type or query",
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

// contractSearchTypes is the set the contract path actually serves.
//
// It is deliberately narrower than validSearchTypes, which our own /v1/search
// route also uses and must keep accepting: the store has no episode, person or
// list search index, so those values answer 400 here rather than a silently
// empty array that a client reads as "no matches".
var contractSearchTypes = map[string]bool{"movie": true, "show": true}

// knownExternalIDTypes is the set of id_type segments the contract documents and
// external_ids can actually be keyed by. Anything else is a bad request: the
// lookup would find nothing and answer 404, which reads as "this title does not
// exist" rather than "you asked the wrong question".
var knownExternalIDTypes = map[string]bool{
	"trakt": true, "imdb": true, "tmdb": true, "tvdb": true,
}

// handleContractSearchType serves GET /v1/search/{type}.
func handleContractSearchType(c *fiber.Ctx) error {
	mediaType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if !contractSearchTypes[mediaType] {
		return badRequest(c, "type must be one of: movie, show (no episode, person or list search index exists)")
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

	out := contractSearchItems(local.Results)
	setSearchPagination(c, page.page, page.limit, local.TotalItems, len(out))
	return c.JSON(out)
}

// handleContractSearchExact serves GET /v1/search/{type}/exact: a case-insensitive
// exact title match, paginated, in the same contract search-item shape as
// GET /v1/search/{type}.
func handleContractSearchExact(c *fiber.Ctx) error {
	mediaType := strings.ToLower(strings.TrimSpace(c.Params("type")))
	if !contractSearchTypes[mediaType] {
		return badRequest(c, "type must be one of: movie, show")
	}

	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		return badRequest(c, "query parameter is required")
	}

	page, limit := response.ParsePagination(c, 25)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	offset := (page - 1) * limit
	var results []database.SearchResult
	var total int64
	switch mediaType {
	case "movie":
		movies, count, err := database.ExactTitleMovies(db, query, offset, limit)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
		}
		total = count
		for i := range movies {
			results = append(results, database.SearchResult{Type: "movie", Movie: &movies[i]})
		}
	case "show":
		shows, count, err := database.ExactTitleShows(db, query, offset, limit)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
		}
		total = count
		for i := range shows {
			results = append(results, database.SearchResult{Type: "show", Show: &shows[i]})
		}
	}

	out := contractSearchItems(results)
	response.SetPaginationHeaders(c, page, limit, total, len(out))
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

	if !knownExternalIDTypes[idType] {
		return badRequest(c, "id_type must be one of: trakt, imdb, tmdb, tvdb")
	}

	result, err := database.LookupByExternal(db, idType, externalID)
	if err != nil {
		// Only a genuine miss is a 404. A database failure used to be reported as
		// "no media found", which tells a client to stop looking when the truth is
		// that we could not look.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return notFound(c, "no media found for "+idType+":"+externalID)
		}
		return writeError(c, fiber.StatusInternalServerError, "internal", "lookup failed")
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
		if n > maxSearchPage {
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

// maxSearchPage bounds page so the offset arithmetic cannot overflow. It is far
// beyond any real result set; a larger page is a bad request, not a 500.
const maxSearchPage = 1_000_000

// atoi parses a base-10 page/limit. The hand-rolled accumulation it replaces
// accepted "99999999999999999999" and wrapped, and it reported every failure with
// the page message even when the limit was the problem.
func atoi(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errBadPage
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
