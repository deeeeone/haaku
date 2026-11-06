package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

// The two recommendation endpoints must fail closed. A 200 with a fabricated
// empty array is forbidden (AGENTS.md, issue #4): a made-up payload is worse
// than an honest 501. This asserts the wire behaviour so the stub cannot
// silently regress to a fabricated 200.
func TestRecommendationRoutesAnswer501(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app.Group("/v1"))

	for _, path := range []string{
		"/v1/movies/recommendations/123",
		"/v1/shows/recommendations/123",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test(%s): %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("%s answered %d, want 501 — an honest 501, never a fabricated 200", path, resp.StatusCode)
		}

		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode error envelope: %v", path, err)
		}
		if body.Error.Code != "not_implemented" {
			t.Errorf("%s error code = %q, want not_implemented", path, body.Error.Code)
		}
	}
}

// The RouteDef is what puts 501 into openapi.yaml, so the spec and the handler
// must agree. If a RouteDef dropped its 501 the spec would advertise the routes
// as implemented (and the parity gate would stop treating them as stubs) while
// the handler kept answering 501 — this couples the two so they cannot drift.
func TestRecommendationRouteDefsDeclare501(t *testing.T) {
	want := map[string]bool{
		"/v1/movies/recommendations/{id}": false,
		"/v1/shows/recommendations/{id}":  false,
	}
	for _, d := range openapi.AllRoutes() {
		if _, ok := want[d.Path]; !ok {
			continue
		}
		if _, ok := d.StatusCodes[501]; !ok {
			t.Errorf("RouteDef %s declares no 501 response, so the spec would claim it is implemented", d.Path)
		}
		want[d.Path] = true
	}
	for path, found := range want {
		if !found {
			t.Errorf("no RouteDef registered for %s", path)
		}
	}
}
