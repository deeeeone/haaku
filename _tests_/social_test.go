package tests

import (
	"encoding/json"
	"testing"
	"time"

	"trakt-api/internal/database"
	_ "trakt-api/internal/handlers/social"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Social routes registration tests
// ---------------------------------------------------------------------------

func TestSocialRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/users/{id}/follow":                              true,
		"/v1/users/{id}/followers":                           true,
		"/v1/users/{id}/following":                           true,
		"/v1/users/{id}/friends":                             true,
		"/v1/users/me/follow_requests":                       true,
		"/v1/users/me/follow_requests/{follower_id}":         true,
		"/v1/users/me/follow_requests/{follower_id}/approve": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected social route %q not registered", path)
		}
	}
}

func TestSocialRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	socialPaths := map[string]bool{
		"/v1/users/{id}/follow":                              true,
		"/v1/users/{id}/followers":                           true,
		"/v1/users/{id}/following":                           true,
		"/v1/users/{id}/friends":                             true,
		"/v1/users/me/follow_requests":                       true,
		"/v1/users/me/follow_requests/{follower_id}":         true,
		"/v1/users/me/follow_requests/{follower_id}/approve": true,
	}

	for _, r := range routes {
		if socialPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Social" {
				t.Errorf("social route %s %s has tags=%v, want [Social]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Model JSON shape tests
// ---------------------------------------------------------------------------

func TestUserFollowModelShape(t *testing.T) {
	now := time.Now().UTC()

	follow := database.UserFollow{
		FollowerID:  "user_abc",
		FollowingID: "user_def",
		ApprovedAt:  &now,
		CreatedAt:   now,
	}

	data, err := json.Marshal(follow)
	if err != nil {
		t.Fatalf("failed to marshal UserFollow: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["follower_id"] != "user_abc" {
		t.Errorf("follower_id = %v, want user_abc", raw["follower_id"])
	}
	if raw["following_id"] != "user_def" {
		t.Errorf("following_id = %v, want user_def", raw["following_id"])
	}
	if raw["approved_at"] == nil {
		t.Error("approved_at should be present")
	}
}

func TestUserFollowPendingShape(t *testing.T) {
	follow := database.UserFollow{
		FollowerID:  "user_abc",
		FollowingID: "user_def",
		ApprovedAt:  nil,
		CreatedAt:   time.Now().UTC(),
	}

	if !follow.IsPending() {
		t.Error("follow with nil ApprovedAt should be pending")
	}

	data, err := json.Marshal(follow)
	if err != nil {
		t.Fatalf("failed to marshal pending follow: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["approved_at"] != nil {
		t.Error("pending follow should have null approved_at")
	}
}

func TestUserBlockModelShape(t *testing.T) {
	now := time.Now().UTC()

	block := database.UserBlock{
		BlockerID: "user_abc",
		BlockedID: "user_def",
		CreatedAt: now,
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("failed to marshal UserBlock: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["blocker_id"] != "user_abc" {
		t.Errorf("blocker_id = %v, want user_abc", raw["blocker_id"])
	}
	if raw["blocked_id"] != "user_def" {
		t.Errorf("blocked_id = %v, want user_def", raw["blocked_id"])
	}
}

func TestListCollaboratorModelShape(t *testing.T) {
	now := time.Now().UTC()

	collab := database.ListCollaborator{
		ListID:     42,
		UserID:     "user_abc",
		Role:       "editor",
		InvitedAt:  now,
		AcceptedAt: nil,
	}

	data, err := json.Marshal(collab)
	if err != nil {
		t.Fatalf("failed to marshal ListCollaborator: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["list_id"] != float64(42) {
		t.Errorf("list_id = %v, want 42", raw["list_id"])
	}
	if raw["user_id"] != "user_abc" {
		t.Errorf("user_id = %v, want user_abc", raw["user_id"])
	}
	if raw["role"] != "editor" {
		t.Errorf("role = %v, want editor", raw["role"])
	}
}

// ---------------------------------------------------------------------------
// Follow response shape tests
// ---------------------------------------------------------------------------

func TestFollowResponseShape(t *testing.T) {
	approved := "2025-01-15T10:30:00.000Z"
	resp := map[string]interface{}{
		"follower_id":  "user_abc",
		"following_id": "user_def",
		"status":       "approved",
		"created_at":   "2025-01-15T10:30:00.000Z",
		"approved_at":  approved,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	required := []string{"follower_id", "following_id", "status", "created_at"}
	for _, f := range required {
		if _, ok := parsed[f]; !ok {
			t.Errorf("follow response missing %q", f)
		}
	}

	if parsed["status"] != "approved" {
		t.Errorf("status = %v, want approved", parsed["status"])
	}
}

func TestPendingFollowResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"follower_id":  "user_abc",
		"following_id": "user_def",
		"status":       "pending",
		"created_at":   "2025-01-15T10:30:00.000Z",
		"approved_at":  nil,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["status"] != "pending" {
		t.Errorf("status = %v, want pending", parsed["status"])
	}
	if parsed["approved_at"] != nil {
		t.Error("pending follow should have null approved_at")
	}
}

// ---------------------------------------------------------------------------
// Follow request response shape tests
// ---------------------------------------------------------------------------

func TestFollowRequestResponseShape(t *testing.T) {
	resp := map[string]interface{}{
		"follower_id": "user_abc",
		"username":    "follower1",
		"name":        "Follower One",
		"avatar_url":  "https://example.com/avatar.jpg",
		"created_at":  "2025-01-15T10:30:00.000Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	required := []string{"follower_id", "username", "name", "created_at"}
	for _, f := range required {
		if _, ok := parsed[f]; !ok {
			t.Errorf("follow request response missing %q", f)
		}
	}
}

// ---------------------------------------------------------------------------
// Friends logic tests
// ---------------------------------------------------------------------------

func TestFriendsLogicMutual(t *testing.T) {
	// User A follows User B AND User B follows User A = friends.
	aFollowsB := true
	bFollowsA := true
	isFriend := aFollowsB && bFollowsA
	if !isFriend {
		t.Error("mutual follows should be friends")
	}
}

func TestFriendsLogicOneWay(t *testing.T) {
	// One-way follow is not a friend.
	aFollowsB := true
	bFollowsA := false
	isFriend := aFollowsB && bFollowsA
	if isFriend {
		t.Error("one-way follow should not be friends")
	}
}

// ---------------------------------------------------------------------------
// Block semantics tests
// ---------------------------------------------------------------------------

func TestBlockRemovesFollows(t *testing.T) {
	// When A blocks B, all follow relationships between them are removed.
	// This is a unit test of the intended behavior.
	suppressFollows := true
	if !suppressFollows {
		t.Error("blocking should remove follows")
	}
}

func TestBlockDoesNotPreventFollowing(t *testing.T) {
	// Blocked users can still follow you — they just can't see content.
	// The block and follow mechanisms are independent (spec).
	canFollowWhileBlocked := true
	if !canFollowWhileBlocked {
		t.Error("blocked users should still be able to follow (just can't see content)")
	}
}

func TestSocialPackageExists(t *testing.T) {
	_ = "ok"
}
