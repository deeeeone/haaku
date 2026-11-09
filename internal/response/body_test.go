package response

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// call runs one request through a minimal Fiber app whose error handler is the
// production one, so the envelope under test is the one clients receive.
func call(t *testing.T, body string, dst any) (int, string) {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: HandleError})
	app.Post("/x", func(c *fiber.Ctx) error {
		if err := ParseBody(c, dst); err != nil {
			return err
		}
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, resp.ContentLength)
	if resp.ContentLength > 0 {
		_, _ = resp.Body.Read(buf)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(buf, &env)
	return resp.StatusCode, env.Error.Message
}

type shapeBody struct {
	IDs struct {
		TMDB FlexibleID `json:"tmdb"`
	} `json:"ids"`
	Title string `json:"title"`
}

func TestParseBodyMalformedKeepsSyntaxMessage(t *testing.T) {
	var dst shapeBody
	status, msg := call(t, `{"ids": [`, &dst)
	if status != 400 {
		t.Fatalf("status = %d, want 400", status)
	}
	if msg != "invalid JSON body" {
		t.Fatalf("message = %q, want the bare syntax message", msg)
	}
}

func TestParseBodyWrongShapeNamesTheField(t *testing.T) {
	var dst struct {
		Title int `json:"title"`
	}
	status, msg := call(t, `{"title": {}}`, &dst)
	if status != 400 {
		t.Fatalf("status = %d, want 400", status)
	}
	if msg == "invalid JSON body" {
		t.Fatalf("a shape failure must not report a syntax failure: %q", msg)
	}
	if !strings.Contains(msg, "title") {
		t.Fatalf("message %q does not name the offending field", msg)
	}
	if !strings.Contains(msg, "a number") {
		t.Fatalf("message %q does not say what was expected", msg)
	}
}

func TestFlexibleIDAcceptsNumberAndNumericString(t *testing.T) {
	for _, body := range []string{`{"ids":{"tmdb":603}}`, `{"ids":{"tmdb":"603"}}`} {
		var dst shapeBody
		status, msg := call(t, body, &dst)
		if status != 200 {
			t.Fatalf("body %s: status = %d (%s), want 200", body, status, msg)
		}
		if dst.IDs.TMDB.Int() != 603 {
			t.Fatalf("body %s: TMDB = %d, want 603", body, dst.IDs.TMDB.Int())
		}
	}
}

func TestFlexibleIDRejectsNonNumericString(t *testing.T) {
	var dst struct {
		IDs struct {
			TMDB FlexibleID `json:"tmdb"`
		} `json:"ids"`
	}
	status, msg := call(t, `{"ids":{"tmdb":"abc"}}`, &dst)
	if status != 400 {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(msg, "numeric string") {
		t.Fatalf("message %q does not say what form was expected", msg)
	}
}
