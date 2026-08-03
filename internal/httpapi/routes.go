package httpapi

import (
	"trakt-api/internal/auth"
	"trakt-api/internal/database"
	"trakt-api/internal/handlers/activity"
	"trakt-api/internal/handlers/calendar"
	"trakt-api/internal/handlers/checkin"
	"trakt-api/internal/handlers/collab"
	"trakt-api/internal/handlers/comments"
	"trakt-api/internal/handlers/discovery"
	"trakt-api/internal/handlers/episodes"
	"trakt-api/internal/handlers/filters"
	"trakt-api/internal/handlers/hidden"
	"trakt-api/internal/handlers/integrations"
	"trakt-api/internal/handlers/lists"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/handlers/notes"
	"trakt-api/internal/handlers/notifications"
	"trakt-api/internal/handlers/progress"
	"trakt-api/internal/handlers/recommendations"
	"trakt-api/internal/handlers/reference"
	"trakt-api/internal/handlers/reports"
	"trakt-api/internal/handlers/scrobble"
	"trakt-api/internal/handlers/seasons"
	"trakt-api/internal/handlers/social"
	"trakt-api/internal/handlers/stats"
	"trakt-api/internal/handlers/sync"
	"trakt-api/internal/handlers/syncs"
	"trakt-api/internal/handlers/updates"
	"trakt-api/internal/handlers/users"
	"trakt-api/internal/handlers/watch"
	"trakt-api/internal/handlers/webhooks"
	"trakt-api/internal/oauth"
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

	// Clerk webhook receiver (Phase 2: T2.2 — signature verified, no user auth required)
	api.Post("/webhooks/clerk", auth.HandleClerkWebhook)

	// Placeholder for v1 route groups (Phase 1+):
	// Search (public, API key)
	api.Get("/search", handleSearch)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search",
		Summary: "Full-text search across movies, shows, episodes, people, and lists",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Search results",
			400: "Invalid query parameters",
		},
	})

	api.Get("/search/exact", handleSearchExact)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/exact",
		Summary: "Exact title match for movies and shows",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Exact match found",
			404: "No exact match — candidates provided",
			400: "Invalid query parameters",
		},
	})

	api.Get("/search/lookup", handleSearchLookup)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/search/lookup",
		Summary: "Look up media by external ID (IMDB, TMDB, etc.)",
		Tags:    []string{"Search"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "Media found",
			404: "No media found for the given external ID",
			400: "Invalid query parameters",
		},
	})

	// Media read endpoints (Phase 1: T1.5)
	media.RegisterRoutes(api)

	// Watch providers endpoints (Phase 1: T1.10)
	watch.RegisterRoutes(api)

	// Calendars (Phase 1: T1.8)
	calendar.RegisterRoutes(api)

	// Updates (Phase 1: T1.9)
	updates.RegisterRoutes(api)

	// Discovery endpoints (Phase 1: T1.7)
	discovery.RegisterRoutes(api)

	// Episode detail endpoints (T1.x)
	episodes.RegisterRoutes(api)

	// Season detail endpoints (T1.x)
	seasons.RegisterRoutes(api)

	// Users endpoints (Phase 2: T2.6, T2.7)
	users.RegisterRoutes(api)

	// User stats (T3.8)
	stats.RegisterRoutes(api)

	// Sync sessions (T3.9)
	syncs.RegisterRoutes(api)

	// Sync history/collection/watchlist/ratings/favorites (T3.1 + T3.2)
	sync.RegisterRoutes(api)

	// Hidden items (T3.10)
	hidden.RegisterRoutes(api)

	// Lists + Smart Lists (T4.1 + T4.2)
	lists.RegisterRoutes(api)

	// Collaborations (T4.3)
	collab.RegisterRoutes(api)

	// Social — follows/friends (T4.4)
	social.RegisterRoutes(api)

	// Scrobble + playback (T3.3, T3.4)
	scrobble.RegisterRoutes(api)

	// Playback progress (T9b.2)
	progress.RegisterRoutes(api)

	// Checkin (T3.5)
	checkin.RegisterRoutes(api)

	// Comments (T4.5)
	comments.RegisterRoutes(api)

	// Activity feed (T4.6)
	activity.RegisterRoutes(api)

	// Notifications (T4.7)
	notifications.RegisterRoutes(api)

	// Webhooks (T8.1)
	webhooks.RegisterRoutes(api)

	// Recommendations (T9a.2 + T9a.3)
	recommendations.RegisterRoutes(api)

	// Integrations (T8.3)
	integrations.RegisterRoutes(api)
	integrations.RegisterReceiverRoutes(api)

	// Reference data endpoints (T9a.1)
	reference.RegisterRoutes(api)

	// Personal notes (T5.x)
	notes.RegisterRoutes(api)

	// Saved filters (T5.x)
	filters.RegisterRoutes(api)

	// Content reports (T5.x)
	reports.RegisterRoutes(api)

	// OAuth2 server (T2.4)
	oauth.RegisterRoutes(app)

	// API key management (T2.5) — TODO: auth.RegisterRoutes not yet implemented
	// auth.RegisterRoutes(api)

	_ = api
}
