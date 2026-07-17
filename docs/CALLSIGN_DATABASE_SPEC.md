# Callsign Database Spec
## RadioLedger's "Better QRZ"

---

## Data Sources

RadioLedger maintains a local cache of approximately **1.5 million callsign records** from regulatory databases worldwide.

### Supported Regulatory Sources (Synced Regularly)

| Source | Country | Format | Records (Approx) | Sync Worker |
|--------|---------|--------|-------------------|-------------|
| **FCC ULS** | USA | Pipe-delimited | 780,000 | `FCCSyncWorker` |
| **ISED** | Canada | CSV | 72,000 | `ISEDSyncWorker` |
| **ACMA** | Australia | CSV | 16,000 | `ACMASyncWorker` |
| **ANFR** | France | JSON/CSV | 14,000 | `ANFRSyncWorker` |
| **IFT** | Mexico | CSV | 4,000 | `IFTSyncWorker` |
| **RDI** | Netherlands | CSV | 12,000 | `RDISyncWorker` |
| **Ofcom** | UK | CSV | 75,000 | `OfcomSyncWorker` |
| **BNetzA** | Germany | PDF | 65,000 | `BNetzASyncWorker` |
| **JJ1WTL/MIC** | Japan | CSV | 380,000 | `JJ1WTLSyncWorker` |

### Supplemental Sources (Free, Supplemental Lookups)

- **HamDB.org**: Free API, aggregates FCC + some international data.
- **HamQTH.com**: Free XML lookup API for international callsigns contributed by users.
- **ITU Prefix Table**: Maps prefixes to DXCC entities for initial identification.

---

## Sync Architecture

### Worker Infrastructure

Syncing is handled by [River](https://riverqueue.com/) background workers.

- **Weekly/Monthly Sync**: Full refreshes of regulatory sources.
- **Daily Diffs**: Incremental updates for the FCC ULS (USA).
- **Background Parsing**: Dedicated parsers for each source (e.g., `api/internal/services/callsign/bnetza_parser.go`).
- **Metrics**: Track sync success rates, record counts, and job durations in Prometheus.

### Database Schema

```sql
-- Core callsign records from regulatory databases
CREATE TABLE callsign_records (
    id              BIGSERIAL PRIMARY KEY,
    callsign        TEXT NOT NULL,
    source          TEXT NOT NULL,  -- 'fcc', 'ised', 'ofcom', etc.
    source_id       TEXT,           -- License number, etc.
    
    -- Operator info
    first_name      TEXT,
    last_name       TEXT,
    full_name       TEXT,
    address_line1   TEXT,
    address_line2   TEXT,
    city            TEXT,
    state_province  TEXT,
    postal_code     TEXT,
    country         TEXT NOT NULL,  -- ISO 3166-1 alpha-2
    
    -- License details
    license_class   TEXT,
    grant_date      DATE,
    expiry_date     DATE,
    status          TEXT NOT NULL DEFAULT 'active',
    
    -- Location
    grid_square     TEXT,
    latitude        DOUBLE PRECISION,
    longitude       DOUBLE PRECISION,
    dxcc_entity_id  INTEGER REFERENCES dxcc_entities(id),
    
    -- Timestamps
    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE (callsign, source)
);
```

---

## Why This Wins

QRZ's callsign database is why people go there. RadioLedger builds the same database from public data, but:
1. **No Ads**: A clean, modern interface.
2. **No Subscription Wall**: Free, documented lookup API.
3. **Modern Profiles**: High-quality profiles without the "MySpace" look.
4. **Integrated**: Confirm contacts inline without leaving the logbook.
5. **API-First**: Open and accessible for third-party developers.

---

## Related

- [Callsign Parsing (Reference)](reference/callsign-parsing.md)
- [Sync Worker Infrastructure](sync/worker-infrastructure.md)
- [Admin Endpoints](api/endpoints/admin.md)
