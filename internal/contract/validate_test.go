package contract

import (
	"encoding/json"
	"testing"
)

func docWithSchema(t *testing.T, schema map[string]any) *Document {
	t.Helper()
	raw := map[string]any{
		"openapi": "3.1.0",
		"paths":   map[string]any{},
		"components": map[string]any{
			"schemas": map[string]any{"Thing": schema},
		},
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	return &Document{raw: decoded, paths: map[string]map[string]any{}}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidateRequiredField(t *testing.T) {
	d := docWithSchema(t, map[string]any{
		"type":     []any{"object"},
		"required": []any{"title", "ids"},
		"properties": map[string]any{
			"title": map[string]any{"type": []any{"string"}},
			"ids": map[string]any{
				"type":     []any{"object"},
				"required": []any{"trakt"},
				"properties": map[string]any{
					"trakt": map[string]any{"type": []any{"integer"}},
				},
			},
		},
	})
	problems := d.Validate(mapOfAny(d.raw, "components", "schemas", "Thing"), decode(t, `{"title":"x","ids":{}}`))
	if len(problems) != 1 || problems[0] != "$.ids.trakt: missing required field" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidateNullabilityAndType(t *testing.T) {
	d := docWithSchema(t, map[string]any{
		"type": []any{"object"},
		"properties": map[string]any{
			"year":  map[string]any{"type": []any{"integer", "null"}},
			"title": map[string]any{"type": []any{"string"}},
		},
	})
	schema := mapOfAny(d.raw, "components", "schemas", "Thing")
	if p := d.Validate(schema, decode(t, `{"year":null,"title":"x"}`)); len(p) != 0 {
		t.Fatalf("nullable integer rejected: %v", p)
	}
	problems := d.Validate(schema, decode(t, `{"year":"1999","title":"x"}`))
	if len(problems) != 1 || problems[0] != "$.year: expected integer or null, got string" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidateArrayItems(t *testing.T) {
	d := docWithSchema(t, map[string]any{
		"type":  []any{"array"},
		"items": map[string]any{"type": []any{"integer"}},
	})
	schema := mapOfAny(d.raw, "components", "schemas", "Thing")
	problems := d.Validate(schema, decode(t, `[1,"two"]`))
	if len(problems) != 1 || problems[0] != "$[1]: expected integer, got string" {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidateDateTimeFormat(t *testing.T) {
	d := docWithSchema(t, map[string]any{
		"type": []any{"object"},
		"properties": map[string]any{
			"updated_at": map[string]any{"type": []any{"string"}, "format": "date-time"},
		},
	})
	schema := mapOfAny(d.raw, "components", "schemas", "Thing")
	if p := d.Validate(schema, decode(t, `{"updated_at":"2026-11-09T12:00:00.000Z"}`)); len(p) != 0 {
		t.Fatalf("millisecond timestamp rejected: %v", p)
	}
	if p := d.Validate(schema, decode(t, `{"updated_at":"yesterday"}`)); len(p) != 1 {
		t.Fatalf("non-timestamp accepted: %v", p)
	}
}

func TestValidateOneOf(t *testing.T) {
	d := docWithSchema(t, map[string]any{
		"oneOf": []any{
			map[string]any{"type": []any{"object"}, "required": []any{"movie"}},
			map[string]any{"type": []any{"object"}, "required": []any{"show"}},
		},
	})
	schema := mapOfAny(d.raw, "components", "schemas", "Thing")
	if p := d.Validate(schema, decode(t, `{"show":{}}`)); len(p) != 0 {
		t.Fatalf("matching alternative rejected: %v", p)
	}
	if p := d.Validate(schema, decode(t, `{"other":{}}`)); len(p) == 0 {
		t.Fatal("no-alternative object accepted")
	}
}

func mapOfAny(root map[string]any, path ...string) map[string]any {
	node := root
	for _, seg := range path {
		next, ok := node[seg].(map[string]any)
		if !ok {
			return nil
		}
		node = next
	}
	return node
}
