package contract

import "strings"

// Resolve follows a schema's local $ref and flattens allOf, returning a schema
// map that Validate can read directly. It is used by callers that need to
// inspect the shape (the conformance harness's injection probes) rather than
// validate a value.
func (d *Document) Resolve(schema map[string]any) map[string]any {
	seen := 0
	for schema != nil && seen < 32 {
		seen++
		if ref, ok := schema["$ref"].(string); ok {
			schema = d.resolveRef(ref)
			continue
		}
		if all, ok := schema["allOf"].([]any); ok {
			merged := map[string]any{}
			for _, sub := range all {
				m := d.Resolve(asMap(sub))
				for k, v := range m {
					switch k {
					case "properties":
						mergeProps(merged, v)
						continue
					case "required":
						mergeRequired(merged, v)
						continue
					}
					merged[k] = v
				}
			}
			merged["__resolved_allOf"] = true
			return merged
		}
		break
	}
	return schema
}

// mergeRequired unions the required sets of every allOf branch.
//
// The old loop assigned each branch's `required` over the previous one's, so only
// the LAST branch's required names survived. RequiredNames then under-reported,
// and a response missing a field an earlier branch demanded validated clean -
// the exact class of false negative this harness exists to prevent.
func mergeRequired(dst map[string]any, v any) {
	names, ok := v.([]any)
	if !ok {
		return
	}
	existing, _ := dst["required"].([]any)
	seen := make(map[string]bool, len(existing)+len(names))
	for _, n := range existing {
		if s, ok := n.(string); ok {
			seen[s] = true
		}
	}
	for _, n := range names {
		s, ok := n.(string)
		if !ok || seen[s] {
			continue
		}
		seen[s] = true
		existing = append(existing, s)
	}
	dst["required"] = existing
}

func mergeProps(dst map[string]any, v any) {
	p, ok := v.(map[string]any)
	if !ok {
		return
	}
	existing, _ := dst["properties"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
		dst["properties"] = existing
	}
	for name, ps := range p {
		existing[name] = ps
	}
}

// RequiredNames returns the required property names of an object schema,
// resolving $refs and allOf. It returns nil when the schema is not an object
// with a required set.
func (d *Document) RequiredNames(schema map[string]any) []string {
	s := d.Resolve(schema)
	if s == nil {
		return nil
	}
	if items := asMap(s["items"]); items != nil {
		return d.RequiredNames(items)
	}
	req, _ := s["required"].([]any)
	var out []string
	for _, r := range req {
		if name, ok := r.(string); ok && !strings.HasPrefix(name, "__") {
			out = append(out, name)
		}
	}
	return out
}
