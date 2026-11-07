package workers

import (
	"fmt"
	"strings"

	"trakt-api/internal/obs"
)

// The background workers a process can run. They are the label values
// obs.NewWorker renders and the names TKILLER_WORKER selects, so a heartbeat, a
// processed counter and a log line always name the same worker regardless of
// which process hosts it.
const (
	NameWebhooks = obs.WorkerWebhooks
	NameActivity = obs.WorkerActivity
	NameOutbox   = obs.WorkerOutbox
	NameMetadata = obs.WorkerMetadata
	NameStats    = obs.WorkerStats
)

// EnvSelection is the environment variable that selects which background
// workers a process runs: a comma-separated list of names, or "all" / "none".
// Unset or empty means "all", so a single-container install keeps running the
// workers in-process and nothing changes for an existing install.
const EnvSelection = "TKILLER_WORKER"

// canonicalOrder fixes both the order workers start in and the order Names
// reports, so logs and metrics are stable no matter how the selector was
// written.
var canonicalOrder = []string{NameWebhooks, NameActivity, NameOutbox, NameMetadata, NameStats}

// Selection is the set of worker names a process runs.
type Selection struct {
	names []string
}

// All returns every known worker.
func All() Selection { return Selection{names: append([]string(nil), canonicalOrder...)} }

// None returns an empty selection.
func None() Selection { return Selection{} }

// Select returns a selection of the named workers, erroring on an unknown name.
func Select(names ...string) (Selection, error) {
	return ParseSelection(strings.Join(names, ","))
}

// Names returns every known worker name, in canonical order.
func Names() []string { return append([]string(nil), canonicalOrder...) }

// ParseSelection turns a raw TKILLER_WORKER value into a Selection. An empty
// value or "all" selects every worker; "none" selects none; anything else is a
// comma-separated list of names. An unknown name is an error rather than a
// silent fallthrough: a typo that quietly started (or skipped) the whole fleet
// is the same expensive kind of wrong that mode.Parse refuses.
func ParseSelection(raw string) (Selection, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "all":
		return All(), nil
	case "none":
		return None(), nil
	}

	chosen := make(map[string]bool, len(canonicalOrder))
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !known(name) {
			return Selection{}, fmt.Errorf("unknown worker %q (want a comma-separated list of %s, or all/none)",
				name, strings.Join(canonicalOrder, ", "))
		}
		chosen[name] = true
	}
	if len(chosen) == 0 {
		return None(), nil
	}

	names := make([]string, 0, len(chosen))
	for _, name := range canonicalOrder {
		if chosen[name] {
			names = append(names, name)
		}
	}
	return Selection{names: names}, nil
}

func known(name string) bool {
	for _, n := range canonicalOrder {
		if n == name {
			return true
		}
	}
	return false
}

// Names returns the selected names, in canonical order.
func (s Selection) Names() []string { return append([]string(nil), s.names...) }

// Has reports whether name is selected.
func (s Selection) Has(name string) bool {
	for _, n := range s.names {
		if n == name {
			return true
		}
	}
	return false
}

// Empty reports whether nothing is selected.
func (s Selection) Empty() bool { return len(s.names) == 0 }

// String renders the selection for logs: "all", "none", or the names.
func (s Selection) String() string {
	switch {
	case len(s.names) == 0:
		return "none"
	case len(s.names) == len(canonicalOrder):
		return "all"
	default:
		return strings.Join(s.names, ",")
	}
}
