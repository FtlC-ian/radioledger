# api/

Go API server for RadioLedger.

## What Goes Here

- `cmd/radioledger-api/` — main entry point, flags, config loading, server startup
- `cmd/radioledger-worker/` — background job worker (or use `--mode=worker` flag)
- `internal/api/handlers/` — HTTP handlers, one file per resource group
- `internal/api/middleware/` — auth, rate limiting, tenant (RLS), request ID, compression
- `internal/api/router/` — chi router setup
- `internal/service/` — business logic (QSO dedup, ADIF pipeline, sync orchestration, awards)
- `internal/repository/` — sqlc-generated data access layer
- `internal/worker/` — River job definitions and workers
- `internal/domain/` — shared types: QSO, Logbook, User, etc.
- `internal/pkg/` — internal libraries (ADIF parser, Maidenhead grid utils, callsign normalization)

## Tech Stack

- **Router:** go-chi/chi
- **DB Driver:** jackc/pgx/v5
- **Query gen:** sqlc
- **Job queue:** riverqueue/river (Postgres-backed, no Redis)
- **JWT:** golang-jwt/jwt/v5
- **Logging:** slog (stdlib, JSON output)
- **Metrics:** prometheus/client_golang
- **Testing:** testcontainers-go (real Postgres for integration tests)

## Generated Database Code

Run `make sqlc` from this directory to regenerate `internal/database/sqlc`. The target pins sqlc v1.30.0 so generated files stay reproducible; newer global `sqlc` binaries may produce unrelated formatting/order diffs.
