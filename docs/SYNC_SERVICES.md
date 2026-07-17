# Sync Services

## Overview

Automated bidirectional sync with major ham radio log services. This is the feature that will differentiate us — nobody does this well today.

## Services

### LoTW (Logbook of The World)
- **Operator**: ARRL
- **Auth**: TLS client certificates (tQSL-generated .p12 files)
- **Upload**: Signed ADIF via HTTPS POST
- **Download**: ADIF query with date filters
- **Rate limits**: Unclear/undocumented, be conservative (1 req/30s)
- **Quirks**: 
  - Must use tQSL to sign uploads (or implement the signing ourselves)
  - Certificate management is the #1 pain point for users
  - Their server can be slow/down during contest weekends
  - Download returns only confirmed QSOs by default
- **Priority**: HIGH — most hams care about LoTW confirmations

### QRZ.com Logbook
- **Auth**: API key (from QRZ subscription)
- **Upload**: XML API
- **Download**: XML API with filters
- **Rate limits**: Documented, reasonable
- **Quirks**:
  - Requires paid QRZ subscription for API access
  - Their API is actually pretty decent
  - Callsign lookup data also available (useful for auto-fill)
- **Priority**: HIGH

### eQSL.cc
- **Auth**: Username/password (no OAuth)
- **Upload**: ADIF via HTTP POST
- **Download**: ADIF inbox query
- **Rate limits**: Aggressive, must throttle
- **Quirks**:
  - Authentication is basic HTTP
  - QSL card image generation is their main draw
  - Inbox vs Outbox model
- **Priority**: MEDIUM

### ClubLog
- **Auth**: API key + email
- **Upload**: ADIF via HTTPS POST
- **Download**: Limited — mainly for DXCC status
- **Quirks**:
  - Good DXCC entity resolution
  - Propagation data integration
  - Their upload API is straightforward
- **Priority**: MEDIUM

### HamQTH
- **Auth**: Session-based (login → session ID)
- **Upload**: ADIF POST
- **Download**: Limited
- **Priority**: LOW (but easy to implement)

### POTA (Parks on the Air)
- **Auth**: TBD — they have an API but it's not fully public
- **Upload**: ADIF format, specific field requirements
- **Quirks**:
  - Specific ADIF field requirements (MY_SIG=POTA, MY_SIG_INFO=park ref)
  - Activation validation rules
  - Batch upload preferred
- **Priority**: HIGH (POTA is huge right now)

### SOTA (Summits on the Air)
- **Auth**: API with login
- **Upload**: CSV format (not ADIF)
- **Quirks**:
  - Uses their own CSV format, not ADIF
  - Activation vs chaser log distinction
  - Summit reference validation
- **Priority**: MEDIUM

## Sync Architecture

```
┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│   QSO Table  │────▶│   Sync Queue     │────▶│  Service     │
│              │     │                  │     │  Adapter     │
│              │◀────│  (per-service    │◀────│              │
│              │     │   per-QSO)       │     │  (LoTW/QRZ/  │
│              │     │                  │     │   eQSL/etc)  │
└──────────────┘     └──────────────────┘     └──────────────┘
```

### Sync Flow

1. **Outbound** (local → service):
   - New/updated QSO triggers sync_status row (status='pending')
   - Background worker picks up pending items per service
   - Adapter formats and uploads
   - Update sync_status on success/failure

2. **Inbound** (service → local):
   - Scheduled pull from each service (configurable interval)
   - Download new/updated QSOs
   - Match against existing QSOs using composite key
   - Update confirmation status, merge new fields
   - Flag conflicts for manual review

### Conflict Resolution

When the same QSO has different data locally vs remotely:
- **Confirmation status**: Remote always wins (that's the whole point)
- **Core fields** (callsign, band, mode, time): Flag for review, don't auto-overwrite
- **Supplementary fields** (grid, name, QTH): Prefer most recently updated
- **User preference**: Per-service override (always trust / always ask / always keep local)

### Error Handling

- Exponential backoff on failures
- Per-service circuit breaker (if service is down, stop trying for N minutes)
- Error details stored in sync_status for user visibility
- Daily sync summary available (X uploaded, Y confirmed, Z errors)

## Credential Storage

- Service credentials encrypted at rest (user-specific encryption key)
- LoTW certificates: reference local tQSL installation ONLY — .p12 private keys MUST NEVER be stored on the server (see ARCHITECTURE.md constraint)
- Self-hosted users manage their own credential security

## Open Questions

- [ ] Should we implement tQSL signing natively or require users to have tQSL installed?
- [ ] Real-time sync (push on every QSO save) vs batch sync (periodic)?
- [ ] How to handle LoTW cert renewal flow in the UI?
- [ ] POTA API access — need to check their current developer program
