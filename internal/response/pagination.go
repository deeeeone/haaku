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
