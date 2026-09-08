package response

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// newPaginationApp registers a route that echoes ParsePagination/QueryInt
// results as JSON.
func newPaginationApp() *fiber.App {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		page, limit := ParsePagination(c, 25)
		return c.JSON(fiber.Map{"page": page, "limit": limit, "days": QueryInt(c, "days", 30)})
	})
	return app
}

func parseJSON(t *testing.T, app *fiber.App, query string) map[string]int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/?"+query, nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out map[string]int
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %q: %v", string(body), err)
	}
	return out
}

func TestParsePaginationDefaults(t *testing.T) {
	out := parseJSON(t, newPaginationApp(), "")
	if out["page"] != 1 || out["limit"] != 25 {
		t.Errorf("defaults = %v, want page 1, limit 25", out)
	}
}

func TestParsePaginationClampsLimit(t *testing.T) {
	out := parseJSON(t, newPaginationApp(), "page=3&limit=1000")
	if out["page"] != 3 || out["limit"] != 100 {
		t.Errorf("got %v, want page 3, limit 100", out)
	}
}

func TestParsePaginationIgnoresInvalid(t *testing.T) {
	out := parseJSON(t, newPaginationApp(), "page=-2&limit=abc")
	if out["page"] != 1 || out["limit"] != 25 {
		t.Errorf("got %v, want page 1, limit 25", out)
	}
}

func TestParsePaginationZeroLimitUsesDefault(t *testing.T) {
	out := parseJSON(t, newPaginationApp(), "page=2&limit=0")
	if out["page"] != 2 || out["limit"] != 25 {
		t.Errorf("got %v, want page 2, limit 25", out)
	}
}

func TestQueryInt(t *testing.T) {
	if out := parseJSON(t, newPaginationApp(), "days=7"); out["days"] != 7 {
		t.Errorf("days = %d, want 7", out["days"])
	}
	if out := parseJSON(t, newPaginationApp(), "days=abc"); out["days"] != 30 {
		t.Errorf("invalid days = %d, want 30", out["days"])
	}
	if out := parseJSON(t, newPaginationApp(), ""); out["days"] != 30 {
		t.Errorf("missing days = %d, want 30", out["days"])
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		v, min, max, want int
	}{
		{5, 1, 100, 5},
		{0, 1, 100, 1},
		{1000, 1, 100, 100},
	}
	for _, c := range cases {
		if got := ClampLimit(c.v, c.min, c.max); got != c.want {
			t.Errorf("ClampLimit(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestSetPaginationHeaders(t *testing.T) {
	app := fiber.New()
	app.Get("/list", func(c *fiber.Ctx) error {
		SetPaginationHeaders(c, 2, 25, 60, 25) // 60 items, 25/page -> 3 pages
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/list", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	h := func(k string) string { return resp.Header.Get(k) }
	cases := map[string]string{
		"X-Pagination-Page":        "2",
		"X-Pagination-Limit":       "25",
		"X-Pagination-Total-Pages": "3",
		"X-Pagination-Total-Items": "60",
		"X-Pagination-Items-Count": "25",
		"Link":                     `</list?page=3&limit=25>; rel="next"`,
	}
	for k, want := range cases {
		if got := h(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestSetPaginationHeadersNoNextOnLastPage(t *testing.T) {
	app := fiber.New()
	app.Get("/list", func(c *fiber.Ctx) error {
		SetPaginationHeaders(c, 3, 25, 60, 10) // page 3 of 3 -> no Link
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/list", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Link"); got != "" {
		t.Errorf("Link = %q, want empty on last page", got)
	}
	if got := resp.Header.Get("X-Pagination-Total-Pages"); got != "3" {
		t.Errorf("Total-Pages = %q, want 3", got)
	}
}

func TestSetPaginationHeadersEmpty(t *testing.T) {
	app := fiber.New()
	app.Get("/list", func(c *fiber.Ctx) error {
		SetPaginationHeaders(c, 1, 25, 0, 0) // empty result set
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/list", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Pagination-Total-Pages"); got != "0" {
		t.Errorf("Total-Pages = %q, want 0 for empty set", got)
	}
	if got := resp.Header.Get("Link"); got != "" {
		t.Errorf("Link = %q, want empty for empty set", got)
	}
}
