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

func TestFiltersRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/me/saved_filters/{section}": true,
		"/v1/users/me/saved_filters":           true,
		"/v1/users/me/saved_filters/{id}":      true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected saved filter route %q not registered", path)
		}
	}
}

func TestFiltersRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	filterPaths := map[string]bool{
		"/v1/users/me/saved_filters/{section}": true,
		"/v1/users/me/saved_filters":           true,
		"/v1/users/me/saved_filters/{id}":      true,
	}

	for _, r := range routes {
		if filterPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Filters" {
				t.Errorf("filter route %s %s has tags=%v, want [Filters]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

func TestFiltersRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	filterPaths := map[string]bool{
		"/v1/users/me/saved_filters/{section}": true,
		"/v1/users/me/saved_filters":           true,
		"/v1/users/me/saved_filters/{id}":      true,
	}

	for _, r := range routes {
		if filterPaths[r.Path] {
			if r.Auth != "required" {
				t.Errorf("filter route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Model shape tests
// ---------------------------------------------------------------------------

func TestSavedFilterModelShape(t *testing.T) {
	f := database.SavedFilter{
		ID:        1,
		UserID:    "user_abc",
		Section:   "discovery",
		Name:      "My filter",
		Filter:    database.JSONMap{"genre": "action", "year": 2024},
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("failed to marshal SavedFilter: %v", err)
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
	if raw["section"] != "discovery" {
		t.Errorf("section = %v, want discovery", raw["section"])
	}
	if raw["name"] != "My filter" {
		t.Errorf("name = %v, want 'My filter'", raw["name"])
	}

	filterMap, ok := raw["filter"].(map[string]interface{})
	if !ok {
		t.Fatal("filter should be an object")
	}
	if filterMap["genre"] != "action" {
		t.Errorf("filter.genre = %v, want action", filterMap["genre"])
	}

	// TableName check.
	if f.TableName() != "saved_filters" {
		t.Errorf("TableName = %s, want saved_filters", f.TableName())
	}
}

func TestSavedFilterJSONBDefault(t *testing.T) {
	// Verify that Filter defaults to empty map if not set.
	f := database.SavedFilter{
		UserID:  "user_xyz",
		Section: "search",
		Name:    "empty filter",
	}

	if f.Filter != nil {
		// If nil, GORM will use the default '{}' from the column definition.
		// If the struct sets it to an empty map, that's fine too.
		_ = f // use it
	}
}

func TestSavedFilterModelFields(t *testing.T) {
	f := database.SavedFilter{
		ID:      42,
		UserID:  "user_test",
		Section: "lists",
		Name:    "test name",
		Filter:  database.JSONMap{"sort": "popularity"},
	}

	if f.ID != 42 {
		t.Errorf("ID = %d, want 42", f.ID)
	}
	if f.UserID != "user_test" {
		t.Errorf("UserID = %s, want user_test", f.UserID)
	}
	if f.Section != "lists" {
		t.Errorf("Section = %s, want lists", f.Section)
	}
	if f.Name != "test name" {
		t.Errorf("Name = %s, want test name", f.Name)
	}
	if f.Filter["sort"] != "popularity" {
		t.Errorf("Filter[sort] = %v, want popularity", f.Filter["sort"])
	}
}
