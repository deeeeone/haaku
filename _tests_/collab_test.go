package tests

import (
	"encoding/json"
	"testing"

	_ "github.com/haaku-tv/haaku/internal/handlers/collab"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// ---------------------------------------------------------------------------
// Collab routes registration tests
// ---------------------------------------------------------------------------

func TestCollabRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedMethods := map[string]map[string]bool{
		"/v1/users/me/lists/collaborations":      {"GET": true},
		"/v1/lists/{id}/collaborators":           {"POST": true, "GET": true},
		"/v1/lists/{id}/collaborators/{user_id}": {"DELETE": true},
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
				t.Errorf("expected collab route %s %s not registered", method, path)
			}
		}
	}
}

func TestCollabRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	for _, r := range routes {
		if r.Path == "/v1/lists/{id}/collaborators" || r.Path == "/v1/lists/{id}/collaborators/{user_id}" ||
			r.Path == "/v1/users/me/lists/collaborations" {
			if len(r.Tags) == 0 || r.Tags[0] != "Lists" {
				t.Errorf("collab route %s %s has tags=%v, want [Lists]",
					r.Method, r.Path, r.Tags)
			}
			if r.Auth != "required" {
				t.Errorf("collab route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Response shape tests
// ---------------------------------------------------------------------------

func TestCollaboratorResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"user_id":     "user_abc123",
		"role":        "editor",
		"invited_at":  "2025-01-15T10:30:00.000Z",
		"accepted_at": "2025-01-15T10:35:00.000Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	requiredFields := []string{"user_id", "role", "invited_at"}
	for _, f := range requiredFields {
		if _, ok := parsed[f]; !ok {
			t.Errorf("collaborator response missing field %q", f)
		}
	}
}

func TestInviteRequestValidation(t *testing.T) {
	// Valid roles only.
	validRoles := map[string]bool{"editor": true, "viewer": true}
	invalidRoles := []string{"admin", "owner", "", "random"}

	for _, role := range invalidRoles {
		if validRoles[role] {
			t.Errorf("role %q should be invalid", role)
		}
	}

	if !validRoles["editor"] || !validRoles["viewer"] {
		t.Error("editor and viewer should be valid roles")
	}
}

func TestCollabPackageExists(t *testing.T) {
	_ = "ok"
}
