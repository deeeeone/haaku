.PHONY: help build bin frontend api-client embed-stub dev dev-backend dev-frontend fmt vet test test-go test-web lint lint-web \
	tidy run compose-up compose-down pg-up pg-down pg-status test-db test-db-pins \
	backup backup-list migrate-create migrate-validate migrate-status migrate-up \
	api-spec api-check clean docker-base docker-omnibus docker-lint lint-go vuln secrets workflow-lint check-size check-bidi coverage check parity-check contract-diff check-migration-ledger format-web conformance

# pnpm is expected on PATH (the frontend is a pnpm workspace). Override for a
# checkout that keeps its own toolchain, e.g. make frontend PNPM=/opt/node/bin/pnpm
PNPM ?= pnpm

# Port the Vite dev server listens on. `make dev-backend` redirects UI requests
# here, and `make dev-frontend` binds it with --strictPort, so the two halves
# cannot drift apart when the port is busy. Change it for one run with
# `make dev VITE_PORT=5199`.
VITE_PORT ?= 5173

# The source-built binary the dev targets run. Built once and run as a file
# rather than through `go run`: a killed `go run` leaves its compiled child
# holding the port, which is a nuisance for the process that then wants it.
DEV_BIN := bin/api

# Frontend dependencies are gitignored, so every fresh checkout starts without
# node_modules. Targets that run pnpm depend on this stamp: the first one
# installs, later ones are a no-op, and a changed manifest or lockfile installs
# again. The stamp lives inside node_modules, so deleting that directory
# forces a re-install too.
INSTALL_STAMP := web/node_modules/.install-stamp

# Environment for a source run. `.env` is also read by the omnibus container
# (docker-compose.yml passes it as env_file), so host-specific overrides belong
# in the gitignored `.env.local`, sourced second so it wins. In particular the
# DATABASE_URL/REDIS_URL that `make dev-db` provides must stay out of `.env`:
# a non-empty value there makes the image skip its bundled PostgreSQL/Redis.
# Either file may be absent — a checkout that keeps everything in `.env.local`
# needs no `.env` — and sourcing a missing file would abort the recipe.
ENV_SOURCE = set -a; [ ! -f .env ] || . ./.env; [ ! -f .env.local ] || . ./.env.local; set +a

# Minimum total statement coverage for `make coverage` (merged profile from both
# Go suites). Measured on main at ~24.6%; the floor sits below that so ordinary
# untested additions do not turn main red, but a whole suite going missing does.
# Raise it as coverage improves.
COVERAGE_FLOOR ?= 22

# Goose, pinned so every machine and CI apply the same tool to the same files.
GOOSE     := go run github.com/pressly/goose/v3/cmd/goose@v3.27.1
GOOSE_DIR := migrations/sql

# Where the bundled frontend build lands, and what gets embedded into the binary
# (see web/embed.go).
WEB_DIST := web/dist

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- Frontend ---

$(INSTALL_STAMP): web/package.json web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile
	@touch $@

frontend: $(INSTALL_STAMP) ## Build the UI into web/dist (installs deps if needed)
	cd web && $(PNPM) run build

# The typed client (web/src/api/generated/schema.ts) is generated from the
# contract and committed, so a normal build does not need this. Re-run it after
# changing openapi.yaml; CI checks it stays in step (see the frontend job).
api-client: $(INSTALL_STAMP) ## Regenerate the typed API client from openapi.yaml
	cd web && $(PNPM) run api:generate

# The Go binary embeds web/dist, so every Go build and test needs the directory
# to exist. Tests never serve it, so a placeholder is enough; `make frontend`
# still produces the real bundle. Without this, `go build ./...` fails on a
# checkout that has never run pnpm — which is most of them.
embed-stub: ## Create a placeholder web/dist so Go builds work without a frontend build
	@mkdir -p $(WEB_DIST)
	@[ -e $(WEB_DIST)/index.html ] || printf '%s\n' '<!doctype html><html lang="en"><head><meta charset="utf-8"><title>haaku</title></head><body><p>Frontend not built. Run: make frontend</p></body></html>' > $(WEB_DIST)/index.html

# --- Backend ---

build: embed-stub ## Build all Go packages
	go build ./...

bin: frontend ## Build the real single binary with the frontend embedded
	go build -o $(DEV_BIN) ./cmd/api

# A source run is two processes: the API, and Vite for the UI (`dev-frontend`,
# or both together with `dev`). The API serves no UI of its own in development —
# it redirects UI requests to Vite (HAAKU_DEV_FRONTEND_URL, internal/webui) —
# because web/dist is a build output that only `make frontend` refreshes: serving
# it means serving the frontend as it was at that build, next to a current API.
dev-backend: embed-stub ## Run the API from source (reads .env, then .env.local)
	$(ENV_SOURCE); \
		export HAAKU_DEV_FRONTEND_URL="$${HAAKU_DEV_FRONTEND_URL:-http://localhost:$(VITE_PORT)}"; \
		go run ./cmd/api

dev-frontend: $(INSTALL_STAMP) ## Run the Vite dev server (installs deps if needed)
	cd web && $(PNPM) run dev --port $(VITE_PORT) --strictPort

# Both halves of a source run in one terminal. The API is started first and
# stays in the foreground's background (kill it on Ctrl-C or when Vite exits),
# so there is nothing to stop by hand and no window in which the API answers
# without a UI behind it.
#
# pnpm takes the dev script's arguments with no `--`: passing one hands Vite a
# literal separator (`vite -- --port 15173`), which it ignores — the port is then
# 5173 from vite.config.ts, and --strictPort never applies. Verified by running
# it: with the separator, Vite moved to 5174 and the API redirected to a port
# nothing was listening on.
dev: embed-stub $(INSTALL_STAMP) ## Run the API and the UI together, in one terminal
	@echo "  UI   http://localhost:$(VITE_PORT)   ← open this (Vite, with HMR)"
	@echo "  API  the PORT in .env — it redirects UI requests to the URL above"
	@echo
	$(ENV_SOURCE); \
		export HAAKU_DEV_FRONTEND_URL="$${HAAKU_DEV_FRONTEND_URL:-http://localhost:$(VITE_PORT)}"; \
		go build -o $(DEV_BIN) ./cmd/api || exit 1; \
		stop_tree() { for p in "$$@"; do for c in $$(pgrep -P "$$p" 2>/dev/null); do stop_tree "$$c"; done; kill -TERM "$$p" 2>/dev/null; done; }; \
		./$(DEV_BIN) & api=$$!; \
		( cd web && exec $(PNPM) run dev --port $(VITE_PORT) --strictPort ) & vite=$$!; \
		trap 'stop_tree "$$api" "$$vite"' EXIT INT TERM; \
		while kill -0 "$$api" 2>/dev/null && kill -0 "$$vite" 2>/dev/null; do sleep 0.5; done; \
		if kill -0 "$$api" 2>/dev/null; then wait "$$vite"; status=$$?; \
		else wait "$$api"; status=$$?; fi; \
		stop_tree "$$api" "$$vite"; \
		exit $$status

run: dev-backend ## Alias for dev-backend

# The real thing, locally: the UI is rebuilt into web/dist first, so what the
# binary serves is the current frontend rather than a leftover build. `make dev`
# is the loop for editing the frontend; this is for looking at the bundle the
# image ships.
build-run: bin ## Build the binary with the current UI embedded, then run it
	$(ENV_SOURCE); unset HAAKU_DEV_FRONTEND_URL; ./$(DEV_BIN)

fmt: ## Format Go sources
	gofmt -l -w .

vet: embed-stub ## Run go vet
	go vet ./...

lint: vet lint-web ## Vet the backend and lint the frontend

# The frontend gate is two commands: the formatter check (biome format, no
# writes) and the lint (biome ci --error-on-warnings, so a warning is fatal).
# The `frontend` job in .github/workflows/ci.yml runs both; `make lint-web`
# runs both here. `check` deliberately does not include this — that target has
# no node toolchain (see docs/architecture/ci-gates.md).
lint-web: format-web ## Lint and format-check the frontend
	cd web && $(PNPM) run lint

format-web: $(INSTALL_STAMP) ## Check the frontend is formatted (no writes)
	cd web && $(PNPM) run format:check

# --- Gates (issue #8) ---
#
# CI calls these targets and nothing else, so `make check` locally is the same
# gate CI runs. Tools that must be on PATH: golangci-lint, govulncheck, gitleaks,
# actionlint, shellcheck (versions pinned in .github/workflows/ci.yml).

lint-go: embed-stub ## Lint Go with the committed golangci-lint config
	golangci-lint run ./...
	# _tests_/ is a standalone "command-line-arguments" package (a leading
	# underscore hides it from ./...), so it needs its own invocation.
	golangci-lint run ./_tests_/*.go
	# Files behind //go:build postgres are invisible to the default run; lint the
	# tagged DB-backed suite too (the postgres CI job runs this same invocation).
	golangci-lint run --build-tags postgres ./internal/database/

vuln: embed-stub ## Fail on known vulnerabilities reachable from this code
	govulncheck ./...

secrets: ## Scan the whole git history for committed secrets (gitleaks)
	gitleaks git --no-banner --redact --config .gitleaks.toml

workflow-lint: ## Lint the workflow files (actionlint) and shell scripts (shellcheck)
	actionlint
	shellcheck scripts/*.sh

check-size: ## Size ratchet: files over the soft limit may only shrink
	./scripts/check-size.sh

check-bidi: ## Fail on bidi control characters in tracked text (CVE-2021-42574)
	./scripts/check-bidi.sh

coverage: embed-stub ## Run both Go suites under coverage and enforce COVERAGE_FLOOR
	COVERAGE_OUT=coverage.out COVERAGE_FLOOR=$(COVERAGE_FLOOR) ./scripts/test-go.sh

check: lint-go vuln secrets workflow-lint check-size check-bidi api-check parity-check contract-diff ## Run every gate CI runs

# TODO remove this once we reach parity, we'll branch off and no longer need to check.
# --- Trakt parity manifest (issue #4) ---
#
# docs/parity/parity.yaml grades every Trakt endpoint in issue #4 against the
# same route registry openapi.yaml is generated from, so "how much of Trakt do we
# serve" is measured rather than asserted. cmd/parity prints the coverage table
# and fails when a row disagrees with the registry (a route that does not exist,
# a serving route marked blocked, a 501 route marked implemented, an
# unrecorded 501). See docs/parity/README.md.

parity-check: embed-stub ## Grade docs/parity/parity.yaml against the route registry
	go run ./cmd/parity -check
	python3 scripts/parity-scope.py --check
	go test ./internal/parity/ -run 'TestCommittedManifestMatchesTheRegistry|TestManifestFileIsWhereTheDocsSay' -count=1

# --- Public contract diff (issue #15) ---
#
# api-check proves openapi.yaml matches the code; it says nothing about whether
# the contract a client was written against still holds. This gate compares the
# working tree's openapi.yaml against the frozen baseline in contracts/api/v1/
# and fails when a breaking change (removed/renamed path, removed/renamed
# response field, newly-required request field, narrowed enum, changed type,
# removed enum value, removed status code) is not acknowledged in
# contracts/api/v1/breaking-changes.yaml. It only reads openapi.yaml — it never
# regenerates it. See docs/architecture/api-versioning.md.

contract-diff: embed-stub ## Fail on an unacknowledged breaking change to the public contract
	go run ./cmd/contract-diff -check
	# The tool is a no-op if the classifier is; run the classifier tests too, so
	# a green gate cannot hide a broken diff.
	go test ./internal/contractdiff/ -run 'TestGateOnCommittedContractIsClean|TestDiff_ClassifiesEveryChangeClass' -count=1

# --- Test ---

test: test-go test-web ## Run backend and frontend tests

# One runner for both Go suites: `go test ./...` cannot see _tests_/ (a leading
# underscore hides a directory from the ./... pattern), so the harness used to be
# skipped unless you remembered the second command. See scripts/test-go.sh.
test-go: embed-stub ## Run both Go test suites (./... and the _tests_ harness)
	./scripts/test-go.sh

test-web: $(INSTALL_STAMP) ## Run the frontend test suite
	cd web && $(PNPM) run test

test-db: embed-stub ## Run the postgres-tagged suite against a real PostgreSQL (see scripts/test-db.sh)
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	./scripts/test-db.sh

# --- Trakt conformance (issue #102) ---
#
# Boots a seeded, migrated server on a throwaway PostgreSQL and checks every
# operation docs/parity/parity.yaml grades implemented or divergent against the
# vendored Trakt contract, in both directions (see cmd/trakt-conformance). It is
# a gate, not yet part of `make check`: main would go red on day one because
# conformance finds real response-shape defects on the rows already graded
# implemented. Flip it into `check` once those are fixed; see the PR body.
conformance: embed-stub ## Run the Trakt conformance harness against a seeded server
	./scripts/conformance.sh

# The query budget pins (scripts/ci/db-pins.txt): the hot queries must still be
# served by their dedicated index. Needs a real PostgreSQL because there is no
# plan to inspect without one.
test-db-pins: embed-stub ## Run the query budget pins against a real PostgreSQL
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	go test -tags postgres ./internal/database/ -run TestQueryBudgetPins -count=1

# The migration ledger must match the files on disk: no file without an applied
# row, no row without a file. Expected version/count come from migrations/sql,
# never a constant.
check-migration-ledger: ## Assert the Goose ledger matches migrations/sql (needs DATABASE_URL)
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	./scripts/check-migration-ledger.sh "$$DATABASE_URL"

# --- Migrations (issue #14) ---
#
# The server applies the embedded migrations at startup (internal/database.Migrate),
# so these targets are for humans: authoring, validating offline, and inspecting
# or applying a pending set from a shell. `migrate-up` deliberately runs the
# binary, not the Goose CLI, so it also takes the advisory lock, ensures the
# Goose ledger and writes the pre-migration backup — the Goose CLI does none of
# those.

migrate-create: ## Author a timestamped Goose migration: make migrate-create NAME=add_thing
	@if [ -z "$(NAME)" ]; then echo "usage: make migrate-create NAME=add_thing"; exit 1; fi
	$(GOOSE) -dir $(GOOSE_DIR) create "$(NAME)" sql

migrate-validate: ## Validate the Goose migration files offline (no database needed)
	$(GOOSE) -dir $(GOOSE_DIR) validate

# Reads the Goose ledger directly, so it does not seed a legacy ledger first;
# on an install upgrading from the old numbering, run `make migrate-up` once
# (it bootstraps) and status is accurate from then on.
# Needs DATABASE_URL and, because it goes through config.Load, TMDB_API_KEY.
migrate-status: ## Show migration status against $$DATABASE_URL
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	$(GOOSE) -dir $(GOOSE_DIR) postgres "$$DATABASE_URL" status

migrate-up: ## Apply pending migrations (locks, bootstraps a legacy ledger, backs up)
	go run ./cmd/api --migrate-only

# --- Local PostgreSQL (development and DB-backed tests) ---
#
# Docker is not available everywhere, so these targets run a throwaway cluster
# with the host's own postgres binaries. See scripts/dev-pg.sh.

pg-up: ## Start a throwaway PostgreSQL and print its DATABASE_URL
	./scripts/dev-pg.sh start

pg-down: ## Stop the throwaway PostgreSQL
	./scripts/dev-pg.sh stop

pg-status: ## Show throwaway PostgreSQL status
	./scripts/dev-pg.sh status

# --- OpenAPI specification (issue #15) ---
#
# openapi.yaml is a generated artifact: it is produced from the code-first route
# registry by cmd/openapi, never edited by hand. `api-check` is the gate — it
# fails when the committed spec has drifted from the registry (freshness) or
# when the route inventory differs in either direction.

api-spec: embed-stub ## Regenerate openapi.yaml from the route registry
	go run ./cmd/openapi

api-check: embed-stub ## Verify openapi.yaml is fresh and its route inventory matches the registry
	go run ./cmd/openapi -check
	go test ./internal/httpapi/ -run TestOpenAPIRouteInventory -count=1
# --- Backups (issue #16) ---
#
# One-shot backup commands. They need DATABASE_URL and pg_dump/pg_restore on the
# host; the destination is $HAAKU_BACKUP_DIR (default ./backups). See
# docs/architecture/backups.md.

backup: embed-stub ## Take a manual database backup and exit (needs DATABASE_URL)
	TMDB_API_KEY=dummy go run ./cmd/api --backup

backup-list: embed-stub ## List database backups and exit (needs DATABASE_URL)
	TMDB_API_KEY=dummy go run ./cmd/api --backup-list

# --- Housekeeping ---

tidy: ## Tidy the Go module graph
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin

# --- Dev services (optional) ---

compose-up: ## Start the dev infrastructure (dev/compose.yml: postgres, redis, minio, mailpit)
	docker compose -f dev/compose.yml up -d

compose-down: ## Stop the dev infrastructure
	docker compose -f dev/compose.yml down

dev-db: ## Start only PostgreSQL + Redis (Docker) for a source run
	docker compose -f dev/compose.yml up -d postgres redis

# --- Images (issue #13) ---
#
# CI builds both images (see .github/workflows/docker.yml); these targets are
# the same commands, for a local checkout. The omnibus build takes the omnibus/
# directory as its context, not the repo root.

docker-base: ## Build the base image (haaku:base)
	docker build -t haaku:base \
		--build-arg IMAGE_VERSION="$(shell git rev-parse --short HEAD)" \
		--build-arg BUILD_REVISION="$(shell git rev-parse HEAD)" \
		--build-arg BUILD_DATE="$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" .

docker-omnibus: docker-base ## Build the omnibus image (haaku:omnibus) from the base
	docker build -f omnibus/Dockerfile \
		--build-arg HAAKU_IMAGE=haaku:base \
		-t haaku:omnibus omnibus/

# The init and service scripts carry an s6 shebang (`#!/command/with-contenv
# bash`), so their interpreter has to be named. SC1091 is the "not following a
# sourced file" notice: lib.sh is a path that only exists inside the image.
docker-lint: ## Lint the omnibus shell scripts
	shellcheck -s bash -e SC1091 \
		omnibus/rootfs/etc/s6-overlay/scripts/* \
		omnibus/rootfs/etc/haaku-omnibus/lib.sh \
		omnibus/rootfs/etc/s6-overlay/s6-rc.d/*/run \
		omnibus/rootfs/etc/s6-overlay/s6-rc.d/*/finish
	shellcheck -s sh omnibus/rootfs/etc/s6-overlay/s6-rc.d/*/data/check
