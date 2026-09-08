package response

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// ParsePagination extracts page/limit from query params per api-conventions.md
// §7: page defaults to 1, limit defaults to defaultLimit and clamps to 100.
func ParsePagination(c *fiber.Ctx, defaultLimit int) (page, limit int) {
	page = 1
	limit = defaultLimit

	if p := c.Query("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = ClampLimit(parsed, 1, 100)
		}
	}

	return page, limit
}

// QueryInt parses an integer query param, falling back to defaultVal on
// missing/invalid input.
func QueryInt(c *fiber.Ctx, key string, defaultVal int) int {
	raw := c.Query(key)
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return n
}

// ClampLimit bounds a value between min and max.
func ClampLimit(limit, min, max int) int {
	if limit < min {
		return min
	}
	if limit > max {
		return max
	}
	return limit
}

// SetPaginationHeaders writes the §7 pagination response headers and a
// Link: rel="next" header (when another page exists) onto c. It is the single
// shared implementation of the api-conventions.md §7 contract, which every
// handler must emit so clients page deterministically:
//
//	X-Pagination-Page / -Limit / -Total-Pages / -Total-Items / -Items-Count
//	Link: <path?page=N+1&limit=M>; rel="next"   (when page < totalPages)
//
// total is the total number of items across all pages; itemsCount is the number
// of items on the current page. Callers should always pass the count of rows
// actually returned so -Items-Count stays accurate even for an empty page.
func SetPaginationHeaders(c *fiber.Ctx, page, limit int, total int64, itemsCount int) {
	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}
	c.Set("X-Pagination-Page", strconv.Itoa(page))
	c.Set("X-Pagination-Limit", strconv.Itoa(limit))
	c.Set("X-Pagination-Total-Pages", strconv.FormatInt(totalPages, 10))
	c.Set("X-Pagination-Total-Items", strconv.FormatInt(total, 10))
	c.Set("X-Pagination-Items-Count", strconv.Itoa(itemsCount))
	if page < int(totalPages) {
		c.Set("Link", `<`+c.Path()+`?page=`+strconv.Itoa(page+1)+`&limit=`+strconv.Itoa(limit)+`>; rel="next"`)
	}
}
