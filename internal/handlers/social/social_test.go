package social

import (
	"testing"
	"time"

	"trakt-api/internal/database"
)

func TestTotalPages(t *testing.T) {
	cases := []struct {
		total int64
		limit int
		want  int64
	}{
		{0, 10, 0},
		{1, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{25, 10, 3},
		{100, 50, 2},
	}
	for _, tc := range cases {
		if got := totalPages(tc.total, tc.limit); got != tc.want {
			t.Errorf("totalPages(%d, %d) = %d, want %d", tc.total, tc.limit, got, tc.want)
		}
	}
}

func TestBuildFollowResponseApproved(t *testing.T) {
	approved := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &database.UserFollow{
		FollowerID:  "a",
		FollowingID: "b",
		ApprovedAt:  &approved,
		CreatedAt:   created,
	}
	got := buildFollowResponse(f, "approved")
	if got.FollowerID != "a" || got.FollowingID != "b" || got.Status != "approved" {
		t.Errorf("follow base wrong: %+v", got)
	}
	if got.ApprovedAt == nil || *got.ApprovedAt != "2026-01-02T03:04:05.000Z" {
		t.Errorf("ApprovedAt = %v", got.ApprovedAt)
	}
	if got.CreatedAt != "2026-01-01T00:00:00.000Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
}

func TestBuildFollowResponsePending(t *testing.T) {
	f := &database.UserFollow{FollowerID: "a", FollowingID: "b", CreatedAt: time.Now()}
	got := buildFollowResponse(f, "pending")
	if got.ApprovedAt != nil {
		t.Errorf("ApprovedAt = %v, want nil for pending follow", got.ApprovedAt)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want pending", got.Status)
	}
}
