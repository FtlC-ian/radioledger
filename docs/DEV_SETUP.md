# Local Development Setup

## Prerequisites

- Go 1.22+ (macOS: `/opt/homebrew/bin/go`)
- Node.js 20+
- PostgreSQL 15+ with a `radioledger` database
- npm or pnpm

## Database

Create the database and run migrations:

```bash
createdb radioledger
cd database/migrations
# Apply migrations in order (001_*.sql through latest)
for f in *.sql; do psql -d radioledger -f "$f"; done
```

Or use the migration script if available:

```bash
./database/scripts/run-migrations.sh dev
```

## Backend (Go API)

```bash
cd api

# Required environment variables
export DATABASE_URL="postgres://your_user:your_pass@localhost:5432/radioledger?sslmode=disable"
export AUTH_MODE=local        # local auth with passwords (alias: "dev")
export APP_ENV=development    # required for local auth mode

# Optional
export RADIOLEDGER_MASTER_KEY="..."   # auto-generated on first run if not set
export CORS_ALLOWED_ORIGINS="http://localhost:9000"  # if not using dev proxy
export OTEL_TRACES_EXPORTER=none      # suppress verbose OTel trace output
export OTEL_METRICS_EXPORTER=none
export OTEL_LOGS_EXPORTER=none

# Run
go run ./cmd/server/
# Listens on :9091
```

## Frontend (Vue 3 + Quasar)

```bash
cd web
corepack enable
corepack prepare pnpm@11.3.0 --activate
pnpm install
pnpm run dev
# Listens on :9000
```

The dev server proxies `/v1/*` requests to `http://localhost:9091` automatically
(configured in `quasar.config.ts`), so no CORS issues in development.

## Auth Modes

| Mode | Config | Use Case |
|------|--------|----------|
| `local` | `AUTH_MODE=local` | Self-hosters, local dev. Email/password with bcrypt, JWT tokens. |
| `dev` | `AUTH_MODE=dev` | Alias for `local` (backward compat). |
| `zitadel` | `AUTH_MODE=zitadel` | Production (radioledger.com). OIDC via Zitadel. Register/login endpoints return 501; auth flows through Zitadel hosted UI. |

### Local Auth Endpoints

- `POST /v1/auth/register` — `{ email, password, callsign?, display_name? }` (password min 8 chars)
- `POST /v1/auth/login` — `{ email, password }` → returns JWT token
- `POST /v1/auth/change-password` — `{ old_password, new_password }` (requires auth)
- `GET /v1/auth/me` — returns user profile (requires auth)
- `DELETE /v1/auth/me` — soft-delete account (requires auth)

### Test Credentials

Register a new account via the web UI or curl:

```bash
curl -X POST http://localhost:9091/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@example.com","password":"password123"}'
```

## Known Issues

- **OTel tracing** is extremely verbose in dev mode. Set the `OTEL_*_EXPORTER=none` vars above to suppress.
- **River queue permissions**: If your DB user isn't the table owner, you may see `permission denied for table river_queue`. Grant permissions or use a superuser for dev.
- **Pagination display**: QSO count in table footer may show "1-1 of 1" instead of correct count — minor UI bug, does not affect functionality.
- **ESLint config**: `pnpm run lint` has pre-existing parser config issues with TypeScript in Vue SFCs. Build still succeeds.
