package checkin

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
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
		{"/map/notashow", http.StatusBadRequest},  // wrong media type → 400
		{"/map/nomedia", http.StatusBadRequest},   // no item specified → 400
		{"/map/season", http.StatusNotFound},      // missing season → 404
		{"/map/episode", http.StatusNotFound},     // missing episode → 404
		{"/map/db", http.StatusServiceUnavailable}, // DB unavailable → 503
		{"/map/generic", http.StatusInternalServerError}, // anything else → 500
	}
	for _, tc := range cases {
		resp, err := app.Test(getReq(tc.path))
		if err != nil {
			t.Fatalf("%s failed: %v", tc.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s status = %d, want %d", tc.path, resp.StatusCode, tc.want)
		}
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
	resp.Body.Close()
	if string(buf[:n]) != "user-abc" {
		t.Errorf("getUserID = %q, want user-abc", string(buf[:n]))
	}

	// Missing locals → empty string (not a panic).
	resp2, err := app.Test(getReq("/whoami-empty"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	n2, _ := resp2.Body.Read(buf)
	resp2.Body.Close()
	if string(buf[:n2]) != "" {
		t.Errorf("getUserID without locals = %q, want empty", string(buf[:n2]))
	}
}
