package httpapi

import (
	"github.com/gofiber/fiber/v2"
)

// RegisterRoutes wires all route groups.
//
// CODE-FIRST OPENAPI (decision log 2026-08-02): every endpoint registered here
// will be emitted into openapi.yaml by the route-registry generator (T0.6).
// Until then, this file is the registry. Convention per docs/03-api:
//   - versioned route packages: routes/v1routes/{public,protected,admin}.go
//   - no endpoint without registration — CI freshness check enforces it
func RegisterRoutes(app *fiber.App) {
	api := app.Group("/v1")

	// Health (excluded from rate limits — hot path)
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/readyz", func(c *fiber.Ctx) error {
		// TODO(T0.4): check DB + Redis connectivity here once wired
		return c.JSON(fiber.Map{"status": "ready"})
	})

	// Placeholder for v1 route groups (Phase 1+):
	// api.Group("/movies") -> media agent
	// api.Group("/sync")   -> sync agent
	// api.Group("/users")  -> social agent
	_ = api
}
