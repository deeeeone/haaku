package updates

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpdatesFeedCacheKey(t *testing.T) {
	date := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := updatesFeedCacheKey("movie", date, 2, 20, "")
	want := "updates:movie:2026-05-10:page2:limit20"
	if got != want {
		t.Errorf("cache key without extended = %q, want %q", got, want)
	}

	gotExt := updatesFeedCacheKey("movie", date, 2, 20, "full")
	wantExt := "updates:movie:2026-05-10:page2:limit20:extendedfull"
	if gotExt != wantExt {
		t.Errorf("cache key with extended = %q, want %q", gotExt, wantExt)
	}
}

func TestMustMarshal(t *testing.T) {
	type item struct {
		ID int `json:"id"`
	}
	// Marshalable value serializes normally.
	raw := mustMarshal([]item{{ID: 1}})
	if string(raw) != `[{"id":1}]` {
		t.Errorf("mustMarshal = %s", raw)
	}

	// Unmarshalable value (chan) falls back to "[]" instead of panicking.
	rawBad := mustMarshal(make(chan int))
	if string(rawBad) != "[]" {
		t.Errorf("mustMarshal(unmarshalable) = %s, want []", rawBad)
	}

	// Result must be valid JSON.
	var v []item
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Errorf("unmarshal result: %v", err)
	}
}
