// Package paritystub registers a 501 stub for every Trakt endpoint haaku does not
// serve yet, so the surface is complete and honest: a client asking for an
// unimplemented endpoint gets `501 not_implemented` naming the blocker and the issue
// that tracks it, rather than a 404 that reads as "this endpoint does not exist".
//
// Nothing here serves data. A stub disappears as its owning issue lands: the real
// handler is registered by the owning package and the manifest row moves out of
// blocked-* in the same PR. See docs/parity/parity.yaml and issue #4.
package paritystub

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/internal/openapi"
	"github.com/haaku-tv/haaku/internal/response"
)

func init() {
	for _, r := range routeDefs() {
		openapi.Register(r)
	}
}

// stubHandler answers 501 with the blocker and the issue that tracks the endpoint.
func stubHandler(issue int, trakt, reason string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		return response.NotImplemented(c, fmt.Sprintf(
			"%s is not implemented yet: %s (tracked in #%d — https://github.com/haaku-tv/haaku/issues/%d)",
			trakt, reason, issue, issue))
	}
}

// RegisterRoutes wires every parity stub. It is called FIRST in httpapi.RegisterRoutes
// so a static stub segment (`/v1/movies/watched/{period}`) is matched before the
// `{id}` routes that would otherwise swallow it.
func RegisterRoutes(api fiber.Router) {
	registerStub49(api)
	registerStub66(api)
	registerStub67(api)
	registerStub68(api)
	registerStub69(api)
	registerStub70(api)
	registerStub71(api)
	registerStub105(api)
}

// routeDefs is every stub's registry entry, in a stable order.
func routeDefs() []openapi.RouteDef {
	var all []openapi.RouteDef
	all = append(all, defsStub49()...)
	all = append(all, defsStub66()...)
	all = append(all, defsStub67()...)
	all = append(all, defsStub68()...)
	all = append(all, defsStub69()...)
	all = append(all, defsStub70()...)
	all = append(all, defsStub71()...)
	all = append(all, defsStub105()...)
	return all
}
