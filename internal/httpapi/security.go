package httpapi

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/haaku-tv/haaku/internal/obs"
)

// RequestLogger logs each request on completion using structured slog.
// Fields: method, path, status, latency_ms, remote_ip.
// It also records the HTTP metrics series (requests_total, request_duration_seconds,
// requests_in_flight) and counts the request as in flight.
//
// The status it reports is the final one. On the success path Fiber's response
// already carries it, so the logger completes the request itself. When a handler
// (or the router, for an unknown route) returns an error, the app-level error
// handler runs *after* this middleware returns and is what writes the final
// status; reporting here would log the still-default 200 for a request the client
// sees as 404. So the error path hands off to the error handler, which calls
// finishRequest once the status is written.
func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		beginRequest(c, start)
		defer obs.DecInFlight()

		if err := c.Next(); err != nil {
			// Leave logging and metrics to the error handler (server.go).
			return err
		}

		finishRequest(c)
		return nil
	}
}

// Recovery returns a panic-recovery middleware that returns a 500 error envelope.
// Uses Fiber's built-in recover middleware.
func Recovery() fiber.Handler {
	return recover.New(recover.Config{
		EnableStackTrace: true,
	})
}

// BodyLimit returns a body-size limiting middleware (default 10MB).
func BodyLimit() fiber.Handler {
	const maxSize int64 = 10 * 1024 * 1024 // 10MB
	return func(c *fiber.Ctx) error {
		cl := c.Get("Content-Length")
		if cl != "" {
			size, err := strconv.ParseInt(cl, 10, 64)
			if err == nil && size > maxSize {
				return writeError(c, fiber.StatusRequestEntityTooLarge, "payload_too_large",
					"request body exceeds 10MB limit")
			}
		}
		return c.Next()
	}
}

// Timeout returns a request timeout middleware (30s default).
func Timeout() fiber.Handler {
	return func(c *fiber.Ctx) error {
		done := make(chan error, 1)
		go func() {
			done <- c.Next()
		}()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			return writeError(c, fiber.StatusGatewayTimeout, "timeout",
				"request timed out")
		}
	}
}

// SecurityHeaders sets standard security response headers.
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - X-XSS-Protection: 1; mode=block
func SecurityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-XSS-Protection", "1; mode=block")
		return c.Next()
	}
}

// CORS returns configurable CORS middleware.
// origins is a list of allowed origins from CORS_ORIGINS env var.
// If empty, no CORS headers are added (same-origin only).
func CORS(origins []string) fiber.Handler {
	if len(origins) == 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	return func(c *fiber.Ctx) error {
		origin := c.Get("Origin")
		if origin == "" {
			return c.Next()
		}

		allowed := false
		for _, o := range origins {
			if strings.EqualFold(o, origin) || o == "*" {
				allowed = true
				break
			}
		}

		if !allowed {
			return c.Next()
		}

		c.Set("Access-Control-Allow-Origin", origin)
		c.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Api-Key,X-Trakt-Api-Key")
		c.Set("Access-Control-Max-Age", "86400")

		// Handle preflight.
		if c.Method() == "OPTIONS" {
			return c.SendStatus(fiber.StatusNoContent)
		}

		return c.Next()
	}
}
