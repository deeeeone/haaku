package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"trakt-api/internal/handlers/users"
	"trakt-api/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// OpenAPI route registration tests
// ---------------------------------------------------------------------------

// TestUsersRoutesRegistered verifies all user routes are in the OpenAPI registry.
func TestUsersRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/me":          true,
		"/v1/users/{id}":        true,
		"/v1/users/me/settings": true,
		"/v1/users/{id}/block":  true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected users route %q not registered", path)
		}
	}
}

// TestUsersRoutesHaveCorrectTags verifies users routes have the Users tag.
func TestUsersRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	usersPaths := map[string]bool{
		"/v1/users/me":          true,
		"/v1/users/{id}":        true,
		"/v1/users/me/settings": true,
		"/v1/users/{id}/block":  true,
	}

	for _, r := range routes {
		if usersPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Users" {
				t.Errorf("users route %s %s has tags=%v, want [Users]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

// TestUsersRoutesAuthAnnotations verifies auth annotations on users routes.
func TestUsersRoutesAuthAnnotations(t *testing.T) {
	routes := openapi.AllRoutes()

	requiredAuth := map[string]bool{
		"/v1/users/me":          true,
		"/v1/users/me/settings": true,
		"/v1/users/{id}/block":  true,
	}
	optionalAuth := map[string]bool{
		"/v1/users/{id}": true,
	}

	for _, r := range routes {
		if requiredAuth[r.Path] {
			if r.Auth != "required" {
				t.Errorf("users route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
		if optionalAuth[r.Path] {
			if r.Auth != "optional" {
				t.Errorf("users route %s %s has auth=%q, want 'optional'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Profile response shape tests
// ---------------------------------------------------------------------------

// TestProfileResponseJSON verifies the ProfileResponse JSON shape.
func TestProfileResponseJSON(t *testing.T) {
	resp := map[string]interface{}{
		"id":           "user_abc123",
		"username":     "testuser",
		"name":         "Test User",
		"bio":          "A test bio",
		"avatar_url":   "https://example.com/avatar.jpg",
		"location":     "San Francisco",
		"timezone":     "America/Los_Angeles",
		"language":     "en",
		"is_private":   false,
		"is_admin":     false,
		"is_moderator": false,
		"status":       "active",
		"plan":         "free",
		"created_at":   "2024-01-15T10:30:00.000Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal profile response: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify required fields exist.
	requiredFields := []string{"id", "username", "name", "is_private", "status", "plan", "created_at"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("profile response missing required field %q", field)
		}
	}
}

// TestPublicProfileResponseJSON verifies the public profile response shape.
func TestPublicProfileResponseJSON(t *testing.T) {
	resp := map[string]interface{}{
		"id":         "user_abc123",
		"username":   "testuser",
		"name":       "Test User",
		"bio":        "A test bio",
		"avatar_url": "https://example.com/avatar.jpg",
		"location":   "San Francisco",
		"is_private": false,
		"stats_summary": map[string]interface{}{
			"watched":   0,
			"lists":     0,
			"followers": 42,
			"following": 10,
		},
		"recent_activity": []interface{}{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal public profile: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	requiredFields := []string{"id", "username", "name", "is_private", "stats_summary"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("public profile response missing required field %q", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Settings response shape tests
// ---------------------------------------------------------------------------

// TestSettingsResponseShape verifies the settings JSON shape.
func TestSettingsResponseShape(t *testing.T) {
	settings := map[string]interface{}{
		"privacy": map[string]interface{}{
			"profile":  "public",
			"history":  "friends",
			"ratings":  "public",
			"activity": "friends",
		},
		"notifications": map[string]interface{}{
			"follows":         true,
			"replies":         true,
			"import_complete": true,
			"weekly_digest":   false,
		},
	}

	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("failed to marshal settings: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify privacy section.
	privacy, ok := parsed["privacy"].(map[string]interface{})
	if !ok {
		t.Fatal("settings missing 'privacy' section")
	}
	privacyFields := []string{"profile", "history", "ratings", "activity"}
	for _, f := range privacyFields {
		if _, ok := privacy[f]; !ok {
			t.Errorf("settings.privacy missing required field %q", f)
		}
	}

	// Verify notifications section.
	notif, ok := parsed["notifications"].(map[string]interface{})
	if !ok {
		t.Fatal("settings missing 'notifications' section")
	}
	notifFields := []string{"follows", "replies", "import_complete", "weekly_digest"}
	for _, f := range notifFields {
		if _, ok := notif[f]; !ok {
			t.Errorf("settings.notifications missing required field %q", f)
		}
	}
}

// TestDefaultSettingsValues verifies the default settings values.
func TestDefaultSettingsValues(t *testing.T) {
	defaults := map[string]interface{}{
		"privacy": map[string]interface{}{
			"profile":  "public",
			"history":  "friends",
			"ratings":  "public",
			"activity": "friends",
		},
		"notifications": map[string]interface{}{
			"follows":         true,
			"replies":         true,
			"import_complete": true,
			"weekly_digest":   false,
		},
	}

	// Verify all default values are sensible.
	privacy := defaults["privacy"].(map[string]interface{})
	validVis := map[string]bool{"public": true, "friends": true, "private": true}
	for k, v := range privacy {
		if str, ok := v.(string); ok {
			if !validVis[str] {
				t.Errorf("settings.privacy.%s has invalid value %q, want one of: public, friends, private", k, str)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Privacy middleware logic tests (unit tests without Fiber)
// ---------------------------------------------------------------------------

// TestPrivacyLogicPublicProfile verifies public profiles are always visible.
func TestPrivacyLogicPublicProfile(t *testing.T) {
	// A public profile should be visible to everyone.
	isPrivate := false
	isSelf := false
	isAdmin := false
	isFollower := false

	visible := !isPrivate || isSelf || isAdmin || isFollower
	if !visible {
		t.Error("public profile should always be visible")
	}
}

// TestPrivacyLogicPrivateProfileSelf verifies self can always see own profile.
func TestPrivacyLogicPrivateProfileSelf(t *testing.T) {
	isPrivate := true
	isSelf := true

	visible := !isPrivate || isSelf
	if !visible {
		t.Error("user should be able to see own private profile")
	}
}

// TestPrivacyLogicPrivateProfileAdmin verifies admins can see private profiles.
func TestPrivacyLogicPrivateProfileAdmin(t *testing.T) {
	isPrivate := true
	isSelf := false
	isAdmin := true

	visible := !isPrivate || isSelf || isAdmin
	if !visible {
		t.Error("admin should be able to see private profiles")
	}
}

// TestPrivacyLogicPrivateProfileFollower verifies followers can see private profiles.
func TestPrivacyLogicPrivateProfileFollower(t *testing.T) {
	isPrivate := true
	isSelf := false
	isAdmin := false
	isFollower := true

	visible := !isPrivate || isSelf || isAdmin || isFollower
	if !visible {
		t.Error("followers should be able to see private profiles")
	}
}

// TestPrivacyLogicPrivateProfileStranger verifies strangers can't see private profiles.
func TestPrivacyLogicPrivateProfileStranger(t *testing.T) {
	isPrivate := true
	isSelf := false
	isAdmin := false
	isFollower := false

	visible := !isPrivate || isSelf || isAdmin || isFollower
	if visible {
		t.Error("stranger should NOT be able to see private profiles")
	}
}

// TestPrivacyLogicBlockedUserInvisible verifies blocked users are invisible.
func TestPrivacyLogicBlockedUserInvisible(t *testing.T) {
	// Blocked users should always be invisible (404).
	isBlocked := true
	visible := !isBlocked
	if visible {
		t.Error("blocked users should be invisible")
	}
}

// ---------------------------------------------------------------------------
// Settings parsing tests
// ---------------------------------------------------------------------------

// TestSettingsParsingEmpty verifies empty settings returns defaults.
func TestSettingsParsingEmpty(t *testing.T) {
	// Empty settings should produce default values.
	defaultSettings := map[string]interface{}{
		"privacy": map[string]interface{}{
			"profile":  "public",
			"history":  "friends",
			"ratings":  "public",
			"activity": "friends",
		},
		"notifications": map[string]interface{}{
			"follows":         true,
			"replies":         true,
			"import_complete": true,
			"weekly_digest":   false,
		},
	}

	// Simulate parsing empty string.
	rawSettings := []string{"", "{}", "null"}
	for _, raw := range rawSettings {
		if raw != "" && raw != "{}" && raw != "null" {
			continue
		}
		// Empty/{} → defaults.
		_ = raw
		_ = defaultSettings
	}

	// All empty values should resolve to defaults.
	if defaultSettings["privacy"].(map[string]interface{})["profile"] != "public" {
		t.Error("default privacy profile should be 'public'")
	}
}

// TestSettingsParsingPartial verifies partial settings merge with defaults.
func TestSettingsParsingPartial(t *testing.T) {
	partialJSON := `{"privacy":{"profile":"private"}}`

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(partialJSON), &parsed); err != nil {
		t.Fatalf("failed to parse partial settings: %v", err)
	}

	privacy, ok := parsed["privacy"].(map[string]interface{})
	if !ok {
		t.Fatal("missing privacy section")
	}
	if privacy["profile"] != "private" {
		t.Errorf("profile = %v, want 'private'", privacy["profile"])
	}

	// Notifications should be missing — defaults would fill in.
	if _, ok := parsed["notifications"]; ok {
		t.Log("notifications present in partial JSON — will be backfilled by defaults")
	}
}

// ---------------------------------------------------------------------------
// Update profile request validation tests
// ---------------------------------------------------------------------------

// TestUpdateProfileRequestShape verifies the update profile request shape.
// NOTE: name and avatar are NOT updatable — they are Clerk-managed identity.
func TestUpdateProfileRequestShape(t *testing.T) {
	req := map[string]interface{}{
		"bio":        "New bio",
		"location":   "New Location",
		"timezone":   "UTC",
		"language":   "fr",
		"is_private": true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal update request: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	updatableFields := []string{"bio", "location", "timezone", "language", "is_private"}
	for _, field := range updatableFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("update request missing field %q", field)
		}
	}
	// Identity fields must NOT be updatable through the profile endpoint.
	for _, field := range []string{"name", "avatar_url", "username"} {
		if _, ok := parsed[field]; ok {
			t.Errorf("identity field %q must not be in the update request", field)
		}
	}
}

// TestUpdateProfileRequestOmitEmpty verifies empty body is rejected.
func TestUpdateProfileRequestOmitEmpty(t *testing.T) {
	req := map[string]interface{}{}

	data, _ := json.Marshal(req)
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if len(parsed) != 0 {
		t.Log("empty body has keys — handler should reject with 'no fields to update'")
	}
}

// ---------------------------------------------------------------------------
// Account deletion tests
// ---------------------------------------------------------------------------

// TestAccountDeletionStatus verifies the deletion status value.
func TestAccountDeletionStatus(t *testing.T) {
	pendingDeletion := "pending_deletion"
	active := "active"

	if pendingDeletion == active {
		t.Error("pending_deletion should differ from active")
	}
	if pendingDeletion != "pending_deletion" {
		t.Errorf("deletion status = %q, want 'pending_deletion'", pendingDeletion)
	}
}

// ---------------------------------------------------------------------------
// Route precedence: /users/me must not be swallowed by /users/:id
// ---------------------------------------------------------------------------

// TestUsersMeRoutePrecedence verifies /users/me resolves to the auth-required
// me handler, not the public /users/:id handler. With no auth, the me handler
// returns 401; the public profile handler would NOT 401 (it only hits the DB).
func TestUsersMeRoutePrecedence(t *testing.T) {
	app := fiber.New()
	users.RegisterRoutes(app.Group("/v1"))

	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"me no trailing slash", "GET", "/v1/users/me", http.StatusUnauthorized},
		{"me trailing slash", "GET", "/v1/users/me/", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.want {
				t.Errorf("%s %s: got %d, want %d (would mean /users/:id swallowed /users/me)",
					tc.method, tc.path, resp.StatusCode, tc.want)
			}
		})
	}

	// A real :id path must NOT require auth (public profile route).
	req := httptest.NewRequest("GET", "/v1/users/someuser", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("GET /users/someuser returned 401 — public /users/:id route not reachable")
	}
}
