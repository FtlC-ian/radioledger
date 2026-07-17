-- Queries for the sync_status table and sync-related operations.
-- Schema reference: docs/SCHEMA.md § sync_status
-- These queries are used by sync workers (radioledger_worker role) and the sync status API.

-- name: GetPendingSyncQSOs :many
-- Fetch QSOs for a user+service that are pending/dirty or errored and eligible for retry.
-- Used by the sync upload workers to find work. Runs as radioledger_worker role.
SELECT
    ss.id             AS sync_id,
    ss.qso_id,
    ss.retry_count,
    q.uuid            AS qso_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.gridsquare,
    q.my_gridsquare,
    q.name,
    q.station_callsign,
    q.frequency_hz,
    lb.user_id
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE ss.service = $1
  AND lb.user_id = $2
  AND ss.status IN ('pending', 'dirty', 'error')
  AND (ss.next_retry_at IS NULL OR ss.next_retry_at <= NOW())
  AND q.deleted_at IS NULL
ORDER BY ss.created_at ASC
LIMIT $3;

-- name: GetSyncStatusSummary :many
-- Aggregate sync status per service for a user. Used by GET /v1/sync/status.
SELECT
    ss.service,
    COUNT(*) FILTER (WHERE ss.status IN ('pending', 'dirty', 'error')) AS pending_count,
    COUNT(*) FILTER (WHERE ss.status = 'error')               AS error_count,
    MAX(ss.last_synced_at)                                    AS last_sync_at,
    MAX(ss.updated_at)                                        AS last_updated_at
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE lb.user_id = $1
GROUP BY ss.service;

-- name: GetRecentSyncHistory :many
-- Recent sync activity for a user, newest first. Used by GET /v1/sync/history.
SELECT
    ss.id,
    ss.service,
    ss.status,
    ss.last_synced_at,
    ss.error_message,
    ss.last_error_code,
    ss.retry_count,
    q.uuid   AS qso_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on
FROM sync_status ss
JOIN qsos q ON q.id = ss.qso_id
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE lb.user_id = $1
  AND ss.last_synced_at IS NOT NULL
ORDER BY ss.last_synced_at DESC
LIMIT $2;

-- name: InsertPendingSyncForQSO :exec
-- Insert a pending sync_status row for a single (qso_id, service) combination.
-- ON CONFLICT DO NOTHING: if a pending row already exists, leave it alone.
-- This is called by the QSO handler after create/update to enqueue sync work.
INSERT INTO sync_status (qso_id, service, status, updated_at)
VALUES ($1, $2, 'pending', NOW())
ON CONFLICT (qso_id, service) DO NOTHING;

-- name: FindQSOForEQSLMatch :one
-- Look up a QSO by the composite key used for eQSL matching:
-- their callsign, band, mode, and time window (±15 minutes).
-- eQSL timestamps may be off by a few minutes — use a generous window.
SELECT
    q.id,
    q.uuid,
    q.logbook_id,
    lb.user_id
FROM qsos q
JOIN logbooks lb ON lb.id = q.logbook_id
WHERE upper(q.callsign) = upper($1)
  AND q.band = $2
  AND q.mode = $3
  AND q.datetime_on BETWEEN ($4::timestamptz - INTERVAL '15 minutes')
                        AND ($4::timestamptz + INTERVAL '15 minutes')
  AND q.deleted_at IS NULL
LIMIT 1;
