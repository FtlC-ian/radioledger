-- Queries for notifications feed.
--
-- Notification payloads are JSONB so feature-specific data can evolve without
-- schema churn, while type remains strongly constrained in the database.

-- name: CreateNotification :one
INSERT INTO notifications (
    user_id,
    type,
    payload,
    qso_id
)
VALUES (
    sqlc.arg(user_id)::bigint,
    sqlc.arg(type)::text,
    COALESCE(sqlc.arg(payload)::jsonb, '{}'::jsonb),
    sqlc.narg(qso_id)::bigint
)
RETURNING id, uuid, user_id, type, payload, qso_id, read_at, created_at;

-- name: ListNotifications :many
SELECT id, uuid, user_id, type, payload, qso_id, read_at, created_at
FROM notifications
WHERE user_id = $1
ORDER BY (read_at IS NULL) DESC, created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUnreadNotifications :one
SELECT COUNT(*)::bigint AS unread_count
FROM notifications
WHERE user_id = $1
  AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications
SET read_at = COALESCE(read_at, NOW())
WHERE uuid = $1
  AND user_id = $2;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications
SET read_at = NOW()
WHERE user_id = $1
  AND read_at IS NULL;

-- name: DeleteNotification :execrows
DELETE FROM notifications
WHERE uuid = $1
  AND user_id = $2;

-- name: HasRecentCertExpiryNotification :one
-- Returns the count of cert_expiry notifications already sent for a specific
-- (user, callsign, expiry date, threshold) combination within the past 25 days.
-- Used by CertExpiryCheckJob to avoid sending duplicate alerts.
-- Runs as radioledger_worker role (see migration 013 for the SELECT policy).
SELECT COUNT(*)::bigint
FROM notifications
WHERE user_id = sqlc.arg(user_id)
  AND type = 'cert_expiry'
  AND payload->>'callsign' = sqlc.arg(callsign)::text
  AND (payload->>'expires_at')::date = sqlc.arg(expires_at)::date
  AND payload->>'threshold_days' = sqlc.arg(threshold_days)::text
  AND created_at > NOW() - INTERVAL '25 days';

-- name: CreateWorkerNotification :one
-- Creates a notification as the worker role (no user RLS context needed).
-- Used by background jobs to create notifications for any user.
-- The caller is responsible for SET LOCAL ROLE radioledger_worker before executing.
INSERT INTO notifications (
    user_id,
    type,
    payload
)
VALUES (
    sqlc.arg(user_id)::bigint,
    sqlc.arg(type)::text,
    sqlc.arg(payload)::jsonb
)
RETURNING id, uuid, user_id, type, payload, qso_id, read_at, created_at;

-- name: ListUnreadNotifications :many
-- Returns only unread notifications for a user, newest first.
-- Used by GET /v1/notifications?unread=true.
SELECT id, uuid, user_id, type, payload, qso_id, read_at, created_at
FROM notifications
WHERE user_id = $1
  AND read_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
