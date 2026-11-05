package backup

import (
	"context"
	"testing"
	"time"
)

// TestPreMigrationCopyIsExposed pins the helper a migration runner calls: one
// call takes a before-upgrade copy without the caller holding a Service, and
// rotation keeps two of them.
func TestPreMigrationCopyIsExposed(t *testing.T) {
	dest := newMemDestination()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dest.now = clock.now
	opts := baseOptions(dest, clock, goodDumper())

	summary, err := PreMigrationCopy(context.Background(), opts)
	if err != nil {
		t.Fatalf("PreMigrationCopy: %v", err)
	}
	if summary.Kind != KindBeforeUpgrade {
		t.Fatalf("kind = %q, want %q", summary.Kind, KindBeforeUpgrade)
	}
	requireStatus(t, summary.Status, StatusComplete)

	svc, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	listing, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if listing.Complete != 1 {
		t.Fatalf("listing.Complete = %d, want 1", listing.Complete)
	}
}

// TestBeforeUpgradeFailsWhenTheGuardFails means a migration runner that aborts
// on the error never proceeds unprotected.
func TestBeforeUpgradeFailsWhenTheGuardFails(t *testing.T) {
	dest := newMemDestination()
	clock := newClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	dest.now = clock.now
	dumper := goodDumper()
	dumper.tables = []TableCount{{Name: "users", Present: false}}
	svc, err := New(baseOptions(dest, clock, dumper))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := svc.BeforeUpgrade(context.Background()); err == nil {
		t.Fatalf("BeforeUpgrade succeeded with no user data")
	}
}
