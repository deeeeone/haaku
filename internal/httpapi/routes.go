package httpapi

import (
	"trakt-api/internal/database"

	"github.com/gofiber/fiber/v2"
)

// RegisterRoutes wires all route groups.
//
// CODE-FIRST OPENAPI (decision log 2026-08-02): every endpoint registered here
// will be emitted into openapi.yaml by the route-registry generator (T0.6).
// Convention per docs/03-api:
//   - versioned route packages: routes/v1routes/{public,protected,admin}.go
//   - no endpoint without registration — CI freshness check enforces it
func RegisterRoutes(app *fiber.App) {
	api := app.Group("/v1")

	// Health (excluded from rate limits — hot path)
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/readyz", func(c *fiber.Ctx) error {
		// Check DB + Redis connectivity
		if db, err := database.GetDB(); err != nil || db == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "degraded", "reason": "database unavailable",
			})
		}
		// Redis is soft — degradation, not failure
		rdb, _ := database.GetRedis()
		if rdb == nil {
			return c.JSON(fiber.Map{"status": "ready", "redis": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})

	// Placeholder for v1 route groups (Phase 1+):
	_ = api
}
