package tests

import (
	"testing"

	_ "trakt-api/internal/handlers/hidden"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Hidden items handler route registration tests
// ---------------------------------------------------------------------------

func TestHiddenRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedMethods := map[string]map[string]bool{
		"/v1/users/hidden/{section}": {
			"GET":    true,
			"POST":   true,
			"DELETE": true,
		},
	}

	found := make(map[string]map[string]bool)
	for _, r := range routes {
		if found[r.Path] == nil {
			found[r.Path] = make(map[string]bool)
		}
		found[r.Path][r.Method] = true
	}

	for path, methods := range expectedMethods {
		for method := range methods {
			if !found[path][method] {
				t.Errorf("expected hidden route %s %s not registered", method, path)
			}
		}
	}
}

func TestHiddenRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	for _, r := range routes {
		if r.Path == "/v1/users/hidden/{section}" {
			if len(r.Tags) == 0 || r.Tags[0] != "Users" {
				t.Errorf("hidden route %s %s has tags=%v, want [Users]",
					r.Method, r.Path, r.Tags)
			}
			if r.Auth != "required" {
				t.Errorf("hidden route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

func TestHiddenPackageExists(t *testing.T) {
	// Verify the hidden package compiles (imported above).
	_ = "ok"
}
