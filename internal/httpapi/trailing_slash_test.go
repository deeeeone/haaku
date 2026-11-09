package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestTrailingSlashIsTheSameRoute pins the StrictRouting answer for the parity
// contract: Fiber's default (StrictRouting off) treats /foo and /foo/ as one
// route, so contract paths that end with a slash are served by our slash-less
// registrations rather than needing a duplicate route (which would collide).
//
// The proof is a real request: the contract-shaped paths reach a handler and
// are answered (401 from the auth middleware, or 500 with no DB) instead of
// 404, while a genuinely unregistered path still 404s.
func TestTrailingSlashIsTheSameRoute(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	app := fiber.New()
	RegisterRoutes(app)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"POST /comments/ (contract)", "POST", "/v1/comments/"},
		{"POST /comments (ours)", "POST", "/v1/comments"},
		{"PUT /comments/{id}/ (contract)", "PUT", "/v1/comments/1/"},
		{"DELETE /comments/{id}/ (contract)", "DELETE", "/v1/comments/1/"},
		{"POST /comments/{id}/reactions/{type} (contract)", "POST", "/v1/comments/1/reactions/like"},
		{"DELETE /comments/{id}/reactions/{type}/ (contract)", "DELETE", "/v1/comments/1/reactions/like/"},
		{"GET /comments/{id}/reactions/ (contract)", "GET", "/v1/comments/1/reactions/"},
		{"GET /comments/recent/{type}/{type}/ (contract)", "GET", "/v1/comments/recent/shouts/movies/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.path, strings.NewReader(""))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Host = "example.com"
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf("%s %s answered 404 — the trailing-slash contract path did not match a route", tc.method, tc.path)
			}
		})
	}

	// Control: an unregistered path must still be a 404, so "not 404" above
	// means "matched", not "everything matches".
	req, _ := http.NewRequest("POST", "/v1/comments-nope/", strings.NewReader(""))
	req.Host = "example.com"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("control POST /v1/comments-nope/ = %d, want 404", resp.StatusCode)
	}
}
