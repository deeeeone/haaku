package httpapi

import (
	"os"
	"testing"

	"github.com/haaku-tv/haaku/internal/openapi"

	"github.com/gofiber/fiber/v2"
)

// TestOpenAPIRouteInventory is the two-way inventory gate. It registers every
// route exactly as the server does, then compares the registry against the
// committed openapi.yaml:
//
//   - a route registered in code but absent from the spec fails (someone added
//     an endpoint and forgot to regenerate), and
//   - a path in the spec with no registered handler fails (the spec advertises
//     something the router does not serve).
//
// This is what keeps a 9,000-line generated file honest without a human
// reading it. It is also the check `make api-check` runs.
func TestOpenAPIRouteInventory(t *testing.T) {
	// The registry is populated by package init() functions (each handler
	// package registers its RouteDefs in init) plus the inline registrations in
	// RegisterRoutes, so we must NOT Reset() here — that would discard the
	// init-populated entries. RegisterRoutes is called once; RegistryInventory
	// de-duplicates, so repeat calls would be harmless anyway.
	RegisterRoutes(fiber.New())

	specBytes, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	spec, err := openapi.SpecInventory(specBytes)
	if err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}

	missing, extra := openapi.DiffInventory(openapi.RegistryInventory(), spec)
	if len(missing) > 0 {
		t.Errorf("registered routes missing from openapi.yaml (run `make api-spec`):%s",
			openapi.FormatRouteList(missing))
	}
	if len(extra) > 0 {
		t.Errorf("openapi.yaml declares routes with no registered handler (stale spec — run `make api-spec`):%s",
			openapi.FormatRouteList(extra))
	}
}
