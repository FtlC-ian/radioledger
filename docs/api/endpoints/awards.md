# Award Tracking Endpoints

> Query award and activity progress via the RadioLedger API.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/awards/dxcc` | DXCC progress |
| `GET` | `/v1/awards/was` | WAS progress |
| `GET` | `/v1/awards/grids` | VUCC-style grid progress |
| `GET` | `/v1/awards/pota` | POTA activator + hunter summary |

## POTA awards summary

`GET /v1/awards/pota`

```json
{
  "success": true,
  "message": "pota progress retrieved",
  "data": {
    "parks_activated": 18,
    "parks_hunted": 221,
    "activations_total": 27,
    "valid_activations": 19
  },
  "error": null
}
```

Definitions:
- `parks_activated`: distinct park references from your POTA activations
- `activations_total`: total POTA activation sessions
- `valid_activations`: activation sessions meeting POTA minimum (10 unique callsigns)
- `parks_hunted`: distinct park references worked as a hunter via `SIG=POTA` and `SIG_INFO`

## Related

- [Activation Endpoints](activations.md)
- [Awards Tracking (User Guide)](../../user-guide/awards-tracking.md)
- [DXCC Entities Reference](../../reference/dxcc-entities.md)

---

## Unified Award Endpoints (Issue #50)

The following endpoints provide a unified view across all supported award types.

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/awards` | Summary of all award progress (from cache) |
| `GET` | `/v1/awards/:type` | Detailed progress for one award type |
| `GET` | `/v1/awards/:type/needs` | Needs list for bounded awards (WAS, WAZ, DXCC) |
| `POST` | `/v1/awards/refresh` | Trigger background recalculation |

### Supported Award Types

| `type` value | Award | Bounded? | Target |
|---|---|---|---|
| `dxcc` | DXCC (DX Century Club) | Yes | 340 entities |
| `was` | WAS (Worked All States) | Yes | 50 states |
| `vucc` | VUCC (VHF/UHF Century Club) | Yes | 100 grids (basic) |
| `waz` | WAZ (Worked All Zones) | Yes | 40 CQ zones |
| `wpx` | WPX (Worked All Prefixes) | No | — |
| `pota_hunter` | POTA Hunter | No | — |
| `pota_activator` | POTA Activator | No | — |
| `sota_chaser` | SOTA Chaser | No | — |
| `sota_activator` | SOTA Activator | No | — |
| `stats_activity_heatmap` | Stats activity heatmap | No | — |

### GET /v1/awards

Returns a summary row per award type from the `award_progress` cache.
Call `POST /v1/awards/refresh` to trigger recalculation if the cache is stale.

```json
{
  "success": true,
  "message": "award progress retrieved",
  "data": {
    "awards": [
      {
        "award_type": "dxcc",
        "worked": 138,
        "confirmed": 87,
        "target": 340,
        "needed": 202,
        "progress_pct": 40.59,
        "latest_qso_at": "2026-02-28T14:23:00Z",
        "cache_updated_at": "2026-03-01T06:00:00Z"
      },
      {
        "award_type": "was",
        "worked": 38,
        "confirmed": 12,
        "target": 50,
        "needed": 12,
        "progress_pct": 76.0
      }
    ]
  }
}
```

### GET /v1/awards/:type

Returns all cached `award_progress` rows for the named award type.
Returns `success=true, data=null` (not HTTP 404) for unknown award types.

```json
{
  "success": true,
  "message": "was progress retrieved",
  "data": {
    "award_type": "was",
    "worked": 38,
    "confirmed": 12,
    "target": 50,
    "needed": 12,
    "progress_pct": 76.0,
    "rows": [
      { "entity_key": "AL", "worked": true, "confirmed": false, "qso_count": 3 },
      { "entity_key": "TX", "worked": true, "confirmed": true,  "qso_count": 47 }
    ]
  }
}
```

### GET /v1/awards/:type/needs

Returns the entity keys not yet worked. For unbounded awards (WPX, POTA, SOTA),
returns `needed=0` and an empty items list.

```json
{
  "success": true,
  "message": "was needs list retrieved",
  "data": {
    "award_type": "was",
    "needed": 12,
    "items": [
      { "entity_key": "AK" },
      { "entity_key": "HI" }
    ]
  }
}
```

### POST /v1/awards/refresh

Marks all `award_progress` rows dirty, triggering the `AwardRefreshWorker`
background job to recalculate. Returns immediately (async).

```json
{
  "success": true,
  "message": "award refresh scheduled",
  "data": {
    "queued": true,
    "note": "background refresh triggered; progress will update shortly"
  }
}
```

### Architecture Notes

- Progress is cached in the `award_progress` table (dirty flag pattern).
- `AwardRefreshWorker` (River job kind `award_refresh`) recalculates from raw QSOs.
- Milestone notifications (type `award_milestone`) are created when counts cross thresholds.
- RLS enforces tenant isolation — users never see each other's award rows.
- WPX prefix extraction uses the standard ITU algorithm: letters before first digit + first digit.
