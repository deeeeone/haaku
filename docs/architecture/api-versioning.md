# API versioning and the generated specification

How this API signals change, what a client is allowed to rely on, and how the
OpenAPI document that describes it is produced and kept honest.

## The specification is a generated artifact

`openapi.yaml` at the repository root is **generated**, not written by hand. It
is produced from the code-first route registry (the `openapi.RouteDef` values
each package registers, plus the inline registrations in
`internal/httpapi/routes.go`) by the tool at `cmd/openapi`.

```bash
make api-spec     # regenerate openapi.yaml
make api-check    # fail if it has drifted from the registry
```

Generation is a deliberate step, **not a side effect of starting the server**.
It used to run on boot inside `cmd/api`, which rewrote `openapi.yaml` into the
working tree on every start and dropped unrelated spec diffs into innocent
pull requests. That is fixed: the server never writes the file.

The output is deterministic — every YAML/JSON object is a Go map and the
marshaller emits keys in sorted order, with no timestamps or version noise — so
`git diff --exit-code openapi.yaml` is a meaningful freshness check.

The generator pins `APP_ENV=development` for its own process, so the committed
document always describes the *full* route surface, including routes the server
only registers outside production (for example `/v1/dev/clerk-webhook-simulate`).
Runtime registration still honours `APP_ENV`; only the generated artifact is
pinned, so it does not depend on the caller's environment.

### Drift detection

Two independent checks, run by `make api-check` and by the `openapi` CI job:

1. **Freshness** — regenerate in memory and compare against the committed
   bytes. Any difference fails.
2. **Route inventory, both directions** — the set of operations in the registry
   must equal the set in the spec. A route registered in code but absent from
   the spec fails; a path in the spec with no registered handler fails. This is
   what makes a 9,000-line generated file reviewable without a human reading it.

If either fails: run `make api-spec` and commit the result.

### The spec over HTTP

`GET /v1/openapi.json` serves the OpenAPI 3.1 document as JSON, generated from
the routes the running process actually serves. Unlike the committed file, this
reflects the live registry, so in production the development-only routes are
absent — which is correct.

## The contract baseline and the break gate

Freshness and the route inventory prove the spec matches the code. They say
nothing about whether the contract a client was written against still holds: a
route can be removed, the spec regenerated, and both checks still pass.

That gap is closed by a frozen copy of the contract and a diff against it:

- **`contracts/api/v1/openapi-baseline.yaml`** — the contract as it stood at the
  last accepted release. A copy of `openapi.yaml`, nothing more.
- **`contracts/api/v1/breaking-changes.yaml`** — every break against that
  baseline, each with its exact signature, a date and a one-line reason.

```bash
make contract-diff    # fail on a breaking change that is not acknowledged
```

`cmd/contract-diff` classifies every difference between the baseline and the
working tree's `openapi.yaml`:

- **breaking** — removed or renamed path; removed or renamed response field;
  newly-required request field; narrowed enum; changed type; removed enum value;
  removed status code.
- **additive** — new path, new optional field, new enum value, new status code.

It fails when a breaking change is not written down in
`breaking-changes.yaml`. The acknowledgement is a committed file, not a flag, so
the break appears in the pull request for a reviewer to see. The tool only
*reads* `openapi.yaml`; it never regenerates it.

### When the baseline is refreshed

Update `openapi-baseline.yaml` **when you accept a set of contract changes into a
release** — when the release notes can say "this is what `/v1` is now":

```bash
cp openapi.yaml contracts/api/v1/openapi-baseline.yaml
```

It is a deliberate, reviewable act, never a step of the build. Additive changes
may be folded in at any release boundary. A breaking change must be acknowledged
first (so the gate is green) and is still against the versioning policy below —
a break inside `/v1` means a new prefix, `/v2`. Once a break is part of the
baseline it drops out of the diff, and its acknowledgement entry may be deleted;
git history on the baseline is the record of accepted breaks. Do not refresh the
baseline to make the gate pass: if it is red, the change is either a bug to
revert or a break that belongs in `breaking-changes.yaml`.

## Versioning policy

**Versioning is by URL prefix: `/v1`.** There is no content-negotiated version
header and no `Accept`-based major version. The prefix is the contract.

Within `/v1`:

- **Additive changes are allowed at any time** and do not require a new prefix:
  new endpoints, new optional request fields, new optional response fields, new
  enum members in a field documented as open, new headers.
- **Breaking changes require a new prefix** (`/v2`). A breaking change is any
  of: removing or renaming an endpoint; removing or renaming a response field;
  tightening a field from optional to required; changing a field's type;
  removing an enum member a client may have received; changing the meaning of an
  existing status code for an existing endpoint.

When `/v2` exists, `/v1` keeps working until a deprecation window has elapsed.
The window is announced in the `/v2` release notes and surfaced to clients
through the repository, **not** through a response code — see below.

## What a client can rely on

- The **path and method** for an endpoint are stable for the life of the
  version prefix. Paths are not silently repurposed.
- Every error response is the standard envelope:
  `{"error":{"code":"...","message":"..."}}`. The presence and shape of
  `error.code` and `error.message` are guaranteed; the set of codes is open and
  grows additively.
- Standard status codes mean their usual things (`400` invalid request, `401`
  unauthenticated, `403` forbidden, `404` not found, `409` conflict, `422`
  unprocessable, `429` rate limited, `500` internal error).
- Rate limiting uses `429` only. A client should treat `429` as retryable after
  a backoff.

## What this API does **not** do

- It **never emits `420`**, and it **never emits an `X-Upgrade-URL` response
  header**. Those are Trakt's proprietary "upgrade your app" signals; this
  project deliberately does not reproduce them (project charter). A client
  written against this API must not expect a forced-upgrade response — version
  changes are announced out of band, not smuggled into a status code.
- Endpoints whose backing engine has not landed yet respond `501 Not
  implemented` rather than failing open. `501` appears in the specification on
  exactly those operations.

## Deferred

The issue that motivated this doc also describes a larger refactor: moving
endpoint definitions out of the Go route registry into a machine-readable
`contracts/api/v1` directory as the single source of truth. That is **not** part
of this change. The `contracts/api/v1` directory now exists, but it holds the
baseline and the acknowledgement record used by the break gate above — not the
route definitions. The generator and its gates remain trustworthy on their own;
making `contracts/` the source of truth is still the follow-up.
