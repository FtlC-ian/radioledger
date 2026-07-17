---
title: ADIF Import and Export
description: Move logs in and out of RadioLedger without losing meaningful ADIF data.
sidebar:
  order: 3
---

RadioLedger imports older ADIF versions and exports canonical ADIF 3.1.7 output.

## Prerequisites

- An existing logbook in RadioLedger
- A valid `.adi` or `.adx` file for import

## Import ADIF

1. Open your target logbook.
2. Click **Import → ADIF**.
3. Choose a file.
4. Pick duplicate-handling behavior.
5. Start import and monitor progress.

### Import options

| Option | Default | Description |
| --- | --- | --- |
| Duplicate handling | `skip` | Skip, overwrite, or import both |
| Duplicate window | `30s` | Contact time window used for dedupe |
| Callsign normalization | Uppercase | Normalizes callsigns on ingest |
| Timestamp mode | `utc` | Use UTC, source timezone, or auto |

### Import status values

- `pending`
- `processing`
- `completed`
- `failed`

## Export ADIF

1. Open the logbook.
2. Click **Export → ADIF**.
3. Optionally apply filters first (band/mode/date).
4. Download the file.

### Export behavior

- Uses canonical ADIF 3.1.7 formatting.
- Normalizes recognized mode aliases on export, for example `FT2` to `MODE=MFSK` plus `SUBMODE=FT2`.
- Preserves long-tail ADIF fields through `extra` metadata.
- Is designed to preserve **semantic-lossless** round-trip fidelity for supported ADIF data.

## API shortcuts

```bash
# Start import job
curl -X POST \
  -H "Authorization: Bearer <token>" \
  -F "file=@./my-log.adi" \
  http://localhost:8080/v1/logbooks/<logbook_uuid>/import

# Export ADIF
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/v1/logbooks/<logbook_uuid>/export?format=adif"
```

## Troubleshooting

- **Unknown fields in source ADIF:** retained in `extra`, not dropped.
- **Large import appears stalled:** use job status endpoint or SSE stream.
- **Unexpected duplicates:** tighten dedupe window and normalize timestamps.
- **Looking for an ADIF version picker:** current export is fixed to canonical 3.1.7 output.
