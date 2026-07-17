# QSL Tracking & Awards

## QSL Confirmation Model

Every QSO has a unified confirmation status that aggregates across all sync services.

### Per-Service Status (sync_status table)
Each QSO tracks its state with each service independently:
- **not_sent** — haven't pushed to this service yet
- **pending** — uploaded, awaiting confirmation
- **confirmed** — both sides match, QSL confirmed
- **error** — upload/sync failed (with details)
- **rejected** — service rejected the QSO (mismatch, invalid data)

### Unified Confirmation View
Roll up per-service status into a single QSO-level confirmation summary:

```
W1AW  20m SSB  2026-02-28
  LoTW:    ✓ Confirmed (2026-03-02)
  QRZ:     ✓ Confirmed (2026-03-01)
  eQSL:    ◷ Pending
  ClubLog: ✓ Uploaded
  RadioLedger:  — (no internal QSL)
```

A QSO is "confirmed" for award purposes if ANY service confirms it (LoTW being the gold standard for DXCC).

### Internal QSL (RadioLedger-to-RadioLedger)
If both sides of a QSO are on RadioLedger, we can auto-confirm internally:
- Match by callsign + band + mode + time window
- Instant confirmation, no external service needed
- Display as a separate confirmation source
- This becomes more valuable as the user base grows

## Award Tracking

### DXCC (DX Century Club)
- Track confirmed DXCC entities per band and per mode
- Current vs deleted entities
- "Needs" list: entities not yet confirmed (or not yet worked)
- Progress toward endorsements (100, 200, 300, etc.)
- Mixed, Phone, CW, Digital, Satellite categories
- Challenge score (total band-entities confirmed on LoTW)

### WAS (Worked All States)
- 50 states map view with confirmed/worked/needed status
- Per-band and per-mode tracking
- Triple Play (Phone + CW + Digital all 50)

### VUCC (VHF/UHF Century Club)
- Grid square map with worked/confirmed overlay
- Per-band tracking (6m, 2m, 70cm, etc.)
- Grid count progress

### WAZ (Worked All Zones)
- 40 CQ zones world map
- Per-band tracking

### WPX (Worked All Prefixes)
- Prefix collection tracking
- Auto-extract prefix from callsign

### POTA Awards
- Activator: parks activated with QSO count per park
- Hunter: unique parks worked
- Honor Roll progress

### SOTA Awards
- Activator points (by summit value)
- Chaser points
- S2S count
- Mountain Goat / Shack Sloth progress

### Custom Awards
- Let users define custom tracking goals
- "Work all counties in Arkansas"
- "Work all grid squares in EM"
- Flexible enough to handle club awards, state QSO party awards, etc.

## Dashboard Views

### QSO Log View
- Sortable, filterable table
- Confirmation status icons per service (inline, compact)
- Click to expand full QSL detail
- Bulk actions: "upload all unsynced to LoTW"

### Award Progress Dashboard
- Visual cards for each award being tracked
- Progress bars/counts
- World map with DXCC entities colored by status
- US map for WAS
- Grid map for VUCC
- "Next needed" suggestions based on current progress

### Statistics Page
- QSOs by band, mode, year, month
- Countries worked over time (growth chart)
- Most worked callsigns/countries
- Operating patterns (time of day, day of week)
- Band/mode heatmap

### Confirmation Timeline
- Feed of recent confirmations ("W1AW confirmed on LoTW!")
- Daily/weekly confirmation summary
- This is the dopamine — seeing confirmations roll in

## Onboarding Flow

The first five minutes sell the platform:

1. **Sign up** (email or callsign-based)
2. **Upload ADIF** — drag and drop your existing log
3. **Instant gratification**: 
   - "You have 3,247 QSOs across 127 DXCC entities"
   - Show the world map lit up with their contacts
   - Band/mode breakdown
   - "You're 73% of the way to DXCC on 20m!"
4. **Connect services** — "Link your LoTW account to see confirmations"
5. **Desktop client** — "Install for auto-logging from WSJT-X"

The key: they see value BEFORE they connect anything. The ADIF upload alone should make them go "oh wow, this is way better than what I had."

## QSL Card Features (Future)

- Upload images of received QSL cards
- Generate printable QSL cards from templates
- eQSL electronic card design
- QSL card gallery (public profile feature)

## Schema Additions

```sql
-- Award tracking (which awards the user is pursuing)
CREATE TABLE award_tracking (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    award_type      TEXT NOT NULL,          -- 'dxcc', 'was', 'vucc', 'waz', 'wpx', 'pota', 'sota', 'custom'
    award_config    JSONB DEFAULT '{}',     -- band, mode, or custom criteria
    enabled         BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Materialized award progress (rebuilt on QSO changes)
CREATE TABLE award_progress (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id),
    award_type      TEXT NOT NULL,
    entity_key      TEXT NOT NULL,          -- DXCC number, state code, grid, etc.
    band            TEXT,
    mode            TEXT,
    first_qso_id    BIGINT REFERENCES qsos(id),
    confirmed       BOOLEAN DEFAULT FALSE,
    confirmed_via   TEXT,                   -- which service confirmed it
    confirmed_at    TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    UNIQUE(user_id, award_type, entity_key, band, mode)
);
CREATE INDEX idx_award_progress_user ON award_progress(user_id, award_type);
```

## Open Questions

- [ ] Should award progress be materialized tables or computed views? (Materialized is faster for dashboards but needs refresh triggers)
- [ ] LoTW confirmation matching: their time windows can be generous — how fuzzy do we match?
- [ ] Internal QSL: auto-confirm silently or require user acknowledgment?
- [ ] QSL card printing/mailing service integration? (Bureau, direct mail)
- [ ] Contest log checking — should we validate against published contest logs?
