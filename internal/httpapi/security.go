package httpapi

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// RequestLogger logs each request on completion using structured slog.
// Fields: method, path, status, latency_ms, remote_ip.
// Also updates metrics counters (total_requests, errors, active_requests).
func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		IncActiveRequests()
		defer DecActiveRequests()

		err := c.Next()

		latencyMs := float64(time.Since(start).Microseconds()) / 1000.0
		status := c.Response().StatusCode()

		IncTotalRequests()

		if status >= 400 {
			IncErrors()
		}

		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"latency_ms", latencyMs,
			"remote_ip", c.IP(),
		}

		if status >= 500 {
			slog.Error("request completed", attrs...)
		} else {
			slog.Info("request completed", attrs...)
		}

		return err
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
