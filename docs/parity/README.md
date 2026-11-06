# Trakt parity manifest

`parity.yaml` is the machine-checkable answer to "how much of the Trakt API does
haaku actually serve?". It exists because coverage was being asserted in prose
(issue #4) while the registry and the inventory drifted apart.

- **Trakt side:** the endpoint inventory in
  [haaku-tv/haaku#4](https://github.com/haaku-tv/haaku/issues/4) sections A and B
  (produced by diffing our spec against `trakt/trakt-api` contracts). Rows for
  our own non-Trakt surface (section C) sit in the last section so the map is
  complete; they are excluded from Trakt coverage.
- **Our side:** the live code-first route registry — the same `RouteDef` values
  `openapi.yaml` is generated from. The manifest never keeps its own copy of
  what we serve.

```bash
make parity-check          # gate: fails when the manifest and the registry disagree
go run ./cmd/parity        # same report, exit 0 (for reading during development)
```

## Grading

Every row is one Trakt endpoint with four columns: `trakt`, `ours`, `status`,
`rationale` (plus `engine` for a blocked engine and `compat` for a divergence).

| Status | Meaning |
|---|---|
| `implemented` | a registered route serves this data at the Trakt path |
| `divergent` | a registered route serves this data, but at a different path or with query params where Trakt uses path segments |
| `blocked-engine` | no route serves it; the named engine (issue #5 stats/rankings, #6 sync, lists, activity, recommendations) must land first |
| `blocked-data` | no route serves it; the data is not in our store (missing table/column, or a media type the schema cannot hold) |
| `out-of-scope` | deliberately not served; the rationale starts with `decision:` (declined), `deferred:` (buildable, nobody has built it) or, for our own surface, `custom:` |

An out-of-scope rationale must say *which* kind it is. "We are not doing this"
and "nobody has got to it yet" are different facts, and a manifest that cannot
tell them apart is not a measurement.

## What the gate refuses

`make parity-check` (and `TestCommittedManifestMatchesTheRegistry`) fail when:

- a row marked `implemented` or `divergent` names a route that is not
  registered — the claim is not backed by any handler;
- a row names a registered route whose status contradicts it: a route that
  serves can only be `implemented`/`divergent` (or `out-of-scope` for our own
  non-Trakt surface), and a route that answers `501 not_implemented` can only be
  `blocked-engine`;
- a registered route answers `501` and no row records it as `blocked-engine` —
  new honest-501 stubs have to be measured, not quietly added;
- **a route we serve has an inventory row graded anything other than
  `implemented`/`divergent`** — the manifest may never under-report what we
  ship (see below);
- a `blocked-engine` row names no engine, a `divergent` row has no `compat`
  note, a row has no rationale, or a Trakt endpoint appears twice.

### The no-under-report rule

The staleness this manifest exists to stop is a route landing while the row
that names its Trakt endpoint keeps an old grade. So the gate checks the
registry in the other direction too: for every registered route that *serves*
(anything but a `501`), it looks up the inventory row for that route's
Trakt-shaped path and fails if that row is graded `blocked-engine`,
`blocked-data` or `out-of-scope`.

- "Trakt-shaped path" means our registered path with the `/v1` mount stripped
  and every `{param}` segment collapsed to `{}`, so our `{media_type}` reads the
  same as Trakt's `{type}`. A route is matched to a row by shape+method, not by
  a hand-kept list.
- A `501` route is skipped — it is not served, and the existing "record every
  501 as `blocked-engine`" rule already covers it.
- A serving route with **no** matching inventory row is left alone. That is our
  own surface (`/v1/webhooks`, `/v1/movies/trending`, …), which is deliberately
  not in the Trakt inventory; section C rows have an empty `trakt` column and
  are never matched. Distinguishing "our surface" from "a Trakt endpoint whose
  row is missing entirely" would need a hand-maintained route→row map, which is
  the drift this file is meant to remove — so the rule only fires where a row
  already exists and is graded wrongly. Adding a route in Trakt's *exact* shape
  is therefore always caught; adding one in a divergent shape still needs a
  human to write the `compat` note.

`501` is read from the route definitions themselves (the registration that
declares the `501` response is exactly what puts `501` in `openapi.yaml`), so
the manifest cannot disagree with the generated spec about what is implemented.

## How the file is parsed

YAML with `gopkg.in/yaml.v3`, which the module already depends on for the
OpenAPI inventory (no new dependency). The parser is strict — an unknown key is
an error, not a silently ignored field — and every row keeps its source line so
failures point at the row, not the file. `rationale`, `engine` and `compat` are
quoted strings, so `#` and `:` in prose are safe.

Chosen over JSON because the manifest is reviewable by humans (comments, one row
per endpoint, diffs that read like the issue) and over a hand-written parser
because yaml.v3 is already in the module graph.

## Maintaining it

- A new Trakt endpoint: add the row in the section for it, in the path order the
  issue uses.
- A route lands: flip the row's status and point `ours` at the registered path
  (copy it verbatim from `internal/httpapi/routes.go` / `openapi.yaml`).
- A route is deliberately stubbed with `501`: mark it `blocked-engine` and name
  the engine, or the gate fails.

### When you add a route

`make parity-check` runs in `make check` and in CI, so the fastest path is to
run it after wiring the route:

1. **If the route's path is Trakt's, exactly** (same segments, our `/v1` prefix
   and our `{media_type}` wording aside): the gate will fail with "is served,
   but its inventory row … is graded …". Flip that row to `implemented`, copy
   the registered path into `ours`, and say in the rationale what the handler
   actually returns. Do not mark it `implemented` if the handler returns a
   hard-coded empty payload — implement it or make it an honest `501`; a
   fabricated `200` is the one thing this file will not accept.
2. **If the shape diverges** (an extra `{sort}` segment, a query param where
   Trakt uses a path segment): set the row `divergent` and add the `compat` note
   saying how the listener bridges it. The rule cannot see a route that is only
   *close* to Trakt's path, so this step is on you — run `go run ./cmd/parity`
   and read the table.
3. **If the route is our own surface** (no Trakt equivalent): add a row with an
   empty `trakt` column, `ours` set, and `out-of-scope` with a `custom:`
   rationale — or nothing at all if it is not worth recording; the rule ignores
   routes that match no inventory row either way.
4. **If the route is a new `501`**: add `blocked-engine` with its engine, or the
   gate fails on the unrecorded-501 rule.

Run `go run ./cmd/parity` (exit 0, prints coverage) while you work; run
`make parity-check` before committing.

