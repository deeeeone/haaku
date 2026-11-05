package httpapi

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// segmentMatches reports whether a registered path segment (which may be a
// parameter such as ":id") consumes a concrete path segment.
func segmentMatches(pattern, concrete string) bool {
	return pattern == concrete || strings.HasPrefix(pattern, ":")
}

// pathShadows reports whether the pattern route would capture a request intended
// for the concrete route: same number of segments, every pattern segment either
// literal-equal or a parameter. Both paths are expected to be rooted, e.g.
// "/v1/movies/:id" and "/v1/movies/trending".
func pathShadows(pattern, concrete string) bool {
	if pattern == concrete {
		return false // the same route shadowing "itself" is not shadowing
	}
	p := strings.Split(strings.Trim(pattern, "/"), "/")
	c := strings.Split(strings.Trim(concrete, "/"), "/")
	if len(p) != len(c) {
		return false
	}
	for i := range p {
		if strings.Contains(p[i], "*") {
			return false // wildcards are handled by their own route; keep this precise
		}
		if !segmentMatches(p[i], c[i]) {
			return false
		}
	}
	return true
}

// TestNoStaticRouteIsShadowed is the guard for a real defect class: Fiber matches
// routes in registration order, so a parameterised route registered before a
// static route at the same depth silently swallows it. Six endpoints
// (/v1/movies|shows/{trending,popular} and /v1/lists/{trending,popular}) shipped
// unreachable because media.RegisterRoutes ran before discovery.RegisterRoutes
// and /lists/:id ran before /lists/popular. The handlers were fine; only the
// wiring was wrong, and nothing — not the handler tests, not the OpenAPI
// registry/spec checks — looks at request routing.
//
// The rule this enforces: a static path must never be preceded by an
// earlier-registered parameterised path that would match it. Fix ordering rather
// than adding an exception here.
func TestNoStaticRouteIsShadowed(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app)

	type route struct {
		method string
		path   string
	}
	var routes []route
	for _, r := range app.GetRoutes() {
		if r.Path == "" || strings.Contains(r.Path, "*") {
			continue
		}
		routes = append(routes, route{method: r.Method, path: r.Path})
	}
	if len(routes) == 0 {
		t.Fatal("no routes registered — the test would pass vacuously")
	}

	for i, later := range routes {
		if strings.Contains(later.path, ":") {
			continue // only static routes can be shadowed
		}
		for _, earlier := range routes[:i] {
			if earlier.method != later.method {
				continue
			}
			if pathShadows(earlier.path, later.path) {
				t.Errorf("%s %s is unreachable: it is shadowed by %s %s, registered earlier",
					later.method, later.path, earlier.method, earlier.path)
			}
		}
	}
}

// TestPathShadows pins the matcher itself, so the guard above cannot pass by
// being unable to detect anything.
func TestPathShadows(t *testing.T) {
	cases := []struct {
		pattern, concrete string
		want              bool
	}{
		{"/v1/movies/:id", "/v1/movies/trending", true},
		{"/v1/movies/trending", "/v1/movies/trending", false},
		{"/v1/movies/:id", "/v1/movies/:id", false},
		{"/v1/movies/updates/:d", "/v1/movies/updates/2026-01-01", true},
		{"/v1/movies/:id", "/v1/movies/updates/2026-01-01", false}, // different depth
		{"/v1/movies/trending", "/v1/movies/popular", false},
	}
	for _, tc := range cases {
		if got := pathShadows(tc.pattern, tc.concrete); got != tc.want {
			t.Errorf("pathShadows(%q, %q) = %v, want %v", tc.pattern, tc.concrete, got, tc.want)
		}
	}
}
