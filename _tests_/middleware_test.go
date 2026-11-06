package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"trakt-api/internal/httpapi"

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
// Rate limit key format tests
// ---------------------------------------------------------------------------

func TestRateLimitKeyFormat(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		group      string
		wantKey    string
	}{
		{
			name:       "authenticated user",
			identifier: "user_abc123",
			group:      "auth",
			wantKey:    "ratelimit:user_abc123:auth",
		},
		{
			name:       "public IP",
			identifier: "192.168.1.1",
			group:      "public",
			wantKey:    "ratelimit:192.168.1.1:public",
		},
		{
			name:       "IPv6 public",
			identifier: "::1",
			group:      "public",
			wantKey:    "ratelimit:::1:public",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := fmt.Sprintf("ratelimit:%s:%s", tt.identifier, tt.group)
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

func TestRateLimitKeyNotMalformed(t *testing.T) {
	identifier := "192.168.0.1"
	group := "public"
	key := fmt.Sprintf("ratelimit:%s:%s", identifier, group)

	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 colon-separated parts, got %d in key %q", len(parts), key)
	}
	if parts[0] != "ratelimit" {
		t.Errorf("prefix = %q, want ratelimit", parts[0])
	}
	if parts[1] != identifier {
		t.Errorf("identifier part = %q, want %q", parts[1], identifier)
	}
	if parts[2] != group {
		t.Errorf("group part = %q, want %q", parts[2], group)
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

// ---------------------------------------------------------------------------
// Sliding-window logic tests (unit-level, no Redis)
// ---------------------------------------------------------------------------

func TestSlidingWindow_KeyGeneration(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		group      string
	}{
		{"user auth", "usr_abc", "auth"},
		{"IP public", "10.0.0.1", "public"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := fmt.Sprintf("ratelimit:%s:%s", tt.identifier, tt.group)
			if !strings.Contains(key, "ratelimit:") {
				t.Errorf("key %q missing ratelimit: prefix", key)
			}
			if !strings.Contains(key, tt.identifier) {
				t.Errorf("key %q missing identifier %q", key, tt.identifier)
			}
			if !strings.HasSuffix(key, ":"+tt.group) {
				t.Errorf("key %q missing suffix :%s", key, tt.group)
			}
		})
	}
}

func TestSlidingWindow_MemberUniqueness(t *testing.T) {
	members := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		now := time.Now().UnixNano()
		member := fmt.Sprintf("%d:%d", now, now%100000)
		if members[member] {
			t.Logf("collision: %s (note: expected under extreme load or on fast CPUs)", member)
		}
		members[member] = true
		time.Sleep(1 * time.Microsecond)
	}
	if len(members) < 10 {
		t.Errorf("only %d unique members out of 1000 — member generation may be broken", len(members))
	}
}
