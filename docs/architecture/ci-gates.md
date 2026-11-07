# CI gates

What CI refuses to let through, how to run the same checks locally, and what is
deliberately not gated yet.

Every gate is a `make` target; the workflow only calls those targets, so
`make check` locally runs exactly what CI runs (given the same tools on PATH).

## The jobs

| Job | Gate | Target |
|---|---|---|
| `build` | `go vet`, `go build`, both Go test suites | `make vet`, `make build`, `make test-go` |
| `lint` | `golangci-lint` with the committed `.golangci.yml` | `make lint-go` |
| `security` | `govulncheck` (reachable vulns), `gitleaks` (secrets, full history) | `make vuln`, `make secrets` |
| `hygiene` | `actionlint`, `shellcheck`, size ratchet, bidi-controls check | `make workflow-lint`, `make check-size`, `make check-bidi` |
| `coverage` | both Go suites under one merged profile, with a floor | `make coverage` |
| `postgres` | DB-backed suite + query budget pins + migration-ledger drift, against a real PostgreSQL service | `make test-db`, `make test-db-pins`, `make check-migration-ledger` |
| `openapi` | `openapi.yaml` freshness + two-way route inventory | `make api-check` |
| `openapi` | Trakt parity manifest graded against the route registry | `make parity-check` |
| `openapi` | public-contract diff vs the frozen baseline; unacknowledged break fails | `make contract-diff` |
| `frontend` | frontend lint, build, tests (incl. typed-client drift) | `make lint-web`, `make test-web` |

`make check` is `lint-go vuln secrets workflow-lint check-size check-bidi
api-check parity-check contract-diff`; the frontend job has its own toolchain and
stays separate.

## The contract diff

`make api-check` proves the committed spec matches the code. It cannot prove the
*contract* still holds for a client written against the previous release: a route
can be removed and the spec regenerated cleanly, and the route inventory still
matches.

`make contract-diff` (`cmd/contract-diff`) compares the working tree's
`openapi.yaml` against the frozen baseline
`contracts/api/v1/openapi-baseline.yaml` and classifies each difference:

- **breaking** — removed/renamed path, removed/renamed response field,
  newly-required request field, narrowed enum, changed type, removed enum value,
  removed status code. Any that is not listed in
  `contracts/api/v1/breaking-changes.yaml` fails the build.
- **additive** — new path, new optional field, new enum value, new status code.
  Reported, never fatal.

The acknowledgement is a committed file, not a flag: an entry carries the
change's exact signature, a date and a one-line reason, so a reviewer sees the
break in the diff. This is what keeps a silent break — the kind the Jellyfin
plugin would hit — out of a release. The tool only reads `openapi.yaml`; it never
regenerates it. See [api-versioning.md](api-versioning.md) for when the baseline
itself is refreshed.

## Two things that surprise people

**`_tests_/` needs its own invocation.** A leading underscore makes a directory
invisible to the `./...` pattern — `go test ./...` never ran the integration
harness, and neither does `golangci-lint run ./...`. Both are run twice
explicitly (`scripts/test-go.sh`, `make lint-go`). Removing that second
invocation silently drops the harness; the coverage floor is the backstop,
because the merged number drops by ~3.6 points when the harness does not run.

**The merged coverage profile needs `-coverpkg=./...`.** Without it each `go test`
run only instruments the package under test, and the harness (whose own files are
test code) contributes nothing. `scripts/test-go.sh` sets it and concatenates the
two `mode: set` profiles into one file.

## The floor

`COVERAGE_FLOOR` in the `Makefile` (currently 22%, measured at ~24.6% when it was
introduced). It is a ratchet, not a target: raise it when coverage improves, and
treat a drop through it as a signal that a suite stopped running.

## The ratchets

- **Size** (`scripts/check-size.sh`, `tools/size-baseline.txt`): a Go file over
  800 lines must be listed in the baseline, and a listed file may only shrink.
  The baseline is meant to be regenerated deliberately when a file is split
  (`scripts/check-size.sh --update`), never raised to accommodate growth.
- **Bidi controls** (`scripts/check-bidi.sh`): fails on Unicode bidirectional
  control characters in tracked text (CVE-2021-42574). Nothing here needs them.

## Worker topology

The background-worker topology (`internal/workers`) is covered by `./...`, not
the `_tests_` harness: the selector, the catalog, panic recovery per pass, prompt
shutdown and the worker health listener are unit tests built from fake workers,
so they need no database and no Redis. The readiness/liveness signal and the
heartbeat metric share this coverage (see [workers.md](workers.md)).

The **modes** themselves — `HAAKU_MODE=worker` refusing to migrate, an
API-only process starting no worker — are asserted on the pure decision
functions (`wantsMigrations`, `workerPlan`) and exercised for real by running the
binary against a throwaway PostgreSQL, which is a manual step because there is
no PostgreSQL- or Redis-backed CI job yet (see below).

## The DB-backed suite

The `postgres` job starts a `postgres:17` service container, applies the embedded
migrations to an empty database with `go run ./cmd/api --migrate-only`, checks the
Goose ledger against the files on disk (`scripts/check-migration-ledger.sh`, which
derives the expected version and row count from `migrations/sql` — never a
constant), then runs the `postgres`-tagged suite and the query budget pins.

Those tests live behind `//go:build postgres` (`internal/database/postgres_*_test.go`)
so `go test ./...` and `make test-go` stay fast and need no server. They `t.Fatal`,
never `t.Skip`, when `DATABASE_URL` is unset: a skipped test reads as a pass, which
is exactly the failure mode this job exists to close. `make test-db` runs
`scripts/test-db.sh`, which scopes the tagged run to the packages that carry a
tagged file — a plain `go test -tags postgres ./...` would drag every
`DATABASE_URL`-sensitive helper test onto a real server, and their postgres paths
are a separate piece of work.

The query budget pins (`scripts/ci/db-pins.txt`, `make test-db-pins`) assert the
hot queries with a dedicated index are still served by it and do not fall back to
a sequential scan.

## Deliberately not gated yet

- `bodyclose` and `errorlint` (golangci-lint): ~56 and ~27 sites respectively,
  almost all in test scaffolding and error type assertions. Each needs its own
  sweep; enabling them now would mean disabling them again to land anything else.
- The `_tests_` harness still runs against sqlite in CI; only the `postgres`-tagged
  `internal/database` suite exercises real PostgreSQL (issue #14 §4). Moving the
  handler/helper tests that flip backend on `DATABASE_URL` onto that job is the
  remaining work.

## Tool versions

Pinned in `.github/workflows/ci.yml`: golangci-lint v2.15.0, gitleaks v8.30.1,
actionlint v1.7.12, govulncheck v1.9.0. Bump them deliberately — a floating linter
turns `main` red without a code change, which is the failure mode this whole file
exists to avoid. The Go toolchain is pinned by `toolchain` in `go.mod`, which is
also how the `crypto/x509` advisory (GO-2026-6809, fixed in go1.26.10) stays
fixed.
