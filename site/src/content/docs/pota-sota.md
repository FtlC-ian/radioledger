---
title: POTA and SOTA Guide
description: Track portable activations in the field with activation-specific workflows.
sidebar:
  order: 5
---

RadioLedger supports activation-focused logging for both Parks on the Air (POTA) and Summits on the Air (SOTA).

## POTA workflow

1. Start a new activation and enter the park reference (example: `K-1234`).
2. Log QSOs as normal from the activation screen.
3. Track progress to minimum valid activation threshold.
4. Export/upload activation log when complete.

### POTA validation rules

- Minimum 10 unique callsigns for valid activation
- Station callsign and grid should be present
- `MY_SIG` / `MY_SIG_INFO` are enforced on export

## SOTA workflow

1. Start activation with summit reference (example: `W4C/WM-001`).
2. Log contacts and flag S2S contacts when applicable.
3. Validate activation criteria before upload.

### SOTA validation rules

- Minimum 4 contacts
- S2S contacts are tracked separately
- Upload pipeline supports SOTA-specific export handling

## Mobile field ops tips

- Sync before leaving coverage.
- Keep battery optimization disabled for active logging sessions.
- Use offline queue and sync after returning to service.

## API endpoints for activations

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/activations/pota` | Create POTA activation |
| `GET` | `/v1/activations/pota/{activation_uuid}/status` | Check POTA validity |
| `POST` | `/v1/activations/pota/{activation_uuid}/export` | Export POTA ADIF |
| `POST` | `/v1/activations/sota` | Create SOTA activation |
| `GET` | `/v1/activations/sota/{activation_uuid}/status` | Check SOTA validity |

## Related

- [ADIF Import and Export](./adif-import-export/)
- [API Reference](./api-reference/)
