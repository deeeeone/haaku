package reports

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestValidReportReasonsAllowlist(t *testing.T) {
	allowed := []string{"spam", "spoiler", "offensive", "incorrect", "duplicate", "other"}
	for _, r := range allowed {
		if !validReportReasons[r] {
			t.Errorf("validReportReasons missing %q", r)
		}
	}
	for _, r := range []string{"", "violence", "SPAM"} {
		if validReportReasons[r] {
			t.Errorf("validReportReasons unexpectedly accepts %q", r)
		}
	}
}

func TestMaxReportDescription(t *testing.T) {
	if maxReportDescription != 1000 {
		t.Errorf("maxReportDescription = %d, want 1000", maxReportDescription)
	}
}

func TestToReportResponse(t *testing.T) {
	created := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	r := &database.ContentReport{
		ID:          3,
		ReporterID:  "user-1",
		MediaType:   "movie",
		MediaID:     42,
		Reason:      "spam",
		Description: "dupe",
		CreatedAt:   created,
	}
	got := toReportResponse(r)
	if got.ID != 3 || got.ReporterID != "user-1" || got.MediaType != "movie" || got.MediaID != 42 {
		t.Errorf("report base wrong: %+v", got)
	}
	if got.Reason != "spam" || got.Description != "dupe" {
		t.Errorf("report content wrong: %+v", got)
	}
	if got.CreatedAt != "2026-06-01T10:00:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}
