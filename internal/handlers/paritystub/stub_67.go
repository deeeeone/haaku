// Code generated from docs/parity/parity.yaml by gen.py — do not edit by hand;
// re-run the generator after the manifest changes.
package paritystub

import (
	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
)

// registerStub67 wires the Trakt endpoints owned by #67 (lists engine)
// that haaku does not serve yet. Each answers an honest 501 naming the blocker and the
// tracking issue, so a Trakt client gets a real answer instead of a 404.
func registerStub67(api fiber.Router) {
	// GET /people/{id}/lists/{type}/{sort} — blocked-engine: lists engine (issue #5)
	// Tracked in #67: https://github.com/haaku-tv/haaku/issues/67
	api.Get("/v1/people/:id/lists/:type/:sort", stubHandler(67, "GET /people/{id}/lists/{type}/{sort}", "lists engine (issue #5)"))
}

// defsStub67 is the registry entry for each stub above. The 501 in
// StatusCodes is what makes the route grade as blocked-* in the parity manifest and
// appear as a 501 in the generated spec.
func defsStub67() []openapi.RouteDef {
	return []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/people/{id}/lists/{type}/{sort}",
			Summary:     "Parity stub — not implemented (lists engine)",
			Tags:        []string{"People"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — lists engine (issue #5) (#67)"},
		},
	}
}
