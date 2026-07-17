# API Reference

> RadioLedger REST API — complete reference for developers.

The RadioLedger API gives you full read/write access to all logbook data. Build custom integrations, scripts, alternative clients, or automate your logging workflow.

## Base URLs

| Instance | Base URL |
|---------|---------|
| Self-hosted (default) | `http://localhost:8080/v1` |

All endpoints are prefixed with `/v1/`.

## API Design

- **RESTful** — standard HTTP methods and status codes
- **JSON** — all requests and responses use `application/json`
- **Response envelope** — all responses use `{success, data, message, error}` format
- **Cursor pagination** — no offset/limit; uses cursor-based pagination for large result sets
- **UUIDs** — all resource IDs are UUIDs (no integer IDs exposed)

## Response Format

All API responses follow this envelope:

```json
{
  "success": true,
  "data": { ... },
  "message": "QSO created",
  "error": null
}
```

Error responses:

```json
{
  "success": false,
  "data": null,
  "message": "Validation failed",
  "error": {
    "code": "VALIDATION_ERROR",
    "fields": {
      "callsign": "invalid callsign format"
    }
  }
}
```

**Note:** HTTP 404 is only returned for unknown routes, not for missing resources. A resource that doesn't exist returns HTTP 200 with `{success: false}`.

## Authentication

All endpoints (except `/health` and `/ready`) require authentication.

See [Authentication](authentication.md) for:
- API key authentication
- OAuth2 access tokens
- Generating API keys in Settings

Quick example:

```bash
curl -H "Authorization: Bearer <your-api-key>" \
  http://localhost:8080/v1/logbooks
```

## OpenAPI Specification

The full OpenAPI 3.1 spec is available at:

```
http://localhost:8080/api/openapi.json
```

Use this with any OpenAPI-compatible tool (Swagger UI, Scalar, Postman, etc.).

## Quick Start: First API Call

```bash
# 1. Get an API key from Settings → API Keys

# 2. List your logbooks
curl -H "Authorization: Bearer <key>" \
  http://localhost:8080/v1/logbooks

# 3. Log a QSO
curl -X POST \
  -H "Authorization: Bearer <key>" \
  -H "Content-Type: application/json" \
  -d '{
    "callsign": "W1AW",
    "band": "20m",
    "mode": "SSB",
    "datetime_on": "2026-02-28T14:32:00Z"
  }' \
  http://localhost:8080/v1/logbooks/<logbook-uuid>/qsos
```

## In This Section

| Doc | What it covers |
|-----|---------------|
| [authentication.md](authentication.md) | API keys, OAuth2 tokens, scopes |
| [rate-limits.md](rate-limits.md) | Rate limits by tier and endpoint |
| [errors.md](errors.md) | Error codes and response format |
| [pagination.md](pagination.md) | Cursor-based pagination |
| [webhooks.md](webhooks.md) | Webhook events and payloads |
| [endpoints/qsos.md](endpoints/qsos.md) | QSO CRUD |
| [endpoints/logbooks.md](endpoints/logbooks.md) | Logbook management |
| [endpoints/import-export.md](endpoints/import-export.md) | ADIF import/export |
| [endpoints/sync.md](endpoints/sync.md) | Sync service management |
| [endpoints/awards.md](endpoints/awards.md) | Award tracking |
| [endpoints/activations.md](endpoints/activations.md) | POTA/SOTA activation workflows |
| [endpoints/search.md](endpoints/search.md) | Search and filtering |
| [endpoints/users.md](endpoints/users.md) | User/account management |

## No-Moat Philosophy

The API provides full access to all your data. Third parties can build real products on top of RadioLedger's API — alternative desktop clients, mobile apps, club dashboards, DX cluster bots, QSL card services. We don't artificially limit API access to protect our UI.

Your moat is the quality of your integration, not API restrictions.

## Related

- [Authentication](authentication.md)
- [Contributing](../contributing/index.md)
- [Architecture Overview](../ARCHITECTURE.md)
