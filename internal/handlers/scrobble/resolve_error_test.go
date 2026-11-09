package scrobble

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/response"
)

// newResolveErrorApp wires writeResolveError behind two routes: one where the
// resolver has already written its envelope (so the helper must stay silent) and
// one where it has not (so a normal resolve failure still answers 400).
func newResolveErrorApp() *fiber.App {
	app := fiber.New()
	app.Get("/already-written", func(c *fiber.Ctx) error {
		_ = response.NotFound(c, "media not found")
		return writeResolveError(c, response.ErrHandled)
	})
	app.Get("/plain", func(c *fiber.Ctx) error {
		return writeResolveError(c, errors.New("id does not resolve to a show"))
	})
	return app
}

// TestWriteResolveErrorPreservesAWrittenEnvelope: media.ResolveID writes its own
// 404 and returns ErrHandled; the scrobble handlers must not overwrite that with
// their generic 400 (whose message used to be ErrHandled's own text, "response
// already written").
func TestWriteResolveErrorPreservesAWrittenEnvelope(t *testing.T) {
	app := newResolveErrorApp()

	req := httptest.NewRequest(http.MethodGet, "/already-written", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (the resolver's envelope must survive)", resp.StatusCode, http.StatusNotFound)
	}
	if !strings.Contains(string(body), `"not_found"`) {
		t.Errorf("body = %s, want the not_found envelope the resolver wrote", body)
	}
	if strings.Contains(string(body), "response already written") {
		t.Errorf("body leaks ErrHandled's text: %s", body)
	}
}

// TestWriteResolveErrorStillAnswers400: the helper must keep answering 400 for a
// resolve failure that wrote nothing.
func TestWriteResolveErrorStillAnswers400(t *testing.T) {
	app := newResolveErrorApp()

	req := httptest.NewRequest(http.MethodGet, "/plain", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), "id does not resolve to a show") {
		t.Errorf("body = %s, want the resolve error's message", body)
	}
}
