# Pagination

> How cursor-based pagination works in the RadioLedger API.

## Overview

All list endpoints use cursor-based pagination — not offset/limit. This provides consistent results even when data changes during pagination.

## Request Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 50 | Number of results per page (max: 500) |
| `cursor` | string | (none) | Cursor for the next page (from previous response) |
| `direction` | string | `forward` | `forward` or `backward` |

## Response Format

```json
{
  "success": true,
  "data": {
    "items": [ ... ],
    "pagination": {
      "cursor_next": "eyJpZCI6MTIzNH0=",
      "cursor_prev": "eyJpZCI6MTAwMH0=",
      "has_next": true,
      "has_prev": false,
      "total": 52341
    }
  }
}
```

## Usage

```bash
# First page
curl -H "Authorization: Bearer <key>" \
  "https://your-radioledger.example/v1/logbooks/{uuid}/qsos?limit=100"

# Next page (use cursor_next from previous response)
curl -H "Authorization: Bearer <key>" \
  "https://your-radioledger.example/v1/logbooks/{uuid}/qsos?limit=100&cursor=eyJpZCI6MTIzNH0="
```

## Cursor Format

Cursors are opaque base64-encoded strings. Do not parse or construct them — treat them as opaque tokens. They may change format between API versions.

## Total Count

The `total` field in the response reflects the total number of items matching your filters, regardless of page size. This may be expensive to compute for very large datasets — it's calculated efficiently with a count query.

## Related

- [API Overview](index.md)
- [QSOs Endpoint](endpoints/qsos.md)
- [Search Endpoint](endpoints/search.md)
