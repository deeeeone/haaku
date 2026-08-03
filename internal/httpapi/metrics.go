package httpapi

import (
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Metrics counters — tracked with atomic int64, reset on restart.
var (
	startTime      = time.Now()
	totalRequests  atomic.Int64
	errorCount     atomic.Int64
	rateLimited    atomic.Int64
	activeRequests atomic.Int64
)

// IncTotalRequests atomically increments the total request counter.
func IncTotalRequests() { totalRequests.Add(1) }

// IncErrors atomically increments the error counter (4xx/5xx).
func IncErrors() { errorCount.Add(1) }

// IncRateLimited atomically increments the rate-limited counter.
func IncRateLimited() { rateLimited.Add(1) }

// IncActiveRequests atomically increments active request count.
func IncActiveRequests() { activeRequests.Add(1) }

// DecActiveRequests atomically decrements active request count.
func DecActiveRequests() { activeRequests.Add(-1) }

// metricsResponse is the JSON shape for GET /metrics.
type metricsResponse struct {
	UptimeSeconds  int64 `json:"uptime_seconds"`
	TotalRequests  int64 `json:"total_requests"`
	Errors         int64 `json:"errors"`
	RateLimited    int64 `json:"rate_limited"`
	ActiveRequests int64 `json:"active_requests"`
}

// metricsHandler handles GET /metrics — public, no auth.
// Returns JSON with service health counters.
func metricsHandler(c *fiber.Ctx) error {
	resp := metricsResponse{
		UptimeSeconds:  int64(time.Since(startTime).Seconds()),
		TotalRequests:  totalRequests.Load(),
		Errors:         errorCount.Load(),
		RateLimited:    rateLimited.Load(),
		ActiveRequests: activeRequests.Load(),
	}
	return c.JSON(resp)
}

// registerMetricsRoute registers the /metrics endpoint on the app.
// This is infrastructure, not an API route — registered directly, not via routes.go.
func registerMetricsRoute(app *fiber.App) {
	app.Get("/metrics", metricsHandler)
}
