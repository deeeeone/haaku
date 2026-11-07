package contractdiff

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Class is the kind of difference between two contract documents. The set is
// closed: every difference the tool reports is one of these, so a reviewer can
// reason about the gate without reading its source.
type Class string

const (
	// Breaking classes.
	RemovedPath             Class = "removed-path"
	RenamedPath             Class = "renamed-path"
	RemovedStatusCode       Class = "removed-status-code"
	RemovedResponseField    Class = "removed-response-field"
	RenamedResponseField    Class = "renamed-response-field"
	NewRequiredRequestField Class = "new-required-request-field"
	NarrowedEnum            Class = "narrowed-enum"
	RemovedEnumValue        Class = "removed-enum-value"
	ChangedType             Class = "changed-type"

	// Additive classes.
	AddedPath          Class = "added-path"
	AddedStatusCode    Class = "added-status-code"
	AddedOptionalField Class = "added-optional-field"
	AddedEnumValue     Class = "added-enum-value"
)

// breakingClasses are the classes that break a client written against the
// baseline: a call that worked now 404s, a field a client read is gone or has a
// different type, a value a client may have received is no longer produced, or
// a request the client already sent is now rejected.
var breakingClasses = map[Class]bool{
	RemovedPath:             true,
	RenamedPath:             true,
	RemovedStatusCode:       true,
	RemovedResponseField:    true,
	RenamedResponseField:    true,
	NewRequiredRequestField: true,
	NarrowedEnum:            true,
	RemovedEnumValue:        true,
	ChangedType:             true,
}

// AllClasses lists every class, breaking first, for reports and documentation.
func AllClasses() []Class {
	return []Class{
		RemovedPath, RenamedPath, RemovedStatusCode, RemovedResponseField,
		RenamedResponseField, NewRequiredRequestField, NarrowedEnum,
		RemovedEnumValue, ChangedType,
		AddedPath, AddedStatusCode, AddedOptionalField, AddedEnumValue,
	}
}

// Breaking reports whether a class breaks a client.
func (c Class) Breaking() bool { return breakingClasses[c] }

// Change is one difference between the baseline and the working tree.
type Change struct {
	Class Class
	Op    Operation // the operation the change is on (the old one for a rename)

	NewOp      Operation // renamed-path: where the path moved to
	Scope      string    // "request" | "response", for field changes
	Status     string    // response status code, for response field changes
	Media      string    // media type, for field changes
	Field      string    // dotted field path, for field changes
	NewField   string    // renamed-response-field: the new field path
	Value      string    // removed/addded-enum-value: the member
	OldValue   string    // changed-type: the old type
	NewValue   string    // changed-type: the new type
	StatusCode string    // removed/added-status-code
}

// Breaking reports whether the change breaks clients.
func (c Change) Breaking() bool { return c.Class.Breaking() }

// scopeLabel is "request" or the response status code.
func (c Change) scopeLabel() string {
	if c.Scope == "request" {
		return "request"
	}
	return c.Status
}

// Signature is the stable, human-readable key that identifies this change. It
// is what a maintainer pastes into breaking-changes.yaml to acknowledge a break,
// and what the gate compares against that file.
func (c Change) Signature() string {
	base := fmt.Sprintf("%s %s", c.Class, c.Op)
	switch c.Class {
	case RemovedPath, AddedPath:
		return base
	case RenamedPath:
		return base + " -> " + c.NewOp.Path
	case RemovedStatusCode, AddedStatusCode:
		return base + " " + c.StatusCode
	}

	// Field classes share a prefix and differ in the trailing detail.
	sig := fmt.Sprintf("%s %s %s %s", base, c.scopeLabel(), c.Media, c.Field)
	switch c.Class {
	case RenamedResponseField:
		return sig + " -> " + c.NewField
	case RemovedEnumValue, AddedEnumValue:
		return sig + " " + strconv.Quote(c.Value)
	case ChangedType:
		return sig + " " + c.OldValue + " -> " + c.NewValue
	}
	return sig
}

// Detail is the human sentence that accompanies a signature in the report.
func (c Change) Detail() string {
	switch c.Class {
	case RemovedPath:
		return fmt.Sprintf("path removed: a client calling %s now gets 404", c.Op)
	case RenamedPath:
		return fmt.Sprintf("path moved from %s to %s", c.Op.Path, c.NewOp.Path)
	case AddedPath:
		return fmt.Sprintf("new path %s", c.Op)
	case RemovedStatusCode:
		return fmt.Sprintf("%s no longer returns %s", c.Op, c.StatusCode)
	case AddedStatusCode:
		return fmt.Sprintf("%s may now return %s", c.Op, c.StatusCode)
	case RemovedResponseField:
		return fmt.Sprintf("response field %q removed from %s %s %s", c.Field, c.Op, c.scopeLabel(), c.Media)
	case RenamedResponseField:
		return fmt.Sprintf("response field %q renamed to %q on %s %s %s", c.Field, c.NewField, c.Op, c.scopeLabel(), c.Media)
	case NewRequiredRequestField:
		return fmt.Sprintf("request field %q is now required on %s", c.Field, c.Op)
	case NarrowedEnum:
		return fmt.Sprintf("field %q on %s %s %s is now constrained to an enum", c.Field, c.Op, c.scopeLabel(), c.Media)
	case RemovedEnumValue:
		return fmt.Sprintf("enum value %s removed from %q on %s %s %s", strconv.Quote(c.Value), c.Field, c.Op, c.scopeLabel(), c.Media)
	case ChangedType:
		return fmt.Sprintf("field %q on %s %s %s changed type from %s to %s", c.Field, c.Op, c.scopeLabel(), c.Media, c.OldValue, c.NewValue)
	case AddedOptionalField:
		return fmt.Sprintf("new optional field %q on %s %s %s", c.Field, c.Op, c.scopeLabel(), c.Media)
	case AddedEnumValue:
		return fmt.Sprintf("new enum value %s on %q (%s %s %s)", strconv.Quote(c.Value), c.Field, c.Op, c.scopeLabel(), c.Media)
	}
	return string(c.Class)
}

// Diff classifies every difference between the baseline and the current
// document. The result is sorted: breaking changes first, then by signature.
func Diff(baseline, current *Document) []Change {
	var changes []Change

	changes = append(changes, diffOperations(baseline, current)...)

	// Field-level and status-code diffs only make sense on operations that
	// exist in both documents; an operation's removal is already reported.
	baseOps := opSet(baseline.Operations())
	for _, op := range current.Operations() {
		if !baseOps[op] {
			continue
		}
		changes = append(changes, diffStatusCodes(baseline, current, op)...)
		changes = append(changes, diffFields(baseline, current, op)...)
	}

	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].Breaking() != changes[j].Breaking() {
			return changes[i].Breaking()
		}
		return changes[i].Signature() < changes[j].Signature()
	})
	return changes
}

func opSet(ops []Operation) map[Operation]bool {
	out := make(map[Operation]bool, len(ops))
	for _, op := range ops {
		out[op] = true
	}
	return out
}

// diffOperations reports removed, renamed and added paths. A path that moved is
// paired with its new location so the report says "renamed", not "removed and
// added": the pair shares a method and everything up to the final path segment.
func diffOperations(baseline, current *Document) []Change {
	baseOps := opSet(baseline.Operations())
	curOps := opSet(current.Operations())

	var removed, added []Operation
	for _, op := range baseline.Operations() {
		if !curOps[op] {
			removed = append(removed, op)
		}
	}
	for _, op := range current.Operations() {
		if !baseOps[op] {
			added = append(added, op)
		}
	}

	used := map[Operation]bool{}
	var changes []Change
	for _, r := range removed {
		if match, ok := findRename(r, added, used); ok {
			used[match] = true
			changes = append(changes, Change{Class: RenamedPath, Op: r, NewOp: match})
			continue
		}
		changes = append(changes, Change{Class: RemovedPath, Op: r})
	}
	for _, a := range added {
		if !used[a] {
			changes = append(changes, Change{Class: AddedPath, Op: a})
		}
	}
	return changes
}

// findRename pairs a removed operation with an added one that is the same
// operation at a new address: same method, same parent path, a different final
// segment. An unrelated removal and addition can be paired by this rule, but the
// outcome is correct either way — the path is gone, which is breaking.
func findRename(r Operation, added []Operation, used map[Operation]bool) (Operation, bool) {
	rParent, rLast := splitLast(r.Path)
	for _, a := range added {
		if used[a] || a.Method != r.Method {
			continue
		}
		aParent, aLast := splitLast(a.Path)
		if aParent == rParent && aLast != rLast {
			return a, true
		}
	}
	return Operation{}, false
}

func splitLast(path string) (parent, last string) {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

func diffStatusCodes(baseline, current *Document, op Operation) []Change {
	base := baseline.StatusCodes(op)
	cur := current.StatusCodes(op)

	var changes []Change
	for _, code := range sortedKeys(base) {
		if !cur[code] {
			changes = append(changes, Change{Class: RemovedStatusCode, Op: op, StatusCode: code})
		}
	}
	for _, code := range sortedKeys(cur) {
		if !base[code] {
			changes = append(changes, Change{Class: AddedStatusCode, Op: op, StatusCode: code})
		}
	}
	return changes
}

func diffFields(baseline, current *Document, op Operation) []Change {
	base := baseline.fields(op)
	cur := current.fields(op)

	var changes []Change
	var removedResponse []field
	var addedFields []field

	for _, k := range sortedFieldKeys(base) {
		bf := base[k]
		if cf, ok := cur[k]; ok {
			changes = append(changes, compareField(bf, cf)...)
			continue
		}
		// A removed request field is not breaking: a client that still sends it
		// is ignored by the server. A removed response field is.
		if bf.Scope == "response" {
			removedResponse = append(removedResponse, bf)
		}
	}

	for _, k := range sortedFieldKeys(cur) {
		cf := cur[k]
		if _, ok := base[k]; ok {
			continue
		}
		if cf.Scope == "request" && cf.Required {
			changes = append(changes, Change{
				Class: NewRequiredRequestField, Op: op,
				Scope: "request", Media: cf.Media, Field: cf.Path,
			})
			continue
		}
		addedFields = append(addedFields, cf)
	}

	changes = append(changes, pairResponseRenames(op, removedResponse, addedFields)...)
	return changes
}

// pairResponseRenames turns a removed response field and an added one in the
// same object into a single "renamed" change, and reports the rest as a plain
// removal or addition. Pairing requires exactly one removal and one addition in
// the same parent object, so a genuine rename is recognised without guessing.
func pairResponseRenames(op Operation, removed, added []field) []Change {
	var addedResp []field
	addedCount := map[string]int{}
	for _, f := range added {
		if f.Scope != "response" {
			continue
		}
		addedResp = append(addedResp, f)
		addedCount[groupKey(f)]++
	}
	removedCount := map[string]int{}
	for _, f := range removed {
		removedCount[groupKey(f)]++
	}

	used := map[fieldKey]bool{}
	var changes []Change
	for _, rf := range removed {
		key := groupKey(rf)
		if removedCount[key] == 1 && addedCount[key] == 1 {
			for _, af := range addedResp {
				if groupKey(af) == key && !used[af.key()] {
					used[af.key()] = true
					changes = append(changes, Change{
						Class: RenamedResponseField, Op: op,
						Scope: "response", Status: rf.Status, Media: rf.Media,
						Field: rf.Path, NewField: af.Path,
					})
					break
				}
			}
			continue
		}
		changes = append(changes, Change{
			Class: RemovedResponseField, Op: op,
			Scope: "response", Status: rf.Status, Media: rf.Media, Field: rf.Path,
		})
	}

	// Everything added that was not consumed by a rename is additive.
	for _, af := range added {
		if af.Scope == "response" && used[af.key()] {
			continue
		}
		changes = append(changes, Change{
			Class: AddedOptionalField, Op: op,
			Scope: af.Scope, Status: af.Status, Media: af.Media, Field: af.Path,
		})
	}
	return changes
}

// groupKey identifies the object a response field lives in: two fields with the
// same group key are siblings, so one replacing the other is a rename.
func groupKey(f field) string {
	return f.Scope + "\x00" + f.Status + "\x00" + f.Media + "\x00" + f.parentLocator() + "\x00"
}

// compareField reports the breaking/additive differences between a field that
// exists in both documents.
func compareField(base, cur field) []Change {
	var changes []Change
	add := func(c Change) { changes = append(changes, c) }

	common := Change{Op: base.Op, Scope: base.Scope, Status: base.Status, Media: base.Media, Field: base.Path}

	// Type change: only meaningful when both sides declare a type, so a schema
	// that merely stopped naming its type is not reported as a break.
	if base.Type != "" && cur.Type != "" && base.Type != cur.Type {
		c := common
		c.Class = ChangedType
		c.OldValue = base.Type
		c.NewValue = cur.Type
		add(c)
	}

	switch {
	case !base.HasEnum && cur.HasEnum:
		c := common
		c.Class = NarrowedEnum
		add(c)
	case base.HasEnum && cur.HasEnum:
		for _, v := range removedValues(base.Enum, cur.Enum) {
			c := common
			c.Class = RemovedEnumValue
			c.Value = v
			add(c)
		}
		for _, v := range removedValues(cur.Enum, base.Enum) {
			c := common
			c.Class = AddedEnumValue
			c.Value = v
			add(c)
		}
	}

	// A request field that became required rejects requests the client already
	// sends. A response field becoming required is spec strictness, not a break.
	if base.Scope == "request" && !base.Required && cur.Required {
		c := common
		c.Class = NewRequiredRequestField
		add(c)
	}

	return changes
}

// removedValues returns the members of a that are absent from b.
func removedValues(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	var out []string
	for _, v := range a {
		if !inB[v] {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFieldKeys(m map[fieldKey]field) []fieldKey {
	out := make([]fieldKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		if out[i].Media != out[j].Media {
			return out[i].Media < out[j].Media
		}
		return out[i].Path < out[j].Path
	})
	return out
}
