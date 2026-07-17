# QSO Endpoints

> CRUD operations for QSOs (individual radio contacts).

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/logbooks/{logbook_uuid}/qsos` | List QSOs in a logbook |
| `POST` | `/v1/logbooks/{logbook_uuid}/qsos` | Create a new QSO |
| `GET` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Get a single QSO |
| `PUT` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Update a QSO |
| `DELETE` | `/v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}` | Delete a QSO (soft) |

## List QSOs

`GET /v1/logbooks/{logbook_uuid}/qsos`

### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `limit` | integer | Results per page (default: 50, max: 500) |
| `cursor` | string | Pagination cursor |
| `sort` | string | Sort field: `datetime_on`, `callsign` (default: `datetime_on`) |
| `order` | string | `desc` or `asc` (default: `desc`) |
| `band` | string | Filter by band (e.g., `20m`) |
| `mode` | string | Filter by mode |
| `callsign` | string | Filter by callsign (prefix match) |
| `dxcc` | integer | Filter by DXCC entity number |
| `date_from` | string | ISO 8601 date filter start |
| `date_to` | string | ISO 8601 date filter end |

### Response

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "uuid": "550e8400-e29b-41d4-a716-446655440000",
        "callsign": "W1AW",
        "band": "20m",
        "freq": 14.225,
        "mode": "SSB",
        "submode": null,
        "datetime_on": "2026-02-28T14:32:00Z",
        "datetime_off": null,
        "rst_sent": "59",
        "rst_rcvd": "59",
        "name": "Hiram",
        "qth": "Newington, CT",
        "gridsquare": "FN31",
        "dxcc_entity": {
          "adif_id": 291,
          "name": "United States of America",
          "prefix": "W"
        },
        "qsl_lotw": "confirmed",
        "qsl_qrz": "sent",
        "qsl_eqsl": "not_sent",
        "created_at": "2026-02-28T14:33:01Z",
        "updated_at": "2026-02-28T14:33:01Z"
      }
    ],
    "pagination": {
      "cursor_next": "eyJ...",
      "has_next": true,
      "total": 52341
    }
  }
}
```

## Create a QSO

`POST /v1/logbooks/{logbook_uuid}/qsos`

### Request Body

```json
{
  "callsign": "W1AW",
  "band": "20m",
  "freq": 14.225,
  "mode": "SSB",
  "datetime_on": "2026-02-28T14:32:00Z",
  "datetime_off": null,
  "rst_sent": "59",
  "rst_rcvd": "59",
  "name": "Hiram",
  "qth": "Newington, CT",
  "gridsquare": "FN31",
  "tx_pwr": 100,
  "comment": "Great signal!",
  "extra": {
    "IOTA": "NA-001"
  }
}
```

### Required Fields

- `callsign` — the worked station's callsign
- `band` OR `freq` — at least one required
- `mode` — operating mode
- `datetime_on` — ISO 8601 UTC timestamp

### Response

`HTTP 201 Created` with the created QSO object.

## Get a QSO

`GET /v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}`

Returns the full QSO object including all ADIF fields and sync status per service.

## Update a QSO

`PUT /v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}`

Request body: same fields as Create. Returns the updated QSO.

Note: Editing `callsign`, `band`, `mode`, or `datetime_on` on a QSO that has been uploaded to LoTW returns a warning in the response.

## Delete a QSO

`DELETE /v1/logbooks/{logbook_uuid}/qsos/{qso_uuid}`

Soft-deletes the QSO. Returns `{success: true}`. Recoverable for 30 days.

## Related

- [Import/Export Endpoints](import-export.md)
- [Search Endpoint](search.md)
- [Logbook Endpoints](logbooks.md)
- [Pagination](../pagination.md)
