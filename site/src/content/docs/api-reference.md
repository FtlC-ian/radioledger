---
title: API Reference
description: Complete endpoint reference for the RadioLedger REST API, with authentication, examples, and rate limits.
sidebar:
  order: 1
---

RadioLedger exposes a JSON REST API for logging, import/export, awards, activations, sync, and account management.

## Base URLs

| Deployment | Base URL |
| --- | --- |
| Self-hosted default | `http://localhost:8080/v1` |

## Response envelope

All API responses use the same envelope:

```json
{
  "success": true,
  "data": {},
  "message": "optional",
  "error": null
}
```

## Authentication

Use Bearer authentication for all protected endpoints.

```http
Authorization: Bearer <token>
```

### API keys

- Best for scripts and personal integrations
- Prefixes: `rl_live_` and `rl_test_`
- Scoped permissions (`qsos:read`, `qsos:write`, etc.)

### JWT access tokens (OAuth2/OIDC)

- Best for user-authorized apps
- Short-lived access tokens + refresh token rotation
- Common in web, desktop, and mobile clients

### Unauthenticated endpoints

- `GET /health`
- `GET /ready`

Everything else requires valid auth.

## Rate limits by tier

| Tier | Requests / minute | Burst |
| --- | ---: | ---: |
| Unauthenticated | 20 | 50 |
| Self-hosted | Configurable | Configurable |

Additional documented limits:

| Endpoint | Limit |
| --- | --- |
| `POST /v1/logbooks/{logbook_uuid}/import` | 5/hour |
| `GET /v1/logbooks/{logbook_uuid}/export` | 10/hour |
| `POST /v1/auth/token` | 10/min |

## Request/response examples

### Example: create a QSO

```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "callsign": "W1AW",
    "band": "20m",
    "mode": "SSB",
    "datetime_on": "2026-02-28T14:32:00Z"
  }' \
  http://localhost:8080/v1/logbooks/<logbook_uuid>/qsos
```

```json
{
  "success": true,
  "data": {
    "uuid": "550e8400-e29b-41d4-a716-446655440000",
    "callsign": "W1AW",
    "band": "20m",
    "mode": "SSB",
    "datetime_on": "2026-02-28T14:32:00Z"
  },
  "message": "QSO created",
  "error": null
}
```

### Example: list paginated QSOs

```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/v1/logbooks/<logbook_uuid>/qsos?limit=100&cursor=<cursor>"
```

```json
{
  "success": true,
  "data": {
    "items": [],
    "pagination": {
      "cursor_next": "eyJpZCI6MTIzNH0=",
      "cursor_prev": "eyJpZCI6MTAwMH0=",
      "has_next": true,
      "has_prev": false,
      "total": 52341
    }
  },
  "message": "ok",
  "error": null
}
```

## Endpoint catalog

### Health and auth

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness check |
| `GET` | `/ready` | Readiness check |
| `POST` | `/v1/auth/register` | Dev-mode account registration |
| `POST` | `/v1/auth/login` | Dev-mode login |
| `GET` | `/v1/auth/me` | Get authenticated profile |

### Logbooks

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/logbooks` | List logbooks |
| `POST` | `/v1/logbooks` | Create logbook |
| `GET` | `/v1/logbooks/{uuid}` | Get logbook |
| `PUT` | `/v1/logbooks/{uuid}` | Update logbook |
| `DELETE` | `/v1/logbooks/{uuid}` | Soft-delete logbook |

### QSOs

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/logbooks/{logbook_uuid}/qsos` | List QSOs in logbook |
| `POST` | `/v1/logbooks/{logbook_uuid}/qsos` | Create QSO |
| `GET` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Get QSO |
| `PUT` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Update QSO |
| `DELETE` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Soft-delete QSO |

### Import / export

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/logbooks/{logbook_uuid}/import` | Upload ADIF import job |
| `GET` | `/v1/import-jobs/{job_uuid}` | Get import job status |
| `GET` | `/v1/import-jobs/{job_uuid}/stream` | Stream import progress (SSE) |
| `GET` | `/v1/import-jobs/{job_uuid}/errors` | Retrieve import errors |
| `GET` | `/v1/logbooks/{logbook_uuid}/export` | Export ADIF |

### Search

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/search/qsos` | Search QSOs across logbooks |

### Sync services

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/sync/services` | List sync services |
| `GET` | `/v1/sync/services/{service}` | Get service status |
| `POST` | `/v1/sync/services/{service}/trigger` | Trigger manual sync |
| `GET` | `/v1/sync/jobs` | List recent sync jobs |
| `GET` | `/v1/qsos/{uuid}/sync-status` | Per-QSO sync status |

### Awards

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/awards/dxcc` | DXCC progress |
| `GET` | `/v1/awards/was` | WAS progress |
| `GET` | `/v1/awards/grids` | Grid/VUCC-style progress |
| `GET` | `/v1/awards/pota` | POTA progress summary |

### Activations (POTA)

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/activations/pota` | Start POTA activation |
| `GET` | `/v1/activations/pota` | List POTA activations |
| `GET` | `/v1/activations/pota/{activation_uuid}` | Get activation details |
| `PUT` | `/v1/activations/pota/{activation_uuid}` | Update activation |
| `GET` | `/v1/activations/pota/{activation_uuid}/status` | Validate activation status |
| `POST` | `/v1/activations/pota/{activation_uuid}/export` | Export activation ADIF |

### Activations (SOTA)

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/activations/sota` | Start SOTA activation |
| `GET` | `/v1/activations/sota` | List SOTA activations |
| `GET` | `/v1/activations/sota/{activation_uuid}` | Get activation details |
| `PUT` | `/v1/activations/sota/{activation_uuid}` | Update activation |
| `GET` | `/v1/activations/sota/{activation_uuid}/status` | Validate activation status |

### Users and API keys

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/users/me` | Get current user |
| `PUT` | `/v1/users/me` | Update user profile |
| `GET` | `/v1/users/me/api-keys` | List API keys |
| `POST` | `/v1/users/me/api-keys` | Create API key |
| `DELETE` | `/v1/users/me/api-keys/{key_id}` | Revoke API key |

### Webhooks and realtime

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/v1/webhooks` | Create webhook subscription |
| `POST` | `/api/v1/ws/token` | Mint short-lived WebSocket token |

## Error behavior

- `401` — missing/invalid auth token
- `403` — valid token but missing scope/permission
- `429` — rate limit exceeded
- `500` — internal server error

Validation failures return `VALIDATION_ERROR` with field-level details.

## OpenAPI

OpenAPI 3.1 spec endpoint:

```text
http://localhost:8080/api/openapi.json
```
