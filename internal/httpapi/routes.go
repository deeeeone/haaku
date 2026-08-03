package httpapi

import (
	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
	"trakt-api/redis"

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
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/healthz",
		Summary: "Health check — returns service liveness status",
		Tags:    []string{"Health"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Service is healthy",
		},
	})

	app.Get("/readyz", func(c *fiber.Ctx) error {
		// Check DB + Redis connectivity
		if db, err := database.GetDB(); err != nil || db == nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "degraded", "reason": "database unavailable",
			})
		}
		// Redis is soft — degradation, not failure
		rdb := redis.GetClient()
		if rdb == nil {
			return c.JSON(fiber.Map{"status": "ready", "redis": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/readyz",
		Summary: "Readiness check — returns service health including DB/Redis",
		Tags:    []string{"Health"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Service is ready",
			503: "Service is degraded",
		},
	})

	// Placeholder for v1 route groups (Phase 1+):
	_ = api
}
