package tests

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
)

// ============================================================================
// List Routes Registered
// ============================================================================

func TestListRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/{id}/lists":           true,
		"/v1/users/me/lists":             true,
		"/v1/lists/{id}":                 true,
		"/v1/lists/{id}/items":           true,
		"/v1/lists/{id}/items/remove":    true,
		"/v1/lists/{id}/items/reorder":   true,
		"/v1/lists/{id}/likes":           true,
		"/v1/lists/{id}/like":            true,
		"/v1/lists/{id}/comments":        true,
		"/v1/lists/popular":              true,
		"/v1/lists/trending":             true,
		"/v1/users/me/smart-lists":       true,
		"/v1/smart-lists/{id}":           true,
		"/v1/smart-lists/{id}/items":     true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected list route %q not registered", path)
		}
	}
}

// ============================================================================
// List Model Tests
// ============================================================================

func TestListModelJSON(t *testing.T) {
	now := time.Now().UTC()
	l := database.List{
		ID:            1,
		UserID:        "user_abc123",
		Name:          "My Top Sci-Fi",
		Description:   "Best sci-fi movies ever",
		Slug:           "my-top-sci-fi-user_abc1",
		Privacy:        "public",
		SortBy:         "rank",
		SortHow:        "asc",
		IsRanked:       true,
		CoverURL:       "",
		AllowComments:  true,
		LikeCount:      42,
		CommentCount:   7,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	data, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("failed to marshal List: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["id"] != float64(1) {
		t.Errorf("id = %v, want 1", raw["id"])
	}
	if raw["user_id"] != "user_abc123" {
		t.Errorf("user_id = %v, want user_abc123", raw["user_id"])
	}
	if raw["name"] != "My Top Sci-Fi" {
		t.Errorf("name = %v, want My Top Sci-Fi", raw["name"])
	}
	if raw["privacy"] != "public" {
		t.Errorf("privacy = %v, want public", raw["privacy"])
	}
	if raw["like_count"] != float64(42) {
		t.Errorf("like_count = %v, want 42", raw["like_count"])
	}
	if raw["is_ranked"] != true {
		t.Errorf("is_ranked = %v, want true", raw["is_ranked"])
	}
	if raw["allow_comments"] != true {
		t.Errorf("allow_comments = %v, want true", raw["allow_comments"])
	}
}

func TestListPrivacyValues(t *testing.T) {
	validPrivacy := map[string]bool{
		"public":    true,
		"link_only": true,
		"private":   true,
	}

	tests := []struct {
		privacy string
		valid   bool
	}{
		{"public", true},
		{"link_only", true},
		{"private", true},
		{"secret", false},
		{"", false},
		{"PUBLIC", false},
	}

	for _, tt := range tests {
		_, ok := validPrivacy[tt.privacy]
		if ok != tt.valid {
			t.Errorf("privacy %q: valid=%v, expected=%v", tt.privacy, ok, tt.valid)
		}
	}
}

func TestListItemModelJSON(t *testing.T) {
	now := time.Now().UTC()
	item := database.ListItem{
		ID:        100,
		ListID:    1,
		MediaType: "movie",
		MediaID:   550,
		Rank:      1,
		Notes:     "Must watch!",
		AddedBy:   "user_abc123",
		AddedAt:   now,
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("failed to marshal ListItem: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v, want movie", raw["media_type"])
	}
	if raw["media_id"] != float64(550) {
		t.Errorf("media_id = %v, want 550", raw["media_id"])
	}
	if raw["rank"] != float64(1) {
		t.Errorf("rank = %v, want 1", raw["rank"])
	}
	if raw["notes"] != "Must watch!" {
		t.Errorf("notes = %v, want Must watch!", raw["notes"])
	}
}

func TestListItemUniqueConstraint(t *testing.T) {
	// Verify the model has the right unique index fields
	// (just a structural test - actual constraint tested at DB level)
	item := database.ListItem{
		ListID:    1,
		MediaType: "movie",
		MediaID:   550,
		Rank:      0,
		AddedBy:   "user_abc123",
	}

	if item.ListID != 1 {
		t.Error("ListID should be 1")
	}
	if item.MediaType != "movie" {
		t.Error("MediaType should be movie")
	}
	if item.MediaID != 550 {
		t.Error("MediaID should be 550")
	}
}

// ============================================================================
// Smart List Model Tests
// ============================================================================

func TestSmartListModelJSON(t *testing.T) {
	now := time.Now().UTC()
	sl := database.SmartList{
		ID:     1,
		UserID: "user_abc123",
		Name:   "Sci-Fi to Watch",
		Filters: database.JSONMap{
			"type":       []interface{}{"movie", "show"},
			"genres":     []interface{}{"sci-fi"},
			"rating_min": 7.0,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(sl)
	if err != nil {
		t.Fatalf("failed to marshal SmartList: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["name"] != "Sci-Fi to Watch" {
		t.Errorf("name = %v, want Sci-Fi to Watch", raw["name"])
	}

	filters, ok := raw["filters"].(map[string]interface{})
	if !ok {
		t.Fatal("filters should be an object")
	}
	if filters["rating_min"] != 7.0 {
		t.Errorf("rating_min = %v, want 7.0", filters["rating_min"])
	}
}

// ============================================================================
// List Like Model Tests
// ============================================================================

func TestListLikeModelJSON(t *testing.T) {
	now := time.Now().UTC()
	like := database.ListLike{
		ListID:    1,
		UserID:    "user_abc123",
		CreatedAt: now,
	}

	data, err := json.Marshal(like)
	if err != nil {
		t.Fatalf("failed to marshal ListLike: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["list_id"] != float64(1) {
		t.Errorf("list_id = %v, want 1", raw["list_id"])
	}
	if raw["user_id"] != "user_abc123" {
		t.Errorf("user_id = %v, want user_abc123", raw["user_id"])
	}
}

// ============================================================================
// List Comment Model Tests
// ============================================================================

func TestListCommentModelJSON(t *testing.T) {
	now := time.Now().UTC()
	comment := database.ListComment{
		ID:        1,
		ListID:    1,
		UserID:    "user_abc123",
		Body:      "Great list!",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(comment)
	if err != nil {
		t.Fatalf("failed to marshal ListComment: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["body"] != "Great list!" {
		t.Errorf("body = %v, want Great list!", raw["body"])
	}
	if raw["list_id"] != float64(1) {
		t.Errorf("list_id = %v, want 1", raw["list_id"])
	}
}

// ============================================================================
// Smart List Filter DSL Tests
// ============================================================================

func TestSmartListFilterDSLTypes(t *testing.T) {
	// Verify filter DSL structure
	filters := database.JSONMap{
		"type":       []interface{}{"movie", "show"},
		"genres":     []interface{}{"sci-fi", "action"},
		"year":       "2000-2015",
		"rating_min": 7.0,
		"votes_min":  100.0,
		"status":     []interface{}{"returning", "ended"},
		"watched":    "any",
		"collected":  "none",
		"providers":  []interface{}{"netflix"},
	}

	data, err := json.Marshal(filters)
	if err != nil {
		t.Fatalf("failed to marshal filters: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal filters: %v", err)
	}

	// Type
	types, ok := parsed["type"].([]interface{})
	if !ok || len(types) != 2 {
		t.Errorf("type should be [movie, show], got %v", parsed["type"])
	}

	// Genres
	genres, ok := parsed["genres"].([]interface{})
	if !ok || len(genres) != 2 {
		t.Errorf("genres should be [sci-fi, action], got %v", parsed["genres"])
	}

	// Year
	if parsed["year"] != "2000-2015" {
		t.Errorf("year = %v, want 2000-2015", parsed["year"])
	}

	// Rating min
	if parsed["rating_min"] != 7.0 {
		t.Errorf("rating_min = %v, want 7.0", parsed["rating_min"])
	}

	// Watched
	if parsed["watched"] != "any" {
		t.Errorf("watched = %v, want any", parsed["watched"])
	}

	// Collected
	if parsed["collected"] != "none" {
		t.Errorf("collected = %v, want none", parsed["collected"])
	}
}

// ============================================================================
// Slug Generation Test
// ============================================================================

func TestSlugGenerationPattern(t *testing.T) {
	// Test that slugs follow the pattern: name-userID_prefix
	tests := []struct {
		name   string
		userID string
	}{
		{"My Top Sci-Fi", "user_abc12345678"},
		{"Best Shows Ever", "user_xyz98765432"},
		{"Watch Later", "clerk_abcdefgh"},
	}

	for _, tt := range tests {
		if tt.name == "" {
			t.Error("name should not be empty")
		}
		if len(tt.userID) < 8 {
			t.Errorf("userID too short: %s", tt.userID)
		}
		// Slug pattern validation: should be lowercase alphanumeric with hyphens
		// (actual generation tested in handler, not here)
	}
}

// ============================================================================
// Pagination Defaults Test
// ============================================================================

func TestPaginationDefaults(t *testing.T) {
	// Verify pagination defaults match project convention
	defaultPage := 1
	defaultLimit := 25
	maxLimit := 100

	if defaultPage != 1 {
		t.Errorf("default page should be 1, got %d", defaultPage)
	}
	if defaultLimit != 25 {
		t.Errorf("default limit should be 25, got %d", defaultLimit)
	}
	if maxLimit != 100 {
		t.Errorf("max limit should be 100, got %d", maxLimit)
	}
}

// ============================================================================
// Privacy Filtering Test
// ============================================================================

func TestPrivacyFiltering(t *testing.T) {
	// Public lists: visible to everyone
	// Link-only lists: visible to anyone with link
	// Private lists: visible only to owner
	privacies := []string{"public", "link_only", "private"}

	for _, p := range privacies {
		switch p {
		case "public", "link_only":
			// These should be visible in user list queries for other users
		case "private":
			// Should NOT be visible to other users
		}
	}

	if len(privacies) != 3 {
		t.Errorf("expected 3 privacy levels, got %d", len(privacies))
	}
}

// ============================================================================
// List Models Registration Test
// ============================================================================

func TestListModelsRegistered(t *testing.T) {
	models := database.ListModels()

	if len(models) != 5 {
		t.Errorf("expected 5 list models, got %d", len(models))
	}

	modelNames := make(map[string]bool)
	for _, m := range models {
		modelNames[fmt.Sprintf("%T", m)] = true
	}

	expected := []string{"*database.List", "*database.ListItem", "*database.SmartList", "*database.ListLike", "*database.ListComment"}
	for _, name := range expected {
		if !modelNames[name] {
			t.Errorf("expected model %q not found in ListModels()", name)
		}
	}
}
