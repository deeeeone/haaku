package response

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// ParseIDParam parses the "id" route param as an int64. On failure it writes a
// 400 envelope and returns ErrHandled so the caller stops immediately instead of
// falling through to a DB lookup with id 0 (which used to answer 404 for input
// the contract advertises as 400 — issue #40).
func ParseIDParam(c *fiber.Ctx, resource string) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		_ = BadRequest(c, "invalid "+resource+" id")
		return 0, ErrHandled
	}
	return id, nil
}
