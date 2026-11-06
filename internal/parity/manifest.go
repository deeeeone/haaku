// Package parity loads docs/parity/parity.yaml — the machine-checkable record
// of how much of the Trakt API surface haaku actually serves — and grades every
// row against the live code-first route registry.
//
// The manifest exists because "100% Trakt coverage" was being asserted rather
// than measured (issue #4). A row is only allowed to say "implemented" when a
// registered route serves that data; a route that answers 501 not_implemented
// can only be recorded as blocked-engine. That is the same honesty rule the
// handlers follow: an honest 501 beats a fabricated 200.
//
// The package has no dependency beyond internal/openapi and gopkg.in/yaml.v3
// (already a direct dependency, used by the OpenAPI inventory) so the manifest
// is parsed strictly without adding anything to the module graph.
package parity

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"trakt-api/internal/openapi"

	"gopkg.in/yaml.v3"
)

// Status is the grade a manifest row carries. The set is closed: the manifest
// is a measurement, so a row must fit one of these or the tool fails.
type Status string

const (
	// StatusImplemented: a registered route serves this data at the Trakt path.
	StatusImplemented Status = "implemented"
	// StatusDivergent: a registered route serves this data, but at a different
	// path or with query params where Trakt uses path segments.
	StatusDivergent Status = "divergent"
	// StatusBlockedEngine: no route serves it; a named engine must land first.
	StatusBlockedEngine Status = "blocked-engine"
	// StatusBlockedData: no route serves it; the data is not in our store.
	StatusBlockedData Status = "blocked-data"
	// StatusOutOfScope: deliberately not served, with a written rationale.
	StatusOutOfScope Status = "out-of-scope"
)

// AllStatuses is the closed set, in report order.
func AllStatuses() []Status {
	return []Status{StatusImplemented, StatusDivergent, StatusBlockedEngine, StatusBlockedData, StatusOutOfScope}
}

func (s Status) valid() bool {
	for _, v := range AllStatuses() {
		if s == v {
			return true
		}
	}
	return false
}

// Row is one Trakt endpoint graded against our reality. Trakt is empty for rows
// describing our own non-Trakt surface (issue #4 section C).
type Row struct {
	Line      int    // 1-based line of the row's first key, for error messages
	Trakt     string // "METHOD /path" on the Trakt side; "" for non-Trakt rows
	Ours      string // "METHOD /path" in our registry; "" when we serve nothing
	Status    Status
	Rationale string
	Engine    string // required for blocked-engine: which engine blocks it
	Compat    string // required for divergent: what the compat listener must do
}

// Section is a group of rows, named after issue #4's sections.
type Section struct {
	Line int
	Name string
	Rows []Row
}

// Manifest is the parsed file.
type Manifest struct {
	Version  int
	Upstream string
	Notes    []string
	Sections []Section
}

// Load reads and parses a manifest from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// manifestKeys are the only top-level keys the format accepts.
var manifestKeys = []string{"manifest", "upstream", "notes", "sections"}

// rowKeys are the only keys a row accepts.
var rowKeys = []string{"trakt", "ours", "status", "rationale", "engine", "compat"}

// Parse decodes the manifest, rejecting unknown keys (a typo in a key would
// otherwise silently drop a field and grade the row wrongly) and recording the
// source line of every row so failures point at a line.
func Parse(data []byte) (*Manifest, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errAt(root, "manifest must be a mapping")
	}

	m := &Manifest{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if seen[key.Value] {
			return nil, errAt(key, "duplicate key %q", key.Value)
		}
		seen[key.Value] = true

		switch key.Value {
		case "manifest":
			if err := val.Decode(&m.Version); err != nil {
				return nil, errAt(val, "manifest: %v", err)
			}
		case "upstream":
			if err := val.Decode(&m.Upstream); err != nil {
				return nil, errAt(val, "upstream: %v", err)
			}
		case "notes":
			if err := val.Decode(&m.Notes); err != nil {
				return nil, errAt(val, "notes: %v", err)
			}
		case "sections":
			sections, err := parseSections(val)
			if err != nil {
				return nil, err
			}
			m.Sections = sections
		default:
			return nil, errAt(key, "unknown key %q (expected one of %s)", key.Value, strings.Join(manifestKeys, ", "))
		}
	}
	return m, nil
}

func parseSections(node *yaml.Node) ([]Section, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, errAt(node, "sections must be a list")
	}
	var sections []Section
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, errAt(item, "each section must be a mapping with name and rows")
		}
		s := Section{Line: item.Line}
		seen := map[string]bool{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if seen[key.Value] {
				return nil, errAt(key, "duplicate section key %q", key.Value)
			}
			seen[key.Value] = true

			switch key.Value {
			case "name":
				if err := val.Decode(&s.Name); err != nil {
					return nil, errAt(val, "section name: %v", err)
				}
			case "rows":
				rows, err := parseRows(val)
				if err != nil {
					return nil, err
				}
				s.Rows = rows
			default:
				return nil, errAt(key, "unknown section key %q (expected name, rows)", key.Value)
			}
		}
		sections = append(sections, s)
	}
	return sections, nil
}

func parseRows(node *yaml.Node) ([]Row, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, errAt(node, "rows must be a list")
	}
	var rows []Row
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, errAt(item, "each row must be a mapping")
		}
		r := Row{Line: item.Line}
		seen := map[string]bool{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			key, val := item.Content[i], item.Content[i+1]
			if seen[key.Value] {
				return nil, errAt(key, "duplicate row key %q", key.Value)
			}
			seen[key.Value] = true

			var target *string
			switch key.Value {
			case "trakt":
				target = &r.Trakt
			case "ours":
				target = &r.Ours
			case "rationale":
				target = &r.Rationale
			case "engine":
				target = &r.Engine
			case "compat":
				target = &r.Compat
			case "status":
				if err := val.Decode(&r.Status); err != nil {
					return nil, errAt(val, "status: %v", err)
				}
				continue
			default:
				return nil, errAt(key, "unknown row key %q (expected one of %s)", key.Value, strings.Join(rowKeys, ", "))
			}
			if err := val.Decode(target); err != nil {
				return nil, errAt(val, "%s: %v", key.Value, err)
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func errAt(n *yaml.Node, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if n == nil || n.Line == 0 {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("line %d: %s", n.Line, msg)
}

// RouteSet is the live registry as the grader sees it: every registered
// operation, plus the subset that answers 501 not_implemented.
type RouteSet struct {
	Registered     map[openapi.RouteKey]bool
	NotImplemented map[openapi.RouteKey]bool
}

// NewRouteSet indexes route definitions. A route counts as not-implemented when
// its registration declares a 501 response — the same signal that makes 501
// appear in the generated spec, so the two cannot disagree.
func NewRouteSet(defs []openapi.RouteDef) RouteSet {
	rs := RouteSet{
		Registered:     make(map[openapi.RouteKey]bool, len(defs)),
		NotImplemented: make(map[openapi.RouteKey]bool),
	}
	for _, d := range defs {
		k := openapi.RouteKey{Method: strings.ToUpper(d.Method), Path: d.Path}
		rs.Registered[k] = true
		if _, ok := d.StatusCodes[501]; ok {
			rs.NotImplemented[k] = true
		}
	}
	return rs
}

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

// ParseRouteKey turns "GET /v1/movies/{id}" into a RouteKey. Paths are compared
// verbatim against the registry, so a row that spells a parameter differently
// fails loudly instead of silently matching nothing.
func ParseRouteKey(s string) (openapi.RouteKey, error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return openapi.RouteKey{}, fmt.Errorf("%q is not \"METHOD /path\"", s)
	}
	method := strings.ToUpper(fields[0])
	if !httpMethods[method] {
		return openapi.RouteKey{}, fmt.Errorf("%q is not an HTTP method", fields[0])
	}
	if !strings.HasPrefix(fields[1], "/") {
		return openapi.RouteKey{}, fmt.Errorf("%q is not a path", fields[1])
	}
	return openapi.RouteKey{Method: method, Path: fields[1]}, nil
}

// coverageShape is the key that matches a registered route to the Trakt
// endpoint it serves, ignoring the /v1 prefix we mount everything under and the
// spelling of path parameters. Trakt spells the media-type segment {type}, we
// spell it {media_type}; both collapse to {}, so an exact-shape route is
// recognised as serving its inventory row whatever the placeholder is called.
//
// The method is part of the key: Trakt's GET and POST reset routes are distinct
// endpoints and must be graded separately.
func coverageShape(method, path string) openapi.RouteKey {
	return openapi.RouteKey{
		Method: strings.ToUpper(method),
		Path:   normalizePath(strings.TrimPrefix(path, "/v1")),
	}
}

// normalizePath collapses every "{param}" segment to "{}" so two paths that
// differ only in parameter names compare equal.
func normalizePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if len(s) >= 2 && strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			parts[i] = "{}"
		}
	}
	return strings.Join(parts, "/")
}

// Problem is one disagreement between the manifest and reality.
type Problem struct {
	Line int
	Msg  string
}

func (p Problem) String() string {
	if p.Line == 0 {
		return p.Msg
	}
	return fmt.Sprintf("line %d: %s", p.Line, p.Msg)
}

// Check grades every row against the registry and returns the disagreements.
//
// The rules, in the order they are applied:
//
//  1. every status is one of the five, every row has a rationale;
//  2. a Trakt endpoint appears once; a blocked-engine row names its engine; a
//     divergent row explains the compat decision; an out-of-scope rationale
//     says whether it was declined ("decision:") or merely not built yet
//     ("deferred:");
//  3. anything named in the ours column must be a registered route;
//  4. a registered route that answers 501 can only be recorded as
//     blocked-engine — and every such route must be recorded somewhere, so a
//     new fabricated-looking 501 cannot slip in unmeasured;
//  5. a registered route that serves can only be graded implemented or
//     divergent (a custom non-Trakt route: out-of-scope).
func Check(m *Manifest, rs RouteSet) []Problem {
	var problems []Problem
	add := func(line int, format string, args ...any) {
		problems = append(problems, Problem{Line: line, Msg: fmt.Sprintf(format, args...)})
	}

	if m.Version != 1 {
		add(0, "manifest version must be 1, got %d", m.Version)
	}
	if len(m.Sections) == 0 {
		add(0, "manifest has no sections")
	}

	seenSection := map[string]int{}
	seenTrakt := map[openapi.RouteKey]int{}
	recorded501 := map[openapi.RouteKey]bool{}
	// traktByShape indexes every inventory row by the shape of the route that
	// would serve it, so a registered route can be looked up to the row it
	// belongs to. See the coverage rule at the end of this function.
	traktByShape := map[openapi.RouteKey]Row{}

	for _, s := range m.Sections {
		if strings.TrimSpace(s.Name) == "" {
			add(s.Line, "section has no name")
		} else if prev, ok := seenSection[s.Name]; ok {
			add(s.Line, "duplicate section %q (first at line %d)", s.Name, prev)
		} else {
			seenSection[s.Name] = s.Line
		}
		if len(s.Rows) == 0 {
			add(s.Line, "section %q has no rows", s.Name)
		}

		for _, r := range s.Rows {
			if !r.Status.valid() {
				add(r.Line, "unknown status %q (expected one of %s)", r.Status, statusNames())
				continue
			}
			if strings.TrimSpace(r.Rationale) == "" {
				add(r.Line, "%s has no rationale", r.label())
			}
			if r.Status == StatusBlockedEngine && strings.TrimSpace(r.Engine) == "" {
				add(r.Line, "%s is blocked-engine but names no engine", r.label())
			}
			if r.Status == StatusDivergent && strings.TrimSpace(r.Compat) == "" {
				add(r.Line, "%s is divergent but has no compat note explaining the bridge", r.label())
			}
			if r.Status == StatusOutOfScope && !r.explainsOutOfScope() {
				add(r.Line,
					"%s is out-of-scope but its rationale starts with neither \"decision:\" (declined), \"deferred:\" (buildable, not built), nor \"custom:\" (our own surface)",
					r.label())
			}

			extra := strings.TrimSpace(r.Trakt) == ""
			if !extra {
				k, err := ParseRouteKey(r.Trakt)
				if err != nil {
					add(r.Line, "trakt column: %v", err)
				} else if prev, ok := seenTrakt[k]; ok {
					add(r.Line, "duplicate Trakt endpoint %s (first at line %d)", k, prev)
				} else {
					seenTrakt[k] = r.Line
					traktByShape[coverageShape(k.Method, k.Path)] = r
				}
			}

			ours := strings.TrimSpace(r.Ours)
			if ours == "" {
				if r.Status == StatusImplemented || r.Status == StatusDivergent {
					add(r.Line, "%s is %s but names no route in the ours column", r.label(), r.Status)
				}
				if extra && r.Status == StatusOutOfScope {
					add(r.Line, "custom-surface row has no route in the ours column")
				}
				continue
			}

			key, err := ParseRouteKey(ours)
			if err != nil {
				add(r.Line, "ours column: %v", err)
				continue
			}
			switch {
			case !rs.Registered[key]:
				add(r.Line, "%s points at %s, which is not in the route registry", r.label(), key)
			case rs.NotImplemented[key]:
				recorded501[key] = true
				if r.Status != StatusBlockedEngine {
					add(r.Line,
						"%s answers 501 not_implemented, so %s can only be blocked-engine, not %s",
						key, r.label(), r.Status)
				}
			case extra:
				if r.Status != StatusOutOfScope {
					add(r.Line, "custom route %s is served, so its row can only be out-of-scope, not %s", key, r.Status)
				}
			default:
				if r.Status != StatusImplemented && r.Status != StatusDivergent {
					add(r.Line,
						"the ours column names the served route %s, so %s can only be implemented or divergent, not %s",
						key, r.label(), r.Status)
				}
			}
		}
	}

	var unrecorded []openapi.RouteKey
	for k := range rs.NotImplemented {
		if !recorded501[k] {
			unrecorded = append(unrecorded, k)
		}
	}
	sort.Slice(unrecorded, func(i, j int) bool { return unrecorded[i].String() < unrecorded[j].String() })
	for _, k := range unrecorded {
		add(0, "registered route %s answers 501 not_implemented but no row records it as blocked-engine", k)
	}

	// Coverage rule: a route we serve must be reflected by its inventory row.
	//
	// The staleness this manifest exists to stop is a route landing while the
	// row that names its Trakt endpoint keeps a blocked/absent grade. Match a
	// registered serving route to the inventory row it belongs to by the shape
	// of its Trakt-side path (see coverageShape) and refuse to let that row
	// say anything other than implemented/divergent.
	//
	// Two deliberate boundaries keep it false-positive free:
	//
	//   * a 501 route is not served — it is the recorded501 rule's business,
	//     not this one, so it is skipped here;
	//   * a route with no matching inventory row is our own surface (section C
	//     rows have no Trakt path, native routes like /v1/movies/trending are
	//     not inventoried as missing). It cannot be matched to a row without a
	//     hand-maintained route->row map, so it is left alone rather than
	//     flagged.
	serving := make([]openapi.RouteKey, 0, len(rs.Registered))
	for k := range rs.Registered {
		if rs.NotImplemented[k] {
			continue
		}
		serving = append(serving, k)
	}
	sort.Slice(serving, func(i, j int) bool { return serving[i].String() < serving[j].String() })
	for _, k := range serving {
		row, ok := traktByShape[coverageShape(k.Method, k.Path)]
		if !ok {
			continue
		}
		if row.Status != StatusImplemented && row.Status != StatusDivergent {
			add(row.Line,
				"registered route %s is served, but its inventory row %s is graded %s — a served route can only be implemented or divergent (the manifest may not under-report what we ship)",
				k, row.Trakt, row.Status)
		}
	}

	return problems
}

func (r Row) label() string {
	if strings.TrimSpace(r.Trakt) != "" {
		return r.Trakt
	}
	if strings.TrimSpace(r.Ours) != "" {
		return r.Ours
	}
	return "row"
}

// explainsOutOfScope reports whether an out-of-scope row says what kind of
// decision it records: declined ("decision:"), buildable but not built
// ("deferred:"), or our own non-Trakt surface ("custom:"). Coverage that cannot
// tell "we will never do this" from "nobody has got to it yet" is not a
// measurement, so the prefix is required.
func (r Row) explainsOutOfScope() bool {
	prefixes := []string{"decision:", "deferred:"}
	if strings.TrimSpace(r.Trakt) == "" {
		prefixes = append(prefixes, "custom:")
	}
	for _, p := range prefixes {
		if strings.HasPrefix(strings.TrimSpace(r.Rationale), p) {
			return true
		}
	}
	return false
}

func statusNames() string {
	names := make([]string, 0, len(AllStatuses()))
	for _, s := range AllStatuses() {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// Coverage is the measurement the tool prints: how many inventoried Trakt
// endpoints we serve, broken down by section and by status. Rows describing our
// own non-Trakt surface are counted separately and excluded from Trakt totals.
type Coverage struct {
	Sections  []SectionCoverage
	ByStatus  map[Status]int
	Trakt     int
	ExtraRows int
}

// SectionCoverage is one section's line in the table.
type SectionCoverage struct {
	Name     string
	Trakt    int
	ByStatus map[Status]int
}

// Implemented returns the number of rows graded implemented in Trakt sections.
func (c Coverage) Implemented() int { return c.ByStatus[StatusImplemented] }

// Divergent returns the number of rows graded divergent.
func (c Coverage) Divergent() int { return c.ByStatus[StatusDivergent] }

// Compute counts a parsed manifest.
func Compute(m *Manifest) Coverage {
	c := Coverage{ByStatus: map[Status]int{}}
	for _, s := range m.Sections {
		sc := SectionCoverage{Name: s.Name, ByStatus: map[Status]int{}}
		for _, r := range s.Rows {
			if strings.TrimSpace(r.Trakt) == "" {
				c.ExtraRows++
				continue
			}
			sc.Trakt++
			sc.ByStatus[r.Status]++
			c.Trakt++
			c.ByStatus[r.Status]++
		}
		c.Sections = append(c.Sections, sc)
	}
	return c
}

// Table renders the coverage report. It is printed unconditionally — the point
// of the manifest is that coverage is visible on every run, not only on failure.
func (c Coverage) Table() string {
	width := len("section")
	for _, s := range c.Sections {
		if len(s.Name) > width {
			width = len(s.Name)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Trakt parity: %d of %d inventoried Trakt endpoints implemented",
		c.Implemented(), c.Trakt)
	if c.Trakt > 0 {
		fmt.Fprintf(&b, " (%.1f%%)", 100*float64(c.Implemented())/float64(c.Trakt))
	}
	fmt.Fprintf(&b, "; %d more served in a divergent shape\n", c.Divergent())

	cols := AllStatuses()
	fmt.Fprintf(&b, "\n%-*s  %5s", width, "section", "trakt")
	for _, s := range cols {
		fmt.Fprintf(&b, "  %12s", shortStatus(s))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "%-*s  %5s", width, strings.Repeat("-", width), "-----")
	for range cols {
		fmt.Fprintf(&b, "  %12s", "------------")
	}
	b.WriteString("\n")

	for _, s := range c.Sections {
		fmt.Fprintf(&b, "%-*s  %5d", width, s.Name, s.Trakt)
		for _, st := range cols {
			fmt.Fprintf(&b, "  %12d", s.ByStatus[st])
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "%-*s  %5d", width, "TOTAL", c.Trakt)
	for _, st := range cols {
		fmt.Fprintf(&b, "  %12d", c.ByStatus[st])
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "\nrows for our own non-Trakt surface (issue #4 section C): %d (excluded above)\n", c.ExtraRows)

	return b.String()
}

func shortStatus(s Status) string { return string(s) }
