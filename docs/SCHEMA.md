# Schema Design

**Last updated:** 2026-02-28

---

## Philosophy

The schema must handle the full complexity of ham radio logging while staying clean,
queryable, and multi-tenant safe from day one. ADIF defines 100+ fields, but most QSOs
only use 10-15. We normalize the common fields as typed columns and use JSONB for the
long tail.

Core tenets:
- Fast queries on common fields (callsign, band, mode, date, frequency)
- No data loss on ADIF import (uncommon fields land in JSONB)
- Clean foreign keys for bands, modes, DXCC entities
- Semantic-lossless ADIF round-trip fidelity: no field loss, canonical export output
- UUID external IDs — BIGSERIAL internal only, never exposed in the API
- Row-Level Security (RLS) on every tenant-scoped table — isolation is database-enforced,
  not just application-enforced. One missed WHERE clause in Go should not cause a breach.

---

## Extensions

```sql
CREATE EXTENSION IF NOT EXISTS postgis;      -- spatial queries, great-circle distance
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid(), crypt()
```

---

## Database Roles

```sql
-- The Go API server connects as this role. RLS policies apply to radioledger_api.
CREATE ROLE radioledger_api LOGIN;

-- Sync workers need cross-tenant read access for QSO matching.
-- All writes are scoped in application code; worker INSERT/UPDATE is explicitly bounded.
CREATE ROLE radioledger_worker LOGIN;

-- Migrations and maintenance run as the superuser or a BYPASSRLS role.
-- The radioledger_api role MUST NOT have BYPASSRLS.
```

---

## Multi-Tenant Strategy: Row-Level Security

**Decision:** Shared tables with PostgreSQL Row-Level Security (RLS).

Schema-per-tenant does not scale operationally (imagine running a migration across
10,000 schemas). Pure application-level filtering is one missed WHERE clause away from
a data breach. RLS gives database-level isolation that survives SQL injection.

**How it works:** Every request sets a session-local variable in a transaction before
running any query. The Go TenantMiddleware does this using `SET LOCAL` (transaction-scoped,
NOT session-scoped — critical for pgx connection pools).

```sql
-- Middleware sets this at the start of every authenticated transaction:
SET LOCAL app.current_user_id = '42';
```

```sql
-- Safe helper used by every RLS policy.
-- missing_ok=true prevents errors when middleware forgets to set the variable.
CREATE OR REPLACE FUNCTION app_current_user_id()
RETURNS BIGINT
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_user_id', true), '')::BIGINT;
$$;
```

All RLS policies call `app_current_user_id()` (wrapper around `current_setting('app.current_user_id', true)`), so missing context returns NULL/0 rows instead of throwing.

> **CRITICAL:** Use `SET LOCAL` (transaction-scoped), never bare `SET` (session-scoped).
> Session-scoped settings on a pooled connection will bleed into the next request.

**Integration test requirement:** Write tests that:
- Attempt to read User A\'s QSOs while authenticated as User B (must return 0 rows, not error)
- Attempt to UPDATE a QSO belonging to User A while authenticated as User B (must affect 0 rows)
- Verify that forgetting to set `app.current_user_id` returns 0 rows (not all rows)
- Verify SQL injection in a search field cannot bypass RLS

---

## Core Tables

### users

```sql
CREATE TABLE users (
    -- Internal PK. Used for FK references within the DB only.
    id                          BIGSERIAL PRIMARY KEY,

    -- External-facing identifier. Use this in all API URLs and responses.
    -- UUID v7 (time-ordered) preferred for insert-friendly B-tree behavior.
    uuid                        UUID NOT NULL DEFAULT gen_random_uuid(),

    -- Authentication
    email                       TEXT NOT NULL,
    password_hash               TEXT,                    -- null for OAuth/OIDC-only accounts
    email_verified_at           TIMESTAMPTZ,             -- NULL = unverified; gate feature access on this
    email_verification_token_hash TEXT,                   -- SHA-256 hash of one-time token; clear on verify. Never store plaintext tokens.

    -- Ham radio identity
    callsign                    TEXT,                    -- primary callsign; stored UPPERCASE
    callsign_verified_at        TIMESTAMPTZ,
    callsign_verification_source TEXT
        CHECK (callsign_verification_source IN ('qrz', 'hamdb', 'manual')),
    display_name                TEXT,
    grid_square                 TEXT,                    -- 4 or 6-char Maidenhead

    -- Station defaults applied to new logbooks
    default_power_watts         NUMERIC,                 -- watts; explicit unit to avoid dBm confusion

    -- User UI/logging preferences (web + desktop).
    -- Keys include timezone/default_band/default_mode/default_power/ui_theme/dedup_window
    -- plus optional desktop client ports and sync toggles.
    preferences                 JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Account metadata
    timezone                    TEXT NOT NULL DEFAULT 'UTC',
    subscription_tier           TEXT NOT NULL DEFAULT 'free'
        CHECK (subscription_tier IN ('free', 'standard', 'premium', 'club')),
    subscription_expires_at     TIMESTAMPTZ,
    last_login_at               TIMESTAMPTZ,

    -- Soft delete. GDPR: background job hard-deletes PII 30 days after deleted_at is set.
    deleted_at                  TIMESTAMPTZ,

    -- PostGIS: home station location, auto-computed from grid_square via trigger.
    -- SRID 4326 = WGS-84.
    location                    GEOMETRY(Point, 4326),

    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_users_uuid  UNIQUE (uuid),
    CONSTRAINT chk_users_grid CHECK (
        grid_square IS NULL
        OR grid_square ~ \'^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$\'
    )
);

CREATE UNIQUE INDEX idx_users_uuid      ON users(uuid);
CREATE INDEX         idx_users_callsign ON users(upper(callsign))
    WHERE callsign IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX  idx_users_email_ci_unique ON users(lower(email))
    WHERE deleted_at IS NULL;
CREATE INDEX         idx_users_location ON users USING GIST(location)
    WHERE location IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_self ON users
    FOR ALL TO radioledger_api
    USING  (id = app_current_user_id())
    WITH CHECK (id = app_current_user_id());

COMMENT ON TABLE  users IS \'One row per registered RadioLedger account. BIGSERIAL id is for internal joins only — always use uuid in API responses and URLs. RLS limits each session to its own row. Soft-deleted via deleted_at; a background GDPR job hard-deletes PII 30 days later.\';
COMMENT ON COLUMN users.uuid IS \'External-facing stable identifier. Use in all API responses and URLs. Never expose id (BIGSERIAL) externally — it leaks user count and enables enumeration attacks.\';
COMMENT ON COLUMN users.callsign IS \'Primary callsign. Always stored uppercase. Additional/historical callsigns go in user_callsigns table (club calls, vanity upgrades, /P operations).\';
COMMENT ON COLUMN users.default_power_watts IS \'Default transmit power in watts. Column name is explicit to prevent confusion with dBm. Hams routinely log in both scales.\';
COMMENT ON COLUMN users.location IS \'PostGIS Point (WGS-84) derived from grid_square via trigger. Used for distance calculations and nearest-station queries.\';
```

### user_callsigns

```sql
-- A ham may have multiple callsigns over their lifetime: original sequential call,
-- vanity call, club call, historical/expired calls, and portable suffixes that
-- change DXCC entity (/P, /MM, /DX-prefix).
CREATE TABLE user_callsigns (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    callsign        TEXT NOT NULL,     -- stored UPPERCASE
    license_class   TEXT
        CHECK (license_class IN ('novice','technician','general','advanced','extra','other')),
    country         TEXT,
    dxcc_entity     INTEGER,
    is_primary      BOOLEAN NOT NULL DEFAULT FALSE,
    valid_from      DATE,
    valid_to        DATE,              -- NULL = currently active
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_user_callsigns_uuid  UNIQUE (uuid),
    CONSTRAINT chk_user_callsign_upper CHECK (callsign = upper(callsign)),
    CONSTRAINT chk_user_callsign_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

-- Only one primary callsign per user at a time
CREATE UNIQUE INDEX idx_user_callsigns_primary ON user_callsigns(user_id)
    WHERE is_primary = TRUE;
CREATE INDEX idx_user_callsigns_callsign ON user_callsigns(callsign)
    WHERE valid_to IS NULL;

ALTER TABLE user_callsigns ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_callsigns_isolation ON user_callsigns
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE user_callsigns IS \'All callsigns associated with a user, current and historical. is_primary marks the callsign used for new QSOs. valid_to=NULL means currently active. The partial unique index on (user_id) WHERE is_primary enforces single-primary.\';
```

### station_locations

```sql
-- Required for LoTW. tQSL "station locations" are distinct from callsigns.
-- One callsign can have multiple locations: Home, POTA Portable, DXpedition.
-- Each location has its own tQSL certificate and possibly a different expiry.
-- Without this table, LoTW integration breaks for anyone operating from multiple locations.
CREATE TABLE station_locations (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                TEXT NOT NULL,      -- human label: "Home Station", "POTA Portable"
    callsign            TEXT NOT NULL,      -- callsign used from this location
    grid_square         TEXT NOT NULL,

    dxcc_entity         INTEGER,
    state               TEXT,
    county              TEXT,
    city                TEXT,
    country             TEXT,
    latitude            NUMERIC,
    longitude           NUMERIC,
    location            GEOMETRY(Point, 4326),    -- computed from lat/lon or grid

    -- LoTW: exact location name as configured in tQSL.
    -- Must match EXACTLY (case-sensitive) for successful LoTW upload.
    lotw_location_name  TEXT,
    -- Surface warnings at 60, 30, and 7 days before expiry.
    -- Cert renewal requires ARRL approval (days to weeks of lead time).
    lotw_cert_expiry    DATE,

    is_default          BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_station_locations_uuid UNIQUE (uuid),
    CONSTRAINT chk_stationloc_grid CHECK (
        grid_square ~ \'^[A-R]{2}[0-9]{2}([A-X]{2})?$\'
    )
);

CREATE UNIQUE INDEX idx_station_locations_uuid    ON station_locations(uuid);
CREATE UNIQUE INDEX idx_station_locations_default ON station_locations(user_id)
    WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX         idx_station_locations_user   ON station_locations(user_id)
    WHERE deleted_at IS NULL;
CREATE INDEX         idx_station_locations_geom   ON station_locations USING GIST(location)
    WHERE location IS NOT NULL;

ALTER TABLE station_locations ENABLE ROW LEVEL SECURITY;
CREATE POLICY station_locations_isolation ON station_locations
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  station_locations IS \'tQSL station locations. Not the same as a callsign — one callsign can have many locations (Home, POTA Portable, DXpedition), each with its own tQSL cert. Required for correct LoTW upload.\';
COMMENT ON COLUMN station_locations.lotw_location_name IS \'The exact string used as the "Station Location" in tQSL. Mismatch between this and what tQSL expects causes LoTW rejection. Copy character-for-character from the tQSL Callsign Certificate window.\';
COMMENT ON COLUMN station_locations.lotw_cert_expiry IS \'Date the tQSL certificate for this location expires. Renewing an ARRL cert takes days to weeks. The notification system surfaces warnings at 60, 30, and 7 days before expiry.\';
```

### logbooks

```sql
-- A user can have multiple logbooks: per-station, per-callsign, per-activity.
-- Examples: "Home Station", "POTA Portable", "Field Day 2026", "Contest Log W5YM"
CREATE TABLE logbooks (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    name                    TEXT NOT NULL,
    callsign                TEXT,            -- operating callsign for this logbook
    description             TEXT,

    -- Links to tQSL location for LoTW certificate matching
    station_location_id     BIGINT REFERENCES station_locations(id) ON DELETE SET NULL,
    grid_square             TEXT,

    default_power_watts     NUMERIC,
    logbook_type            TEXT NOT NULL DEFAULT \'general\'
        CHECK (logbook_type IN (
            \'general\', \'contest\', \'pota\', \'sota\', \'wwff\', \'club\', \'portable\'
        )),

    -- Duplicate detection window. FT8/FT4: 30 seconds (15-second protocol slots).
    -- CW contests: 300 seconds. General: 300 seconds.
    -- FT8 logs MUST use 30 here or valid contacts will be incorrectly flagged as dupes.
    dedup_window_seconds    INTEGER NOT NULL DEFAULT 300,

    is_default              BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_logbooks_uuid   UNIQUE (uuid),
    CONSTRAINT chk_logbooks_dedup CHECK (dedup_window_seconds BETWEEN 5 AND 3600),
    CONSTRAINT chk_logbooks_grid  CHECK (
        grid_square IS NULL
        OR grid_square ~ \'^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$\'
    )
);

CREATE UNIQUE INDEX idx_logbooks_uuid         ON logbooks(uuid);
-- Enforce exactly one default logbook per user
CREATE UNIQUE INDEX idx_logbooks_user_default ON logbooks(user_id)
    WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX         idx_logbooks_user        ON logbooks(user_id)
    WHERE deleted_at IS NULL;

ALTER TABLE logbooks ENABLE ROW LEVEL SECURITY;
CREATE POLICY logbook_isolation ON logbooks
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  logbooks IS \'Groups QSOs under a callsign/station/activity. One user can have many logbooks. is_default=TRUE selects the logbook for new QSOs; enforced unique by partial index. station_location_id links to the tQSL location for LoTW certificate matching.\';
COMMENT ON COLUMN logbooks.dedup_window_seconds IS \'Time window (seconds) for duplicate QSO detection. FT8 and FT4 contacts happen in 15-second windows — set to 30s for digital logbooks. Configurable per logbook because the right value depends on operating mode.\';
```

### operators

```sql
-- Human operators are distinct from station callsigns.
-- One operator can work under many callsigns; one callsign can have many operators.
CREATE TABLE operators (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    linked_user_id      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    operator_callsign   TEXT,
    display_name        TEXT NOT NULL,
    is_owner            BOOLEAN NOT NULL DEFAULT FALSE,
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_operators_uuid UNIQUE (uuid),
    CONSTRAINT chk_operators_callsign_upper CHECK (
        operator_callsign IS NULL OR operator_callsign = upper(operator_callsign)
    )
);

CREATE UNIQUE INDEX idx_operators_uuid ON operators(uuid);
CREATE INDEX idx_operators_user ON operators(user_id) WHERE active = TRUE;
CREATE UNIQUE INDEX idx_operators_owner_name ON operators(user_id, lower(display_name));

ALTER TABLE operators ENABLE ROW LEVEL SECURITY;
CREATE POLICY operators_isolation ON operators
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE operators IS 'Operator identity table (person-level identity). Distinct from callsigns. A single person can operate under multiple station callsigns; multiple people can operate under a single station callsign (club/contest).';
```

### station_callsigns

```sql
-- Callsigns are station identities, not people.
CREATE TABLE station_callsigns (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    callsign            TEXT NOT NULL,
    callsign_type       TEXT NOT NULL DEFAULT 'personal'
        CHECK (callsign_type IN ('personal', 'club', 'special_event', 'contest', 'guest')),
    description         TEXT,
    valid_from          DATE,
    valid_to            DATE,
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_station_callsigns_uuid UNIQUE (uuid),
    CONSTRAINT chk_station_callsign_upper CHECK (callsign = upper(callsign)),
    CONSTRAINT chk_station_callsign_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX idx_station_callsigns_uuid ON station_callsigns(uuid);
CREATE UNIQUE INDEX idx_station_callsigns_user_call_active ON station_callsigns(user_id, callsign)
    WHERE active = TRUE;

ALTER TABLE station_callsigns ENABLE ROW LEVEL SECURITY;
CREATE POLICY station_callsigns_isolation ON station_callsigns
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE station_callsigns IS 'Station callsign identity. Club calls, special event calls, and personal calls all live here. Not equivalent to person identity.';
```

### station_callsign_operators

```sql
-- M:N mapping between station callsigns and operators, with role and validity windows.
CREATE TABLE station_callsign_operators (
    id                  BIGSERIAL PRIMARY KEY,
    station_callsign_id BIGINT NOT NULL REFERENCES station_callsigns(id) ON DELETE CASCADE,
    operator_id         BIGINT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    role                TEXT NOT NULL DEFAULT 'operator'
        CHECK (role IN ('trustee', 'primary', 'operator', 'logger', 'guest')),
    valid_from          TIMESTAMPTZ,
    valid_to            TIMESTAMPTZ,
    is_default          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_station_callsign_operator UNIQUE (station_callsign_id, operator_id, role),
    CONSTRAINT chk_station_callsign_operator_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX idx_station_callsign_default_operator
    ON station_callsign_operators(station_callsign_id)
    WHERE is_default = TRUE;

ALTER TABLE station_callsign_operators ENABLE ROW LEVEL SECURITY;
CREATE POLICY station_callsign_operators_isolation ON station_callsign_operators
    FOR ALL TO radioledger_api
    USING (station_callsign_id IN (
        SELECT id FROM station_callsigns WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (station_callsign_id IN (
        SELECT id FROM station_callsigns WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE station_callsign_operators IS 'Authorization and attribution mapping between callsigns and operators. Supports club and contest multi-op operation.';
```

### contests

```sql
-- Contest catalog (seeded from known Cabrillo contest definitions).
CREATE TABLE contests (
    id                  BIGSERIAL PRIMARY KEY,
    contest_code        TEXT NOT NULL UNIQUE,    -- ADIF CONTEST_ID / Cabrillo CONTEST line
    name                TEXT NOT NULL,
    sponsor             TEXT,
    cabrillo_name       TEXT NOT NULL,
    exchange_schema     JSONB NOT NULL DEFAULT '{}'::jsonb,
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_contests_active ON contests(active) WHERE active = TRUE;

COMMENT ON TABLE contests IS 'Contest metadata used for category validation, exchange parsing, and Cabrillo export.';
```

### contest_sessions

```sql
-- One operating session/entry for a specific contest and logbook.
-- A session carries Cabrillo header/category metadata and links to many contest QSOs.
CREATE TABLE contest_sessions (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    logbook_id              BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE CASCADE,
    contest_id              BIGINT NOT NULL REFERENCES contests(id) ON DELETE RESTRICT,
    station_callsign_id     BIGINT NOT NULL REFERENCES station_callsigns(id) ON DELETE RESTRICT,

    name                    TEXT NOT NULL,
    starts_at               TIMESTAMPTZ,
    ends_at                 TIMESTAMPTZ,

    category_operator       TEXT NOT NULL CHECK (category_operator IN ('SINGLE-OP', 'MULTI-OP', 'CHECKLOG', 'SWL')),
    category_assisted       TEXT NOT NULL DEFAULT 'NON-ASSISTED' CHECK (category_assisted IN ('ASSISTED', 'NON-ASSISTED')),
    category_band           TEXT NOT NULL DEFAULT 'ALL',
    category_mode           TEXT NOT NULL DEFAULT 'MIXED',
    category_power          TEXT NOT NULL DEFAULT 'HIGH' CHECK (category_power IN ('QRP', 'LOW', 'HIGH')),
    category_station        TEXT NOT NULL DEFAULT 'FIXED' CHECK (category_station IN ('FIXED', 'MOBILE', 'PORTABLE', 'ROVER', 'EXPEDITION')),
    category_time           TEXT NOT NULL DEFAULT '24-HOURS',
    category_transmitter    TEXT NOT NULL DEFAULT 'ONE' CHECK (category_transmitter IN ('ONE', 'TWO', 'LIMITED', 'UNLIMITED', 'SWL')),
    category_overlay        TEXT,

    operators_line          TEXT,                   -- Cabrillo OPERATORS: comma-separated callsigns
    club_name               TEXT,
    location                TEXT,
    soapbox                 TEXT,
    claimed_score           BIGINT,

    exchange_sent           TEXT,
    cabrillo_version        TEXT NOT NULL DEFAULT '3.0',

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_contest_sessions_uuid UNIQUE (uuid),
    CONSTRAINT chk_contest_session_time CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
);

CREATE UNIQUE INDEX idx_contest_sessions_uuid ON contest_sessions(uuid);
CREATE INDEX idx_contest_sessions_user ON contest_sessions(user_id, starts_at DESC);
CREATE INDEX idx_contest_sessions_logbook ON contest_sessions(logbook_id, starts_at DESC);

ALTER TABLE contest_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_sessions_isolation ON contest_sessions
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE contest_sessions IS 'Native contest logging session. Holds multi-operator categories, Cabrillo header metadata, and links contest QSOs into one submission unit.';
```

### contest_session_operators

```sql
-- Explicit operator roster for each contest session.
CREATE TABLE contest_session_operators (
    id                  BIGSERIAL PRIMARY KEY,
    contest_session_id  BIGINT NOT NULL REFERENCES contest_sessions(id) ON DELETE CASCADE,
    operator_id         BIGINT NOT NULL REFERENCES operators(id) ON DELETE RESTRICT,
    role                TEXT NOT NULL DEFAULT 'operator'
        CHECK (role IN ('operator', 'logger', 'mentor', 'guest')),
    operating_from      TIMESTAMPTZ,
    operating_to        TIMESTAMPTZ,
    is_primary          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_contest_session_operator UNIQUE (contest_session_id, operator_id, role),
    CONSTRAINT chk_contest_session_operator_time CHECK (
        operating_to IS NULL OR operating_from IS NULL OR operating_to >= operating_from
    )
);

CREATE INDEX idx_contest_session_operators_session ON contest_session_operators(contest_session_id);

ALTER TABLE contest_session_operators ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_session_operators_isolation ON contest_session_operators
    FOR ALL TO radioledger_api
    USING (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE contest_session_operators IS 'Operator roster for contest sessions. Supports per-QSO attribution in multi-op categories and Cabrillo OPERATORS line generation.';
```

### qsos

```sql
-- The main event. One row = one radio contact (QSO).
-- Core ADIF fields are typed columns for query performance.
-- Uncommon/custom fields land in `extra` JSONB for semantic-lossless ADIF round-trip.
-- Soft-deleted via deleted_at. Never hard-delete QSOs from a live logbook.
-- Multi-tenant isolation via RLS on logbook_id.
CREATE TABLE qsos (
    -- Internal PK. FK references within the database only.
    id                  BIGSERIAL PRIMARY KEY,

    -- External-facing identifier.
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),

    -- Client-generated UUID for offline-first mobile sync.
    client_uuid         UUID,

    logbook_id          BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE RESTRICT,

    -- Dual-identity model: station identity + person identity
    station_callsign_id BIGINT REFERENCES station_callsigns(id) ON DELETE SET NULL,
    operator_id         BIGINT REFERENCES operators(id) ON DELETE SET NULL,

    -- ── Core contact fields ───────────────────────────────────────────
    callsign            TEXT NOT NULL,
    name                TEXT,
    qth                 TEXT,

    band                TEXT NOT NULL,
    mode                TEXT NOT NULL,
    submode             TEXT,

    -- Frequency is stored in Hz integer for deterministic precision.
    frequency_hz        BIGINT,
    freq_rx_hz          BIGINT,

    -- ── Time ─────────────────────────────────────────────────────────
    -- Always UTC in datetime_on/datetime_off.
    datetime_on         TIMESTAMPTZ NOT NULL,
    datetime_off        TIMESTAMPTZ,
    time_source         TEXT NOT NULL DEFAULT 'utc'
        CHECK (time_source IN ('utc', 'local_converted', 'assumed_utc')),
    source_timezone     TEXT,

    -- ── Signal reports ────────────────────────────────────────────────
    rst_sent            TEXT,
    rst_rcvd            TEXT,

    -- ── Power and equipment ───────────────────────────────────────────
    tx_power            NUMERIC(7,2),
    rx_pwr              NUMERIC(7,2),
    my_antenna          TEXT,
    my_rig              TEXT,

    -- ── Their location ────────────────────────────────────────────────
    gridsquare          TEXT,
    dxcc                INTEGER,
    country             TEXT,
    state               TEXT,
    county              TEXT,
    cq_zone             SMALLINT,
    itu_zone            SMALLINT,
    continent           TEXT,

    -- ── My location ───────────────────────────────────────────────────
    my_gridsquare       TEXT,
    my_city             TEXT,
    my_state            TEXT,
    my_country          TEXT,
    my_dxcc             INTEGER,

    -- ── Propagation data ──────────────────────────────────────────────
    sfi                 SMALLINT,
    a_index             SMALLINT,
    k_index             SMALLINT,

    -- ── Operator snapshot fields (ADIF compatibility) ────────────────
    operator            TEXT,
    station_callsign    TEXT,

    -- ── Contest linkage + ADIF contest exchange fields ───────────────
    contest_session_id  BIGINT REFERENCES contest_sessions(id) ON DELETE SET NULL,
    activation_id       BIGINT REFERENCES activations(id) ON DELETE SET NULL,
    contest_id          TEXT,
    srx                 TEXT,
    stx                 TEXT,
    srx_string          TEXT,
    stx_string          TEXT,

    -- ── Satellite ─────────────────────────────────────────────────────
    sat_name            TEXT,
    sat_mode            TEXT,
    prop_mode           TEXT,

    -- ── Awards and activities ─────────────────────────────────────────
    sota_ref            TEXT,
    my_sota_ref         TEXT,
    pota_refs           TEXT[],
    my_pota_refs        TEXT[],
    wwff_ref            TEXT,
    my_wwff_ref         TEXT,
    iota                TEXT,
    sig                 TEXT,
    sig_info            TEXT,

    -- ── QSL status ────────────────────────────────────────────────────
    qsl_sent            TEXT CHECK (qsl_sent IS NULL OR qsl_sent IN ('Y','N','R','I','Q')),
    qsl_sent_date       DATE,
    qsl_rcvd            TEXT CHECK (qsl_rcvd IS NULL OR qsl_rcvd IN ('Y','N','R','I','V')),
    qsl_rcvd_date       DATE,
    qsl_via             TEXT,

    lotw_qsl_sent       TEXT CHECK (lotw_qsl_sent IS NULL OR lotw_qsl_sent IN ('Y','N')),
    lotw_qsl_sent_date  DATE,
    lotw_qsl_rcvd       TEXT CHECK (lotw_qsl_rcvd IS NULL OR lotw_qsl_rcvd IN ('Y','N')),
    lotw_qsl_rcvd_date  DATE,

    eqsl_qsl_sent       TEXT CHECK (eqsl_qsl_sent IS NULL OR eqsl_qsl_sent IN ('Y','N')),
    eqsl_qsl_sent_date  DATE,
    eqsl_qsl_rcvd       TEXT CHECK (eqsl_qsl_rcvd IS NULL OR eqsl_qsl_rcvd IN ('Y','N')),
    eqsl_qsl_rcvd_date  DATE,

    lotw_confirmed_callsign TEXT,

    -- ── Notes ─────────────────────────────────────────────────────────
    comment             TEXT,
    notes               TEXT,

    -- ── Extensibility ─────────────────────────────────────────────────
    extra               JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- ── PostGIS spatial ───────────────────────────────────────────────
    my_location         GEOMETRY(Point, 4326),
    their_location      GEOMETRY(Point, 4326),
    distance_km         NUMERIC,

    -- ── Import tracking ───────────────────────────────────────────────
    source              TEXT,
    source_id           TEXT,

    -- ── Metadata ──────────────────────────────────────────────────────
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    -- ── Constraints ───────────────────────────────────────────────────
    CONSTRAINT uq_qsos_uuid                   UNIQUE (uuid),
    CONSTRAINT chk_qso_callsign_upper         CHECK (callsign = upper(callsign)),
    CONSTRAINT chk_qso_station_callsign_upper CHECK (
        station_callsign IS NULL OR station_callsign = upper(station_callsign)
    ),
    CONSTRAINT chk_qso_rst_sent               CHECK (rst_sent IS NULL OR length(rst_sent) <= 10),
    CONSTRAINT chk_qso_rst_rcvd               CHECK (rst_rcvd IS NULL OR length(rst_rcvd) <= 10),
    CONSTRAINT chk_qso_cq_zone                CHECK (cq_zone IS NULL OR cq_zone BETWEEN 1 AND 40),
    CONSTRAINT chk_qso_itu_zone               CHECK (itu_zone IS NULL OR itu_zone BETWEEN 1 AND 90),
    CONSTRAINT chk_qso_continent              CHECK (
        continent IS NULL OR continent IN ('NA','SA','EU','AF','AS','OC','AN')
    ),
    CONSTRAINT chk_qso_frequency              CHECK (
        frequency_hz IS NULL OR (frequency_hz > 0 AND frequency_hz < 300000000000)
    ),
    CONSTRAINT chk_qso_freq_rx                CHECK (
        freq_rx_hz IS NULL OR (freq_rx_hz > 0 AND freq_rx_hz < 300000000000)
    ),
    CONSTRAINT chk_qso_tx_power               CHECK (
        tx_power IS NULL OR (tx_power > 0 AND tx_power <= 50000)
    ),
    CONSTRAINT chk_qso_datetime_order         CHECK (
        datetime_off IS NULL OR datetime_off >= datetime_on
    ),
    CONSTRAINT chk_qso_gridsquare             CHECK (
        gridsquare IS NULL
        OR gridsquare ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    ),
    CONSTRAINT chk_qso_my_gridsquare          CHECK (
        my_gridsquare IS NULL
        OR my_gridsquare ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    )
);

-- ── Lean v1 indexes (add more only after pg_stat_statements evidence) ──────
CREATE UNIQUE INDEX idx_qsos_client_uuid ON qsos(client_uuid)
    WHERE client_uuid IS NOT NULL;

CREATE INDEX idx_qsos_logbook_datetime ON qsos(logbook_id, datetime_on DESC)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_datetime_brin ON qsos USING BRIN(datetime_on)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_logbook_callsign ON qsos(logbook_id, upper(callsign))
    WHERE deleted_at IS NULL;

-- Source identity (strong dedup when source provides immutable IDs)
CREATE UNIQUE INDEX idx_qsos_source_unique ON qsos(logbook_id, source, source_id)
    WHERE source_id IS NOT NULL AND deleted_at IS NULL;

-- Source/mode-aware dedup accelerator: includes station callsign, contest session,
-- submode, and frequency bucket (100 Hz) to reduce false positives.
CREATE INDEX idx_qsos_dedup_v1 ON qsos(
    logbook_id,
    upper(callsign),
    band,
    mode,
    COALESCE(submode, ''),
    COALESCE(station_callsign, ''),
    COALESCE(source, ''),
    COALESCE(contest_session_id, 0),
    (frequency_hz / 100),
    datetime_on
) WHERE deleted_at IS NULL;

-- Contest run-rate and dupe checks
CREATE INDEX idx_qsos_contest_lookup ON qsos(contest_session_id, upper(callsign), band, mode, datetime_on)
    WHERE contest_session_id IS NOT NULL AND deleted_at IS NULL;

-- ── RLS ──────────────────────────────────────────────────────────────────────
ALTER TABLE qsos ENABLE ROW LEVEL SECURITY;

CREATE POLICY qso_isolation ON qsos
    FOR ALL TO radioledger_api
    USING (logbook_id IN (
        SELECT id FROM logbooks
        WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (logbook_id IN (
        SELECT id FROM logbooks
        WHERE user_id = app_current_user_id()
    ));

-- Sync workers need read-only access across all tenants for QSO matching
CREATE POLICY qso_worker_read ON qsos FOR SELECT TO radioledger_worker USING (TRUE);

COMMENT ON TABLE  qsos IS 'Core QSO log table. Stores both station identity (station_callsign_id) and operator identity (operator_id) for correct club/contest attribution.';
COMMENT ON COLUMN qsos.station_callsign_id IS 'FK to station_callsigns identity row. This is the transmitting station identity for this QSO.';
COMMENT ON COLUMN qsos.operator_id IS 'FK to operators identity row. This is the human operator attribution for this QSO.';
COMMENT ON COLUMN qsos.time_source IS 'How timestamp interpretation was derived at import: utc, local_converted, or assumed_utc.';
COMMENT ON COLUMN qsos.source_timezone IS 'IANA TZ used when converting local timestamps to UTC (for audit + explainability).';
COMMENT ON COLUMN qsos.extra IS 'JSONB store for ADIF fields not mapped to typed columns. Keys use original ADIF field names (uppercase). Preserves semantic fidelity.';
```


### activations

```sql
CREATE TABLE activations (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    logbook_id          BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE RESTRICT,
    program             TEXT NOT NULL CHECK (program IN ('POTA', 'SOTA', 'WWFF', 'IOTA')),
    reference           TEXT NOT NULL,
    activation_date     DATE NOT NULL,
    station_location_id BIGINT REFERENCES station_locations(id) ON DELETE SET NULL,
    notes               TEXT,
    status              TEXT NOT NULL DEFAULT 'in_progress'
        CHECK (status IN ('in_progress', 'valid', 'submitted')),
    qso_count           INTEGER NOT NULL DEFAULT 0,
    unique_callsigns    INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_activations_uuid UNIQUE (uuid),
    CONSTRAINT chk_activations_reference_upper CHECK (reference = UPPER(reference)),
    CONSTRAINT chk_activations_reference_format CHECK (
        (program = 'POTA' AND reference ~ '^[A-Z]{1,3}-[0-9]{1,5}$')
        OR (program = 'SOTA' AND reference ~ '^[A-Z0-9]{1,3}/[A-Z]{2}-[0-9]{3}$')
        OR (program NOT IN ('POTA', 'SOTA'))
    )
);

CREATE INDEX idx_activations_user_program_date
    ON activations(user_id, program, activation_date DESC, created_at DESC);

ALTER TABLE activations ENABLE ROW LEVEL SECURITY;

CREATE POLICY activations_select ON activations
    FOR SELECT TO radioledger_api
    USING (user_id = app_current_user_id());

CREATE POLICY activations_insert ON activations
    FOR INSERT TO radioledger_api
    WITH CHECK (
        user_id = app_current_user_id()
        AND app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
    );

COMMENT ON TABLE activations IS 'Portable activation sessions (POTA/SOTA/etc.) used to group QSOs by activity reference and date for validation, awards, and service-specific exports.';
COMMENT ON COLUMN qsos.activation_id IS 'Optional FK linking a QSO directly to an activation session. Used when QSOs are logged inside activation workflows.';
```

### contest_qso_exchange

```sql
-- Contest-specific per-QSO data normalized for scoring and Cabrillo export.
CREATE TABLE contest_qso_exchange (
    qso_id               BIGINT PRIMARY KEY REFERENCES qsos(id) ON DELETE CASCADE,
    contest_session_id   BIGINT NOT NULL REFERENCES contest_sessions(id) ON DELETE CASCADE,
    sent_serial          INTEGER,
    recv_serial          INTEGER,
    sent_exchange        TEXT,
    recv_exchange        TEXT,
    qso_points           SMALLINT,
    is_dupe              BOOLEAN NOT NULL DEFAULT FALSE,
    multiplier_credit    JSONB NOT NULL DEFAULT '{}'::jsonb,
    cabrillo_qso_line    TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_contest_serial_sent CHECK (sent_serial IS NULL OR sent_serial > 0),
    CONSTRAINT chk_contest_serial_recv CHECK (recv_serial IS NULL OR recv_serial > 0)
);

CREATE INDEX idx_contest_qso_exchange_session ON contest_qso_exchange(contest_session_id);
CREATE INDEX idx_contest_qso_exchange_serials ON contest_qso_exchange(contest_session_id, sent_serial, recv_serial);

ALTER TABLE contest_qso_exchange ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_qso_exchange_isolation ON contest_qso_exchange
    FOR ALL TO radioledger_api
    USING (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE contest_qso_exchange IS 'Per-QSO contest exchange + serial information for native contest logging and Cabrillo generation.';
```

### contest_multipliers

```sql
-- First-worked multipliers for contest scoring (band/mode aware when needed).
CREATE TABLE contest_multipliers (
    id                  BIGSERIAL PRIMARY KEY,
    contest_session_id  BIGINT NOT NULL REFERENCES contest_sessions(id) ON DELETE CASCADE,
    multiplier_type     TEXT NOT NULL, -- e.g. dxcc, zone, state, grid, prefix, custom
    multiplier_key      TEXT NOT NULL,
    band                TEXT,
    mode                TEXT,
    first_qso_id        BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    value               SMALLINT NOT NULL DEFAULT 1,
    worked_at           TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_contest_multipliers_session ON contest_multipliers(contest_session_id, multiplier_type);
CREATE UNIQUE INDEX idx_contest_multipliers_unique ON contest_multipliers (
    contest_session_id,
    multiplier_type,
    multiplier_key,
    COALESCE(band, ''),
    COALESCE(mode, '')
);

ALTER TABLE contest_multipliers ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_multipliers_isolation ON contest_multipliers
    FOR ALL TO radioledger_api
    USING (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE contest_multipliers IS 'Normalized multiplier tracking for contests. Used for live score calculation and Cabrillo claimed score support.';
```

### sync_status

```sql
-- Per-QSO sync state with each external service.
-- One row per (qso_id, service) combination.
CREATE TABLE sync_status (
    id              BIGSERIAL PRIMARY KEY,
    qso_id          BIGINT NOT NULL REFERENCES qsos(id) ON DELETE CASCADE,
    service         TEXT NOT NULL
        CHECK (service IN (
            \'lotw\', \'qrz\', \'eqsl\', \'clublog\', \'hamqth\',
            \'pota\', \'sota\', \'radioledger\'
        )),
    status          TEXT NOT NULL DEFAULT \'pending\'
        CHECK (status IN (
            \'pending\', \'uploaded\', \'confirmed\',
            \'error\', \'rejected\', \'not_applicable\', \'skipped\'
        )),
    last_synced_at  TIMESTAMPTZ,
    remote_id       TEXT,           -- ID in the remote system (LoTW record number, etc.)
    error_message   TEXT,

    -- Retry infrastructure for failed syncs (exponential backoff with jitter)
    retry_count     SMALLINT NOT NULL DEFAULT 0,
    next_retry_at   TIMESTAMPTZ,    -- when to retry; NULL = immediately eligible
    last_error_code TEXT,           -- structured error code for monitoring/alerting

    extra           JSONB NOT NULL DEFAULT \'{}\'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(qso_id, service)
);

-- Sync worker\'s main query: records that are pending or errored and ready to retry
CREATE INDEX idx_sync_status_pending ON sync_status(service, next_retry_at)
    WHERE status IN (\'pending\', \'error\');
CREATE INDEX idx_sync_status_qso     ON sync_status(qso_id);

ALTER TABLE sync_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_status_isolation ON sync_status
    FOR ALL TO radioledger_api
    USING (qso_id IN (
        SELECT q.id FROM qsos q
        JOIN logbooks lb ON lb.id = q.logbook_id
        WHERE lb.user_id = app_current_user_id()
    ));
CREATE POLICY sync_status_worker_read ON sync_status FOR SELECT TO radioledger_worker USING (TRUE);

COMMENT ON TABLE  sync_status IS \'Per-QSO sync state for each external service. ON DELETE CASCADE so deleting a QSO cleans up its sync rows. retry_count and next_retry_at drive exponential backoff in the sync worker.\';
COMMENT ON COLUMN sync_status.next_retry_at IS \'When to retry a failed sync. Set by the worker: NOW() + (2^retry_count * base_interval) + random_jitter. Jitter prevents thundering herd during LoTW outages — especially critical during contest weekends when LoTW is under heavy load.\';
```

---

## Reference Tables

### dxcc_entities

```sql
CREATE TABLE dxcc_entities (
    entity_id       INTEGER PRIMARY KEY,    -- ARRL-assigned DXCC entity number
    name            TEXT NOT NULL,          -- standard DXCC name
    -- LoTW sometimes uses different names than the DXCC list. Mismatch causes sync failures.
    -- e.g., "The Republic of North Macedonia" vs "Macedonia"
    lotw_entity_name TEXT,
    prefix          TEXT NOT NULL,          -- primary prefix
    continent       TEXT NOT NULL
        CHECK (continent IN (\'NA\',\'SA\',\'EU\',\'AF\',\'AS\',\'OC\',\'AN\')),
    cq_zone         SMALLINT CHECK (cq_zone BETWEEN 1 AND 40),
    itu_zone        SMALLINT CHECK (itu_zone BETWEEN 1 AND 90),
    latitude        NUMERIC,
    longitude       NUMERIC,
    location        GEOMETRY(Point, 4326),

    -- Deleted entities still valid for historical QSOs. Never delete rows.
    deleted         BOOLEAN NOT NULL DEFAULT FALSE,
    valid_from      DATE,
    valid_to        DATE
);

CREATE INDEX idx_dxcc_prefix   ON dxcc_entities(prefix);
CREATE INDEX idx_dxcc_location ON dxcc_entities USING GIST(location)
    WHERE location IS NOT NULL;

COMMENT ON TABLE  dxcc_entities IS \'ARRL DXCC entity list. Do not delete rows — deleted entities (e.g., East Germany) are still valid for historical QSOs and certain award endorsements. lotw_entity_name handles LoTW name discrepancies.\';
COMMENT ON COLUMN dxcc_entities.lotw_entity_name IS \'LoTW sometimes uses different entity names than the DXCC standard list. Store the LoTW variant here for matching during LoTW sync confirmation.\';
```

### bands

```sql
CREATE TABLE bands (
    name        TEXT PRIMARY KEY,   -- \'160m\', \'80m\', \'40m\', \'20m\', \'2m\', \'70cm\', etc.
    lower_freq  NUMERIC NOT NULL,   -- MHz lower edge
    upper_freq  NUMERIC NOT NULL,   -- MHz upper edge
    band_group  TEXT
        CHECK (band_group IN (\'HF\', \'VHF\', \'UHF\', \'SHF\', \'microwave\')),
    -- TRUE for the WARC bands (30m/10MHz, 17m/18MHz, 12m/24MHz).
    -- WARC bands are excluded from most contest categories (ARRL DX, CQ WW, etc.).
    -- Filter with WHERE warc = FALSE for contest-legal band lists.
    warc        BOOLEAN NOT NULL DEFAULT FALSE,

    CONSTRAINT chk_bands_freq CHECK (lower_freq < upper_freq AND lower_freq > 0)
);

COMMENT ON COLUMN bands.warc IS \'TRUE for the WARC bands (30m, 17m, 12m). These are excluded from most major contest categories. Filter WHERE warc = FALSE for contest-legal band lists.\';
```

### modes

```sql
CREATE TABLE modes (
    name        TEXT PRIMARY KEY,   -- \'SSB\', \'CW\', \'FT8\', \'FT4\', \'JS8\', \'RTTY\', etc.
    category    TEXT
        CHECK (category IN (\'PHONE\', \'CW\', \'DIGITAL\', \'IMAGE\')),
    -- ADIF canonical mapping. Many programs write "FT8" as the MODE field directly,
    -- not the spec-correct MFSK (mode) + FT8 (submode). The importer normalizes via this.
    adif_mode   TEXT,               -- canonical ADIF MODE field (e.g., \'MFSK\')
    adif_submode TEXT,              -- canonical ADIF SUBMODE (e.g., \'FT8\')
    submodes    TEXT[]              -- valid submodes for this mode
);

COMMENT ON TABLE modes IS \'Mode definitions and ADIF canonical name mapping. adif_mode and adif_submode handle the common case where programs write "FT8" as the mode field (not the spec-correct MFSK/FT8 pair). The ADIF importer normalizes on import using this mapping.\';
```

```sql
-- Enforce declared taxonomy in qsos after reference tables exist.
ALTER TABLE qsos
    ADD CONSTRAINT fk_qsos_band FOREIGN KEY (band) REFERENCES bands(name),
    ADD CONSTRAINT fk_qsos_mode FOREIGN KEY (mode) REFERENCES modes(name),
    ADD CONSTRAINT fk_qsos_dxcc FOREIGN KEY (dxcc) REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL,
    ADD CONSTRAINT fk_qsos_my_dxcc FOREIGN KEY (my_dxcc) REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;

ALTER TABLE user_callsigns
    ADD CONSTRAINT fk_user_callsigns_dxcc FOREIGN KEY (dxcc_entity) REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;

ALTER TABLE station_locations
    ADD CONSTRAINT fk_station_locations_dxcc FOREIGN KEY (dxcc_entity) REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;
```

### pota_parks

```sql
-- Updated weekly via POTA API (api.pota.app). POTA is the fastest-growing ham activity.
CREATE TABLE pota_parks (
    park_ref        TEXT PRIMARY KEY,   -- \'K-1234\' format
    name            TEXT NOT NULL,
    country         TEXT NOT NULL,
    state_province  TEXT,
    latitude        NUMERIC,
    longitude       NUMERIC,
    location        GEOMETRY(Point, 4326),
    park_type       TEXT,               -- \'NP\', \'NF\', \'SRA\', etc.
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pota_parks_country  ON pota_parks(country) WHERE active = TRUE;
CREATE INDEX idx_pota_parks_location ON pota_parks USING GIST(location)
    WHERE location IS NOT NULL AND active = TRUE;
```

### sota_summits

```sql
CREATE TABLE sota_summits (
    summit_ref      TEXT PRIMARY KEY,   -- \'W5N/CI-001\' format
    name            TEXT NOT NULL,
    association     TEXT NOT NULL,
    region          TEXT NOT NULL,
    elevation_m     INTEGER,
    points          SMALLINT,
    bonus_points    SMALLINT NOT NULL DEFAULT 0,
    latitude        NUMERIC,
    longitude       NUMERIC,
    location        GEOMETRY(Point, 4326),
    grid_square     TEXT,
    valid_from      DATE,
    valid_to        DATE,
    active          BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE INDEX idx_sota_location ON sota_summits USING GIST(location)
    WHERE location IS NOT NULL AND active = TRUE;
```

---

## Award Tables

### award_tracking

```sql
-- Which award programs each user is actively pursuing.
CREATE TABLE award_tracking (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Optional: scope to a specific logbook (POTA logbook vs home station DXCC)
    logbook_id  BIGINT REFERENCES logbooks(id) ON DELETE SET NULL,
    award_type  TEXT NOT NULL
        CHECK (award_type IN (
            \'dxcc\', \'was\', \'vucc\', \'waz\', \'wpx\',
            \'pota_hunter\', \'pota_activator\',
            \'sota_chaser\', \'sota_activator\',
            \'custom\'
        )),
    -- Flexible config: band/mode filters, custom criteria
    -- DXCC by band: {"band": "20m", "mode": "CW", "confirmed_only": true}
    -- Custom: {"description": "Work all counties in Arkansas", "entity_type": "county", "region": "AR"}
    award_config JSONB NOT NULL DEFAULT \'{}\'::jsonb,
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_award_tracking_uuid UNIQUE (uuid)
);

CREATE INDEX idx_award_tracking_user ON award_tracking(user_id) WHERE enabled = TRUE;

ALTER TABLE award_tracking ENABLE ROW LEVEL SECURITY;
CREATE POLICY award_tracking_isolation ON award_tracking
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  award_tracking IS \'Award programs a user is actively pursuing. logbook_id allows scoping: POTA activator awards to the POTA logbook only. award_config holds band/mode filters and custom criteria as JSONB.\';
COMMENT ON COLUMN award_tracking.award_config IS \'JSON configuration for this award instance. DXCC example: {"band": "20m", "mode": "CW", "confirmed_only": true}. Custom award: {"description": "Work all AR counties", "entity_type": "county", "region": "AR"}.\';
```

### award_progress

```sql
-- Materialized award progress. One row per (user, award_type, entity).
-- entity_key meaning by award type:
--   DXCC:     DXCC entity number as text (\'291\' = USA)
--   WAS:      state code (\'TX\', \'CA\')
--   VUCC:     4-char grid square (\'EM35\')
--   WAZ:      CQ zone number (\'14\')
--   WPX:      callsign prefix (\'W5\', \'DL1\')
--   POTA:     park reference (\'K-1234\')
--   SOTA:     summit reference (\'W5N/CI-001\')
CREATE TABLE award_progress (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    award_type          TEXT NOT NULL,
    entity_key          TEXT NOT NULL,          -- what was worked/confirmed
    band                TEXT,                   -- NULL = mixed/any band
    mode                TEXT,                   -- NULL = mixed/any mode
    first_qso_id        BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    confirmed           BOOLEAN NOT NULL DEFAULT FALSE,
    -- HOW the confirmation was received. Matters for award submission reporting.
    -- DXCC accepts LoTW, physical QSL via bureau, and eQSL (Authentic Level).
    confirmation_method TEXT,                   -- \'lotw\', \'eqsl\', \'qsl_card\', \'radioledger\'
    confirmed_via       TEXT,                   -- which specific service confirmed it
    confirmed_at        TIMESTAMPTZ,

    -- Background worker sets dirty=FALSE after recalculation
    dirty               BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, award_type, entity_key, band, mode)
    -- Uses NULLS NOT DISTINCT so NULL band/mode participate in ON CONFLICT
    -- (requires PostgreSQL 15+). See migration 001.
);

-- Added in migration 008: count, last timestamp, worked flag.
-- qso_count:   total QSOs contributing to this award entity.
-- last_qso_at: timestamp of the most recent contributing QSO.
-- worked:      TRUE once at least one QSO has been logged.
ALTER TABLE award_progress
    ADD COLUMN qso_count   BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_qso_at TIMESTAMPTZ,
    ADD COLUMN worked      BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_award_progress_user  ON award_progress(user_id, award_type);
CREATE INDEX idx_award_progress_dirty ON award_progress(user_id) WHERE dirty = TRUE;

ALTER TABLE award_progress ENABLE ROW LEVEL SECURITY;
CREATE POLICY award_progress_isolation ON award_progress
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  award_progress IS \'Materialized award progress. dirty=TRUE triggers recalculation by AwardProgressRefreshJob. confirmation_method records HOW confirmation happened — matters for award submission reporting (DXCC accepts LoTW, physical QSL, or eQSL Authentic Level).\';
COMMENT ON COLUMN award_progress.dirty IS \'Set TRUE by an AFTER trigger when a relevant QSO is inserted, updated, or deleted. AwardProgressRefreshJob finds dirty=TRUE rows and recalculates. Cleared to FALSE on successful recalculation.\';
```

---

## Infrastructure Tables

### import_jobs

```sql
-- Async ADIF import tracking. POST /import → 202 Accepted + job uuid.
CREATE TABLE import_jobs (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    logbook_id          BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE CASCADE,

    filename            TEXT,
    file_size_bytes     BIGINT,

    status              TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'complete', 'error', 'cancelled')),

    total_records       INTEGER,
    imported            INTEGER NOT NULL DEFAULT 0,
    skipped             INTEGER NOT NULL DEFAULT 0,
    duplicate           INTEGER NOT NULL DEFAULT 0,
    errors              INTEGER NOT NULL DEFAULT 0,
    warnings            INTEGER NOT NULL DEFAULT 0,

    source              TEXT NOT NULL DEFAULT 'web'
        CHECK (source IN ('web', 'api', 'desktop_client', 'sync_service')),
    dedup_strategy      TEXT NOT NULL DEFAULT 'skip'
        CHECK (dedup_strategy IN ('skip', 'overwrite', 'merge', 'flag')),
    timestamp_strategy  TEXT NOT NULL DEFAULT 'trust_utc'
        CHECK (timestamp_strategy IN ('trust_utc', 'interpret_local', 'detect_and_warn')),
    source_timezone     TEXT,
    adif_version        TEXT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,

    CONSTRAINT uq_import_jobs_uuid UNIQUE (uuid)
);

CREATE INDEX idx_import_jobs_uuid    ON import_jobs(uuid);
CREATE INDEX idx_import_jobs_user    ON import_jobs(user_id, created_at DESC);
CREATE INDEX idx_import_jobs_pending ON import_jobs(status)
    WHERE status IN ('pending', 'processing');

ALTER TABLE import_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY import_jobs_isolation ON import_jobs
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE import_jobs IS 'Async ADIF import jobs with summary counters. Detailed row-level parse/validation failures are stored in import_job_errors.';
COMMENT ON COLUMN import_jobs.timestamp_strategy IS 'UTC interpretation mode used during this import job: trust_utc, interpret_local, detect_and_warn.';
```

### import_job_errors

```sql
-- Row-based import failures/warnings for scalable error handling on dirty ADIF files.
CREATE TABLE import_job_errors (
    id                  BIGSERIAL PRIMARY KEY,
    import_job_id       BIGINT NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    severity            TEXT NOT NULL CHECK (severity IN ('error', 'warning')),
    record_number       INTEGER,
    line_number         INTEGER,
    adif_field          TEXT,
    reason_code         TEXT,
    reason_detail       TEXT NOT NULL,
    raw_fragment        TEXT,
    raw_record_hash     TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_import_job_errors_job ON import_job_errors(import_job_id, id);
CREATE INDEX idx_import_job_errors_job_severity ON import_job_errors(import_job_id, severity);
CREATE INDEX idx_import_job_errors_field ON import_job_errors(import_job_id, adif_field)
    WHERE adif_field IS NOT NULL;

ALTER TABLE import_job_errors ENABLE ROW LEVEL SECURITY;
CREATE POLICY import_job_errors_isolation ON import_job_errors
    FOR ALL TO radioledger_api
    USING (import_job_id IN (
        SELECT id FROM import_jobs WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (import_job_id IN (
        SELECT id FROM import_jobs WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE import_job_errors IS 'Per-record import errors/warnings. Replaces large JSONB arrays to avoid giant TOAST rows on bad imports.';
```

### user_service_credentials

```sql
-- Encrypted external service credentials: QRZ API key, eQSL password, ClubLog API key.
-- credentials is AES-256-GCM ciphertext ONLY. Plaintext never enters the database.
-- Encryption: AES-256-GCM with per-user derived key: HKDF(master_key, user_id + version).
-- The master key is in the application process (env var or KMS) — never in the DB.
-- key_version enables rolling key rotation without a massive one-shot re-encryption.
CREATE TABLE user_service_credentials (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service          TEXT NOT NULL
        CHECK (service IN (\'qrz\', \'eqsl\', \'clublog\', \'hamqth\', \'pota\')),
    credential_type  TEXT NOT NULL
        CHECK (credential_type IN (
            \'api_key\', \'username_password\', \'session\', \'oauth_token\'
        )),
    -- Format: 12-byte nonce || GCM ciphertext || 16-byte GCM tag
    credentials      BYTEA NOT NULL,
    -- Master key derivation version. Increment on rotation; decrypt handles all versions.
    key_version      INTEGER NOT NULL DEFAULT 1,
    expires_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ,
    last_verified_at TIMESTAMPTZ,    -- last successful test against the external service
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, service)
);

CREATE INDEX idx_user_service_creds_user ON user_service_credentials(user_id)
    WHERE is_active = TRUE;

ALTER TABLE user_service_credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_service_creds_isolation ON user_service_credentials
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  user_service_credentials IS \'Encrypted external service credentials. credentials is AES-256-GCM ciphertext; plaintext never enters the database. key_version enables rolling key rotation. eQSL uses HTTP Basic auth — we must store the actual password. Warn users in the UI to use a unique password for eQSL.\';
COMMENT ON COLUMN user_service_credentials.key_version IS \'Master key derivation version used to encrypt this row. On rotation: increment the app version, re-encrypt rows in a background job, update this field. Decryption code handles all historical versions.\';
```

### api_keys

```sql
-- API keys for automation and scripting.
-- Generate → Show once → Store hash. Never store plaintext.
-- Format: \'hamlog_\' + base64url(32 random bytes from crypto/rand) = ~50 chars
CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    uuid         UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,         -- user-visible label (e.g., "WSJT-X Home Station")
    -- SHA-256 of the full key. On authentication: hash incoming key, compare with this.
    key_hash     TEXT NOT NULL UNIQUE,
    -- First 8 chars for display. Lets users identify keys without exposing the secret.
    key_prefix   TEXT NOT NULL,
    -- Granular scopes for least-privilege access
    scopes       TEXT[] NOT NULL DEFAULT \'{}\'::text[],
    -- Optional: restrict key to specific IP ranges (CIDR)
    allowed_ips  INET[],
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_used_ip INET,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_api_keys_uuid UNIQUE (uuid)
);

CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix) WHERE revoked_at IS NULL;
CREATE INDEX idx_api_keys_user   ON api_keys(user_id)    WHERE revoked_at IS NULL;

ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
CREATE POLICY api_keys_isolation ON api_keys
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  api_keys IS \'API keys for scripting and automation. key_hash = SHA-256 of full key (plaintext shown once at creation, never stored). key_prefix = first 8 chars for display identification.\';
COMMENT ON COLUMN api_keys.scopes IS \'Least-privilege API scopes. Available: qsos:read, qsos:write, qsos:delete, adif:import, adif:export, sync:trigger, sync:status, logbooks:read, logbooks:write. A WSJT-X integration only needs [qsos:write].\';
```

### audit_log

```sql
-- QSO mutation audit trail. Required for contest log integrity and GDPR compliance.
-- Populated by AFTER trigger on qsos (and other sensitive tables).
-- Partitioned by month — old partitions can be archived/compressed.
-- On GDPR deletion: anonymize user_id and ip_address in place; do not delete the row.
CREATE TABLE audit_log (
    id          BIGSERIAL,
    user_id     BIGINT,             -- NULL for system/background actions
    table_name  TEXT NOT NULL,
    record_id   BIGINT NOT NULL,
    action      TEXT NOT NULL CHECK (action IN (\'INSERT\', \'UPDATE\', \'DELETE\')),
    old_values  JSONB,              -- NULL for INSERT
    new_values  JSONB,              -- NULL for DELETE
    changed_by  TEXT,               -- \'user\', \'sync_lotw\', \'sync_qrz\', \'import\', \'api\'
    ip_address  INET,               -- auto-anonymize after 90 days (GDPR)
    request_id  TEXT,               -- correlates with HTTP access log (X-Request-ID header)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (created_at);

-- Monthly partitions. Create in advance via PartitionMaintenanceJob (pg_cron or River cron).
-- Example:
-- CREATE TABLE audit_log_2026_02 PARTITION OF audit_log
--     FOR VALUES FROM (\'2026-02-01\') TO (\'2026-03-01\');

CREATE INDEX idx_audit_log_record ON audit_log(table_name, record_id);
CREATE INDEX idx_audit_log_user   ON audit_log(user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
-- API users can read their own audit entries
CREATE POLICY audit_log_read ON audit_log
    FOR SELECT TO radioledger_api
    USING (user_id = app_current_user_id());

-- CRITICAL: The audit trigger runs as the table owner (superuser/migration role),
-- but INSERT must also be allowed for radioledger_api when the trigger fires in
-- the API role's transaction context. Without this policy, all audit writes silently
-- fail and the audit trail is empty.
CREATE POLICY audit_log_insert ON audit_log
    FOR INSERT TO radioledger_api
    WITH CHECK (TRUE);

-- Worker role also needs to write audit entries (sync operations, background jobs)
CREATE POLICY audit_log_worker_insert ON audit_log
    FOR INSERT TO radioledger_worker
    WITH CHECK (TRUE);

CREATE POLICY audit_log_worker_read ON audit_log
    FOR SELECT TO radioledger_worker
    USING (TRUE);

COMMENT ON TABLE audit_log IS \'QSO mutation audit trail. Partitioned by month — automate monthly partition creation via PartitionMaintenanceJob. On GDPR account deletion: SET user_id=NULL, ip_address=NULL (anonymize in place) rather than deleting rows. The structural audit record is retained; PII is removed.\';
```

### notifications

```sql
-- In-app notification feed for the header bell, mobile feed, and import automation.
-- Current product-facing types: import_complete, import_failed, sync_complete,
-- qsl_confirmed, system_announcement.
CREATE TABLE notifications (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL CHECK (type IN (
                    'import_complete', 'import_failed', 'sync_complete',
                    'qsl_confirmed', 'system_announcement'
                )),
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    qso_id      BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    read_at     TIMESTAMPTZ,        -- NULL = unread
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_notifications_uuid UNIQUE (uuid)
);

CREATE INDEX idx_notifications_user_unread ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;
CREATE INDEX idx_notifications_user_feed ON notifications(user_id, read_at, created_at DESC);

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY notifications_isolation ON notifications
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

-- Worker role inserts system-generated notifications from River jobs.
CREATE POLICY notifications_worker_insert ON notifications
    FOR INSERT TO radioledger_worker
    WITH CHECK (TRUE);
```

### qsl_routes

```sql
-- Routing history for paper QSL cards (direct, bureau, manager), by worked callsign.
CREATE TABLE qsl_routes (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    worked_callsign     TEXT NOT NULL,
    route_type          TEXT NOT NULL CHECK (route_type IN ('direct', 'bureau', 'manager')),
    manager_callsign    TEXT,
    bureau_name         TEXT,
    valid_from          DATE,
    valid_to            DATE,
    source              TEXT NOT NULL DEFAULT 'manual',
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_qsl_route_worked_callsign_upper CHECK (worked_callsign = upper(worked_callsign)),
    CONSTRAINT chk_qsl_route_manager_callsign_upper CHECK (
        manager_callsign IS NULL OR manager_callsign = upper(manager_callsign)
    ),
    CONSTRAINT chk_qsl_route_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE INDEX idx_qsl_routes_user_callsign ON qsl_routes(user_id, worked_callsign);

ALTER TABLE qsl_routes ENABLE ROW LEVEL SECURITY;
CREATE POLICY qsl_routes_isolation ON qsl_routes
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE qsl_routes IS 'Paper QSL routing history. Captures manager and bureau changes over time for historically accurate card workflows.';
```

### paper_qsl_batches

```sql
-- Outgoing/incoming paper card workflow in explicit batches.
CREATE TABLE paper_qsl_batches (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    direction           TEXT NOT NULL CHECK (direction IN ('outgoing', 'incoming')),
    route_type          TEXT NOT NULL CHECK (route_type IN ('direct', 'bureau', 'manager')),
    station_callsign_id BIGINT REFERENCES station_callsigns(id) ON DELETE SET NULL,
    operator_id         BIGINT REFERENCES operators(id) ON DELETE SET NULL,
    label               TEXT,
    status              TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'printed', 'queued', 'sent', 'partially_received', 'closed', 'cancelled')),
    mailed_on           DATE,
    received_on         DATE,
    expected_reply_by   DATE,
    postage_cents       INTEGER,
    tracking_reference  TEXT,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_paper_qsl_batches_uuid UNIQUE (uuid)
);

CREATE INDEX idx_paper_qsl_batches_user ON paper_qsl_batches(user_id, created_at DESC);
CREATE INDEX idx_paper_qsl_batches_status ON paper_qsl_batches(user_id, status);

ALTER TABLE paper_qsl_batches ENABLE ROW LEVEL SECURITY;
CREATE POLICY paper_qsl_batches_isolation ON paper_qsl_batches
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE paper_qsl_batches IS 'Paper QSL lifecycle container. Tracks bureau/direct/manager batches from draft → printed → sent → received/closed.';
```

### paper_qsl_batch_items

```sql
CREATE TABLE paper_qsl_batch_items (
    id                  BIGSERIAL PRIMARY KEY,
    batch_id            BIGINT NOT NULL REFERENCES paper_qsl_batches(id) ON DELETE CASCADE,
    qso_id              BIGINT NOT NULL REFERENCES qsos(id) ON DELETE RESTRICT,
    worked_callsign     TEXT NOT NULL,
    card_status         TEXT NOT NULL DEFAULT 'queued'
        CHECK (card_status IN ('queued', 'printed', 'sent', 'received', 'confirmed', 'returned', 'lost')),
    sent_on             DATE,
    received_on         DATE,
    qsl_route_id        BIGINT REFERENCES qsl_routes(id) ON DELETE SET NULL,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_paper_qsl_batch_item UNIQUE (batch_id, qso_id),
    CONSTRAINT chk_paper_qsl_worked_callsign_upper CHECK (worked_callsign = upper(worked_callsign))
);

CREATE INDEX idx_paper_qsl_batch_items_batch ON paper_qsl_batch_items(batch_id);
CREATE INDEX idx_paper_qsl_batch_items_qso ON paper_qsl_batch_items(qso_id);
CREATE INDEX idx_paper_qsl_batch_items_status ON paper_qsl_batch_items(card_status);

ALTER TABLE paper_qsl_batch_items ENABLE ROW LEVEL SECURITY;
CREATE POLICY paper_qsl_batch_items_isolation ON paper_qsl_batch_items
    FOR ALL TO radioledger_api
    USING (batch_id IN (
        SELECT id FROM paper_qsl_batches WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (batch_id IN (
        SELECT id FROM paper_qsl_batches WHERE user_id = app_current_user_id()
    ));

-- SECURITY: FK to qsos(id) bypasses RLS — PostgreSQL FK checks run as the table owner,
-- not the current role. A malicious user could reference another tenant's qso_id.
-- This trigger enforces cross-tenant scope validation on INSERT/UPDATE.
CREATE OR REPLACE FUNCTION enforce_paper_qsl_item_scope() RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM qsos WHERE id = NEW.qso_id AND logbook_id IN (
            SELECT id FROM logbooks WHERE user_id = app_current_user_id()
        )
    ) THEN
        RAISE EXCEPTION 'qso_id % does not belong to the current user', NEW.qso_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_paper_qsl_item_scope
    BEFORE INSERT OR UPDATE ON paper_qsl_batch_items
    FOR EACH ROW EXECUTE FUNCTION enforce_paper_qsl_item_scope();

COMMENT ON TABLE paper_qsl_batch_items IS 'Per-QSO paper QSL workflow state. Enables bureau queue management and direct/manager tracking per card. Has scope enforcement trigger to prevent cross-tenant FK bypass.';
```

### callsign_cache

```sql
-- QRZ/HamDB rate limit protection. Prevents hammering callbook APIs during ADIF import.
-- A 100k-QSO import without this = 100k API calls. QRZ rate-limits aggressively.
CREATE TABLE callsign_cache (
    callsign    TEXT PRIMARY KEY,
    data        JSONB NOT NULL,     -- full callbook response (name, addr, grid, photo_url, etc.)
    source      TEXT NOT NULL CHECK (source IN (\'qrz\', \'hamdb\', \'lotw\', \'manual\')),
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,

    CONSTRAINT chk_callsign_cache_upper CHECK (callsign = upper(callsign))
);

CREATE INDEX idx_callsign_cache_expiry ON callsign_cache(expires_at);
```

---

## PostGIS Functions and Triggers

```sql
-- ─────────────────────────────────────────────────────────────────────────────
-- Maidenhead grid square → WGS-84 center point
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION maidenhead_to_point(grid TEXT)
RETURNS GEOMETRY(Point, 4326) AS $$
DECLARE
    lon NUMERIC;
    lat NUMERIC;
    g   TEXT;
BEGIN
    IF grid IS NULL OR length(grid) < 4 THEN
        RETURN NULL;
    END IF;
    g := upper(grid);
    -- Field: A-R × 18, each 20° lon × 10° lat
    lon := (ascii(substr(g, 1, 1)) - ascii(\'A\')) * 20 - 180;
    lat := (ascii(substr(g, 2, 1)) - ascii(\'A\')) * 10 - 90;
    -- Square: 0-9 × 10, each 2° lon × 1° lat
    lon := lon + (ascii(substr(g, 3, 1)) - ascii(\'0\')) * 2;
    lat := lat + (ascii(substr(g, 4, 1)) - ascii(\'0\')) * 1;
    IF length(g) >= 6 THEN
        -- Subsquare: A-X × 24, each ~5\' lon × ~2.5\' lat
        lon := lon + (ascii(substr(g, 5, 1)) - ascii(\'A\')) * (2.0 / 24);
        lat := lat + (ascii(substr(g, 6, 1)) - ascii(\'A\')) * (1.0 / 24);
        lon := lon + (1.0 / 24);   -- center of subsquare
        lat := lat + (0.5 / 24);
    ELSE
        lon := lon + 1.0;           -- center of square
        lat := lat + 0.5;
    END IF;
    RETURN ST_SetSRID(ST_MakePoint(lon, lat), 4326);
END;
$$ LANGUAGE plpgsql IMMUTABLE STRICT;

COMMENT ON FUNCTION maidenhead_to_point(TEXT) IS
    \'Converts a Maidenhead grid locator (4 or 6 chars) to the center point as WGS-84
    PostGIS geometry. Returns NULL for NULL or short input. IMMUTABLE: safe for index use.\';

-- ─────────────────────────────────────────────────────────────────────────────
-- QSO insert/update trigger: compute PostGIS geometry and great-circle distance
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION update_qso_locations() RETURNS TRIGGER AS $$
BEGIN
    NEW.my_location    := maidenhead_to_point(NEW.my_gridsquare);
    NEW.their_location := maidenhead_to_point(NEW.gridsquare);
    IF NEW.my_location IS NOT NULL AND NEW.their_location IS NOT NULL THEN
        -- ST_DistanceSphere: fast great-circle approximation in meters; divide by 1000 for km
        NEW.distance_km := ST_DistanceSphere(NEW.my_location, NEW.their_location) / 1000.0;
    ELSE
        NEW.distance_km := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Fire only when the grid columns change (not on every UPDATE to other columns)
CREATE TRIGGER trg_qso_locations
    BEFORE INSERT OR UPDATE OF my_gridsquare, gridsquare
    ON qsos
    FOR EACH ROW EXECUTE FUNCTION update_qso_locations();

-- ─────────────────────────────────────────────────────────────────────────────
-- User home station geometry from grid_square
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION update_user_location() RETURNS TRIGGER AS $$
BEGIN
    NEW.location := maidenhead_to_point(NEW.grid_square);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_user_location
    BEFORE INSERT OR UPDATE OF grid_square
    ON users
    FOR EACH ROW EXECUTE FUNCTION update_user_location();

-- ─────────────────────────────────────────────────────────────────────────────
-- QSO identity scope trigger:
-- operator_id, station_callsign_id, and contest_session_id must belong to the
-- same tenant user that owns the target logbook.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION enforce_qso_identity_scope() RETURNS TRIGGER AS $$
DECLARE
    owner_user_id BIGINT;
    ref_user_id   BIGINT;
BEGIN
    SELECT user_id INTO owner_user_id FROM logbooks WHERE id = NEW.logbook_id;
    IF owner_user_id IS NULL THEN
        RAISE EXCEPTION 'logbook_id % does not exist', NEW.logbook_id;
    END IF;

    IF NEW.operator_id IS NOT NULL THEN
        SELECT user_id INTO ref_user_id FROM operators WHERE id = NEW.operator_id;
        IF ref_user_id IS DISTINCT FROM owner_user_id THEN
            RAISE EXCEPTION 'operator_id % is not owned by logbook user %', NEW.operator_id, owner_user_id;
        END IF;
    END IF;

    IF NEW.station_callsign_id IS NOT NULL THEN
        SELECT user_id INTO ref_user_id FROM station_callsigns WHERE id = NEW.station_callsign_id;
        IF ref_user_id IS DISTINCT FROM owner_user_id THEN
            RAISE EXCEPTION 'station_callsign_id % is not owned by logbook user %', NEW.station_callsign_id, owner_user_id;
        END IF;
    END IF;

    IF NEW.contest_session_id IS NOT NULL THEN
        SELECT user_id INTO ref_user_id FROM contest_sessions WHERE id = NEW.contest_session_id;
        IF ref_user_id IS DISTINCT FROM owner_user_id THEN
            RAISE EXCEPTION 'contest_session_id % is not owned by logbook user %', NEW.contest_session_id, owner_user_id;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_qso_identity_scope
    BEFORE INSERT OR UPDATE OF logbook_id, operator_id, station_callsign_id, contest_session_id
    ON qsos
    FOR EACH ROW EXECUTE FUNCTION enforce_qso_identity_scope();
```

---

## ADIF Round-Trip Strategy

**Contract:** semantic-lossless fidelity, not byte-identical replay.

Byte-identical output is not realistic across normalization (field ordering, canonical casing,
Unicode normalization, line endings, and regenerated headers). The system guarantee is:

1. No ADIF field value loss on import/export.
2. Known fields normalize into typed columns.
3. Unknown fields remain in `extra` with original ADIF keys.
4. Canonical export is deterministic for equivalent QSO content.

### On import

1. Stream-parse ADIF (never load full file in memory).
2. Normalize field names to uppercase.
3. Parse known fields into typed columns.
4. Preserve unknown/duplicate tags in `extra._raw_fields` (ordered list for forensic replay).
5. Record normalization and timestamp interpretation in import metadata.
6. Persist row-level issues in `import_job_errors`.

### On export

1. Emit canonical ADIF headers/version metadata.
2. Serialize typed columns first in deterministic order.
3. Merge `extra` fields that are not shadowed by typed columns.
4. Emit `<EOR>` delimiter per record.

### POTA array conversion

```
Import:  "K-1234,K-5678"  →  {'K-1234', 'K-5678'}  (TEXT[])
Export:  {'K-1234', 'K-5678'}  →  "K-1234,K-5678"
Query:   WHERE 'K-1234' = ANY(pota_refs)
```

## Deduplication

Dedup is profile-based (source + mode aware), not one global rule.

### Matching inputs

Base identity dimensions:
- `logbook_id`
- `callsign` (uppercase)
- `band`
- `mode`
- `submode` (when present)
- `datetime_on` within `logbooks.dedup_window_seconds`

Tie-breakers to reduce false positives:
- `station_callsign`
- `contest_session_id`
- `frequency_hz` bucket (`/100` for 100 Hz granularity)
- `source`
- `source_id` (authoritative when present)

### Profiles

- **WSJT-X / JTDX digital**: narrow time windows (typically 30s), include frequency bucket and submode.
- **Contest**: include `contest_session_id` and station callsign; serial/exchange mismatch can force non-duplicate.
- **LoTW**: prefer remote `source_id`; time can drift.
- **Bulk ADIF import**: conservative match; when ambiguous, mark as `flag` per import strategy.

```sql
-- Application-level dedup query skeleton using idx_qsos_dedup_v1
SELECT id
FROM qsos
WHERE logbook_id = $1
  AND upper(callsign) = upper($2)
  AND band = $3
  AND mode = $4
  AND COALESCE(submode, '') = COALESCE($5, '')
  AND COALESCE(station_callsign, '') = COALESCE($6, '')
  AND COALESCE(contest_session_id, 0) = COALESCE($7, 0)
  AND COALESCE(source, '') = COALESCE($8, '')
  AND (frequency_hz / 100) IS NOT DISTINCT FROM ($9 / 100)
  AND datetime_on BETWEEN ($10 - make_interval(secs => $11))
                      AND ($10 + make_interval(secs => $11))
  AND deleted_at IS NULL;
```

## Schema Versioning

- Sequential migration files currently start with `001_initial_schema.sql` and
  `002_reconcile_legacy_schema.sql`.
- Location: `database/migrations/` (public repo)
- Tool: `goose` (Go-native, up/down, simple)
- Self-hosted: migrations run automatically at container startup before the API server starts
- Pre-Goose installations are baselined only when their complete schema and
  reference-data fingerprint matches an explicitly reviewed variant. Migration
  002 then normalizes legacy grants, policies, credential constraints, and ADIF
  band/mode catalogs without rebuilding tenant data.
- Always use `CREATE INDEX CONCURRENTLY` and `ADD CONSTRAINT ... NOT VALID` + `VALIDATE CONSTRAINT` on live tables
- Never `ALTER COLUMN TYPE` on large tables without the expand-contract pattern
- Never auto-run down migrations; require explicit operator action

---

## Open Questions — Resolved

| Question | Decision |
|----------|----------|
| Multi-tenant strategy | RLS + shared tables. Implemented above. |
| band/mode: FK vs CHECK | FK enforced on `qsos.band` and `qsos.mode` after reference tables load. Unknown values are quarantined as import errors, not silently inserted. |
| POTA_REF data type | `TEXT[]`. Add GIN only after observed query demand (not by default in v1). |
| Audit log | AFTER trigger + partitioned table, monthly. |
| UUID vs BIGSERIAL externally | UUIDs in all API responses. BIGSERIAL for internal joins. |
| Award progress refresh | Dirty-flag materialized table + background River job. |
| Dedup strategy | Source/mode-aware dedup with station callsign + contest + frequency tie-breakers. |
| Auth provider | Zitadel (Go-native, multi-tenant, Docker-friendly). |
