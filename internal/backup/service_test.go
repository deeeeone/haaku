package backup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBackupWritesManifestLast is the commit-point test: the manifest is the
// last object written, so its presence means every object it lists is whole.
func TestBackupWritesManifestLast(t *testing.T) {
	dest := newMemDestination()
	svc, _ := testService(t, dest, goodDumper(), &fakeRestorer{})

	summary, err := svc.Backup(context.Background(), KindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	order := dest.putOrder()
	if len(order) != 2 {
		t.Fatalf("Put order = %v, want [db.dump manifest.json]", order)
	}
	if !strings.HasSuffix(order[0], "/"+DumpName) {
		t.Fatalf("first object written = %q, want %s", order[0], DumpName)
	}
	if !strings.HasSuffix(order[1], "/"+ManifestName) {
		t.Fatalf("last object written = %q, want %s", order[1], ManifestName)
	}
	requireStatus(t, findSummary(t, svc, summary.ID).Status, StatusComplete)
}

// TestBackupFailedDumpLeavesNoBackup: a run whose dump fails writes nothing that
// could be mistaken for a backup.
func TestBackupFailedDumpLeavesNoBackup(t *testing.T) {
	dest := newMemDestination()
	dumper := goodDumper()
	dumper.dumpErr = errBoom
	svc, _ := testService(t, dest, dumper, &fakeRestorer{})

	if _, err := svc.Backup(context.Background(), KindManual); !errors.Is(err, errBoom) {
		t.Fatalf("Backup error = %v, want %v", err, errBoom)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 0 || listing.Incomplete != 0 {
		t.Fatalf("listing = %d complete, %d incomplete; want none", listing.Complete, listing.Incomplete)
	}
}

// TestBackupManifestWriteFailureLeavesIncomplete: a dump that lands but whose
// manifest fails is incomplete, is not counted, and is never rotated over a
// complete one.
func TestBackupManifestWriteFailureLeavesIncomplete(t *testing.T) {
	dest := newMemDestination()
	dest.putErr = func(key string) error {
		if strings.HasSuffix(key, ManifestName) {
			return errBoom
		}
		return nil
	}
	svc, clock := testService(t, dest, goodDumper(), &fakeRestorer{})

	summary, err := svc.Backup(context.Background(), KindManual)
	if err == nil {
		t.Fatalf("Backup succeeded, want a manifest-write failure")
	}
	if summary != nil {
		t.Fatalf("Backup returned a summary for a failed run")
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 0 || listing.Incomplete != 1 {
		t.Fatalf("listing = %d complete, %d incomplete; want 0, 1", listing.Complete, listing.Incomplete)
	}
	// A fresh incomplete folder is not swept.
	deleted, err := svc.Rotate(context.Background())
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("Rotate deleted %v; an incomplete folder is not swept while fresh", deleted)
	}
	// Once clearly abandoned, the sweep removes it.
	clock.advance(2 * time.Hour)
	if deleted, err = svc.Rotate(context.Background()); err != nil || len(deleted) != 1 {
		t.Fatalf("Rotate after grace = %v, %v; want one deletion", deleted, err)
	}
}

// TestBackupRefusesWhenUserDataMissing: a backup must never claim to be complete
// without the user data.
func TestBackupRefusesWhenUserDataMissing(t *testing.T) {
	dest := newMemDestination()
	dumper := goodDumper()
	dumper.tables = []TableCount{{Name: "users", Present: false}}
	svc, _ := testService(t, dest, dumper, &fakeRestorer{})

	_, err := svc.Backup(context.Background(), KindManual)
	if !errors.Is(err, ErrUserDataMissing) {
		t.Fatalf("Backup error = %v, want %v", err, ErrUserDataMissing)
	}
	if strings.Contains(strings.Join(dest.keys(), ","), ManifestName) {
		t.Fatalf("a refused backup wrote a manifest: %v", dest.keys())
	}
}

// TestConcurrentBackupsGetDistinctIDs is the id-reservation test: many runs
// started in the same millisecond serialise under the lock and each reserves its
// own folder. It fails if the lock is removed: two runs would pick the same id.
func TestConcurrentBackupsGetDistinctIDs(t *testing.T) {
	dest := newMemDestination()
	svc, _ := testService(t, dest, goodDumper(), &fakeRestorer{}, func(o *Options) { o.Keep = 100 })

	const runs = 8
	var wg sync.WaitGroup
	ids := make([]string, runs)
	errs := make([]error, runs)
	for i := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			summary, err := svc.Backup(context.Background(), KindManual)
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = summary.ID
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("backup %d failed: %v", i, err)
		}
		if seen[ids[i]] {
			t.Fatalf("folder id %q reserved twice", ids[i])
		}
		seen[ids[i]] = true
	}
	if len(seen) != runs {
		t.Fatalf("got %d distinct ids, want %d", len(seen), runs)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != runs {
		t.Fatalf("listing.Complete = %d, want %d", listing.Complete, runs)
	}
}
