# Admin Endpoints

> Administrative operations for RadioLedger self-hosters.
> These endpoints require admin-level permissions, typically controlled via the `RADIOLEDGER_ADMIN_EMAILS` environment variable.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/admin/jobs` | List River background jobs |
| `POST` | `/v1/admin/jobs/{id}/retry` | Manually retry a failed job |
| `POST` | `/v1/admin/sync/trigger` | Manually trigger a callsign sync |
| `GET` | `/v1/admin/sync/overview` | Get callsign sync database status |

---

## Job Management

### List Jobs

`GET /v1/admin/jobs`

Returns a list of all River background jobs with their status, attempt count, and metadata.

### Retry Job

`POST /v1/admin/jobs/{id}/retry`

Manually moves a failed or snoozed job back to the `available` state for immediate processing.

---

## Callsign Database Management

### Database Overview

`GET /v1/admin/sync/overview`

Returns the current record count and last sync timestamp for each supported callsign database source.

```json
{
  "success": true,
  "data": {
    "sources": [
      {
        "source": "fcc",
        "record_count": 780123,
        "last_sync_at": "2026-03-01T02:15:00Z",
        "status": "ok"
      },
      {
        "source": "ised",
        "record_count": 72456,
        "last_sync_at": "2026-03-02T04:00:00Z",
        "status": "ok"
      }
    ]
  }
}
```

### Trigger Sync

`POST /v1/admin/sync/trigger`

Forces an immediate sync run for a specific callsign source.

| Field | Type | Description |
|-------|------|-------------|
| `source` | `string` | Source to sync (e.g., `fcc`, `ised`, `acma`, `anfr`, `ift`, `rdi`, `ofcom`, `bnetza`, `jj1wtl`) |
| `full` | `boolean` | Whether to perform a full refresh (true) or incremental sync (false) |

---

## Related

- [Sync Worker Infrastructure](../../sync/worker-infrastructure.md)
- [Self-Hosting Admin Settings](../../user-guide/settings.md#self-hosting-admin-settings)
