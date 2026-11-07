package response

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestParseIDParamNumeric(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: HandleError})
	app.Get("/v1/things/:id", func(c *fiber.Ctx) error {
		id, err := ParseIDParam(c, "thing")
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": id})
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/things/42", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != 42 {
		t.Errorf("id = %d, want 42", body.ID)
	}
}

// TestParseIDParamMalformed pins the #40 contract: the helper writes the 400
// envelope AND returns a non-nil error, so `if err != nil { return err }` stops
// the handler instead of letting it continue with id 0 and answer 404.
func TestParseIDParamMalformed(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: HandleError})
	app.Get("/v1/episodes/:id", func(c *fiber.Ctx) error {
		id, err := ParseIDParam(c, "episode")
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": id})
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/episodes/abc", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (a 200 here means the handler ran on with id 0)", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", body.Error.Code)
	}
	if body.Error.Message != "invalid episode id" {
		t.Errorf("message = %q, want %q", body.Error.Message, "invalid episode id")
	}
}

// TestHandleErrorLeavesAWrittenResponseAlone proves the sentinel is a no-op, so
// the status a helper already wrote is the status the client sees.
func TestHandleErrorLeavesAWrittenResponseAlone(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: HandleError})
	app.Get("/v1/x/:id", func(c *fiber.Ctx) error {
		_ = NotFound(c, "nope")
		return ErrHandled
	})
	resp, err := app.Test(httptest.NewRequest("GET", "/v1/x/1", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want the 404 the handler already wrote", resp.StatusCode)
	}
}
