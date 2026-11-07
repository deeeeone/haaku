package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
	"github.com/haaku-tv/haaku/internal/openapi"
)

// ---------------------------------------------------------------------------
// Route registration tests
// ---------------------------------------------------------------------------

func TestReportsRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/movies/{id}/report":   true,
		"/v1/shows/{id}/report":    true,
		"/v1/seasons/{id}/report":  true,
		"/v1/episodes/{id}/report": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected report route %q not registered", path)
		}
	}
}

func TestReportsRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	reportPaths := map[string]bool{
		"/v1/movies/{id}/report":   true,
		"/v1/shows/{id}/report":    true,
		"/v1/seasons/{id}/report":  true,
		"/v1/episodes/{id}/report": true,
	}

	for _, r := range routes {
		if reportPaths[r.Path] {
			if len(r.Tags) == 0 || r.Tags[0] != "Reports" {
				t.Errorf("report route %s %s has tags=%v, want [Reports]",
					r.Method, r.Path, r.Tags)
			}
		}
	}
}

func TestReportsRoutesRequireAuth(t *testing.T) {
	routes := openapi.AllRoutes()

	reportPaths := map[string]bool{
		"/v1/movies/{id}/report":   true,
		"/v1/shows/{id}/report":    true,
		"/v1/seasons/{id}/report":  true,
		"/v1/episodes/{id}/report": true,
	}

	for _, r := range routes {
		if reportPaths[r.Path] {
			if r.Auth != "required" {
				t.Errorf("report route %s %s has auth=%q, want 'required'",
					r.Method, r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Model shape tests
// ---------------------------------------------------------------------------

func TestContentReportModelShape(t *testing.T) {
	r := database.ContentReport{
		ID:          1,
		ReporterID:  "user_abc",
		MediaType:   "movie",
		MediaID:     12345,
		Reason:      "spam",
		Description: "This entry contains spam content",
		CreatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal ContentReport: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if raw["id"].(float64) != 1 {
		t.Errorf("id = %v, want 1", raw["id"])
	}
	if raw["reporter_id"] != "user_abc" {
		t.Errorf("reporter_id = %v, want user_abc", raw["reporter_id"])
	}
	if raw["media_type"] != "movie" {
		t.Errorf("media_type = %v, want movie", raw["media_type"])
	}
	if raw["media_id"].(float64) != 12345 {
		t.Errorf("media_id = %v, want 12345", raw["media_id"])
	}
	if raw["reason"] != "spam" {
		t.Errorf("reason = %v, want spam", raw["reason"])
	}
	if raw["description"] != "This entry contains spam content" {
		t.Errorf("description = %v, want 'This entry contains spam content'", raw["description"])
	}

	// TableName check.
	if r.TableName() != "content_reports" {
		t.Errorf("TableName = %s, want content_reports", r.TableName())
	}
}

func TestContentReportAllValidReasons(t *testing.T) {
	validReasons := []string{"spam", "spoiler", "offensive", "incorrect", "duplicate", "other"}

	for _, reason := range validReasons {
		r := database.ContentReport{
			ReporterID: "user_abc",
			MediaType:  "show",
			MediaID:    123,
			Reason:     reason,
		}
		if r.Reason != reason {
			t.Errorf("reason = %s, want %s", r.Reason, reason)
		}
	}
}

func TestContentReportDescriptionOptional(t *testing.T) {
	// Description defaults to empty string.
	r := database.ContentReport{
		ReporterID: "user_abc",
		MediaType:  "movie",
		MediaID:    456,
		Reason:     "other",
	}

	if r.Description != "" {
		t.Errorf("Description default = %q, want empty string", r.Description)
	}
}

func TestContentReportMediaTypes(t *testing.T) {
	mediaTypes := []string{"movie", "show", "season", "episode"}

	for _, mt := range mediaTypes {
		r := database.ContentReport{
			ReporterID: "user_abc",
			MediaType:  mt,
			MediaID:    1,
			Reason:     "offensive",
		}
		if r.MediaType != mt {
			t.Errorf("for mt=%s, MediaType = %s", mt, r.MediaType)
		}
	}
}
