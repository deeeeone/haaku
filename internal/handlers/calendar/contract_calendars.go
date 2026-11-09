package calendar

// Contract-shaped show feeds (issue #69).
//
// Trakt's calendar endpoints do not paginate: they return an array of releases
// for the window. The pre-existing /v1/calendars/{all,my}/shows routes returned
// our own paginated envelope, so these mounts replace them at the same paths
// with the pin middleware that switches the shared handler to the contract
// array. The friends/favorites/watched targets are ours, not Trakt's, and keep
// the envelope.

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/haaku-tv/haaku/internal/auth"
	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
)

func init() {
	for _, kind := range contractCalendarKinds {
		for _, target := range []string{"all", "my"} {
			openapi.Register(openapi.RouteDef{
				Method:      "GET",
				Path:        "/v1/calendars/" + target + "/shows/" + kind.segment + "/{start_date}/{days}",
				Summary:     "Show calendar for the " + kind.segment + " feed (contract path)",
				Tags:        []string{"Calendars"},
				Auth:        authForTarget(target),
				StatusCodes: map[int]string{200: "Calendar show entries"},
			})
		}
	}
}

// registerContractShowRoutes mounts the plain contract calendar and the
// new/premieres/finales feeds for both contract targets.
func registerContractShowRoutes(calendars fiber.Router) {
	for _, target := range []string{"all", "my"} {
		calendars.Get("/"+target+"/shows/:start_date/:days", contractMiddlewares(target, "")...)
		for _, kind := range contractCalendarKinds {
			calendars.Get("/"+target+"/shows/"+kind.segment+"/:start_date/:days",
				contractMiddlewares(target, kind.subtype)...)
		}
	}
}

// contractMiddlewares builds the registration chain for one contract feed:
// auth first for the user-scoped target, then the Locals pin, then the shared
// handler. The handler MUST be part of the chain: a chain that ends with the
// pin middleware leaves its c.Next() with nothing to call, which Fiber turns
// into its "unknown route" 404 — exactly how these feeds shipped unreachable.
func contractMiddlewares(target, subtype string) []fiber.Handler {
	handlers := make([]fiber.Handler, 0, 3)
	if target == "my" {
		handlers = append(handlers, auth.AuthRequired())
	}
	handlers = append(handlers, pinCalendarFeed(target, subtype), handleCalendarShows)
	return handlers
}

// pinCalendarFeed labels a contract-shaped feed for handleCalendarShows without
// changing its path shape.
func pinCalendarFeed(target, subtype string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("calendar_target", target)
		c.Locals("calendar_contract", true)
		if subtype != "" {
			c.Locals("calendar_type", subtype)
		}
		return c.Next()
	}
}

// pinCalendarTarget selects the target for a handler that has no :target path
// segment (the contract spells the target literally).
func pinCalendarTarget(target string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("calendar_target", target)
		return c.Next()
	}
}

// calendarTarget resolves the target from the middleware pin, falling back to
// the legacy :target path segment for the routes that still carry one.
func calendarTarget(c *fiber.Ctx) string {
	if v, ok := c.Locals("calendar_target").(string); ok && v != "" {
		return v
	}
	return deriveTarget(c.Path())
}

// parseCalendarWindow parses the shared {start_date}/{days} path pair. The
// window is [start_date, start_date+days): the start date is included, the end
// is exclusive, so `days` counts calendar days.
func parseCalendarWindow(c *fiber.Ctx) (time.Time, int, error) {
	startDate, err := time.Parse("2006-01-02", c.Params("start_date"))
	if err != nil {
		return time.Time{}, 0, response.BadRequest(c, "invalid start_date format, expected YYYY-MM-DD")
	}
	return startDate, clampDays(c.Params("days")), nil
}

// authForTarget maps a contract target onto the OpenAPI auth label.
func authForTarget(target string) string {
	if target == "my" {
		return "required"
	}
	return "none"
}
