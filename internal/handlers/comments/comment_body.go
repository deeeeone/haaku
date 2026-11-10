package comments

import (
	"fmt"

	"github.com/haaku-tv/haaku/internal/response"

	"github.com/gofiber/fiber/v2"
)

// validateCommentBody rejects an empty body and one over the length cap,
// matching the 5000-char comment limit.
//
// It writes the 400 itself and returns response.ErrHandled — NOT the writer's
// nil — so a caller's `if err := validateCommentBody(...); err != nil { return
// err }` actually stops the handler instead of carrying on and overwriting the
// 400 with a later success. This is the same contract ParseIDParam and the
// other writing helpers honour.
func validateCommentBody(c *fiber.Ctx, body string) error {
	if body == "" {
		_ = response.WriteError(c, fiber.StatusBadRequest, "invalid_request", "comment body is required")
		return response.ErrHandled
	}
	if len(body) > maxCommentLength {
		_ = response.WriteError(c, fiber.StatusBadRequest, "invalid_request",
			fmt.Sprintf("comment body exceeds maximum length of %d characters", maxCommentLength))
		return response.ErrHandled
	}
	return nil
}
