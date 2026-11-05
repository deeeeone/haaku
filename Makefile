.PHONY: help build bin frontend embed-stub dev-backend dev-frontend fmt vet test test-go test-web lint lint-web \
	tidy run compose-up compose-down pg-up pg-down pg-status test-db clean

# pnpm is expected on PATH (the frontend is a pnpm workspace). Override for a
# checkout that keeps its own toolchain, e.g. make frontend PNPM=/opt/node/bin/pnpm
PNPM ?= pnpm

# Where the bundled frontend build lands, and what gets embedded into the binary
# (see web/embed.go).
WEB_DIST := web/dist

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- Frontend ---

frontend: ## Install frontend dependencies and build the UI into web/dist
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) run build

# The Go binary embeds web/dist, so every Go build and test needs the directory
# to exist. Tests never serve it, so a placeholder is enough; `make frontend`
# still produces the real bundle. Without this, `go build ./...` fails on a
# checkout that has never run pnpm — which is most of them.
embed-stub: ## Create a placeholder web/dist so Go builds work without a frontend build
	@mkdir -p $(WEB_DIST)
	@[ -e $(WEB_DIST)/index.html ] || printf '%s\n' '<!doctype html><html lang="en"><head><meta charset="utf-8"><title>trakt-killer</title></head><body><p>Frontend not built. Run: make frontend</p></body></html>' > $(WEB_DIST)/index.html

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

# --- Test ---

test: test-go test-web ## Run backend and frontend tests

test-go: embed-stub ## Run the Go test suites
	go test ./...
	# _tests_/ is a standalone "command-line-arguments" package (underscore dirs
	# are invisible to `go test ./...`), so run it via explicit files.
	go test ./_tests_/*.go

test-web: ## Run the frontend test suite
	cd web && $(PNPM) run test

test-db: embed-stub ## Run tests against a real PostgreSQL (see scripts/dev-pg.sh)
	@test -n "$$DATABASE_URL" || { echo 'DATABASE_URL is not set — run: make pg-up' >&2; exit 1; }
	go test ./...

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
