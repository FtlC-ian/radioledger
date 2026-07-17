# Rate Limits

> Rate limiting policies for the RadioLedger API.

## Overview

RadioLedger applies rate limits at two layers:

| Layer | Applies to | Default |
|-------|-----------|---------|
| **Per IP** | Unauthenticated and authenticated requests | 100 req/min |
| **Per User** | Authenticated requests | 1,000 req/min |

The stricter limit applies when both would trigger.

## Rate Limit Headers

Every API response includes rate limit information:

```
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 987
X-RateLimit-Reset: 1709128800
Retry-After: 30   (only on 429 responses)
```

## Tier Limits

| Traffic class | Requests/minute | Burst |
|---------------|-----------------|-------|
| Unauthenticated | Configurable | Configurable |
| Authenticated | Configurable | Configurable |

An administrator chooses appropriate limits for each self-hosted deployment.

## Endpoint-Specific Limits

Some endpoints have additional limits:

| Endpoint | Limit | Reason |
|---------|-------|--------|
| `POST /logbooks/{id}/import` | 5/hour | Large file processing |
| `GET /logbooks/{id}/export` | 10/hour | Heavy DB query |
| `POST /auth/token` | 10/min | Brute force protection |

## Handling Rate Limits

When you hit a rate limit, the API returns `HTTP 429 Too Many Requests`:

```json
{
  "success": false,
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Too many requests. Retry after 30 seconds."
  }
}
```

Respect the `Retry-After` header. Implement exponential backoff in your client.

## Self-Hosted Configuration

For self-hosted instances, configure rate limits in your `.env`:

```dotenv
RADIOLEDGER_RATE_LIMIT_IP=100/min
RADIOLEDGER_RATE_LIMIT_USER=1000/min
```

## Related

- [API Overview](index.md)
- [Errors](errors.md)
- [Authentication](authentication.md)
