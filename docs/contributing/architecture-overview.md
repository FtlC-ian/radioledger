# Architecture Overview (For Contributors)

> High-level RadioLedger architecture — read this before contributing code.

This is a contributor-focused summary. For the full architecture document, see [ARCHITECTURE.md](../ARCHITECTURE.md).

## System Components

```
Clients (Web UI / Desktop / Mobile)
         │
         ▼
Go API Server (chi router) ── Zitadel (Auth)
         │
    ┌────┴────┐
    │         │
PostgreSQL   River (Job Queue)
+ PostGIS     │
              └── External Services
                  (LoTW / QRZ / eQSL / ClubLog / POTA)
```

## Key Architectural Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Language | Go | Performance, type safety, single binary |
| Database | PostgreSQL + PostGIS | RLS, spatial, JSONB, constraints |
| Auth | Zitadel | Go-native, multi-tenant, Docker-friendly |
| Job queue | River | Postgres-backed, no Redis needed |
| Query layer | sqlc | Type-safe, no ORM magic |
| Desktop | Tauri (Rust) | Small binary, native tray, no Electron |

**Do NOT use GORM.** sqlc + pgx is the pattern here.

## Row-Level Security (RLS)

Every tenant-scoped table has RLS enabled. The API sets `SET LOCAL app.current_user_id = $1` at the start of each request. This is the primary data isolation mechanism — the API layer is defense-in-depth.

**Never bypass RLS for convenience.**

## ADIF Processing

ADIF imports are async — never processed synchronously in an HTTP handler. A 100k-QSO file is common. The flow:

1. HTTP handler validates and stores the file
2. Creates an `import_jobs` record
3. Enqueues a River `ImportProcessJob`
4. Returns 202 with a job URL
5. Worker processes the file, updates `import_jobs` in real time
6. Client polls or subscribes via SSE for progress

## LoTW Constraint

The LoTW private key must NEVER reach the server. The desktop client handles all tQSL signing. This is a hard architectural constraint — any proposal to change it requires explicit security review.

## Package Structure

```
internal/
  api/
    handlers/     # one file per resource (qsos.go, logbooks.go, ...)
    middleware/   # auth, ratelimit, tenant, requestid
    router/       # chi setup + middleware stack
  service/        # business logic, no HTTP concerns
  repository/     # sqlc-generated data access
  worker/         # River job handlers
  domain/         # shared types
pkg/
  adifparser/     # streaming ADIF parser (fuzz-tested)
  maidenhead/     # grid utilities
  callsign/       # normalization
  encrypt/        # AES-256-GCM
```

## Related

- [ARCHITECTURE.md](../ARCHITECTURE.md) — full architecture document
- [SCHEMA.md](../SCHEMA.md) — database schema
- [Development Setup](development-setup.md)
- [Database Guide](database-guide.md)
