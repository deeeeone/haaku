package httpapi

// Registry-vs-mount drift guard.
//
// The OpenAPI registry and the router are two separate statements of the same
// surface: every RouteDef says an endpoint exists, RegisterRoutes is what makes
// it reachable. Nothing compared them, so calendar show routes shipped in both
// the registry and openapi.yaml while the running server answered
// {"error":{"code":"not_found","message":"unknown route"}}. Handler tests miss
// this because they wire the handler directly; inventory tests miss it because
// the registry does contain the route.
//
// This test builds the real app router and calls every registered GET route
// with sample path parameters. Any answer is acceptable except Fiber's own
// route-not-found envelope — a handler's legitimate 404 ("movie not found")
// carries a different message and is fine.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

var routeParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

// sampleRouteParams gives path parameters a plausible value. Anything absent
// falls back to "1"; an invalid-but-matched value still proves the route is
// mounted, which is all this guard asserts.
var sampleRouteParams = map[string]string{
	"id":           "1",
	"type":         "movies",
	"sort":         "popular",
	"season":       "1",
	"episode":      "1",
	"start_date":   "2026-01-01",
	"days":         "7",
	"period":       "weekly",
	"lang":         "en",
	"section":      "movies",
	"comment_type": "all",
}

func sampleRoutePath(routePath string) string {
	return routeParamPattern.ReplaceAllStringFunc(routePath, func(param string) string {
		name := strings.Trim(param, "{}")
		if value, ok := sampleRouteParams[name]; ok {
			return value
		}
		return "1"
	})
}

// errorEnvelopeBody is the shared {"error":{"code","message"}} shape.
type errorEnvelopeBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// isUnknownRoute reports whether the response is Fiber's unmatched-route 404
// rendered by the app error handler, rather than a handler's own 404.
func isUnknownRoute(status int, body []byte) bool {
	if status != fiber.StatusNotFound {
		return false
	}
	var envelope errorEnvelopeBody
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	return envelope.Error.Code == "not_found" && envelope.Error.Message == "unknown route"
}

func TestEveryRegisteredGetRouteIsMounted(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Use(Recovery())
	RegisterRoutes(app)

	checked := 0
	for _, route := range openapi.AllRoutes() {
		if route.Method != http.MethodGet {
			continue
		}
		requestPath := sampleRoutePath(route.Path)
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, requestPath, nil), -1)
		if err != nil {
			t.Errorf("%s %s (request %s): %v", route.Method, route.Path, requestPath, err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			t.Errorf("%s %s: read body: %v", route.Method, route.Path, readErr)
			continue
		}
		if isUnknownRoute(resp.StatusCode, body) {
			t.Errorf("%s %s is in the registry but not mounted: request %s answered %d %s",
				route.Method, route.Path, requestPath, resp.StatusCode, body)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no GET RouteDefs checked — the guard would pass vacuously")
	}
}

// TestIsUnknownRoute pins the sentinel matcher, so the guard cannot pass by
// being unable to recognize the failure it exists to catch.
func TestIsUnknownRoute(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"fiber unmatched", http.StatusNotFound, `{"error":{"code":"not_found","message":"unknown route"}}`, true},
		{"handler not found", http.StatusNotFound, `{"error":{"code":"not_found","message":"movie not found"}}`, false},
		{"other status", http.StatusOK, `{"error":{"code":"not_found","message":"unknown route"}}`, false},
		{"not an envelope", http.StatusNotFound, `[]`, false},
	}
	for _, tc := range cases {
		if got := isUnknownRoute(tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: isUnknownRoute(%d, %s) = %v, want %v", tc.name, tc.status, tc.body, got, tc.want)
		}
	}
}
