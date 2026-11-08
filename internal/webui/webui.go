// Package webui serves the embedded single-page application.
//
// It is mounted last in the middleware chain (internal/httpapi/server.go) so it
// only sees requests no API route handled. API paths are deliberately excluded
// rather than served the SPA shell: an unknown /v1 path must answer with the
// JSON error envelope, never HTML, or every API client breaks at the first typo.
//
// A source run in development owns no UI: it is paired with the Vite dev server
// (see the Makefile's `dev` target) and hands every UI request to it with a
// redirect instead of serving its own copy of the bundle. See Handler.
package webui

import (
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/haaku-tv/haaku/web"
)

// apiPrefixes are path prefixes owned by the API. A request under one of these
// is passed back to Fiber's router so the shared error handler renders the JSON
// 404 envelope.
var apiPrefixes = []string{"/v1", "/oauth", "/healthz", "/readyz", "/metrics"}

// assetPrefix is the directory Vite writes content-hashed assets to. Those names
// change on every build, so they are safe to cache forever.
const assetPrefix = "assets/"

// isAPIPath reports whether p belongs to the API surface.
func isAPIPath(p string) bool {
	for _, prefix := range apiPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// Handler returns the Fiber handler that serves the embedded UI.
//
// # Development: redirecting instead of serving
//
// When devFrontendURL is non-empty the bundle is NOT served at all. Every UI
// request is answered with a redirect to that URL (path and query preserved),
// so a source run can only ever show the bundle the Vite dev server is building
// right now. This is the difference between a working dev page and a confusing
// one: web/dist is a build output that nothing refreshes, so an API process that
// serves it answers with the frontend as it was at the last `make frontend` —
// old code, against a current API, with no hint that it is stale. A build-time
// frontend switch that no longer matches the running server (the Clerk
// publishable key, for one) then surfaces as an error in the browser instead of
// as a version mismatch in the terminal. `make dev-backend` therefore sets
// HAAKU_DEV_FRONTEND_URL, and the API has no UI of its own to be stale.
//
// # Production: serving the bundle
//
// Otherwise known files are served from the bundle with an inferred content
// type; every other GET is answered with index.html so client-side routes
// survive a reload or a deep link. When the bundle was never built (only the
// placeholder exists) the app still starts and the API still works.
func Handler(devFrontendURL string) (fiber.Handler, error) {
	if devFrontendURL != "" {
		return func(c *fiber.Ctx) error {
			if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
				return c.Next()
			}

			if isAPIPath(c.Path()) {
				return c.Next()
			}

			// Never cached: the redirect is a development pairing, and a
			// cached 302 outlives the dev server it points at.
			c.Set(fiber.HeaderCacheControl, "no-store")
			return c.Redirect(devFrontendURL+c.OriginalURL(), fiber.StatusFound)
		}, nil
	}

	dist, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, err
	}

	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return nil, err
	}

	return func(c *fiber.Ctx) error {
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
			return c.Next()
		}

		if isAPIPath(c.Path()) {
			return c.Next()
		}

		// Resolve a real file in the bundle, rejecting any traversal.
		if name, ok := resolveAsset(c.Path()); ok {
			if body, err := fs.ReadFile(dist, name); err == nil {
				if strings.HasPrefix(name, assetPrefix) {
					c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
				}
				return send(c, name, body)
			}
		}

		// SPA fallback: index.html must never be cached, or a deploy is invisible
		// until the browser cache expires.
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return send(c, "index.html", index)
	}, nil
}

// resolveAsset maps a request path to a bundle path, returning false when the
// path is not a plausible static file.
func resolveAsset(requestPath string) (string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if name == "" || name == "." {
		return "", false
	}
	if name == "index.html" {
		return "", false // always served through the fallback below
	}
	return name, true
}

// send writes body with a content type inferred from name.
func send(c *fiber.Ctx, name string, body []byte) error {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		c.Set(fiber.HeaderContentType, ct)
	} else if strings.HasSuffix(name, ".html") {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	}
	return c.Send(body)
}
