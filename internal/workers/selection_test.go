package workers

import (
	"reflect"
	"testing"
)

// TestParseSelectionDefaultsToAll pins the compatibility property: an unset (or
// blank, or "all") HAAKU_WORKER runs every worker, so a single-container
// install behaves exactly as it did before the selector existed.
func TestParseSelectionDefaultsToAll(t *testing.T) {
	for _, raw := range []string{"", "   ", "all", "ALL", " All "} {
		got, err := ParseSelection(raw)
		if err != nil {
			t.Errorf("ParseSelection(%q) error = %v", raw, err)
			continue
		}
		if !reflect.DeepEqual(got.Names(), Names()) {
			t.Errorf("ParseSelection(%q) = %v, want every worker", raw, got.Names())
		}
	}
}

// TestParseSelectionNone asserts the API-only escape hatch: an empty selection
// is how a deployment hands the workers to a dedicated worker process.
func TestParseSelectionNone(t *testing.T) {
	got, err := ParseSelection("none")
	if err != nil {
		t.Fatalf("ParseSelection(none) error = %v", err)
	}
	if !got.Empty() {
		t.Errorf("ParseSelection(none) = %v, want empty", got.Names())
	}
}

// TestParseSelectionList asserts a list is deduped and ordered canonically, so
// logs and startup order do not depend on how the operator wrote the value.
func TestParseSelectionList(t *testing.T) {
	got, err := ParseSelection(" stats , webhooks,webhooks ")
	if err != nil {
		t.Fatalf("ParseSelection error = %v", err)
	}
	want := []string{NameWebhooks, NameStats}
	if !reflect.DeepEqual(got.Names(), want) {
		t.Errorf("Names() = %v, want %v (canonical order, deduped)", got.Names(), want)
	}
	if !got.Has(NameWebhooks) || got.Has(NameMetadata) {
		t.Errorf("Has() disagrees with Names() = %v", got.Names())
	}
}

// TestParseSelectionRejectsUnknown asserts a typo fails loudly. A misspelled
// name that silently started the whole fleet (or none of it) is the same
// expensive kind of wrong mode.Parse refuses.
func TestParseSelectionRejectsUnknown(t *testing.T) {
	for _, raw := range []string{"webhook", "all,none", "metadata2", " worker "} {
		if _, err := ParseSelection(raw); err == nil {
			t.Errorf("ParseSelection(%q) returned no error, want one", raw)
		}
	}
}

// TestSelectionString pins the log rendering an operator greps for.
func TestSelectionString(t *testing.T) {
	cases := map[string]string{"": "all", "none": "none", "outbox,webhooks": "webhooks,outbox"}
	for raw, want := range cases {
		sel, err := ParseSelection(raw)
		if err != nil {
			t.Fatalf("ParseSelection(%q) error = %v", raw, err)
		}
		if got := sel.String(); got != want {
			t.Errorf("ParseSelection(%q).String() = %q, want %q", raw, got, want)
		}
	}
}

// TestSelectRejectsUnknown pins the variadic helper used by tests and callers.
func TestSelectRejectsUnknown(t *testing.T) {
	if _, err := Select(NameOutbox, "nope"); err == nil {
		t.Error("Select with an unknown name returned no error")
	}
	sel, err := Select(NameOutbox)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !sel.Has(NameOutbox) || len(sel.Names()) != 1 {
		t.Errorf("Select(outbox) = %v, want just outbox", sel.Names())
	}
}
