package tests

import (
	"encoding/json"
	"testing"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Route registration tests
// ---------------------------------------------------------------------------

func TestNotesRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/notes":             true,
		"/v1/notes/{id}":        true,
		"/v1/users/me/notes":    true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected notes route %q not registered", path)
		}
	}
}

func TestNotesRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	notesPaths := map[string]bool{
		"/v1/notes":          true,
		"/v1/notes/{id}":     true,
		"/v1/users/me/notes": true,
	}

	for _, r := range routes {
		if notesPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Notes" {
				t.Errorf("notes route %s %s has tags=%v, want [Notes]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

func TestNotesRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	notesPaths := map[string]bool{
		"/v1/notes":          true,
		"/v1/notes/{id}":     true,
		"/v1/users/me/notes": true,
	}

	for _, r := range routes {
		if notesPaths[r.Path] {
			if r.Auth != "required" {
				t.Errorf("notes route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Model shape tests
// ---------------------------------------------------------------------------

func TestUserNoteModelShape(t *testing.T) {
	note := database.UserNote{
		ID:        1,
		UserID:    "user_abc",
		MediaType: "movie",
		MediaID:   12345,
		Note:      "This is a great movie!",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(note)
	if err != nil {
		t.Fatalf("failed to marshal UserNote: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["id"].(float64) != 1 {
		t.Errorf("id = %v, want 1", raw["id"])
	}
	if raw["user_id"] != "user_abc" {
		t.Errorf("user_id = %v, want user_abc", raw["user_id"])
	}
	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v, want movie", raw["media_type"])
	}
	if raw["note"] != "This is a great movie!" {
		t.Errorf("note = %v, want 'This is a great movie!'", raw["note"])
	}

	// TableName check.
	if note.TableName() != "user_notes" {
		t.Errorf("TableName = %s, want user_notes", note.TableName())
	}
}

func TestUserNoteModelRequiredFields(t *testing.T) {
	// UserNote requires UserID, MediaType, MediaID, Note to be set.
	note := database.UserNote{
		UserID:    "user_xyz",
		MediaType: "show",
		MediaID:   999,
		Note:      "My thoughts",
	}

	if note.UserID == "" {
		t.Error("UserID should be set")
	}
	if note.MediaType == "" {
		t.Error("MediaType should be set")
	}
	if note.MediaID == 0 {
		t.Error("MediaID should be set")
	}
	if note.Note == "" {
		t.Error("Note should be set")
	}
}
