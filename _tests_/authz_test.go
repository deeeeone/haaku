package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"trakt-api/internal/handlers/collab"
	"trakt-api/internal/handlers/integrations"
	"trakt-api/internal/handlers/recommendations"
	"trakt-api/internal/handlers/social"
	"trakt-api/internal/handlers/webhooks"

	"github.com/gofiber/fiber/v2"
)

// newAuthTestApp builds a Fiber app with the /v1 group and the given routes.
// No DB is needed: auth.AuthRequired rejects unauthenticated requests before
// any handler runs.
func newAuthTestApp(register func(fiber.Router)) *fiber.App {
	app := fiber.New()
	register(app.Group("/v1"))
	return app
}

// unauthStatus performs an unauthenticated request and returns the status code.
func unauthStatus(app *fiber.App, method, path string) (int, string) {
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// assertUnauthorized asserts the response is a 401 with the error envelope.
func assertUnauthorized(t *testing.T, app *fiber.App, method, path string) {
	t.Helper()
	status, body := unauthStatus(app, method, path)
	if status != http.StatusUnauthorized {
		t.Errorf("%s %s: got status %d, want 401", method, path, status)
	}
	if !strings.Contains(body, "unauthorized") {
		t.Errorf("%s %s: response body missing unauthorized envelope: %q", method, path, body)
	}
}

// ---------------------------------------------------------------------------
// Social
// ---------------------------------------------------------------------------

func TestSocialWriteRoutesRequireAuth(t *testing.T) {
	app := newAuthTestApp(social.RegisterRoutes)

	cases := []struct{ method, path string }{
		{"POST", "/v1/users/u1/follow"},
		{"DELETE", "/v1/users/u1/follow"},
		{"GET", "/v1/users/me/follow_requests"},
		{"POST", "/v1/users/me/follow_requests/f1/approve"},
		{"DELETE", "/v1/users/me/follow_requests/f1"},
	}
	for _, tc := range cases {
		assertUnauthorized(t, app, tc.method, tc.path)
	}
}

func TestSocialReadRoutesAllowAnonymous(t *testing.T) {
	// Followers/following/friends are public reads — they must NOT be 401.
	app := newAuthTestApp(social.RegisterRoutes)

	for _, path := range []string{
		"/v1/users/u1/followers",
		"/v1/users/u1/following",
		"/v1/users/u1/friends",
	} {
		status, _ := unauthStatus(app, "GET", path)
		if status == http.StatusUnauthorized {
			t.Errorf("GET %s: public read route must not require auth", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Collab
// ---------------------------------------------------------------------------

func TestCollabRoutesRequireAuth(t *testing.T) {
	app := newAuthTestApp(collab.RegisterRoutes)

	cases := []struct{ method, path string }{
		{"GET", "/v1/users/me/lists/collaborations"},
		{"POST", "/v1/lists/1/collaborators"},
		{"DELETE", "/v1/lists/1/collaborators/u2"},
		{"GET", "/v1/lists/1/collaborators"},
	}
	for _, tc := range cases {
		assertUnauthorized(t, app, tc.method, tc.path)
	}
}

// ---------------------------------------------------------------------------
// Integrations
// ---------------------------------------------------------------------------

func TestIntegrationsRoutesRequireAuth(t *testing.T) {
	app := newAuthTestApp(integrations.RegisterRoutes)

	cases := []struct{ method, path string }{
		{"POST", "/v1/integrations"},
		{"GET", "/v1/integrations"},
		{"DELETE", "/v1/integrations/1"},
	}
	for _, tc := range cases {
		assertUnauthorized(t, app, tc.method, tc.path)
	}
}

// ---------------------------------------------------------------------------
// Webhooks
// ---------------------------------------------------------------------------

func TestWebhooksRoutesRequireAuth(t *testing.T) {
	app := newAuthTestApp(webhooks.RegisterRoutes)

	cases := []struct{ method, path string }{
		{"GET", "/v1/webhooks"},
		{"POST", "/v1/webhooks"},
		{"PUT", "/v1/webhooks/1"},
		{"DELETE", "/v1/webhooks/1"},
		{"POST", "/v1/webhooks/1/rotate-secret"},
		{"POST", "/v1/webhooks/1/test"},
		{"GET", "/v1/webhooks/1/deliveries"},
	}
	for _, tc := range cases {
		assertUnauthorized(t, app, tc.method, tc.path)
	}
}

// ---------------------------------------------------------------------------
// Recommendations
// ---------------------------------------------------------------------------

func TestRecommendationsEndpointsRequireAuth(t *testing.T) {
	app := newAuthTestApp(recommendations.RegisterRoutes)

	cases := []struct{ method, path string }{
		{"GET", "/v1/recommendations/movies"},
		{"GET", "/v1/recommendations/shows"},
		{"GET", "/v1/social_recommendations/movies"},
		{"GET", "/v1/social_recommendations/shows"},
		{"GET", "/v1/media/anticipated"},
	}
	for _, tc := range cases {
		assertUnauthorized(t, app, tc.method, tc.path)
	}
}
