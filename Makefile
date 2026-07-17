# RadioLedger Makefile

.PHONY: help build test lint test-go test-js lint-web migrate-up migrate-down docker-up docker-down

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the supported API and web artifacts
	cd api && go build -o bin/radioledger-api ./cmd/server
	pnpm --filter radioledger-web build
	pnpm --filter radioledger-docs docs:build
	pnpm --filter site build

test: test-go test-js ## Run Go and JavaScript unit tests

test-go: ## Run all Go module tests (database tests require RADIOLEDGER_TEST_DATABASE_URL)
	cd api && go test ./...
	cd pkg/adif && go test ./...
	cd pkg/gridsquare && go test ./...
	cd pkg/plan && go test ./...
	cd lotw-vault && go test ./...

test-js: ## Run web and desktop unit tests
	pnpm --filter radioledger-web test:unit
	pnpm --filter radioledger-desktop test:unit

lint: lint-web ## Lint supported source packages
	cd api && go vet ./...

lint-web: ## Lint the web application
	pnpm --filter radioledger-web lint

migrate-up: ## Run database migrations up
	goose -dir database/migrations postgres "$$DATABASE_URL" up

migrate-down: ## Roll back last migration (use with care)
	goose -dir database/migrations postgres "$$DATABASE_URL" down

migrate-status: ## Show migration status
	goose -dir database/migrations postgres "$$DATABASE_URL" status

docker-up: ## Start local dev stack
	docker compose -f docker/docker-compose.yml up -d

docker-down: ## Stop local dev stack
	docker compose -f docker/docker-compose.yml down

fuzz-adif: ## Fuzz test the ADIF parser
	cd pkg/adif && go test -fuzz=FuzzADIFParser ./... -fuzztime=60s
