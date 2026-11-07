// Package contractdiff compares two OpenAPI documents and classifies every
// difference as breaking or additive for a client written against the earlier
// one.
//
// It exists because the generated specification was gated for freshness and for
// route inventory, but nothing stopped a pull request from silently removing a
// path, dropping a response field, tightening an enum or making a request field
// required — the exact class of change that breaks the Jellyfin plugin and any
// other third-party client. `cmd/openapi -check` proves the spec matches the
// code; this package proves the *contract* did not change under a client's feet
// without someone writing the change down.
//
// Only the parts of an OpenAPI document a client depends on are read: paths and
// methods, response status codes, and the fields reachable (following local
// `$ref`s) from request bodies and response schemas. The document is parsed into
// generic maps, so the package adds no dependency beyond gopkg.in/yaml.v3,
// already a direct dependency of the module.
package contractdiff

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// httpMethods is the closed set of lowercase keys an OpenAPI path item can
// carry to describe an operation; anything else (parameters, summary) is not an
// operation.
var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// Operation identifies one API operation. Method is always upper-case so two
// documents compare cleanly.
type Operation struct {
	Method string
	Path   string
}

func (o Operation) String() string { return o.Method + " " + o.Path }

// Document is a parsed OpenAPI document. It is a thin view over the raw YAML:
// only paths and components.schemas are retained.
type Document struct {
	Paths      map[string]map[string]any
	Components map[string]any
}

// LoadDocument reads and parses an OpenAPI document from disk.
func LoadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	d, err := ParseDocument(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// ParseDocument parses an OpenAPI document from bytes.
func ParseDocument(data []byte) (*Document, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("openapi document is empty")
	}

	d := &Document{Paths: map[string]map[string]any{}, Components: map[string]any{}}

	pathsRaw, ok := raw["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapi document has no paths")
	}
	for path, itemRaw := range pathsRaw {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}
		d.Paths[path] = item
	}
	if comps, ok := raw["components"].(map[string]any); ok {
		d.Components = comps
	}
	if len(d.Paths) == 0 {
		return nil, fmt.Errorf("openapi document declares no paths")
	}
	return d, nil
}

// Operations returns every operation in the document, sorted.
func (d *Document) Operations() []Operation {
	var out []Operation
	for path, item := range d.Paths {
		for method := range item {
			if httpMethods[strings.ToLower(method)] {
				out = append(out, Operation{Method: strings.ToUpper(method), Path: path})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// operation returns the raw operation map for a key, or nil.
func (d *Document) operation(op Operation) map[string]any {
	item, ok := d.Paths[op.Path]
	if !ok {
		return nil
	}
	raw, ok := item[strings.ToLower(op.Method)]
	if !ok {
		return nil
	}
	m, _ := raw.(map[string]any)
	return m
}

// StatusCodes returns the response codes an operation declares.
func (d *Document) StatusCodes(op Operation) map[string]bool {
	out := map[string]bool{}
	opMap := d.operation(op)
	if opMap == nil {
		return out
	}
	responses, _ := opMap["responses"].(map[string]any)
	for code := range responses {
		out[strings.TrimSpace(code)] = true
	}
	return out
}

const refPrefix = "#/components/schemas/"

// resolve follows a single local $ref to the named schema. It returns the
// schema to inspect and the referenced component name ("" when the schema is
// inline). References are resolved only within the same document: a contract
// diff compares two documents and an external reference would make the two
// incomparable.
func (d *Document) resolve(schema map[string]any) (map[string]any, string) {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema, ""
	}
	name := strings.TrimPrefix(ref, refPrefix)
	if name == ref {
		return schema, ""
	}
	schemas, ok := d.Components["schemas"].(map[string]any)
	if !ok {
		return schema, ""
	}
	target, ok := schemas[name].(map[string]any)
	if !ok {
		return schema, ""
	}
	return target, name
}

// field is one named property reachable from a request or response schema.
type field struct {
	Op       Operation
	Scope    string   // "request" or "response"
	Status   string   // response status code; "" for a request body
	Media    string   // e.g. "application/json"
	Path     string   // dotted path within the schema, e.g. "error.code", "items[].title"
	Name     string   // the property name (the last segment of Path)
	Type     string   // effective JSON type
	HasEnum  bool     // the schema carries an enum
	Enum     []string // the enum members, when HasEnum
	Required bool     // the field is in its parent object's required list
}

type fieldKey struct {
	Scope, Status, Media, Path string
}

func (f field) key() fieldKey {
	return fieldKey{Scope: f.Scope, Status: f.Status, Media: f.Media, Path: f.Path}
}

// parentLocator returns the dotted path of the object that contains a field.
func (f field) parentLocator() string {
	i := strings.LastIndex(f.Path, ".")
	if i < 0 {
		return ""
	}
	return f.Path[:i]
}

// fields indexes every field reachable from an operation's request bodies and
// responses.
func (d *Document) fields(op Operation) map[fieldKey]field {
	opMap := d.operation(op)
	out := map[fieldKey]field{}
	if opMap == nil {
		return out
	}

	var collected []field

	if rb, ok := opMap["requestBody"].(map[string]any); ok {
		d.collectContent(op, "request", "", rb["content"], &collected)
	}
	if responses, ok := opMap["responses"].(map[string]any); ok {
		for code, respRaw := range responses {
			resp, _ := respRaw.(map[string]any)
			if resp == nil {
				continue
			}
			d.collectContent(op, "response", strings.TrimSpace(code), resp["content"], &collected)
		}
	}

	for _, f := range collected {
		out[f.key()] = f
	}
	return out
}

// collectContent walks every media type of a requestBody/response content map.
func (d *Document) collectContent(op Operation, scope, status string, contentRaw any, out *[]field) {
	content, ok := contentRaw.(map[string]any)
	if !ok {
		return
	}
	for media, mediaRaw := range content {
		m, _ := mediaRaw.(map[string]any)
		if m == nil {
			continue
		}
		schema, ok := m["schema"].(map[string]any)
		if !ok {
			continue
		}
		d.collectFields(op, scope, status, media, schema, "", 0, map[string]bool{}, out)
	}
}

// maxDepth bounds schema recursion. A self-referential schema (a comment with
// nested replies, say) would otherwise recurse forever; a contract diff does not
// need to descend indefinitely to know a field was removed.
const maxDepth = 32

// collectFields appends every named property reachable in schema, resolving
// local $refs once per branch. loc is the dotted prefix for the current
// location.
func (d *Document) collectFields(op Operation, scope, status, media string, schema map[string]any, loc string, depth int, seen map[string]bool, out *[]field) {
	if depth > maxDepth || schema == nil {
		return
	}
	resolved, refName := d.resolve(schema)
	if refName != "" {
		if seen[refName] {
			return // cycle: this branch is already being walked
		}
		next := make(map[string]bool, len(seen)+1)
		for k := range seen {
			next[k] = true
		}
		next[refName] = true
		seen = next
	}

	props, _ := resolved["properties"].(map[string]any)
	required := requiredSet(resolved)

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		propRaw := props[name]
		propSchema, _ := propRaw.(map[string]any)
		path := joinPath(loc, name)

		enumVals, hasEnum := enumOf(propSchema)
		*out = append(*out, field{
			Op:       op,
			Scope:    scope,
			Status:   status,
			Media:    media,
			Path:     path,
			Name:     name,
			Type:     effectiveType(propSchema),
			HasEnum:  hasEnum,
			Enum:     enumVals,
			Required: required[name],
		})

		d.collectFields(op, scope, status, media, propSchema, path, depth+1, seen, out)
	}

	// Array element schema, addressed as "<loc>[]".
	if items, ok := resolved["items"].(map[string]any); ok {
		d.collectFields(op, scope, status, media, items, loc+"[]", depth+1, seen, out)
	}
	// additionalProperties object schema, addressed as "<loc>{}".
	if ap, ok := resolved["additionalProperties"].(map[string]any); ok {
		d.collectFields(op, scope, status, media, ap, loc+"{}", depth+1, seen, out)
	}
}

func joinPath(loc, name string) string {
	if loc == "" {
		return name
	}
	if strings.HasSuffix(loc, "[]") || strings.HasSuffix(loc, "{}") {
		return loc + "." + name
	}
	return loc + "." + name
}

func requiredSet(schema map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := schema["required"].([]any)
	for _, v := range list {
		if s, ok := v.(string); ok {
			out[s] = true
		}
	}
	return out
}

// effectiveType returns the schema's JSON type, inferring object/array when the
// type keyword is absent but the shape implies it.
func effectiveType(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	if t, ok := schema["type"].(string); ok && t != "" {
		return t
	}
	if _, ok := schema["properties"].(map[string]any); ok {
		return "object"
	}
	if _, ok := schema["items"].(map[string]any); ok {
		return "array"
	}
	return ""
}

// enumOf extracts an enum, when present. Numbers and booleans are rendered with
// their literal spelling so a contract change in an integer enum is still a
// visible value.
func enumOf(schema map[string]any) ([]string, bool) {
	raw, ok := schema["enum"].([]any)
	if !ok {
		return nil, false
	}
	vals := make([]string, 0, len(raw))
	for _, v := range raw {
		vals = append(vals, fmt.Sprint(v))
	}
	sort.Strings(vals)
	return vals, true
}
