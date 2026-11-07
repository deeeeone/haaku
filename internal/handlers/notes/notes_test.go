package notes

import (
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
)

func TestValidMediaTypes(t *testing.T) {
	allowed := []string{"movie", "show", "season", "episode"}
	for _, m := range allowed {
		if !validMediaTypes[m] {
			t.Errorf("validMediaTypes missing %q", m)
		}
	}
	for _, m := range []string{"", "person", "list"} {
		if validMediaTypes[m] {
			t.Errorf("validMediaTypes unexpectedly accepts %q", m)
		}
	}
}

func TestMaxNoteLength(t *testing.T) {
	if maxNoteLength != 500 {
		t.Errorf("maxNoteLength = %d, want 500", maxNoteLength)
	}
}

func TestToNoteResponse(t *testing.T) {
	created := time.Date(2026, 2, 2, 2, 2, 2, 0, time.UTC)
	n := &database.UserNote{
		ID:        9,
		UserID:    "u",
		MediaType: "movie",
		MediaID:   5,
		Note:      "my note",
		CreatedAt: created,
		UpdatedAt: created,
	}
	got := toNoteResponse(n)
	if got.ID != 9 || got.UserID != "u" || got.MediaType != "movie" || got.MediaID != 5 {
		t.Errorf("note base wrong: %+v", got)
	}
	if got.Note != "my note" {
		t.Errorf("note body = %q", got.Note)
	}
	if got.CreatedAt != "2026-02-02T02:02:02.000Z" || got.UpdatedAt != "2026-02-02T02:02:02.000Z" {
		t.Errorf("timestamps wrong: %+v", got)
	}
}
