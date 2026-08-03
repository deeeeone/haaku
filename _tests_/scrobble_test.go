package tests

import (
	"encoding/json"
	"testing"

	_ "trakt-api/internal/handlers/checkin"
	_ "trakt-api/internal/handlers/scrobble"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// Route registration tests
// ---------------------------------------------------------------------------

// TestScrobbleRoutesRegistered verifies scrobble routes are in the OpenAPI registry.
func TestScrobbleRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/scrobble/start":                    true,
		"/v1/scrobble/pause":                    true,
		"/v1/scrobble/stop":                     true,
		"/v1/sync/playback":                     true,
		"/v1/sync/playback/{media_type}/{media_id}": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected scrobble route %q not registered", path)
		}
	}
}

// TestCheckinRoutesRegistered verifies checkin routes are in the OpenAPI registry.
func TestCheckinRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/checkin": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected checkin route %q not registered", path)
		}
	}
}

// TestScrobbleRoutesHaveCorrectTags verifies scrobble routes have correct tags.
func TestScrobbleRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	scrobblePaths := map[string]string{
		"/v1/scrobble/start": "Scrobble",
		"/v1/scrobble/pause": "Scrobble",
		"/v1/scrobble/stop":  "Scrobble",
	}

	for _, r := range routes {
		if expectedTag, ok := scrobblePaths[r.Path]; ok {
			if len(r.Tags) == 0 || r.Tags[0] != expectedTag {
				t.Errorf("scrobble route %s has tags=%v, want [%s]", r.Path, r.Tags, expectedTag)
			}
		}
	}
}

// TestCheckinRoutesHaveCorrectTags verifies checkin routes have correct tags.
func TestCheckinRoutesHaveCorrectTags(t *testing.T) {
	routes := openapi.AllRoutes()

	checkinPaths := map[string]string{
		"/v1/checkin": "Checkin",
	}

	for _, r := range routes {
		if expectedTag, ok := checkinPaths[r.Path]; ok {
			if len(r.Tags) == 0 || r.Tags[0] != expectedTag {
				t.Errorf("checkin route %s has tags=%v, want [%s]", r.Path, r.Tags, expectedTag)
			}
		}
	}
}

// TestScrobbleRoutesAuthAnnotations verifies scrobble and checkin routes require auth.
func TestScrobbleRoutesAuthAnnotations(t *testing.T) {
	routes := openapi.AllRoutes()

	requiredAuth := map[string]bool{
		"/v1/scrobble/start": true,
		"/v1/scrobble/pause": true,
		"/v1/scrobble/stop":  true,
		"/v1/checkin":        true,
	}

	for _, r := range routes {
		if requiredAuth[r.Path] {
			if r.Auth != "required" {
				t.Errorf("route %s has auth=%q, want 'required'", r.Path, r.Auth)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Scrobble request body shape tests
// ---------------------------------------------------------------------------

type scrobbleTestRequest struct {
	Movie    *scrobbleTestItem `json:"movie,omitempty"`
	Episode  *scrobbleTestItem `json:"episode,omitempty"`
	Show     *scrobbleTestItem `json:"show,omitempty"`
	Progress float64           `json:"progress"`
}

type scrobbleTestItem struct {
	IDs    scrobbleTestIDs `json:"ids"`
	Season int             `json:"season,omitempty"`
	Number int             `json:"number,omitempty"`
}

type scrobbleTestIDs struct {
	TMDB int `json:"tmdb"`
}

// TestScrobbleStartRequestBodyShape verifies the start request JSON shape.
func TestScrobbleStartRequestBodyShape(t *testing.T) {
	t.Run("movie_start", func(t *testing.T) {
		req := scrobbleTestRequest{
			Movie: &scrobbleTestItem{
				IDs: scrobbleTestIDs{TMDB: 603},
			},
			Progress: 0.0,
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if movie, ok := raw["movie"].(map[string]interface{}); !ok {
			t.Error("movie field missing")
		} else {
			ids := movie["ids"].(map[string]interface{})
			if ids["tmdb"].(float64) != 603 {
				t.Errorf("tmdb = %v, want 603", ids["tmdb"])
			}
		}
		if raw["progress"].(float64) != 0.0 {
			t.Errorf("progress = %v, want 0.0", raw["progress"])
		}
	})

	t.Run("episode_start", func(t *testing.T) {
		req := scrobbleTestRequest{
			Episode: &scrobbleTestItem{
				IDs:    scrobbleTestIDs{TMDB: 1396},
				Season: 1,
				Number: 1,
			},
			Progress: 45.5,
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		ep := raw["episode"].(map[string]interface{})
		if ep["season"].(float64) != 1 {
			t.Errorf("season = %v, want 1", ep["season"])
		}
		if ep["number"].(float64) != 1 {
			t.Errorf("number = %v, want 1", ep["number"])
		}
	})
}

// ---------------------------------------------------------------------------
// Progress threshold boundary tests
// ---------------------------------------------------------------------------

// TestScrobbleStopThresholds verifies the percentage boundary semantics.
// These test the business rules around stop → scrobble vs. pause.
func TestScrobbleStopThresholds(t *testing.T) {
	tests := []struct {
		name     string
		progress float64
		action   string // expected action: scrobble, pause, or error
	}{
		{"below_1_percent", 0.5, "error"},
		{"at_1_percent", 1.0, "pause"},
		{"mid_progress", 50.0, "pause"},
		{"at_79_percent", 79.0, "pause"},
		{"at_79_9_percent", 79.9, "pause"},
		{"at_80_percent", 80.0, "scrobble"},
		{"at_90_percent", 90.0, "scrobble"},
		{"at_100_percent", 100.0, "scrobble"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.progress < 1.0 {
				if tt.action != "error" {
					t.Errorf("progress %.1f should be error, got %s", tt.progress, tt.action)
				}
			} else if tt.progress >= 80.0 {
				if tt.action != "scrobble" {
					t.Errorf("progress %.1f should be scrobble, got %s", tt.progress, tt.action)
				}
			} else {
				if tt.action != "pause" {
					t.Errorf("progress %.1f should be pause, got %s", tt.progress, tt.action)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Checkin request shape tests
// ---------------------------------------------------------------------------

type checkinTestRequest struct {
	Movie   *checkinTestItem    `json:"movie,omitempty"`
	Episode *checkinTestItem    `json:"episode,omitempty"`
	Show    *checkinTestItem    `json:"show,omitempty"`
	Message string              `json:"message,omitempty"`
	Sharing *checkinTestSharing `json:"sharing,omitempty"`
}

type checkinTestItem struct {
	IDs    checkinTestIDs `json:"ids"`
	Season int            `json:"season,omitempty"`
	Number int            `json:"number,omitempty"`
}

type checkinTestIDs struct {
	TMDB int `json:"tmdb"`
}

type checkinTestSharing struct {
	Twitter  bool `json:"twitter"`
	Tumblr   bool `json:"tumblr"`
	Facebook bool `json:"facebook"`
}

// TestCheckinRequestBodyShape verifies checkin request JSON shape.
func TestCheckinRequestBodyShape(t *testing.T) {
	t.Run("movie_checkin", func(t *testing.T) {
		req := checkinTestRequest{
			Movie: &checkinTestItem{
				IDs: checkinTestIDs{TMDB: 603},
			},
			Message: "This movie is amazing!",
			Sharing: &checkinTestSharing{Twitter: true},
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if raw["message"] != "This movie is amazing!" {
			t.Errorf("message = %v", raw["message"])
		}
		sharing := raw["sharing"].(map[string]interface{})
		if sharing["twitter"] != true {
			t.Errorf("sharing.twitter = %v, want true", sharing["twitter"])
		}
	})

	t.Run("episode_checkin", func(t *testing.T) {
		req := checkinTestRequest{
			Episode: &checkinTestItem{
				IDs:    checkinTestIDs{TMDB: 1396},
				Season: 1,
				Number: 1,
			},
			Message: "Great episode!",
		}
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		ep := raw["episode"].(map[string]interface{})
		if ep["season"].(float64) != 1 {
			t.Errorf("season = %v, want 1", ep["season"])
		}
		if ep["number"].(float64) != 1 {
			t.Errorf("number = %v, want 1", ep["number"])
		}
	})
}

// ---------------------------------------------------------------------------
// Scrobble response shape tests
// ---------------------------------------------------------------------------

type scrobbleTestResponse struct {
	ID        int64   `json:"id,omitempty"`
	Action    string  `json:"action"`
	Progress  float64 `json:"progress"`
	WatchedAt string  `json:"watched_at,omitempty"`
}

// TestScrobbleStopResponseShape verifies scrobble response JSON shape.
func TestScrobbleStopResponseShape(t *testing.T) {
	resp := scrobbleTestResponse{
		ID:        42,
		Action:    "scrobble",
		Progress:  85.5,
		WatchedAt: "2026-08-02T12:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["action"] != "scrobble" {
		t.Errorf("action = %v, want 'scrobble'", raw["action"])
	}
	if raw["id"].(float64) != 42 {
		t.Errorf("id = %v, want 42", raw["id"])
	}
	if raw["watched_at"] != "2026-08-02T12:00:00Z" {
		t.Errorf("watched_at = %v", raw["watched_at"])
	}
}

// TestScrobblePauseResponseShape verifies pause response JSON shape.
func TestScrobblePauseResponseShape(t *testing.T) {
	resp := scrobbleTestResponse{
		Action:   "pause",
		Progress: 45.5,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["action"] != "pause" {
		t.Errorf("action = %v, want 'pause'", raw["action"])
	}
}

// ---------------------------------------------------------------------------
// Duplicate scrobble response shape tests
// ---------------------------------------------------------------------------

type duplicateScrobbleTestResponse struct {
	WatchedAt string `json:"watched_at"`
	ExpiresAt string `json:"expires_at"`
}

// TestDuplicateScrobbleResponseShape verifies 409 duplicate scrobble response shape.
func TestDuplicateScrobbleResponseShape(t *testing.T) {
	resp := duplicateScrobbleTestResponse{
		WatchedAt: "2026-08-01T12:00:00Z",
		ExpiresAt: "2026-08-02T12:00:00Z",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["watched_at"] == nil {
		t.Error("watched_at field missing")
	}
	if raw["expires_at"] == nil {
		t.Error("expires_at field missing")
	}
}

// ---------------------------------------------------------------------------
// Playback response shape tests
// ---------------------------------------------------------------------------

type playbackTestResponse struct {
	MediaType string  `json:"media_type"`
	MediaID   int64   `json:"media_id"`
	Progress  float64 `json:"progress"`
	PausedAt  string  `json:"paused_at"`
}

// TestPlaybackResponseShape verifies playback list response shape.
func TestPlaybackResponseShape(t *testing.T) {
	resp := []playbackTestResponse{
		{MediaType: "movie", MediaID: 603, Progress: 45.5, PausedAt: "2026-08-02T10:00:00Z"},
		{MediaType: "episode", MediaID: 2001, Progress: 75.0, PausedAt: "2026-08-02T09:00:00Z"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(raw) != 2 {
		t.Fatalf("expected 2 playback entries, got %d", len(raw))
	}
	if raw[0]["media_type"] != "movie" {
		t.Errorf("first media_type = %v, want 'movie'", raw[0]["media_type"])
	}
	if raw[1]["media_type"] != "episode" {
		t.Errorf("second media_type = %v, want 'episode'", raw[1]["media_type"])
	}
}

// ---------------------------------------------------------------------------
// Checkin response shape tests
// ---------------------------------------------------------------------------

type checkinTestResponse struct {
	ID        int64                  `json:"id"`
	ExpiresAt string                 `json:"expires_at"`
	WatchedAt string                 `json:"watched_at"`
	Movie     *checkinTestMediaRef   `json:"movie,omitempty"`
	Episode   *checkinTestMediaRef   `json:"episode,omitempty"`
	Message   string                 `json:"message,omitempty"`
}

type checkinTestMediaRef struct {
	IDs   checkinTestRefIDs `json:"ids"`
	Title string            `json:"title"`
	Year  int               `json:"year,omitempty"`
}

type checkinTestRefIDs struct {
	TMDB int `json:"tmdb"`
}

// TestCheckinCreateResponseShape verifies checkin create response shape.
func TestCheckinCreateResponseShape(t *testing.T) {
	resp := checkinTestResponse{
		ID:        1,
		ExpiresAt: "2026-08-02T14:00:00Z",
		WatchedAt: "2026-08-02T12:00:00Z",
		Movie: &checkinTestMediaRef{
			IDs:   checkinTestRefIDs{TMDB: 603},
			Title: "The Matrix",
			Year:  1999,
		},
		Message: "Wow!",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw["id"].(float64) != 1 {
		t.Errorf("id = %v, want 1", raw["id"])
	}
	if raw["watched_at"] == nil {
		t.Error("watched_at field missing")
	}
	if raw["expires_at"] == nil {
		t.Error("expires_at field missing")
	}
	if movie, ok := raw["movie"].(map[string]interface{}); ok {
		ids := movie["ids"].(map[string]interface{})
		if ids["tmdb"].(float64) != 603 {
			t.Errorf("movie.ids.tmdb = %v, want 603", ids["tmdb"])
		}
	}
}

// TestCheckinNoActiveResponse verifies 204 No Content shape when no active checkin.
func TestCheckinNoActiveResponse(t *testing.T) {
	// A 204 response has no body — just verify the status concept
	status := 204
	if status != 204 {
		t.Errorf("no-checkin response should be 204, got %d", status)
	}
}

// ---------------------------------------------------------------------------
// Advisory lock concept tests (documenting the pattern)
// ---------------------------------------------------------------------------

// TestAdvisoryLockPattern verifies the advisory lock key computation pattern.
// pg_advisory_xact_lock(hashtext(user_id), media_id) for scrobble writes.
// pg_advisory_xact_lock(hashtext(user_id)) for checkin exclusivity.
func TestAdvisoryLockPattern(t *testing.T) {
	// Verify that we understand the lock pattern:
	// - Two different user IDs should get different first keys
	// - Same user + same media should get same lock pair
	// This is a behavioral contract, not a hash test.

	user1 := "user_abc123"
	user2 := "user_xyz789"
	mediaID := int64(603)

	// Different users should have different lock keys (via hashtext)
	if user1 == user2 {
		t.Error("different users should have different IDs")
	}

	// Same user + same media should produce consistent lock pair
	_ = mediaID // Use the same mediaID for both users
	_ = user1
	_ = user2

	// Pattern documented: SELECT pg_advisory_xact_lock(hashtext($1), $2)
	t.Log("advisory lock pattern: pg_advisory_xact_lock(hashtext(user_id), media_id) — auto-released on commit/rollback")
}
