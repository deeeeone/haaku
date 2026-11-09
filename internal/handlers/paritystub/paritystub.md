# Parity stubs (`internal/handlers/paritystub`)

## What it does

Registers a route for **every Trakt endpoint haaku does not serve yet** — every
`blocked-engine` / `blocked-data` row in `docs/parity/parity.yaml` — and answers
an honest `501 not_implemented` naming the blocker and the issue that tracks it:

```
GET /v1/shows/streaming/30d
{"error":{"code":"not_implemented","message":"GET /shows/streaming/{period} is
 not implemented yet: STOP (no honest source) (tracked in #69 —
 https://github.com/haaku-tv/haaku/issues/69)"}}
```

Before this package those paths answered `404`, which reads as "this endpoint does
not exist" — wrong for a Trakt client, and it made the gap invisible in the spec.
A `404` says the *route* is missing; `501` says the *data* is. That distinction is
the whole point, and it is the same rule the media module's stats/lists/watching
stubs already follow (a fabricated `200` is worse than an honest `501`).

## Generation

The files are **generated from the manifest**, never hand-edited:

```
python3 scripts/parity-stubs.py   # writes stub_<issue>.go, paritystub.go, and the `ours` column
make api-spec api-client  # openapi.yaml + web/src/api/generated/schema.ts
```

Each `stub_<issue>.go` holds the endpoints owned by one sub-issue of #4, with a
per-route comment naming the manifest row, the blocker and the issue URL. When a
group's engine lands, its stub file is deleted and the real handlers are
registered by the owning package — the manifest row moves out of `blocked-*` in the
same PR, which is what `make parity-check` measures.

## Route order

`RegisterRoutes` is called **first** in `httpapi.RegisterRoutes`, before the
parameterised routes. Several stubs are static segments sitting at a depth where a
real package has a `:param` route — `/v1/movies/watched/{period}` against
`/v1/movies/{id}` — so registering them later would leave them unreachable
(Fiber matches in registration order). `TestNoStaticRouteIsShadowed` guards the
general rule across the whole app; do not move this registration without moving
the stubs into the owning packages instead.

## Completion

| Piece | Status |
|---|---|
| One stub per blocked manifest row | Done |
| Per-route comment + runtime message naming the tracking issue | Done |
| `501` in every RouteDef, so the spec and the manifest agree | Done |
| `make parity-check` grades each stub as `blocked-*`, never `implemented` | Done |
| Grouped by owning issue, so a group can delete its file when it lands | Done |

## Notes for whoever picks one up

- **Do not delete a stub to "clean up"** — the manifest row would then name a route
  that is not registered and `make parity-check` fails. Delete the stub **and**
  re-grade its row in the same PR that makes the real handler serve.
- Adding an endpoint is a two-file change (`gen.py` output + the manifest). Re-run
  the generator rather than editing a `stub_*.go` by hand; a hand edit is lost on
  the next run.
- The stubs declare `Auth: "none"` and answer `501` regardless of credentials: they
  serve nothing, so there is nothing to protect. The real handler applies its own
  auth when it lands.
