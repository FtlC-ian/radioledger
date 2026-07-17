-- name: GetDXCCTotalEntities :one
SELECT COUNT(*)::bigint AS total_entities
FROM dxcc_entities;

-- name: GetDXCCSummary :one
WITH qso_entities AS (
    SELECT
        COALESCE(q.dxcc, d_match.entity_id)::int AS entity_id,
        q.lotw_qsl_rcvd,
        q.qsl_rcvd,
        q.eqsl_qsl_rcvd
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN LATERAL (
        SELECT d.entity_id
        FROM dxcc_entities d
        WHERE NULLIF(BTRIM(q.country), '') IS NOT NULL
          AND (
              UPPER(BTRIM(d.name)) = UPPER(BTRIM(q.country))
              OR (
                  d.lotw_entity_name IS NOT NULL
                  AND UPPER(BTRIM(d.lotw_entity_name)) = UPPER(BTRIM(q.country))
              )
          )
        ORDER BY d.entity_id
        LIMIT 1
    ) d_match ON TRUE
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
)
SELECT
    COUNT(DISTINCT entity_id) FILTER (WHERE entity_id IS NOT NULL)::bigint AS worked,
    COUNT(DISTINCT entity_id) FILTER (
        WHERE entity_id IS NOT NULL
          AND (
              lotw_qsl_rcvd = 'Y'
              OR qsl_rcvd = 'Y'
              OR eqsl_qsl_rcvd = 'Y'
          )
    )::bigint AS confirmed
FROM qso_entities;

-- name: ListDXCCByBand :many
WITH qso_entities AS (
    SELECT
        q.band,
        COALESCE(q.dxcc, d_match.entity_id)::int AS entity_id,
        q.lotw_qsl_rcvd,
        q.qsl_rcvd,
        q.eqsl_qsl_rcvd
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN LATERAL (
        SELECT d.entity_id
        FROM dxcc_entities d
        WHERE NULLIF(BTRIM(q.country), '') IS NOT NULL
          AND (
              UPPER(BTRIM(d.name)) = UPPER(BTRIM(q.country))
              OR (
                  d.lotw_entity_name IS NOT NULL
                  AND UPPER(BTRIM(d.lotw_entity_name)) = UPPER(BTRIM(q.country))
              )
          )
        ORDER BY d.entity_id
        LIMIT 1
    ) d_match ON TRUE
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
      AND NULLIF(BTRIM(q.band), '') IS NOT NULL
)
SELECT
    band,
    COUNT(DISTINCT entity_id) FILTER (WHERE entity_id IS NOT NULL)::bigint AS worked,
    COUNT(DISTINCT entity_id) FILTER (
        WHERE entity_id IS NOT NULL
          AND (
              lotw_qsl_rcvd = 'Y'
              OR qsl_rcvd = 'Y'
              OR eqsl_qsl_rcvd = 'Y'
          )
    )::bigint AS confirmed
FROM qso_entities
GROUP BY band
ORDER BY band ASC;

-- name: ListDXCCEntitiesProgress :many
WITH qso_entities AS (
    SELECT
        q.band,
        q.datetime_on,
        COALESCE(q.dxcc, d_match.entity_id)::int AS entity_id,
        q.lotw_qsl_rcvd,
        q.qsl_rcvd,
        q.eqsl_qsl_rcvd
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN LATERAL (
        SELECT d.entity_id
        FROM dxcc_entities d
        WHERE NULLIF(BTRIM(q.country), '') IS NOT NULL
          AND (
              UPPER(BTRIM(d.name)) = UPPER(BTRIM(q.country))
              OR (
                  d.lotw_entity_name IS NOT NULL
                  AND UPPER(BTRIM(d.lotw_entity_name)) = UPPER(BTRIM(q.country))
              )
          )
        ORDER BY d.entity_id
        LIMIT 1
    ) d_match ON TRUE
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
),
entity_work AS (
    SELECT
        entity_id,
        TRUE AS worked,
        BOOL_OR(lotw_qsl_rcvd = 'Y' OR qsl_rcvd = 'Y' OR eqsl_qsl_rcvd = 'Y') AS confirmed,
        ARRAY_AGG(DISTINCT band ORDER BY band) FILTER (WHERE NULLIF(BTRIM(band), '') IS NOT NULL) AS bands,
        MIN(datetime_on)::timestamptz AS first_qso,
        COUNT(*)::bigint AS qso_count
    FROM qso_entities
    WHERE entity_id IS NOT NULL
    GROUP BY entity_id
)
SELECT
    d.entity_id,
    d.name,
    d.prefix,
    d.continent,
    COALESCE(w.worked, FALSE) AS worked,
    COALESCE(w.confirmed, FALSE) AS confirmed,
    COALESCE(w.bands, ARRAY[]::text[])::text[] AS bands,
    w.first_qso,
    COALESCE(w.qso_count, 0)::bigint AS qso_count
FROM dxcc_entities d
LEFT JOIN entity_work w ON w.entity_id = d.entity_id
ORDER BY d.name ASC;

-- name: ListDXCCEntityQSODetails :many
WITH qso_entities AS (
    SELECT
        q.uuid,
        q.callsign,
        q.band,
        q.mode,
        q.datetime_on,
        COALESCE(q.dxcc, d_match.entity_id)::int AS entity_id
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN LATERAL (
        SELECT d.entity_id
        FROM dxcc_entities d
        WHERE NULLIF(BTRIM(q.country), '') IS NOT NULL
          AND (
              UPPER(BTRIM(d.name)) = UPPER(BTRIM(q.country))
              OR (
                  d.lotw_entity_name IS NOT NULL
                  AND UPPER(BTRIM(d.lotw_entity_name)) = UPPER(BTRIM(q.country))
              )
          )
        ORDER BY d.entity_id
        LIMIT 1
    ) d_match ON TRUE
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
)
SELECT
    entity_id,
    uuid,
    callsign,
    band,
    mode,
    datetime_on
FROM qso_entities
WHERE entity_id IS NOT NULL
ORDER BY entity_id ASC, datetime_on DESC;

-- name: ListWorkedStates :many
WITH state_rows AS (
    SELECT
        COALESCE(
            NULLIF(BTRIM(q.state), ''),
            NULLIF(BTRIM(cr.state_province), '')
        ) AS derived_state,
        q.datetime_on
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN callsign_records cr
        ON UPPER(BTRIM(q.callsign)) = UPPER(BTRIM(cr.callsign))
       AND cr.country = 'US'
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
      AND COALESCE(
          NULLIF(BTRIM(q.state), ''),
          NULLIF(BTRIM(cr.state_province), '')
      ) IS NOT NULL
)
SELECT
    UPPER(BTRIM(derived_state))::text AS state_value,
    COUNT(*)::bigint AS qso_count,
    MIN(datetime_on)::timestamptz AS first_qso
FROM state_rows
GROUP BY UPPER(BTRIM(derived_state))::text
ORDER BY state_value ASC;

-- name: ListWorkedGridSquares :many
SELECT
    UPPER(LEFT(BTRIM(q.gridsquare), 4))::text AS grid_square,
    COUNT(*)::bigint AS qso_count,
    MIN(q.datetime_on)::timestamptz AS first_qso,
    MAX(q.datetime_on)::timestamptz AS last_qso
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND q.gridsquare ~* '^[A-R]{2}[0-9]{2}'
GROUP BY UPPER(LEFT(BTRIM(q.gridsquare), 4))::text
ORDER BY qso_count DESC, grid_square ASC;

-- name: ListWorkedZones :many
-- WAZ: CQ zones worked from QSO records.
SELECT
    q.cq_zone::int                          AS zone,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso,
    COALESCE(BOOL_OR(
        q.lotw_qsl_rcvd = 'Y'
        OR q.qsl_rcvd = 'Y'
        OR q.eqsl_qsl_rcvd = 'Y'
    ), false)                               AS confirmed
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND q.cq_zone IS NOT NULL
  AND q.cq_zone BETWEEN 1 AND 40
GROUP BY q.cq_zone
ORDER BY q.cq_zone ASC;

-- name: ListWorkedWPXPrefixes :many
-- WPX: raw callsigns for prefix extraction (done in Go).
SELECT
    q.callsign,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso,
    COALESCE(BOOL_OR(
        q.lotw_qsl_rcvd = 'Y'
        OR q.qsl_rcvd = 'Y'
        OR q.eqsl_qsl_rcvd = 'Y'
    ), false)                               AS confirmed
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND NULLIF(BTRIM(q.callsign), '') IS NOT NULL
GROUP BY q.callsign
ORDER BY q.callsign ASC;

-- name: ListWorkedSOTASummits :many
-- SOTA chaser: summits worked as chaser from sig/sig_info fields.
SELECT
    UPPER(BTRIM(q.sota_ref))::text          AS summit_ref,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso,
    BOOL_OR(
        q.lotw_qsl_rcvd = 'Y'
        OR q.qsl_rcvd = 'Y'
        OR q.eqsl_qsl_rcvd = 'Y'
    )                                       AS confirmed
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND NULLIF(BTRIM(q.sota_ref), '') IS NOT NULL
GROUP BY UPPER(BTRIM(q.sota_ref))
ORDER BY summit_ref ASC;

-- name: ListActivatedSOTASummits :many
-- SOTA activator: summits activated (my_sota_ref from activator QSOs).
SELECT
    UPPER(BTRIM(q.my_sota_ref))::text       AS summit_ref,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND NULLIF(BTRIM(q.my_sota_ref), '') IS NOT NULL
GROUP BY UPPER(BTRIM(q.my_sota_ref))
ORDER BY summit_ref ASC;

-- name: ListHuntedPOTAParks :many
-- POTA hunter: parks worked (sig=POTA, sig_info has park reference).
SELECT
    UPPER(BTRIM(ref))::text                 AS park_ref,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso,
    BOOL_OR(
        q.lotw_qsl_rcvd = 'Y'
        OR q.qsl_rcvd = 'Y'
        OR q.eqsl_qsl_rcvd = 'Y'
    )                                       AS confirmed
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
CROSS JOIN LATERAL regexp_split_to_table(COALESCE(q.sig_info, ''), '\s*,\s*') AS ref
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND UPPER(BTRIM(COALESCE(q.sig, ''))) = 'POTA'
  AND NULLIF(BTRIM(ref), '') IS NOT NULL
  AND UPPER(BTRIM(ref)) ~ '^[A-Z]{1,3}-[0-9]{1,5}$'
GROUP BY UPPER(BTRIM(ref))
ORDER BY park_ref ASC;

-- name: ListActivatedPOTAParks :many
-- POTA activator: parks activated from the operator-side MY_POTA_REF ADIF field.
SELECT
    UPPER(BTRIM(ref))::text                 AS park_ref,
    COUNT(*)::bigint                        AS qso_count,
    MIN(q.datetime_on)::timestamptz         AS first_qso
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
CROSS JOIN LATERAL unnest(COALESCE(q.my_pota_refs, ARRAY[]::text[])) AS ref
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND NULLIF(BTRIM(ref), '') IS NOT NULL
  AND UPPER(BTRIM(ref)) ~ '^[A-Z]{1,3}-[0-9]{1,5}$'
GROUP BY UPPER(BTRIM(ref))
ORDER BY park_ref ASC;

-- name: UpsertAwardProgress :exec
-- Upsert a single award_progress row. Used by AwardRefreshWorker.
INSERT INTO award_progress (
    user_id,
    award_type,
    entity_key,
    band,
    mode,
    worked,
    confirmed,
    qso_count,
    last_qso_at,
    dirty,
    updated_at
)
VALUES (
    sqlc.arg(user_id)::bigint,
    sqlc.arg(award_type)::text,
    sqlc.arg(entity_key)::text,
    sqlc.narg(band)::text,
    sqlc.narg(mode)::text,
    sqlc.arg(worked)::boolean,
    sqlc.arg(confirmed)::boolean,
    sqlc.arg(qso_count)::bigint,
    sqlc.narg(last_qso_at)::timestamptz,
    FALSE,
    NOW()
)
ON CONFLICT ON CONSTRAINT uq_award_progress_nulls_not_distinct
DO UPDATE SET
    worked      = EXCLUDED.worked,
    confirmed   = EXCLUDED.confirmed,
    qso_count   = EXCLUDED.qso_count,
    last_qso_at = EXCLUDED.last_qso_at,
    dirty       = FALSE,
    updated_at  = NOW();

-- name: GetAwardProgressSummary :many
-- Unified summary of all award types for a user (for GET /v1/awards).
-- Returns one row per award_type with aggregate counts.
SELECT
    award_type,
    COUNT(*) FILTER (WHERE worked = TRUE)::bigint              AS worked_count,
    COUNT(*) FILTER (WHERE confirmed = TRUE)::bigint           AS confirmed_count,
    MAX(last_qso_at)::timestamptz                              AS latest_qso_at,
    MAX(updated_at)::timestamptz                               AS cache_updated_at
FROM award_progress
GROUP BY award_type
ORDER BY award_type ASC;

-- name: ListAwardProgressByType :many
-- Detailed progress rows for a specific award type (for GET /v1/awards/:type).
SELECT
    id,
    award_type,
    entity_key,
    band,
    mode,
    worked,
    confirmed,
    qso_count,
    last_qso_at,
    updated_at
FROM award_progress
WHERE award_type = sqlc.arg(award_type)::text
ORDER BY entity_key ASC, band ASC NULLS LAST, mode ASC NULLS LAST;

-- name: MarkUserAwardsDirty :exec
-- Called after QSO mutations to flag all cached award progress for recalculation.
UPDATE award_progress
SET dirty = TRUE, updated_at = NOW()
WHERE user_id = sqlc.arg(user_id)::bigint;

-- name: DeleteAwardProgressByType :exec
-- Clears cached progress for a specific award type before full recalculation.
DELETE FROM award_progress
WHERE user_id = sqlc.arg(user_id)::bigint
  AND award_type = sqlc.arg(award_type)::text;
