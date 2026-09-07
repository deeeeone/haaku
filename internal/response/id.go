package response

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// ParseIDParam parses the "id" route param as an int64. On failure it writes a
// 400 envelope and returns the error for the handler to propagate (shared by
// the episodes/seasons/comments handlers to avoid per-module parseID copies).
func ParseIDParam(c *fiber.Ctx, resource string) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return 0, BadRequest(c, "invalid "+resource+" id")
	}
	return id, nil
}
