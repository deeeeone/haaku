package httpapi

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"trakt-api/internal/config"
	"trakt-api/internal/obs"
)

// httpSeries is one observation of tkiller_http_requests_total, keyed by the
// route and status labels.
type httpSeries struct {
	route  string
	method string
	status string
	count  float64
}

// gatherHTTPSeries reads the process registry and returns every
// tkiller_http_requests_total series.
func gatherHTTPSeries(t *testing.T) []httpSeries {
	t.Helper()
	mfs, err := obs.Gatherer().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var out []httpSeries
	for _, mf := range mfs {
		if mf.GetName() != "tkiller_http_requests_total" {
			continue
		}
		for _, m := range mf.Metric {
			s := httpSeries{count: m.GetCounter().GetValue()}
			for _, lp := range m.Label {
				switch lp.GetName() {
				case "route":
					s.route = lp.GetValue()
				case "method":
					s.method = lp.GetValue()
				case "status":
					s.status = lp.GetValue()
				}
			}
			out = append(out, s)
		}
	}
	return out
}

// TestRouteLabelUsesPatternNotRawPath is the cardinality regression: two requests
// to the same parameterised route must land on one series labelled with the
// pattern, and no series may carry a raw path. Without the pattern, a client
// walking ids would mint a new time series per id.
func TestRouteLabelUsesPatternNotRawPath(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: observedErrorHandler})
	app.Use(RequestLogger())
	app.Get("/v1/users/:id", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, path := range []string{"/v1/users/alice", "/v1/users/bob"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", path, resp.StatusCode)
		}
	}
	// An unknown route must collapse to the sentinel, not leak its raw path.
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/nope", nil))
	if err != nil {
		t.Fatalf("GET /v1/nope: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("GET /v1/nope: status %d, want 404", resp.StatusCode)
	}

	series := gatherHTTPSeries(t)

	var patternCount float64
	for _, s := range series {
		switch s.route {
		case "/v1/users/alice", "/v1/users/bob", "/v1/nope":
			t.Errorf("series leaked the raw request path: route=%q status=%q", s.route, s.status)
		case "/v1/users/:id":
			if s.method == "GET" && s.status == "200" {
				patternCount += s.count
			}
		}
	}
	if patternCount != 2 {
		t.Errorf("series route=\"/v1/users/:id\" status=\"200\" = %v, want 2", patternCount)
	}

	var sawUnmatched bool
	for _, s := range series {
		if s.route == routeUnmatched && s.status == "404" {
			sawUnmatched = true
		}
	}
	if !sawUnmatched {
		t.Error(`no series with route="unmatched" status="404" for the unknown route`)
	}
}

// TestRequestLoggerReportsFinalStatusForErrorHandledRoute is the bug regression:
// a request the client receives as 404 must be logged as 404, not the 200 the
// response still carried inside the middleware.
func TestRequestLoggerReportsFinalStatusForErrorHandledRoute(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	app := fiber.New(fiber.Config{ErrorHandler: observedErrorHandler})
	app.Use(RequestLogger())
	app.Get("/v1/ok", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	// Success path still logs.
	if _, err := app.Test(httptest.NewRequest("GET", "/v1/ok", nil)); err != nil {
		t.Fatalf("GET /v1/ok: %v", err)
	}
	if okLogs := buf.String(); !strings.Contains(okLogs, "status=200") {
		t.Errorf("success path not logged with status=200: %q", okLogs)
	}
	buf.Reset()

	resp, err := app.Test(httptest.NewRequest("GET", "/v1/nope", nil))
	if err != nil {
		t.Fatalf("GET /v1/nope: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("client status = %d, want 404", resp.StatusCode)
	}

	logs := buf.String()
	if !strings.Contains(logs, "status=404") {
		t.Errorf("logger did not report the final status: %q", logs)
	}
	if strings.Contains(logs, "status=200") {
		t.Errorf("logger reported the pre-error-handler status 200: %q", logs)
	}
	if n := strings.Count(logs, "request completed"); n != 1 {
		t.Errorf("completion lines = %d, want exactly 1 (error path must not double-log): %q", n, logs)
	}
}

// TestPublicMetricsRouteRemoved pins the security fix: /metrics no longer answers
// on the main listener. It now lives on the separate obs listener.
func TestPublicMetricsRouteRemoved(t *testing.T) {
	app := NewServer(&config.Config{Env: "test"})

	resp, err := app.Test(httptest.NewRequest("GET", "/metrics", nil))
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("GET /metrics on the public listener: status %d, want 404", resp.StatusCode)
	}
}
