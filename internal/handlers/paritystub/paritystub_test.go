package paritystub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/parity"
)

// newStubApp registers every parity stub on a bare Fiber app, which is what the
// routes do in production — only the ordering in httpapi.RegisterRoutes differs.
func itoa(i int) string { return strconv.Itoa(i) }

func newStubApp() *fiber.App {
	app := fiber.New()
	RegisterRoutes(app)
	return app
}

// TestStubsAnswer501NamingTheTracker pins the contract a client sees: an
// unimplemented Trakt endpoint answers 501 (never 404, never a fabricated 200)
// and the message names the issue that tracks it.
func TestStubsAnswer501NamingTheTracker(t *testing.T) {
	app := newStubApp()
	for _, path := range []string{
		"/v1/media/trending",                       // #66
		"/v1/movies/watched/30d",                   // #66 — static segment over /movies/:id
		"/v1/shows/1/related",                      // #49
		"/v1/shows/2/videos",                       // #68
		"/v1/calendars/all/dvd/2026-11-01/7",       // #69, {target} template
		"/v1/calendars/all/streaming/2026-11-01/7", // #69
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatalf("%s: request failed: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s = %d, want %d (a stub must never answer 404 or a fake 200)", path, resp.StatusCode, http.StatusNotImplemented)
			continue
		}
		got := string(body)
		if !strings.Contains(got, `"code":"not_implemented"`) {
			t.Errorf("%s body = %s, want the not_implemented envelope", path, got)
		}
		if !strings.Contains(got, "https://github.com/haaku-tv/haaku/issues/") {
			t.Errorf("%s body = %s, want a link to the tracking issue", path, got)
		}
	}
}

// TestEveryBlockedRowNamesARoute pins the invariant this package exists for, at
// the level a leaf package can actually observe: no row graded blocked-* is left
// with an empty `ours` column, i.e. no endpoint we call "pending" is also
// unrouted. (The registry side — a 501 route must be recorded, and a blocked row
// must have a 501 — is checked crate-wide by internal/parity, which imports every
// handler package; a test in this package only sees its own defs.)
func TestEveryBlockedRowNamesARoute(t *testing.T) {
	manifest, err := parity.Load("../../../docs/parity/parity.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	var missing []string
	blocked := 0
	for _, s := range manifest.Sections {
		for _, r := range s.Rows {
			if r.Status != parity.StatusBlockedEngine && r.Status != parity.StatusBlockedData {
				continue
			}
			blocked++
			if strings.TrimSpace(r.Ours) == "" {
				missing = append(missing, r.Trakt+" (line "+itoa(r.Line)+")")
			}
		}
	}
	if blocked == 0 {
		t.Fatal("manifest has no blocked rows — the fixture changed, this test is no longer evidence")
	}
	if len(missing) > 0 {
		t.Errorf("%d of %d blocked row(s) name no route, so they answer 404:\n  %s",
			len(missing), blocked, strings.Join(missing, "\n  "))
	}
}
