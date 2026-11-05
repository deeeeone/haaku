// Package web carries the built single-page app as an embedded filesystem.
//
// The frontend lives in this repository (web/) and is compiled into the binary,
// so one artifact serves both /v1 and the UI. The build is wired through the
// Makefile: `make frontend` writes web/dist, and `make embed-stub` writes a
// placeholder when the real bundle is absent so that go build/go test work on a
// checkout that has never run pnpm.
//
// Do not commit a real bundle: web/dist is ignored except for the placeholder.
package web

import "embed"

// DistFS holds the frontend build output under the "dist" directory.
//
// `all:` includes files whose names begin with "." or "_", which the plain
// form skips — Vite emits assets like that, and silently dropping them would
// break the app at runtime.
//
//go:embed all:dist
var DistFS embed.FS
