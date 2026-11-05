# web — trakt-killer frontend

The React + TypeScript single-page app. It lives in this repository and is
**embedded into the API binary**, so a deploy is one artifact serving both the
UI and `/v1` (see `web/embed.go` and `internal/webui/`).

## Layout

```
web/
  src/
    routes/                  TanStack Router file-based routes
    api/                     typed client + generated schema
    components/              app components (composed from the design kit)
    design-kit-components/   vendored design-kit primitives (see below)
  public/                    static assets and the PWA manifest
  e2e/                       Playwright specs
  dist/                      build output, embedded into the Go binary
  embed.go                   //go:embed all:dist
```

## Commands

Run from `web/`, or use the Makefile targets from the repository root.

| Command | Purpose |
|---|---|
| `pnpm install --frozen-lockfile` | Install dependencies |
| `pnpm dev` | Vite dev server (proxies `/v1` to the backend) |
| `pnpm build` | Typecheck + production build into `dist/` |
| `pnpm test` | Vitest unit tests |
| `pnpm e2e` | Playwright end-to-end tests |
| `pnpm lint` | Biome |

From the root: `make frontend`, `make dev-frontend`, `make test-web`.

## Working on the frontend

`make dev-frontend` expects the API on `localhost:8080` (`make dev-backend`).
`VITE_API_URL` in `.env.local` controls where the client points; leave it as `/`
when serving the built UI from the API process.

After changing API-facing code, regenerate the typed client from the contract —
do not hand-edit `src/api/generated/`.

## Known gaps

Tracked in the repository issues; the ones that bite contributors:

- The generated API schema (`src/api/generated/schema.ts`) is produced from
  `openapi.yaml` and must be regenerated, not edited.
- `src/design-kit-components/` is a vendored copy of the design kit. It is
  **not** the source of truth and has already drifted from it — see the
  design-kit decision in the architecture ADR.
