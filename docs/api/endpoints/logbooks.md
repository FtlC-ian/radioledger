# Logbook Endpoints

> Manage RadioLedger logbooks via API.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/logbooks` | List all logbooks |
| `POST` | `/v1/logbooks` | Create a new logbook |
| `GET` | `/v1/logbooks/{uuid}` | Get a logbook |
| `PUT` | `/v1/logbooks/{uuid}` | Update logbook settings |
| `DELETE` | `/v1/logbooks/{uuid}` | Delete a logbook (soft) |

## List Logbooks

`GET /v1/logbooks`

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "uuid": "...",
        "name": "W1AW Main Log",
        "description": null,
        "station_callsign": "W1AW",
        "qso_count": 52341,
        "created_at": "2026-01-01T00:00:00Z",
        "updated_at": "2026-02-28T14:33:01Z"
      }
    ]
  }
}
```

## Create a Logbook

`POST /v1/logbooks`

```json
{
  "name": "POTA Log 2026",
  "description": "Parks on the Air activations",
  "station_callsign": "W1AW/P"
}
```

## Get a Logbook

`GET /v1/logbooks/{uuid}`

Returns logbook metadata and settings.

## Update a Logbook

`PUT /v1/logbooks/{uuid}`

Update name, description, station callsign, and default settings.

## Delete a Logbook

`DELETE /v1/logbooks/{uuid}`

Soft-deletes the logbook and all its QSOs. Recoverable for 30 days.

## Related

- [QSO Endpoints](qsos.md)
- [Import/Export Endpoints](import-export.md)
