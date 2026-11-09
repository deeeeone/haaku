package checkin

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/response"
)

// newCheckinApp registers a route that maps a sentinel error and a route that
// echoes getUserID, so the ctx-writing paths run through a real Fiber context.
func newCheckinApp() *fiber.App {
	app := fiber.New()
	app.Get("/map/:err", func(c *fiber.Ctx) error {
		switch c.Params("err") {
		case "notashow":
			return mapResolveError(c, errNotAShow)
		case "season":
			return mapResolveError(c, errSeasonNotFound)
		case "episode":
			return mapResolveError(c, errEpisodeNotFound)
		case "db":
			return mapResolveError(c, errDBUnavailable)
		case "nomedia":
			return mapResolveError(c, errNoMediaSpecified)
		case "generic":
			return mapResolveError(c, errors.New("boom"))
		}
		return mapResolveError(c, errNotAMovie)
	})
	app.Get("/whoami", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user-abc")
		return c.SendString(getUserID(c))
	})
	app.Get("/whoami-empty", func(c *fiber.Ctx) error {
		return c.SendString(getUserID(c))
	})
	// A route that reproduces what media.ResolveID does before it returns
	// response.ErrHandled: write the envelope, then hand the error back.
	// Registered at its own path, not under /map/:err — a static path declared
	// after a same-depth :param is shadowed by it (Fiber matches in order).
	app.Get("/already-written", func(c *fiber.Ctx) error {
		_ = response.NotFound(c, "media not found")
		return mapResolveError(c, response.ErrHandled)
	})
	return app
}

func getReq(target string) *http.Request {
	req, _ := http.NewRequest("GET", target, nil)
	req.Host = "example.com"
	return req
}

func TestMapResolveErrorStatusCodes(t *testing.T) {
	app := newCheckinApp()
	cases := []struct {
		path string
		want int
	}{
		{"/map/notashow", http.StatusBadRequest},         // wrong media type → 400
		{"/map/nomedia", http.StatusBadRequest},          // no item specified → 400
		{"/map/season", http.StatusNotFound},             // missing season → 404
		{"/map/episode", http.StatusNotFound},            // missing episode → 404
		{"/map/db", http.StatusServiceUnavailable},       // DB unavailable → 503
		{"/map/generic", http.StatusInternalServerError}, // anything else → 500
	}
	for _, tc := range cases {
		resp, err := app.Test(getReq(tc.path))
		if err != nil {
			t.Fatalf("%s failed: %v", tc.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s status = %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}

// TestMapResolveErrorPreservesAWrittenEnvelope pins the contract media.ResolveID
// relies on: it writes its own 404 (or 400) and returns response.ErrHandled, and
// the check-in mapper must let that status stand. Before this case existed,
// mapResolveError fell through to the generic-500 branch and answered 500 for a
// media id that simply was not in the catalogue.
func TestMapResolveErrorPreservesAWrittenEnvelope(t *testing.T) {
	app := newCheckinApp()
	resp, err := app.Test(getReq("/already-written"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (the resolver's own envelope must survive)", resp.StatusCode, http.StatusNotFound)
	}
	if !strings.Contains(string(body), `"not_found"`) {
		t.Errorf("body = %s, want the not_found envelope the resolver wrote", body)
	}
}

func TestErrAlreadyCheckedInSentinel(t *testing.T) {
	if !errors.Is(ErrAlreadyCheckedIn, ErrAlreadyCheckedIn) {
		t.Fatal("ErrAlreadyCheckedIn must be errors.Is-comparable with itself")
	}
	if ErrAlreadyCheckedIn.Error() != "already checked in" {
		t.Errorf("sentinel message = %q", ErrAlreadyCheckedIn.Error())
	}
}

func TestDefaultCheckinDuration(t *testing.T) {
	if d := defaultCheckinDuration(); d != 2*time.Hour {
		t.Errorf("defaultCheckinDuration = %v, want 2h", d)
	}
}

func TestGetUserID(t *testing.T) {
	app := newCheckinApp()
	resp, err := app.Test(getReq("/whoami"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	_ = resp.Body.Close()
	if string(buf[:n]) != "user-abc" {
		t.Errorf("getUserID = %q, want user-abc", string(buf[:n]))
	}

	// Missing locals → empty string (not a panic).
	resp2, err := app.Test(getReq("/whoami-empty"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	n2, _ := resp2.Body.Read(buf)
	_ = resp2.Body.Close()
	if string(buf[:n2]) != "" {
		t.Errorf("getUserID without locals = %q, want empty", string(buf[:n2]))
	}
}
