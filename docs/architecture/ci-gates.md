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
| `openapi` | `openapi.yaml` freshness + two-way route inventory | `make api-check` |
| `frontend` | frontend lint, build, tests (incl. typed-client drift) | `make lint-web`, `make test-web` |

`make check` is `lint-go vuln secrets workflow-lint check-size check-bidi
api-check`; the frontend job has its own toolchain and stays separate.

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

## Deliberately not gated yet

- `bodyclose` and `errorlint` (golangci-lint): ~56 and ~27 sites respectively,
  almost all in test scaffolding and error type assertions. Each needs its own
  sweep; enabling them now would mean disabling them again to land anything else.
- A Postgres-backed CI job: tracked in issue #14. The `_tests_` harness runs
  against sqlite in CI and only exercises real PostgreSQL when `DATABASE_URL` is
  set, which CI does not do yet.

## Tool versions

Pinned in `.github/workflows/ci.yml`: golangci-lint v2.15.0, gitleaks v8.30.1,
actionlint v1.7.12, govulncheck v1.9.0. Bump them deliberately — a floating linter
turns `main` red without a code change, which is the failure mode this whole file
exists to avoid. The Go toolchain is pinned by `toolchain` in `go.mod`, which is
also how the `crypto/x509` advisory (GO-2026-6809, fixed in go1.26.10) stays
fixed.
