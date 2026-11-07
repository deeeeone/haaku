package workers

import (
	"reflect"
	"testing"

	"github.com/robfig/cron/v3"
)

// TestCatalogBuildsOnlyTheSelectedWorkers is the structural guard behind "the
// API does not double-start a worker the worker mode owns": a worker that is
// not in the selection is never built, so there is nothing to start.
func TestCatalogBuildsOnlyTheSelectedWorkers(t *testing.T) {
	sel, err := ParseSelection("webhooks,outbox")
	if err != nil {
		t.Fatalf("ParseSelection: %v", err)
	}
	got := namesOf(Catalog(Deps{}, sel))
	want := []string{NameWebhooks, NameOutbox}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Catalog(webhooks,outbox) built %v, want %v", got, want)
	}

	if built := Catalog(Deps{}, None()); len(built) != 0 {
		t.Errorf("Catalog(none) built %d workers, want 0", len(built))
	}
}

// TestCatalogCoversEveryName pins the coverage property: "all" builds exactly
// the known workers, in canonical order, so adding a name cannot leave it
// silently unrunnable.
func TestCatalogCoversEveryName(t *testing.T) {
	got := namesOf(Catalog(Deps{}, All()))
	if !reflect.DeepEqual(got, Names()) {
		t.Errorf("Catalog(all) = %v, want %v", got, Names())
	}
}

// TestCatalogWorkersAreWellFormed catches the class of bug that only shows up
// at startup: a loop with no step or a bad interval, or a scheduled job whose
// cron spec the scheduler would reject.
func TestCatalogWorkersAreWellFormed(t *testing.T) {
	for _, w := range Catalog(Deps{}, All()) {
		switch w.Kind {
		case Loop:
			if w.Step == nil {
				t.Errorf("loop worker %q has no step", w.Name)
			}
			if w.Interval <= 0 {
				t.Errorf("loop worker %q has interval %s", w.Name, w.Interval)
			}
			if len(w.Jobs) != 0 {
				t.Errorf("loop worker %q also has %d jobs", w.Name, len(w.Jobs))
			}
		case Schedule:
			if w.Step != nil {
				t.Errorf("scheduled worker %q also has a step", w.Name)
			}
			for _, job := range w.Jobs {
				if _, err := cron.ParseStandard(job.Schedule); err != nil {
					t.Errorf("worker %q job schedule %q does not parse: %v", w.Name, job.Schedule, err)
				}
				if job.Run == nil {
					t.Errorf("worker %q job %q has no Run", w.Name, job.Schedule)
				}
				if job.Describe == "" {
					t.Errorf("worker %q job %q has no description", w.Name, job.Schedule)
				}
			}
		default:
			t.Errorf("worker %q has an unknown kind %d", w.Name, w.Kind)
		}
	}
}

// TestStatsWorkerHasAnEmptyJobList documents, in code, the one deliberate hole
// in this change: the stats recompute is a sibling lane, and this PR leaves the
// seam rather than inventing placeholder work. When the recompute lands, this
// test is the line to change.
func TestStatsWorkerHasAnEmptyJobList(t *testing.T) {
	sel, err := ParseSelection(NameStats)
	if err != nil {
		t.Fatalf("ParseSelection: %v", err)
	}
	built := Catalog(Deps{}, sel)
	if len(built) != 1 {
		t.Fatalf("Catalog(stats) built %d workers, want 1", len(built))
	}
	if n := len(built[0].Jobs); n != 0 {
		t.Errorf("stats worker has %d jobs, want 0 until the recompute lane lands", n)
	}
}

func namesOf(ws []Worker) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name
	}
	return out
}
