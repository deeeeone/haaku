package backup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestListingRefusesAnEndlessDestination is the bounded-listing test: a
// destination that pages without end must be refused, not read until memory runs
// out.
func TestListingRefusesAnEndlessDestination(t *testing.T) {
	svc, _ := testService(t, newEndlessDestination(), goodDumper(), &fakeRestorer{})
	if _, err := svc.List(context.Background()); !errors.Is(err, ErrListingTooLarge) {
		t.Fatalf("List error = %v, want %v", err, ErrListingTooLarge)
	}
}

// TestListingCountsCompleteAndIncomplete: the listing reports a folder without a
// manifest as incomplete and a committed one as complete.
func TestListingCountsCompleteAndIncomplete(t *testing.T) {
	dest := newMemDestination()
	svc, _ := testService(t, dest, goodDumper(), &fakeRestorer{})
	if _, err := svc.Backup(context.Background(), KindManual); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	// A folder with a dump but no manifest, as an abandoned run leaves.
	id := FolderName(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), KindManual)
	if _, err := dest.Put(context.Background(), folderPrefix(DefaultServerID, id)+DumpName, bytes.NewReader([]byte("partial"))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 1 || listing.Incomplete != 1 {
		t.Fatalf("listing = %d complete, %d incomplete; want 1, 1", listing.Complete, listing.Incomplete)
	}
}

// TestListingReportsAnUnreadableManifest: a manifest that names an object the
// folder does not hold is incomplete, and one that cannot be read at all is
// unreadable; neither is a valid backup.
func TestListingReportsUnreadableManifest(t *testing.T) {
	dest := newMemDestination()
	svc, _ := testService(t, dest, goodDumper(), &fakeRestorer{})
	id := FolderName(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), KindManual)
	prefix := folderPrefix(DefaultServerID, id)
	if _, err := dest.Put(context.Background(), prefix+ManifestName, strings.NewReader("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Unreadable != 1 {
		t.Fatalf("listing.Unreadable = %d, want 1", listing.Unreadable)
	}
	// An unreadable folder is never restored from.
	if _, err := svc.Restore(context.Background(), id); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Restore error = %v, want %v", err, ErrUnreadable)
	}
}
