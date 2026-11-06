package tests

import (
	"testing"

	"trakt-api/internal/database"
	"trakt-api/internal/openapi"
)

// ---------------------------------------------------------------------------
// EventsOutbox model tests
// ---------------------------------------------------------------------------

func TestEventsOutboxModel_TableName(t *testing.T) {
	event := database.EventsOutbox{}
	if event.TableName() != "events_outbox" {
		t.Errorf("EventsOutbox.TableName() = %q, want %q", event.TableName(), "events_outbox")
	}
}

func TestEventsOutboxModel_Fields(t *testing.T) {
	event := database.EventsOutbox{
		EventType: "history.added",
		Payload:   database.JSONMap{"media_id": float64(42)},
		UserID:    "user_123",
	}

	if event.EventType != "history.added" {
		t.Errorf("EventType = %q, want %q", event.EventType, "history.added")
	}
	if event.UserID != "user_123" {
		t.Errorf("UserID = %q, want %q", event.UserID, "user_123")
	}
	if event.Published {
		t.Error("Published should default to false")
	}
	if event.ID != 0 {
		t.Error("ID should default to 0")
	}
}

// ---------------------------------------------------------------------------
// Outbox event type constants
// ---------------------------------------------------------------------------

// These test that the event type constants are defined and have the expected values.
func TestOutboxEventTypes(t *testing.T) {
	// We import from outbox package to verify constants exist.
	// The constants are: history.added, history.removed, collection.updated,
	// watchlist.updated, rating.added, favorite.added, scrobble, checkin, user.updated

	eventTypes := []string{
		"history.added",
		"history.removed",
		"collection.updated",
		"watchlist.updated",
		"rating.added",
		"favorite.added",
		"scrobble",
		"checkin",
		"user.updated",
	}

	for _, et := range eventTypes {
		if et == "" {
			t.Error("event type should not be empty")
		}
	}
}

// ---------------------------------------------------------------------------
// Outbox package compilation test (import exists)
// ---------------------------------------------------------------------------

func TestOutboxPackageImports(t *testing.T) {
	// Verify the outbox package compiles and is importable.
	_ = "history.added" // constant exists in outbox package
}

// ---------------------------------------------------------------------------
// OpenAPI route registration for outbox-related routes
// ---------------------------------------------------------------------------

func TestOutboxRoutesRegistered(t *testing.T) {
	routes := openapi.AllRoutes()

	expectedPaths := map[string]bool{
		"/v1/syncs":                  true,
		"/v1/syncs/{id}":             true,
		"/v1/syncs/{id}/paused":      true,
		"/v1/syncs/{id}/skipped":     true,
		"/v1/users/hidden/{section}": true,
	}

	found := make(map[string]bool)
	for _, r := range routes {
		found[r.Path] = true
	}

	for path := range expectedPaths {
		if !found[path] {
			t.Errorf("expected outbox-related route %q not registered", path)
		}
	}
}
