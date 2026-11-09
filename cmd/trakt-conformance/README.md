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

## Cases come from the seed, not from literals

Every fixture-backed value — a path id, an `ids.tmdb` in a body, an OAuth
client id or grant code — is read from `.conformance-fixtures.json`, which
`-seed` writes with the ids the inserted rows actually landed on. A case never
pins a constant that happens to exist: `TestCasesFollowFixtureIDs` generates the
same operation against two disjoint fixture sets and requires the requests to
differ, so changing a seeded id changes the request.

Path parameters resolve the way the route resolves them:

- numeric local id for `/movies/{id}`, `/shows/{id}`, `/users/{id}/...`;
- slug / `imdb` / `tmdb` / `tvdb` where the route names an external flavour
  (including `/search/{id_type}/{id}`);
- a distinct target account for follow/block (the handlers refuse
  `target == caller`), and distinct rows for the destructive DELETE cases, so a
  case cannot consume the row a later read asserts against;
- enums (search `type`, comment `comment_type`, calendar `section`, list sort)
  come from the contract's own parameter schema before any default.

Request bodies are synthesised from the contract's `requestBody` schema with
the seeded ids, including media-type context (`ids.tmdb` under `show` is the
show's tmdb id, not the movie's). OAuth gets a real seeded application and
authorization-code grant; the device grant is process-local in the API, so the
harness creates and authorizes one through `/oauth/device/code` and
`/oauth/device/verify` before the cases run.

## What a case checks

Request side (built from the contract, never hand-kept):

- path parameters, including Trakt's id flavours (numeric / slug / imdb / tmdb);
- `extended`, `page`, `limit` and every operation-specific query filter;
- a request body synthesised from the contract's `requestBody` schema;
- the contract's headers: `trakt-api-key`, `trakt-api-version: 2`, and
  `Authorization: *** for user-scoped operations.

Response side:

- status is one the contract declares for the operation;
- every field at every level: type, `required` sets, nullability, nested
  objects/arrays, `enum`, and `date-time` formats (milliseconds included);
- the `X-Pagination-*` headers whenever the body is an array — **a convention,
  not a contract statement**: the vendored document declares no response
  headers, so a failure here is labelled `header (convention)`;
- an empty array never satisfies a list-shaped assertion: it is reported as an
  `empty` failure instead of passing because there was nothing to validate;
- error bodies remain a JSON object envelope.

Plus two probe suites:

- request-side (#88): malformed JSON keeps the syntax message,
  valid-JSON-wrong-shape names the offending field, and a numeric id sent as a
  string is accepted;
- response-side injection (#102): take a live passing payload, rename one
  required field, and separately delete one; both mutations must fail
  validation. A gate that cannot fail is not evidence.

## Output

A per-operation `PASS`/`FAIL` table, the probe tables, and:

```
N of M graded operations conformant
graded operations: M  (conformant N, failing F, no case K)
failures by cause: status=… header=… field=… envelope=… empty=… harness=…
not graded (out of scope): …
```

`M` is the graded set, never the whole 336-operation contract. Every failure
carries its cause. `harness` is the one cause that means the fixture/case side
is wrong; a run quoting failures as defects must have `harness=0`. Graded
operations with no case are `NO-CASE` (`-strict` makes them fail, which is the
flip for `make check`).

## Fixtures

`-seed` inserts two movies, a show with two seasons and three episodes, a
person and a credit, a network, one local viewer plus three target accounts,
history/collection/watchlist/ratings/favorites/playback, comments (including a
replied/liked thread, season and episode comments, and a throwaway row for
DELETE), a list with movie and episode items, a list like, a smart list, notes
(plus a throwaway), hidden items, and an OAuth application plus authorization
grant. It writes the resulting ids to `.conformance-fixtures.json`. The seed is
idempotent: a second run writes byte-identical ids. `scripts/conformance.sh`
destroys the private cluster on exit, whatever the result.