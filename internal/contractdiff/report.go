package contractdiff

import (
	"fmt"
	"strings"
	"time"
)

// Report is the result of evaluating one baseline/current/acknowledgement set.
type Report struct {
	BaselinePath string
	CurrentPath  string

	Changes []Change

	// Unacknowledged are the breaking changes with no entry in
	// breaking-changes.yaml. The gate fails when this is non-empty.
	Unacknowledged []Change
	// Acknowledged is the count of breaking changes that are written down.
	Acknowledged int
	// StaleAcks are acknowledgement signatures that no longer match a breaking
	// change (usually because the baseline was refreshed afterwards). They are
	// informational, never fatal.
	StaleAcks []string
}

// Breaking returns the breaking changes in the report.
func (r *Report) Breaking() []Change {
	var out []Change
	for _, c := range r.Changes {
		if c.Breaking() {
			out = append(out, c)
		}
	}
	return out
}

// Additive returns the additive changes in the report.
func (r *Report) Additive() []Change {
	var out []Change
	for _, c := range r.Changes {
		if !c.Breaking() {
			out = append(out, c)
		}
	}
	return out
}

// Evaluate diffs the two documents and matches the breaking changes against the
// acknowledgements.
func Evaluate(baseline, current *Document, acks *Acknowledgements, baselinePath, currentPath string) *Report {
	r := &Report{BaselinePath: baselinePath, CurrentPath: currentPath}
	r.Changes = Diff(baseline, current)

	acked := acks.signatureSet()
	matched := map[string]bool{}
	for _, c := range r.Changes {
		if !c.Breaking() {
			continue
		}
		if acked[c.Signature()] {
			r.Acknowledged++
			matched[c.Signature()] = true
			continue
		}
		r.Unacknowledged = append(r.Unacknowledged, c)
	}

	for _, e := range acks.Acknowledged {
		if !matched[strings.TrimSpace(e.Signature)] {
			r.StaleAcks = append(r.StaleAcks, strings.TrimSpace(e.Signature))
		}
	}
	return r
}

// Summary renders the human-readable diff report printed on every run.
func (r *Report) Summary() string {
	breaking := r.Breaking()
	additive := r.Additive()

	var b strings.Builder
	fmt.Fprintf(&b, "contract diff: %s -> %s\n", r.BaselinePath, r.CurrentPath)
	fmt.Fprintf(&b, "  %d breaking (%d acknowledged), %d additive\n",
		len(breaking), r.Acknowledged, len(additive))

	acked := map[string]bool{}
	for _, c := range breaking {
		if !containsChange(r.Unacknowledged, c) {
			acked[c.Signature()] = true
		}
	}

	if len(breaking) > 0 {
		b.WriteString("\nbreaking:\n")
		for _, c := range breaking {
			mark := ""
			if acked[c.Signature()] {
				mark = " [acknowledged]"
			}
			fmt.Fprintf(&b, "  BREAKING%s  %s\n", mark, c.Signature())
			fmt.Fprintf(&b, "            %s\n", c.Detail())
		}
	}
	if len(additive) > 0 {
		b.WriteString("\nadditive:\n")
		for _, c := range additive {
			fmt.Fprintf(&b, "  additive  %s\n", c.Signature())
		}
	}
	if len(r.StaleAcks) > 0 {
		b.WriteString("\nnote: acknowledgement(s) no longer present in the diff (the baseline may have been refreshed):\n")
		for _, s := range r.StaleAcks {
			fmt.Fprintf(&b, "  %s\n", s)
		}
	}
	return b.String()
}

// Failure reports whether the gate fails: an unacknowledged breaking change.
func (r *Report) Failure() bool { return len(r.Unacknowledged) > 0 }

// UnacknowledgedMessage renders the failure message: it names each unacknowledged
// change and shows the exact entry to add to the acknowledgement file.
func (r *Report) UnacknowledgedMessage() string {
	today := time.Now().Format("2006-01-02")

	var b strings.Builder
	fmt.Fprintf(&b, "\n%d breaking change(s) not acknowledged — refusing to pass the gate:\n", len(r.Unacknowledged))
	for _, c := range r.Unacknowledged {
		fmt.Fprintf(&b, "\n  %s\n", c.Signature())
		fmt.Fprintf(&b, "    %s\n", c.Detail())
		fmt.Fprintf(&b, "    If this break is deliberate, write it down in contracts/api/v1/breaking-changes.yaml:\n\n")
		fmt.Fprintf(&b, "      - signature: %q\n", c.Signature())
		fmt.Fprintf(&b, "        date: %s\n", today)
		fmt.Fprintf(&b, "        reason: \"<one line: why this break is acceptable and what clients must do>\"\n")
	}
	fmt.Fprintf(&b, "\nA breaking change is supposed to mean a new URL prefix (/v2). If you did not mean to\n"+
		"break the contract, revert the change instead of acknowledging it.\n")
	return b.String()
}

func containsChange(list []Change, c Change) bool {
	for _, x := range list {
		if x.Signature() == c.Signature() {
			return true
		}
	}
	return false
}
