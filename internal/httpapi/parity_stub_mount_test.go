package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestParityStubIsMountedUnderV1 pins the mount contract for the generated
// stubs: their paths already carry /v1, so they must be registered on the app
// root. Registered on the /v1 group instead they lived at /v1/v1/... and every
// stub was unreachable — a client got a 404 (or a parameterised handler's 400)
// where the contract says an honest 501.
func TestParityStubIsMountedUnderV1(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "user_test")
		return c.Next()
	})
	RegisterRoutes(app)

	cases := []struct {
		path string
		want int
	}{
		{"/v1/smart-lists/templates", http.StatusNotImplemented},
		{"/v1/shows/streaming/weekly", http.StatusNotImplemented},
		{"/v1/v1/smart-lists/templates", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Host = "example.com"
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: request failed: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s = %d, want %d (%s)", tc.path, resp.StatusCode, tc.want, body)
		}
	}
}
