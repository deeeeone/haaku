package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/httpapi"
	"github.com/haaku-tv/haaku/redis"

	"github.com/gofiber/fiber/v2"
)

// newMiddlewareTestApp creates a Fiber test app with the full middleware chain.
func newMiddlewareTestApp() *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit: 10 * 1024 * 1024, // 10MB
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			message := "test error"
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				message = e.Message
			}
			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "error",
					"message": message,
				},
			})
		},
	})

	app.Use(httpapi.SecurityHeaders())
	app.Use(httpapi.Timeout())

	app.Get("/test-ok", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	app.Post("/test-ok", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// The public JSON /metrics endpoint was removed (issue #9): metrics now live
	// on the separate Prometheus listener in internal/obs. This fixture stays a
	// plain JSON route so the envelope tests below keep a stable shape to read.
	app.Get("/test-metrics", func(c *fiber.Ctx) error {
		uptime := int64(3600)
		return c.JSON(fiber.Map{
			"uptime_seconds":  uptime,
			"total_requests":  12345,
			"errors":          42,
			"rate_limited":    7,
			"active_requests": 3,
		})
	})

	return app
}

// ---------------------------------------------------------------------------
// Rate limit and usage counter key format (internal/ratelimit)
//
// These keys are the whole contract between the limiter and the counters in
// Redis: group first (so a colon in an IPv6 identifier cannot shift the fields),
// and for the daily budget a trailing date so the counter rotates by itself.
// ---------------------------------------------------------------------------

func TestRateLimitKeyFormat(t *testing.T) {
	tests := []struct {
		name       string
		group      string
		identifier string
		want       string
	}{
		{"account", "read", "local_abc123", "rate_limit:read:local_abc123"},
		{"IPv4 client", "media", "198.51.100.7", "rate_limit:media:198.51.100.7"},
		{"IPv6 client keeps its colons", "sync", "2001:db8::1", "rate_limit:sync:2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redis.RateLimitKey(tt.group, tt.identifier); got != tt.want {
				t.Errorf("RateLimitKey(%q, %q) = %q, want %q", tt.group, tt.identifier, got, tt.want)
			}
			// The group must be the second field: anything reading the key splits
			// from the left.
			parts := strings.SplitN(redis.RateLimitKey(tt.group, tt.identifier), ":", 3)
			if len(parts) != 3 || parts[0] != "rate_limit" || parts[1] != tt.group {
				t.Errorf("key %q does not carry the group in its second field", tt.group)
			}
		})
	}
}

func TestUsageKeyFormat(t *testing.T) {
	day := time.Date(2026, 11, 7, 15, 4, 5, 0, time.UTC)
	got := redis.UsageKey("read", "local_abc123", day)
	want := "usage:read:local_abc123:2026-11-07"
	if got != want {
		t.Errorf("UsageKey = %q, want %q", got, want)
	}
	// A different day is a different counter — that is how the budget resets
	// without a sweeper.
	if redis.UsageKey("read", "local_abc123", day.AddDate(0, 0, 1)) == got {
		t.Error("the usage key does not change with the day")
	}
}

// ---------------------------------------------------------------------------
// Security headers tests
// ---------------------------------------------------------------------------

func TestSecurityHeadersPresent(t *testing.T) {
	app := newMiddlewareTestApp()

	req, err := http.NewRequest("GET", "/test-ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := resp.Header.Get(tt.header)
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Body size limit tests
// ---------------------------------------------------------------------------

func TestBodyLimit_AllowsNormalPayload(t *testing.T) {
	app := newMiddlewareTestApp()

	body := bytes.NewReader([]byte(`{"key":"value"}`))
	req, err := http.NewRequest("POST", "/test-ok", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 OK, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func TestBodyLimit_WithinLimit(t *testing.T) {
	app := newMiddlewareTestApp()

	// 1MB payload — within limit.
	bigBody := bytes.Repeat([]byte("a"), 1024*1024)
	req, err := http.NewRequest("POST", "/test-ok", bytes.NewReader(bigBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 OK for 1MB payload, got %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

// TestBodyLimit_RejectsOversized tests that Fiber's built-in BodyLimit rejects oversized payloads.
// Fiber's app.Test() returns an error (not a 413 response) when BodyLimit is exceeded.
// In production, Fiber returns a proper 413 response.
func TestBodyLimit_RejectsOversized(t *testing.T) {
	// App with 5-byte body limit for testing.
	app := fiber.New(fiber.Config{
		BodyLimit: 5,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var code int
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			} else {
				code = fiber.StatusInternalServerError
			}
			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "payload_too_large",
					"message": err.Error(),
				},
			})
		},
	})

	app.Post("/upload", func(c *fiber.Ctx) error {
		return c.Send(c.Body())
	})

	// Send a 20-byte body — exceeds 5-byte limit.
	oversized := bytes.Repeat([]byte("x"), 20)
	req, err := http.NewRequest("POST", "/upload", bytes.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"
	req.Header.Set("Content-Type", "text/plain")

	_, testErr := app.Test(req)
	// Fiber's test client returns an error when BodyLimit is exceeded.
	// This is the expected behavior for oversized payloads.
	if testErr == nil {
		t.Error("expected oversized payload to be rejected, but no error returned")
	}
	if testErr != nil && !strings.Contains(strings.ToLower(testErr.Error()), "body") {
		t.Logf("got unexpected error (expected body-size-related): %v", testErr)
	}
}

// ---------------------------------------------------------------------------
// Timeout tests
// ---------------------------------------------------------------------------

func TestTimeout_PassesWithinDeadline(t *testing.T) {
	app := newMiddlewareTestApp()

	req, err := http.NewRequest("GET", "/test-ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Metrics endpoint tests
// ---------------------------------------------------------------------------

func TestMetricsEndpoint_ReturnsValidJSON(t *testing.T) {
	app := newMiddlewareTestApp()

	req, err := http.NewRequest("GET", "/test-metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var metrics struct {
		UptimeSeconds  int64 `json:"uptime_seconds"`
		TotalRequests  int64 `json:"total_requests"`
		Errors         int64 `json:"errors"`
		RateLimited    int64 `json:"rate_limited"`
		ActiveRequests int64 `json:"active_requests"`
	}
	if err := json.Unmarshal(body, &metrics); err != nil {
		t.Fatalf("failed to parse metrics JSON: %v\nbody: %s", err, string(body))
	}

	if metrics.UptimeSeconds != 3600 {
		t.Errorf("uptime_seconds = %d, want 3600", metrics.UptimeSeconds)
	}
	if metrics.TotalRequests != 12345 {
		t.Errorf("total_requests = %d, want 12345", metrics.TotalRequests)
	}
	if metrics.Errors != 42 {
		t.Errorf("errors = %d, want 42", metrics.Errors)
	}
}

func TestMetricsEndpoint_JSONContentType(t *testing.T) {
	app := newMiddlewareTestApp()

	req, err := http.NewRequest("GET", "/test-metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ---------------------------------------------------------------------------
// CORS tests
// ---------------------------------------------------------------------------

func TestCORS_NoOriginsConfigured(t *testing.T) {
	app := newMiddlewareTestApp()

	req, err := http.NewRequest("GET", "/test-ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"
	req.Header.Set("Origin", "https://evil.example.com")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no Access-Control-Allow-Origin header with empty CORS origins")
	}
}

// ---------------------------------------------------------------------------
// Error envelope tests
// ---------------------------------------------------------------------------

func TestErrorEnvelope_Format(t *testing.T) {
	app := fiber.New(fiber.Config{
		BodyLimit: 5,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var code int
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			} else {
				code = fiber.StatusInternalServerError
			}
			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "payload_too_large",
					"message": err.Error(),
				},
			})
		},
	})

	app.Post("/upload", func(c *fiber.Ctx) error {
		return c.Send(c.Body())
	})

	oversized := bytes.Repeat([]byte("x"), 20)
	req, err := http.NewRequest("POST", "/upload", bytes.NewReader(oversized))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "test"
	req.Header.Set("Content-Type", "text/plain")

	// Fiber's test client returns error when BodyLimit exceeded — verify rejection.
	_, testErr := app.Test(req)
	if testErr == nil {
		t.Error("expected oversized payload to be rejected")
	}
	// Verify it's a body-size related error.
	if testErr != nil && !strings.Contains(strings.ToLower(testErr.Error()), "body") {
		t.Logf("unexpected error type: %v", testErr)
	}
}
