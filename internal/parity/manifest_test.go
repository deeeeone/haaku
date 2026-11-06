package parity

import (
	"fmt"
	"strings"
	"testing"

	"trakt-api/internal/openapi"
)

// The fixtures below are deliberately tiny: a serving route, a 501 route, and a
// custom (non-Trakt) serving route, which is enough to exercise every rule.

func defs() []openapi.RouteDef {
	return []openapi.RouteDef{
		{Method: "GET", Path: "/v1/movies/trending", StatusCodes: map[int]string{200: "OK"}},
		{Method: "GET", Path: "/v1/movies/{id}/stats", StatusCodes: map[int]string{200: "Stats", 501: "Not implemented"}},
		{Method: "GET", Path: "/v1/search", StatusCodes: map[int]string{200: "OK"}},
		{Method: "GET", Path: "/v1/webhooks", StatusCodes: map[int]string{200: "OK"}},
	}
}

// manifestYAML builds a manifest from row blocks so each test can state exactly
// the row it is about.
func manifestYAML(rows string) string {
	return "manifest: 1\nupstream: test\nsections:\n" +
		"  - name: Test\n    rows:\n" + rows
}

const (
	rowImplemented = `      - trakt: "GET /movies/trending"
        ours: "GET /v1/movies/trending"
        status: implemented
        rationale: "served by the discovery list."
`
	rowDivergent = `      - trakt: "GET /search/{type}"
        ours: "GET /v1/search"
        status: divergent
        rationale: "type is a query param upstream, a path segment here."
        compat: "listener maps the segment to ?type="
`
	rowBlockedEngine = `      - trakt: "GET /movies/{id}/stats"
        ours: "GET /v1/movies/{id}/stats"
        status: blocked-engine
        rationale: "the route answers 501."
        engine: "stats engine (issue #5)"
`
	rowBlockedData = `      - trakt: "GET /movies/boxoffice"
        status: blocked-data
        rationale: "no revenue columns."
`
	rowOutOfScope = `      - trakt: "GET /team"
        status: out-of-scope
        rationale: "decision: no team page on a self-hosted instance."
`
	rowCustom = `      - trakt: ""
        ours: "GET /v1/webhooks"
        status: out-of-scope
        rationale: "custom: our own webhook API."
`
)

// validManifest is a manifest that Check must accept, once it also records the
// 501 route (rowBlockedEngine does).
const validRows = rowImplemented + rowDivergent + rowBlockedEngine + rowBlockedData + rowOutOfScope + rowCustom

func mustParse(t *testing.T, yaml string) *Manifest {
	t.Helper()
	m, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func check(t *testing.T, yaml string) []Problem {
	t.Helper()
	m := mustParse(t, yaml)
	return Check(m, NewRouteSet(defs()))
}

func TestCheckAcceptsAConsistentManifest(t *testing.T) {
	if problems := check(t, manifestYAML(validRows)); len(problems) != 0 {
		t.Fatalf("expected no problems, got: %v", problems)
	}
}

// Rule 1: a row may not claim a route that does not exist.
func TestCheckFailsWhenImplementedRowNamesAnUnregisteredRoute(t *testing.T) {
	broken := strings.Replace(rowImplemented, "GET /v1/movies/trending", "GET /v1/movies/trending/now", 1)
	problems := check(t, manifestYAML(broken+rowDivergent+rowBlockedEngine+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "is not in the route registry") {
		t.Fatalf("expected an unregistered-route problem, got: %v", problems)
	}
}

// Rule 1 again, from the other side: a route that answers 501 is not
// implemented, so a row that marks it so must fail.
func TestCheckFailsWhenImplementedRowNamesA501Route(t *testing.T) {
	broken := `      - trakt: "GET /movies/{id}/stats"
        ours: "GET /v1/movies/{id}/stats"
        status: implemented
        rationale: "claims parity by presence."
`
	problems := check(t, manifestYAML(rowImplemented+broken+rowDivergent+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "answers 501 not_implemented") {
		t.Fatalf("expected a 501-contradiction problem, got: %v", problems)
	}
}

// Rule 2: the ours column names a route that serves, so the status cannot say
// otherwise.
func TestCheckFailsWhenAServedRouteIsMarkedBlocked(t *testing.T) {
	broken := `      - trakt: "GET /movies/trending"
        ours: "GET /v1/movies/trending"
        status: blocked-engine
        rationale: "wrong: this route serves."
        engine: "stats engine (issue #5)"
`
	problems := check(t, manifestYAML(broken+rowDivergent+rowBlockedEngine+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "can only be implemented or divergent") {
		t.Fatalf("expected a status-contradiction problem, got: %v", problems)
	}
}

func TestCheckFailsWhenACustomServedRouteIsNotOutOfScope(t *testing.T) {
	broken := `      - trakt: ""
        ours: "GET /v1/webhooks"
        status: blocked-data
        rationale: "wrong: this route is served."
`
	problems := check(t, manifestYAML(rowImplemented+rowDivergent+rowBlockedEngine+rowBlockedData+rowOutOfScope+broken))
	if !hasProblem(problems, "can only be out-of-scope") {
		t.Fatalf("expected a custom-row problem, got: %v", problems)
	}
}

// A 501 route that no row records is exactly the drift this manifest exists to
// catch: the surface grew a non-serving endpoint and coverage did not notice.
func TestCheckFailsWhenA501RouteIsUnrecorded(t *testing.T) {
	problems := check(t, manifestYAML(rowImplemented+rowDivergent+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "no row records it as blocked-engine") {
		t.Fatalf("expected an unrecorded-501 problem, got: %v", problems)
	}
}

func TestCheckFailsWhenDivergentHasNoCompatNote(t *testing.T) {
	broken := strings.Replace(rowDivergent, `        compat: "listener maps the segment to ?type="`+"\n", "", 1)
	problems := check(t, manifestYAML(rowImplemented+broken+rowBlockedEngine+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "no compat note") {
		t.Fatalf("expected a missing-compat problem, got: %v", problems)
	}
}

func TestCheckFailsWhenBlockedEngineNamesNoEngine(t *testing.T) {
	broken := strings.Replace(rowBlockedEngine, `        engine: "stats engine (issue #5)"`+"\n", "", 1)
	problems := check(t, manifestYAML(rowImplemented+rowDivergent+broken+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "names no engine") {
		t.Fatalf("expected a missing-engine problem, got: %v", problems)
	}
}

func TestCheckFailsWhenOutOfScopeRationaleIsUnexplained(t *testing.T) {
	broken := strings.Replace(rowOutOfScope, `"decision: no team page on a self-hosted instance."`, `"not doing it."`, 1)
	problems := check(t, manifestYAML(rowImplemented+rowDivergent+rowBlockedEngine+rowBlockedData+broken+rowCustom))
	if !hasProblem(problems, `starts with neither "decision:"`) {
		t.Fatalf("expected an unexplained out-of-scope problem, got: %v", problems)
	}
}

func TestCheckFailsWhenImplementedRowHasNoRoute(t *testing.T) {
	broken := `      - trakt: "GET /movies/anticipated"
        status: implemented
        rationale: "claims parity without a route."
`
	problems := check(t, manifestYAML(broken+rowDivergent+rowBlockedEngine+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "names no route in the ours column") {
		t.Fatalf("expected a missing-route problem, got: %v", problems)
	}
}

func TestCheckFailsOnUnknownStatus(t *testing.T) {
	broken := strings.Replace(rowBlockedData, "blocked-data", "partially-there", 1)
	problems := check(t, manifestYAML(rowImplemented+rowDivergent+rowBlockedEngine+broken+rowOutOfScope+rowCustom))
	if !hasProblem(problems, "unknown status") {
		t.Fatalf("expected an unknown-status problem, got: %v", problems)
	}
}

func TestCheckFailsOnDuplicateTraktEndpoint(t *testing.T) {
	problems := check(t, manifestYAML(validRows+rowImplemented))
	if !hasProblem(problems, "duplicate Trakt endpoint") {
		t.Fatalf("expected a duplicate problem, got: %v", problems)
	}
}

// A typo in the ours column must not be silently treated as "we serve nothing".
func TestCheckFailsOnMalformedOursValue(t *testing.T) {
	broken := strings.Replace(rowImplemented, `        ours: "GET /v1/movies/trending"`, `        ours: "v1/movies/trending"`, 1)
	problems := check(t, manifestYAML(broken+rowDivergent+rowBlockedEngine+rowBlockedData+rowOutOfScope+rowCustom))
	if !hasProblem(problems, `ours column: "v1/movies/trending" is not`) {
		t.Fatalf("expected a malformed-route problem, got: %v", problems)
	}
}

func TestParseRejectsUnknownTopLevelKey(t *testing.T) {
	_, err := Parse([]byte("manifest: 1\nsectons:\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("expected an unknown-key error, got: %v", err)
	}
}

func TestParseRejectsUnknownRowKey(t *testing.T) {
	_, err := Parse([]byte(manifestYAML(`      - trakt: "GET /x"
        ours: ""
        status: blocked-data
        rationale: "x"
        reason: "typo for rationale"
`)))
	if err == nil || !strings.Contains(err.Error(), "unknown row key") {
		t.Fatalf("expected an unknown-row-key error, got: %v", err)
	}
}

func TestParseRecordsRowLines(t *testing.T) {
	m := mustParse(t, manifestYAML(validRows))
	const firstRowLine = 6 // 5 header lines, then the first "- trakt:" entry
	if got := m.Sections[0].Rows[0].Line; got != firstRowLine {
		t.Fatalf("first row should be on line %d, got %d", firstRowLine, got)
	}
}

func TestCoverageCountsSectionsAndStatuses(t *testing.T) {
	m := mustParse(t, manifestYAML(validRows))
	c := Compute(m)

	if c.Trakt != 5 {
		t.Fatalf("expected 5 Trakt rows, got %d", c.Trakt)
	}
	if c.ExtraRows != 1 {
		t.Fatalf("expected 1 custom row, got %d", c.ExtraRows)
	}
	if c.Implemented() != 1 || c.Divergent() != 1 {
		t.Fatalf("expected 1 implemented and 1 divergent, got %d and %d", c.Implemented(), c.Divergent())
	}
	if c.ByStatus[StatusBlockedEngine] != 1 || c.ByStatus[StatusBlockedData] != 1 || c.ByStatus[StatusOutOfScope] != 1 {
		t.Fatalf("unexpected status breakdown: %v", c.ByStatus)
	}

	table := c.Table()
	for _, want := range []string{"Test", "TOTAL", "1 of 5", "non-Trakt surface"} {
		if !strings.Contains(table, want) {
			t.Fatalf("coverage table is missing %q:\n%s", want, table)
		}
	}
}

func TestParseRouteKey(t *testing.T) {
	if _, err := ParseRouteKey("GET /v1/x"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, bad := range []string{"", "GET", "GET /v1/x extra", "FETCH /v1/x", "GET v1/x"} {
		if _, err := ParseRouteKey(bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

func TestNewRouteSetMarks501Routes(t *testing.T) {
	rs := NewRouteSet(defs())
	if !rs.Registered[openapi.RouteKey{Method: "GET", Path: "/v1/movies/trending"}] {
		t.Fatal("trending should be registered")
	}
	if !rs.NotImplemented[openapi.RouteKey{Method: "GET", Path: "/v1/movies/{id}/stats"}] {
		t.Fatal("stats declares 501 and should be marked not-implemented")
	}
	if rs.NotImplemented[openapi.RouteKey{Method: "GET", Path: "/v1/movies/trending"}] {
		t.Fatal("trending does not declare 501")
	}
}

func hasProblem(problems []Problem, substr string) bool {
	for _, p := range problems {
		if strings.Contains(p.String(), substr) {
			return true
		}
	}
	return false
}

// TestProblemStringKeepsTheLine guards the error format reviewers read.
func TestProblemStringKeepsTheLine(t *testing.T) {
	p := Problem{Line: 12, Msg: "boom"}
	if got := fmt.Sprint(p); got != "line 12: boom" {
		t.Fatalf("unexpected problem format: %q", got)
	}
	if got := fmt.Sprint(Problem{Msg: "boom"}); got != "boom" {
		t.Fatalf("unexpected problem format without a line: %q", got)
	}
}
