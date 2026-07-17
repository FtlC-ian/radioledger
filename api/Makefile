# RadioLedger API — Makefile
# Uses Go from PATH so contributors and CI do not depend on one developer's Mac.
GO      := go
BINARY  := radioledger-api
MAIN    := ./cmd/server
SQLC_VERSION := v1.30.0

.PHONY: build run test lint fmt tidy sqlc clean help

## build: Compile the API server binary to ./bin/radioledger-api
build:
	$(GO) build -o bin/$(BINARY) $(MAIN)

## run: Run the API server (requires DATABASE_URL env var)
run:
	$(GO) run $(MAIN)

## test: Run all tests with race detector
test:
	$(GO) test -race -count=1 ./...

## test-verbose: Run all tests with verbose output and race detector
test-verbose:
	$(GO) test -race -count=1 -v ./...

## lint: Run golangci-lint (install: brew install golangci-lint)
lint:
	golangci-lint run ./...

## fmt: Format all Go source files
fmt:
	$(GO) fmt ./...

## tidy: Tidy go.mod and go.sum
tidy:
	$(GO) mod tidy

## sqlc: Regenerate database query code with the pinned sqlc version
sqlc:
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

## vet: Run go vet
vet:
	$(GO) vet ./...

## clean: Remove compiled binaries
clean:
	rm -rf bin/

## help: Show this help message
help:
	@grep -E '^## ' Makefile | sed 's/## //'
