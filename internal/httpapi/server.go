package httpapi

import (
	"log/slog"
	"time"

	"trakt-api/internal/auth"
	"trakt-api/internal/config"
	"trakt-api/internal/webui"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// NewServer builds the Fiber app with global middleware and routes.
// Middleware chain order: Recovery → SecurityHeaders → RequestLogger → RateLimit → CORS → BodyLimit → Timeout.
// Body limit (10MB) is enforced via Fiber's built-in config.
// Lifecycle pattern: config -> middleware chain -> routes (theintrodb-api main.go).
func NewServer(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		// Error handler renders the JSON envelope for panics/404s uniformly, then
		// completes the request's observability record with the status it wrote.
		ErrorHandler: observedErrorHandler,
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

	// The metrics surface is NOT registered here. It lives on its own listener
	// (internal/obs, TKILLER_METRICS_LISTEN) with no application auth, for a
	// private monitoring network. The old public JSON /metrics route was removed:
	// it published traffic volume on the main listener and described nothing.

	// Serve the embedded single-page app. Mounted last, and mounted after every
	// API route, so it only sees requests the router did not handle; API paths
	// are excluded inside the handler so an unknown /v1 request still gets the
	// JSON envelope (internal/webui).
	ui, err := webui.Handler()
	if err != nil {
		// The bundle is embedded at build time, so a failure here is a packaging
		// bug, not a runtime condition. Serve the API rather than refusing to
		// start, and say so loudly.
		slog.Error("embedded frontend unavailable — serving API only", "error", err)
	} else {
		app.Use(ui)
	}

	return app
}

// observedErrorHandler wraps the envelope error handler so the request's metrics
// and log line are recorded with the final status. Fiber invokes the app-level
// error handler after the middleware chain returns, and it is the error handler
// that writes the status (404 for an unknown route, 500 for a panic), so this is
// the only place the true status is visible. RequestLogger deliberately skips the
// error path to avoid double-recording.
func observedErrorHandler(c *fiber.Ctx, err error) error {
	herr := errorHandler(c, err)
	finishRequest(c)
	return herr
}
