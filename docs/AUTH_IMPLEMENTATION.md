# Authentication Implementation Guide

**Last updated:** 2026-02-28

This document covers the RadioLedger authentication implementation: the pluggable `Authenticator` interface, the `DevAuth` and `ZitadelAuth` implementations, and the middleware stack.

---

## Overview

Authentication is implemented as a pluggable interface (`auth.Authenticator`) that allows:

- **Development mode** (`AUTH_MODE=dev`): Simple "dev-user-{id}" bearer tokens. No external dependency.
- **Production mode** (`AUTH_MODE=zitadel`): RS256 JWT validation against Zitadel's JWKS endpoint.

The `AUTH_MODE` environment variable selects the implementation at startup.

---

## Architecture

```
HTTP Request
     │
     ▼
Auth Middleware (middleware.Auth)
     │  Extracts: Authorization: Bearer <token>
     │  Calls:    authenticator.ValidateToken(ctx, token)
     │  Stores:   auth.UserInfo in request context
     │
     ▼
Handler
     │  Reads:  auth.UserIDFromContext(ctx)
     │  Opens:  tenant transaction (SET LOCAL app.current_user_id)
     │  Uses:   RLS-enforced DB queries
```

---

## Authenticator Interface

```go
// auth/auth.go
type Authenticator interface {
    ValidateToken(ctx context.Context, token string) (UserInfo, error)
}

type UserInfo struct {
    UserID     int64     // Internal DB primary key — never expose in API responses
    UserUUID   uuid.UUID // External-facing identifier — use in API responses
    Email      string
    ExternalID string    // Zitadel sub claim (empty in dev mode)
}
```

---

## Dev Mode (`AUTH_MODE=dev`)

**Implementation:** `auth.DevAuth`

**Token format:** `dev-user-{userID}` where `{userID}` is the internal database `id`.

**How to get a token:**

```bash
# Register a new user (dev mode only)
curl -X POST http://localhost:9091/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "you@example.com", "display_name": "Your Name"}'

# Response includes:
# {"success": true, "data": {"token": "dev-user-42", "user": {...}}}

# Use the token:
token="dev-user-42"
curl http://localhost:9091/v1/logbooks \
  -H "Authorization: Bearer ${token}"

# Login (email lookup, no password):
curl -X POST http://localhost:9091/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "you@example.com"}'
```

**Security note:** Dev mode is intentionally insecure. The server enforces `AUTH_MODE=dev` is only allowed when `APP_ENV != production`.

---

## Zitadel Mode (`AUTH_MODE=zitadel`)

**Implementation:** `auth.ZitadelAuth`

**How it works:**

1. Client authenticates with Zitadel via OAuth2 PKCE flow
2. Client receives a Zitadel-issued RS256 JWT access token
3. Client sends token: `Authorization: Bearer <jwt>`
4. `ZitadelAuth.ValidateToken`:
   - Fetches JWKS from `{ZITADEL_URL}/oauth/v2/keys` (cached for 1 hour)
   - Validates JWT signature, issuer, and expiry
   - Extracts `sub` (Zitadel user ID) and `email` from claims
   - Looks up local user by `zitadel_id` (first-login JIT provisioning)

**Environment variables:**

```env
AUTH_MODE=zitadel
ZITADEL_URL=https://your-org.zitadel.cloud   # or http://zitadel:8080 for Docker
ZITADEL_CLIENT_ID=your-client-id
```

**First login behavior:**

- If a user with this `zitadel_id` exists → return their record
- If a user with this email exists (no `zitadel_id`) → link the Zitadel ID and return
- If neither exists → provision a new user + default logbook

---

## API Endpoints

| Method | Path | Auth required | Description |
|--------|------|---------------|-------------|
| POST | `/v1/auth/register` | No | Dev mode: create account + return token. Production: 501. |
| POST | `/v1/auth/login` | No | Dev mode: email lookup + return token. Production: 501. |
| GET | `/v1/auth/me` | Yes | Return authenticated user's profile. |

---

## Database Changes

Migration `002_add_zitadel_id_to_users.sql` adds:

```sql
ALTER TABLE users ADD COLUMN zitadel_id TEXT UNIQUE;
CREATE INDEX idx_users_zitadel_id ON users (zitadel_id) WHERE zitadel_id IS NOT NULL;
```

The `zitadel_id` column stores the Zitadel subject claim (`sub`) and is `NULL` for dev-mode users.

---

## Docker Compose — Enabling Zitadel

```bash
# Start with Zitadel (profile=zitadel):
docker compose --profile zitadel up -d

# Configure the API to use Zitadel:
AUTH_MODE=zitadel
ZITADEL_URL=http://zitadel:8080

# Zitadel UI available at: http://localhost:8081
```

---

## Testing

```bash
# Run all auth tests
go test ./internal/handler/... -run "TestAuth_" -v

# Run full integration suite
go test ./internal/handler/... -v
```

Key test scenarios:
- `TestAuth_UnauthenticatedRequestsReturn401` — all protected routes require auth
- `TestAuth_HealthEndpointsArePublic` — /health and /ready are exempt
- `TestAuth_InvalidTokenReturns401` — bad tokens are rejected
- `TestAuth_DevRegistrationAndLogin` — dev register/login flow
- `TestAuth_UserCanOnlySeeOwnData` — RLS isolation between users
- `TestAuth_RegistrationCreatesDefaultLogbook` — new users get a default logbook
