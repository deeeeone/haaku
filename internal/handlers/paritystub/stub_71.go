// Code generated from docs/parity/parity.yaml by gen.py — do not edit by hand;
// re-run the generator after the manifest changes.
package paritystub

import (
	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
)

// registerStub71 wires the Trakt endpoints owned by #71 (season/episode-level sync)
// that haaku does not serve yet. Each answers an honest 501 naming the blocker and the
// tracking issue, so a Trakt client gets a real answer instead of a 404.
func registerStub71(api fiber.Router) {
	// GET /sync/collection/episodes — blocked-data: user_collection.media_type is 'movie' or 'show' only; an episode collection row cannot be stored.
	// Tracked in #71: https://github.com/haaku-tv/haaku/issues/71
	api.Get("/v1/sync/collection/episodes", stubHandler(71, "GET /sync/collection/episodes", "user_collection.media_type is 'movie' or 'show' only; an episode collection row cannot be stored."))
	// GET /sync/watchlist/seasons — blocked-data: user_watchlist.media_type is 'movie' or 'show' only.
	// Tracked in #71: https://github.com/haaku-tv/haaku/issues/71
	api.Get("/v1/sync/watchlist/seasons", stubHandler(71, "GET /sync/watchlist/seasons", "user_watchlist.media_type is 'movie' or 'show' only."))
	// GET /sync/watchlist/episodes — blocked-data: user_watchlist.media_type is 'movie' or 'show' only.
	// Tracked in #71: https://github.com/haaku-tv/haaku/issues/71
	api.Get("/v1/sync/watchlist/episodes", stubHandler(71, "GET /sync/watchlist/episodes", "user_watchlist.media_type is 'movie' or 'show' only."))
}

// defsStub71 is the registry entry for each stub above. The 501 in
// StatusCodes is what makes the route grade as blocked-* in the parity manifest and
// appear as a 501 in the generated spec.
func defsStub71() []openapi.RouteDef {
	return []openapi.RouteDef{
		{
			Method:      "GET",
			Path:        "/v1/sync/collection/episodes",
			Summary:     "Parity stub — not implemented (season/episode-level sync)",
			Tags:        []string{"Sync"},
			Auth:        "required",
			StatusCodes: map[int]string{501: "Not implemented — user_collection.media_type is 'movie' or 'show' only; an episode collection row cannot be stored. (#71)"},
		},
		{
			Method:      "GET",
			Path:        "/v1/sync/watchlist/seasons",
			Summary:     "Parity stub — not implemented (season/episode-level sync)",
			Tags:        []string{"Sync"},
			Auth:        "required",
			StatusCodes: map[int]string{501: "Not implemented — user_watchlist.media_type is 'movie' or 'show' only. (#71)"},
		},
		{
			Method:      "GET",
			Path:        "/v1/sync/watchlist/episodes",
			Summary:     "Parity stub — not implemented (season/episode-level sync)",
			Tags:        []string{"Sync"},
			Auth:        "required",
			StatusCodes: map[int]string{501: "Not implemented — user_watchlist.media_type is 'movie' or 'show' only. (#71)"},
		},
	}
}
