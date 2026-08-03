package httpapi

import "github.com/gofiber/fiber/v2"

// Error envelope — conventions per docs/03-api/api-conventions.md §9.
// Every error response is {"error": {"code": "...", "message": "..."}}.
// Deliberately absent: 420 quota errors, X-Upgrade-URL headers (charter).

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError sends a JSON error envelope with the given status and code.
func writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// errorHandler is the Fiber app-level error handler: renders envelopes for
// route-not-found, panics, and errors returned from handlers.
func errorHandler(c *fiber.Ctx, err error) error {
	// Route not found (Fiber's ErrNotFound)
	if e, ok := err.(*fiber.Error); ok {
		switch e.Code {
		case fiber.StatusNotFound:
			return writeError(c, fiber.StatusNotFound, "not_found", "unknown route")
		case fiber.StatusMethodNotAllowed:
			return writeError(c, fiber.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route")
		default:
			return writeError(c, e.Code, "error", e.Message)
		}
	}
	// Panics recovered by middleware land here as well
	return writeError(c, fiber.StatusInternalServerError, "internal", "internal server error")
}

// Common error constructors for handlers.
func notFound(c *fiber.Ctx, message string) error {
	return writeError(c, fiber.StatusNotFound, "not_found", message)
}

func badRequest(c *fiber.Ctx, message string) error {
	return writeError(c, fiber.StatusBadRequest, "invalid_request", message)
}

func unauthorized(c *fiber.Ctx) error {
	// Generic message — detailed reasons go to server logs only (no info disclosure).
	return writeError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
}
