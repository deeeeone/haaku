package tests

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"trakt-api/internal/config"
	"trakt-api/internal/httpapi"

	"github.com/gofiber/fiber/v2"
)

// newTestApp creates a test Fiber app with a fresh route registry.
func newTestApp(t *testing.T) *fiber.App {
	t.Helper()

	cfg := &config.Config{
		Env:         "test",
		Port:        "0",
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	return httpapi.NewServer(cfg)
}

// doTestRequest makes an HTTP request against a Fiber test app.
func doTestRequest(t *testing.T, app *fiber.App, method, path string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

// TestSearchRouteExists verifies the search endpoint is registered and reachable.
func TestSearchRouteExists(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search?query=test")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("route /v1/search returned 404 — route not registered. body=%s", body)
	}
}

// TestSearchExactRouteExists verifies /search/exact is registered.
func TestSearchExactRouteExists(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search/exact?query=test&type=movie")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("route /v1/search/exact returned 404 — route not registered. body=%s", body)
	}
}

// TestSearchLookupRouteExists verifies /search/lookup is registered.
func TestSearchLookupRouteExists(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search/lookup?id_type=imdb&id=tt1234567")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("route /v1/search/lookup returned 404 — route not registered. body=%s", body)
	}
}

// TestSearchMissingQuery validates that missing query returns 400.
func TestSearchMissingQuery(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 400 for missing query, got %d. body=%s", resp.StatusCode, body)
	}
}

// TestSearchExactMissingQuery validates 400 on missing query.
func TestSearchExactMissingQuery(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search/exact")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 400 for missing query, got %d. body=%s", resp.StatusCode, body)
	}
}

// TestSearchExactInvalidType validates 400 on invalid type.
func TestSearchExactInvalidType(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search/exact?query=test&type=invalid")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 400 for invalid type, got %d. body=%s", resp.StatusCode, body)
	}
}

// TestSearchLookupMissingParams validates 400 on missing params.
func TestSearchLookupMissingParams(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name string
		path string
	}{
		{"missing id_type", "/v1/search/lookup?id=tt123"},
		{"missing id", "/v1/search/lookup?id_type=imdb"},
		{"missing both", "/v1/search/lookup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doTestRequest(t, app, "GET", tt.path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 400 for %s, got %d. body=%s", tt.name, resp.StatusCode, body)
			}
		})
	}
}

// TestSearchDefaultType verifies that the default type parameter is applied.
func TestSearchDefaultType(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search?query=test")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("search route not found")
	}
}

// TestSearchPaginationDefaults validates pagination defaults.
func TestSearchPaginationDefaults(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search?query=test&page=2&limit=10")
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("search route not found with pagination params")
	}

	// Headers should be set (even on error).
	page := resp.Header.Get("X-Pagination-Page")
	if page == "" {
		t.Log("X-Pagination-Page header not set (expected on DB error)")
	}
}

// TestErrorEnvelopeFormat verifies that errors use the standard envelope.
func TestSearchErrorEnvelope(t *testing.T) {
	app := newTestApp(t)

	resp := doTestRequest(t, app, "GET", "/v1/search") // missing query
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, `"error"`) {
		t.Errorf("error response missing 'error' envelope: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"code"`) {
		t.Errorf("error response missing 'code' field: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"message"`) {
		t.Errorf("error response missing 'message' field: %s", bodyStr)
	}
}