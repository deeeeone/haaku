package httpapi

import (
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// NewServer builds the Fiber app with global middleware and routes.
// Middleware chain order: Recovery → SecurityHeaders → RequestLogger → RateLimit → CORS → BodyLimit → Timeout.
// Body limit (10MB) is enforced via Fiber's built-in config.
// Lifecycle pattern: config -> middleware chain -> routes (theintrodb-api main.go).
func NewServer(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		// Error handler renders the JSON envelope for panics/404s uniformly.
		ErrorHandler: errorHandler,
		ReadTimeout:  35 * time.Second, // accommodates Timeout middleware (30s)
		BodyLimit:    10 * 1024 * 1024, // 10MB
	})

	// 1. Request ID — adds X-Request-Id header.
	app.Use(requestid.New())

	// 2. Recovery — panic recovery returning 500 error envelope.
	app.Use(Recovery())

	// 3. Security headers — X-Content-Type-Options, X-Frame-Options, X-XSS-Protection.
	app.Use(SecurityHeaders())

	// 4. Request logger — structured slog logging + metrics counters.
	app.Use(RequestLogger())

	// 5. Rate limiting — sliding window via Redis (fail-open if Redis unavailable).
	app.Use(RateLimit())

	// 6. CORS — configurable origin allowlist.
	app.Use(CORS(cfg.CORSOrigins))

	// 7. Timeout — overall request deadline (30s).
	app.Use(Timeout())

	// Initialize Clerk client (Phase 2: T2.1)
	auth.InitClerk(cfg)

	// Register API routes.
	RegisterRoutes(app)

	// Register infrastructure routes (/metrics).
	registerMetricsRoute(app)

	return app
}
