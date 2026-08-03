package httpapi

import (
	"time"

	"trakt-api/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// NewServer builds the Fiber app with global middleware and routes.
// Lifecycle pattern: config -> middleware chain -> routes (theintrodb-api main.go).
func NewServer(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		// Error handler renders the JSON envelope for panics/404s uniformly.
		ErrorHandler: errorHandler,
		ReadTimeout:  15 * time.Second,
	})

	app.Use(requestid.New())
	app.Use(recover.New(recover.Config{
		EnableStackTrace: cfg.Env == "development",
	}))

	RegisterRoutes(app)

	return app
}
