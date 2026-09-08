package stats

import (
	"testing"
	"time"
)

func TestIsStale(t *testing.T) {
	if !isStale(nil) {
		t.Error("isStale(nil) = false, want true")
	}
	fresh := time.Now().Add(-time.Minute)
	if isStale(&fresh) {
		t.Error("a 1-minute-old timestamp should not be stale")
	}
	old := time.Now().Add(-2 * time.Hour)
	if !isStale(&old) {
		t.Error("a 2-hour-old timestamp should be stale")
	}
}

func TestStatsVersionSnapshotKey(t *testing.T) {
	got := statsVersionSnapshotKey("user-123")
	if got != "stats:user_version:user-123" {
		t.Errorf("statsVersionSnapshotKey = %q", got)
	}
}
