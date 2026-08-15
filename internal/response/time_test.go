package response

import (
	"testing"
	"time"
)

func TestFormatTime(t *testing.T) {
	tm := time.Date(2026, 8, 1, 12, 0, 0, 123456000, time.UTC)
	if got := FormatTime(tm); got != "2026-08-01T12:00:00.123Z" {
		t.Fatalf("FormatTime = %q, want %q", got, "2026-08-01T12:00:00.123Z")
	}
}

func TestFormatTimeTruncatesSubMillisecond(t *testing.T) {
	tm := time.Date(2026, 8, 1, 12, 0, 0, 123456789, time.UTC)
	if got := FormatTime(tm); got != "2026-08-01T12:00:00.123Z" {
		t.Fatalf("FormatTime = %q, want %q", got, "2026-08-01T12:00:00.123Z")
	}
}

func TestFormatTimePreservesLayout(t *testing.T) {
	tm := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if got := FormatTime(tm); got != tm.Format("2006-01-02T15:04:05.000Z") {
		t.Fatalf("FormatTime changed the historical layout: got %q", got)
	}
}
