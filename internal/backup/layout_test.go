package backup

import (
	"testing"
	"time"
)

func TestFolderNameRoundTrip(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)
	name := FolderName(at, KindNightly)
	if name != "20260102T030405.123Z-nightly" {
		t.Fatalf("FolderName = %q", name)
	}
	got, kind, ok := ParseFolderName(name)
	if !ok {
		t.Fatalf("ParseFolderName(%q) did not parse", name)
	}
	if !got.Equal(at) || kind != KindNightly {
		t.Fatalf("ParseFolderName(%q) = %s %q", name, got, kind)
	}
}

func TestParseFolderNameRejectsNonBackups(t *testing.T) {
	for _, name := range []string{
		"",
		"garbage",
		"20260102T030405.123Z-overwritten-blobs",
		"20260102T030405.123Z",
		"not-a-time-nightly",
	} {
		if _, _, ok := ParseFolderName(name); ok {
			t.Errorf("ParseFolderName(%q) parsed, want refusal", name)
		}
	}
}

func TestParseFolderNameAcceptsSeconds(t *testing.T) {
	got, kind, ok := ParseFolderName("20260102T030405Z-manual")
	if !ok || kind != KindManual {
		t.Fatalf("ParseFolderName did not read a seconds-only id: %s %q %v", got, kind, ok)
	}
}

// TestReserveIDMovesOnATakenMillisecond pins the property rotation's ordering
// rests on: a time a folder already has moves on a millisecond, so two runs can
// never choose the same folder id.
func TestReserveIDMovesOnATakenMillisecond(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	folders := []folder{
		{id: FolderName(at, KindNightly), at: at},
		{id: FolderName(at.Add(time.Millisecond), KindManual), at: at.Add(time.Millisecond)},
	}
	got := reserveID(folders, at, KindManual)
	want := FolderName(at.Add(2*time.Millisecond), KindManual)
	if got != want {
		t.Fatalf("reserveID = %q, want %q", got, want)
	}
}

func TestValidServerID(t *testing.T) {
	for _, ok := range []string{"default", "abc-123", "0e5f._x"} {
		if !validServerID(ok) {
			t.Errorf("validServerID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a\\b", "a b"} {
		if validServerID(bad) {
			t.Errorf("validServerID(%q) = true", bad)
		}
	}
}
