# Activation Endpoints (POTA/SOTA)

> Manage portable activations and export upload-ready logs.

## POTA Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/activations/pota` | Start a POTA activation |
| `GET` | `/v1/activations/pota` | List your POTA activations |
| `GET` | `/v1/activations/pota/{activation_uuid}` | Activation detail with linked QSO list |
| `PUT` | `/v1/activations/pota/{activation_uuid}` | Update activation metadata |
| `GET` | `/v1/activations/pota/{activation_uuid}/status` | Validation status (10 unique callsigns minimum) |
| `POST` | `/v1/activations/pota/{activation_uuid}/export` | Export POTA ADIF (injects `MY_SIG` + `MY_SIG_INFO`) |

### Create POTA activation

`POST /v1/activations/pota`

```json
{
  "reference": "K-1234",
  "activation_date": "2026-02-28",
  "logbook_uuid": "550e8400-e29b-41d4-a716-446655440000",
  "station_location_uuid": "8f7b2ef7-b9fb-4f72-8ab4-4f950f89e711",
  "notes": "Late afternoon activation"
}
```

Rules:
- `reference` must match POTA format (`^[A-Z]{1,3}-[0-9]{1,5}$`)
- `activation_date` is UTC date (`YYYY-MM-DD`)
- If `logbook_uuid` is omitted, the user default logbook is used

### POTA status response

`GET /v1/activations/pota/{activation_uuid}/status`

```json
{
  "success": true,
  "message": "pota activation status retrieved",
  "data": {
    "program": "POTA",
    "reference": "K-1234",
    "activation_date": "2026-02-28",
    "status": "in_progress",
    "qso_count": 7,
    "unique_callsigns": 7,
    "minimum_contacts": 10,
    "contacts_needed": 3,
    "missing_required_fields": ["MY_GRIDSQUARE"],
    "warnings": ["POTA requires at least 10 unique callsigns"],
    "ready_to_submit": false
  },
  "error": null
}
```

### POTA export response

`POST /v1/activations/pota/{activation_uuid}/export`

```json
{
  "success": true,
  "message": "pota activation exported",
  "data": {
    "filename": "radioledger-pota-k-1234-2026-02-28.adi",
    "adif": "<ADIF_VER:5>3.1.7...",
    "qso_count": 12,
    "unique_callsigns": 10,
    "ready_to_submit": true,
    "missing_fields": [],
    "validation_warnings": []
  },
  "error": null
}
```

Export behavior:
- `MY_SIG` is forced to `POTA` on every QSO
- `MY_SIG_INFO` is forced to activation reference on every QSO
- Required upload fields are validated pre-export:
  - `STATION_CALLSIGN`
  - `MY_GRIDSQUARE`

## SOTA Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/activations/sota` | Start a SOTA activation |
| `GET` | `/v1/activations/sota` | List your SOTA activations |
| `GET` | `/v1/activations/sota/{activation_uuid}` | Activation detail with linked QSO list |
| `PUT` | `/v1/activations/sota/{activation_uuid}` | Update activation metadata |
| `GET` | `/v1/activations/sota/{activation_uuid}/status` | Validation status (4 contacts minimum) |

SOTA rules:
- `reference` must match summit format like `W4C/WM-001`
- Minimum contacts: 4
- S2S contacts are surfaced in validation (`s2s_count`) and recommended when possible

## Related

- [Import/Export Endpoints](import-export.md)
- [Awards Endpoints](awards.md)
- [POTA Sync Guide](../../sync/pota.md)
- [SOTA Sync Guide](../../sync/sota.md)
