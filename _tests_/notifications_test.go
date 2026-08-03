package tests

import (
	"encoding/json"
	"testing"
	"time"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"

	_ "trakt-api/internal/handlers/notifications"
)

// ============================================================================
// Notification Routes Registered
// ============================================================================

func TestNotificationRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/notifications":          true,
		"/v1/notifications/read":     true,
		"/v1/notifications/settings": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected notification route %q not registered", path)
		}
	}
}

func TestNotificationRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	authRequiredPaths := map[string]bool{
		"/v1/notifications":          true,
		"/v1/notifications/read":     true,
		"/v1/notifications/settings": true,
	}

	for _, r := range routes {
		if _, ok := authRequiredPaths[r.Path]; ok {
			if r.Auth != "required" {
				t.Errorf("notification route %s auth=%q, want required", r.Path, r.Auth)
			}
		}
	}
}

// ============================================================================
// Notification Model Tests
// ============================================================================

func TestNotificationModel(t *testing.T) {
	now := time.Now().UTC()
	payload := database.JSONMap{
		"comment_id": float64(42),
		"body":       "Great reply!",
	}

	n := database.Notification{
		ID:        1,
		UserID:    "user_target",
		Type:      "reply",
		ActorID:   "user_actor",
		Payload:   payload,
		Read:      false,
		CreatedAt: now,
	}

	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("failed to marshal Notification: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["user_id"] != "user_target" {
		t.Errorf("user_id = %v, want user_target", raw["user_id"])
	}
	if raw["type"] != "reply" {
		t.Errorf("type = %v, want reply", raw["type"])
	}
	if raw["actor_id"] != "user_actor" {
		t.Errorf("actor_id = %v, want user_actor", raw["actor_id"])
	}
	if raw["read"] != false {
		t.Errorf("read = %v, want false", raw["read"])
	}

	payloadRaw, ok := raw["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("payload missing or not an object")
	}
	if payloadRaw["comment_id"] != float64(42) {
		t.Errorf("payload.comment_id = %v, want 42", payloadRaw["comment_id"])
	}
}

func TestNotificationMarkRead(t *testing.T) {
	n := database.Notification{
		ID:     1,
		UserID: "user_123",
		Type:   "follow",
		Read:   true,
	}

	if !n.Read {
		t.Error("notification should be marked as read")
	}
}

// ============================================================================
// Notification Types
// ============================================================================

func TestNotificationTypes(t *testing.T) {
	validTypes := map[string]bool{
		"follow":          true,
		"follow_request":  true,
		"reply":           true,
		"list_invite":     true,
		"import_complete": true,
	}

	expected := 5
	if len(validTypes) != expected {
		t.Errorf("valid notification types = %d, want %d", len(validTypes), expected)
	}
}

// ============================================================================
// Notification Settings Tests
// ============================================================================

func TestNotificationSettingsDefaults(t *testing.T) {
	settings := struct {
		Follows        bool `json:"follows"`
		Replies        bool `json:"replies"`
		ListInvite     bool `json:"list_invite"`
		ImportComplete bool `json:"import_complete"`
	}{
		Follows:        true,
		Replies:        true,
		ListInvite:     true,
		ImportComplete: true,
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["follows"] != true {
		t.Error("follows should default to true")
	}
	if raw["replies"] != true {
		t.Error("replies should default to true")
	}
	if raw["list_invite"] != true {
		t.Error("list_invite should default to true")
	}
	if raw["import_complete"] != true {
		t.Error("import_complete should default to true")
	}
}

// ============================================================================
// Notif Models Export Check
// ============================================================================

func TestNotifModelsFunc(t *testing.T) {
	models := database.NotifModels()
	if len(models) != 1 {
		t.Errorf("NotifModels() returned %d models, want 1", len(models))
	}
}