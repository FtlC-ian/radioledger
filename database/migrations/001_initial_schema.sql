-- +goose Up
-- ═══════════════════════════════════════════════════════════════════════════════
-- RADIOLEDGER INITIAL SCHEMA
-- Consolidated from original migrations 001–014.
-- Organizes: extensions → roles → helper functions → tables (FK order) →
--            indexes → RLS policies → functions/triggers → grants.
-- ═══════════════════════════════════════════════════════════════════════════════


-- ─────────────────────────────────────────────────────────────────────────────
-- EXTENSIONS
-- ─────────────────────────────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS postgis;      -- spatial queries, great-circle distance
CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_uuid(), crypt()


-- ─────────────────────────────────────────────────────────────────────────────
-- DATABASE ROLES
-- ─────────────────────────────────────────────────────────────────────────────
-- The Go API server connects as this role. RLS policies apply to radioledger_api.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api') THEN
        CREATE ROLE radioledger_api LOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

-- Sync workers need cross-tenant read access for QSO matching.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker') THEN
        CREATE ROLE radioledger_worker LOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

-- Migrations and maintenance run as the superuser or a BYPASSRLS role.
-- The radioledger_api role MUST NOT have BYPASSRLS.


-- ─────────────────────────────────────────────────────────────────────────────
-- HELPER FUNCTIONS (referenced by RLS policies — must precede table RLS)
-- ─────────────────────────────────────────────────────────────────────────────

-- Safe helper used by every RLS policy.
-- missing_ok=true prevents errors when middleware forgets to set the variable.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_current_user_id()
RETURNS BIGINT
LANGUAGE sql
STABLE
AS $$
    SELECT NULLIF(current_setting('app.current_user_id', true), '')::BIGINT;
$$;
-- +goose StatementEnd

-- Maps RBAC role names to sortable rank values. owner > admin > operator > contributor > viewer.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_role_rank(role_name TEXT)
RETURNS INTEGER
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE lower(COALESCE(role_name, ''))
        WHEN 'viewer'      THEN 1
        WHEN 'contributor' THEN 2
        WHEN 'operator'    THEN 3
        WHEN 'admin'       THEN 4
        WHEN 'owner'       THEN 5
        ELSE 0
    END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION app_role_rank(TEXT) IS 'Maps RBAC role names to sortable rank values. owner > admin > operator > contributor > viewer.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: users
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE users (
    id                            BIGSERIAL PRIMARY KEY,
    uuid                          UUID NOT NULL DEFAULT gen_random_uuid(),

    -- Authentication
    email                         TEXT NOT NULL,
    password_hash                 TEXT,
    email_verified_at             TIMESTAMPTZ,
    email_verification_token_hash TEXT,

    -- Zitadel OIDC identity (migration 002)
    zitadel_id                    TEXT UNIQUE,

    -- Ham radio identity
    callsign                      TEXT,
    callsign_verified_at          TIMESTAMPTZ,
    callsign_verification_source  TEXT
        CHECK (callsign_verification_source IN ('qrz', 'hamdb', 'manual')),
    display_name                  TEXT,
    grid_square                   TEXT,

    default_power_watts           NUMERIC,
    onboarding_complete           BOOLEAN NOT NULL DEFAULT FALSE,

    -- Account metadata
    timezone                      TEXT NOT NULL DEFAULT 'UTC',
    subscription_tier             TEXT NOT NULL DEFAULT 'free'
        CHECK (subscription_tier IN ('free', 'standard', 'premium', 'club')),
    subscription_expires_at       TIMESTAMPTZ,
    last_login_at                 TIMESTAMPTZ,
    last_auto_sync_at             TIMESTAMPTZ,

    -- User-scoped UI and logging preferences (migration 007)
    preferences                   JSONB NOT NULL DEFAULT '{}'::jsonb,

    deleted_at                    TIMESTAMPTZ,
    location                      GEOMETRY(Point, 4326),
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_users_uuid  UNIQUE (uuid),
    CONSTRAINT chk_users_grid CHECK (
        grid_square IS NULL
        OR grid_square ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    )
);

CREATE UNIQUE INDEX idx_users_uuid            ON users(uuid);
CREATE INDEX         idx_users_callsign       ON users(upper(callsign))
    WHERE callsign IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX  idx_users_email_ci_unique ON users(lower(email))
    WHERE deleted_at IS NULL;
CREATE INDEX         idx_users_location       ON users USING GIST(location)
    WHERE location IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX         idx_users_zitadel_id     ON users(zitadel_id)
    WHERE zitadel_id IS NOT NULL;

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_self ON users
    FOR ALL TO radioledger_api
    USING  (id = app_current_user_id())
    WITH CHECK (id = app_current_user_id());

COMMENT ON TABLE  users IS 'One row per registered RadioLedger account. BIGSERIAL id is for internal joins only — always use uuid in API responses and URLs.';
COMMENT ON COLUMN users.uuid IS 'External-facing stable identifier. Use in all API responses and URLs. Never expose id (BIGSERIAL) externally.';
COMMENT ON COLUMN users.callsign IS 'Primary callsign. Always stored uppercase.';
COMMENT ON COLUMN users.default_power_watts IS 'Default transmit power in watts. Column name is explicit to prevent confusion with dBm.';
COMMENT ON COLUMN users.location IS 'PostGIS Point (WGS-84) derived from grid_square via trigger.';
COMMENT ON COLUMN users.zitadel_id IS 'Zitadel user subject claim (sub). Set on first Zitadel OIDC authentication. NULL for dev-mode users and pre-Zitadel accounts.';
COMMENT ON COLUMN users.preferences IS 'User-scoped UI and logging preferences JSONB payload. Expected keys include timezone, default_band, default_mode, default_power, ui_theme, dedup_window, desktop_udp_port, and desktop_rig_port.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: user_callsigns
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE user_callsigns (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    callsign        TEXT NOT NULL,
    license_class   TEXT
        CHECK (license_class IN ('novice','technician','general','advanced','extra','other')),
    country         TEXT,
    dxcc_entity     INTEGER,
    is_primary      BOOLEAN NOT NULL DEFAULT FALSE,
    valid_from      DATE,
    valid_to        DATE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_user_callsigns_uuid  UNIQUE (uuid),
    CONSTRAINT chk_user_callsign_upper CHECK (callsign = upper(callsign)),
    CONSTRAINT chk_user_callsign_dates CHECK (valid_to IS NULL OR valid_to >= valid_from)
);

CREATE UNIQUE INDEX idx_user_callsigns_primary  ON user_callsigns(user_id)
    WHERE is_primary = TRUE;
CREATE INDEX         idx_user_callsigns_callsign ON user_callsigns(callsign)
    WHERE valid_to IS NULL;

ALTER TABLE user_callsigns ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_callsigns_isolation ON user_callsigns
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE user_callsigns IS 'All callsigns associated with a user, current and historical.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: station_locations
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE station_locations (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                TEXT NOT NULL,
    callsign            TEXT NOT NULL,
    grid_square         TEXT NOT NULL,

    dxcc_entity         INTEGER,
    state               TEXT,
    county              TEXT,
    city                TEXT,
    country             TEXT,
    latitude            NUMERIC,
    longitude           NUMERIC,
    location            GEOMETRY(Point, 4326),

    lotw_location_name  TEXT,
    lotw_cert_expiry    DATE,

    is_default          BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_station_locations_uuid UNIQUE (uuid),
    CONSTRAINT chk_stationloc_grid CHECK (
        grid_square ~ '^[A-R]{2}[0-9]{2}([A-X]{2})?$'
    )
);

CREATE UNIQUE INDEX idx_station_locations_uuid        ON station_locations(uuid);
CREATE UNIQUE INDEX idx_station_locations_default     ON station_locations(user_id)
    WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX         idx_station_locations_user        ON station_locations(user_id)
    WHERE deleted_at IS NULL;
CREATE INDEX         idx_station_locations_geom        ON station_locations USING GIST(location)
    WHERE location IS NOT NULL;
-- Accelerates the daily CertExpiryCheckJob scan (migration 013)
CREATE INDEX         idx_station_locations_cert_expiry ON station_locations(lotw_cert_expiry ASC)
    WHERE lotw_cert_expiry IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE station_locations ENABLE ROW LEVEL SECURITY;
CREATE POLICY station_locations_isolation ON station_locations
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());
-- Worker needs to read station locations for cert expiry monitoring job (migration 013)
CREATE POLICY station_locations_worker_select ON station_locations
    FOR SELECT TO radioledger_worker
    USING (TRUE);

COMMENT ON TABLE  station_locations IS 'tQSL station locations. Not the same as a callsign — one callsign can have many locations.';
COMMENT ON COLUMN station_locations.lotw_location_name IS 'Exact string used as the "Station Location" in tQSL. Mismatch causes LoTW rejection.';
COMMENT ON COLUMN station_locations.lotw_cert_expiry IS 'Date the tQSL certificate for this location expires. Notification system surfaces warnings at 60, 30, and 7 days before expiry.';
COMMENT ON INDEX  idx_station_locations_cert_expiry IS 'Accelerates the daily CertExpiryCheckJob scan for approaching LoTW cert expirations.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: logbooks
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE logbooks (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    name                    TEXT NOT NULL,
    callsign                TEXT,
    description             TEXT,

    station_location_id     BIGINT REFERENCES station_locations(id) ON DELETE SET NULL,
    grid_square             TEXT,

    default_power_watts     NUMERIC,
    logbook_type            TEXT NOT NULL DEFAULT 'general'
        CHECK (logbook_type IN (
            'general', 'contest', 'pota', 'sota', 'wwff', 'club', 'portable'
        )),

    dedup_window_seconds    INTEGER NOT NULL DEFAULT 300,

    is_default              BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_logbooks_uuid   UNIQUE (uuid),
    CONSTRAINT chk_logbooks_dedup CHECK (dedup_window_seconds BETWEEN 5 AND 3600),
    CONSTRAINT chk_logbooks_grid  CHECK (
        grid_square IS NULL
        OR grid_square ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    )
);

CREATE UNIQUE INDEX idx_logbooks_uuid         ON logbooks(uuid);
CREATE UNIQUE INDEX idx_logbooks_user_default ON logbooks(user_id)
    WHERE is_default = TRUE AND deleted_at IS NULL;
CREATE INDEX         idx_logbooks_user        ON logbooks(user_id)
    WHERE deleted_at IS NULL;

-- RLS: RBAC policies defined after user_roles + app_has_logbook_min_role exist (below).
ALTER TABLE logbooks ENABLE ROW LEVEL SECURITY;

COMMENT ON TABLE  logbooks IS 'Groups QSOs under a callsign/station/activity.';
COMMENT ON COLUMN logbooks.dedup_window_seconds IS 'Time window (seconds) for duplicate QSO detection. FT8/FT4: use 30s. Configurable per logbook.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: user_roles  (migration 004)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE user_roles (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    logbook_id  BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'operator', 'contributor', 'viewer')),
    invited_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_user_roles_uuid         UNIQUE (uuid),
    CONSTRAINT uq_user_roles_logbook_user UNIQUE (logbook_id, user_id)
);

CREATE UNIQUE INDEX idx_user_roles_owner_per_logbook ON user_roles(logbook_id)
    WHERE role = 'owner';
CREATE INDEX         idx_user_roles_user    ON user_roles(user_id, logbook_id);
CREATE INDEX         idx_user_roles_logbook ON user_roles(logbook_id, role);

ALTER TABLE user_roles ENABLE ROW LEVEL SECURITY;

COMMENT ON TABLE  user_roles IS 'Per-logbook membership and RBAC role assignments.';
COMMENT ON COLUMN user_roles.role IS 'Role for this user within this specific logbook. Valid values: owner, admin, operator, contributor, viewer.';
COMMENT ON COLUMN user_roles.invited_by IS 'User who granted this membership/role. NULL for bootstrapped owner rows.';


-- ─────────────────────────────────────────────────────────────────────────────
-- FUNCTION: app_has_logbook_min_role  (migration 004)
-- Defined AFTER user_roles table exists.
-- SECURITY DEFINER avoids recursive RLS checks when evaluating membership in policies.
-- ─────────────────────────────────────────────────────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app_has_logbook_min_role(p_logbook_id BIGINT, p_user_id BIGINT, p_min_role TEXT)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public
AS $$
    SELECT COALESCE((
        SELECT app_role_rank(ur.role)
        FROM user_roles ur
        WHERE ur.logbook_id = p_logbook_id
          AND ur.user_id = p_user_id
        LIMIT 1
    ), 0) >= app_role_rank(p_min_role);
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION app_has_logbook_min_role(BIGINT, BIGINT, TEXT) IS 'Returns true when the user has at least the required role within the given logbook.';

REVOKE ALL ON FUNCTION app_has_logbook_min_role(BIGINT, BIGINT, TEXT) FROM PUBLIC;


-- ─────────────────────────────────────────────────────────────────────────────
-- RLS: logbooks (RBAC — migrations 004 + 009)
-- Applied after user_roles + app_has_logbook_min_role are defined.
-- logbooks_select includes direct owner check to handle INSERT...RETURNING
-- before the AFTER trigger populates user_roles (migration 009 fix).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE POLICY logbooks_select ON logbooks
    FOR SELECT TO radioledger_api
    USING (
        deleted_at IS NULL
        AND (
            user_id = app_current_user_id()
            OR app_has_logbook_min_role(id, app_current_user_id(), 'viewer')
        )
    );
CREATE POLICY logbooks_insert ON logbooks
    FOR INSERT TO radioledger_api
    WITH CHECK (user_id = app_current_user_id());
CREATE POLICY logbooks_update ON logbooks
    FOR UPDATE TO radioledger_api
    USING  (app_has_logbook_min_role(id, app_current_user_id(), 'admin'))
    WITH CHECK (app_has_logbook_min_role(id, app_current_user_id(), 'admin'));
CREATE POLICY logbooks_delete ON logbooks
    FOR DELETE TO radioledger_api
    USING (app_has_logbook_min_role(id, app_current_user_id(), 'owner'));


-- ─────────────────────────────────────────────────────────────────────────────
-- RLS: user_roles
-- ─────────────────────────────────────────────────────────────────────────────
CREATE POLICY user_roles_select ON user_roles
    FOR SELECT TO radioledger_api
    USING (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'viewer'));
CREATE POLICY user_roles_insert ON user_roles
    FOR INSERT TO radioledger_api
    WITH CHECK (
        app_has_logbook_min_role(logbook_id, app_current_user_id(), 'admin')
        OR (
            user_id = app_current_user_id()
            AND role = 'owner'
            AND EXISTS (
                SELECT 1 FROM logbooks lb
                WHERE lb.id = logbook_id
                  AND lb.user_id = app_current_user_id()
            )
        )
    );
CREATE POLICY user_roles_update ON user_roles
    FOR UPDATE TO radioledger_api
    USING  (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'admin'))
    WITH CHECK (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'admin'));
CREATE POLICY user_roles_delete ON user_roles
    FOR DELETE TO radioledger_api
    USING (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'admin'));


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: operators
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE UNIQUE INDEX idx_operators_uuid       ON operators(uuid);
CREATE INDEX         idx_operators_user       ON operators(user_id) WHERE active = TRUE;
CREATE UNIQUE INDEX  idx_operators_owner_name ON operators(user_id, lower(display_name));

ALTER TABLE operators ENABLE ROW LEVEL SECURITY;
CREATE POLICY operators_isolation ON operators
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE operators IS 'Operator identity table (person-level identity). Distinct from callsigns.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: station_callsigns
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE UNIQUE INDEX idx_station_callsigns_uuid             ON station_callsigns(uuid);
CREATE UNIQUE INDEX idx_station_callsigns_user_call_active ON station_callsigns(user_id, callsign)
    WHERE active = TRUE;

ALTER TABLE station_callsigns ENABLE ROW LEVEL SECURITY;
CREATE POLICY station_callsigns_isolation ON station_callsigns
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE station_callsigns IS 'Station callsign identity. Club calls, special event calls, and personal calls all live here.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: station_callsign_operators
-- ─────────────────────────────────────────────────────────────────────────────
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

COMMENT ON TABLE station_callsign_operators IS 'Authorization and attribution mapping between callsigns and operators.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: contests
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE contests (
    id                  BIGSERIAL PRIMARY KEY,
    contest_code        TEXT NOT NULL UNIQUE,
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


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: contest_sessions
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE contest_sessions (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    logbook_id              BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE CASCADE,
    contest_id              BIGINT NOT NULL REFERENCES contests(id) ON DELETE RESTRICT,
    station_callsign_id     BIGINT REFERENCES station_callsigns(id) ON DELETE RESTRICT,

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

    operators_line          TEXT,
    club_name               TEXT,
    location                TEXT,
    soapbox                 TEXT,
    claimed_score           BIGINT,
    exchange_sent           TEXT,
    cabrillo_version        TEXT NOT NULL DEFAULT '3.0',

    -- Live contest operation (migration 006)
    exchange_template       TEXT NOT NULL DEFAULT 'serial'
        CHECK (exchange_template IN ('serial', 'grid', 'state', 'zone', 'custom')),
    serial_counter          INTEGER NOT NULL DEFAULT 0 CHECK (serial_counter >= 0),
    status                  TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'finished', 'submitted')),

    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_contest_sessions_uuid UNIQUE (uuid),
    CONSTRAINT chk_contest_session_time CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at >= starts_at)
);

CREATE UNIQUE INDEX idx_contest_sessions_uuid    ON contest_sessions(uuid);
CREATE INDEX         idx_contest_sessions_user   ON contest_sessions(user_id, starts_at DESC);
CREATE INDEX         idx_contest_sessions_logbook ON contest_sessions(logbook_id, starts_at DESC);
CREATE INDEX         idx_contest_sessions_active  ON contest_sessions(user_id, status)
    WHERE status = 'active';

ALTER TABLE contest_sessions ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_sessions_isolation ON contest_sessions
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  contest_sessions IS 'Native contest logging session. Holds multi-operator categories, Cabrillo header metadata, and links contest QSOs.';
COMMENT ON COLUMN contest_sessions.exchange_template IS 'Controls UI entry fields and Cabrillo SENT-EXCH format. serial=auto counter; grid=Maidenhead; state=US state/province; zone=CQ/ITU zone; custom=free text.';
COMMENT ON COLUMN contest_sessions.serial_counter IS 'Monotonically increasing QSO serial. Incremented atomically by UPDATE...RETURNING; never client-supplied.';
COMMENT ON COLUMN contest_sessions.status IS 'active=contest in progress; finished=window closed; submitted=Cabrillo submitted to sponsor.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: contest_session_operators
-- ─────────────────────────────────────────────────────────────────────────────
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

COMMENT ON TABLE contest_session_operators IS 'Operator roster for contest sessions.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: activations  (migration 005)
-- Must be created BEFORE qsos because qsos.activation_id FK references this table.
-- ─────────────────────────────────────────────────────────────────────────────
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
    ),
    CONSTRAINT chk_activations_counts_nonnegative CHECK (
        qso_count >= 0 AND unique_callsigns >= 0
    )
);

CREATE INDEX idx_activations_user_program_date  ON activations(user_id, program, activation_date DESC, created_at DESC);
CREATE INDEX idx_activations_logbook_program_date ON activations(logbook_id, program, activation_date DESC);
CREATE INDEX idx_activations_status             ON activations(status, program)
    WHERE status IN ('in_progress', 'valid');

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
CREATE POLICY activations_update ON activations
    FOR UPDATE TO radioledger_api
    USING (user_id = app_current_user_id())
    WITH CHECK (
        user_id = app_current_user_id()
        AND app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
    );
CREATE POLICY activations_delete ON activations
    FOR DELETE TO radioledger_api
    USING (
        user_id = app_current_user_id()
        AND app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
    );

COMMENT ON TABLE  activations IS 'Portable activation sessions (POTA/SOTA/etc.) grouped by reference + date.';
COMMENT ON COLUMN activations.reference IS 'Program reference identifier: POTA park (e.g., K-1234) or SOTA summit (e.g., W4C/WM-001). Stored uppercase.';
COMMENT ON COLUMN activations.status IS 'Activation workflow state: in_progress, valid (minimum criteria met), or submitted (uploaded to target service).';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: qsos
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE qsos (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    client_uuid         UUID,

    logbook_id          BIGINT NOT NULL REFERENCES logbooks(id) ON DELETE RESTRICT,

    -- Dual-identity model
    station_callsign_id BIGINT REFERENCES station_callsigns(id) ON DELETE SET NULL,
    operator_id         BIGINT REFERENCES operators(id) ON DELETE SET NULL,

    -- Creator tracking for RBAC (migration 004)
    created_by_user_id  BIGINT REFERENCES users(id) ON DELETE SET NULL,

    -- Activation linkage (migration 005)
    activation_id       BIGINT REFERENCES activations(id) ON DELETE SET NULL,

    -- ── Core contact fields ───────────────────────────────────────────
    callsign            TEXT NOT NULL,
    name                TEXT,
    qth                 TEXT,

    band                TEXT NOT NULL,
    mode                TEXT NOT NULL,
    submode             TEXT,

    frequency_hz        BIGINT,
    freq_rx_hz          BIGINT,

    -- ── Time ─────────────────────────────────────────────────────────
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

    -- ── Propagation ───────────────────────────────────────────────────
    sfi                 SMALLINT,
    a_index             SMALLINT,
    k_index             SMALLINT,

    -- ── Operator snapshot fields (ADIF compatibility) ────────────────
    operator            TEXT,
    station_callsign    TEXT,

    -- ── Contest linkage ───────────────────────────────────────────────
    contest_session_id  BIGINT REFERENCES contest_sessions(id) ON DELETE SET NULL,
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

    qrz_qsl_rcvd        TEXT CHECK (qrz_qsl_rcvd IS NULL OR qrz_qsl_rcvd IN ('Y','N')),
    qrz_qsl_rcvd_date   DATE,
    clublog_qsl_rcvd    TEXT CHECK (clublog_qsl_rcvd IS NULL OR clublog_qsl_rcvd IN ('Y','N')),
    clublog_qsl_rcvd_date DATE,

    lotw_confirmed_callsign TEXT,

    comment             TEXT,
    notes               TEXT,
    extra               JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- ── PostGIS spatial ───────────────────────────────────────────────
    my_location         GEOMETRY(Point, 4326),
    their_location      GEOMETRY(Point, 4326),
    distance_km         NUMERIC,

    -- ── LoTW upload tracking ──────────────────────────────────────────
    -- lotw_sent_at marks when this QSO was last submitted to ARRL LoTW.
    -- lotw_sync_job_id is added via ALTER TABLE after lotw_sync_jobs is defined.
    lotw_sent_at        TIMESTAMPTZ,

    -- ── Import tracking ───────────────────────────────────────────────
    source              TEXT,
    source_id           TEXT,

    -- ── Metadata ──────────────────────────────────────────────────────
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT uq_qsos_uuid                   UNIQUE (uuid),
    CONSTRAINT chk_qso_callsign_upper         CHECK (callsign = upper(callsign)),
    CONSTRAINT chk_qso_station_callsign_upper CHECK (
        station_callsign IS NULL OR station_callsign = upper(station_callsign)
    ),
    CONSTRAINT chk_qso_rst_sent    CHECK (rst_sent IS NULL OR length(rst_sent) <= 10),
    CONSTRAINT chk_qso_rst_rcvd    CHECK (rst_rcvd IS NULL OR length(rst_rcvd) <= 10),
    CONSTRAINT chk_qso_cq_zone     CHECK (cq_zone IS NULL OR cq_zone BETWEEN 1 AND 40),
    CONSTRAINT chk_qso_itu_zone    CHECK (itu_zone IS NULL OR itu_zone BETWEEN 1 AND 90),
    CONSTRAINT chk_qso_continent   CHECK (
        continent IS NULL OR continent IN ('NA','SA','EU','AF','AS','OC','AN')
    ),
    CONSTRAINT chk_qso_frequency   CHECK (
        frequency_hz IS NULL OR (frequency_hz > 0 AND frequency_hz < 300000000000)
    ),
    CONSTRAINT chk_qso_freq_rx     CHECK (
        freq_rx_hz IS NULL OR (freq_rx_hz > 0 AND freq_rx_hz < 300000000000)
    ),
    CONSTRAINT chk_qso_tx_power    CHECK (
        tx_power IS NULL OR (tx_power > 0 AND tx_power <= 50000)
    ),
    CONSTRAINT chk_qso_datetime_order CHECK (
        datetime_off IS NULL OR datetime_off >= datetime_on
    ),
    CONSTRAINT chk_qso_gridsquare  CHECK (
        gridsquare IS NULL
        OR gridsquare ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    ),
    CONSTRAINT chk_qso_my_gridsquare CHECK (
        my_gridsquare IS NULL
        OR my_gridsquare ~ '^[A-R]{2}[0-9]{2}([A-X]{2}([a-x]{2})?)?$'
    )
);

CREATE UNIQUE INDEX idx_qsos_client_uuid     ON qsos(client_uuid) WHERE client_uuid IS NOT NULL;
CREATE INDEX         idx_qsos_logbook_datetime ON qsos(logbook_id, datetime_on DESC) WHERE deleted_at IS NULL;
CREATE INDEX         idx_qsos_datetime_brin  ON qsos USING BRIN(datetime_on) WHERE deleted_at IS NULL;
CREATE INDEX         idx_qsos_logbook_callsign ON qsos(logbook_id, upper(callsign)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX  idx_qsos_source_unique  ON qsos(logbook_id, source, source_id)
    WHERE source_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX         idx_qsos_dedup_v1 ON qsos(
    logbook_id, upper(callsign), band, mode,
    COALESCE(submode, ''), COALESCE(station_callsign, ''), COALESCE(source, ''),
    COALESCE(contest_session_id, 0), (frequency_hz / 100), datetime_on
) WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_contest_lookup ON qsos(contest_session_id, upper(callsign), band, mode, datetime_on)
    WHERE contest_session_id IS NOT NULL AND deleted_at IS NULL;
-- Creator index for RBAC (migration 004)
CREATE INDEX idx_qsos_logbook_creator ON qsos(logbook_id, created_by_user_id) WHERE deleted_at IS NULL;
-- DXCC entity index for ClubLog worker (migration 012)
CREATE INDEX idx_qsos_dxcc_entity ON qsos(dxcc) WHERE dxcc IS NOT NULL AND deleted_at IS NULL;
-- Activation linkage (migration 005)
CREATE INDEX idx_qsos_activation_id ON qsos(activation_id, datetime_on DESC)
    WHERE activation_id IS NOT NULL AND deleted_at IS NULL;

-- Statistics dashboard indexes (migration 011)
CREATE INDEX idx_qsos_stats_band ON qsos (logbook_id, band)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_stats_mode ON qsos (logbook_id, mode)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_stats_datetime ON qsos (logbook_id, datetime_on)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_stats_datetime_brin ON qsos USING BRIN (datetime_on)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_qsos_stats_country ON qsos (logbook_id, country)
    WHERE deleted_at IS NULL AND country IS NOT NULL AND country != '';
CREATE INDEX idx_qsos_stats_callsign ON qsos (logbook_id, callsign)
    WHERE deleted_at IS NULL;

ALTER TABLE qsos ENABLE ROW LEVEL SECURITY;

-- Granular RBAC policies (migration 004)
CREATE POLICY qso_select ON qsos
    FOR SELECT TO radioledger_api
    USING (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'viewer'));
CREATE POLICY qso_insert ON qsos
    FOR INSERT TO radioledger_api
    WITH CHECK (app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor'));
CREATE POLICY qso_update ON qsos
    FOR UPDATE TO radioledger_api
    USING (
        app_has_logbook_min_role(logbook_id, app_current_user_id(), 'operator')
        OR (
            app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
            AND created_by_user_id = app_current_user_id()
        )
    )
    WITH CHECK (
        app_has_logbook_min_role(logbook_id, app_current_user_id(), 'operator')
        OR (
            app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
            AND created_by_user_id = app_current_user_id()
        )
    );
CREATE POLICY qso_delete ON qsos
    FOR DELETE TO radioledger_api
    USING (
        app_has_logbook_min_role(logbook_id, app_current_user_id(), 'operator')
        OR (
            app_has_logbook_min_role(logbook_id, app_current_user_id(), 'contributor')
            AND created_by_user_id = app_current_user_id()
        )
    );
-- Sync workers need read-only access across all tenants for QSO matching
CREATE POLICY qso_worker_read ON qsos FOR SELECT TO radioledger_worker USING (TRUE);

COMMENT ON TABLE  qsos IS 'Core QSO log table. One row = one radio contact. Core ADIF fields typed; uncommon fields in extra JSONB.';
COMMENT ON COLUMN qsos.time_source IS 'How timestamp was derived at import: utc, local_converted, or assumed_utc.';
COMMENT ON COLUMN qsos.extra IS 'JSONB store for ADIF fields not mapped to typed columns. Keys use original ADIF field names (uppercase).';
COMMENT ON COLUMN qsos.created_by_user_id IS 'User who created this QSO. Contributors can only edit/delete their own QSOs.';
COMMENT ON COLUMN qsos.activation_id IS 'Optional FK to activation session. Used when QSOs are logged inside POTA/SOTA activation workflows.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: qso_confirmations (migration 010)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE qso_confirmations (
    id                  BIGSERIAL PRIMARY KEY,

    qso_id              BIGINT NOT NULL REFERENCES qsos(id),
    matched_qso_id      BIGINT REFERENCES qsos(id),

    our_callsign        TEXT NOT NULL,
    their_callsign      TEXT NOT NULL,
    band                TEXT NOT NULL,
    mode                TEXT NOT NULL,
    qso_date            DATE NOT NULL,
    qso_time            TIME NOT NULL,

    status              TEXT NOT NULL DEFAULT 'unconfirmed'
                            CHECK (status IN ('unconfirmed', 'pending', 'matched', 'confirmed', 'rejected')),

    our_verification    TEXT NOT NULL DEFAULT 'none'
                            CHECK (our_verification IN ('none', 'email', 'address', 'cross_verified', 'vouched')),
    their_verification  TEXT NOT NULL DEFAULT 'none'
                            CHECK (their_verification IN ('none', 'email', 'address', 'cross_verified', 'vouched')),

    lotw_confirmed      BOOLEAN DEFAULT FALSE,
    lotw_confirmed_at   TIMESTAMPTZ,
    eqsl_confirmed      BOOLEAN DEFAULT FALSE,
    eqsl_confirmed_at   TIMESTAMPTZ,
    qrz_confirmed       BOOLEAN DEFAULT FALSE,
    qrz_confirmed_at    TIMESTAMPTZ,

    rl_confirmed        BOOLEAN DEFAULT FALSE,
    rl_confirmed_at     TIMESTAMPTZ,

    confirmed_at        TIMESTAMPTZ,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_confirmations_qso_unique ON qso_confirmations (qso_id);
CREATE INDEX idx_confirmations_qso        ON qso_confirmations (qso_id);
CREATE INDEX idx_confirmations_matched    ON qso_confirmations (matched_qso_id) WHERE matched_qso_id IS NOT NULL;
CREATE INDEX idx_confirmations_callsigns  ON qso_confirmations (our_callsign, their_callsign);
CREATE INDEX idx_confirmations_status     ON qso_confirmations (status);
CREATE INDEX idx_confirmations_date       ON qso_confirmations (qso_date);

COMMENT ON TABLE qso_confirmations IS 'Links two QSO entries from different users confirming the same contact.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: operator_verifications (migration 010)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE operator_verifications (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    callsign        TEXT NOT NULL,

    method          TEXT NOT NULL
                        CHECK (method IN ('email', 'address', 'lotw_cross', 'qrz_cross', 'vouch')),
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'verified', 'expired', 'revoked')),

    verification_code   TEXT,
    verified_at         TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ,

    vouched_by          BIGINT REFERENCES users(id),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_verification_active
    ON operator_verifications (user_id, callsign, method)
    WHERE status = 'verified';

CREATE INDEX idx_verifications_user     ON operator_verifications (user_id);
CREATE INDEX idx_verifications_callsign ON operator_verifications (callsign);

COMMENT ON TABLE operator_verifications IS 'Identity verification records for operators per callsign.';

-- Cross-tenant QSO match function (SECURITY DEFINER bypasses RLS intentionally)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION find_qso_matches(
    p_our_callsign    TEXT,
    p_their_callsign  TEXT,
    p_band            TEXT,
    p_mode_group      TEXT,
    p_datetime_on     TIMESTAMPTZ,
    p_time_window     INTERVAL DEFAULT INTERVAL '30 minutes',
    p_exclude_user_id BIGINT   DEFAULT NULL
)
RETURNS TABLE (
    qso_id        BIGINT,
    user_id       BIGINT,
    their_callsign TEXT,
    our_callsign  TEXT,
    band          TEXT,
    mode          TEXT,
    datetime_on   TIMESTAMPTZ,
    confidence    NUMERIC
)
LANGUAGE sql
SECURITY DEFINER
STABLE
AS $$
    SELECT
        q.id,
        lb.user_id,
        q.callsign,
        q.station_callsign,
        q.band,
        q.mode,
        q.datetime_on,
        CASE
            WHEN upper(q.mode) = upper(p_mode_group) THEN 1.0
            ELSE 0.9
        END::NUMERIC AS confidence
    FROM qsos q
    JOIN logbooks lb ON lb.id = q.logbook_id
    WHERE
        upper(q.callsign) = upper(p_our_callsign)
        AND (
            upper(q.station_callsign) = upper(p_their_callsign)
            OR q.station_callsign IS NULL
        )
        AND lower(q.band) = lower(p_band)
        AND (
            upper(q.mode) = upper(p_mode_group)
            OR (p_mode_group = 'SSB'  AND upper(q.mode) IN ('USB', 'LSB', 'AM', 'SSB'))
            OR (p_mode_group = 'CW'   AND upper(q.mode) IN ('CW', 'CWR'))
            OR (p_mode_group = 'RTTY' AND upper(q.mode) IN ('RTTY', 'BAUDOT'))
        )
        AND q.datetime_on BETWEEN (p_datetime_on - p_time_window) AND (p_datetime_on + p_time_window)
        AND q.deleted_at IS NULL
        AND (p_exclude_user_id IS NULL OR lb.user_id != p_exclude_user_id)
    ORDER BY ABS(EXTRACT(EPOCH FROM (q.datetime_on - p_datetime_on))) ASC,
             confidence DESC
$$;
-- +goose StatementEnd


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: contest_qso_exchange
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_contest_qso_exchange_session  ON contest_qso_exchange(contest_session_id);
CREATE INDEX idx_contest_qso_exchange_serials  ON contest_qso_exchange(contest_session_id, sent_serial, recv_serial);
-- Non-dupe index for fast scoring (migration 006)
CREATE INDEX idx_contest_qso_exchange_non_dupe ON contest_qso_exchange(contest_session_id) WHERE NOT is_dupe;

ALTER TABLE contest_qso_exchange ENABLE ROW LEVEL SECURITY;
CREATE POLICY contest_qso_exchange_isolation ON contest_qso_exchange
    FOR ALL TO radioledger_api
    USING (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ))
    WITH CHECK (contest_session_id IN (
        SELECT id FROM contest_sessions WHERE user_id = app_current_user_id()
    ));

COMMENT ON TABLE contest_qso_exchange IS 'Per-QSO contest exchange and serial information for native contest logging and Cabrillo generation.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: contest_multipliers
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE contest_multipliers (
    id                  BIGSERIAL PRIMARY KEY,
    contest_session_id  BIGINT NOT NULL REFERENCES contest_sessions(id) ON DELETE CASCADE,
    multiplier_type     TEXT NOT NULL,
    multiplier_key      TEXT NOT NULL,
    band                TEXT,
    mode                TEXT,
    first_qso_id        BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    value               SMALLINT NOT NULL DEFAULT 1,
    worked_at           TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_contest_multipliers_session ON contest_multipliers(contest_session_id, multiplier_type);
CREATE UNIQUE INDEX idx_contest_multipliers_unique ON contest_multipliers (
    contest_session_id, multiplier_type, multiplier_key,
    COALESCE(band, ''), COALESCE(mode, '')
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

COMMENT ON TABLE contest_multipliers IS 'Normalized multiplier tracking for contests.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: sync_status
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sync_status (
    id              BIGSERIAL PRIMARY KEY,
    qso_id          BIGINT NOT NULL REFERENCES qsos(id) ON DELETE CASCADE,
    service         TEXT NOT NULL
        CHECK (service IN (
            'lotw', 'qrz', 'eqsl', 'clublog', 'hamqth',
            'pota', 'sota', 'radioledger'
        )),
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN (
            'pending', 'dirty', 'uploaded', 'confirmed',
            'error', 'rejected', 'not_applicable', 'skipped'
        )),
    last_synced_at  TIMESTAMPTZ,
    remote_id       TEXT,
    error_message   TEXT,

    retry_count     SMALLINT NOT NULL DEFAULT 0,
    next_retry_at   TIMESTAMPTZ,
    last_error_code TEXT,

    extra           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(qso_id, service)
);

CREATE INDEX idx_sync_status_pending ON sync_status(service, next_retry_at)
    WHERE status IN ('pending', 'dirty', 'error');
CREATE INDEX idx_sync_status_qso ON sync_status(qso_id);

ALTER TABLE sync_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_status_isolation ON sync_status
    FOR ALL TO radioledger_api
    USING (qso_id IN (
        SELECT q.id FROM qsos q
        JOIN logbooks lb ON lb.id = q.logbook_id
        WHERE lb.user_id = app_current_user_id()
    ));
CREATE POLICY sync_status_worker_read ON sync_status FOR SELECT TO radioledger_worker USING (TRUE);

COMMENT ON TABLE  sync_status IS 'Per-QSO sync state for each external service. ON DELETE CASCADE cleans up rows when QSO is deleted.';
COMMENT ON COLUMN sync_status.next_retry_at IS 'When to retry. Worker sets: NOW() + (2^retry_count * base_interval) + random_jitter to prevent thundering herd.';


-- ─────────────────────────────────────────────────────────────────────────────
-- NOTE: river_job and related queue tables are created by River migrations
-- (migrate-river) in the docker entrypoint. Do NOT create a compat table here
-- as it conflicts with River's own CREATE TABLE statements.
-- ─────────────────────────────────────────────────────────────────────────────


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: lotw_sync_jobs
-- One row per LoTW upload attempt. Tracks which QSOs were included, the ARRL
-- response, and retry state. Created before lotw_sync_status and the qsos FK
-- so the FK can be added via ALTER TABLE below.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE lotw_sync_jobs (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    status           TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','running','completed','failed','cancelled')),

    -- How many QSOs were bundled in this upload, and which ones.
    qso_count        INT NOT NULL,
    qso_ids          JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Size of the signed .tq8 blob sent to ARRL.
    tq8_size_bytes   INT,

    -- Raw HTML body returned by ARRL.
    arrl_response    TEXT,

    error_message    TEXT,

    retry_count      SMALLINT NOT NULL DEFAULT 0,
    max_retries      SMALLINT NOT NULL DEFAULT 3,
    next_retry_at    TIMESTAMPTZ,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ
);

CREATE INDEX idx_lotw_sync_jobs_user   ON lotw_sync_jobs(user_id, created_at DESC);
CREATE INDEX idx_lotw_sync_jobs_status ON lotw_sync_jobs(status)
    WHERE status IN ('pending','running');

ALTER TABLE lotw_sync_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY lotw_sync_jobs_isolation ON lotw_sync_jobs
    FOR ALL TO radioledger_api
    USING (user_id = app_current_user_id());
CREATE POLICY lotw_sync_jobs_worker_all ON lotw_sync_jobs
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT SELECT ON TABLE lotw_sync_jobs TO radioledger_api;
GRANT INSERT, UPDATE, DELETE, SELECT ON TABLE lotw_sync_jobs TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE lotw_sync_jobs_id_seq TO radioledger_api;
GRANT USAGE, SELECT ON SEQUENCE lotw_sync_jobs_id_seq TO radioledger_worker;

COMMENT ON TABLE lotw_sync_jobs IS
    'One row per LoTW upload attempt. Tracks QSOs included, ARRL response, and retry state.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: lotw_sync_status
-- Per-user LoTW preferences and aggregate sync state (cert present, last sync).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE lotw_sync_status (
    id                   BIGSERIAL PRIMARY KEY,
    user_id              BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    has_cert             BOOLEAN NOT NULL DEFAULT FALSE,
    auto_sync_prompt     BOOLEAN NOT NULL DEFAULT TRUE,

    last_sync_at         TIMESTAMPTZ,
    last_pull_at         TIMESTAMPTZ,
    last_sync_qso_count  INT,
    last_sync_result     TEXT
        CHECK (last_sync_result IS NULL OR last_sync_result IN ('accepted','rejected','error')),
    last_sync_error      TEXT,
    total_qsos_synced    INT NOT NULL DEFAULT 0,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id)
);

ALTER TABLE lotw_sync_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY lotw_sync_status_isolation ON lotw_sync_status
    FOR ALL TO radioledger_api
    USING (user_id = app_current_user_id());
CREATE POLICY lotw_sync_status_worker_all ON lotw_sync_status
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT SELECT, INSERT, UPDATE ON TABLE lotw_sync_status TO radioledger_api;
GRANT INSERT, UPDATE, DELETE, SELECT ON TABLE lotw_sync_status TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE lotw_sync_status_id_seq TO radioledger_api;
GRANT USAGE, SELECT ON SEQUENCE lotw_sync_status_id_seq TO radioledger_worker;

COMMENT ON TABLE lotw_sync_status IS
    'Per-user LoTW sync preferences and aggregate state. One row per user.';


-- ─────────────────────────────────────────────────────────────────────────────
-- ADD FK: qsos.lotw_sync_job_id → lotw_sync_jobs
-- Added here (after both tables exist) because qsos is defined earlier in the
-- schema and lotw_sync_jobs depends on users which precedes qsos.
-- ─────────────────────────────────────────────────────────────────────────────
ALTER TABLE qsos ADD COLUMN lotw_sync_job_id BIGINT REFERENCES lotw_sync_jobs(id);

COMMENT ON COLUMN qsos.lotw_sent_at     IS 'Timestamp when this QSO was last successfully submitted to ARRL LoTW.';
COMMENT ON COLUMN qsos.lotw_sync_job_id IS 'The lotw_sync_jobs row that last uploaded this QSO to LoTW.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: dxcc_entities
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE dxcc_entities (
    entity_id        INTEGER PRIMARY KEY,
    name             TEXT NOT NULL,
    lotw_entity_name TEXT,
    prefix           TEXT NOT NULL,
    continent        TEXT NOT NULL
        CHECK (continent IN ('NA','SA','EU','AF','AS','OC','AN')),
    cq_zone          SMALLINT CHECK (cq_zone BETWEEN 1 AND 40),
    itu_zone         SMALLINT CHECK (itu_zone BETWEEN 1 AND 90),
    latitude         NUMERIC,
    longitude        NUMERIC,
    location         GEOMETRY(Point, 4326),
    deleted          BOOLEAN NOT NULL DEFAULT FALSE,
    valid_from       DATE,
    valid_to         DATE
);

CREATE INDEX idx_dxcc_prefix   ON dxcc_entities(prefix);
CREATE INDEX idx_dxcc_location ON dxcc_entities USING GIST(location) WHERE location IS NOT NULL;

COMMENT ON TABLE  dxcc_entities IS 'ARRL DXCC entity list. Do not delete rows — deleted entities still valid for historical QSOs.';
COMMENT ON COLUMN dxcc_entities.lotw_entity_name IS 'LoTW sometimes uses different entity names than the DXCC standard list. Stored here for matching during LoTW sync confirmation.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: dxcc_prefixes (migration 013)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE dxcc_prefixes (
    prefix      TEXT PRIMARY KEY,
    entity_id   INTEGER NOT NULL REFERENCES dxcc_entities(entity_id) ON DELETE CASCADE,
    source      TEXT NOT NULL DEFAULT 'seed',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dxcc_prefixes_upper CHECK (prefix = UPPER(prefix))
);

CREATE INDEX idx_dxcc_prefixes_entity ON dxcc_prefixes(entity_id);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: callsign_records (migration 009)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE callsign_records (
    id              BIGSERIAL PRIMARY KEY,
    callsign        TEXT NOT NULL,
    source          TEXT NOT NULL,
    source_id       TEXT,

    first_name      TEXT,
    last_name       TEXT,
    full_name       TEXT,
    address_line1   TEXT,
    address_line2   TEXT,
    city            TEXT,
    state_province  TEXT,
    postal_code     TEXT,
    country         TEXT NOT NULL,

    license_class   TEXT,
    grant_date      DATE,
    expiry_date     DATE,
    status          TEXT NOT NULL DEFAULT 'active',

    grid_square     TEXT,
    latitude        DOUBLE PRECISION,
    longitude       DOUBLE PRECISION,
    dxcc_entity_id  INTEGER REFERENCES dxcc_entities(entity_id),

    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (callsign, source)
);

CREATE INDEX idx_callsign_records_call    ON callsign_records (callsign);
CREATE INDEX idx_callsign_records_name    ON callsign_records (last_name, first_name);
CREATE INDEX idx_callsign_records_grid    ON callsign_records (grid_square) WHERE grid_square IS NOT NULL;
CREATE INDEX idx_callsign_records_dxcc    ON callsign_records (dxcc_entity_id) WHERE dxcc_entity_id IS NOT NULL;
CREATE INDEX idx_callsign_records_country ON callsign_records (country);
CREATE INDEX idx_callsign_records_status  ON callsign_records (status);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: operator_profiles (migration 009)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE operator_profiles (
    id              BIGSERIAL PRIMARY KEY,
    callsign        TEXT NOT NULL UNIQUE,
    user_id         BIGINT REFERENCES users(id),

    display_name    TEXT,
    bio             TEXT,
    avatar_url      TEXT,
    website         TEXT,
    qrz_page        TEXT,

    station_description TEXT,
    antennas        TEXT[],
    rigs            TEXT[],
    grid_square     TEXT,

    qsl_via         TEXT,
    qsl_message     TEXT,

    twitter         TEXT,
    mastodon        TEXT,
    youtube         TEXT,

    total_qsos      INTEGER NOT NULL DEFAULT 0,
    unique_dxcc     INTEGER NOT NULL DEFAULT 0,
    unique_grids    INTEGER NOT NULL DEFAULT 0,
    member_since    TIMESTAMPTZ,
    last_active     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_operator_profiles_user ON operator_profiles (user_id) WHERE user_id IS NOT NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: callsign_sync_runs (migration 009)
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE callsign_sync_runs (
    id                  BIGSERIAL PRIMARY KEY,
    source              TEXT NOT NULL,
    run_type            TEXT NOT NULL,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at        TIMESTAMPTZ,
    records_processed   INTEGER NOT NULL DEFAULT 0,
    records_added       INTEGER NOT NULL DEFAULT 0,
    records_updated     INTEGER NOT NULL DEFAULT 0,
    records_removed     INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL DEFAULT 'running',
    error               TEXT
);

CREATE INDEX idx_callsign_sync_runs_source ON callsign_sync_runs (source, started_at DESC);
CREATE INDEX idx_callsign_sync_runs_status ON callsign_sync_runs (status);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: bands
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE bands (
    name        TEXT PRIMARY KEY,
    lower_freq  NUMERIC NOT NULL,
    upper_freq  NUMERIC NOT NULL,
    band_group  TEXT CHECK (band_group IN ('LF', 'MF', 'HF', 'VHF', 'UHF', 'SHF', 'microwave')),
    warc        BOOLEAN NOT NULL DEFAULT FALSE,
    is_common   BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order  INTEGER NOT NULL DEFAULT 999,

    CONSTRAINT chk_bands_freq CHECK (lower_freq < upper_freq AND lower_freq > 0)
);

COMMENT ON COLUMN bands.warc      IS 'TRUE for WARC bands (30m, 17m, 12m). Excluded from most major contest categories.';
COMMENT ON COLUMN bands.is_common IS 'TRUE for bands shown in default UI views. FALSE for obscure/experimental/country-specific bands.';
COMMENT ON COLUMN bands.sort_order IS 'Display sort order. Lower values appear first. HF bands 1–20, VHF/UHF 11–18, others 50+';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: modes
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE modes (
    name         TEXT PRIMARY KEY,
    category     TEXT CHECK (category IN ('PHONE', 'CW', 'DIGITAL', 'IMAGE', 'DATA')),
    adif_mode    TEXT,
    adif_submode TEXT,
    submodes     TEXT[],
    is_analog    BOOLEAN NOT NULL DEFAULT FALSE,
    is_popular   BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order   INTEGER NOT NULL DEFAULT 999
);

COMMENT ON TABLE  modes IS 'Mode definitions and ADIF canonical export mapping. adif_mode/adif_submode distinguish canonical export values from tolerated import or UI aliases.';
COMMENT ON COLUMN modes.is_analog  IS 'TRUE for analog modes (SSB, FM, AM, CW). FALSE for digital/image/data modes.';
COMMENT ON COLUMN modes.is_popular IS 'TRUE for modes shown in default/quick-pick UI lists. FALSE for less-common modes.';
COMMENT ON COLUMN modes.sort_order IS 'Display sort order. Lower values appear first in mode pickers.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: band_region_allocations
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE band_region_allocations (
    id                 BIGSERIAL PRIMARY KEY,
    itu_region         INTEGER NOT NULL CHECK (itu_region IN (1, 2, 3)),
    band_name          TEXT NOT NULL REFERENCES bands(name) ON DELETE CASCADE,
    lower_freq         NUMERIC(12,4) NOT NULL,
    upper_freq         NUMERIC(12,4) NOT NULL,
    is_default_visible BOOLEAN NOT NULL DEFAULT TRUE,
    notes              TEXT,
    UNIQUE (itu_region, band_name)
);

COMMENT ON TABLE  band_region_allocations IS 'Per-ITU-region band allocations with region-specific frequency edges. Used for region-aware band visibility defaults.';
COMMENT ON COLUMN band_region_allocations.itu_region IS '1=Europe/Africa/Middle East, 2=Americas, 3=Asia/Pacific.';
COMMENT ON COLUMN band_region_allocations.is_default_visible IS 'TRUE if the band is commonly used/allocated in that region and should appear in default UI band pickers.';

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE band_region_allocations TO radioledger_api;
GRANT SELECT                          ON TABLE band_region_allocations TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE band_region_allocations_id_seq TO radioledger_api;


-- ─────────────────────────────────────────────────────────────────────────────
-- FK ADDITIONS: enforce declared taxonomy in qsos, user_callsigns, station_locations
-- (Added after dxcc_entities, bands, modes are created.)
-- ─────────────────────────────────────────────────────────────────────────────
ALTER TABLE qsos
    ADD CONSTRAINT fk_qsos_band    FOREIGN KEY (band)    REFERENCES bands(name),
    ADD CONSTRAINT fk_qsos_mode    FOREIGN KEY (mode)    REFERENCES modes(name),
    ADD CONSTRAINT fk_qsos_dxcc    FOREIGN KEY (dxcc)    REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL,
    ADD CONSTRAINT fk_qsos_my_dxcc FOREIGN KEY (my_dxcc) REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;

ALTER TABLE user_callsigns
    ADD CONSTRAINT fk_user_callsigns_dxcc FOREIGN KEY (dxcc_entity)
        REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;

ALTER TABLE station_locations
    ADD CONSTRAINT fk_station_locations_dxcc FOREIGN KEY (dxcc_entity)
        REFERENCES dxcc_entities(entity_id) ON DELETE SET NULL;


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: pota_parks
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE pota_parks (
    park_ref        TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    country         TEXT NOT NULL,
    state_province  TEXT,
    latitude        NUMERIC,
    longitude       NUMERIC,
    location        GEOMETRY(Point, 4326),
    park_type       TEXT,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pota_parks_country  ON pota_parks(country) WHERE active = TRUE;
CREATE INDEX idx_pota_parks_location ON pota_parks USING GIST(location)
    WHERE location IS NOT NULL AND active = TRUE;


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: sota_summits
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sota_summits (
    summit_ref      TEXT PRIMARY KEY,
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


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: award_tracking
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE award_tracking (
    id           BIGSERIAL PRIMARY KEY,
    uuid         UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    logbook_id   BIGINT REFERENCES logbooks(id) ON DELETE SET NULL,
    award_type   TEXT NOT NULL
        CHECK (award_type IN (
            'dxcc', 'was', 'vucc', 'waz', 'wpx',
            'pota_hunter', 'pota_activator',
            'sota_chaser', 'sota_activator',
            'custom'
        )),
    award_config JSONB NOT NULL DEFAULT '{}'::jsonb,
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

COMMENT ON TABLE  award_tracking IS 'Award programs a user is actively pursuing.';
COMMENT ON COLUMN award_tracking.award_config IS 'JSON config for this award instance. DXCC example: {"band": "20m", "mode": "CW", "confirmed_only": true}.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: award_progress
-- Note: uses NULLS NOT DISTINCT unique index (migration 012) so NULL band/mode
-- participate correctly in ON CONFLICT detection (requires PostgreSQL 15+).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE award_progress (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    award_type          TEXT NOT NULL
        CHECK (award_type IN (
            'dxcc', 'was', 'vucc', 'waz', 'wpx',
            'pota_hunter', 'pota_activator',
            'sota_chaser', 'sota_activator'
        )),
    entity_key          TEXT NOT NULL,
    band                TEXT,
    mode                TEXT,
    first_qso_id        BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    confirmed           BOOLEAN NOT NULL DEFAULT FALSE,
    confirmation_method TEXT,
    confirmed_via       TEXT,
    confirmed_at        TIMESTAMPTZ,
    qso_count           BIGINT NOT NULL DEFAULT 0,
    last_qso_at         TIMESTAMPTZ,
    worked              BOOLEAN NOT NULL DEFAULT FALSE,
    dirty               BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- NULLS NOT DISTINCT: allows NULL band/mode to participate in conflict detection.
CREATE UNIQUE INDEX uq_award_progress_nulls_not_distinct
    ON award_progress (user_id, award_type, entity_key, band, mode)
    NULLS NOT DISTINCT;

-- Promote the unique index to a named constraint so ON CONFLICT ON CONSTRAINT works (PostgreSQL requires
-- an actual constraint, not just an index, for that syntax).
ALTER TABLE award_progress
    ADD CONSTRAINT uq_award_progress_nulls_not_distinct
    UNIQUE USING INDEX uq_award_progress_nulls_not_distinct;

CREATE INDEX idx_award_progress_user  ON award_progress(user_id, award_type);
CREATE INDEX idx_award_progress_dirty ON award_progress(user_id) WHERE dirty = TRUE;

ALTER TABLE award_progress ENABLE ROW LEVEL SECURITY;
CREATE POLICY award_progress_isolation ON award_progress
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());
CREATE POLICY award_progress_worker_all ON award_progress
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

COMMENT ON TABLE  award_progress IS 'Materialized award progress. dirty=TRUE triggers recalculation by AwardProgressRefreshJob.';
COMMENT ON COLUMN award_progress.dirty IS 'Set TRUE by AFTER trigger when a relevant QSO changes. Cleared to FALSE on successful recalculation.';
COMMENT ON COLUMN award_progress.confirmation_method IS 'HOW confirmation happened: lotw, eqsl, qsl_card, radioledger. Matters for award submission reporting.';
COMMENT ON COLUMN award_progress.qso_count IS 'Total QSOs contributing to this award entity (band/mode combo). Updated by AwardRefreshWorker.';
COMMENT ON COLUMN award_progress.last_qso_at IS 'Timestamp of the most recent QSO contributing to this progress record.';
COMMENT ON COLUMN award_progress.worked IS 'TRUE when at least one QSO has been logged for this entity_key/band/mode.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: import_jobs
-- ─────────────────────────────────────────────────────────────────────────────
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

COMMENT ON TABLE import_jobs IS 'Async ADIF import jobs with summary counters. Per-row failures stored in import_job_errors.';
COMMENT ON COLUMN import_jobs.timestamp_strategy IS 'UTC interpretation mode: trust_utc, interpret_local, detect_and_warn.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: import_job_errors
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_import_job_errors_job          ON import_job_errors(import_job_id, id);
CREATE INDEX idx_import_job_errors_job_severity ON import_job_errors(import_job_id, severity);
CREATE INDEX idx_import_job_errors_field        ON import_job_errors(import_job_id, adif_field)
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

COMMENT ON TABLE import_job_errors IS 'Per-record import errors/warnings. Avoids large JSONB arrays on bad imports.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: user_service_credentials
-- ─────────────────────────────────────────────────────────────────────────────
-- credentials is AES-256-GCM ciphertext ONLY. Plaintext never enters the database.
-- Format: 12-byte nonce || GCM ciphertext || 16-byte GCM tag
CREATE TABLE user_service_credentials (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    service          TEXT NOT NULL
        CHECK (service IN ('qrz', 'eqsl', 'clublog', 'hamqth', 'pota', 'lotw')),
    credential_type  TEXT NOT NULL
        CHECK (credential_type IN (
            'api_key', 'username_password', 'session', 'oauth_token'
        )),
    credentials      BYTEA NOT NULL,
    key_version      INTEGER NOT NULL DEFAULT 1,
    expires_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ,
    last_verified_at TIMESTAMPTZ,
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, service)
);

CREATE INDEX idx_user_service_creds_user ON user_service_credentials(user_id) WHERE is_active = TRUE;
-- Index for RotateKey batch queries (migration 011)
CREATE INDEX idx_usc_key_version ON user_service_credentials(key_version);

ALTER TABLE user_service_credentials ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_service_creds_isolation ON user_service_credentials
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE  user_service_credentials IS 'Encrypted external service credentials. credentials is AES-256-GCM ciphertext; plaintext never enters the database.';
COMMENT ON COLUMN user_service_credentials.key_version IS 'Master key derivation version. On rotation: increment app version, re-encrypt rows in background, update this field.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: api_keys
-- ─────────────────────────────────────────────────────────────────────────────
-- Generate → Show once → Store hash. Never store plaintext.
CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    uuid         UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    key_prefix   TEXT NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT '{}'::text[],
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

COMMENT ON TABLE  api_keys IS 'API keys for scripting and automation. key_hash = SHA-256 of full key (shown once at creation, never stored).';
COMMENT ON COLUMN api_keys.scopes IS 'Least-privilege API scopes. Examples: qsos:read, qsos:write, adif:import, sync:trigger.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: audit_log
-- ─────────────────────────────────────────────────────────────────────────────
-- QSO mutation audit trail. Partitioned by month.
-- On GDPR deletion: anonymize user_id and ip_address in place; do not delete rows.
CREATE TABLE audit_log (
    id          BIGSERIAL,
    user_id     BIGINT,
    table_name  TEXT NOT NULL,
    record_id   BIGINT NOT NULL,
    action      TEXT NOT NULL CHECK (action IN ('INSERT', 'UPDATE', 'DELETE')),
    old_values  JSONB,
    new_values  JSONB,
    changed_by  TEXT,
    ip_address  INET,
    request_id  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
) PARTITION BY RANGE (created_at);

CREATE INDEX idx_audit_log_record ON audit_log(table_name, record_id);
CREATE INDEX idx_audit_log_user   ON audit_log(user_id, created_at DESC) WHERE user_id IS NOT NULL;

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_log_read ON audit_log
    FOR SELECT TO radioledger_api
    USING (user_id = app_current_user_id());
-- Trigger runs as table owner but INSERT in API role's transaction context requires this policy.
CREATE POLICY audit_log_insert ON audit_log
    FOR INSERT TO radioledger_api
    WITH CHECK (TRUE);
CREATE POLICY audit_log_worker_insert ON audit_log
    FOR INSERT TO radioledger_worker
    WITH CHECK (TRUE);
CREATE POLICY audit_log_worker_read ON audit_log
    FOR SELECT TO radioledger_worker
    USING (TRUE);

COMMENT ON TABLE audit_log IS 'QSO mutation audit trail. Partitioned by month. GDPR deletion: SET user_id=NULL, ip_address=NULL (anonymize) rather than deleting rows.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: notifications
-- Type list expanded in migration 003 (import_failed, sync_complete, qsl_confirmed,
-- system_announcement added alongside legacy values).
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE notifications (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL CHECK (type IN (
                    'lotw_confirmation', 'new_dxcc', 'new_state', 'award_milestone',
                    'sync_error', 'import_complete', 'cert_expiry', 'new_grid',
                    'pota_activation_valid', 'pota_upload_reminder',
                    'import_failed', 'sync_complete', 'qsl_confirmed', 'system_announcement',
                    'spot_alert'
                )),
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    qso_id      BIGINT REFERENCES qsos(id) ON DELETE SET NULL,
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_notifications_uuid UNIQUE (uuid)
);

CREATE INDEX idx_notifications_user_unread ON notifications(user_id, created_at DESC) WHERE read_at IS NULL;
-- Feed query index for notification dropdown (migration 003)
CREATE INDEX idx_notifications_user_feed   ON notifications(user_id, read_at, created_at DESC);

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
CREATE POLICY notifications_isolation ON notifications
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());
-- Worker emits import/sync notifications (migration 003)
CREATE POLICY notifications_worker_insert ON notifications
    FOR INSERT TO radioledger_worker
    WITH CHECK (TRUE);
-- Worker needs to read notifications for duplicate-check in CertExpiryCheckJob (migration 013)
CREATE POLICY notifications_worker_select ON notifications
    FOR SELECT TO radioledger_worker
    USING (TRUE);

COMMENT ON COLUMN notifications.type IS 'Notification category. Product-facing types: import_complete, import_failed, sync_complete, qsl_confirmed, system_announcement, spot_alert. Legacy values retained for compatibility.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: spots
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE spots (
    id              BIGSERIAL PRIMARY KEY,
    source          TEXT NOT NULL CHECK (source IN ('pota', 'sota')),
    callsign        TEXT NOT NULL,
    reference       TEXT NOT NULL,
    frequency_khz   NUMERIC(10,3),
    band            TEXT,
    mode            TEXT,
    spotted_at      TIMESTAMPTZ NOT NULL,
    raw_payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_spots_identity UNIQUE (source, callsign, reference, spotted_at),
    CONSTRAINT chk_spots_callsign_upper CHECK (callsign = UPPER(callsign)),
    CONSTRAINT chk_spots_reference_upper CHECK (reference = UPPER(reference))
);

CREATE INDEX idx_spots_active ON spots(spotted_at DESC);
CREATE INDEX idx_spots_filter ON spots(source, band, mode, spotted_at DESC);
CREATE INDEX idx_spots_reference ON spots(source, reference, spotted_at DESC);

ALTER TABLE spots ENABLE ROW LEVEL SECURITY;
CREATE POLICY spots_api_select ON spots
    FOR SELECT TO radioledger_api
    USING (TRUE);
CREATE POLICY spots_worker_all ON spots
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

COMMENT ON TABLE spots IS 'Active POTA/SOTA spots pulled from upstream spot feeds. Old rows are pruned after 24h by background worker.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: spot_watch_rules
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE spot_watch_rules (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source              TEXT NOT NULL CHECK (source IN ('pota', 'sota')),
    reference           TEXT NOT NULL,
    mode                TEXT,
    band                TEXT,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    last_notified_at    TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_spot_watch_rules_uuid UNIQUE (uuid),
    CONSTRAINT chk_spot_watch_rules_reference_upper CHECK (reference = UPPER(reference))
);

CREATE INDEX idx_spot_watch_rules_user ON spot_watch_rules(user_id, created_at DESC);
CREATE INDEX idx_spot_watch_rules_match ON spot_watch_rules(source, reference) WHERE enabled = TRUE;
CREATE UNIQUE INDEX uq_spot_watch_rules_unique
    ON spot_watch_rules(user_id, source, reference, COALESCE(mode, ''), COALESCE(band, ''));

ALTER TABLE spot_watch_rules ENABLE ROW LEVEL SECURITY;
CREATE POLICY spot_watch_rules_isolation ON spot_watch_rules
    FOR ALL TO radioledger_api
    USING (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());
CREATE POLICY spot_watch_rules_worker_select ON spot_watch_rules
    FOR SELECT TO radioledger_worker
    USING (TRUE);
CREATE POLICY spot_watch_rules_worker_update ON spot_watch_rules
    FOR UPDATE TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

COMMENT ON TABLE spot_watch_rules IS 'Per-user spot alert watch list. Matching spots generate in-app notifications via SpotPoller worker.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: spot_notification_preferences
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE spot_notification_preferences (
    id                      BIGSERIAL PRIMARY KEY,
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,
    cooldown_minutes        INTEGER NOT NULL DEFAULT 30 CHECK (cooldown_minutes >= 0 AND cooldown_minutes <= 1440),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_spot_notification_preferences_user UNIQUE (user_id)
);

ALTER TABLE spot_notification_preferences ENABLE ROW LEVEL SECURITY;
CREATE POLICY spot_notification_preferences_isolation ON spot_notification_preferences
    FOR ALL TO radioledger_api
    USING (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());
CREATE POLICY spot_notification_preferences_worker_select ON spot_notification_preferences
    FOR SELECT TO radioledger_worker
    USING (TRUE);

COMMENT ON TABLE spot_notification_preferences IS 'Per-user spot alert settings. cooldown_minutes throttles repeated alerts for a watch rule.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: qsl_routes
-- ─────────────────────────────────────────────────────────────────────────────
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

COMMENT ON TABLE qsl_routes IS 'Paper QSL routing history. Captures manager and bureau changes over time.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: paper_qsl_batches
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_paper_qsl_batches_user   ON paper_qsl_batches(user_id, created_at DESC);
CREATE INDEX idx_paper_qsl_batches_status ON paper_qsl_batches(user_id, status);

ALTER TABLE paper_qsl_batches ENABLE ROW LEVEL SECURITY;
CREATE POLICY paper_qsl_batches_isolation ON paper_qsl_batches
    FOR ALL TO radioledger_api
    USING  (user_id = app_current_user_id())
    WITH CHECK (user_id = app_current_user_id());

COMMENT ON TABLE paper_qsl_batches IS 'Paper QSL lifecycle container. Tracks bureau/direct/manager batches from draft → printed → sent → received/closed.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: paper_qsl_batch_items
-- ─────────────────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_paper_qsl_batch_items_batch  ON paper_qsl_batch_items(batch_id);
CREATE INDEX idx_paper_qsl_batch_items_qso    ON paper_qsl_batch_items(qso_id);
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

COMMENT ON TABLE paper_qsl_batch_items IS 'Per-QSO paper QSL workflow state. Has scope enforcement trigger to prevent cross-tenant FK bypass.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: callsign_cache
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE callsign_cache (
    callsign    TEXT PRIMARY KEY,
    data        JSONB NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('qrz', 'hamdb', 'lotw', 'manual')),
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,

    CONSTRAINT chk_callsign_cache_upper CHECK (callsign = upper(callsign))
);

CREATE INDEX idx_callsign_cache_expiry ON callsign_cache(expires_at);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: sync_rate_limit_window  (migration 010)
-- Global rate limit tracking for sync worker services.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sync_rate_limit_window (
    service      TEXT NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    count        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (service, bucket_start)
);

CREATE INDEX idx_sync_rate_limit_window_service_time
    ON sync_rate_limit_window(service, bucket_start DESC);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: sync_circuit_state  (migration 010)
-- Circuit breaker persistence for sync worker services.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sync_circuit_state (
    service               TEXT PRIMARY KEY,
    state                 TEXT NOT NULL DEFAULT 'closed'
        CHECK (state IN ('closed', 'open', 'half_open')),
    consecutive_failures  INTEGER NOT NULL DEFAULT 0,
    opened_at             TIMESTAMPTZ,
    half_open_in_flight   BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error            TEXT
);


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: system_settings  (migration 011)
-- Server-level operational metadata. Not tenant-scoped.
-- Used for master key generation timestamps and similar self-hosted metadata.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE system_settings (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE system_settings IS 'Server-level operational metadata. Not tenant-scoped. Used for master key generation timestamps and similar self-hosted metadata.';


-- ─────────────────────────────────────────────────────────────────────────────
-- TABLE: sync_conflicts  (migration 014)
-- Field-level conflicts between sync services per QSO.
-- ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE sync_conflicts (
    id                  BIGSERIAL PRIMARY KEY,
    qso_id              BIGINT NOT NULL REFERENCES qsos(id) ON DELETE CASCADE,
    service_a           TEXT NOT NULL,
    service_b           TEXT NOT NULL,
    field_conflicts     JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'resolved')),
    resolution          JSONB,
    resolved_by_service TEXT,
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (service_a <> service_b)
);

CREATE INDEX idx_sync_conflicts_qso_status
    ON sync_conflicts(qso_id, status, created_at DESC);

ALTER TABLE sync_conflicts ENABLE ROW LEVEL SECURITY;
CREATE POLICY sync_conflicts_isolation ON sync_conflicts
    FOR ALL TO radioledger_api
    USING (qso_id IN (
        SELECT q.id FROM qsos q
        JOIN logbooks lb ON lb.id = q.logbook_id
        WHERE lb.user_id = app_current_user_id()
    ))
    WITH CHECK (qso_id IN (
        SELECT q.id FROM qsos q
        JOIN logbooks lb ON lb.id = q.logbook_id
        WHERE lb.user_id = app_current_user_id()
    ));
CREATE POLICY sync_conflicts_worker_read ON sync_conflicts
    FOR SELECT TO radioledger_worker USING (TRUE);


-- ─────────────────────────────────────────────────────────────────────────────
-- FUNCTIONS AND TRIGGERS
-- ─────────────────────────────────────────────────────────────────────────────

-- ── Maidenhead grid square → WGS-84 center point ─────────────────────────────
-- +goose StatementBegin
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
    lon := (ascii(substr(g, 1, 1)) - ascii('A')) * 20 - 180;
    lat := (ascii(substr(g, 2, 1)) - ascii('A')) * 10 - 90;
    lon := lon + (ascii(substr(g, 3, 1)) - ascii('0')) * 2;
    lat := lat + (ascii(substr(g, 4, 1)) - ascii('0')) * 1;
    IF length(g) >= 6 THEN
        lon := lon + (ascii(substr(g, 5, 1)) - ascii('A')) * (2.0 / 24);
        lat := lat + (ascii(substr(g, 6, 1)) - ascii('A')) * (1.0 / 24);
        lon := lon + (1.0 / 24);
        lat := lat + (0.5 / 24);
    ELSE
        lon := lon + 1.0;
        lat := lat + 0.5;
    END IF;
    RETURN ST_SetSRID(ST_MakePoint(lon, lat), 4326);
END;
$$ LANGUAGE plpgsql IMMUTABLE STRICT;
-- +goose StatementEnd

COMMENT ON FUNCTION maidenhead_to_point(TEXT) IS
    'Converts a Maidenhead grid locator (4 or 6 chars) to WGS-84 center point. IMMUTABLE: safe for index use.';


-- ── QSO insert/update trigger: compute PostGIS geometry and great-circle distance ──
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_qso_locations() RETURNS TRIGGER AS $$
BEGIN
    NEW.my_location    := maidenhead_to_point(NEW.my_gridsquare);
    NEW.their_location := maidenhead_to_point(NEW.gridsquare);
    IF NEW.my_location IS NOT NULL AND NEW.their_location IS NOT NULL THEN
        NEW.distance_km := ST_DistanceSphere(NEW.my_location, NEW.their_location) / 1000.0;
    ELSE
        NEW.distance_km := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_qso_locations
    BEFORE INSERT OR UPDATE OF my_gridsquare, gridsquare
    ON qsos
    FOR EACH ROW EXECUTE FUNCTION update_qso_locations();


-- ── User home station geometry from grid_square ───────────────────────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION update_user_location() RETURNS TRIGGER AS $$
BEGIN
    NEW.location := maidenhead_to_point(NEW.grid_square);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_user_location
    BEFORE INSERT OR UPDATE OF grid_square
    ON users
    FOR EACH ROW EXECUTE FUNCTION update_user_location();


-- ── QSO identity scope: operator_id, station_callsign_id, contest_session_id ──
-- must belong to the same tenant as the logbook.
-- +goose StatementBegin
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
-- +goose StatementEnd

CREATE TRIGGER trg_qso_identity_scope
    BEFORE INSERT OR UPDATE OF logbook_id, operator_id, station_callsign_id, contest_session_id
    ON qsos
    FOR EACH ROW EXECUTE FUNCTION enforce_qso_identity_scope();


-- ── QSO edit trigger: mark uploaded sync rows as dirty for re-sync ────────────
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION mark_sync_dirty_on_qso_edit() RETURNS TRIGGER AS $$
BEGIN
    IF (
        OLD.callsign IS DISTINCT FROM NEW.callsign
        OR OLD.band IS DISTINCT FROM NEW.band
        OR OLD.mode IS DISTINCT FROM NEW.mode
        OR OLD.submode IS DISTINCT FROM NEW.submode
        OR OLD.frequency_hz IS DISTINCT FROM NEW.frequency_hz
        OR OLD.datetime_on IS DISTINCT FROM NEW.datetime_on
        OR OLD.datetime_off IS DISTINCT FROM NEW.datetime_off
        OR OLD.rst_sent IS DISTINCT FROM NEW.rst_sent
        OR OLD.rst_rcvd IS DISTINCT FROM NEW.rst_rcvd
        OR OLD.tx_power IS DISTINCT FROM NEW.tx_power
        OR OLD.gridsquare IS DISTINCT FROM NEW.gridsquare
        OR OLD.my_gridsquare IS DISTINCT FROM NEW.my_gridsquare
        OR OLD.name IS DISTINCT FROM NEW.name
        OR OLD.station_callsign IS DISTINCT FROM NEW.station_callsign
        OR OLD.contest_id IS DISTINCT FROM NEW.contest_id
        OR OLD.srx IS DISTINCT FROM NEW.srx
        OR OLD.stx IS DISTINCT FROM NEW.stx
    ) THEN
        UPDATE sync_status
        SET status = 'dirty',
            updated_at = NOW()
        WHERE qso_id = NEW.id
          AND status IN ('uploaded', 'confirmed');
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_qso_edit_mark_dirty
    AFTER UPDATE ON qsos
    FOR EACH ROW
    EXECUTE FUNCTION mark_sync_dirty_on_qso_edit();


-- ── paper_qsl_batch_items: cross-tenant FK scope enforcement ─────────────────
-- SECURITY: PostgreSQL FK checks run as the table owner, not the current role.
-- This trigger prevents a malicious user from referencing another tenant's qso_id.
-- +goose StatementBegin
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
-- +goose StatementEnd

CREATE TRIGGER trg_paper_qsl_item_scope
    BEFORE INSERT OR UPDATE ON paper_qsl_batch_items
    FOR EACH ROW EXECUTE FUNCTION enforce_paper_qsl_item_scope();


-- ── ensure_logbook_owner_role: auto-assign owner role on logbook creation ─────
-- SECURITY DEFINER so it runs as the function owner (bypassing RLS) to avoid
-- the circular dependency: logbooks_select checks user_roles, but user_roles
-- doesn't exist yet during the INSERT that triggers this function. (migration 008)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ensure_logbook_owner_role()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    INSERT INTO user_roles (logbook_id, user_id, role, invited_by)
    VALUES (NEW.id, NEW.user_id, 'owner', NEW.user_id)
    ON CONFLICT (logbook_id, user_id)
    DO UPDATE SET role = 'owner', invited_by = EXCLUDED.invited_by, updated_at = NOW();

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER trg_logbooks_owner_role
    AFTER INSERT ON logbooks
    FOR EACH ROW
    EXECUTE FUNCTION ensure_logbook_owner_role();


-- ─────────────────────────────────────────────────────────────────────────────
-- GRANTS
-- ─────────────────────────────────────────────────────────────────────────────
GRANT USAGE ON SCHEMA public TO radioledger_api, radioledger_worker;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO radioledger_api;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO radioledger_api;

-- Worker needs broad read for matching, plus writes for sync/jobs/awards/spots.
GRANT SELECT ON ALL TABLES IN SCHEMA public TO radioledger_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO radioledger_worker;
GRANT INSERT, UPDATE ON TABLE sync_status TO radioledger_worker;
GRANT INSERT ON TABLE audit_log TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE spots TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE award_progress TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE callsign_records TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE callsign_cache TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE callsign_sync_runs TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE callsign_sync_runs_id_seq TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE notifications TO radioledger_worker;

-- Function grants for RBAC helpers (migration 004)
GRANT EXECUTE ON FUNCTION app_has_logbook_min_role(BIGINT, BIGINT, TEXT) TO radioledger_api, radioledger_worker;
GRANT EXECUTE ON FUNCTION app_role_rank(TEXT) TO radioledger_api, radioledger_worker;
GRANT EXECUTE ON FUNCTION find_qso_matches(TEXT, TEXT, TEXT, TEXT, TIMESTAMPTZ, INTERVAL, BIGINT) TO radioledger_api;




-- ═══════════════════════════════════════════════════════════════════════════════
-- REFERENCE DATA (folded from database/seeds/001_reference_data.sql)
-- Bands, modes, DXCC entities, DXCC prefixes, and ITU region allocations.
-- ═══════════════════════════════════════════════════════════════════════════════


-- ─────────────────────────────────────────────────────────────────────────────
-- BANDS
-- Frequencies are in MHz. Edges use widest allocation across all ITU regions.
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO bands (name, lower_freq, upper_freq, band_group, warc, is_common, sort_order) VALUES
('2190m', 0.1357, 0.1378, 'LF', FALSE, FALSE, 50),
('630m', 0.472, 0.479, 'MF', FALSE, FALSE, 51),
('560m', 0.501, 0.504, 'MF', FALSE, FALSE, 52),
('160m', 1.800, 2.000, 'HF', FALSE, TRUE, 1),
('80m', 3.500, 4.000, 'HF', FALSE, TRUE, 2),
('60m', 5.060, 5.450, 'HF', TRUE, TRUE, 3),
('40m', 7.000, 7.300, 'HF', FALSE, TRUE, 4),
('30m', 10.100, 10.150, 'HF', TRUE, TRUE, 5),
('20m', 14.000, 14.350, 'HF', FALSE, TRUE, 6),
('17m', 18.068, 18.168, 'HF', TRUE, TRUE, 7),
('15m', 21.000, 21.450, 'HF', FALSE, TRUE, 8),
('12m', 24.890, 24.990, 'HF', TRUE, TRUE, 9),
('10m', 28.000, 29.700, 'HF', FALSE, TRUE, 10),
('8m', 40.000, 45.000, 'VHF', FALSE, FALSE, 53),
('6m', 50.000, 54.000, 'VHF', FALSE, TRUE, 11),
('5m', 54.000, 69.900, 'VHF', FALSE, FALSE, 54),
('4m', 70.000, 71.000, 'VHF', FALSE, FALSE, 12),
('2m', 144.000, 148.000, 'VHF', FALSE, TRUE, 13),
('1.25m', 222.000, 225.000, 'VHF', FALSE, TRUE, 14),
('70cm', 420.000, 450.000, 'UHF', FALSE, TRUE, 15),
('33cm', 902.000, 928.000, 'UHF', FALSE, FALSE, 16),
('23cm', 1240.000, 1300.000, 'UHF', FALSE, TRUE, 17),
('13cm', 2300.000, 2450.000, 'SHF', FALSE, FALSE, 18),
('9cm', 3300.000, 3500.000, 'SHF', FALSE, FALSE, 55),
('6cm', 5650.000, 5925.000, 'SHF', FALSE, FALSE, 56),
('3cm', 10000.000, 10500.000, 'SHF', FALSE, FALSE, 57),
('1.25cm', 24000.000, 24050.000, 'microwave', FALSE, FALSE, 58),
('6mm', 47000.000, 47200.000, 'microwave', FALSE, FALSE, 59),
('4mm', 75500.000, 81000.000, 'microwave', FALSE, FALSE, 60),
('2.5mm', 119980.000, 120020.000, 'microwave', FALSE, FALSE, 61),
('2mm', 142000.000, 149000.000, 'microwave', FALSE, FALSE, 62),
('1mm', 241000.000, 250000.000, 'microwave', FALSE, FALSE, 63),
('submm', 300000.000, 7500000.000, 'microwave', FALSE, FALSE, 64)
ON CONFLICT (name) DO UPDATE SET
    lower_freq = EXCLUDED.lower_freq,
    upper_freq = EXCLUDED.upper_freq,
    band_group = EXCLUDED.band_group,
    warc = EXCLUDED.warc,
    is_common = EXCLUDED.is_common,
    sort_order = EXCLUDED.sort_order;


-- ─────────────────────────────────────────────────────────────────────────────
-- MODES
-- Operating modes with ADIF mapping, popularity, and analog/digital flags.
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO modes (name, category, adif_mode, adif_submode, submodes, is_analog, is_popular, sort_order) VALUES
('FT8',          'DIGITAL', 'FT8',          NULL,          NULL,                                                                 FALSE, TRUE,  1),
('SSB',          'PHONE',   'SSB',          NULL,          ARRAY['USB','LSB'],                                                   TRUE,  TRUE,  2),
('CW',           'CW',      'CW',           NULL,          NULL,                                                                 TRUE,  TRUE,  3),
('FT4',          'DIGITAL', 'MFSK',         'FT4',         NULL,                                                                 FALSE, TRUE,  4),
('FM',           'PHONE',   'FM',           NULL,          NULL,                                                                 TRUE,  TRUE,  5),
('RTTY',         'DIGITAL', 'RTTY',         NULL,          ARRAY['ASCI'],                                                       FALSE, TRUE,  6),
('PSK31',        'DIGITAL', 'PSK',          'PSK31',       NULL,                                                                 FALSE, TRUE,  7),
('DMR',          'DIGITAL', 'DIGITALVOICE', 'DMR',         NULL,                                                                 FALSE, TRUE,  8),
('VARAHF',       'DIGITAL', 'DYNAMIC',      'VARA HF',     NULL,                                                                 FALSE, TRUE,  9),
('JS8',          'DIGITAL', 'MFSK',         'JS8',         NULL,                                                                 FALSE, TRUE, 10),
('WSPR',         'DIGITAL', 'WSPR',         NULL,          NULL,                                                                 FALSE, TRUE, 11),
('C4FM',         'DIGITAL', 'DIGITALVOICE', 'C4FM',        NULL,                                                                 FALSE, TRUE, 12),
('DSTAR',        'DIGITAL', 'DIGITALVOICE', 'DSTAR',       NULL,                                                                 FALSE, TRUE, 13),
('SSTV',         'IMAGE',   'SSTV',         NULL,          NULL,                                                                 FALSE, TRUE, 14),
('PACKET',       'DATA',    'PKT',          NULL,          NULL,                                                                 FALSE, TRUE, 15),
('DIGITALVOICE', 'DIGITAL', 'DIGITALVOICE', NULL,          ARRAY['C4FM','DMR','DSTAR','FREEDV','M17'],                        FALSE, FALSE, 16),
('DYNAMIC',      'DATA',    'DYNAMIC',      NULL,          ARRAY['FREEDATA','VARA HF','VARA SATELLITE','VARA FM 1200','VARA FM 9600'], FALSE, FALSE, 17),
('FSK',          'DATA',    'FSK',          NULL,          ARRAY['SCAMP_FAST','SCAMP_SLOW','SCAMP_VSLOW'],                    FALSE, FALSE, 18),
('MTONE',        'DATA',    'MTONE',        NULL,          ARRAY['SCAMP_OO','SCAMP_OO_SLW'],                                  FALSE, FALSE, 19),
('OFDM',         'DATA',    'OFDM',         NULL,          ARRAY['RIBBIT_PIX','RIBBIT_SMS'],                                  FALSE, FALSE, 20),
('PKT',          'DATA',    'PKT',          NULL,          NULL,                                                                 FALSE, FALSE, 21),
('AM',           'PHONE',   'AM',           NULL,          NULL,                                                                 TRUE,  FALSE, 100),
('LSB',          'PHONE',   'SSB',          'LSB',         NULL,                                                                 TRUE,  FALSE, 101),
('USB',          'PHONE',   'SSB',          'USB',         NULL,                                                                 TRUE,  FALSE, 102),
('PSK63',        'DIGITAL', 'PSK',          'PSK63',       NULL,                                                                 FALSE, FALSE, 103),
('JT65',         'DIGITAL', 'JT65',         NULL,          ARRAY['JT65A','JT65B','JT65C'],                                    FALSE, FALSE, 104),
('JT9',          'DIGITAL', 'JT9',          NULL,          NULL,                                                                 FALSE, FALSE, 105),
('OLIVIA',       'DIGITAL', 'OLIVIA',       NULL,          NULL,                                                                 FALSE, FALSE, 106),
('THOR',         'DIGITAL', 'THOR',         NULL,          NULL,                                                                 FALSE, FALSE, 107),
('HELL',         'DIGITAL', 'HELL',         NULL,          ARRAY['FMHELL','HELL80','HFSK','PSKHELL'],                         FALSE, FALSE, 108),
('DOMINO',       'DIGITAL', 'DOMINO',       NULL,          ARRAY['DOMINOF'],                                                   FALSE, FALSE, 109),
('MFSK',         'DIGITAL', 'MFSK',         NULL,          ARRAY['FT4','FT2','JS8','Q65'],                                    FALSE, FALSE, 110),
('ARDOP',        'DIGITAL', 'ARDOP',        NULL,          NULL,                                                                 FALSE, FALSE, 111),
('FREEDV',       'DIGITAL', 'DIGITALVOICE', 'FREEDV',      NULL,                                                                 FALSE, FALSE, 112),
('M17',          'DIGITAL', 'DIGITALVOICE', 'M17',         NULL,                                                                 FALSE, FALSE, 113),
('Q65',          'DIGITAL', 'MFSK',         'Q65',         NULL,                                                                 FALSE, FALSE, 114),
('ATV',          'IMAGE',   'ATV',          NULL,          NULL,                                                                 FALSE, FALSE, 115),
('FAX',          'IMAGE',   'FAX',          NULL,          NULL,                                                                 FALSE, FALSE, 116),
('FT2',          'DIGITAL', 'MFSK',         'FT2',         NULL,                                                                 FALSE, FALSE, 117),
('FREEDATA',     'DATA',    'DYNAMIC',      'FREEDATA',    NULL,                                                                 FALSE, FALSE, 118),
('CHIP',         'DATA',    'CHIP',         NULL,          ARRAY['CHIP64','CHIP128'],                                          FALSE, FALSE, 119),
('CLO',          'DIGITAL', 'CLO',          NULL,          NULL,                                                                 FALSE, FALSE, 120),
('CONTESTI',     'DATA',    'CONTESTI',     NULL,          NULL,                                                                 FALSE, FALSE, 121),
('FSK441',       'DIGITAL', 'FSK441',       NULL,          NULL,                                                                 FALSE, FALSE, 122),
('ISCAT',        'DIGITAL', 'ISCAT',        NULL,          NULL,                                                                 FALSE, FALSE, 123),
('JT4',          'DIGITAL', 'JT4',          NULL,          ARRAY['JT4A','JT4B','JT4C','JT4D','JT4E','JT4F','JT4G'],         FALSE, FALSE, 124),
('JT44',         'DIGITAL', 'JT44',         NULL,          NULL,                                                                 FALSE, FALSE, 125),
('JT6M',         'DIGITAL', 'JT6M',         NULL,          NULL,                                                                 FALSE, FALSE, 126),
('MSK144',       'DIGITAL', 'MSK144',       NULL,          NULL,                                                                 FALSE, FALSE, 127),
('MT63',         'DIGITAL', 'MT63',         NULL,          NULL,                                                                 FALSE, FALSE, 128),
('OPERA',        'DIGITAL', 'OPERA',        NULL,          NULL,                                                                 FALSE, FALSE, 129),
('PAC',          'DATA',    'PAC',          NULL,          ARRAY['PAC2','PAC3'],                                               FALSE, FALSE, 130),
('PAX',          'DATA',    'PAX',          NULL,          ARRAY['PAX2'],                                                      FALSE, FALSE, 131),
('PSK',          'DIGITAL', 'PSK',          NULL,          ARRAY['PSK31','PSK63','FSK31','PSK10','PSK63F','PSK125','PSKAM10','PSKAM31','PSKAM50','PSKFEC31','QPSK31','QPSK63','QPSK125'], FALSE, FALSE, 132),
('PSK2K',        'DIGITAL', 'PSK2K',        NULL,          NULL,                                                                 FALSE, FALSE, 133),
('Q15',          'DIGITAL', 'Q15',          NULL,          NULL,                                                                 FALSE, FALSE, 134),
('QRA64',        'DIGITAL', 'QRA64',        NULL,          NULL,                                                                 FALSE, FALSE, 135),
('ROS',          'DIGITAL', 'ROS',          NULL,          NULL,                                                                 FALSE, FALSE, 136),
('RTTYM',        'DIGITAL', 'RTTYM',        NULL,          NULL,                                                                 FALSE, FALSE, 137),
('T10',          'DIGITAL', 'T10',          NULL,          NULL,                                                                 FALSE, FALSE, 138),
('THRB',         'DIGITAL', 'THRB',         NULL,          ARRAY['THRBX'],                                                     FALSE, FALSE, 139),
('TOR',          'DATA',    'TOR',          NULL,          ARRAY['AMTORFEC','GTOR'],                                          FALSE, FALSE, 140),
('V4',           'DIGITAL', 'V4',           NULL,          NULL,                                                                 FALSE, FALSE, 141),
('VOI',          'PHONE',   'VOI',          NULL,          NULL,                                                                 TRUE,  FALSE, 142),
('WINMOR',       'DATA',    'WINMOR',       NULL,          NULL,                                                                 FALSE, FALSE, 143)
ON CONFLICT (name) DO UPDATE SET
    category = EXCLUDED.category,
    adif_mode = EXCLUDED.adif_mode,
    adif_submode = EXCLUDED.adif_submode,
    submodes = EXCLUDED.submodes,
    is_analog = EXCLUDED.is_analog,
    is_popular = EXCLUDED.is_popular,
    sort_order = EXCLUDED.sort_order;

-- ─────────────────────────────────────────────────────────────────────────────
-- DXCC ENTITIES
-- Source: k0swe/dxcc-json (dxcc-2020-02.csv), current entities.
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO dxcc_entities (entity_id, name, lotw_entity_name, prefix, continent, cq_zone, itu_zone, deleted, valid_from, valid_to)
VALUES
    (1,   'Canada',                                   'Canada',                                   'VA',   'NA', 1,  2,  FALSE, NULL,         NULL),
    (3,   'Afghanistan',                              'Afghanistan',                              'YA',   'AS', 21, 40, FALSE, NULL,         NULL),
    (4,   'Agaléga and Saint Brandon',               'Agaléga and Saint Brandon',               '3B6',  'AF', 39, 53, FALSE, NULL,         NULL),
    (5,   'Åland Islands',                            'Åland Islands',                            'OH0',  'EU', 15, 18, FALSE, NULL,         NULL),
    (6,   'Alaska',                                   'Alaska',                                   'KL',   'NA', 1,  1,  FALSE, NULL,         NULL),
    (7,   'Albania',                                  'Albania',                                  'ZA',   'EU', 15, 28, FALSE, NULL,         NULL),
    (9,   'American Samoa',                           'American Samoa',                           'KH8',  'OC', 32, 62, FALSE, NULL,         NULL),
    (10,  'Amsterdam and Saint-Paul Islands',         'Amsterdam and Saint-Paul Islands',         'FT/Z', 'AF', 39, 68, FALSE, NULL,         NULL),
    (11,  'Andaman and Nicobar Islands',              'Andaman and Nicobar Islands',              'VU4',  'AS', 26, 49, FALSE, NULL,         NULL),
    (12,  'Anguilla',                                 'Anguilla',                                 'VP2E', 'NA', 8,  11, FALSE, NULL,         NULL),
    (13,  'Antarctica',                               'Antarctica',                               'CE9',  'AN', 12, 67, FALSE, NULL,         NULL),
    (14,  'Armenia',                                  'Armenia',                                  'EK',   'AS', 21, 29, FALSE, NULL,         NULL),
    (15,  'Asiatic Russia',                           'Asiatic Russia',                           'UA0',  'AS', 16, 20, FALSE, NULL,         NULL),
    (16,  'New Zealand Subantarctic Islands',         'New Zealand Subantarctic Islands',         'ZL9',  'OC', 32, 60, FALSE, NULL,         NULL),
    (17,  'Isla de Aves',                             'Isla de Aves',                             'YV0',  'NA', 8,  11, FALSE, NULL,         NULL),
    (18,  'Azerbaijan',                               'Azerbaijan',                               '4J',   'AS', 21, 29, FALSE, NULL,         NULL),
    (20,  'Howland and Baker Islands',                'Howland and Baker Islands',                'KH1',  'OC', 31, 61, FALSE, NULL,         NULL),
    (21,  'Balearic Islands',                         'Balearic Islands',                         'EA6',  'EU', 14, 37, FALSE, NULL,         NULL),
    (22,  'Palau',                                    'Palau',                                    'T8',   'OC', 27, 64, FALSE, '1994-01-01', NULL),
    (24,  'Bouvet Island',                            'Bouvet Island',                            '3Y',   'AF', 38, 67, FALSE, NULL,         NULL),
    (27,  'Belarus',                                  'Belarus',                                  'EU',   'EU', 16, 29, FALSE, NULL,         NULL),
    (29,  'Canary Islands',                           'Canary Islands',                           'EA8',  'AF', 33, 36, FALSE, NULL,         NULL),
    (31,  'Phoenix Islands',                          'Phoenix Islands',                          'T31',  'OC', 31, 62, FALSE, NULL,         NULL),
    (32,  'Ceuta and Melilla',                        'Ceuta and Melilla',                        'EA9',  'AF', 33, 37, FALSE, NULL,         NULL),
    (33,  'Chagos Islands',                           'Chagos Islands',                           'VQ9',  'AF', 39, 41, FALSE, NULL,         NULL),
    (34,  'Chatham Islands',                          'Chatham Islands',                          'ZL7',  'OC', 32, 60, FALSE, NULL,         NULL),
    (35,  'Christmas Island',                         'Christmas Island',                         'VK9X', 'OC', 29, 54, FALSE, NULL,         NULL),
    (36,  'Clipperton Island',                        'Clipperton Island',                        'FO',   'NA', 7,  10, FALSE, NULL,         NULL),
    (37,  'Cocos Island',                             'Cocos Island',                             'TI9',  'NA', 7,  12, FALSE, NULL,         NULL),
    (38,  'Cocos (Keeling) Islands',                  'Cocos (Keeling) Islands',                  'VK9C', 'OC', 29, 54, FALSE, NULL,         NULL),
    (40,  'Crete',                                    'Crete',                                    'SV9',  'EU', 20, 28, FALSE, NULL,         NULL),
    (41,  'Crozet Islands',                           'Crozet Islands',                           'FT/W', 'AF', 39, 68, FALSE, NULL,         NULL),
    (43,  'Desecheo Island',                          'Desecheo Island',                          'KP5',  'NA', 8,  11, FALSE, '1979-03-01', NULL),
    (45,  'Dodecanese',                               'Dodecanese',                               'SV5',  'EU', 20, 28, FALSE, NULL,         NULL),
    (46,  'East Malaysia',                            'East Malaysia',                            '9M6',  'OC', 28, 54, FALSE, '1963-09-16', NULL),
    (47,  'Easter Island',                            'Easter Island',                            'CE0',  'SA', 12, 63, FALSE, NULL,         NULL),
    (48,  'Line Islands',                             'Line Islands',                             'T32',  'OC', 31, 61, FALSE, NULL,         NULL),
    (49,  'Equatorial Guinea',                        'Equatorial Guinea',                        '3C',   'AF', 36, 47, FALSE, NULL,         NULL),
    (50,  'Mexico',                                   'Mexico',                                   'XA',   'NA', 6,  10, FALSE, NULL,         NULL),
    (51,  'Eritrea',                                  'Eritrea',                                  'E3',   'AF', 37, 48, FALSE, NULL,         NULL),
    (52,  'Estonia',                                  'Estonia',                                  'ES',   'EU', 15, 29, FALSE, NULL,         NULL),
    (53,  'Ethiopia',                                 'Ethiopia',                                 'ET',   'AF', 37, 48, FALSE, NULL,         NULL),
    (54,  'European Russia',                          'European Russia',                          'UA1',  'EU', 16, 19, FALSE, NULL,         NULL),
    (56,  'Fernando de Noronha',                      'Fernando de Noronha',                      'PP0F', 'SA', 11, 13, FALSE, NULL,         NULL),
    (60,  'Bahamas',                                  'Bahamas',                                  'C6',   'NA', 8,  11, FALSE, NULL,         NULL),
    (61,  'Franz Josef Land',                         'Franz Josef Land',                         'R1/F', 'EU', 40, 75, FALSE, NULL,         NULL),
    (62,  'Barbados',                                 'Barbados',                                 '8P',   'NA', 8,  11, FALSE, NULL,         NULL),
    (63,  'French Guiana',                            'French Guiana',                            'FY',   'SA', 9,  12, FALSE, NULL,         NULL),
    (64,  'Bermuda',                                  'Bermuda',                                  'VP9',  'NA', 5,  11, FALSE, NULL,         NULL),
    (65,  'British Virgin Is.',                       'British Virgin Is.',                       'VP2V', 'NA', 8,  11, FALSE, NULL,         NULL),
    (66,  'Belize',                                   'Belize',                                   'V3',   'NA', 7,  11, FALSE, NULL,         NULL),
    (69,  'Cayman Islands',                           'Cayman Islands',                           'ZF',   'NA', 8,  11, FALSE, NULL,         NULL),
    (70,  'Cuba',                                     'Cuba',                                     'CM',   'NA', 8,  11, FALSE, NULL,         NULL),
    (71,  'Galápagos Islands',                        'Galápagos Islands',                        'HC8',  'SA', 10, 12, FALSE, NULL,         NULL),
    (72,  'Dominican Republic',                       'Dominican Republic',                       'HI',   'NA', 8,  11, FALSE, NULL,         NULL),
    (74,  'El Salvador',                              'El Salvador',                              'YS',   'NA', 7,  11, FALSE, NULL,         NULL),
    (75,  'Georgia',                                  'Georgia',                                  '4L',   'AS', 21, 29, FALSE, NULL,         NULL),
    (76,  'Guatemala',                                'Guatemala',                                'TG',   'NA', 7,  12, FALSE, NULL,         NULL),
    (77,  'Grenada',                                  'Grenada',                                  'J3',   'NA', 8,  11, FALSE, NULL,         NULL),
    (78,  'Haiti',                                    'Haiti',                                    'HH',   'NA', 8,  11, FALSE, NULL,         NULL),
    (79,  'Guadeloupe',                               'Guadeloupe',                               'FG',   'NA', 8,  11, FALSE, NULL,         NULL),
    (80,  'Honduras',                                 'Honduras',                                 'HQ',   'NA', 7,  11, FALSE, NULL,         NULL),
    (82,  'Jamaica',                                  'Jamaica',                                  '6Y',   'NA', 8,  11, FALSE, NULL,         NULL),
    (84,  'Martinique',                               'Martinique',                               'FM',   'NA', 8,  11, FALSE, NULL,         NULL),
    (86,  'Nicaragua',                                'Nicaragua',                                'YN',   'NA', 7,  11, FALSE, NULL,         NULL),
    (88,  'Panama',                                   'Panama',                                   'HO',   'NA', 7,  11, FALSE, NULL,         NULL),
    (89,  'Turks and Caicos Islands',                 'Turks and Caicos Islands',                 'VP5',  'NA', 8,  11, FALSE, NULL,         NULL),
    (90,  'Trinidad and Tobago',                      'Trinidad and Tobago',                      '9Y',   'SA', 9,  11, FALSE, NULL,         NULL),
    (91,  'Aruba',                                    'Aruba',                                    'P4',   'SA', 9,  11, FALSE, '1986-01-01', NULL),
    (94,  'Antigua and Barbuda',                      'Antigua and Barbuda',                      'V2',   'NA', 8,  11, FALSE, NULL,         NULL),
    (95,  'Dominica',                                 'Dominica',                                 'J7',   'NA', 8,  11, FALSE, NULL,         NULL),
    (96,  'Montserrat',                               'Montserrat',                               'VP2M', 'NA', 8,  11, FALSE, NULL,         NULL),
    (97,  'Saint Lucia',                              'Saint Lucia',                              'J6',   'NA', 8,  11, FALSE, NULL,         NULL),
    (98,  'Saint Vincent and the Grenadines',         'Saint Vincent and the Grenadines',         'J8',   'NA', 8,  11, FALSE, NULL,         NULL),
    (99,  'Glorioso Islands',                         'Glorioso Islands',                         'FT/G', 'AF', 39, 53, FALSE, '1960-06-25', NULL),
    (100, 'Argentina',                                'Argentina',                                'LO',   'SA', 13, 14, FALSE, NULL,         NULL),
    (103, 'Guam',                                     'Guam',                                     'KH2',  'OC', 27, 64, FALSE, NULL,         NULL),
    (104, 'Bolivia',                                  'Bolivia',                                  'CP',   'SA', 10, 12, FALSE, NULL,         NULL),
    (105, 'Guantanamo Bay',                           'Guantanamo Bay',                           'KG4',  'NA', 8,  11, FALSE, NULL,         NULL),
    (106, 'Guernsey',                                 'Guernsey',                                 'GU',   'EU', 14, 27, FALSE, NULL,         NULL),
    (107, 'Guinea',                                   'Guinea',                                   '3X',   'AF', 35, 46, FALSE, NULL,         NULL),
    (108, 'Brazil',                                   'Brazil',                                   'PP',   'SA', 11, 12, FALSE, NULL,         NULL),
    (109, 'Guinea-Bissau',                            'Guinea-Bissau',                            'J5',   'AF', 35, 46, FALSE, NULL,         NULL),
    (110, 'Hawaii',                                   'Hawaii',                                   'KH6',  'OC', 31, 61, FALSE, NULL,         NULL),
    (111, 'Heard Island and McDonald Islands',        'Heard Island and McDonald Islands',        'VK0',  'AF', 39, 68, FALSE, NULL,         NULL),
    (112, 'Chile',                                    'Chile',                                    'CA',   'SA', 12, 14, FALSE, NULL,         NULL),
    (114, 'Isle of Man',                              'Isle of Man',                              'GD',   'EU', 14, 27, FALSE, NULL,         NULL),
    (116, 'Colombia',                                 'Colombia',                                 'HJ',   'SA', 9,  12, FALSE, NULL,         NULL),
    (117, 'International Telecommunication Union Headquarters', 'International Telecommunication Union Headquarters', '4U', 'EU', 14, 28, FALSE, NULL, NULL),
    (118, 'Jan Mayen',                                'Jan Mayen',                                'JX',   'EU', 40, 18, FALSE, NULL,         NULL),
    (120, 'Ecuador',                                  'Ecuador',                                  'HC',   'SA', 10, 12, FALSE, NULL,         NULL),
    (122, 'Jersey',                                   'Jersey',                                   'GJ',   'EU', 14, 27, FALSE, NULL,         NULL),
    (123, 'Johnston Atoll',                           'Johnston Atoll',                           'KH3',  'OC', 31, 61, FALSE, NULL,         NULL),
    (124, 'Juan de Nova and Europa Islands',          'Juan de Nova and Europa Islands',          'FT/J', 'AF', 39, 53, FALSE, '1960-06-25', NULL),
    (125, 'Juan Fernández Islands',                   'Juan Fernández Islands',                   'CE0',  'SA', 12, 14, FALSE, NULL,         NULL),
    (126, 'Kaliningrad',                              'Kaliningrad',                              'UA2',  'EU', 15, 29, FALSE, NULL,         NULL),
    (129, 'Guyana',                                   'Guyana',                                   '8R',   'SA', 9,  12, FALSE, NULL,         NULL),
    (130, 'Kazakhstan',                               'Kazakhstan',                               'UN',   'AS', 17, 29, FALSE, NULL,         NULL),
    (131, 'Kerguelen Islands',                        'Kerguelen Islands',                        'FT/X', 'AF', 39, 68, FALSE, NULL,         NULL),
    (132, 'Paraguay',                                 'Paraguay',                                 'ZP',   'SA', 11, 14, FALSE, NULL,         NULL),
    (133, 'Kermadec Islands',                         'Kermadec Islands',                         'ZL8',  'OC', 32, 60, FALSE, NULL,         NULL),
    (135, 'Kyrgyzstan',                               'Kyrgyzstan',                               'EX',   'AS', 17, 30, FALSE, NULL,         NULL),
    (136, 'Peru',                                     'Peru',                                     'OA',   'SA', 10, 12, FALSE, NULL,         NULL),
    (137, 'South Korea',                              'South Korea',                              'HL',   'AS', 25, 44, FALSE, NULL,         NULL),
    (138, 'Kure Atoll',                               'Kure Atoll',                               'KH7K', 'OC', 31, 61, FALSE, NULL,         NULL),
    (140, 'Suriname',                                 'Suriname',                                 'PZ',   'SA', 9,  12, FALSE, NULL,         NULL),
    (141, 'Falkland Islands',                         'Falkland Islands',                         'VP8',  'SA', 13, 16, FALSE, NULL,         NULL),
    (142, 'Lakshadweep',                              'Lakshadweep',                              'VU7',  'AS', 22, 41, FALSE, NULL,         NULL),
    (143, 'Laos',                                     'Laos',                                     'XW',   'AS', 26, 49, FALSE, NULL,         NULL),
    (144, 'Uruguay',                                  'Uruguay',                                  'CV',   'SA', 13, 14, FALSE, NULL,         NULL),
    (145, 'Latvia',                                   'Latvia',                                   'YL',   'EU', 15, 29, FALSE, NULL,         NULL),
    (146, 'Lithuania',                                'Lithuania',                                'LY',   'EU', 15, 29, FALSE, NULL,         NULL),
    (147, 'Lord Howe Island',                         'Lord Howe Island',                         'VK9L', 'OC', 30, 60, FALSE, NULL,         NULL),
    (148, 'Venezuela',                                'Venezuela',                                'YV',   'SA', 9,  12, FALSE, NULL,         NULL),
    (149, 'Azores',                                   'Azores',                                   'CU',   'EU', 14, 36, FALSE, NULL,         NULL),
    (150, 'Australia',                                'Australia',                                'VK',   'OC', 29, 55, FALSE, NULL,         NULL),
    (152, 'Macao',                                    'Macao',                                    'XX9',  'AS', 24, 44, FALSE, NULL,         NULL),
    (153, 'Macquarie Island',                         'Macquarie Island',                         'VK0',  'OC', 30, 60, FALSE, NULL,         NULL),
    (157, 'Nauru',                                    'Nauru',                                    'C2',   'OC', 31, 65, FALSE, NULL,         NULL),
    (158, 'Vanuatu',                                  'Vanuatu',                                  'YJ',   'OC', 32, 56, FALSE, NULL,         NULL),
    (159, 'Maldives',                                 'Maldives',                                 '8Q',   'AS', 22, 41, FALSE, NULL,         NULL),
    (160, 'Tonga',                                    'Tonga',                                    'A3',   'OC', 32, 62, FALSE, NULL,         NULL),
    (161, 'Malpelo Island',                           'Malpelo Island',                           'HK0',  'SA', 9,  12, FALSE, NULL,         NULL),
    (162, 'New Caledonia',                            'New Caledonia',                            'FK',   'OC', 32, 56, FALSE, NULL,         NULL),
    (163, 'Papua New Guinea',                         'Papua New Guinea',                         'P2',   'OC', 28, 51, FALSE, '1975-09-16', NULL),
    (165, 'Mauritius',                                'Mauritius',                                '3B8',  'AF', 39, 53, FALSE, NULL,         NULL),
    (166, 'Mariana Islands',                          'Mariana Islands',                          'KH0',  'OC', 27, 64, FALSE, NULL,         NULL),
    (167, 'Märket Island',                            'Märket Island',                            'OJ0',  'EU', 15, 18, FALSE, NULL,         NULL),
    (168, 'Marshall Islands',                         'Marshall Islands',                         'V7',   'OC', 31, 65, FALSE, NULL,         NULL),
    (169, 'Mayotte',                                  'Mayotte',                                  'FH',   'AF', 39, 53, FALSE, '1975-07-06', NULL),
    (170, 'New Zealand',                              'New Zealand',                              'ZL',   'OC', 32, 60, FALSE, NULL,         NULL),
    (171, 'Mellish Reef',                             'Mellish Reef',                             'VK9M', 'OC', 30, 56, FALSE, NULL,         NULL),
    (172, 'Pitcairn Islands',                         'Pitcairn Islands',                         'VP6',  'OC', 32, 63, FALSE, NULL,         NULL),
    (173, 'Micronesia',                               'Micronesia',                               'V6',   'OC', 27, 65, FALSE, NULL,         NULL),
    (174, 'Midway Atoll',                             'Midway Atoll',                             'KH4',  'OC', 31, 61, FALSE, NULL,         NULL),
    (175, 'French Polynesia',                         'French Polynesia',                         'FO',   'OC', 32, 63, FALSE, NULL,         NULL),
    (176, 'Fiji',                                     'Fiji',                                     '3D2',  'OC', 32, 56, FALSE, NULL,         NULL),
    (177, 'Minami-Tori-shima',                        'Minami-Tori-shima',                        'JD1',  'OC', 27, 90, FALSE, NULL,         NULL),
    (179, 'Moldova',                                  'Moldova',                                  'ER',   'EU', 16, 29, FALSE, NULL,         NULL),
    (180, 'Mount Athos',                              'Mount Athos',                              'SV/A', 'EU', 20, 28, FALSE, NULL,         NULL),
    (181, 'Mozambique',                               'Mozambique',                               'C8',   'AF', 37, 53, FALSE, NULL,         NULL),
    (182, 'Navassa Island',                           'Navassa Island',                           'KP1',  'NA', 8,  11, FALSE, NULL,         NULL),
    (185, 'Solomon Islands',                          'Solomon Islands',                          'H4',   'OC', 28, 51, FALSE, NULL,         NULL),
    (187, 'Niger',                                    'Niger',                                    '5U',   'AF', 35, 46, FALSE, '1960-08-03', NULL),
    (188, 'Niue',                                     'Niue',                                     'E6',   'OC', 32, 62, FALSE, NULL,         NULL),
    (189, 'Norfolk Island',                           'Norfolk Island',                           'VK9N', 'OC', 32, 60, FALSE, NULL,         NULL),
    (190, 'Samoa',                                    'Samoa',                                    '5W',   'OC', 32, 62, FALSE, NULL,         NULL),
    (191, 'North Cook Islands',                       'North Cook Islands',                       'E5',   'OC', 32, 62, FALSE, NULL,         NULL),
    (192, 'Ogasawara Islands',                        'Ogasawara Islands',                        'JD1',  'AS', 27, 45, FALSE, NULL,         NULL),
    (195, 'Annobón',                                  'Annobón',                                  '3C0',  'AF', 36, 52, FALSE, NULL,         NULL),
    (197, 'Palmyra and Jarvis Islands',               'Palmyra and Jarvis Islands',               'KH5',  'OC', 31, 61, FALSE, NULL,         NULL),
    (199, 'Peter I Island',                           'Peter I Island',                           '3Y',   'AN', 12, 72, FALSE, NULL,         NULL),
    (201, 'Prince Edward and Marion Islands',         'Prince Edward and Marion Islands',         'ZS8',  'AF', 38, 57, FALSE, NULL,         NULL),
    (202, 'Puerto Rico',                              'Puerto Rico',                              'KP3',  'NA', 8,  11, FALSE, NULL,         NULL),
    (203, 'Andorra',                                  'Andorra',                                  'C3',   'EU', 14, 27, FALSE, NULL,         NULL),
    (204, 'Revillagigedo Islands',                    'Revillagigedo Islands',                    'XA4',  'NA', 6,  10, FALSE, NULL,         NULL),
    (205, 'Ascension Island',                         'Ascension Island',                         'ZD8',  'AF', 36, 66, FALSE, NULL,         NULL),
    (206, 'Austria',                                  'Austria',                                  'OE',   'EU', 15, 28, FALSE, NULL,         NULL),
    (207, 'Rodrigues Island',                         'Rodrigues Island',                         '3B9',  'AF', 39, 53, FALSE, NULL,         NULL),
    (209, 'Belgium',                                  'Belgium',                                  'ON',   'EU', 14, 27, FALSE, NULL,         NULL),
    (211, 'Sable Island',                             'Sable Island',                             'CY0',  'NA', 5,  9,  FALSE, NULL,         NULL),
    (212, 'Bulgaria',                                 'Bulgaria',                                 'LZ',   'EU', 20, 28, FALSE, NULL,         NULL),
    (213, 'Saint Martin',                             'Saint Martin',                             'FS',   'NA', 8,  11, FALSE, NULL,         NULL),
    (214, 'Corsica',                                  'Corsica',                                  'TK',   'EU', 15, 28, FALSE, NULL,         NULL),
    (215, 'Cyprus',                                   'Cyprus',                                   '5B',   'AS', 20, 39, FALSE, NULL,         NULL),
    (216, 'San Andrés and Providencia',               'San Andrés and Providencia',               'HK0',  'NA', 7,  11, FALSE, NULL,         NULL),
    (217, 'Desventuradas Islands',                    'Desventuradas Islands',                    'CE0',  'SA', 12, 14, FALSE, NULL,         NULL),
    (219, 'Sao Tome and Principe',                    'Sao Tome and Principe',                    'S9',   'AF', 36, 47, FALSE, NULL,         NULL),
    (221, 'Denmark',                                  'Denmark',                                  'OU',   'EU', 14, 18, FALSE, NULL,         NULL),
    (222, 'Faroe Islands',                            'Faroe Islands',                            'OY',   'EU', 14, 18, FALSE, NULL,         NULL),
    (223, 'England',                                  'England',                                  'G',    'EU', 14, 27, FALSE, NULL,         NULL),
    (224, 'Finland',                                  'Finland',                                  'OF',   'EU', 15, 18, FALSE, NULL,         NULL),
    (225, 'Sardinia',                                 'Sardinia',                                 'IS0',  'EU', 15, 28, FALSE, NULL,         NULL),
    (227, 'France',                                   'France',                                   'F',    'EU', 14, 27, FALSE, NULL,         NULL),
    (230, 'Germany',                                  'Germany',                                  'DA',   'EU', 14, 28, FALSE, NULL,         NULL),
    (232, 'Somalia',                                  'Somalia',                                  'T5',   'AF', 37, 48, FALSE, NULL,         NULL),
    (233, 'Gibraltar',                                'Gibraltar',                                'ZB2',  'EU', 14, 37, FALSE, NULL,         NULL),
    (234, 'South Cook Islands',                       'South Cook Islands',                       'E5',   'OC', 32, 62, FALSE, NULL,         NULL),
    (235, 'South Georgia Island',                     'South Georgia Island',                     'VP8',  'SA', 13, 73, FALSE, NULL,         NULL),
    (236, 'Greece',                                   'Greece',                                   'SV',   'EU', 20, 28, FALSE, NULL,         NULL),
    (237, 'Greenland',                                'Greenland',                                'OX',   'NA', 40, 5,  FALSE, NULL,         NULL),
    (238, 'South Orkney Islands',                     'South Orkney Islands',                     'VP8',  'SA', 13, 73, FALSE, NULL,         NULL),
    (239, 'Hungary',                                  'Hungary',                                  'HA',   'EU', 15, 28, FALSE, NULL,         NULL),
    (240, 'South Sandwich Islands',                   'South Sandwich Islands',                   'VP8',  'SA', 13, 73, FALSE, NULL,         NULL),
    (241, 'South Shetland Islands',                   'South Shetland Islands',                   'VP8',  'SA', 13, 73, FALSE, NULL,         NULL),
    (242, 'Iceland',                                  'Iceland',                                  'TF',   'EU', 40, 17, FALSE, NULL,         NULL),
    (245, 'Ireland',                                  'Ireland',                                  'EI',   'EU', 14, 27, FALSE, NULL,         NULL),
    (246, 'Sovereign Military Order of Malta',        'Sovereign Military Order of Malta',        '1A',   'EU', 15, 28, FALSE, NULL,         NULL),
    (247, 'Spratly Islands',                          'Spratly Islands',                          '',     'AS', 26, 50, FALSE, NULL,         NULL),
    (248, 'Italy',                                    'Italy',                                    'I',    'EU', 15, 28, FALSE, NULL,         NULL),
    (249, 'Saint Kitts and Nevis',                    'Saint Kitts and Nevis',                    'V4',   'NA', 8,  11, FALSE, NULL,         NULL),
    (250, 'St. Helena',                               'St. Helena',                               'ZD7',  'AF', 36, 66, FALSE, NULL,         NULL),
    (251, 'Liechtenstein',                            'Liechtenstein',                            'HB0',  'EU', 14, 28, FALSE, NULL,         NULL),
    (252, 'St. Paul Island',                          'St. Paul Island',                          'CY9',  'NA', 5,  9,  FALSE, NULL,         NULL),
    (253, 'Saint Peter and Saint Paul Archipelago',   'Saint Peter and Saint Paul Archipelago',   'PP0S', 'SA', 11, 13, FALSE, NULL,         NULL),
    (254, 'Luxembourg',                               'Luxembourg',                               'LX',   'EU', 14, 27, FALSE, NULL,         NULL),
    (256, 'Madeira',                                  'Madeira',                                  'CT3',  'AF', 33, 36, FALSE, NULL,         NULL),
    (257, 'Malta',                                    'Malta',                                    '9H',   'EU', 15, 28, FALSE, NULL,         NULL),
    (259, 'Svalbard',                                 'Svalbard',                                 'JW',   'EU', 40, 18, FALSE, NULL,         NULL),
    (260, 'Monaco',                                   'Monaco',                                   '3A',   'EU', 14, 27, FALSE, NULL,         NULL),
    (262, 'Tajikistan',                               'Tajikistan',                               'EY',   'AS', 17, 30, FALSE, NULL,         NULL),
    (263, 'Netherlands',                              'Netherlands',                              'PA',   'EU', 14, 27, FALSE, NULL,         NULL),
    (265, 'Northern Ireland',                         'Northern Ireland',                         'GI',   'EU', 14, 27, FALSE, NULL,         NULL),
    (266, 'Norway',                                   'Norway',                                   'LA',   'EU', 14, 18, FALSE, NULL,         NULL),
    (269, 'Poland',                                   'Poland',                                   'SN',   'EU', 15, 28, FALSE, NULL,         NULL),
    (270, 'Tokelau',                                  'Tokelau',                                  'ZK3',  'OC', 31, 62, FALSE, NULL,         NULL),
    (272, 'Portugal',                                 'Portugal',                                 'CT',   'EU', 14, 37, FALSE, NULL,         NULL),
    (273, 'Trindade and Martin Vaz',                  'Trindade and Martin Vaz',                  'PP0T', 'SA', 11, 15, FALSE, NULL,         NULL),
    (274, 'Tristan da Cunha and Gough Islands',       'Tristan da Cunha and Gough Islands',       'ZD9',  'AF', 38, 66, FALSE, NULL,         NULL),
    (275, 'Romania',                                  'Romania',                                  'YO',   'EU', 20, 28, FALSE, NULL,         NULL),
    (276, 'Tromelin Island',                          'Tromelin Island',                          'FT/T', 'AF', 39, 53, FALSE, NULL,         NULL),
    (277, 'Saint Pierre and Miquelon',                'Saint Pierre and Miquelon',                'FP',   'NA', 5,  9,  FALSE, NULL,         NULL),
    (278, 'San Marino',                               'San Marino',                               'T7',   'EU', 15, 28, FALSE, NULL,         NULL),
    (279, 'Scotland',                                 'Scotland',                                 'GM',   'EU', 14, 27, FALSE, NULL,         NULL),
    (280, 'Turkmenistan',                             'Turkmenistan',                             'EZ',   'AS', 17, 30, FALSE, NULL,         NULL),
    (281, 'Spain',                                    'Spain',                                    'EA',   'EU', 14, 37, FALSE, NULL,         NULL),
    (282, 'Tuvalu',                                   'Tuvalu',                                   'T2',   'OC', 31, 65, FALSE, '1976-01-01', NULL),
    (283, 'Sovereign Base Areas of Akrotiri and Dhekelia', 'Sovereign Base Areas of Akrotiri and Dhekelia', 'ZC4', 'AS', 20, 39, FALSE, '1960-08-16', NULL),
    (284, 'Sweden',                                   'Sweden',                                   'SA',   'EU', 14, 18, FALSE, NULL,         NULL),
    (285, 'US Virgin Islands',                        'US Virgin Islands',                        'KP2',  'NA', 8,  11, FALSE, NULL,         NULL),
    (286, 'Uganda',                                   'Uganda',                                   '5X',   'AF', 37, 48, FALSE, NULL,         NULL),
    (287, 'Switzerland',                              'Switzerland',                              'HB',   'EU', 14, 28, FALSE, NULL,         NULL),
    (288, 'Ukraine',                                  'Ukraine',                                  'UR',   'EU', 16, 29, FALSE, NULL,         NULL),
    (289, 'United Nations Headquarters',              'United Nations Headquarters',              '4U',   'NA', 5,  8,  FALSE, NULL,         NULL),
    (291, 'United States of America',                 'United States of America',                 'K',    'NA', 3,  6,  FALSE, NULL,         NULL),
    (292, 'Uzbekistan',                               'Uzbekistan',                               'UJ',   'AS', 17, 30, FALSE, NULL,         NULL),
    (293, 'Viet Nam',                                 'Viet Nam',                                 '3W',   'AS', 26, 49, FALSE, NULL,         NULL),
    (294, 'Wales',                                    'Wales',                                    'GW',   'EU', 14, 27, FALSE, NULL,         NULL),
    (295, 'Vatican',                                  'Vatican',                                  'HV',   'EU', 15, 28, FALSE, NULL,         NULL),
    (296, 'Serbia',                                   'Serbia',                                   'YT',   'EU', 15, 28, FALSE, NULL,         NULL),
    (297, 'Wake Island',                              'Wake Island',                              'KH9',  'OC', 31, 65, FALSE, NULL,         NULL),
    (298, 'Wallis and Futuna Islands',                'Wallis and Futuna Islands',                'FW',   'OC', 32, 62, FALSE, NULL,         NULL),
    (299, 'West Malaysia',                            'West Malaysia',                            '9M2',  'AS', 28, 54, FALSE, '1963-09-16', NULL),
    (301, 'Gilbert Islands',                          'Gilbert Islands',                          'T30',  'OC', 31, 65, FALSE, NULL,         NULL),
    (302, 'Western Sahara',                           'Western Sahara',                           'S0',   'AF', 33, 46, FALSE, NULL,         NULL),
    (303, 'Willis Island',                            'Willis Island',                            'VK9W', 'OC', 30, 55, FALSE, NULL,         NULL),
    (304, 'Bahrain',                                  'Bahrain',                                  'A9',   'AS', 21, 39, FALSE, NULL,         NULL),
    (305, 'Bangladesh',                               'Bangladesh',                               'S2',   'AS', 22, 41, FALSE, NULL,         NULL),
    (306, 'Bhutan',                                   'Bhutan',                                   'A5',   'AS', 22, 41, FALSE, NULL,         NULL),
    (308, 'Costa Rica',                               'Costa Rica',                               'TI',   'NA', 7,  11, FALSE, NULL,         NULL),
    (309, 'Myanmar',                                  'Myanmar',                                  'XY',   'AS', 26, 49, FALSE, NULL,         NULL),
    (312, 'Cambodia',                                 'Cambodia',                                 'XU',   'AS', 26, 49, FALSE, NULL,         NULL),
    (315, 'Sri Lanka',                                'Sri Lanka',                                '4S',   'AS', 22, 41, FALSE, NULL,         NULL),
    (318, 'China',                                    'China',                                    'B',    'AS', 23, 33, FALSE, NULL,         NULL),
    (321, 'Hong Kong',                                'Hong Kong',                                'VR',   'AS', 24, 44, FALSE, NULL,         NULL),
    (324, 'India',                                    'India',                                    'VU',   'AS', 22, 41, FALSE, NULL,         NULL),
    (327, 'Indonesia',                                'Indonesia',                                'YB',   'OC', 28, 51, FALSE, '1963-05-01', NULL),
    (330, 'Iran',                                     'Iran',                                     'EP',   'AS', 21, 40, FALSE, NULL,         NULL),
    (333, 'Iraq',                                     'Iraq',                                     'YI',   'AS', 21, 39, FALSE, NULL,         NULL),
    (336, 'Israel',                                   'Israel',                                   '4X',   'AS', 20, 39, FALSE, NULL,         NULL),
    (339, 'Japan',                                    'Japan',                                    'JA',   'AS', 25, 45, FALSE, NULL,         NULL),
    (342, 'Jordan',                                   'Jordan',                                   'JY',   'AS', 20, 39, FALSE, NULL,         NULL),
    (344, 'Democratic People''s Republic of Korea',   'Democratic People''s Republic of Korea',   'P5',   'AS', 25, 44, FALSE, '1995-05-14', NULL),
    (345, 'Brunei Darussalam',                        'Brunei Darussalam',                        'V8',   'OC', 28, 54, FALSE, NULL,         NULL),
    (348, 'Kuwait',                                   'Kuwait',                                   '9K',   'AS', 21, 39, FALSE, NULL,         NULL),
    (354, 'Lebanon',                                  'Lebanon',                                  'OD',   'AS', 20, 39, FALSE, NULL,         NULL),
    (363, 'Mongolia',                                 'Mongolia',                                 'JT',   'AS', 23, 32, FALSE, NULL,         NULL),
    (369, 'Nepal',                                    'Nepal',                                    '9N',   'AS', 22, 42, FALSE, NULL,         NULL),
    (370, 'Oman',                                     'Oman',                                     'A4',   'AS', 21, 39, FALSE, NULL,         NULL),
    (372, 'Pakistan',                                 'Pakistan',                                 'AP',   'AS', 21, 41, FALSE, NULL,         NULL),
    (375, 'Philippines',                              'Philippines',                              'DU',   'OC', 27, 50, FALSE, NULL,         NULL),
    (376, 'Qatar',                                    'Qatar',                                    'A7',   'AS', 21, 39, FALSE, NULL,         NULL),
    (378, 'Saudi Arabia',                             'Saudi Arabia',                             'HZ',   'AS', 21, 39, FALSE, NULL,         NULL),
    (379, 'Seychelles',                               'Seychelles',                               'S7',   'AF', 39, 53, FALSE, NULL,         NULL),
    (381, 'Singapore',                                'Singapore',                                '9V',   'AS', 28, 54, FALSE, '1965-08-08', NULL),
    (382, 'Djibouti',                                 'Djibouti',                                 'J2',   'AF', 37, 48, FALSE, NULL,         NULL),
    (384, 'Syria',                                    'Syria',                                    'YK',   'AS', 20, 39, FALSE, NULL,         NULL),
    (386, 'Taiwan',                                   'Taiwan',                                   'BU',   'AS', 24, 44, FALSE, NULL,         NULL),
    (387, 'Thailand',                                 'Thailand',                                 'HS',   'AS', 26, 49, FALSE, NULL,         NULL),
    (390, 'Turkey',                                   'Turkey',                                   'TA',   'EU', 20, 39, FALSE, NULL,         NULL),
    (391, 'United Arab Emirates',                     'United Arab Emirates',                     'A6',   'AS', 21, 39, FALSE, NULL,         NULL),
    (400, 'Algeria',                                  'Algeria',                                  '7T',   'AF', 33, 37, FALSE, NULL,         NULL),
    (401, 'Angola',                                   'Angola',                                   'D2',   'AF', 36, 52, FALSE, NULL,         NULL),
    (402, 'Botswana',                                 'Botswana',                                 'A2',   'AF', 38, 57, FALSE, NULL,         NULL),
    (404, 'Burundi',                                  'Burundi',                                  '9U',   'AF', 36, 52, FALSE, '1962-07-01', NULL),
    (406, 'Cameroon',                                 'Cameroon',                                 'TJ',   'AF', 36, 47, FALSE, NULL,         NULL),
    (408, 'Central African Republic',                 'Central African Republic',                 'TL',   'AF', 36, 47, FALSE, '1960-08-13', NULL),
    (409, 'Cape Verde',                               'Cape Verde',                               'D4',   'AF', 35, 46, FALSE, NULL,         NULL),
    (410, 'Chad',                                     'Chad',                                     'TT',   'AF', 36, 47, FALSE, '1960-08-11', NULL),
    (411, 'Comoros',                                  'Comoros',                                  'D6',   'AF', 39, 53, FALSE, '1975-07-06', NULL),
    (412, 'Republic of the Congo',                    'Republic of the Congo',                    'TN',   'AF', 36, 52, FALSE, '1960-08-15', NULL),
    (414, 'Democratic Republic of the Congo',         'Democratic Republic of the Congo',         '9Q',   'AF', 36, 52, FALSE, NULL,         NULL),
    (416, 'Benin',                                    'Benin',                                    'TY',   'AF', 35, 46, FALSE, '1960-08-01', NULL),
    (420, 'Gabon',                                    'Gabon',                                    'TR',   'AF', 36, 52, FALSE, '1960-08-17', NULL),
    (422, 'The Gambia',                               'The Gambia',                               'C5',   'AF', 35, 46, FALSE, NULL,         NULL),
    (424, 'Ghana',                                    'Ghana',                                    '9G',   'AF', 35, 46, FALSE, '1957-03-05', NULL),
    (428, 'Côte d''Ivoire',                           'Côte d''Ivoire',                           'TU',   'AF', 35, 46, FALSE, '1960-08-07', NULL),
    (430, 'Kenya',                                    'Kenya',                                    '5Y',   'AF', 37, 48, FALSE, NULL,         NULL),
    (432, 'Lesotho',                                  'Lesotho',                                  '7P',   'AF', 38, 57, FALSE, NULL,         NULL),
    (434, 'Liberia',                                  'Liberia',                                  'EL',   'AF', 35, 46, FALSE, NULL,         NULL),
    (436, 'Libya',                                    'Libya',                                    '5A',   'AF', 34, 38, FALSE, NULL,         NULL),
    (438, 'Madagascar',                               'Madagascar',                               '5R',   'AF', 39, 53, FALSE, NULL,         NULL),
    (440, 'Malawi',                                   'Malawi',                                   '7Q',   'AF', 37, 53, FALSE, NULL,         NULL),
    (442, 'Mali',                                     'Mali',                                     'TZ',   'AF', 35, 46, FALSE, '1960-06-20', NULL),
    (444, 'Mauritania',                               'Mauritania',                               '5T',   'AF', 35, 46, FALSE, '1960-06-20', NULL),
    (446, 'Morocco',                                  'Morocco',                                  'CN',   'AF', 33, 37, FALSE, NULL,         NULL),
    (450, 'Nigeria',                                  'Nigeria',                                  '5N',   'AF', 35, 46, FALSE, NULL,         NULL),
    (452, 'Zimbabwe',                                 'Zimbabwe',                                 'Z2',   'AF', 38, 53, FALSE, NULL,         NULL),
    (453, 'Réunion',                                  'Réunion',                                  'FR',   'AF', 39, 53, FALSE, NULL,         NULL),
    (454, 'Rwanda',                                   'Rwanda',                                   '9X',   'AF', 36, 52, FALSE, '1962-07-01', NULL),
    (456, 'Senegal',                                  'Senegal',                                  '6V',   'AF', 35, 46, FALSE, '1960-06-20', NULL),
    (458, 'Sierra Leone',                             'Sierra Leone',                             '9L',   'AF', 35, 46, FALSE, NULL,         NULL),
    (460, 'Rotuma Island',                            'Rotuma Island',                            '3D2',  'OC', 32, 56, FALSE, NULL,         NULL),
    (462, 'South Africa',                             'South Africa',                             'ZR',   'AF', 38, 57, FALSE, NULL,         NULL),
    (464, 'Namibia',                                  'Namibia',                                  'V5',   'AF', 38, 57, FALSE, NULL,         NULL),
    (466, 'Sudan',                                    'Sudan',                                    'ST',   'AF', 34, 47, FALSE, NULL,         NULL),
    (468, 'Eswatini',                                 'Eswatini',                                 '3DA',  'AF', 38, 57, FALSE, NULL,         NULL),
    (470, 'Tanzania',                                 'Tanzania',                                 '5H',   'AF', 37, 53, FALSE, NULL,         NULL),
    (474, 'Tunisia',                                  'Tunisia',                                  '3V',   'AF', 33, 37, FALSE, NULL,         NULL),
    (478, 'Egypt',                                    'Egypt',                                    'SU',   'AF', 34, 38, FALSE, NULL,         NULL),
    (480, 'Burkina Faso',                             'Burkina Faso',                             'XT',   'AF', 35, 46, FALSE, '1960-08-16', NULL),
    (482, 'Zambia',                                   'Zambia',                                   '9I',   'AF', 36, 53, FALSE, NULL,         NULL),
    (483, 'Togo',                                     'Togo',                                     '5V',   'AF', 35, 46, FALSE, NULL,         NULL),
    (489, 'Conway Reef',                              'Conway Reef',                              '3D2',  'OC', 32, 56, FALSE, NULL,         NULL),
    (490, 'Banaba',                                   'Banaba',                                   'T33',  'OC', 31, 65, FALSE, NULL,         NULL),
    (492, 'Yemen',                                    'Yemen',                                    '7O',   'AS', 21, 39, FALSE, '1990-05-22', NULL),
    (497, 'Croatia',                                  'Croatia',                                  '9A',   'EU', 15, 28, FALSE, '1991-06-26', NULL),
    (499, 'Slovenia',                                 'Slovenia',                                 'S5',   'EU', 15, 28, FALSE, '1991-06-26', NULL),
    (501, 'Bosnia-Herzegovina',                       'Bosnia-Herzegovina',                       'E7',   'EU', 15, 28, FALSE, '1991-10-15', NULL),
    (502, 'North Macedonia',                          'North Macedonia',                          'Z3',   'EU', 15, 28, FALSE, '1991-09-08', NULL),
    (503, 'Czech Republic',                           'Czech Republic',                           'OK',   'EU', 15, 28, FALSE, '1993-01-01', NULL),
    (504, 'Slovakia',                                 'Slovakia',                                 'OM',   'EU', 15, 28, FALSE, '1993-01-01', NULL),
    (505, 'Pratas Island',                            'Pratas Island',                            'BV9P', 'AS', 24, 44, FALSE, '1994-01-01', NULL),
    (506, 'Scarborough Shoal',                        'Scarborough Shoal',                        'BS7',  'AS', 27, 50, FALSE, '1995-01-01', NULL),
    (507, 'Temotu Province',                          'Temotu Province',                          'H40',  'OC', 32, 51, FALSE, '1998-04-01', NULL),
    (508, 'Austral Islands',                          'Austral Islands',                          'FO',   'OC', 32, 63, FALSE, '1998-04-01', NULL),
    (509, 'Marquesas Islands',                        'Marquesas Islands',                        'FO',   'OC', 31, 63, FALSE, '1998-04-01', NULL),
    (510, 'Palestine',                                'Palestine',                                'E4',   'AS', 20, 39, FALSE, '1999-02-01', NULL),
    (511, 'Timor-Leste',                              'Timor-Leste',                              '4W',   'OC', 28, 54, FALSE, '2000-03-01', NULL),
    (512, 'Chesterfield Islands',                     'Chesterfield Islands',                     'FK',   'OC', 30, 56, FALSE, '2000-03-23', NULL),
    (513, 'Ducie Island',                             'Ducie Island',                             'VP6',  'OC', 32, 63, FALSE, '2001-11-16', NULL),
    (514, 'Montenegro',                               'Montenegro',                               '4O',   'EU', 15, 28, FALSE, '2006-06-28', NULL),
    (515, 'Swains Island',                            'Swains Island',                            'KH8',  'OC', 32, 62, FALSE, '2006-07-22', NULL),
    (516, 'Saint Barthélemy',                         'Saint Barthélemy',                         'FJ',   'NA', 8,  11, FALSE, '2007-12-14', NULL),
    (517, 'Curaçao',                                  'Curaçao',                                  'PJ2',  'SA', 9,  11, FALSE, '2010-10-10', NULL),
    (518, 'Sint Maarten',                             'Sint Maarten',                             'PJ7',  'NA', 8,  11, FALSE, '2010-10-10', NULL),
    (519, 'Saba and Sint Eustatius',                  'Saba and Sint Eustatius',                  'PJ5',  'NA', 8,  11, FALSE, '2010-10-10', NULL),
    (520, 'Bonaire',                                  'Bonaire',                                  'PJ4',  'SA', 9,  11, FALSE, '2010-10-10', NULL),
    (521, 'South Sudan',                              'South Sudan',                              'Z8',   'AF', 34, 48, FALSE, '2011-07-14', NULL),
    (522, 'Kosovo',                                   'Kosovo',                                   'Z6',   'EU', 15, 28, FALSE, '2018-01-21', NULL)
ON CONFLICT (entity_id) DO UPDATE SET
    name             = EXCLUDED.name,
    lotw_entity_name = EXCLUDED.lotw_entity_name,
    prefix           = EXCLUDED.prefix,
    continent        = EXCLUDED.continent,
    cq_zone          = EXCLUDED.cq_zone,
    itu_zone         = EXCLUDED.itu_zone,
    deleted          = EXCLUDED.deleted,
    valid_from       = EXCLUDED.valid_from,
    valid_to         = EXCLUDED.valid_to;


-- ─────────────────────────────────────────────────────────────────────────────
-- DXCC PREFIX ALIASES
-- Derived from dxcc_entities + high-impact manual aliases.
-- Must run after dxcc_entities seed.
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO dxcc_prefixes (prefix, entity_id, source)
SELECT prefix, entity_id, 'dxcc_entities'
FROM (
    SELECT DISTINCT ON (UPPER(BTRIM(prefix)))
           UPPER(BTRIM(prefix)) AS prefix,
           entity_id
    FROM dxcc_entities
    WHERE NULLIF(BTRIM(prefix), '') IS NOT NULL
      AND deleted = FALSE
    ORDER BY UPPER(BTRIM(prefix)), entity_id
) deduped
ON CONFLICT (prefix) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    source = EXCLUDED.source;

INSERT INTO dxcc_prefixes (prefix, entity_id, source)
VALUES
    -- United States of America (DXCC 291)
    ('W', 291, 'alias'),
    ('N', 291, 'alias'),
    ('AA', 291, 'alias'),
    ('AB', 291, 'alias'),
    ('AC', 291, 'alias'),
    ('AD', 291, 'alias'),
    ('AE', 291, 'alias'),
    ('AF', 291, 'alias'),
    ('AG', 291, 'alias'),
    ('AI', 291, 'alias'),
    ('AJ', 291, 'alias'),
    ('AK', 291, 'alias'),
    ('AL', 291, 'alias'),

    -- Canada (DXCC 1)
    ('VE', 1, 'alias'),
    ('VO', 1, 'alias'),
    ('VY', 1, 'alias'),
    ('CF', 1, 'alias'),
    ('CG', 1, 'alias'),
    ('CH', 1, 'alias'),
    ('CI', 1, 'alias'),
    ('CJ', 1, 'alias'),
    ('CK', 1, 'alias'),

    -- Japan (DXCC 339)
    ('JE', 339, 'alias'),
    ('JF', 339, 'alias'),
    ('JG', 339, 'alias'),
    ('JH', 339, 'alias'),
    ('JI', 339, 'alias'),
    ('JJ', 339, 'alias'),
    ('JK', 339, 'alias'),
    ('JL', 339, 'alias'),
    ('JM', 339, 'alias'),
    ('JN', 339, 'alias'),
    ('JO', 339, 'alias'),
    ('JP', 339, 'alias'),
    ('JQ', 339, 'alias'),
    ('JR', 339, 'alias'),
    ('JS', 339, 'alias'),
    ('7J', 339, 'alias'),
    ('7K', 339, 'alias'),
    ('7L', 339, 'alias'),
    ('7M', 339, 'alias'),
    ('7N', 339, 'alias'),
    ('8J', 339, 'alias')
ON CONFLICT (prefix) DO UPDATE
SET entity_id = EXCLUDED.entity_id,
    source = EXCLUDED.source;



-- ─────────────────────────────────────────────────────────────────────────────
-- BAND REGION ALLOCATIONS
-- Per-ITU-region band frequency edges and default visibility.
-- Region 1: Europe, Africa, Middle East, northern Asia
-- Region 2: Americas
-- Region 3: Asia-Pacific (south/east Asia, Oceania)
-- ─────────────────────────────────────────────────────────────────────────────
INSERT INTO band_region_allocations (itu_region, band_name, lower_freq, upper_freq, is_default_visible, notes) VALUES
-- 2190m (LF) — all regions, same allocation, uncommon
(1, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation'),
(2, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation'),
(3, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation'),
-- 630m (MF) — all regions, same allocation, uncommon
(1, '630m', 0.472, 0.479, FALSE, 'WRC-12 secondary allocation'),
(2, '630m', 0.472, 0.479, FALSE, 'WRC-12 secondary allocation'),
(3, '630m', 0.472, 0.479, FALSE, 'WRC-12 secondary allocation'),
-- 160m — R1 starts at 1.81, R2/R3 at 1.8
(1, '160m', 1.810, 2.000, TRUE, 'R1 starts at 1.810'),
(2, '160m', 1.800, 2.000, TRUE, NULL),
(3, '160m', 1.800, 2.000, TRUE, NULL),
-- 80m — R1 stops at 3.8, R2 goes to 4.0, R3 to 3.9
(1, '80m', 3.500, 3.800, TRUE, 'R1 upper edge 3.800'),
(2, '80m', 3.500, 4.000, TRUE, 'R2 widest allocation'),
(3, '80m', 3.500, 3.900, TRUE, 'R3 upper edge 3.900'),
-- 60m — varies widely by country, channelized in many
(1, '60m', 5.3515, 5.3665, TRUE, 'WRC-15 channelized, some countries wider'),
(2, '60m', 5.3515, 5.3665, TRUE, 'WRC-15 channelized, US has 5.332-5.405'),
(3, '60m', 5.3515, 5.3665, TRUE, 'WRC-15 channelized'),
-- 40m — R1/R3 stop at 7.2, R2 goes to 7.3
(1, '40m', 7.000, 7.200, TRUE, 'R1 upper edge 7.200'),
(2, '40m', 7.000, 7.300, TRUE, 'R2 widest allocation'),
(3, '40m', 7.000, 7.200, TRUE, 'R3 upper edge 7.200'),
-- 30m — same worldwide
(1, '30m', 10.100, 10.150, TRUE, NULL),
(2, '30m', 10.100, 10.150, TRUE, NULL),
(3, '30m', 10.100, 10.150, TRUE, NULL),
-- 20m — same worldwide
(1, '20m', 14.000, 14.350, TRUE, NULL),
(2, '20m', 14.000, 14.350, TRUE, NULL),
(3, '20m', 14.000, 14.350, TRUE, NULL),
-- 17m — same worldwide
(1, '17m', 18.068, 18.168, TRUE, NULL),
(2, '17m', 18.068, 18.168, TRUE, NULL),
(3, '17m', 18.068, 18.168, TRUE, NULL),
-- 15m — same worldwide
(1, '15m', 21.000, 21.450, TRUE, NULL),
(2, '15m', 21.000, 21.450, TRUE, NULL),
(3, '15m', 21.000, 21.450, TRUE, NULL),
-- 12m — same worldwide
(1, '12m', 24.890, 24.990, TRUE, NULL),
(2, '12m', 24.890, 24.990, TRUE, NULL),
(3, '12m', 24.890, 24.990, TRUE, NULL),
-- 10m — same worldwide
(1, '10m', 28.000, 29.700, TRUE, NULL),
(2, '10m', 28.000, 29.700, TRUE, NULL),
(3, '10m', 28.000, 29.700, TRUE, NULL),
-- 8m — experimental, some R1 countries only
(1, '8m', 40.000, 45.000, FALSE, 'Experimental in some R1 countries'),
-- 6m — R1 varies by country, R2/R3 full allocation
(1, '6m', 50.000, 52.000, TRUE, 'R1 varies; 50-52 common, some countries 50-54'),
(2, '6m', 50.000, 54.000, TRUE, 'Full R2 allocation'),
(3, '6m', 50.000, 54.000, TRUE, 'Full R3 allocation'),
-- 5m — Ireland only
(1, '5m', 58.000, 60.100, FALSE, 'Ireland experimental allocation'),
-- 4m — Region 1 only (not all countries)
(1, '4m', 69.900, 70.500, TRUE, 'Available in most R1 countries, not R2/R3'),
-- 2m — R2 goes to 148, R1/R3 to 146
(1, '2m', 144.000, 146.000, TRUE, 'R1 upper edge 146.000'),
(2, '2m', 144.000, 148.000, TRUE, 'R2 widest allocation'),
(3, '2m', 144.000, 148.000, TRUE, 'R3 same as R2'),
-- 1.25m — Region 2 only
(2, '1.25m', 219.000, 225.000, TRUE, 'R2 only, not allocated in R1/R3'),
-- 70cm — varies by region
(1, '70cm', 430.000, 440.000, TRUE, 'R1 430-440'),
(2, '70cm', 420.000, 450.000, TRUE, 'R2 widest allocation'),
(3, '70cm', 430.000, 440.000, TRUE, 'R3 430-440'),
-- 33cm — Region 2 only
(2, '33cm', 902.000, 928.000, FALSE, 'R2 only, not allocated in R1/R3'),
-- 23cm — R1 has UK extension to 1325
(1, '23cm', 1240.000, 1325.000, TRUE, 'UK extends to 1325'),
(2, '23cm', 1240.000, 1300.000, TRUE, NULL),
(3, '23cm', 1240.000, 1300.000, TRUE, NULL),
-- 13cm — all regions
(1, '13cm', 2300.000, 2450.000, FALSE, NULL),
(2, '13cm', 2300.000, 2450.000, FALSE, NULL),
(3, '13cm', 2300.000, 2450.000, FALSE, NULL),
-- 9cm — all regions
(1, '9cm', 3400.000, 3475.000, FALSE, 'R1 narrower'),
(2, '9cm', 3300.000, 3500.000, FALSE, 'R2 widest'),
(3, '9cm', 3300.000, 3500.000, FALSE, NULL),
-- 6cm — all regions
(1, '6cm', 5650.000, 5850.000, FALSE, NULL),
(2, '6cm', 5650.000, 5925.000, FALSE, 'R2 widest'),
(3, '6cm', 5650.000, 5850.000, FALSE, NULL),
-- 3cm — all regions
(1, '3cm', 10000.000, 10500.000, FALSE, NULL),
(2, '3cm', 10000.000, 10500.000, FALSE, NULL),
(3, '3cm', 10000.000, 10500.000, FALSE, NULL),
-- 1.25cm — all regions
(1, '1.25cm', 24000.000, 24250.000, FALSE, NULL),
(2, '1.25cm', 24000.000, 24250.000, FALSE, NULL),
(3, '1.25cm', 24000.000, 24250.000, FALSE, NULL),
-- 6mm — all regions
(1, '6mm', 47000.000, 47200.000, FALSE, NULL),
(2, '6mm', 47000.000, 47200.000, FALSE, NULL),
(3, '6mm', 47000.000, 47200.000, FALSE, NULL),
-- 4mm — all regions (EHF)
(1, '4mm', 76000.000, 81000.000, FALSE, NULL),
(2, '4mm', 76000.000, 81000.000, FALSE, NULL),
(3, '4mm', 76000.000, 81000.000, FALSE, NULL),
-- 2mm — all regions (EHF)
(1, '2mm', 122250.000, 141000.000, FALSE, NULL),
(2, '2mm', 122250.000, 141000.000, FALSE, NULL),
(3, '2mm', 122250.000, 141000.000, FALSE, NULL),
-- 1mm — all regions (EHF)
(1, '1mm', 241000.000, 250000.000, FALSE, NULL),
(2, '1mm', 241000.000, 250000.000, FALSE, NULL),
(3, '1mm', 241000.000, 250000.000, FALSE, NULL)
ON CONFLICT (itu_region, band_name) DO UPDATE SET
    lower_freq = EXCLUDED.lower_freq,
    upper_freq = EXCLUDED.upper_freq,
    is_default_visible = EXCLUDED.is_default_visible,
    notes = EXCLUDED.notes;


-- ─────────────────────────────────────────────────────────────────────────────
-- Auth worker role grants (folded from 005_auth_worker_role_grants.sql)
-- ─────────────────────────────────────────────────────────────────────────────

-- +goose StatementBegin
DO $do$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger')
       AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api') THEN
        EXECUTE 'GRANT radioledger_api TO radioledger';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger')
       AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker') THEN
        EXECUTE 'GRANT radioledger_worker TO radioledger';
    END IF;
END
$do$;
-- +goose StatementEnd

GRANT INSERT, UPDATE ON TABLE public.users TO radioledger_worker;
GRANT INSERT ON TABLE public.logbooks TO radioledger_worker;

-- +goose StatementBegin
DO $do$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_policies
        WHERE schemaname = 'public' AND tablename = 'users' AND policyname = 'users_worker_select'
    ) THEN
        EXECUTE 'CREATE POLICY users_worker_select ON public.users FOR SELECT TO radioledger_worker USING (TRUE)';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_policies
        WHERE schemaname = 'public' AND tablename = 'users' AND policyname = 'users_worker_insert'
    ) THEN
        EXECUTE 'CREATE POLICY users_worker_insert ON public.users FOR INSERT TO radioledger_worker WITH CHECK (TRUE)';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_policies
        WHERE schemaname = 'public' AND tablename = 'users' AND policyname = 'users_worker_update'
    ) THEN
        EXECUTE 'CREATE POLICY users_worker_update ON public.users FOR UPDATE TO radioledger_worker USING (TRUE) WITH CHECK (TRUE)';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_policies
        WHERE schemaname = 'public' AND tablename = 'logbooks' AND policyname = 'logbooks_worker_insert'
    ) THEN
        EXECUTE 'CREATE POLICY logbooks_worker_insert ON public.logbooks FOR INSERT TO radioledger_worker WITH CHECK (TRUE)';
    END IF;
END
$do$;
-- +goose StatementEnd

-- ─────────────────────────────────────────────────────────────────────────────
-- Invite keys (folded from 006_invite_keys.sql)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE invite_keys (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(8) NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    used_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    max_uses   INTEGER NOT NULL DEFAULT 1 CHECK (max_uses >= 1),
    uses_count INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,

    CONSTRAINT invite_keys_code_format CHECK (code ~ '^[A-HJ-NP-Z2-9]{8}$'),
    CONSTRAINT invite_keys_usage_limit CHECK (uses_count <= max_uses)
);

CREATE UNIQUE INDEX idx_invite_keys_code ON invite_keys(code);
CREATE INDEX idx_invite_keys_created_by ON invite_keys(created_by, created_at DESC);

ALTER TABLE invite_keys ENABLE ROW LEVEL SECURITY;

CREATE POLICY invite_keys_isolation ON invite_keys
    FOR ALL TO radioledger_api
    USING (created_by = app_current_user_id())
    WITH CHECK (created_by = app_current_user_id());

CREATE POLICY invite_keys_worker_read ON invite_keys
    FOR SELECT TO radioledger_worker
    USING (TRUE);

CREATE POLICY invite_keys_worker_update ON invite_keys
    FOR UPDATE TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE invite_keys TO radioledger_api;
GRANT USAGE, SELECT ON SEQUENCE invite_keys_id_seq TO radioledger_api;
GRANT SELECT, UPDATE ON TABLE invite_keys TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE invite_keys_id_seq TO radioledger_worker;



-- ─────────────────────────────────────────────────────────────────────────────
-- WORKER ROLE: import_jobs and qsos access
-- Grant radioledger_worker full access to import_jobs and import_job_errors
-- so the ADIF import River worker can read/update job status and write errors.
-- ─────────────────────────────────────────────────────────────────────────────

-- RLS policies: worker can see and modify all import jobs (no user_id filter).
CREATE POLICY import_jobs_worker ON import_jobs
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

CREATE POLICY import_job_errors_worker ON import_job_errors
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

-- Table grants
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE import_jobs TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE import_job_errors TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE import_jobs_id_seq TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE import_job_errors_id_seq TO radioledger_worker;

-- Worker also needs access to logbooks (to resolve logbook for import)
-- and qsos (to insert imported QSOs). qsos already has a worker read policy;
-- add insert/update for imports.
CREATE POLICY qso_worker_write ON qsos
    FOR INSERT TO radioledger_worker
    WITH CHECK (TRUE);

CREATE POLICY qso_worker_update ON qsos
    FOR UPDATE TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT INSERT, UPDATE ON TABLE qsos TO radioledger_worker;

-- Worker needs logbook read access to verify logbook exists
CREATE POLICY logbooks_worker_read ON logbooks
    FOR SELECT TO radioledger_worker
    USING (TRUE);

GRANT SELECT ON TABLE logbooks TO radioledger_worker;


-- ─────────────────────────────────────────────────────────────────────────────
-- PSK REPORTER: psk_reception_reports (folded from 002_pskreporter.sql)
-- ─────────────────────────────────────────────────────────────────────────────

-- ─────────────────────────────────────────────────────────────────────────────
-- PSK REPORTER: psk_reception_reports
-- Stores reception reports fetched from pskreporter.info for each user.
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE psk_reception_reports (
    id                  BIGSERIAL PRIMARY KEY,

    -- Owning user (the station whose callsign was queried)
    user_id             BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- The station that was heard (could be the user's own callsign or another)
    sender_callsign     TEXT NOT NULL,

    -- The station that reported hearing the sender
    receiver_callsign   TEXT NOT NULL,

    -- Signal info
    frequency_khz       NUMERIC,
    mode                TEXT,
    snr                 SMALLINT,
    grid                TEXT,

    -- When the reception was observed
    spotted_at          TIMESTAMPTZ NOT NULL,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_psk_sender_upper   CHECK (sender_callsign   = UPPER(sender_callsign)),
    CONSTRAINT chk_psk_receiver_upper CHECK (receiver_callsign = UPPER(receiver_callsign))
);

-- Primary access pattern: list reports for a user ordered by time
CREATE INDEX idx_psk_reports_user_time
    ON psk_reception_reports(user_id, spotted_at DESC);

-- QSO matching pattern: find reports by sender callsign + time
CREATE INDEX idx_psk_reports_sender_time
    ON psk_reception_reports(sender_callsign, spotted_at DESC);

-- Dedup: one row per (user, sender, receiver, spotted_at)
CREATE UNIQUE INDEX uq_psk_reports_identity
    ON psk_reception_reports(user_id, sender_callsign, receiver_callsign, spotted_at);

ALTER TABLE psk_reception_reports ENABLE ROW LEVEL SECURITY;

-- Authenticated API users can see only their own rows
CREATE POLICY psk_reports_isolation ON psk_reception_reports
    FOR ALL TO radioledger_api
    USING (user_id = app_current_user_id());

-- Workers can insert / select / delete across all users
CREATE POLICY psk_reports_worker_all ON psk_reception_reports
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT SELECT ON TABLE psk_reception_reports TO radioledger_api;
GRANT INSERT, UPDATE, DELETE ON TABLE psk_reception_reports TO radioledger_worker;
GRANT SELECT ON TABLE psk_reception_reports TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE psk_reception_reports_id_seq TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE psk_reception_reports_id_seq TO radioledger_api;

COMMENT ON TABLE psk_reception_reports IS
    'Reception reports fetched from pskreporter.info. '
    'One row per reception event per user. Pruned by background worker after 30 days.';


-- ─────────────────────────────────────────────────────────────────────────────
-- ZIP CENTROIDS: local Maidenhead grid derivation (folded from 008_zip_centroids.sql)
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS zip_centroids (
    zip_code   TEXT             PRIMARY KEY,
    latitude   DOUBLE PRECISION NOT NULL,
    longitude  DOUBLE PRECISION NOT NULL,
    city       TEXT,
    state      TEXT,
    updated_at TIMESTAMPTZ      NOT NULL DEFAULT now()
);

-- Index for state-level queries (e.g. batch refresh filtering).
CREATE INDEX IF NOT EXISTS idx_zip_centroids_state ON zip_centroids (state);

-- Grant access to the application roles.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api') THEN
        GRANT SELECT ON zip_centroids TO radioledger_api;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker') THEN
        GRANT SELECT, INSERT, UPDATE ON zip_centroids TO radioledger_worker;
    END IF;
END
$$;
-- +goose StatementEnd



-- ─────────────────────────────────────────────────────────────────────────────
-- eQSL Confirmation Pull (folded from 009_eqsl_confirmation_pull.sql)
-- ─────────────────────────────────────────────────────────────────────────────

-- ─────────────────────────────────────────────────────────────────────────────
-- eQSL Confirmation Pull: eqsl_sync_status table and qso_confirmations additions.
--
-- eqsl_sync_status  — per-user eQSL pull state (last_pull_at checkpoint)
-- qso_confirmations — add eqsl_ag (Authenticity Guaranteed) boolean column
-- ─────────────────────────────────────────────────────────────────────────────

-- Per-user eQSL pull state (mirrors lotw_sync_status structure).
CREATE TABLE IF NOT EXISTS eqsl_sync_status (
    id           SERIAL PRIMARY KEY,
    user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_pull_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS idx_eqsl_sync_status_user ON eqsl_sync_status (user_id);

ALTER TABLE eqsl_sync_status ENABLE ROW LEVEL SECURITY;
CREATE POLICY eqsl_sync_status_isolation ON eqsl_sync_status
    FOR SELECT TO radioledger_api
    USING (user_id = app_current_user_id());
CREATE POLICY eqsl_sync_status_worker_all ON eqsl_sync_status
    FOR ALL TO radioledger_worker
    USING (TRUE)
    WITH CHECK (TRUE);

GRANT SELECT ON TABLE eqsl_sync_status TO radioledger_api;
GRANT INSERT, UPDATE, SELECT ON TABLE eqsl_sync_status TO radioledger_worker;
GRANT USAGE, SELECT ON SEQUENCE eqsl_sync_status_id_seq TO radioledger_api;
GRANT USAGE, SELECT ON SEQUENCE eqsl_sync_status_id_seq TO radioledger_worker;

-- Add eqsl_ag (Authenticity Guaranteed) to qso_confirmations.
-- TRUE when the eQSL sender had AG status at the time the card was sent.
ALTER TABLE qso_confirmations
    ADD COLUMN IF NOT EXISTS eqsl_ag BOOLEAN DEFAULT FALSE;


-- +goose Down
-- Drop triggers first
DROP TABLE IF EXISTS invite_keys CASCADE;
DROP TABLE IF EXISTS psk_reception_reports CASCADE;
DROP TABLE IF EXISTS zip_centroids        CASCADE;
DROP TABLE IF EXISTS eqsl_sync_status     CASCADE;
DROP TRIGGER IF EXISTS trg_logbooks_owner_role    ON logbooks;
DROP TRIGGER IF EXISTS trg_qso_identity_scope     ON qsos;
DROP TRIGGER IF EXISTS trg_qso_edit_mark_dirty    ON qsos;
DROP TRIGGER IF EXISTS trg_user_location          ON users;
DROP TRIGGER IF EXISTS trg_qso_locations          ON qsos;
DROP TRIGGER IF EXISTS trg_paper_qsl_item_scope   ON paper_qsl_batch_items;

-- Drop functions
DROP FUNCTION IF EXISTS ensure_logbook_owner_role()                         CASCADE;
DROP FUNCTION IF EXISTS enforce_qso_identity_scope()                        CASCADE;
DROP FUNCTION IF EXISTS mark_sync_dirty_on_qso_edit()                       CASCADE;
DROP FUNCTION IF EXISTS update_user_location()                              CASCADE;
DROP FUNCTION IF EXISTS update_qso_locations()                              CASCADE;
DROP FUNCTION IF EXISTS maidenhead_to_point(TEXT)                           CASCADE;
DROP FUNCTION IF EXISTS enforce_paper_qsl_item_scope()                     CASCADE;
DROP FUNCTION IF EXISTS app_has_logbook_min_role(BIGINT, BIGINT, TEXT)     CASCADE;
DROP FUNCTION IF EXISTS app_role_rank(TEXT)                                 CASCADE;
DROP FUNCTION IF EXISTS app_current_user_id()                               CASCADE;
DROP FUNCTION IF EXISTS find_qso_matches(TEXT, TEXT, TEXT, TEXT, TIMESTAMPTZ, INTERVAL, BIGINT) CASCADE;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS sync_conflicts         CASCADE;
-- river_job is managed by River migrations, not goose.
DROP TABLE IF EXISTS system_settings        CASCADE;
DROP TABLE IF EXISTS sync_circuit_state     CASCADE;
DROP TABLE IF EXISTS sync_rate_limit_window CASCADE;
DROP TABLE IF EXISTS callsign_cache         CASCADE;
DROP TABLE IF EXISTS paper_qsl_batch_items  CASCADE;
DROP TABLE IF EXISTS paper_qsl_batches      CASCADE;
DROP TABLE IF EXISTS qsl_routes             CASCADE;
DROP TABLE IF EXISTS notifications          CASCADE;
DROP TABLE IF EXISTS audit_log              CASCADE;
DROP TABLE IF EXISTS api_keys               CASCADE;
DROP TABLE IF EXISTS user_service_credentials CASCADE;
DROP TABLE IF EXISTS import_job_errors      CASCADE;
DROP TABLE IF EXISTS import_jobs            CASCADE;
DROP TABLE IF EXISTS award_progress         CASCADE;
DROP TABLE IF EXISTS award_tracking         CASCADE;
DROP TABLE IF EXISTS sota_summits           CASCADE;
DROP TABLE IF EXISTS pota_parks             CASCADE;
DROP TABLE IF EXISTS band_region_allocations CASCADE;
DROP TABLE IF EXISTS modes                  CASCADE;
DROP TABLE IF EXISTS bands                  CASCADE;
DROP TABLE IF EXISTS callsign_sync_runs     CASCADE;
DROP TABLE IF EXISTS operator_profiles      CASCADE;
DROP TABLE IF EXISTS callsign_records       CASCADE;
DROP TABLE IF EXISTS dxcc_prefixes          CASCADE;
DROP TABLE IF EXISTS dxcc_entities          CASCADE;
DROP TABLE IF EXISTS lotw_sync_status       CASCADE;
DROP TABLE IF EXISTS lotw_sync_jobs         CASCADE;
DROP TABLE IF EXISTS sync_status            CASCADE;
DROP TABLE IF EXISTS contest_multipliers    CASCADE;
DROP TABLE IF EXISTS contest_qso_exchange   CASCADE;
DROP TABLE IF EXISTS qsos                   CASCADE;
DROP TABLE IF EXISTS operator_verifications CASCADE;
DROP TABLE IF EXISTS qso_confirmations      CASCADE;
DROP TABLE IF EXISTS activations            CASCADE;
DROP TABLE IF EXISTS contest_session_operators CASCADE;
DROP TABLE IF EXISTS contest_sessions       CASCADE;
DROP TABLE IF EXISTS contests               CASCADE;
DROP TABLE IF EXISTS station_callsign_operators CASCADE;
DROP TABLE IF EXISTS station_callsigns      CASCADE;
DROP TABLE IF EXISTS operators              CASCADE;
DROP TABLE IF EXISTS user_roles             CASCADE;
DROP TABLE IF EXISTS logbooks               CASCADE;
DROP TABLE IF EXISTS station_locations      CASCADE;
DROP TABLE IF EXISTS user_callsigns         CASCADE;
DROP TABLE IF EXISTS users                  CASCADE;

DROP EXTENSION IF EXISTS pgcrypto;
DROP EXTENSION IF EXISTS postgis;

-- +goose StatementBegin
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='radioledger_worker') THEN DROP OWNED BY radioledger_worker; END IF; END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='radioledger_api')    THEN DROP OWNED BY radioledger_api;    END IF; END $$;
-- +goose StatementEnd

DROP ROLE IF EXISTS radioledger_worker;
DROP ROLE IF EXISTS radioledger_api;
