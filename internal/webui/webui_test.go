package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/web"
)

// newApp mounts the UI handler exactly as the server does: after the API routes,
// with a route that shadows nothing. devFrontendURL is the pairing handed to
// Handler (empty is the production case).
func newApp(t *testing.T, devFrontendURL string) *fiber.App {
	t.Helper()

	app := fiber.New()
	app.Get("/v1/known", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })

	h, err := Handler(devFrontendURL)
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	app.Use(h)
	return app
}

func get(t *testing.T, app *fiber.App, path string) (int, string) {
	t.Helper()

	resp := getResp(t, app, path)
	defer func() { _ = resp.Body.Close() }()

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

// getResp returns the whole response, for the cases that assert on headers.
func getResp(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("app.Test(%q) error = %v", path, err)
	}
	return resp
}

// TestServesIndexForRoot asserts the SPA shell is served at "/".
func TestServesIndexForRoot(t *testing.T) {
	status, body := get(t, newApp(t, ""), "/")

	if status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "<html") {
		t.Fatalf("body is not HTML: %q", body)
	}
}

// TestServesIndexForUnknownClientRoute asserts a deep link to a client-side route
// returns the shell rather than a 404, which is what makes reloads work.
func TestServesIndexForUnknownClientRoute(t *testing.T) {
	status, body := get(t, newApp(t, ""), "/movies/123")

	if status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "<html") {
		t.Fatalf("body is not HTML: %q", body)
	}
}

// TestUnknownAPIPathIsNotTheSPA is the regression that matters most: an unknown
// API path must stay a JSON 404, never the HTML shell.
func TestUnknownAPIPathIsNotTheSPA(t *testing.T) {
	for _, path := range []string{"/v1/nope", "/oauth/nope", "/healthz/nope"} {
		status, body := get(t, newApp(t, ""), path)

		if status != fiber.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, status)
		}
		if strings.Contains(body, "<html") {
			t.Errorf("%s: served the SPA shell to an API path: %q", path, body)
		}
	}
}

// TestKnownAPIRouteStillMatches asserts the catch-all does not shadow real routes.
func TestKnownAPIRouteStillMatches(t *testing.T) {
	status, body := get(t, newApp(t, ""), "/v1/known")

	if status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("body = %q, want the JSON handler response", body)
	}
}

// TestIsAPIPath documents the prefix rules, including the boundary case where a
// client route merely shares a prefix with an API path.
func TestIsAPIPath(t *testing.T) {
	cases := map[string]bool{
		"/v1":              true,
		"/v1/movies":       true,
		"/oauth/authorize": true,
		"/metrics":         true,
		"/v10":             false,
		"/movies":          false,
		"/":                false,
	}

	for path, want := range cases {
		if got := isAPIPath(path); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestResolveAssetRejectsEscape asserts traversal is neutralised rather than
// passed through to the filesystem.
func TestResolveAssetRejectsEscape(t *testing.T) {
	name, ok := resolveAsset("/../../etc/passwd")
	if ok && strings.Contains(name, "..") {
		t.Fatalf("resolveAsset returned a traversing path: %q", name)
	}
	if ok && strings.HasPrefix(name, "/") {
		t.Fatalf("resolveAsset returned an absolute path: %q", name)
	}
}

// TestDistFSIsReadable guards the embed directive: if dist or index.html stops
// being embedded, the binary would panic or serve nothing at runtime.
func TestDistFSIsReadable(t *testing.T) {
	dist, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub error = %v", err)
	}
	if _, err := fs.ReadFile(dist, "index.html"); err != nil {
		t.Fatalf("dist/index.html is not embedded: %v", err)
	}
}

// --- Development pairing: the UI lives on the Vite dev server ----------------
//
// A source run is two processes (see the Makefile's `dev` target). These tests
// pin the handler half of that contract: hand every UI request to the dev
// server, and keep the API surface exactly as it is in production.

const devFrontendURL = "http://localhost:5173"

// TestDevFrontendURLRedirectsEveryUIRoute asserts the path and query survive, so
// a deep link or a reload lands where the Vite bundle would have routed it.
func TestDevFrontendURLRedirectsEveryUIRoute(t *testing.T) {
	app := newApp(t, devFrontendURL)

	for _, path := range []string{"/", "/movies/123", "/settings?tab=privacy"} {
		resp := getResp(t, app, path)
		_ = resp.Body.Close()

		if resp.StatusCode != fiber.StatusFound {
			t.Errorf("%s: status = %d, want %d", path, resp.StatusCode, fiber.StatusFound)
		}
		if got, want := resp.Header.Get("Location"), devFrontendURL+path; got != want {
			t.Errorf("%s: Location = %q, want %q", path, got, want)
		}
		if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
		}
	}
}

// TestDevFrontendURLNeverServesTheBundle is the regression this mode exists for.
// web/dist is refreshed only by `make frontend`, so an API process serving it
// answers with the frontend as it was at that build — old code against a current
// API, and a build-time switch (the Clerk publishable key) that no longer
// matches the running server surfaces as a browser error rather than a stale
// build. Handing the request to the dev server instead makes that impossible.
func TestDevFrontendURLNeverServesTheBundle(t *testing.T) {
	status, body := get(t, newApp(t, devFrontendURL), "/")

	if status != fiber.StatusFound {
		t.Fatalf("status = %d, want %d", status, fiber.StatusFound)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "Frontend not built") {
		t.Fatalf("the development pairing served the embedded bundle: %q", body)
	}
}

// TestDevFrontendURLDoesNotRedirectNonGetRequests asserts the redirect is a
// navigation affordance, not a catch-all that hides a mistyped API call.
func TestDevFrontendURLDoesNotRedirectNonGetRequests(t *testing.T) {
	resp, err := newApp(t, devFrontendURL).Test(
		httptest.NewRequest(fiber.MethodPost, "/", nil))
	if err != nil {
		t.Fatalf("app.Test error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == fiber.StatusFound {
		t.Fatalf("POST / was redirected to %q, want no redirect", resp.Header.Get("Location"))
	}
}

// TestDevFrontendURLLeavesTheAPISurfaceAlone asserts an unknown API path is
// still the JSON envelope, never a redirect: a client that followed one would
// get HTML for a mistyped endpoint, which is the failure this package exists to
// prevent.
func TestDevFrontendURLLeavesTheAPISurfaceAlone(t *testing.T) {
	app := newApp(t, devFrontendURL)

	for _, path := range []string{"/v1/nope", "/oauth/nope", "/healthz/nope"} {
		status, body := get(t, app, path)

		if status != fiber.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, status)
		}
		if strings.Contains(body, "<html") {
			t.Errorf("%s: served the SPA shell to an API path: %q", path, body)
		}
	}

	status, body := get(t, app, "/v1/known")
	if status != fiber.StatusOK {
		t.Fatalf("/v1/known: status = %d, want 200", status)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("/v1/known: body = %q, want the JSON handler response", body)
	}
}
