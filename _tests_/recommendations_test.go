package tests

import (
	"testing"

	_ "trakt-api/internal/handlers/recommendations"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Recommendations route registration tests (T9a.2 + T9a.3)
// ---------------------------------------------------------------------------

func TestRecommendationsRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/recommendations/movies"},
		{"GET", "/v1/recommendations/shows"},
		{"GET", "/v1/social_recommendations/movies"},
		{"GET", "/v1/social_recommendations/shows"},
		{"GET", "/v1/media/anticipated"},
	}

	found := make(map[string]map[string]bool)
	for _, r := range routes {
		if found[r.Path] == nil {
			found[r.Path] = make(map[string]bool)
		}
		found[r.Path][r.Method] = true
	}

	for _, er := range expectedRoutes {
		if !found[er.path][er.method] {
			t.Errorf("expected route %s %s not registered", er.method, er.path)
		}
	}
}

func TestRecommendationsRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	recommendationPaths := map[string]bool{
		"/v1/recommendations/movies":        true,
		"/v1/recommendations/shows":         true,
		"/v1/social_recommendations/movies": true,
		"/v1/social_recommendations/shows":  true,
	}

	anticipatedPath := "/v1/media/anticipated"

	for _, r := range routes {
		if recommendationPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Recommendations" {
				t.Errorf("route %s %s has tags=%v, want [Recommendations]",
					r.Method, r.Path, r.Tags)
			}
		}
		if r.Path == anticipatedPath {
			if len(r.Tags) == 0 || r.Tags[0] != "Media" {
				t.Errorf("route %s %s has tags=%v, want [Media]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

func TestRecommendationsRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	recPaths := []string{
		"/v1/recommendations/movies",
		"/v1/recommendations/shows",
		"/v1/social_recommendations/movies",
		"/v1/social_recommendations/shows",
		"/v1/media/anticipated",
	}

	for _, r := range routes {
		for _, p := range recPaths {
			if r.Path == p && r.Auth != "required" {
				t.Errorf("route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

func TestRecommendationsPackageExists(t *testing.T) {
	// Verify the recommendations package compiles (imported above).
	_ = "ok"
}
