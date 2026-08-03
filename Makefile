.PHONY: build test vet run compose-up compose-down tidy

build:
	go build ./...

test:
	go test ./...
	go test ./_tests_/*.go

vet:
	go vet ./...

run:
	set -a; . ./.env; set +a; go run ./cmd/api

build-run:
	set -a; . ./.env; set +a; go build -o bin/api ./cmd/api && ./bin/api

tidy:
	go mod tidy

compose-up:
	docker compose -f deploy/docker-compose.yml up -d

compose-down:
	docker compose -f deploy/docker-compose.yml down
