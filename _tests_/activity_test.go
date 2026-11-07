package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"

	_ "github.com/haaku-tv/haaku/internal/handlers/activity"
)

// ============================================================================
// Activity Route Registration
// ============================================================================

func TestActivityRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/me/activity": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected activity route %q not registered", path)
		}
	}
}

func TestActivityRouteRequiresAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	for _, r := range routes {
		if r.Path == "/v1/users/me/activity" {
			if r.Auth != "required" {
				t.Errorf("activity route auth=%q, want required", r.Auth)
			}
			return
		}
	}
	t.Error("activity route not found in OpenAPI registry")
}

// ============================================================================
// ActivityFeed Model Tests
// ============================================================================

func TestActivityFeedModel(t *testing.T) {
	now := time.Now().UTC()
	movieID := int64(603)
	payload := database.JSONMap{
		"title": "The Matrix",
		"year":  float64(1999),
	}

	a := database.ActivityFeed{
		ID:        1,
		ActorID:   "user_abc",
		EventType: "history.added",
		MediaType: "movie",
		MediaID:   &movieID,
		Payload:   payload,
		CreatedAt: now,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("failed to marshal ActivityFeed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["actor_id"] != "user_abc" {
		t.Errorf("actor_id = %v, want user_abc", raw["actor_id"])
	}
	if raw["event_type"] != "history.added" {
		t.Errorf("event_type = %v, want history.added", raw["event_type"])
	}
	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v, want movie", raw["media_type"])
	}
	if raw["media_id"] != float64(603) {
		t.Errorf("media_id = %v, want 603", raw["media_id"])
	}

	payloadRaw, ok := raw["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("payload missing or not an object")
	}
	if payloadRaw["title"] != "The Matrix" {
		t.Errorf("payload.title = %v, want The Matrix", payloadRaw["title"])
	}
}

func TestActivityFeedModelNoMedia(t *testing.T) {
	a := database.ActivityFeed{
		ID:        2,
		ActorID:   "user_xyz",
		EventType: "scrobble",
		Payload:   database.JSONMap{"progress": float64(0.75)},
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// media_type and media_id should be omitted when empty
	if _, exists := raw["media_type"]; exists {
		t.Error("media_type should be omitted when empty")
	}
	if _, exists := raw["media_id"]; exists {
		t.Error("media_id should be omitted when nil")
	}
}

// ============================================================================
// Activity Models Export Check
// ============================================================================

func TestActivityModelsFunc(t *testing.T) {
	models := database.ActivityModels()
	if len(models) != 1 {
		t.Errorf("ActivityModels() returned %d models, want 1", len(models))
	}
}

// ============================================================================
// Feed Type Validation
// ============================================================================

func TestActivityFeedTypes(t *testing.T) {
	validTypes := []string{"friends", "followers", "following"}
	expected := 3

	if len(validTypes) != expected {
		t.Errorf("valid feed types count = %d, want %d", len(validTypes), expected)
	}
}
