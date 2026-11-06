package tests

import (
	"testing"

	"trakt-api/internal/database"
)

// ---------------------------------------------------------------------------
// AdvisoryLockKeysInt64 tests
// ---------------------------------------------------------------------------

func TestAdvisoryLockKeysInt64_Positive(t *testing.T) {
	k1, k2 := database.AdvisoryLockKeysInt64(42, 7)
	if k1 == 0 && k2 == 0 {
		t.Error("expected non-zero keys for positive inputs")
	}
	// Keys should be deterministic.
	k1b, k2b := database.AdvisoryLockKeysInt64(42, 7)
	if k1 != k1b || k2 != k2b {
		t.Errorf("AdvisoryLockKeysInt64 should be deterministic: got (%d,%d) and (%d,%d)", k1, k2, k1b, k2b)
	}
}

func TestAdvisoryLockKeysInt64_Negative(t *testing.T) {
	k1, k2 := database.AdvisoryLockKeysInt64(-42, -7)
	if k1 == 0 && k2 == 0 {
		t.Error("expected non-zero keys for negative inputs")
	}
	// Negative values should produce same keys as positive (abs used).
	k1p, k2p := database.AdvisoryLockKeysInt64(42, 7)
	if k1 != k1p || k2 != k2p {
		t.Errorf("negative keys should match positive: got (%d,%d) vs pos (%d,%d)", k1, k2, k1p, k2p)
	}
}

func TestAdvisoryLockKeysInt64_Zero(t *testing.T) {
	k1, k2 := database.AdvisoryLockKeysInt64(0, 0)
	if k1 != 0 || k2 != 0 {
		t.Errorf("zero keys should be (0,0): got (%d,%d)", k1, k2)
	}
}

func TestAdvisoryLockKeysInt64_LargeValues(t *testing.T) {
	k1, k2 := database.AdvisoryLockKeysInt64(9223372036854775807, 9223372036854775807)
	if k1 == 0 && k2 == 0 {
		t.Error("expected non-zero keys for large values")
	}
}

// ---------------------------------------------------------------------------
// AdvisoryLockKeysStr tests
// ---------------------------------------------------------------------------

func TestAdvisoryLockKeysStr_Deterministic(t *testing.T) {
	s := "user_history:user_123"
	k1, k2 := database.AdvisoryLockKeysStr(s)
	k1b, k2b := database.AdvisoryLockKeysStr(s)
	if k1 != k1b || k2 != k2b {
		t.Errorf("AdvisoryLockKeysStr should be deterministic: got (%d,%d) and (%d,%d)", k1, k2, k1b, k2b)
	}
}

func TestAdvisoryLockKeysStr_DifferentStrings(t *testing.T) {
	k1a, k2a := database.AdvisoryLockKeysStr("user_123")
	k1b, k2b := database.AdvisoryLockKeysStr("user_456")
	if k1a == k1b && k2a == k2b {
		t.Error("different strings should produce different keys")
	}
}

func TestAdvisoryLockKeysStr_Empty(t *testing.T) {
	k1, k2 := database.AdvisoryLockKeysStr("")

	// The empty string is a legal lock key. FNV-1a("") is a fixed non-zero value,
	// so the pair must be non-zero, deterministic, and distinct from another key.
	if k1 == 0 && k2 == 0 {
		t.Error("empty string produced an all-zero key pair")
	}
	again1, again2 := database.AdvisoryLockKeysStr("")
	if k1 != again1 || k2 != again2 {
		t.Errorf("keys for \"\" are not deterministic: (%d,%d) then (%d,%d)", k1, k2, again1, again2)
	}
	other1, other2 := database.AdvisoryLockKeysStr("x")
	if k1 == other1 && k2 == other2 {
		t.Error("keys for \"\" and \"x\" collide")
	}
}

func TestAdvisoryLockKeysStr_SimilarStrings(t *testing.T) {
	k1a, k2a := database.AdvisoryLockKeysStr("user:1:movies")
	k1b, k2b := database.AdvisoryLockKeysStr("user:1:shows")
	if k1a == k1b && k2a == k2b {
		t.Error("similar but different strings should produce different keys")
	}
}

// ---------------------------------------------------------------------------
// WithAdvisoryLock smoke tests (no DB required — verify function accepts valid args)
// ---------------------------------------------------------------------------

func TestAdvisoryLockKeysInt64_DifferentArgs(t *testing.T) {
	// Ensure different inputs produce different pairs.
	pairs := make(map[[2]uint32]bool)
	for i := int64(0); i < 100; i++ {
		k1, k2 := database.AdvisoryLockKeysInt64(i, i*2)
		key := [2]uint32{k1, k2}
		if pairs[key] {
			t.Errorf("AdvisoryLockKeysInt64 produced duplicate for (%d,%d): (%d,%d)", i, i*2, k1, k2)
		}
		pairs[key] = true
	}
}

// ---------------------------------------------------------------------------
// UserStats model tests
// ---------------------------------------------------------------------------

func TestUserStatsModel_TableName(t *testing.T) {
	s := database.UserStats{}
	if s.TableName() != "user_stats" {
		t.Errorf("UserStats.TableName() = %q, want %q", s.TableName(), "user_stats")
	}
}

func TestUserStatsModel_Defaults(t *testing.T) {
	s := database.UserStats{UserID: "user_1"}
	if s.Plays != 0 || s.Minutes != 0 {
		t.Error("UserStats should default to zero counts")
	}
	if s.UniqueMovies != 0 || s.UniqueShows != 0 || s.UniqueEpisodes != 0 {
		t.Error("UserStats should default to zero unique counts")
	}
}

// ---------------------------------------------------------------------------
// SyncSession model tests
// ---------------------------------------------------------------------------

func TestSyncSessionModel_TableName(t *testing.T) {
	s := database.SyncSession{}
	if s.TableName() != "sync_sessions" {
		t.Errorf("SyncSession.TableName() = %q, want %q", s.TableName(), "sync_sessions")
	}
}

func TestSyncSessionModel_Defaults(t *testing.T) {
	s := database.SyncSession{Kind: "history"}
	// GORM default values ("pending") are applied at DB level via tags,
	// not in Go struct initialization. The zero value for string is "".
	if s.Kind != "history" {
		t.Errorf("SyncSession.Kind = %q, want %q", s.Kind, "history")
	}
	if s.TotalItems != 0 {
		t.Errorf("SyncSession.TotalItems default = %d, want 0", s.TotalItems)
	}
}

// ---------------------------------------------------------------------------
// UserHidden model tests
// ---------------------------------------------------------------------------

func TestUserHiddenModel_TableName(t *testing.T) {
	h := database.UserHidden{}
	if h.TableName() != "user_hidden" {
		t.Errorf("UserHidden.TableName() = %q, want %q", h.TableName(), "user_hidden")
	}
}

func TestUserHiddenModel_Fields(t *testing.T) {
	h := database.UserHidden{
		UserID:    "user_1",
		Section:   "calendar",
		MediaType: "movie",
		MediaID:   42,
	}
	if h.UserID != "user_1" || h.Section != "calendar" {
		t.Error("UserHidden fields not set correctly")
	}
}
