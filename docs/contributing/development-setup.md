# Development Setup

> Clone, build, and run RadioLedger locally for development.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.23+ | API server |
| Node.js | 20+ | Web UI |
| Docker + Compose | Latest | Database, Zitadel |
| sqlc | 1.27+ | SQL code generation |
| goose | 3.x | Database migrations |
| golangci-lint | 1.60+ | Go linting |
| Rust | 1.80+ (optional) | Desktop client (Tauri) |

## Clone the Repository

```bash
git clone https://github.com/FtlC-ian/radioledger.git
cd radioledger
```

## Start Infrastructure

```bash
# Start PostgreSQL and Zitadel (no API server yet)
docker compose -f docker-compose.dev.yml up -d
```

## Run Database Migrations

```bash
make migrate
# or:
cd api && go run ./cmd/radioledger-api migrate up
```

## Run the API Server

```bash
cd api
cp .env.example .env
# Edit .env for local settings (DB URL, Zitadel URL, etc.)
go run ./cmd/radioledger-api
```

API available at `http://localhost:9091`

## Run the Web UI

```bash
cd web
corepack enable
corepack prepare pnpm@11.3.0 --activate
pnpm install
pnpm run dev
```

Web UI at `http://localhost:6173`

## Run the Desktop Client (Optional)

Requires Rust and Tauri CLI:

```bash
cargo install tauri-cli
cd desktop
pnpm install
cargo tauri dev
```

## Running Tests

```bash
# All tests (requires Docker for integration tests)
make test

# Unit tests only (fast, no Docker)
cd api && go test ./... -short

# Integration tests (starts a test database via testcontainers)
cd api && go test ./... -run Integration

# Web UI tests
cd web && npm test

# E2E tests (Playwright)
make e2e
```

See [Testing Guide](testing-guide.md) for more detail.

## Code Generation

When you change SQL queries:
```bash
cd api && sqlc generate
```

When you change the ADIF parser:
```bash
cd api && go generate ./pkg/adifparser/...
```

## Project Layout

```
radioledger/
├── api/              # Go API server
│   ├── cmd/          # Entry points
│   ├── internal/     # Private packages
│   │   ├── api/      # HTTP handlers and middleware
│   │   ├── service/  # Business logic
│   │   ├── repository/ # Data access (sqlc-generated)
│   │   └── worker/   # River job definitions
│   └── pkg/          # Public/shared packages
├── web/              # Vue 3 web UI
├── desktop/          # Tauri desktop client
├── mobile/           # Mobile app
├── database/
│   ├── migrations/   # goose SQL migrations
│   └── seeds/        # Reference data
├── docs/             # All documentation
├── testdata/         # Test fixtures
└── Makefile
```

## Common Make Targets

```bash
make build          # Build the API binary
make test           # Run all tests
make lint           # Run golangci-lint
make migrate        # Run pending migrations
make sqlc           # Regenerate sqlc code
make e2e            # Run Playwright E2E tests
make docker-build   # Build Docker images
```

## Related

- [Architecture Overview](architecture-overview.md)
- [Database Guide](database-guide.md)
- [Testing Guide](testing-guide.md)
- [Code Style](code-style.md)
