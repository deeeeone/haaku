package httpapi

// Recent-search persistence and trending reads (issue #70).
//
// Trakt's recent searches are global trends, not per-user history: POST records
// one (item, query) counter that every caller shares, and the circulation read
// groups those counters by item. Both endpoints are contract-shaped; the item
// objects are built with internal/contractshape like every other Trakt shape.

import (
	"errors"
	"strings"
	"time"

	"github.com/haaku-tv/haaku/internal/contractshape"
	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func init() {
	openapi.Register(openapi.RouteDef{
		Method:  "POST",
		Path:    "/v1/search/recent",
		Summary: "Record a recent search (contract path)",
		Tags:    []string{"Search"},
		Auth:    "required",
		StatusCodes: map[int]string{
			201: "Recent search recorded",
			400: "Invalid type or body",
			401: "Unauthorized",
			404: "Search item not found",
		},
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/recent_by_id/global/{type}",
		Summary: "Trending recent searches for one media type (contract path)",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Trending search items",
			400: "Invalid type",
		},
	})
}

// recentSearchRequestTypes maps Trakt's plural type segment onto our media_type.
var recentSearchRequestTypes = map[string]string{
	"movies": "movie", "shows": "show", "people": "person", "lists": "list",
}

// recentSearchPathTypes is the narrower set the trending read accepts. Lists are
// searchable through POST but Trakt does not publish a lists trend.
var recentSearchPathTypes = map[string]string{
	"movies": "movie", "shows": "show", "people": "person",
}

// recentSearchRequest is Trakt's recentSearchRequestSchema.
type recentSearchRequest struct {
	Query string `json:"query"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

// handlePostRecentSearch serves POST /v1/search/recent.
func handlePostRecentSearch(c *fiber.Ctx) error {
	var req recentSearchRequest
	if err := response.ParseBody(c, &req); err != nil {
		return err
	}

	query := strings.ToLower(strings.TrimSpace(req.Query))
	if query == "" || req.ID <= 0 {
		return badRequest(c, "query and id are required")
	}
	mediaType, ok := recentSearchRequestTypes[strings.ToLower(strings.TrimSpace(req.Type))]
	if !ok {
		return badRequest(c, "type must be one of: movies, shows, people, lists")
	}

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	exists, err := recentSearchItemExists(db, mediaType, req.ID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "lookup failed")
	}
	if !exists {
		return notFound(c, "search item not found")
	}

	if err := database.UpsertRecentSearch(db, mediaType, req.ID, query, time.Now().UTC()); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "could not record search")
	}

	return c.Status(fiber.StatusCreated).Send(nil)
}

// recentSearchItemExists checks the item a search names actually exists in the
// type's table, so an unknown id is a 404 rather than a counter for nothing.
func recentSearchItemExists(db *gorm.DB, mediaType string, id int64) (bool, error) {
	var err error
	switch mediaType {
	case "movie":
		err = db.First(&database.Movie{}, id).Error
	case "show":
		err = db.First(&database.Show{}, id).Error
	case "person":
		err = db.First(&database.Person{}, id).Error
	case "list":
		err = db.First(&database.List{}, id).Error
	default:
		return false, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// handleRecentSearchesByID serves GET /v1/search/recent_by_id/global/{type}.
func handleRecentSearchesByID(c *fiber.Ctx) error {
	mediaType, ok := recentSearchPathTypes[strings.ToLower(strings.TrimSpace(c.Params("type")))]
	if !ok {
		return badRequest(c, "type must be one of: movies, shows, people")
	}

	filter := strings.TrimSpace(c.Query("query"))
	page, limit := response.ParsePagination(c, 25)

	db, dbErr := database.GetDB()
	if dbErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "database unavailable")
	}

	rows, total, err := database.TrendingRecentSearches(db, mediaType, filter, (page-1)*limit, limit)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "internal", "search failed")
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item, found, err := buildTrendingSearchItem(db, mediaType, row)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "internal", "lookup failed")
		}
		if found {
			out = append(out, item)
		}
	}

	response.SetPaginationHeaders(c, page, limit, total, len(out))
	return c.JSON(out)
}

// buildTrendingSearchItem renders one grouped row. A row whose item no longer
// exists is skipped (found=false) rather than rendered as an empty object.
func buildTrendingSearchItem(db *gorm.DB, mediaType string, row database.TrendingSearch) (map[string]any, bool, error) {
	item := map[string]any{
		"id":    row.MediaID,
		"count": row.Count,
		"type":  mediaType,
	}

	switch mediaType {
	case "movie":
		var m database.Movie
		if err := db.First(&m, row.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		item["movie"] = contractshape.Movie(&m)
		return item, true, nil
	case "show":
		var s database.Show
		if err := db.First(&s, row.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		item["show"] = contractshape.Show(&s)
		return item, true, nil
	case "person":
		var p database.Person
		if err := db.First(&p, row.MediaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		item["person"] = contractshape.Person(&p)
		return item, true, nil
	}
	return nil, false, nil
}
