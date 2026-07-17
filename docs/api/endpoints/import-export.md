# Import/Export Endpoints

> ADIF import and export via the RadioLedger API.

## Import ADIF

### Start an Import

`POST /v1/logbooks/{logbook_uuid}/import`

Upload an ADIF file for import. The file is processed asynchronously.

**Request:** `multipart/form-data`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | file | Yes | ADIF file (`.adi` or `.adx`) |
| `dedup_strategy` | string | No | `skip` (default), `overwrite`, `import_all` |
| `dedup_window_seconds` | integer | No | Duplicate time window (default: 30) |
| `timestamp_mode` | string | No | `utc` (default), `local`, `auto` |

**Response:** `HTTP 202 Accepted`

```json
{
  "success": true,
  "data": {
    "job_uuid": "550e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "status_url": "/v1/import-jobs/550e8400-..."
  }
}
```

### Get Import Status

`GET /v1/import-jobs/{job_uuid}`

```json
{
  "success": true,
  "data": {
    "uuid": "...",
    "status": "processing",
    "total_records": 50000,
    "imported": 25000,
    "skipped": 120,
    "errors": 3,
    "pct_complete": 50.2,
    "started_at": "2026-02-28T14:00:00Z",
    "completed_at": null,
    "errors_url": "/v1/import-jobs/.../errors"
  }
}
```

Status values: `pending`, `processing`, `completed`, `failed`

### Stream Import Progress (SSE)

`GET /v1/import-jobs/{job_uuid}/stream`

Server-Sent Events stream for real-time progress. Each event is a JSON object with updated counts.

### Get Import Errors

`GET /v1/import-jobs/{job_uuid}/errors`

Returns downloadable record-level error details.

## Export ADIF

`GET /v1/logbooks/{logbook_uuid}/export`

### Query Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `format` | `adif` | Export format (currently only `adif`) |
| `band` | (all) | Filter by band |
| `mode` | (all) | Filter by mode |
| `date_from` | (all) | Start date filter |
| `date_to` | (all) | End date filter |

**Response:** ADIF file download (`Content-Type: application/octet-stream`)

Current behavior:
- Export always emits canonical ADIF 3.1.7 headers and record formatting.
- Known legacy mode aliases are normalized during export, for example `FT2` becomes `MODE=MFSK` + `SUBMODE=FT2`.
- Fields stored in `extra` are included automatically when they do not collide with typed exported fields.

For very large exports, the response may be streamed.

## Related

- [QSO Endpoints](qsos.md)
- [Import/Export (User Guide)](../../user-guide/import-export.md)
- [ADIF Field Mapping](../../reference/adif-field-mapping.md)
