package webui

import (
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/web"
)

// newApp mounts the UI handler exactly as the server does: after the API routes,
// with a route that shadows nothing.
func newApp(t *testing.T) *fiber.App {
	t.Helper()

	app := fiber.New()
	app.Get("/v1/known", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })

	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	app.Use(h)
	return app
}

func get(t *testing.T, app *fiber.App, path string) (int, string) {
	t.Helper()

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, path, nil))
	if err != nil {
		t.Fatalf("app.Test(%q) error = %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	return resp.StatusCode, string(body[:n])
}

// TestServesIndexForRoot asserts the SPA shell is served at "/".
func TestServesIndexForRoot(t *testing.T) {
	status, body := get(t, newApp(t), "/")

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
	status, body := get(t, newApp(t), "/movies/123")

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
		status, body := get(t, newApp(t), path)

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
	status, body := get(t, newApp(t), "/v1/known")

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
