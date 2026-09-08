package activity

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestToActivityResponse(t *testing.T) {
	mid := int64(42)
	created := time.Date(2026, 1, 5, 1, 2, 3, 0, time.UTC)
	a := &database.ActivityFeed{
		ID:        8,
		ActorID:   "user-1",
		EventType: "watch",
		MediaType: "movie",
		MediaID:   &mid,
		Payload:   database.JSONMap{"rating": "9"},
		CreatedAt: created,
	}
	got := toActivityResponse(a)
	if got.ID != 8 || got.ActorID != "user-1" || got.EventType != "watch" || got.MediaType != "movie" {
		t.Errorf("activity base wrong: %+v", got)
	}
	if got.MediaID == nil || *got.MediaID != 42 {
		t.Errorf("MediaID = %v, want 42", got.MediaID)
	}
	if got.Payload["rating"] != "9" {
		t.Errorf("Payload = %v", got.Payload)
	}
	if got.CreatedAt != "2026-01-05T01:02:03.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}

func TestToActivityResponseNilPayload(t *testing.T) {
	a := &database.ActivityFeed{ID: 1, ActorID: "u", EventType: "x", CreatedAt: time.Now()}
	got := toActivityResponse(a)
	// Payload must be non-nil so it serializes as {} not null.
	if got.Payload == nil {
		t.Error("Payload must be a non-nil empty map")
	}
	if len(got.Payload) != 0 {
		t.Errorf("Payload = %v, want empty", got.Payload)
	}
}
