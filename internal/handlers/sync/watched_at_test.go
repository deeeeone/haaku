package sync

import (
	"testing"
	"time"

	"github.com/haaku-tv/haaku/internal/database"
)

func TestParseWatchedAt(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	aired := database.Date{Time: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)}

	// "released" uses aired date when present.
	if got, err := parseWatchedAt("released", now, &aired); err != nil || !got.Equal(aired.Time) {
		t.Errorf("released with airedAt = %v, %v; want %v", got, err, aired.Time)
	}
	// "released" falls back to now when there's no aired date.
	if got, err := parseWatchedAt("released", now, nil); err != nil || !got.Equal(now) {
		t.Errorf("released without airedAt = %v, %v; want now", got, err)
	}
	// empty / "unknown" map to now.
	for _, v := range []string{"", "unknown"} {
		if got, err := parseWatchedAt(v, now, &aired); err != nil || !got.Equal(now) {
			t.Errorf("%q = %v, %v; want now", v, got, err)
		}
	}
	// explicit valid RFC3339 is parsed verbatim.
	explicit := "2026-08-01T10:30:00Z"
	want, _ := time.Parse(time.RFC3339, explicit)
	if got, err := parseWatchedAt(explicit, now, nil); err != nil || !got.Equal(want) {
		t.Errorf("explicit = %v, %v; want %v", got, err, want)
	}
	// invalid timestamps error instead of silently becoming "now".
	for _, v := range []string{"2026-13-99", "yesterday", "2026-08-01", "12:00"} {
		if got, err := parseWatchedAt(v, now, nil); err == nil {
			t.Errorf("invalid %q = %v, nil err; want error", v, got)
		}
	}
}
