# web — trakt-killer frontend

The React + TypeScript single-page app. It lives in this repository and is
**embedded into the API binary**, so a deploy is one artifact serving both the
UI and `/v1` (see `web/embed.go` and `internal/webui/`).

## Layout

```
web/
  src/
    routes/                  TanStack Router file-based routes
    api/                     typed client, hooks, and the generated schema
      generated/schema.ts    committed openapi-typescript output (do not edit)
    components/              app components (composed from the design kit)
    design-kit-components/   vendored design-kit primitives (see below)
    lib/a11y.ts              keyboard bridge for clickable non-button elements
  public/                    static assets and the PWA manifest
  e2e/                       Playwright specs
  dist/                      build output, embedded into the Go binary
  embed.go                   //go:embed all:dist
```

## Commands

Run from `web/`, or use the Makefile targets from the repository root.

| Command | Purpose |
|---|---|
| `pnpm install --frozen-lockfile` | Install dependencies (pnpm version pinned by `packageManager`) |
| `pnpm dev` | Vite dev server (proxies `/v1` and `/oauth` to the backend) |
| `pnpm build` | Typecheck + production build into `dist/` |
| `pnpm test` | Vitest unit tests (`src/**/*.test.ts`) |
| `pnpm e2e` | Playwright end-to-end tests |
| `pnpm lint` | Biome |
| `pnpm api:generate` | Regenerate `src/api/generated/schema.ts` from `openapi.yaml` |

From the root: `make frontend`, `make api-client`, `make dev-frontend`,
`make test-web`, `make lint-web`.

## Configuration

Vite inlines `VITE_*` variables into the client bundle, so nothing secret belongs
in them. Copy `.env.example` to `.env.local` for local development.

| Variable | Default | Notes |
|---|---|---|
| `VITE_API_URL` | `/` | API base URL. Leave as `/` for the same-origin single-binary deployment; the client falls back to a relative base when unset. |
| `VITE_CLERK_PUBLISHABLE_KEY` | — | Public by design; only the API holds a secret. |
| `VITE_CLERK_PROXY_URL` | unset | Optional Clerk frontend-API proxy. Only set it when a proxy exists (e.g. the dev `clerk-emulator` on `:4000`); with a real Clerk tenant, leave it unset. |

The dev server proxies the API prefixes to `http://localhost:8080` by default;
export `VITE_API_PROXY_TARGET` to point `pnpm dev` at a backend on another host.

## Working on the frontend

`make dev-frontend` proxies `/v1` and `/oauth` to the Go server (`make
dev-backend`), so relative API calls work in development without CORS.

After changing API-facing code, regenerate the typed client from the contract —
do not hand-edit `src/api/generated/`:

```bash
pnpm api:generate        # or: make api-client
```

`src/api/generated/schema.ts` is committed, and `src/api/schema.test.ts` fails if
it drifts from `openapi.yaml` or if the generator never ran.

Unit tests run under Vitest in a Node environment; the Playwright specs in `e2e/`
are excluded by `vite.config.ts` (they need a browser and the built binary).

## Known gaps

Tracked in the repository issues; the ones that bite contributors:

- `src/design-kit-components/` is a vendored copy of the design kit. It is
  **not** the source of truth and has already drifted from it — see the
  design-kit decision in the architecture ADR. There is no `@design-kit` alias;
  the old one pointed at a sibling checkout that does not exist here.
- The contract is thin: most operations emit `requestBody?: never` and
  `content?: never` (trakt-killer/tkiller-api#15), so the generated types only
  cover paths and error envelopes. `src/api/hooks/useLists.ts` therefore casts
  the openapi-fetch methods and those casts surface as Biome
  `noExplicitAny` / `noNonNullAssertion` **warnings** (they do not fail lint).
  Replacing them is blocked on the richer contract.
- `lint/a11y/useSemanticElements` is disabled in `biome.json`: the app's
  clickable surfaces are layout containers wrapping block-level card content,
  where a native `<button>` is not valid HTML. They carry `role="button"` (or
  `role="radio"`), `tabIndex`, and key handling instead.
