// Code generated from docs/parity/parity.yaml by gen.py — do not edit by hand;
// re-run the generator after the manifest changes.
package paritystub

import (
	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
)

// registerStub49 wires the Trakt endpoints owned by #49 (item-to-item related/recommendation engine)
// that haaku does not serve yet. Each answers an honest 501 naming the blocker and the
// tracking issue, so a Trakt client gets a real answer instead of a 404.
func registerStub49(api fiber.Router) {
	// GET /movies/{id}/related — blocked-engine: recommendations engine (issue #5)
	// Tracked in #49: https://github.com/haaku-tv/haaku/issues/49
	api.Get("/v1/movies/:id/related", stubHandler(49, "GET /movies/{id}/related", "recommendations engine (issue #5)"))
	// GET /shows/{id}/related — blocked-engine: recommendations engine (issue #5)
	// Tracked in #49: https://github.com/haaku-tv/haaku/issues/49
	api.Get("/v1/shows/:id/related", stubHandler(49, "GET /shows/{id}/related", "recommendations engine (issue #5)"))
	// DELETE /recommendations/movies/{id} — blocked-engine: recommendations engine (issue #5)
	// Tracked in #49: https://github.com/haaku-tv/haaku/issues/49
	api.Get("/v1/recommendations/movies/:id", stubHandler(49, "DELETE /recommendations/movies/{id}", "recommendations engine (issue #5)"))
	// DELETE /recommendations/shows/{id} — blocked-engine: recommendations engine (issue #5)
	// Tracked in #49: https://github.com/haaku-tv/haaku/issues/49
	api.Get("/v1/recommendations/shows/:id", stubHandler(49, "DELETE /recommendations/shows/{id}", "recommendations engine (issue #5)"))
}

// defsStub49 is the registry entry for each stub above. The 501 in
// StatusCodes is what makes the route grade as blocked-* in the parity manifest and
// appear as a 501 in the generated spec.
func defsStub49() []openapi.RouteDef {
	return []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/movies/{id}/related",
			Summary:     "Parity stub — not implemented (item-to-item related/recommendation engine)",
			Tags:        []string{"Movies"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#49)"},
		},
		{
			Method:      "GET",
			Path:        "/v1/shows/{id}/related",
			Summary:     "Parity stub — not implemented (item-to-item related/recommendation engine)",
			Tags:        []string{"Shows"},
			Auth:        "none",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#49)"},
		},
		{
			Method:      "DELETE",
			Path:        "/v1/recommendations/movies/{id}",
			Summary:     "Parity stub — not implemented (item-to-item related/recommendation engine)",
			Tags:        []string{"Recommendations"},
			Auth:        "required",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#49)"},
		},
		{
			Method:      "DELETE",
			Path:        "/v1/recommendations/shows/{id}",
			Summary:     "Parity stub — not implemented (item-to-item related/recommendation engine)",
			Tags:        []string{"Recommendations"},
			Auth:        "required",
			StatusCodes: map[int]string{501: "Not implemented — recommendations engine (issue #5) (#49)"},
		},
	}
}
