package tests

import (
	"testing"

	_ "trakt-api/internal/handlers/stats"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Stats handler route registration tests
// ---------------------------------------------------------------------------

func TestStatsRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/{id}/stats": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected stats route %q not registered", path)
		}
	}
}

func TestStatsRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	for _, r := range routes {
		if r.Path == "/v1/users/{id}/stats" {
			if len(r.Tags) == 0 || r.Tags[0] != "Users" {
				t.Errorf("stats route has tags=%v, want [Users]", r.Tags)
			}
			if r.Auth != "optional" {
				t.Errorf("stats route auth=%q, want 'optional'", r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Stats computation unit tests (no DB)
// ---------------------------------------------------------------------------

func TestStatsPackageExists(t *testing.T) {
	// Verify the stats package compiles (imported above).
	_ = "ok"
}