package openapi

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RouteKey identifies a single API operation by method and path. Method is
// always upper-case so the two inventories compare cleanly.
type RouteKey struct {
	Method string
	Path   string
}

func (k RouteKey) String() string { return k.Method + " " + k.Path }

// RegistryInventory returns the operations currently declared in the
// code-first registry.
func RegistryInventory() map[RouteKey]bool {
	inv := make(map[RouteKey]bool)
	for _, r := range AllRoutes() {
		inv[RouteKey{Method: strings.ToUpper(r.Method), Path: r.Path}] = true
	}
	return inv
}

// SpecInventory parses an OpenAPI document and returns the operations it
// declares. Method names are upper-cased for comparison.
func SpecInventory(spec []byte) (map[RouteKey]bool, error) {
	var doc struct {
		Paths map[string]map[string]interface{} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi document: %w", err)
	}
	inv := make(map[RouteKey]bool)
	for path, methods := range doc.Paths {
		for method := range methods {
			inv[RouteKey{Method: strings.ToUpper(method), Path: path}] = true
		}
	}
	return inv, nil
}

// DiffInventory compares a registry inventory against a spec inventory and
// returns the operations that are registered but absent from the spec
// ("missing") and the operations present in the spec with no registered
// handler ("extra"). Both slices are sorted so callers get stable output.
//
// This is the two-way check the freshness diff cannot give on its own: it
// names the exact route that drifted in either direction, which is what makes
// a 9,000-line generated file reviewable.
func DiffInventory(registry, spec map[RouteKey]bool) (missing, extra []RouteKey) {
	for k := range registry {
		if !spec[k] {
			missing = append(missing, k)
		}
	}
	for k := range spec {
		if !registry[k] {
			extra = append(extra, k)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].String() < missing[j].String() })
	sort.Slice(extra, func(i, j int) bool { return extra[i].String() < extra[j].String() })
	return missing, extra
}

// FormatRouteList renders a route list one-per-line for error messages.
func FormatRouteList(routes []RouteKey) string {
	var b strings.Builder
	for _, r := range routes {
		fmt.Fprintf(&b, "\n  %s", r.String())
	}
	return b.String()
}
