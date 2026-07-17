-- name: CreateActivation :one
INSERT INTO activations (
    user_id,
    logbook_id,
    program,
    reference,
    activation_date,
    station_location_id,
    notes,
    status
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(logbook_id),
    sqlc.arg(program),
    UPPER(sqlc.arg(reference)::text),
    sqlc.arg(activation_date),
    sqlc.narg(station_location_id),
    sqlc.narg(notes),
    sqlc.arg(status)
)
RETURNING
    id,
    uuid,
    user_id,
    logbook_id,
    program,
    reference,
    activation_date,
    station_location_id,
    notes,
    status,
    qso_count,
    unique_callsigns,
    created_at,
    updated_at;

-- name: GetActivationByUUIDAndProgram :one
SELECT
    a.id,
    a.uuid,
    a.user_id,
    a.logbook_id,
    l.uuid AS logbook_uuid,
    a.program,
    a.reference,
    a.activation_date,
    a.station_location_id,
    sl.uuid AS station_location_uuid,
    a.notes,
    a.status,
    a.qso_count,
    a.unique_callsigns,
    a.created_at,
    a.updated_at
FROM activations a
JOIN logbooks l ON l.id = a.logbook_id
LEFT JOIN station_locations sl ON sl.id = a.station_location_id
WHERE a.uuid = sqlc.arg(activation_uuid)
  AND a.program = sqlc.arg(program);

-- name: ListActivationsByProgram :many
SELECT
    a.id,
    a.uuid,
    a.user_id,
    a.logbook_id,
    l.uuid AS logbook_uuid,
    a.program,
    a.reference,
    a.activation_date,
    a.station_location_id,
    sl.uuid AS station_location_uuid,
    a.notes,
    a.status,
    a.qso_count,
    a.unique_callsigns,
    a.created_at,
    a.updated_at
FROM activations a
JOIN logbooks l ON l.id = a.logbook_id
LEFT JOIN station_locations sl ON sl.id = a.station_location_id
WHERE a.user_id = sqlc.arg(user_id)
  AND a.program = sqlc.arg(program)
ORDER BY a.activation_date DESC, a.created_at DESC;

-- name: UpdateActivationByUUIDAndProgram :one
UPDATE activations a
SET
    reference = UPPER(sqlc.arg(reference)::text),
    activation_date = sqlc.arg(activation_date),
    station_location_id = sqlc.narg(station_location_id),
    notes = sqlc.narg(notes),
    status = sqlc.arg(status),
    updated_at = NOW()
WHERE a.uuid = sqlc.arg(activation_uuid)
  AND a.program = sqlc.arg(program)
RETURNING
    a.id,
    a.uuid,
    a.user_id,
    a.logbook_id,
    a.program,
    a.reference,
    a.activation_date,
    a.station_location_id,
    a.notes,
    a.status,
    a.qso_count,
    a.unique_callsigns,
    a.created_at,
    a.updated_at;

-- name: GetActivationStatusByUUIDAndProgram :one
WITH act AS (
    SELECT
        a.id,
        a.uuid,
        a.logbook_id,
        a.program,
        a.reference,
        a.activation_date,
        a.status,
        l.callsign AS logbook_callsign,
        sl.callsign AS station_location_callsign,
        sl.grid_square AS station_location_grid_square
    FROM activations a
    JOIN logbooks l ON l.id = a.logbook_id
    LEFT JOIN station_locations sl ON sl.id = a.station_location_id
    WHERE a.uuid = sqlc.arg(activation_uuid)
      AND a.program = sqlc.arg(program)
)
SELECT
    act.id,
    act.uuid,
    act.program,
    act.reference,
    act.activation_date,
    act.status,
    COALESCE(stats.qso_count, 0)::bigint AS qso_count,
    COALESCE(stats.unique_callsigns, 0)::bigint AS unique_callsigns,
    COALESCE(stats.missing_station_callsign, 0)::bigint AS missing_station_callsign,
    COALESCE(stats.missing_my_gridsquare, 0)::bigint AS missing_my_gridsquare,
    COALESCE(stats.s2s_count, 0)::bigint AS s2s_count
FROM act
LEFT JOIN LATERAL (
    SELECT
        COUNT(*)::bigint AS qso_count,
        COUNT(DISTINCT UPPER(BTRIM(q.callsign)))::bigint AS unique_callsigns,
        COUNT(*) FILTER (
            WHERE NULLIF(BTRIM(COALESCE(q.station_callsign, act.logbook_callsign, act.station_location_callsign)), '') IS NULL
        )::bigint AS missing_station_callsign,
        COUNT(*) FILTER (
            WHERE NULLIF(BTRIM(COALESCE(q.my_gridsquare, act.station_location_grid_square)), '') IS NULL
        )::bigint AS missing_my_gridsquare,
        COUNT(*) FILTER (
            WHERE NULLIF(BTRIM(q.sota_ref), '') IS NOT NULL
        )::bigint AS s2s_count
    FROM qsos q
    WHERE q.deleted_at IS NULL
      AND q.logbook_id = act.logbook_id
      AND (
          q.activation_id = act.id
          OR (
              act.program = 'POTA'
              AND (q.datetime_on AT TIME ZONE 'UTC')::date = act.activation_date
              AND EXISTS (
                  SELECT 1
                  FROM unnest(COALESCE(q.my_pota_refs, ARRAY[]::text[])) AS ref
                  WHERE UPPER(BTRIM(ref)) = UPPER(BTRIM(act.reference))
              )
          )
          OR (
              act.program = 'SOTA'
              AND (q.datetime_on AT TIME ZONE 'UTC')::date = act.activation_date
              AND UPPER(BTRIM(COALESCE(q.my_sota_ref, ''))) = UPPER(BTRIM(act.reference))
          )
      )
) stats ON TRUE;

-- name: ListActivationQSOsByUUIDAndProgram :many
WITH act AS (
    SELECT
        a.id,
        a.logbook_id,
        a.program,
        a.reference,
        a.activation_date,
        l.callsign AS logbook_callsign,
        sl.callsign AS station_location_callsign,
        sl.grid_square AS station_location_grid_square
    FROM activations a
    JOIN logbooks l ON l.id = a.logbook_id
    LEFT JOIN station_locations sl ON sl.id = a.station_location_id
    WHERE a.uuid = sqlc.arg(activation_uuid)
      AND a.program = sqlc.arg(program)
)
SELECT
    q.id,
    q.uuid,
    q.callsign,
    q.band,
    q.mode,
    q.submode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.frequency_hz,
    q.comment,
    q.notes,
    q.sota_ref,
    q.my_sota_ref,
    q.sig,
    q.sig_info,
    q.pota_refs,
    q.my_pota_refs,
    COALESCE(NULLIF(BTRIM(q.station_callsign), ''), NULLIF(BTRIM(act.logbook_callsign), ''), NULLIF(BTRIM(act.station_location_callsign), ''))::text AS station_callsign_export,
    COALESCE(NULLIF(BTRIM(q.my_gridsquare), ''), NULLIF(BTRIM(act.station_location_grid_square), ''))::text AS my_gridsquare_export
FROM qsos q
CROSS JOIN act
WHERE q.deleted_at IS NULL
  AND q.logbook_id = act.logbook_id
  AND (
      q.activation_id = act.id
      OR (
          act.program = 'POTA'
          AND (q.datetime_on AT TIME ZONE 'UTC')::date = act.activation_date
          AND EXISTS (
              SELECT 1
              FROM unnest(COALESCE(q.my_pota_refs, ARRAY[]::text[])) AS ref
              WHERE UPPER(BTRIM(ref)) = UPPER(BTRIM(act.reference))
          )
      )
      OR (
          act.program = 'SOTA'
          AND (q.datetime_on AT TIME ZONE 'UTC')::date = act.activation_date
          AND UPPER(BTRIM(COALESCE(q.my_sota_ref, ''))) = UPPER(BTRIM(act.reference))
      )
  )
ORDER BY q.datetime_on ASC, q.id ASC;

-- name: GetPOTAAwardSummary :one
WITH activation_stats AS (
    SELECT
        a.id,
        UPPER(BTRIM(a.reference)) AS reference,
        COUNT(DISTINCT UPPER(BTRIM(q.callsign)))::bigint AS unique_callsigns
    FROM activations a
    LEFT JOIN qsos q ON q.deleted_at IS NULL
      AND q.logbook_id = a.logbook_id
      AND (
          q.activation_id = a.id
          OR (
              (q.datetime_on AT TIME ZONE 'UTC')::date = a.activation_date
              AND EXISTS (
                  SELECT 1
                  FROM unnest(COALESCE(q.my_pota_refs, ARRAY[]::text[])) AS ref
                  WHERE UPPER(BTRIM(ref)) = UPPER(BTRIM(a.reference))
              )
          )
      )
    WHERE a.program = 'POTA'
    GROUP BY a.id, UPPER(BTRIM(a.reference))
),
hunted_refs AS (
    SELECT DISTINCT UPPER(BTRIM(ref)) AS reference
    FROM qsos q
    CROSS JOIN LATERAL regexp_split_to_table(COALESCE(q.sig_info, ''), '\\s*,\\s*') AS ref
    WHERE q.deleted_at IS NULL
      AND UPPER(BTRIM(COALESCE(q.sig, ''))) = 'POTA'
      AND NULLIF(BTRIM(ref), '') IS NOT NULL
      AND UPPER(BTRIM(ref)) ~ '^[A-Z]{1,3}-[0-9]{1,5}$'
)
SELECT
    COUNT(DISTINCT activation_stats.reference)::bigint AS parks_activated,
    COUNT(*)::bigint AS activations_total,
    COUNT(*) FILTER (WHERE activation_stats.unique_callsigns >= 10)::bigint AS valid_activations,
    COALESCE((SELECT COUNT(*)::bigint FROM hunted_refs), 0)::bigint AS parks_hunted
FROM activation_stats;
