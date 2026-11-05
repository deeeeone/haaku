package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRestoreLoadsACompleteBackup is the restore-wiring test: the dump the
// backup wrote is exactly what the restorer reads, and the manifest's inventory
// is checked against the database afterwards.
func TestRestoreLoadsACompleteBackup(t *testing.T) {
	dest := newMemDestination()
	dumper := goodDumper()
	restorer := &fakeRestorer{}
	svc, _ := testService(t, dest, dumper, restorer)

	summary, err := svc.Backup(context.Background(), KindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	result, err := svc.Restore(context.Background(), summary.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	calls, got := restorer.restored()
	if calls != 1 {
		t.Fatalf("restorer called %d times, want 1", calls)
	}
	if string(got) != string(dumper.data) {
		t.Fatalf("restorer read %q, want the dump %q", got, dumper.data)
	}
	if !result.Verified || !result.InventoryChecked {
		t.Fatalf("restore result = %+v, want verified with the inventory checked", result)
	}
}

// TestRestoreRefusesAnIncompleteBackup: a folder without a manifest is never
// restored from.
func TestRestoreRefusesAnIncompleteBackup(t *testing.T) {
	dest := newMemDestination()
	svc, clock := testService(t, dest, goodDumper(), &fakeRestorer{})

	id := FolderName(clock.now(), KindManual)
	if _, err := dest.Put(context.Background(), folderPrefix(DefaultServerID, id)+DumpName, strings.NewReader("a dump with no manifest")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := svc.Restore(context.Background(), id); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Restore error = %v, want %v", err, ErrIncomplete)
	}
}

// TestRestoreRefusesATamperedDump: the dump is verified against the manifest's
// size and checksum before pg_restore sees it.
func TestRestoreRefusesATamperedDump(t *testing.T) {
	dest := newMemDestination()
	dumper := goodDumper()
	restorer := &fakeRestorer{}
	svc, _ := testService(t, dest, dumper, restorer)

	summary, err := svc.Backup(context.Background(), KindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	key := folderPrefix(DefaultServerID, summary.ID) + DumpName
	// Same length, different bytes: the size check passes and the checksum
	// check fails.
	dest.overwrite(key, []byte(strings.Repeat("Z", len(dumper.data))))
	if _, err := svc.Restore(context.Background(), summary.ID); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Restore error = %v, want %v", err, ErrUnreadable)
	}
	if calls, _ := restorer.restored(); calls != 0 {
		t.Fatalf("restorer ran %d times on a tampered dump", calls)
	}
}

// TestRestoreRefusesAnObjectAtTheWrongSize: a folder whose manifest lists an
// object at a size it does not have is incomplete, not a valid backup.
func TestRestoreRefusesAnObjectAtTheWrongSize(t *testing.T) {
	dest := newMemDestination()
	svc, _ := testService(t, dest, goodDumper(), &fakeRestorer{})
	summary, err := svc.Backup(context.Background(), KindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	dest.overwrite(folderPrefix(DefaultServerID, summary.ID)+DumpName, []byte("short"))
	if _, err := svc.Restore(context.Background(), summary.ID); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("Restore error = %v, want %v", err, ErrIncomplete)
	}
}

// TestRestoreRefusesAnUnknownID.
func TestRestoreRefusesAnUnknownID(t *testing.T) {
	svc, _ := testService(t, newMemDestination(), goodDumper(), &fakeRestorer{})
	if _, err := svc.Restore(context.Background(), "20260101T000000.000Z-manual"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Restore error = %v, want %v", err, ErrNotFound)
	}
	if _, err := svc.Restore(context.Background(), "not-a-backup"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Restore error = %v, want %v", err, ErrNotFound)
	}
}

// TestRestoreFailsWhenInventoryDisagrees: the post-restore verification refuses
// a database that does not match the manifest.
func TestRestoreFailsWhenInventoryDisagrees(t *testing.T) {
	dest := newMemDestination()
	dumper := goodDumper()
	svc, _ := testService(t, dest, dumper, &fakeRestorer{})
	summary, err := svc.Backup(context.Background(), KindManual)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	// The database now has no users table: the restore did not bring it back.
	dumper.tables = []TableCount{{Name: "users", Present: false}}
	if _, err := svc.Restore(context.Background(), summary.ID); !errors.Is(err, ErrRestoreVerificationFailed) {
		t.Fatalf("Restore error = %v, want %v", err, ErrRestoreVerificationFailed)
	}
}
