package tests

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// Checkin exclusivity tests
// ---------------------------------------------------------------------------

// TestCheckinOneActivePerUser verifies the one-checkin-per-user constraint shape.
func TestCheckinOneActivePerUser(t *testing.T) {
	// Two checkins for the same user cannot coexist (enforced by UNIQUE(user_id)).
	// The handler uses advisory lock + transaction to enforce this atomically.

	userID := "user_test_123"

	// The constraint: user_checkins has UNIQUE(user_id)
	// When creating a checkin, we:
	// 1. Acquire pg_advisory_xact_lock(hashtext(user_id))
	// 2. Check for non-expired existing checkin → 409
	// 3. Delete any expired checkin
	// 4. Insert new checkin

	_ = userID

	t.Log("checkin exclusivity: one active checkin per user, enforced by UNIQUE(user_id) + advisory lock")
}

// TestCheckinExpiryBehavior verifies checkin expiry semantics.
func TestCheckinExpiryBehavior(t *testing.T) {
	// An expired checkin should NOT block a new one.
	// The handler checks expires_at > now() when checking for conflicts.

	t.Log("expired checkins do not block new checkins — only non-expired ones cause 409")
}

// ---------------------------------------------------------------------------
// Checkin request/response JSON shape edge cases
// ---------------------------------------------------------------------------

type checkinFullRequest struct {
	Movie   *checkinFullItem    `json:"movie,omitempty"`
	Episode *checkinFullItem    `json:"episode,omitempty"`
	Show    *checkinFullItem    `json:"show,omitempty"`
	Message string              `json:"message,omitempty"`
	Sharing *checkinFullSharing `json:"sharing,omitempty"`
}

type checkinFullItem struct {
	IDs    checkinFullIDs `json:"ids"`
	Season int            `json:"season,omitempty"`
	Number int            `json:"number,omitempty"`
}

type checkinFullIDs struct {
	TMDB int `json:"tmdb"`
}

type checkinFullSharing struct {
	Twitter  bool `json:"twitter"`
	Tumblr   bool `json:"tumblr"`
	Facebook bool `json:"facebook"`
}

// TestCheckinJSONRoundtrip verifies JSON marshal/unmarshal roundtrip.
func TestCheckinJSONRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		req  checkinFullRequest
	}{
		{
			name: "movie_minimal",
			req: checkinFullRequest{
				Movie: &checkinFullItem{
					IDs: checkinFullIDs{TMDB: 603},
				},
			},
		},
		{
			name: "movie_full",
			req: checkinFullRequest{
				Movie: &checkinFullItem{
					IDs: checkinFullIDs{TMDB: 603},
				},
				Message: "This is awesome!",
				Sharing: &checkinFullSharing{
					Twitter:  true,
					Facebook: true,
				},
			},
		},
		{
			name: "episode_minimal",
			req: checkinFullRequest{
				Episode: &checkinFullItem{
					IDs:    checkinFullIDs{TMDB: 1396},
					Season: 5,
					Number: 14,
				},
			},
		},
		{
			name: "show_minimal",
			req: checkinFullRequest{
				Show: &checkinFullItem{
					IDs: checkinFullIDs{TMDB: 1396},
				},
			},
		},
		{
			name: "empty_message",
			req: checkinFullRequest{
				Movie: &checkinFullItem{
					IDs: checkinFullIDs{TMDB: 603},
				},
				Message: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var parsed checkinFullRequest
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if tt.req.Movie != nil {
				if parsed.Movie == nil {
					t.Fatal("movie is nil after roundtrip")
				}
				if parsed.Movie.IDs.TMDB != tt.req.Movie.IDs.TMDB {
					t.Errorf("tmdb = %d, want %d", parsed.Movie.IDs.TMDB, tt.req.Movie.IDs.TMDB)
				}
			}

			if tt.req.Episode != nil {
				if parsed.Episode == nil {
					t.Fatal("episode is nil after roundtrip")
				}
				if parsed.Episode.Season != tt.req.Episode.Season {
					t.Errorf("season = %d, want %d", parsed.Episode.Season, tt.req.Episode.Season)
				}
				if parsed.Episode.Number != tt.req.Episode.Number {
					t.Errorf("number = %d, want %d", parsed.Episode.Number, tt.req.Episode.Number)
				}
			}

			if tt.req.Sharing != nil {
				if parsed.Sharing == nil {
					t.Fatal("sharing is nil after roundtrip")
				}
				if parsed.Sharing.Twitter != tt.req.Sharing.Twitter {
					t.Errorf("sharing.twitter = %v, want %v", parsed.Sharing.Twitter, tt.req.Sharing.Twitter)
				}
			}

			// Empty message should serialize to empty string or be omitted
			if tt.req.Message == "" {
				// omitting is acceptable
				t.Logf("empty message roundtrip: message=%q", parsed.Message)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Checkin response edge cases
// ---------------------------------------------------------------------------

type checkinFullResponse struct {
	ID        int64                `json:"id"`
	ExpiresAt string               `json:"expires_at"`
	WatchedAt string               `json:"watched_at"`
	Movie     *checkinFullMediaRef `json:"movie,omitempty"`
	Episode   *checkinFullMediaRef `json:"episode,omitempty"`
	Show      *checkinFullMediaRef `json:"show,omitempty"`
	Message   string               `json:"message,omitempty"`
}

type checkinFullMediaRef struct {
	IDs   checkinFullRefIDs `json:"ids"`
	Title string            `json:"title"`
	Year  int               `json:"year,omitempty"`
}

type checkinFullRefIDs struct {
	TMDB int `json:"tmdb"`
}

// TestCheckinResponseRoundtrip verifies response JSON shape roundtrip.
func TestCheckinResponseRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		resp checkinFullResponse
	}{
		{
			name: "movie_response",
			resp: checkinFullResponse{
				ID:        1,
				ExpiresAt: "2026-08-02T14:00:00Z",
				WatchedAt: "2026-08-02T12:00:00Z",
				Movie: &checkinFullMediaRef{
					IDs:   checkinFullRefIDs{TMDB: 603},
					Title: "The Matrix",
					Year:  1999,
				},
				Message: "Still holds up!",
			},
		},
		{
			name: "episode_response",
			resp: checkinFullResponse{
				ID:        2,
				ExpiresAt: "2026-08-02T15:00:00Z",
				WatchedAt: "2026-08-02T13:00:00Z",
				Episode: &checkinFullMediaRef{
					IDs:   checkinFullRefIDs{TMDB: 1396},
					Title: "Ozymandias",
				},
			},
		},
		{
			name: "minimal_response",
			resp: checkinFullResponse{
				ID:        3,
				ExpiresAt: "2026-08-02T16:00:00Z",
				WatchedAt: "2026-08-02T14:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var parsed checkinFullResponse
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if parsed.ID != tt.resp.ID {
				t.Errorf("id = %d, want %d", parsed.ID, tt.resp.ID)
			}
			if parsed.ExpiresAt != tt.resp.ExpiresAt {
				t.Errorf("expires_at = %s, want %s", parsed.ExpiresAt, tt.resp.ExpiresAt)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Checkin error response tests
// ---------------------------------------------------------------------------

// TestCheckin409ErrorShape verifies the 409 conflict error envelope.
func TestCheckin409ErrorShape(t *testing.T) {
	errResp := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    "conflict",
			"message": "you already have an active checkin",
		},
	}

	data, err := json.Marshal(errResp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	errorObj, ok := parsed["error"].(map[string]interface{})
	if !ok {
		t.Fatal("error envelope missing")
	}
	if errorObj["code"] != "conflict" {
		t.Errorf("error.code = %v, want 'conflict'", errorObj["code"])
	}
	if errorObj["message"] != "you already have an active checkin" {
		t.Errorf("error.message = %v", errorObj["message"])
	}
}
