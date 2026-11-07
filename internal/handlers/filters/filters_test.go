package filters

import (
	"errors"
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
)

func TestValidSectionsAllowlist(t *testing.T) {
	allowed := []string{
		"calendar", "progress_watched", "progress_collected",
		"recommendations", "comments", "dropped",
	}
	for _, s := range allowed {
		if !validSections[s] {
			t.Errorf("validSections missing %q", s)
		}
	}
	for _, s := range []string{"", "movies", "shows", "updates"} {
		if validSections[s] {
			t.Errorf("validSections unexpectedly accepts %q", s)
		}
	}
}

func TestErrFilterLimitReachedSentinel(t *testing.T) {
	if !errors.Is(errFilterLimitReached, errFilterLimitReached) {
		t.Fatal("errFilterLimitReached must be errors.Is-comparable")
	}
	if errFilterLimitReached.Error() != "saved filter limit reached" {
		t.Errorf("sentinel message = %q", errFilterLimitReached.Error())
	}
}

func TestToFilterResponse(t *testing.T) {
	created := time.Date(2026, 3, 3, 3, 3, 3, 0, time.UTC)
	f := &database.SavedFilter{
		ID:        7,
		UserID:    "u",
		Section:   "movies",
		Name:      "My Filter",
		Filter:    database.JSONMap{"genre": "action"},
		CreatedAt: created,
	}
	got := toFilterResponse(f)
	if got.ID != 7 || got.UserID != "u" || got.Section != "movies" || got.Name != "My Filter" {
		t.Errorf("filter base wrong: %+v", got)
	}
	if got.Filter["genre"] != "action" {
		t.Errorf("Filter = %v", got.Filter)
	}
	if got.CreatedAt != "2026-03-03T03:03:03.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}
