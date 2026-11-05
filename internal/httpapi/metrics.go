package httpapi

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"trakt-api/internal/obs"
)

// HTTP observability lives on the separate metrics listener (internal/obs),
// served in Prometheus format. The former hand-rolled JSON /metrics handler —
// five atomic counters, reset on restart, public on the main listener — is gone:
// it leaked traffic volume to anyone who could reach the app and described
// nothing (no route, no latency, no status).
//
// Recording happens at two sites because Fiber runs the app-level error handler
// *after* the middleware chain returns (see errors.go and security.go): the
// logger records the response directly, and the error handler records it once the
// status is final. finishRequest below is that shared completion path.

const (
	// routeUnmatched is the single label for any request that matched no
	// registered route, including the SPA catch-all. Using one sentinel instead
	// of the raw path is what keeps a hostile client from minting a series per
	// unique URL.
	routeUnmatched = "unmatched"

	// locals keys on the request context. Kept unexported and namespaced.
	localsReqStart = "obs_req_start"
)

// knownMethods bounds the method label to real verbs: Fiber accepts an arbitrary
// method token, so echoing c.Method() verbatim would be unbounded too.
var knownMethods = map[string]struct{}{
	fiber.MethodGet:     {},
	fiber.MethodHead:    {},
	fiber.MethodPost:    {},
	fiber.MethodPut:     {},
	fiber.MethodPatch:   {},
	fiber.MethodDelete:  {},
	fiber.MethodOptions: {},
}

// routeLabel returns the registered route pattern for matched requests, or the
// "unmatched" sentinel otherwise. Fiber reports "/" both for an unmatched
// request and for the middleware tree (which is where the SPA handler lives);
// neither is a route name, and c.Route().Path is a pattern — never the raw path —
// for anything that did match.
func routeLabel(c *fiber.Ctx) string {
	p := c.Route().Path
	if p == "" || p == "/" {
		return routeUnmatched
	}
	return p
}

func methodLabel(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return "other"
}

// beginRequest stamps the start time and counts the request as in flight. The
// start time is stashed on the context so the completion site — which may be the
// error handler, not the middleware — can compute latency.
func beginRequest(c *fiber.Ctx, start time.Time) {
	c.Locals(localsReqStart, start)
	obs.IncInFlight()
}

// finishRequest records the final status and latency and emits the completion
// log line. Call it exactly once per request, at the point the response status is
// final.
func finishRequest(c *fiber.Ctx) {
	start, _ := c.Locals(localsReqStart).(time.Time)
	if start.IsZero() {
		start = time.Now()
	}
	status := c.Response().StatusCode()
	obs.ObserveHTTPRequest(routeLabel(c), methodLabel(c.Method()), status, time.Since(start).Seconds())

	logCompleted(c, status, start)
}

// logCompleted writes the structured completion line. Fields are unchanged from
// the original logger: method, path, status, latency_ms, remote_ip.
func logCompleted(c *fiber.Ctx, status int, start time.Time) {
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0
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
}

// IncRateLimited counts a request rejected by the rate limiter. Rate limiting
// runs before routing, so the counter carries no route label.
func IncRateLimited() { obs.IncRateLimited() }
