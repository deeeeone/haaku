// Package webui serves the embedded single-page application.
//
// It is mounted last in the middleware chain (internal/httpapi/server.go) so it
// only sees requests no API route handled. API paths are deliberately excluded
// rather than served the SPA shell: an unknown /v1 path must answer with the
// JSON error envelope, never HTML, or every API client breaks at the first typo.
package webui

import (
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trakt-api/web"
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
// Known files are served from the bundle with an inferred content type; every
// other GET is answered with index.html so client-side routes survive a reload
// or a deep link. When the bundle was never built (only the placeholder exists)
// the app still starts and the API still works.
func Handler() (fiber.Handler, error) {
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
