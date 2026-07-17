-- name: UpsertPSKReceptionReport :exec
-- Insert a new PSK reception report, ignoring duplicates (same user, sender,
-- receiver, spotted_at). Called by the PSK Reporter poller worker.
INSERT INTO psk_reception_reports (
    user_id,
    sender_callsign,
    receiver_callsign,
    frequency_khz,
    mode,
    snr,
    grid,
    spotted_at
)
VALUES (
    sqlc.arg(user_id)::bigint,
    UPPER(sqlc.arg(sender_callsign)::text),
    UPPER(sqlc.arg(receiver_callsign)::text),
    sqlc.narg(frequency_khz)::numeric,
    sqlc.narg(mode)::text,
    sqlc.narg(snr)::smallint,
    sqlc.narg(grid)::text,
    sqlc.arg(spotted_at)::timestamptz
)
ON CONFLICT (user_id, sender_callsign, receiver_callsign, spotted_at)
DO NOTHING;

-- name: ListPSKReceptionReports :many
-- Paginated list of PSK reception reports for the authenticated user.
-- Cursor-based pagination using (spotted_at DESC, id DESC).
SELECT
    id,
    user_id,
    sender_callsign,
    receiver_callsign,
    frequency_khz,
    mode,
    snr,
    grid,
    spotted_at,
    created_at
FROM psk_reception_reports
WHERE user_id = sqlc.arg(user_id)::bigint
  AND (
      sqlc.narg(cursor_spotted_at)::timestamptz IS NULL
      OR spotted_at < sqlc.narg(cursor_spotted_at)::timestamptz
      OR (spotted_at = sqlc.narg(cursor_spotted_at)::timestamptz AND id < sqlc.narg(cursor_id)::bigint)
  )
ORDER BY spotted_at DESC, id DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: ListPSKReportsByCallsignAndWindow :many
-- Find PSK reception reports that match a specific QSO by sender callsign
-- and time window. Used by the /match/:qso_id endpoint.
SELECT
    id,
    user_id,
    sender_callsign,
    receiver_callsign,
    frequency_khz,
    mode,
    snr,
    grid,
    spotted_at,
    created_at
FROM psk_reception_reports
WHERE user_id = sqlc.arg(user_id)::bigint
  AND UPPER(sender_callsign) = UPPER(sqlc.arg(sender_callsign)::text)
  AND spotted_at >= sqlc.arg(window_start)::timestamptz
  AND spotted_at <= sqlc.arg(window_end)::timestamptz
ORDER BY spotted_at DESC, id DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: DeletePSKReportsOlderThan :execrows
-- Prune old PSK reception reports. Called periodically by the poller worker.
DELETE FROM psk_reception_reports
WHERE spotted_at < sqlc.arg(cutoff_at)::timestamptz;

-- name: ListUsersWithCallsign :many
-- Return users who have a callsign configured. Used by the PSK Reporter poller
-- to determine which callsigns to query.
SELECT
    id,
    callsign
FROM users
WHERE callsign IS NOT NULL
  AND callsign <> ''
  AND deleted_at IS NULL;
