// Package response is a leaf package holding shared HTTP response helpers for
// the error envelope and pagination. It must not import any handler or httpapi
// package: handlers import it directly, and httpapi/routes.go imports every
// handler, so the envelope lives here to avoid an import cycle.
package response

import (
	"github.com/gofiber/fiber/v2"
)

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

// WriteError sends a JSON error envelope with the given status and code.
func WriteError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// NotFound writes a 404 envelope.
func NotFound(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusNotFound, "not_found", message)
}

// BadRequest writes a 400 envelope.
func BadRequest(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusBadRequest, "invalid_request", message)
}

// Unauthorized writes a generic 401 envelope. Detailed reasons go to server
// logs only (no information disclosure).
func Unauthorized(c *fiber.Ctx) error {
	return WriteError(c, fiber.StatusUnauthorized, "unauthorized", "invalid or expired token")
}

// Forbidden writes a 403 envelope.
func Forbidden(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusForbidden, "forbidden", message)
}

// Conflict writes a 409 envelope.
func Conflict(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusConflict, "conflict", message)
}

// InternalError writes a 500 envelope. When no message is given, the generic
// "internal server error" is used.
func InternalError(c *fiber.Ctx, message ...string) error {
	msg := "internal server error"
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	return WriteError(c, fiber.StatusInternalServerError, "internal", msg)
}

// ServiceUnavailable writes a 503 envelope for dependency outages (DB/Redis).
func ServiceUnavailable(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusServiceUnavailable, "db_unavailable", message)
}

// NotImplemented writes a 501 envelope.
func NotImplemented(c *fiber.Ctx, message string) error {
	return WriteError(c, fiber.StatusNotImplemented, "not_implemented", message)
}
