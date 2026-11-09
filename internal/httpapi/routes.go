package httpapi

import (
	"os"

	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/handlers/activity"
	"github.com/haaku-tv/haaku/internal/handlers/calendar"
	"github.com/haaku-tv/haaku/internal/handlers/checkin"
	"github.com/haaku-tv/haaku/internal/handlers/collab"
	"github.com/haaku-tv/haaku/internal/handlers/comments"
	"github.com/haaku-tv/haaku/internal/handlers/discovery"
	"github.com/haaku-tv/haaku/internal/handlers/episodes"
	"github.com/haaku-tv/haaku/internal/handlers/filters"
	"github.com/haaku-tv/haaku/internal/handlers/hidden"
	"github.com/haaku-tv/haaku/internal/handlers/integrations"
	"github.com/haaku-tv/haaku/internal/handlers/lists"
	"github.com/haaku-tv/haaku/internal/handlers/media"
	"github.com/haaku-tv/haaku/internal/handlers/notes"
	"github.com/haaku-tv/haaku/internal/handlers/notifications"
	"github.com/haaku-tv/haaku/internal/handlers/paritystub"
	"github.com/haaku-tv/haaku/internal/handlers/progress"
	"github.com/haaku-tv/haaku/internal/handlers/recommendations"
	"github.com/haaku-tv/haaku/internal/handlers/reference"
	"github.com/haaku-tv/haaku/internal/handlers/reports"
	"github.com/haaku-tv/haaku/internal/handlers/scrobble"
	"github.com/haaku-tv/haaku/internal/handlers/seasons"
	"github.com/haaku-tv/haaku/internal/handlers/social"
	"github.com/haaku-tv/haaku/internal/handlers/stats"
	"github.com/haaku-tv/haaku/internal/handlers/sync"
	"github.com/haaku-tv/haaku/internal/handlers/syncs"
	"github.com/haaku-tv/haaku/internal/handlers/updates"
	"github.com/haaku-tv/haaku/internal/handlers/users"
	"github.com/haaku-tv/haaku/internal/handlers/watch"
	"github.com/haaku-tv/haaku/internal/handlers/watchnow"
	"github.com/haaku-tv/haaku/internal/handlers/webhooks"
	"github.com/haaku-tv/haaku/internal/oauth"
	"github.com/haaku-tv/haaku/internal/openapi"

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

	// Parity stubs FIRST, on the app root rather than the /v1 group: the
	// generated routes already carry the /v1 prefix (scripts/parity-stubs.py
	// mounts from the contract path), so registering them on the group produced
	// /v1/v1/... and every stub was unreachable. First so a static stub segment
	// like /v1/smart-lists/templates is matched before the {id} routes; the
	// package skips any stub whose path a real handler already serves.
	paritystub.RegisterRoutes(app)

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

	// Readiness reports migration state, not just process liveness (issue #14):
	// handleReady answers 503 unless the Goose ledger matches the migrations this
	// binary carries. See internal/httpapi/readyz.go.
	app.Get("/readyz", handleReady)
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

	// The generated specification, served as JSON (issue #15). Built from the
	// same registry the router uses, so it cannot drift from the served API.
	api.Get("/openapi.json", handleOpenAPISpec)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/openapi.json",
		Summary: "OpenAPI 3.1 specification generated from the route registry",
		Tags:    []string{"Meta"},
		Auth:    "none",
		StatusCodes: map[int]string{
			200: "The OpenAPI document",
		},
	})

	// Clerk webhook receiver (Phase 2: T2.2 — signature verified, no user auth required)
	api.Post("/webhooks/clerk", auth.HandleClerkWebhook)

	// Dev-only Clerk webhook simulation: clerk-emulator can't deliver
	// webhooks in server mode (subscriptions are in-process only), so this
	// drives the same event pipeline manually. NEVER registered in production.
	if os.Getenv("APP_ENV") != "production" {
		api.Post("/dev/clerk-webhook-simulate", auth.HandleClerkWebhookSimulate)
		openapi.Register(openapi.RouteDef{
			Method:  "POST",
			Path:    "/v1/dev/clerk-webhook-simulate",
			Summary: "Simulate a Clerk webhook event (dev only — emulator cannot deliver webhooks; mirrors the real /v1/webhooks/clerk pipeline)",
			Tags:    []string{"Webhooks"},
			Auth:    "none",
			StatusCodes: map[int]string{
				200: "Event processed",
				400: "Invalid payload",
			},
		})
	}

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

	// Recent-search persistence and the exact-title contract route (issue #70).
	api.Post("/search/recent", auth.AuthRequired(), handlePostRecentSearch)
	api.Get("/search/recent_by_id/global/:type", handleRecentSearchesByID)

	// Contract-shaped search paths (static /search/exact and /search/lookup
	// are registered above, so the parameterised routes cannot swallow them).
	// The exact route must precede the two-segment id lookup: both three-segment
	// forms would otherwise match "/search/movie/exact" as an id lookup.
	api.Get("/search/:type/exact", handleContractSearchExact)
	api.Get("/search/:type", handleContractSearchType)
	api.Get("/search/:id_type/:id", handleContractSearchLookup)

	// Ingest-on-demand: turns an external id a client got from the live provider
	// search into a canonical local media row. Auth is required because it
	// writes (it creates the movie/show, its seasons and its episodes).
	api.Get("/media/resolve", auth.AuthRequired(), handleMediaResolve)
	openapi.Register(openapi.RouteDef{
		Method:  "GET",
		Path:    "/v1/media/resolve",
		Summary: "Resolve an external id (tmdb/tvdb/imdb) to canonical media, ingesting it from TMDB when it is not in the catalogue yet",
		Tags:    []string{"Search"},
		Auth:    "required",
		StatusCodes: map[int]string{
			200: "The canonical media record",
			400: "Invalid query parameters",
			401: "Unauthorized",
			404: "The provider has no such record",
			502: "The metadata provider could not be reached",
			503: "The metadata provider is not configured",
		},
	})

	// Discovery endpoints (Phase 1: T1.7)
	//
	// Registered BEFORE media below, and that order is load-bearing: Fiber matches
	// in registration order, so media's parameterised /movies/:id and /shows/:id
	// would otherwise capture "trending" and "popular" as an id, leaving
	// /v1/movies/{trending,popular} and /v1/shows/{trending,popular} unreachable.
	// internal/httpapi's TestNoStaticRouteIsShadowed guards the general rule: at a
	// given depth, static paths must be registered before parameterised ones.
	discovery.RegisterRoutes(api)

	// Media read endpoints (Phase 1: T1.5)
	media.RegisterRoutes(api)

	// Watch providers endpoints (Phase 1: T1.10)
	watch.RegisterRoutes(api)

	// Watch-now provider catalogue (issue #69).
	watchnow.RegisterRoutes(api)

	// Calendars (Phase 1: T1.8)
	calendar.RegisterRoutes(api)

	// Updates (Phase 1: T1.9)
	updates.RegisterRoutes(api)

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
	// Remaining contract-shaped sync reads/writes (issue #106). Registered
	// after RegisterRoutes (so /watchlist/reorder precedes /watchlist/:id) and
	// before RegisterContractRoutes (so static /collection/media precedes
	// /collection/:type).
	sync.RegisterRemainingRoutes(api)
	// Contract-shaped sync reads (Trakt's /sync/{collection}/{type}[/...]).
	sync.RegisterContractRoutes(api)

	// Hidden items (T3.10)
	hidden.RegisterRoutes(api)

	// Lists + Smart Lists (T4.1 + T4.2)
	lists.RegisterRoutes(api)
	// Contract-shaped public list feeds (/lists/{popular,trending}/{type}).
	lists.RegisterContractRoutes(api)

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
	// Contract-shaped trending comments (/comments/trending/{comment_type}/{type}).
	comments.RegisterContractRoutes(api)

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

	// API key management (T2.5)
	auth.RegisterAPIKeyRoutes(api)

	// Local account management (issue #53). RegisterLocalAuthConfig is always
	// registered — it is the one public way a client learns which mode this
	// instance runs, and in clerk mode it is the only one of these routes that
	// exists. RegisterLocalAuthRoutes registers the signup/login/logout/reset
	// handlers AND their OpenAPI definitions only in local mode, so the
	// registry and the generated spec both prove the module is absent in clerk
	// mode rather than merely switched off at runtime.
	auth.RegisterLocalAuthConfig(api)
	auth.RegisterLocalAuthRoutes(api)

	// Public-profile read surface (issue #4, Users section): /v1/users/{id}/{history,
	// ratings, watchlist, collection, watching, comments, notes, likes} + the
	// caller's /v1/users/me/likes/{comments,lists}.
	//
	// Registered LAST on purpose: these paths are /users/:id/<segment> and Fiber
	// matches in registration order, so they must come after every static
	// /users/me/<segment> route (notes, lists, social, stats, hidden, filters,
	// api keys) or ":id" would swallow "me".
	// TestNoStaticRouteIsShadowed guards the ordering.
	users.RegisterProfileReadRoutes(api)
	// Contract-shaped user reads (/users/settings is registered in
	// users.RegisterRoutes; this adds the sorted per-type watchlist path).
	users.RegisterContractRoutes(api)

	// Contract-shaped list management (issue #105): list CRUD, items,
	// likes/comments/report, collaborations and smart lists at Trakt's paths.
	// Registered LAST so the static /users/me/lists[/collaborations] routes
	// registered earlier win, and nothing here shadows a later static route.
	lists.RegisterManagementContractRoutes(api)

	_ = api
}
