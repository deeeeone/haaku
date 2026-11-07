package main

import (
	"testing"

	"trakt-api/internal/mode"
	"trakt-api/internal/workers"
)

// TestWorkerModeRefusesToMigrate pins the deployment safety property at the
// point the command decides it: a worker-only process never applies a
// migration, even with a database in hand. mode.MayMigrate encodes the rule;
// wantsMigrations is what cmd/api actually calls, so both are asserted.
func TestWorkerModeRefusesToMigrate(t *testing.T) {
	if wantsMigrations(mode.Worker, true) {
		t.Error("wantsMigrations(worker, database present) = true; a worker must never migrate")
	}
	if mode.Worker.MayMigrate() {
		t.Error("mode.Worker.MayMigrate() = true, want false")
	}
	if !wantsMigrations(mode.Migrate, true) {
		t.Error("wantsMigrations(migrate, database present) = false, want true")
	}
	if !wantsMigrations(mode.API, true) {
		t.Error("wantsMigrations(api, database present) = false, want true")
	}
	// No database, no migrations, whatever the mode says.
	if wantsMigrations(mode.API, false) {
		t.Error("wantsMigrations(api, no database) = true, want false")
	}
}

// TestWorkerPlanRunsEachWorkerInOneProcess proves the anti-double-run rule: an
// API-only process (TKILLER_WORKER=none) starts no worker, a worker process
// that selected nothing is refused instead of idling, and selecting a worker is
// the only way it runs.
func TestWorkerPlanRunsEachWorkerInOneProcess(t *testing.T) {
	all, err := workers.ParseSelection("all")
	if err != nil {
		t.Fatalf("ParseSelection(all): %v", err)
	}
	none, err := workers.ParseSelection("none")
	if err != nil {
		t.Fatalf("ParseSelection(none): %v", err)
	}

	cases := []struct {
		name      string
		mode      mode.Mode
		sel       workers.Selection
		wantStart bool
		wantFatal bool
	}{
		{"default api runs workers in-process", mode.API, all, true, false},
		{"api only runs no worker", mode.API, none, false, false},
		{"worker runs its selection", mode.Worker, all, true, false},
		{"worker with nothing selected is refused", mode.Worker, none, false, true},
		{"migrate runs no worker", mode.Migrate, all, false, false},
	}
	for _, tc := range cases {
		start, fatal := workerPlan(tc.mode, tc.sel)
		if start != tc.wantStart || fatal != tc.wantFatal {
			t.Errorf("%s: workerPlan(%s, %q) = (start=%v, fatal=%v), want (%v, %v)",
				tc.name, tc.mode, tc.sel.String(), start, fatal, tc.wantStart, tc.wantFatal)
		}
	}

	// The API-only process must build no runtime: Catalog of the empty
	// selection is empty, so there is nothing to start.
	if built := workers.Catalog(workers.Deps{}, none); len(built) != 0 {
		t.Errorf("API-only process would build %d workers, want 0", len(built))
	}
}

// TestHealthListenerIsOnlyForWorkerMode keeps a split deployment from fighting
// over the health port: the API answers /healthz on its own port, the worker
// mode uses its own listener.
func TestHealthListenerIsOnlyForWorkerMode(t *testing.T) {
	if got := healthListenFor(mode.API); got != "" {
		t.Errorf("healthListenFor(api) = %q, want \"\" (the API serves its own)", got)
	}
	t.Setenv(workers.HealthListenEnv, "")
	if got := healthListenFor(mode.Worker); got != workers.DefaultHealthListen {
		t.Errorf("healthListenFor(worker) = %q, want %q", got, workers.DefaultHealthListen)
	}
}
