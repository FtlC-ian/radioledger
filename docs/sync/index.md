# Sync Services

> Automatically sync QSOs with LoTW, QRZ, eQSL, ClubLog, POTA, and SOTA.

RadioLedger connects to the major ham radio logging services and keeps them in sync automatically. Set it up once and never manually upload logs again.

## Supported Services

| Service | Sync type | Auth | Priority |
|---------|-----------|------|----------|
| [LoTW](lotw.md) | Bidirectional | tQSL cert (desktop client) | High |
| [QRZ Logbook](qrz.md) | Bidirectional | API key | High |
| [eQSL](eqsl.md) | Bidirectional | Username/password | Medium |
| [ClubLog](clublog.md) | Upload + DXCC status | API key | Medium |
| [POTA](pota.md) | Upload | POTA account | High |
| [SOTA](sota.md) | Upload | SOTA account | Medium |

## How Sync Works

### Outbound (RadioLedger → Service)

1. You log a QSO (manually, via desktop UDP capture, or mobile app)
2. RadioLedger creates a pending sync entry for each configured service
3. Background workers pick up pending items and upload to each service
4. Status updates to "uploaded" or "error" (with details if failed)

### Inbound (Service → RadioLedger)

1. Background workers poll each service on a schedule (default: every 5 minutes for LoTW/QRZ, hourly for others)
2. New confirmations and incoming QSOs are matched against your logbook
3. QSL status updates automatically
4. New QSOs from the service (e.g., from another device) are merged into your logbook

### Conflict Resolution

When the same QSO has different data locally vs. on a service:

- **Confirmation status**: Service always wins (that's the whole point)
- **Core fields** (callsign, band, mode, time): Flagged for review — RadioLedger doesn't auto-overwrite
- **Supplementary fields** (grid, name, QTH): Prefer most recently updated

## Sync Status

Each QSO shows per-service sync status. You can see the full sync history for any QSO in the detail view.

The **Sync Status** page (Settings → Sync) shows:
- Last successful sync per service
- Pending upload counts
- Error counts and recent error details
- Queue depths

## Error Handling

RadioLedger handles transient service outages gracefully:

- **Exponential backoff**: Failed uploads retry with increasing delays
- **Circuit breaker**: If a service is consistently down, RadioLedger stops retrying for a period (to avoid getting IP-banned). LoTW during contest weekends is a known failure mode.
- **Error visibility**: All sync errors appear in Settings → Sync with details

## Sync Configuration

Configure sync services in **Settings → Connected Services**. Each logbook can also have per-service sync enabled/disabled.

Default sync intervals:

| Service | Outbound interval | Inbound poll |
|---------|------------------|-------------|
| LoTW | Continuous (desktop client) | Hourly |
| QRZ | Every 5 minutes | Hourly |
| eQSL | Every 15 minutes | 4x daily |
| ClubLog | Every 5 minutes | Daily |
| POTA | On demand | N/A |
| SOTA | On demand | N/A |

TODO: Document interval configuration settings.

## In This Section

| Guide | What it covers |
|-------|---------------|
| [lotw.md](lotw.md) | LoTW setup, tQSL configuration, and troubleshooting |
| [qrz.md](qrz.md) | QRZ logbook API key setup |
| [eqsl.md](eqsl.md) | eQSL username/password setup |
| [clublog.md](clublog.md) | ClubLog API key and DXCC status |
| [pota.md](pota.md) | POTA log upload requirements |
| [sota.md](sota.md) | SOTA log upload (CSV format) |
| [worker-infrastructure.md](worker-infrastructure.md) | River worker infrastructure: rate limiting, circuit breaker, retry, metrics (developer reference) |

## Related

- [Getting Started: Connect Services](../getting-started/connect-services.md)
- [QSL Management](../user-guide/qsl-management.md)
- [Desktop Client (required for LoTW)](../desktop/index.md)
- [SYNC_SERVICES.md](../SYNC_SERVICES.md) — internal architecture reference
