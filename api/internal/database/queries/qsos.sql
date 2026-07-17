-- name: CreateQSO :one
WITH target_logbook AS (
    SELECT l.id, l.uuid
    FROM logbooks l
    WHERE l.uuid = sqlc.arg(logbook_uuid)
      AND l.deleted_at IS NULL
)
INSERT INTO qsos (
    logbook_id,
    created_by_user_id,
    callsign,
    name,
    qth,
    band,
    mode,
    submode,
    frequency_hz,
    datetime_on,
    rst_sent,
    rst_rcvd,
    tx_power,
    gridsquare,
    my_gridsquare,
    dxcc,
    comment,
    notes
)
SELECT
    target_logbook.id,
    sqlc.arg(created_by_user_id),
    UPPER(sqlc.arg(callsign)::text),
    sqlc.narg(name),
    sqlc.narg(qth),
    sqlc.arg(band),
    sqlc.arg(mode),
    sqlc.narg(submode),
    sqlc.narg(frequency_hz),
    sqlc.arg(datetime_on),
    sqlc.narg(rst_sent),
    sqlc.narg(rst_rcvd),
    sqlc.narg(tx_power),
    CASE
        WHEN sqlc.narg(gridsquare)::text IS NULL THEN NULL
        ELSE UPPER(sqlc.narg(gridsquare)::text)
    END,
    CASE
        WHEN sqlc.narg(my_gridsquare)::text IS NULL THEN NULL
        ELSE UPPER(sqlc.narg(my_gridsquare)::text)
    END,
    sqlc.narg(dxcc),
    sqlc.narg(comment),
    sqlc.narg(notes)
FROM target_logbook
RETURNING
    id,
    uuid,
    callsign,
    name,
    qth,
    band,
    mode,
    submode,
    frequency_hz,
    datetime_on,
    rst_sent,
    rst_rcvd,
    tx_power,
    gridsquare,
    my_gridsquare,
    dxcc,
    comment,
    notes,
    created_at,
    updated_at;

-- name: GetQSOByUUID :one
SELECT
    q.id,
    q.uuid,
    l.uuid AS logbook_uuid,
    q.created_by_user_id,
    q.callsign,
    q.name,
    q.qth,
    q.band,
    q.mode,
    q.submode,
    q.frequency_hz,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.tx_power,
    q.gridsquare,
    q.my_gridsquare,
    q.dxcc,
    q.country,
    q.cq_zone,
    q.itu_zone,
    q.continent,
    q.comment,
    q.notes,
    q.created_at,
    q.updated_at
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE q.uuid = sqlc.arg(qso_uuid)
  AND l.uuid = sqlc.arg(logbook_uuid)
  AND l.deleted_at IS NULL
  AND q.deleted_at IS NULL;


-- name: GetQSOCreatorByUUID :one
SELECT
    q.created_by_user_id
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE q.uuid = sqlc.arg(qso_uuid)
  AND l.uuid = sqlc.arg(logbook_uuid)
  AND l.deleted_at IS NULL
  AND q.deleted_at IS NULL;

-- name: ListQSOsByLogbook :many
SELECT
    q.id,
    q.uuid,
    l.uuid AS logbook_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.name,
    q.qth,
    q.gridsquare,
    q.dxcc,
    q.country,
    q.cq_zone,
    q.itu_zone,
    q.continent,
    q.comment,
    q.notes,
    q.created_at,
    q.updated_at
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.uuid = sqlc.arg(logbook_uuid)
  AND l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND (
      sqlc.narg(cursor_datetime)::timestamptz IS NULL
      OR (q.datetime_on, q.id) < (
          sqlc.narg(cursor_datetime)::timestamptz,
          COALESCE(sqlc.narg(cursor_id)::bigint, 9223372036854775807)
      )
  )
ORDER BY q.datetime_on DESC, q.id DESC
LIMIT LEAST(sqlc.arg(page_size)::int, 100);

-- name: UpdateQSO :one
WITH target_qso AS (
    SELECT q.id
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    WHERE q.uuid = sqlc.arg(qso_uuid)
      AND l.uuid = sqlc.arg(logbook_uuid)
      AND l.deleted_at IS NULL
      AND q.deleted_at IS NULL
)
UPDATE qsos q
SET
    callsign = UPPER(sqlc.arg(callsign)::text),
    name = COALESCE(sqlc.narg(name), q.name),
    qth = COALESCE(sqlc.narg(qth), q.qth),
    band = sqlc.arg(band),
    mode = sqlc.arg(mode),
    submode = COALESCE(sqlc.narg(submode), q.submode),
    frequency_hz = COALESCE(sqlc.narg(frequency_hz), q.frequency_hz),
    datetime_on = sqlc.arg(datetime_on),
    rst_sent = COALESCE(sqlc.narg(rst_sent), q.rst_sent),
    rst_rcvd = COALESCE(sqlc.narg(rst_rcvd), q.rst_rcvd),
    tx_power = COALESCE(sqlc.narg(tx_power), q.tx_power),
    gridsquare = CASE
        WHEN sqlc.narg(gridsquare)::text IS NULL THEN q.gridsquare
        ELSE UPPER(sqlc.narg(gridsquare)::text)
    END,
    my_gridsquare = COALESCE(
        CASE
            WHEN sqlc.narg(my_gridsquare)::text IS NULL THEN NULL
            ELSE UPPER(sqlc.narg(my_gridsquare)::text)
        END,
        q.my_gridsquare
    ),
    dxcc = sqlc.narg(dxcc),
    comment = COALESCE(sqlc.narg(comment), q.comment),
    notes = COALESCE(sqlc.narg(notes), q.notes),
    updated_at = NOW()
FROM target_qso
WHERE q.id = target_qso.id
RETURNING
    q.id,
    q.uuid,
    q.callsign,
    q.name,
    q.qth,
    q.band,
    q.mode,
    q.submode,
    q.frequency_hz,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.tx_power,
    q.gridsquare,
    q.my_gridsquare,
    q.dxcc,
    q.comment,
    q.notes,
    q.created_at,
    q.updated_at;

-- name: DeleteQSO :exec
UPDATE qsos q
SET
    deleted_at = NOW(),
    updated_at = NOW()
WHERE q.uuid = sqlc.arg(qso_uuid)
  AND q.logbook_id = (
      SELECT l.id
      FROM logbooks l
      WHERE l.uuid = sqlc.arg(logbook_uuid)
        AND l.deleted_at IS NULL
  )
  AND q.deleted_at IS NULL;

-- name: SearchQSOs :many
SELECT
    q.id,
    q.uuid,
    l.uuid AS logbook_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.name,
    q.qth,
    q.gridsquare,
    q.dxcc,
    q.country,
    q.cq_zone,
    q.itu_zone,
    q.continent,
    q.comment,
    q.notes,
    q.created_at,
    q.updated_at
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.uuid = sqlc.arg(logbook_uuid)
  AND l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND (
      sqlc.narg(callsign_filter)::text IS NULL
      OR q.callsign ILIKE ('%' || sqlc.narg(callsign_filter)::text || '%')
  )
  AND (
      sqlc.narg(band_filter)::text IS NULL
      OR q.band = sqlc.narg(band_filter)::text
  )
  AND (
      sqlc.narg(mode_filter)::text IS NULL
      OR q.mode = sqlc.narg(mode_filter)::text
  )
  AND (
      sqlc.narg(date_from)::timestamptz IS NULL
      OR q.datetime_on >= sqlc.narg(date_from)::timestamptz
  )
  AND (
      sqlc.narg(date_to)::timestamptz IS NULL
      OR q.datetime_on <= sqlc.narg(date_to)::timestamptz
  )
  AND (
      sqlc.narg(dxcc_filter)::integer IS NULL
      OR q.dxcc = sqlc.narg(dxcc_filter)::integer
  )
  AND (
      sqlc.narg(gridsquare_prefix)::text IS NULL
      OR q.gridsquare ILIKE (UPPER(sqlc.narg(gridsquare_prefix)::text) || '%')
  )
  AND (
      sqlc.narg(cursor_datetime)::timestamptz IS NULL
      OR (q.datetime_on, q.id) < (
          sqlc.narg(cursor_datetime)::timestamptz,
          COALESCE(sqlc.narg(cursor_id)::bigint, 9223372036854775807)
      )
  )
ORDER BY q.datetime_on DESC, q.id DESC
LIMIT LEAST(sqlc.arg(page_size)::int, 100);

-- name: GetQSOForPSKMatch :one
-- Look up a QSO by UUID for PSK Reporter matching. Returns just the fields
-- needed to match against reception reports. RLS ensures only the owner sees it.
SELECT
    q.id,
    q.uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE q.uuid = sqlc.arg(qso_uuid)::uuid
  AND l.deleted_at IS NULL
  AND q.deleted_at IS NULL
LIMIT 1;
