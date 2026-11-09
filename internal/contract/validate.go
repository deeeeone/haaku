package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Validate checks a decoded JSON value against an OpenAPI schema and returns
// every violation, each naming the field path and what was expected. An empty
// result means the value conforms at every level.
//
// It resolves $ref against the document's components and understands the shapes
// the vendored Trakt document actually uses: union `type` lists, `nullable`,
// `required`, nested objects/arrays, `enum`, and `oneOf`/`anyOf`/`allOf`.
func (d *Document) Validate(schema map[string]any, value any) []string {
	var problems []string
	d.validate(schema, value, "$", &problems)
	return problems
}

func (d *Document) validate(schema map[string]any, value any, path string, problems *[]string) {
	if schema == nil {
		return
	}
	if ref, ok := schema["$ref"].(string); ok {
		resolved := d.resolveRef(ref)
		if resolved == nil {
			*problems = append(*problems, fmt.Sprintf("%s: unresolvable $ref %s", path, ref))
			return
		}
		d.validate(resolved, value, path, problems)
		return
	}

	// Composition.
	if all, ok := schema["allOf"].([]any); ok {
		for _, sub := range all {
			d.validate(asMap(sub), value, path, problems)
		}
		return
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if subs, ok := schema[key].([]any); ok {
			okCount := 0
			var firstProblems []string
			for _, sub := range subs {
				var subProblems []string
				d.validate(asMap(sub), value, path, &subProblems)
				if len(subProblems) == 0 {
					okCount++
				} else if firstProblems == nil {
					firstProblems = subProblems
				}
			}
			if okCount == 0 {
				*problems = append(*problems, fmt.Sprintf("%s: matches none of the %s alternatives (%s)",
					path, key, strings.Join(firstProblems, "; ")))
			}
			return
		}
	}

	types := schemaTypes(schema)

	// Null handling: explicit null is only allowed by a null type or nullable.
	if value == nil {
		if contains(types, "null") || schema["nullable"] == true {
			return
		}
		*problems = append(*problems, fmt.Sprintf("%s: expected %s, got null", path, expected(types)))
		return
	}

	nonNull := make([]string, 0, len(types))
	for _, t := range types {
		if t != "null" {
			nonNull = append(nonNull, t)
		}
	}
	if len(nonNull) > 0 && !valueMatchesTypes(nonNull, value) {
		*problems = append(*problems, fmt.Sprintf("%s: expected %s, got %s", path, expected(types), valueType(value)))
		return
	}

	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		if !containsEnum(enum, value) {
			*problems = append(*problems, fmt.Sprintf("%s: %s is not one of %s", path, jsonValue(value), enumValues(enum)))
		}
	}

	if f, ok := schema["format"].(string); ok && f == "date-time" {
		if s, ok := value.(string); ok && !isDateTime(s) {
			*problems = append(*problems, fmt.Sprintf("%s: %q is not an RFC3339 date-time", path, s))
		}
	}

	switch v := value.(type) {
	case map[string]any:
		d.validateObject(schema, v, path, problems)
	case []any:
		if items := asMap(schema["items"]); items != nil {
			for i, item := range v {
				d.validate(items, item, fmt.Sprintf("%s[%d]", path, i), problems)
			}
		}
	}
}

func (d *Document) validateObject(schema map[string]any, value map[string]any, path string, problems *[]string) {
	props := asMap(schema["properties"])
	if requiredRaw, ok := schema["required"].([]any); ok {
		for _, r := range requiredRaw {
			name, _ := r.(string)
			if name == "" {
				continue
			}
			if _, present := value[name]; !present {
				*problems = append(*problems, fmt.Sprintf("%s.%s: missing required field", path, name))
			}
		}
	}
	for name, propSchema := range props {
		child, present := value[name]
		if !present {
			continue
		}
		d.validate(asMap(propSchema), child, path+"."+name, problems)
	}
}

// resolveRef follows a local JSON pointer ("#/components/schemas/X").
func (d *Document) resolveRef(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	node := any(d.raw)
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = m[seg]
	}
	return asMap(node)
}

// schemaTypes reads the schema's "type" as a set, defaulting to object when the
// schema has properties and no explicit type.
func schemaTypes(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, v := range t {
			out = append(out, str(v))
		}
		return out
	}
	if _, ok := schema["properties"]; ok {
		return []string{"object"}
	}
	if _, ok := schema["items"]; ok {
		return []string{"array"}
	}
	return nil
}

func valueMatchesTypes(types []string, value any) bool {
	for _, t := range types {
		if valueMatchesType(t, value) {
			return true
		}
	}
	return false
}

func valueMatchesType(t string, value any) bool {
	switch t {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		f, ok := value.(float64)
		return ok && f == math.Trunc(f)
	case "number":
		_, ok := value.(float64)
		return ok
	case "null":
		return value == nil
	}
	return true
}

func valueType(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case nil:
		return "null"
	}
	return "unknown"
}

func expected(types []string) string {
	if len(types) == 0 {
		return "any"
	}
	if len(types) == 1 {
		return types[0]
	}
	return strings.Join(types, " or ")
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func containsEnum(enum []any, value any) bool {
	want := jsonValue(value)
	for _, e := range enum {
		if jsonValue(e) == want {
			return true
		}
	}
	return false
}

func enumValues(enum []any) string {
	parts := make([]string, 0, len(enum))
	for _, e := range enum {
		parts = append(parts, jsonValue(e))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func jsonValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func isDateTime(s string) bool {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// SortedProblems returns problems in a stable order for reporting.
func SortedProblems(problems []string) []string {
	out := append([]string(nil), problems...)
	sort.Strings(out)
	return out
}
