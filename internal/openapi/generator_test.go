package openapi

import (
	"bytes"
	"testing"
)

func sampleRoutes() []RouteDef {
	return []RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/zeta",
			Summary:     "Last alphabetically",
			Tags:        []string{"Sample"},
			Auth:        "none",
			StatusCodes: map[int]string{200: "ok", 404: "missing"},
		},
		{
			Method:      "POST",
			Path:        "/v1/alpha",
			Summary:     "First alphabetically",
			Tags:        []string{"Sample"},
			Auth:        "required",
			StatusCodes: map[int]string{201: "created"},
		},
	}
}

// TestGenerateYAMLBytesIsDeterministic guards the property the freshness check
// depends on: two generations over the same registry must be byte-identical, so
// `git diff --exit-code` only fires on real drift.
func TestGenerateYAMLBytesIsDeterministic(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	for _, r := range sampleRoutes() {
		Register(r)
	}

	first, err := GenerateYAMLBytes()
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	second, err := GenerateYAMLBytes()
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("generation is not deterministic:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestGeneratedSpecOrdersPaths confirms map keys are emitted in sorted order —
// the property that keeps the committed diff stable and reviewable.
func TestGeneratedSpecOrdersPaths(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	for _, r := range sampleRoutes() {
		Register(r)
	}

	out, err := GenerateYAMLBytes()
	if err != nil {
		t.Fatalf("generation: %v", err)
	}
	alpha := bytes.Index(out, []byte("/v1/alpha:"))
	zeta := bytes.Index(out, []byte("/v1/zeta:"))
	if alpha == -1 || zeta == -1 {
		t.Fatalf("expected both paths in output, got:\n%s", out)
	}
	if alpha > zeta {
		t.Fatalf("paths are not sorted: /v1/alpha appears after /v1/zeta")
	}
}

// TestGeneratedSpecIsValidYAML ensures the output parses back into the shape
// SpecInventory expects.
func TestGeneratedSpecIsValidYAML(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	for _, r := range sampleRoutes() {
		Register(r)
	}

	out, err := GenerateYAMLBytes()
	if err != nil {
		t.Fatalf("generation: %v", err)
	}
	spec, err := SpecInventory(out)
	if err != nil {
		t.Fatalf("SpecInventory: %v", err)
	}
	for _, r := range sampleRoutes() {
		key := RouteKey{Method: r.Method, Path: r.Path}
		if !spec[key] {
			t.Errorf("generated spec is missing %s", key)
		}
	}
}

func TestDiffInventoryBothDirections(t *testing.T) {
	registry := map[RouteKey]bool{
		{Method: "GET", Path: "/v1/a"}:  true,
		{Method: "GET", Path: "/v1/b"}:  true,
		{Method: "POST", Path: "/v1/b"}: true,
	}
	spec := map[RouteKey]bool{
		{Method: "GET", Path: "/v1/a"}: true,
		{Method: "GET", Path: "/v1/c"}: true,
	}

	missing, extra := DiffInventory(registry, spec)

	wantMissing := []RouteKey{{Method: "GET", Path: "/v1/b"}, {Method: "POST", Path: "/v1/b"}}
	wantExtra := []RouteKey{{Method: "GET", Path: "/v1/c"}}

	if len(missing) != len(wantMissing) {
		t.Fatalf("missing = %v, want %v", missing, wantMissing)
	}
	for i := range wantMissing {
		if missing[i] != wantMissing[i] {
			t.Errorf("missing[%d] = %v, want %v", i, missing[i], wantMissing[i])
		}
	}
	if len(extra) != len(wantExtra) {
		t.Fatalf("extra = %v, want %v", extra, wantExtra)
	}
	for i := range wantExtra {
		if extra[i] != wantExtra[i] {
			t.Errorf("extra[%d] = %v, want %v", i, extra[i], wantExtra[i])
		}
	}
}

// TestSpecInventoryUppercasesMethods guards against a spec that writes methods
// in lower case (OpenAPI allows it) being reported as drift.
func TestSpecInventoryUppercasesMethods(t *testing.T) {
	doc := []byte("paths:\n  /v1/x:\n    get:\n      responses: {}\n")
	spec, err := SpecInventory(doc)
	if err != nil {
		t.Fatalf("SpecInventory: %v", err)
	}
	if !spec[RouteKey{Method: "GET", Path: "/v1/x"}] {
		t.Fatalf("lower-case method was not normalised: %v", spec)
	}
}
