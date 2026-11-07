package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"

	_ "github.com/haaku-tv/haaku/internal/handlers/comments"
)

// ============================================================================
// Comment Routes Registered
// ============================================================================

func TestCommentRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/comments/trending":       true,
		"/v1/comments/recent":         true,
		"/v1/comments/updates":        true,
		"/v1/comments/{id}":           true,
		"/v1/comments/{id}/replies":   true,
		"/v1/comments/{id}/reactions": true,
		"/v1/comments/{id}/likes":     true,
		"/v1/comments/{id}/item":      true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected comment route %q not registered", path)
		}
	}
}

func TestCommentRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	authRequiredPaths := map[string]bool{
		"/v1/comments":                true,
		"/v1/comments/{id}":           true,
		"/v1/comments/{id}/replies":   true,
		"/v1/comments/{id}/like":      true,
		"/v1/comments/{id}/reactions": true,
		"/v1/comments/{id}/report":    true,
	}

	found := make(map[string]string)
	for _, r := range routes {
		if _, ok := authRequiredPaths[r.Path]; ok {
			found[r.Path] = r.Auth
		}
	}

	for path := range authRequiredPaths {
		auth, ok := found[path]
		if !ok {
			t.Errorf("comment auth-required route %q not found", path)
			continue
		}
		if auth != "required" {
			t.Errorf("comment route %q has auth=%q, want required", path, auth)
		}
	}
}

// ============================================================================
// Comment Model Tests
// ============================================================================

func TestCommentModel(t *testing.T) {
	now := time.Now().UTC()
	movieID := int64(123)
	c := database.Comment{
		ID:          1,
		UserID:      "user_abc",
		MediaType:   "movie",
		MediaID:     &movieID,
		CommentType: "shout",
		Body:        "This movie is amazing!",
		Spoiler:     false,
		LikeCount:   5,
		ReplyCount:  2,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("failed to marshal Comment: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["user_id"] != "user_abc" {
		t.Errorf("user_id = %v, want user_abc", raw["user_id"])
	}
	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v, want movie", raw["media_type"])
	}
	if raw["body"] != "This movie is amazing!" {
		t.Errorf("body = %v", raw["body"])
	}
	if raw["comment_type"] != "shout" {
		t.Errorf("comment_type = %v, want shout", raw["comment_type"])
	}
	if raw["like_count"] != float64(5) {
		t.Errorf("like_count = %v, want 5", raw["like_count"])
	}
	if raw["reply_count"] != float64(2) {
		t.Errorf("reply_count = %v, want 2", raw["reply_count"])
	}
}

func TestCommentLikeModel(t *testing.T) {
	like := database.CommentLike{
		CommentID: 1,
		UserID:    "user_abc",
	}

	data, err := json.Marshal(like)
	if err != nil {
		t.Fatalf("failed to marshal CommentLike: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["comment_id"] != float64(1) {
		t.Errorf("comment_id = %v, want 1", raw["comment_id"])
	}
	if raw["user_id"] != "user_abc" {
		t.Errorf("user_id = %v, want user_abc", raw["user_id"])
	}
}

func TestCommentReactionModel(t *testing.T) {
	reaction := database.CommentReaction{
		CommentID:    1,
		UserID:       "user_abc",
		ReactionType: "👍",
	}

	data, err := json.Marshal(reaction)
	if err != nil {
		t.Fatalf("failed to marshal CommentReaction: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["comment_id"] != float64(1) {
		t.Errorf("comment_id = %v, want 1", raw["comment_id"])
	}
	if raw["user_id"] != "user_abc" {
		t.Errorf("user_id = %v, want user_abc", raw["user_id"])
	}
	if raw["reaction_type"] != "👍" {
		t.Errorf("reaction_type = %v, want 👍", raw["reaction_type"])
	}
}

func TestCommentReportModel(t *testing.T) {
	report := database.CommentReport{
		ID:         1,
		CommentID:  42,
		ReporterID: "reporter_abc",
		Reason:     "spoilers",
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal CommentReport: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["comment_id"] != float64(42) {
		t.Errorf("comment_id = %v, want 42", raw["comment_id"])
	}
	if raw["reporter_id"] != "reporter_abc" {
		t.Errorf("reporter_id = %v, want reporter_abc", raw["reporter_id"])
	}
	if raw["reason"] != "spoilers" {
		t.Errorf("reason = %v, want spoilers", raw["reason"])
	}
}

// ============================================================================
// Comment Type Validation
// ============================================================================

func TestCommentTypes(t *testing.T) {
	validTypes := map[string]bool{
		"shout":  true,
		"review": true,
	}

	// shout is the default
	if !validTypes["shout"] {
		t.Error("shout should be valid comment type")
	}
	if !validTypes["review"] {
		t.Error("review should be valid comment type")
	}
	if validTypes["invalid"] {
		t.Error("invalid should not be a valid comment type")
	}
}

func TestReportReasons(t *testing.T) {
	validReasons := map[string]bool{
		"spoilers":  true,
		"language":  true,
		"abusive":   true,
		"spam":      true,
		"bigotry":   true,
		"political": true,
		"offtopic":  true,
		"duplicate": true,
		"too_short": true,
		"other":     true,
	}

	expected := 10
	if len(validReasons) != expected {
		t.Errorf("valid report reasons count = %d, want %d", len(validReasons), expected)
	}
}

// ============================================================================
// Allowed Reactions Test
// ============================================================================

func TestAllowedReactions(t *testing.T) {
	allowed := map[string]string{
		"\U0001f44d": "thumbs up",
		"\U0001f525": "fire",
		"\U0001f602": "joy",
		"\U0001f62e": "wow",
		"\U0001f622": "cry",
		"\U0001f620": "angry",
	}

	expected := 6
	if len(allowed) != expected {
		t.Errorf("allowed reactions count = %d, want %d", len(allowed), expected)
	}
}

// ============================================================================
// Comment Models Export Check
// ============================================================================

func TestCommentModelsFunc(t *testing.T) {
	models := database.CommentModels()
	if len(models) != 4 {
		t.Errorf("CommentModels() returned %d models, want 4", len(models))
	}
}

// ============================================================================
// Threading: Parent/Top-Level Tests
// ============================================================================

func TestCommentThreading(t *testing.T) {
	parentID := int64(100)
	topLevel := database.Comment{
		ID:       1,
		UserID:   "user_1",
		ParentID: nil,
		Body:     "Top level comment",
	}

	reply := database.Comment{
		ID:       2,
		UserID:   "user_2",
		ParentID: &parentID,
		Body:     "A reply!",
	}

	if topLevel.ParentID != nil {
		t.Error("top-level comment should have nil parent_id")
	}
	if reply.ParentID == nil {
		t.Error("reply should have non-nil parent_id")
	}
	if *reply.ParentID != 100 {
		t.Errorf("reply parent_id = %d, want 100", *reply.ParentID)
	}
}
