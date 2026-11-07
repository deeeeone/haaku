package contractdiff

import (
	"strings"
	"testing"
)

// doc wraps a `paths:` body in the minimum valid OpenAPI document.
func doc(pathsBody string) string {
	return "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\npaths:\n" + pathsBody
}

func mustDoc(t *testing.T, y string) *Document {
	t.Helper()
	d, err := ParseDocument([]byte(y))
	if err != nil {
		t.Fatalf("ParseDocument: %v\n%s", err, y)
	}
	return d
}

// findChange returns the single change with the given class, failing if there is
// not exactly one.
func findChange(t *testing.T, changes []Change, class Class) Change {
	t.Helper()
	var found []Change
	for _, c := range changes {
		if c.Class == class {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		var have []string
		for _, c := range changes {
			have = append(have, string(c.Class)+": "+c.Signature())
		}
		t.Fatalf("expected exactly one %q change, got %d; changes: %s", class, len(found), strings.Join(have, "; "))
	}
	return found[0]
}

const baseOp = `  /v1/a:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  name:
                    type: string
`

// omitLastProperty returns baseOp without the `name` property, for schemas that
// declare no fields.
func TestDiff_ClassifiesEveryChangeClass(t *testing.T) {
	cases := []struct {
		name         string
		baseline     string
		current      string
		wantClass    Class
		wantBreaking bool
		wantSig      string
	}{
		{
			name:         "removed-path",
			baseline:     doc(baseOp + "  /v1/b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			current:      doc(baseOp),
			wantClass:    RemovedPath,
			wantBreaking: true,
			wantSig:      "removed-path GET /v1/b",
		},
		{
			name:         "renamed-path",
			baseline:     doc("  /v1/shows:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			current:      doc("  /v1/series:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			wantClass:    RenamedPath,
			wantBreaking: true,
			wantSig:      "renamed-path GET /v1/shows -> /v1/series",
		},
		{
			name:         "added-path",
			baseline:     doc(baseOp),
			current:      doc(baseOp + "  /v1/b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			wantClass:    AddedPath,
			wantBreaking: false,
			wantSig:      "added-path GET /v1/b",
		},
		{
			name:         "removed-status-code",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n        \"404\":\n          description: missing\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			wantClass:    RemovedStatusCode,
			wantBreaking: true,
			wantSig:      "removed-status-code GET /v1/a 404",
		},
		{
			name:         "added-status-code",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n        \"409\":\n          description: conflict\n"),
			wantClass:    AddedStatusCode,
			wantBreaking: false,
			wantSig:      "added-status-code GET /v1/a 409",
		},
		{
			name:         "removed-response-field",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n                  year:\n                    type: integer\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n"),
			wantClass:    RemovedResponseField,
			wantBreaking: true,
			wantSig:      "removed-response-field GET /v1/a 200 application/json year",
		},
		{
			name:         "renamed-response-field",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n                  year:\n                    type: integer\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n                  released:\n                    type: integer\n"),
			wantClass:    RenamedResponseField,
			wantBreaking: true,
			wantSig:      "renamed-response-field GET /v1/a 200 application/json year -> released",
		},
		{
			name:         "added-optional-field",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n                  year:\n                    type: integer\n"),
			wantClass:    AddedOptionalField,
			wantBreaking: false,
			wantSig:      "added-optional-field GET /v1/a 200 application/json year",
		},
		{
			name:         "new-required-request-field",
			baseline:     doc("  /v1/a:\n    post:\n      requestBody:\n        required: true\n        content:\n          application/json:\n            schema:\n              type: object\n              properties:\n                name:\n                  type: string\n      responses:\n        \"200\":\n          description: ok\n"),
			current:      doc("  /v1/a:\n    post:\n      requestBody:\n        required: true\n        content:\n          application/json:\n            schema:\n              type: object\n              required:\n                - name\n              properties:\n                name:\n                  type: string\n      responses:\n        \"200\":\n          description: ok\n"),
			wantClass:    NewRequiredRequestField,
			wantBreaking: true,
			wantSig:      "new-required-request-field POST /v1/a request application/json name",
		},
		{
			name:         "narrowed-enum",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n                    enum: [active, hidden]\n"),
			wantClass:    NarrowedEnum,
			wantBreaking: true,
			wantSig:      "narrowed-enum GET /v1/a 200 application/json status",
		},
		{
			name:         "removed-enum-value",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n                    enum: [active, hidden]\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n                    enum: [active]\n"),
			wantClass:    RemovedEnumValue,
			wantBreaking: true,
			wantSig:      "removed-enum-value GET /v1/a 200 application/json status \"hidden\"",
		},
		{
			name:         "added-enum-value",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n                    enum: [active]\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  status:\n                    type: string\n                    enum: [active, hidden]\n"),
			wantClass:    AddedEnumValue,
			wantBreaking: false,
			wantSig:      "added-enum-value GET /v1/a 200 application/json status \"hidden\"",
		},
		{
			name:         "changed-type",
			baseline:     doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: string\n"),
			current:      doc("  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                type: object\n                properties:\n                  name:\n                    type: integer\n"),
			wantClass:    ChangedType,
			wantBreaking: true,
			wantSig:      "changed-type GET /v1/a 200 application/json name string -> integer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes := Diff(mustDoc(t, tc.baseline), mustDoc(t, tc.current))
			c := findChange(t, changes, tc.wantClass)
			if c.Breaking() != tc.wantBreaking {
				t.Errorf("%s: Breaking() = %v, want %v", tc.wantClass, c.Breaking(), tc.wantBreaking)
			}
			if c.Signature() != tc.wantSig {
				t.Errorf("signature = %q, want %q", c.Signature(), tc.wantSig)
			}
			if tc.wantBreaking != tc.wantClass.Breaking() {
				t.Errorf("class %s Breaking() disagrees with the case's expectation", tc.wantClass)
			}
		})
	}
}

func TestDiff_IdenticalDocumentsProduceNoChanges(t *testing.T) {
	d := mustDoc(t, doc(baseOp))
	if changes := Diff(d, d); len(changes) != 0 {
		t.Fatalf("expected no changes for identical documents, got %d: %v", len(changes), changes)
	}
}

func TestDiff_FollowsLocalRefs(t *testing.T) {
	// A response that $refs a component schema: removing a field from the
	// component must be detected on the operation that references it.
	base := "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\npaths:\n" +
		"  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: '#/components/schemas/Thing'\n" +
		"components:\n  schemas:\n    Thing:\n      type: object\n      properties:\n        name:\n          type: string\n        year:\n          type: integer\n"
	cur := "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\npaths:\n" +
		"  /v1/a:\n    get:\n      responses:\n        \"200\":\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: '#/components/schemas/Thing'\n" +
		"components:\n  schemas:\n    Thing:\n      type: object\n      properties:\n        name:\n          type: string\n"

	changes := Diff(mustDoc(t, base), mustDoc(t, cur))
	c := findChange(t, changes, RemovedResponseField)
	if c.Signature() != "removed-response-field GET /v1/a 200 application/json year" {
		t.Fatalf("unexpected signature through $ref: %q", c.Signature())
	}
}

func TestEvaluate_AcknowledgedBreakingChangePasses(t *testing.T) {
	baseline := mustDoc(t, doc(baseOp+"  /v1/b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"))
	current := mustDoc(t, doc(baseOp))

	acks, err := ParseAcknowledgements([]byte(
		"version: 1\nacknowledged:\n  - signature: \"removed-path GET /v1/b\"\n    date: 2026-11-07\n    reason: \"retired with the feature it served; no client used it\"\n"))
	if err != nil {
		t.Fatalf("ParseAcknowledgements: %v", err)
	}

	r := Evaluate(baseline, current, acks, "baseline.yaml", "openapi.yaml")
	if r.Failure() {
		t.Fatalf("expected the gate to pass on an acknowledged change; unacknowledged: %v", r.Unacknowledged)
	}
	if r.Acknowledged != 1 {
		t.Fatalf("Acknowledged = %d, want 1", r.Acknowledged)
	}
	if got := r.Summary(); !strings.Contains(got, "[acknowledged]") {
		t.Errorf("summary should mark the change acknowledged:\n%s", got)
	}
}

func TestEvaluate_UnacknowledgedBreakingChangeFails(t *testing.T) {
	baseline := mustDoc(t, doc(baseOp+"  /v1/b:\n    get:\n      responses:\n        \"200\":\n          description: ok\n"))
	current := mustDoc(t, doc(baseOp))

	acks, err := ParseAcknowledgements([]byte("version: 1\nacknowledged: []\n"))
	if err != nil {
		t.Fatalf("ParseAcknowledgements: %v", err)
	}

	r := Evaluate(baseline, current, acks, "baseline.yaml", "openapi.yaml")
	if !r.Failure() {
		t.Fatal("expected the gate to fail on an unacknowledged breaking change")
	}
	msg := r.UnacknowledgedMessage()
	if !strings.Contains(msg, "removed-path GET /v1/b") {
		t.Errorf("failure message must name the change:\n%s", msg)
	}
	if !strings.Contains(msg, "breaking-changes.yaml") {
		t.Errorf("failure message must point at the acknowledgement file:\n%s", msg)
	}
	if !strings.Contains(msg, "- signature: \"removed-path GET /v1/b\"") {
		t.Errorf("failure message must show the entry to write:\n%s", msg)
	}
}

func TestParseAcknowledgements_RejectsMalformedEntries(t *testing.T) {
	cases := map[string]string{
		"missing reason": "version: 1\nacknowledged:\n  - signature: \"removed-path GET /v1/b\"\n    date: 2026-11-07\n",
		"bad date":       "version: 1\nacknowledged:\n  - signature: \"removed-path GET /v1/b\"\n    date: soon\n    reason: x\n",
		"missing sig":    "version: 1\nacknowledged:\n  - date: 2026-11-07\n    reason: x\n",
		"wrong version":  "version: 2\nacknowledged: []\n",
		"unknown key":    "version: 1\nacknowledged:\n  - signature: \"removed-path GET /v1/b\"\n    date: 2026-11-07\n    reason: x\n    who: someone\n",
		"duplicate sig":  "version: 1\nacknowledged:\n  - signature: \"removed-path GET /v1/b\"\n    date: 2026-11-07\n    reason: a\n  - signature: \"removed-path GET /v1/b\"\n    date: 2026-11-08\n    reason: b\n",
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAcknowledgements([]byte(y)); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

// TestGateOnCommittedContractIsClean runs the gate on the real committed files.
// It fails if the baseline or acknowledgement files are missing or malformed, or
// if the working tree carries an unacknowledged break — which is exactly what
// `make contract-diff` checks, so the tool cannot silently become a no-op.
func TestGateOnCommittedContractIsClean(t *testing.T) {
	baseline, err := LoadDocument("../../contracts/api/v1/openapi-baseline.yaml")
	if err != nil {
		t.Fatalf("load baseline: %v", err)
	}
	current, err := LoadDocument("../../openapi.yaml")
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	acks, err := LoadAcknowledgements("../../contracts/api/v1/breaking-changes.yaml")
	if err != nil {
		t.Fatalf("load acknowledgements: %v", err)
	}

	r := Evaluate(baseline, current, acks, "baseline", "openapi.yaml")
	if r.Failure() {
		t.Fatalf("the committed contract carries unacknowledged breaking changes:\n%s", r.UnacknowledgedMessage())
	}
}
