// Code generated from docs/parity/parity.yaml by gen.py — do not edit by hand;
// re-run the generator after the manifest changes.
package paritystub

import (
	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
)

// registerStubTracker wires the Trakt endpoints owned by #4 (parity tracker)
// that haaku does not serve yet. Each answers an honest 501 naming the blocker and the
// tracking issue, so a Trakt client gets a real answer instead of a 404.
func registerStubTracker(api fiber.Router) {
	// GET /movies/{id}/related — blocked-engine: recommendations engine (issue #5)
	// Tracked in #4: https://github.com/haaku-tv/haaku/issues/4
	api.Get("/v1/movies/:id/related", stubHandler(4, "GET /movies/{id}/related", "recommendations engine (issue #5)"))
	// GET /shows/{id}/related — blocked-engine: recommendations engine (issue #5)
	// Tracked in #4: https://github.com/haaku-tv/haaku/issues/4
	api.Get("/v1/shows/:id/related", stubHandler(4, "GET /shows/{id}/related", "recommendations engine (issue #5)"))
	// DELETE /recommendations/movies/{id} — blocked-engine: recommendations engine (issue #5)
	// Tracked in #4: https://github.com/haaku-tv/haaku/issues/4
	api.Get("/v1/recommendations/movies/:id", stubHandler(4, "DELETE /recommendations/movies/{id}", "recommendations engine (issue #5)"))
	// DELETE /recommendations/shows/{id} — blocked-engine: recommendations engine (issue #5)
	// Tracked in #4: https://github.com/haaku-tv/haaku/issues/4
	api.Get("/v1/recommendations/shows/:id", stubHandler(4, "DELETE /recommendations/shows/{id}", "recommendations engine (issue #5)"))
}

// defsStubTracker is the registry entry for each stub above. The 501 in
// StatusCodes is what makes the route grade as blocked-* in the parity manifest and
// appear as a 501 in the generated spec.
func defsStubTracker() []openapi.RouteDef {
	return []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/movies/{id}/related",
			Summary:     "Parity stub — not implemented (parity tracker)",
			Tags:        []string{"Movies"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#4)"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/related",
			Summary:     "Parity stub — not implemented (parity tracker)",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#4)"},
		},
		{
			Method:      "DELETE",
			Path:        "/v1/recommendations/movies/{id}",
			Summary:     "Parity stub — not implemented (parity tracker)",
			Tags:        []string{"Recommendations"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#4)"},
		},
		{
			Method:      "DELETE",
			Path:        "/v1/recommendations/shows/{id}",
			Summary:     "Parity stub — not implemented (parity tracker)",
			Tags:        []string{"Recommendations"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#4)"},
		},
	}
}
