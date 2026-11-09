# trakt-conformance

The gate issue #102 asks for: every operation `docs/parity/parity.yaml` grades
`implemented` or `divergent` is checked against the vendored Trakt contract
(`docs/parity/upstream/trakt-openapi.json`) **in both directions**, against a
live server on a seeded PostgreSQL.

```bash
make conformance            # boots the throwaway cluster, seeds, runs, tears down
```

The target is deliberately **not** part of `make check` yet: the first honest
run finds real response-shape defects on rows already graded `implemented`, so
wiring it in would turn main red on day one. Flip it into `check` once those
defects are fixed.

## What a case checks

Request side (built from the contract, never hand-kept):

- path parameters, including Trakt's id flavours (numeric / slug / imdb / tmdb);
- `extended`, `page`, `limit` and every operation-specific query filter;
- a request body synthesised from the contract's `requestBody` schema (with the
  seeded fixture ids, so items resolve to real rows);
- the contract's headers: `trakt-api-key`, `trakt-api-version: 2`, and
  `Authorization: Bearer` for user-scoped operations.

Response side:

- status is one the contract declares for the operation;
- every field at every level: type, `required` sets, nullability, nested
  objects/arrays, `enum`, and `date-time` formats (milliseconds included);
- the `X-Pagination-*` headers whenever the body is an array — **a convention, not
  a contract statement**: the vendored document declares no response headers, so a
  failure here is labelled `convention` in the output rather than contract;
- error bodies remain a JSON object envelope.

Plus request-side probes for issue #88: malformed JSON keeps the syntax
message, valid-JSON-wrong-shape names the offending field, and a numeric id sent
as a string is accepted.

## Output

A per-operation `PASS`/`FAIL` table and a `N of M graded operations conformant`
summary (M is the graded set, not the 336-operation contract). It exits non-zero
when any case fails. Graded operations with no case are reported
as `NO-CASE` (and `-strict` makes them fail, which is the flip for `make check`).

## Fixtures

`-seed` inserts one movie, one show with a season and episodes, a person and a
credit, one local viewer with history/collection/watchlist/ratings/favorites/
playback, a comment and a list, and writes the resulting ids to
`.conformance-fixtures.json`. The harness reads those ids; it never assumes
bigserial values. `scripts/conformance.sh` destroys the cluster on exit, whatever
the result.