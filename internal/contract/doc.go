// Package contract reads the vendored upstream Trakt OpenAPI document
// (docs/parity/upstream/trakt-openapi.json) and validates a JSON value against
// the response schema an operation declares.
//
// It exists for the conformance harness (cmd/trakt-conformance): a handler
// answering 200 with "close enough" JSON is a fabricated payload with extra
// steps, and the only way to make that measurable is to compare what we send
// against the schema the contract publishes.
//
// The document the harness vendors is a generated mirror of Trakt's own
// contracts; the per-operation index (docs/parity/schema-index.json) is a
// lossy summary of it (union types such as `extended` collapse to
// {"type":["null"]}), so this package always reads the full document and only
// uses the index as a fast path for parameter enumeration.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Document is the vendored OpenAPI document plus an index of its operations.
type Document struct {
	raw   map[string]any
	paths map[string]map[string]any
}

// Operation is one (method, path) pair from the document.
type Operation struct {
	Method  string
	Path    string
	Summary string
	Tags    []string

	raw map[string]any
}

// Load reads and parses the document at path.
func Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read contract: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse contract %s: %w", path, err)
	}
	pathsRaw, ok := raw["paths"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("contract %s: no paths object", path)
	}
	paths := make(map[string]map[string]any, len(pathsRaw))
	for p, item := range pathsRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		paths[normalizePath(p)] = m
	}
	return &Document{raw: raw, paths: paths}, nil
}

// Operations returns every HTTP operation in the document, sorted by method
// and path. Path templates are kept verbatim (e.g. /movies/{id}).
func (d *Document) Operations() []Operation {
	var ops []Operation
	for _, p := range sortedKeys(d.raw["paths"].(map[string]any)) {
		item := d.raw["paths"].(map[string]any)[p].(map[string]any)
		for _, method := range []string{"get", "post", "put", "patch", "delete"} {
			opRaw, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			op := Operation{
				Method:  strings.ToUpper(method),
				Path:    p,
				Summary: str(opRaw["summary"]),
				raw:     opRaw,
			}
			if tags, ok := opRaw["tags"].([]any); ok {
				for _, t := range tags {
					op.Tags = append(op.Tags, str(t))
				}
			}
			ops = append(ops, op)
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Method != ops[j].Method {
			return ops[i].Method < ops[j].Method
		}
		return ops[i].Path < ops[j].Path
	})
	return ops
}

// Operation returns the operation for a method and path template exactly as
// spelled in the document.
func (d *Document) Operation(method, path string) (Operation, bool) {
	item, ok := d.paths[normalizePath(path)]
	if !ok {
		return Operation{}, false
	}
	opRaw, ok := item[strings.ToLower(method)].(map[string]any)
	if !ok {
		return Operation{}, false
	}
	return Operation{Method: strings.ToUpper(method), Path: path, raw: opRaw}, true
}

// Parameters returns the operation's path and query parameters.
func (o Operation) Parameters() []Parameter {
	var out []Parameter
	params, _ := o.raw["parameters"].([]any)
	for _, p := range params {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Parameter{
			Name:     str(pm["name"]),
			In:       str(pm["in"]),
			Required: pm["required"] == true,
			Schema:   asMap(pm["schema"]),
		})
	}
	return out
}

// RequestBodySchema returns the JSON schema of the request body for the first
// declared content type, or nil when the operation takes no body.
func (o Operation) RequestBodySchema() map[string]any {
	rb, ok := o.raw["requestBody"].(map[string]any)
	if !ok {
		return nil
	}
	content, _ := rb["content"].(map[string]any)
	for _, ct := range sortedKeys(content) {
		media, _ := content[ct].(map[string]any)
		if s := asMap(media["schema"]); s != nil {
			return s
		}
	}
	return nil
}

// ResponseSchema returns the JSON schema for a status code ("200", "201", …).
// The bool is false when the operation does not declare that status.
func (o Operation) ResponseSchema(code string) (map[string]any, bool) {
	responses, _ := o.raw["responses"].(map[string]any)
	r, ok := responses[code].(map[string]any)
	if !ok {
		return nil, false
	}
	content, _ := r["content"].(map[string]any)
	for _, ct := range sortedKeys(content) {
		media, _ := content[ct].(map[string]any)
		return asMap(media["schema"]), true
	}
	return map[string]any{}, true
}

// ResponseCodes returns the declared status codes, sorted.
func (o Operation) ResponseCodes() []string {
	responses, _ := o.raw["responses"].(map[string]any)
	return sortedKeys(responses)
}

// Parameter is one declared request parameter.
type Parameter struct {
	Name     string
	In       string
	Required bool
	Schema   map[string]any
}

// Type returns a human-readable type for the parameter schema, collapsing the
// document's union-type lists the way the schema index does.
func (p Parameter) Type() string {
	return TypeName(p.Schema)
}

// JSONSchema helpers ---------------------------------------------------------

// TypeName renders a schema's declared type (a string or a list of strings).
func TypeName(schema map[string]any) string {
	if schema == nil {
		return "unknown"
	}
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		var parts []string
		for _, v := range t {
			parts = append(parts, str(v))
		}
		return strings.Join(parts, "|")
	}
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	return "unknown"
}

func normalizePath(p string) string {
	return p
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
