# Search API

> Search QSOs across logbooks with flexible filtering.

## Search Endpoint

`GET /v1/search/qsos`

The search endpoint supports the same filters as the logbook QSO list, but across all logbooks (or a specified subset).

## Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `q` | string | Free-text search (callsign, name, comment) |
| `logbook_uuid` | string | Limit to a specific logbook |
| `band` | string | Filter by band |
| `mode` | string | Filter by mode |
| `callsign` | string | Callsign (supports prefix wildcard: `W1*`) |
| `dxcc` | integer | DXCC entity ADIF number |
| `continent` | string | `NA`, `EU`, `AS`, `AF`, `OC`, `SA`, `AN` |
| `grid` | string | Maidenhead grid prefix (2-6 chars) |
| `state` | string | US state abbreviation |
| `cq_zone` | integer | CQ zone |
| `date_from` | string | Start date (ISO 8601) |
| `date_to` | string | End date (ISO 8601) |
| `qsl_lotw` | string | LoTW status: `pending`, `uploaded`, `confirmed` |
| `limit` | integer | Results per page (default: 50) |
| `cursor` | string | Pagination cursor |

## Response

```json
{
  "success": true,
  "data": {
    "items": [ ... QSO objects ... ],
    "pagination": {
      "cursor_next": "...",
      "has_next": true,
      "total": 1234
    }
  }
}
```

## Full-Text Search

The `q` parameter searches across:
- Callsign (exact and prefix)
- Operator name
- QTH
- Comment

Full-text search uses PostgreSQL's `tsvector` indexes for performance.

## Related

- [Search and Filter (User Guide)](../../user-guide/search-and-filter.md)
- [QSO Endpoints](qsos.md)
- [Pagination](../pagination.md)
