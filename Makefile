.PHONY: help build bin frontend api-client embed-stub dev-backend dev-frontend fmt vet test test-go test-web lint lint-web \
	tidy run compose-up compose-down pg-up pg-down pg-status test-db \
	backup backup-list migrate-create migrate-validate migrate-status migrate-up \
	api-spec api-check clean lint-go vuln secrets workflow-lint check-size check-bidi coverage check

# pnpm is expected on PATH (the frontend is a pnpm workspace). Override for a
# checkout that keeps its own toolchain, e.g. make frontend PNPM=/opt/node/bin/pnpm
PNPM ?= pnpm

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

frontend: ## Install frontend dependencies and build the UI into web/dist
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) run build

# The typed client (web/src/api/generated/schema.ts) is generated from the
# contract and committed, so a normal build does not need this. Re-run it after
# changing openapi.yaml; CI checks it stays in step (see the frontend job).
api-client: ## Regenerate the typed API client from openapi.yaml
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
	go build -o bin/api ./cmd/api

dev-backend: embed-stub ## Run the API from source (needs .env and a database)
	set -a; . ./.env; set +a; go run ./cmd/api

dev-frontend: ## Run the Vite dev server (proxies /v1 to the backend)
	cd web && $(PNPM) run dev

run: dev-backend ## Alias for dev-backend

build-run: embed-stub ## Build and run the binary from source
	set -a; . ./.env; set +a; go build -o bin/api ./cmd/api && ./bin/api

fmt: ## Format Go sources
	gofmt -l -w .

vet: embed-stub ## Run go vet
	go vet ./...

lint: vet lint-web ## Vet the backend and lint the frontend

lint-web: ## Lint the frontend
	cd web && $(PNPM) run lint

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

check: lint-go vuln secrets workflow-lint check-size check-bidi api-check ## Run every gate CI runs

# --- Test ---

test: test-go test-web ## Run backend and frontend tests

# One runner for both Go suites: `go test ./...` cannot see _tests_/ (a leading
# underscore hides a directory from the ./... pattern), so the harness used to be
# skipped unless you remembered the second command. See scripts/test-go.sh.
test-go: embed-stub ## Run both Go test suites (./... and the _tests_ harness)
	./scripts/test-go.sh

test-web: ## Run the frontend test suite
	cd web && $(PNPM) run test

test-db: embed-stub ## Run tests against a real PostgreSQL (see scripts/dev-pg.sh)
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	go test ./...

# --- Migrations (issue #14) ---
#
# The server applies the embedded migrations at startup (internal/database.Migrate),
# so these targets are for humans: authoring, validating offline, and inspecting
# or applying a pending set from a shell. `migrate-up` deliberately runs the
# binary, not the Goose CLI, so it also takes the advisory lock, seeds a legacy
# `schema_versions` ledger and writes the pre-migration backup — the Goose CLI
# does none of those.

migrate-create: ## Author a timestamped Goose migration: make migrate-create NAME=add_thing
	@if [ -z "$(NAME)" ]; then echo "usage: make migrate-create NAME=add_thing"; exit 1; fi
	$(GOOSE) -dir $(GOOSE_DIR) create "$(NAME)" sql

migrate-validate: ## Validate the Goose migration files offline (no database needed)
	$(GOOSE) -dir $(GOOSE_DIR) validate

# Reads the Goose ledger directly, so it does not seed a legacy ledger first;
# on an install upgrading from the old numbering, run `make migrate-up` once
# (it bootstraps) and status is accurate from then on.
# Needs DATABASE_URL and, because it goes through config.Load, TMDB_API_TOKEN.
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
# host; the destination is $TKILLER_BACKUP_DIR (default ./backups). See
# docs/architecture/backups.md.

backup: embed-stub ## Take a manual database backup and exit (needs DATABASE_URL)
	TMDB_API_TOKEN=dummy go run ./cmd/api --backup

backup-list: embed-stub ## List database backups and exit (needs DATABASE_URL)
	TMDB_API_TOKEN=dummy go run ./cmd/api --backup-list

# --- Housekeeping ---

tidy: ## Tidy the Go module graph
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin

# --- Dev services (optional) ---

compose-up: ## Start the dev stack (postgres, redis, minio, mailpit)
	docker compose -f deploy/docker-compose.yml up -d

compose-down: ## Stop the dev stack
	docker compose -f deploy/docker-compose.yml down
