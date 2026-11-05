package backup

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRotationKeepsNewestN: rotation keeps the newest complete backups and
// deletes the rest, newest-first.
func TestRotationKeepsNewestN(t *testing.T) {
	dest := newMemDestination()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dest.now = clock.now

	o := baseOptions(dest, clock, goodDumper())
	o.Keep = 10 // keep everything while we build the set
	maker, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var created []string
	for range 5 {
		s, err := maker.Backup(context.Background(), KindManual)
		if err != nil {
			t.Fatalf("Backup: %v", err)
		}
		created = append(created, s.ID)
		clock.advance(time.Minute)
	}

	o.Keep = 2
	rotator, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	deleted, err := rotator.Rotate(context.Background())
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(deleted) != 3 {
		t.Fatalf("deleted %v, want the 3 oldest", deleted)
	}
	wantDeleted := map[string]bool{created[0]: true, created[1]: true, created[2]: true}
	for _, id := range deleted {
		if !wantDeleted[id] {
			t.Fatalf("deleted %s, which is not among the 3 oldest", id)
		}
	}
	listing, err := rotator.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 2 {
		t.Fatalf("listing.Complete = %d, want 2", listing.Complete)
	}
	kept := map[string]bool{}
	for _, s := range listing.Backups {
		kept[s.ID] = true
	}
	if !kept[created[4]] || !kept[created[3]] {
		t.Fatalf("rotation kept %v, want the two newest", listing.Backups)
	}
}

// TestRotationDoesNotCountIncomplete: an incomplete folder never consumes a
// retention slot, so a complete backup is never deleted in its place, and an
// incomplete folder is not deleted while it is fresh.
func TestRotationDoesNotCountIncomplete(t *testing.T) {
	dest := newMemDestination()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dest.now = clock.now
	o := baseOptions(dest, clock, goodDumper())

	svc, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for range 3 {
		if _, err := svc.Backup(context.Background(), KindManual); err != nil {
			t.Fatalf("Backup: %v", err)
		}
		clock.advance(time.Minute)
	}
	// An incomplete folder, newer than nothing in particular.
	incID := FolderName(clock.now(), KindManual)
	if _, err := dest.Put(context.Background(), folderPrefix(DefaultServerID, incID)+DumpName, strings.NewReader("partial")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	clock.advance(time.Minute)

	o.Keep = 2
	rotator, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := rotator.Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	listing, err := rotator.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 2 {
		t.Fatalf("listing.Complete = %d, want 2 (the incomplete folder must not be counted)", listing.Complete)
	}
	if listing.Incomplete != 1 {
		t.Fatalf("listing.Incomplete = %d, want the fresh incomplete folder kept", listing.Incomplete)
	}
}

// TestRotationKeepsTwoNewestBeforeUpgrade: pre-migration safety copies are kept
// two deep, separately from the ordinary retention count.
func TestRotationKeepsTwoNewestBeforeUpgrade(t *testing.T) {
	dest := newMemDestination()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dest.now = clock.now
	o := baseOptions(dest, clock, goodDumper())
	o.Keep = 5
	svc, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var created []string
	for range 4 {
		s, err := svc.BeforeUpgrade(context.Background())
		if err != nil {
			t.Fatalf("BeforeUpgrade: %v", err)
		}
		if s.Kind != KindBeforeUpgrade {
			t.Fatalf("BeforeUpgrade kind = %q", s.Kind)
		}
		created = append(created, s.ID)
		clock.advance(time.Minute)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != beforeUpgradeKeep {
		t.Fatalf("listing.Complete = %d, want %d", listing.Complete, beforeUpgradeKeep)
	}
	kept := map[string]bool{}
	for _, s := range listing.Backups {
		kept[s.ID] = true
	}
	if !kept[created[3]] || !kept[created[2]] {
		t.Fatalf("kept %v, want the two newest", listing.Backups)
	}
}
