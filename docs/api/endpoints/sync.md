# Sync Endpoints

> Manage external service sync configuration and status via API.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/sync/services` | List configured sync services |
| `GET` | `/v1/sync/services/{service}` | Get sync status for a service |
| `POST` | `/v1/sync/services/{service}/trigger` | Trigger a manual sync |
| `GET` | `/v1/sync/jobs` | List recent sync jobs |
| `GET` | `/v1/qsos/{uuid}/sync-status` | Per-QSO sync status |

## List Sync Services

`GET /v1/sync/services`

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "service": "lotw",
        "enabled": true,
        "last_sync_at": "2026-02-28T14:00:00Z",
        "pending_count": 5,
        "error_count": 0,
        "status": "ok"
      },
      {
        "service": "qrz",
        "enabled": true,
        "last_sync_at": "2026-02-28T13:55:00Z",
        "pending_count": 0,
        "error_count": 0,
        "status": "ok"
      }
    ]
  }
}
```

## Trigger Sync

`POST /v1/sync/services/{service}/trigger`

Enqueues an immediate sync job for the specified service. Returns the job ID.

Services: `lotw`, `qrz`, `eqsl`, `clublog`, `pota`

## Per-QSO Sync Status

`GET /v1/qsos/{uuid}/sync-status`

```json
{
  "success": true,
  "data": {
    "lotw": {
      "status": "confirmed",
      "uploaded_at": "2026-02-28T14:05:00Z",
      "confirmed_at": "2026-02-28T16:30:00Z"
    },
    "qrz": {
      "status": "uploaded",
      "uploaded_at": "2026-02-28T14:05:00Z",
      "confirmed_at": null
    },
    "eqsl": {
      "status": "pending",
      "uploaded_at": null,
      "confirmed_at": null
    }
  }
}
```

## Related

- [Sync Overview](../../sync/index.md)
- [QSO Endpoints](qsos.md)
