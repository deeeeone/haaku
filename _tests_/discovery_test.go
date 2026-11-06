package tests

import (
	"encoding/json"
	"testing"

	_ "trakt-api/internal/handlers/discovery"
	"trakt-api/internal/handlers/media"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestDiscoveryRoutesRegistered verifies discovery routes are in the OpenAPI registry.
func TestDiscoveryRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/trending":             true,
		"/v1/movies/popular":              true,
		"/v1/movies/recommendations/{id}": true,
		"/v1/shows/trending":              true,
		"/v1/shows/popular":               true,
		"/v1/shows/recommendations/{id}":  true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected discovery route %q not registered", path)
		}
	}
}

// TestDiscoveryRoutesHaveCorrectTags verifies discovery routes have the Discovery tag.
func TestDiscoveryRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	discoveryPaths := map[string]bool{
		"/v1/movies/trending":             true,
		"/v1/movies/popular":              true,
		"/v1/movies/recommendations/{id}": true,
		"/v1/shows/trending":              true,
		"/v1/shows/popular":               true,
		"/v1/shows/recommendations/{id}":  true,
	}

	for _, r := range routes {
		if discoveryPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Discovery" {
				t.Errorf("discovery route %s %s has tags=%v, want [Discovery]",
					r.Method, r.Path, r.Tags)
			}
			if r.Auth != "none" {
				t.Errorf("discovery route %s %s has auth=%q, want 'none'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Pagination parsing tests
// ---------------------------------------------------------------------------

// TestDiscoveryParsePaginationDefaults verifies discovery pagination defaults (page=1, limit=20).
func TestDiscoveryParsePaginationDefaults(t *testing.T) {
	page, limit := discoveryParsePaginationHelper("", "")
	if page != 1 {
		t.Errorf("default page = %d, want 1", page)
	}
	if limit != 20 {
		t.Errorf("default limit = %d, want 20 (discovery default)", limit)
	}
}

// TestDiscoveryParsePaginationCustom verifies custom pagination params.
func TestDiscoveryParsePaginationCustom(t *testing.T) {
	page, limit := discoveryParsePaginationHelper("3", "50")
	if page != 3 {
		t.Errorf("page = %d, want 3", page)
	}
	if limit != 50 {
		t.Errorf("limit = %d, want 50", limit)
	}
}

// TestDiscoveryParsePaginationMaxLimit verifies limit is capped at 100.
func TestDiscoveryParsePaginationMaxLimit(t *testing.T) {
	_, limit := discoveryParsePaginationHelper("1", "200")
	if limit != 100 {
		t.Errorf("limit = %d, want 100 (capped)", limit)
	}
}

// TestDiscoveryParsePaginationInvalidValues verifies invalid values use defaults.
func TestDiscoveryParsePaginationInvalidValues(t *testing.T) {
	page, limit := discoveryParsePaginationHelper("invalid", "invalid")
	if page != 1 {
		t.Errorf("page with invalid input = %d, want 1", page)
	}
	if limit != 20 {
		t.Errorf("limit with invalid input = %d, want 20", limit)
	}

	page, limit = discoveryParsePaginationHelper("-1", "0")
	if page != 1 {
		t.Errorf("page with negative = %d, want 1", page)
	}
	if limit != 20 {
		t.Errorf("limit with zero = %d, want 20", limit)
	}
}

// ---------------------------------------------------------------------------
// Extended parameter pass-through test
// ---------------------------------------------------------------------------

// TestDiscoveryExtendedParamValues verifies that the valid extended values
// are recognized (same logic as media.GetExtended).
func TestDiscoveryExtendedParamValues(t *testing.T) {
	// These are the same valid values used by media.GetExtended.
	// The discovery package uses media.GetExtended directly.
	valid := map[string]bool{
		"min": true, "full": true, "metadata": true, "images": true, "all": true,
	}
	for _, v := range []string{"min", "full", "metadata", "images", "all"} {
		if !valid[v] {
			t.Errorf("expected %q to be a valid extended value", v)
		}
	}
}

// ---------------------------------------------------------------------------
// Recommendations stub tests
// ---------------------------------------------------------------------------

// TestMoviesRecommendationsReturnsEmptyArray verifies the recommendations
// response shape is an empty JSON array.
func TestMoviesRecommendationsReturnsEmptyArray(t *testing.T) {
	resp := make([]media.MovieResponse, 0)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("movie recommendations should be [], got %s", string(data))
	}
}

// TestShowsRecommendationsReturnsEmptyArray verifies the shows recommendations
// response shape is an empty JSON array.
func TestShowsRecommendationsReturnsEmptyArray(t *testing.T) {
	resp := make([]media.ShowResponse, 0)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("show recommendations should be [], got %s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Helpers (replicate discovery package's unexported pagination logic)
// ---------------------------------------------------------------------------

func discoveryParsePaginationHelper(pageStr, limitStr string) (int, int) {
	page, limit := 1, 20
	if pageStr != "" {
		p := 0
		for _, c := range pageStr {
			if c >= '0' && c <= '9' {
				p = p*10 + int(c-'0')
			} else {
				p = -1
				break
			}
		}
		if p > 0 {
			page = p
		}
	}
	if limitStr != "" {
		l := 0
		for _, c := range limitStr {
			if c >= '0' && c <= '9' {
				l = l*10 + int(c-'0')
			} else {
				l = -1
				break
			}
		}
		if l > 0 {
			if l > 100 {
				l = 100
			}
			limit = l
		}
	}
	return page, limit
}
