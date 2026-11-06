package httpapi

import (
	"github.com/gofiber/fiber/v2"

	"trakt-api/internal/response"
)

// Error envelope — conventions per docs/03-api/api-conventions.md §9.
// Every error response is {"error": {"code": "...", "message": "..."}}.
// Deliberately absent: 420 quota errors, X-Upgrade-URL headers (charter).
//
// The envelope itself lives in the leaf package internal/response so that
// handler packages can share it without importing httpapi (httpapi/routes.go
// imports every handler, which would otherwise form an import cycle).

// errorHandler is the Fiber app-level error handler: renders envelopes for
// route-not-found, panics, and errors returned from handlers.
func errorHandler(c *fiber.Ctx, err error) error {
	// Route not found (Fiber's ErrNotFound)
	if e, ok := err.(*fiber.Error); ok {
		switch e.Code {
		case fiber.StatusNotFound:
			return response.WriteError(c, fiber.StatusNotFound, "not_found", "unknown route")
		case fiber.StatusMethodNotAllowed:
			return response.WriteError(c, fiber.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route")
		default:
			return response.WriteError(c, e.Code, "error", e.Message)
		}
	}
	// Panics recovered by middleware land here as well
	return response.InternalError(c)
}

// Common error constructors for handlers.
// Exported for cross-package use (e.g., oauth, auth packages).

func NotFound(c *fiber.Ctx, message string) error {
	return response.NotFound(c, message)
}

func BadRequest(c *fiber.Ctx, message string) error {
	return response.BadRequest(c, message)
}

func Unauthorized(c *fiber.Ctx) error {
	return response.Unauthorized(c)
}

func Forbidden(c *fiber.Ctx, message string) error {
	return response.Forbidden(c, message)
}

func Conflict(c *fiber.Ctx, message string) error {
	return response.Conflict(c, message)
}

// Lowercase aliases for internal httpapi package use.
func writeError(c *fiber.Ctx, status int, code, message string) error {
	return response.WriteError(c, status, code, message)
}
func notFound(c *fiber.Ctx, msg string) error   { return response.NotFound(c, msg) }
func badRequest(c *fiber.Ctx, msg string) error { return response.BadRequest(c, msg) }
