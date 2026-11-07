# Repository layout: one repo, one binary, one image

## Decision

`haaku-tv/haaku` is the **monorepo** for the server:

```
cmd/api/                 the one entrypoint: one binary, several modes
internal/                domain packages
web/                     the React frontend, embedded into the binary
migrations/              SQL migrations (embedded; see issue #14)
api-contracts/           machine-readable API contract (planned; issue #15)
deploy/                  compose files for local services
docs/architecture/       design docs (this directory)
scripts/                 developer tooling
```

The frontend was previously a separate repository (`haaku-tv/tkiller-web`),
imported here with its history preserved. The C# Jellyfin plugin stays in
`haaku-jellyfin`: it targets Jellyfin's ABI and ships through Jellyfin's plugin
catalogue, so it has a genuinely independent release cycle.

## Why

1. **The UI has no independent release.** It changes with the API and has no
   other consumers. A second repository bought a version-skew axis and a
   deployment step for nothing.
2. **Same-origin removes a class of problems.** No cross-origin configuration,
   no split redirect handling, one `/v1` surface for both clients.
3. **One artifact is what self-hosters want.** The install target is a single
   container with its own database (issue #13). That is only possible if the
   binary already carries the UI.
4. **One contract, one generation step.** The frontend's typed client and the
   server's spec come from the same source, so they cannot disagree (issue #15).

The reference is Silo (`Silo-Server/silo-server`), which has the same shape: a Go
server with `web/` embedded, `contracts/`, embedded Goose migrations, and an
"omnibus" image bundling PostgreSQL.

## How the frontend is embedded

- `web/dist` is the frontend build output.
- `web/embed.go` embeds it (`//go:embed all:dist` — `all:` so Vite's dotfiles
  are not silently dropped).
- `internal/webui` serves it: known files with a long cache, everything else
  falls back to `index.html` so client-side routes survive a reload.
- **API paths are excluded from that fallback.** An unknown `/v1/...` must
  answer with the JSON error envelope, never the SPA shell.
- `make embed-stub` writes a placeholder `web/dist/index.html` so `go build` and
  `go test` work on a checkout that has never run pnpm. Only that placeholder is
  tracked; the rest of `dist/` is ignored.

## Modes

One binary runs several roles (`internal/mode`), selected with
`TKILLER_MODE=api|worker|migrate` or `--migrate-only`:

| Mode | Serves HTTP | Runs workers | May migrate |
|---|---|---|---|
| `api` (default) | yes | yes | yes |
| `worker` | no | yes | **no** |
| `migrate` | no | no | yes |

A worker must never change the schema — several run against one database, and a
migration under a running API is a deployment accident rather than a feature.
Encoding that in the mode table means it holds by construction.

**Which** workers a process runs is a second axis: `TKILLER_WORKER` is a
comma-separated list of worker names, or `all` (the default) / `none`. The
default keeps an existing install running the workers in-process; a dedicated
worker deployment sets `TKILLER_WORKER=none` on the API so the same worker is
never started twice. The names, their schedule and the deployment recipes are in
[workers.md](workers.md); the topology itself is `internal/workers`.

## Costs and consequences

- `go build ./...` now depends on `web/dist` existing. The `embed-stub` target
  and the tracked placeholder exist to keep that from being a papercut.
- Contributors who only touch Go still pull the frontend sources. The
  `.dockerignore` and the frontend build stage keep them out of the image.
- The repository is larger and the CI is longer.

## Status

This layout is being filled in. The pieces still missing are tracked as issues:
the migration runner (#14), the omnibus image (#13), the API contract (#15), and
backups plus at-rest secret encryption (#16).
