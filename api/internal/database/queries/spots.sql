-- name: UpsertSpot :exec
INSERT INTO spots (
    source,
    callsign,
    reference,
    frequency_khz,
    band,
    mode,
    spotted_at,
    raw_payload,
    updated_at
)
VALUES (
    sqlc.arg(source)::text,
    UPPER(sqlc.arg(callsign)::text),
    UPPER(sqlc.arg(reference)::text),
    sqlc.narg(frequency_khz)::numeric,
    sqlc.narg(band)::text,
    sqlc.narg(mode)::text,
    sqlc.arg(spotted_at)::timestamptz,
    COALESCE(sqlc.narg(raw_payload)::jsonb, '{}'::jsonb),
    NOW()
)
ON CONFLICT (source, callsign, reference, spotted_at)
DO UPDATE SET
    frequency_khz = EXCLUDED.frequency_khz,
    band = EXCLUDED.band,
    mode = EXCLUDED.mode,
    raw_payload = EXCLUDED.raw_payload,
    updated_at = NOW();

-- name: DeleteSpotsOlderThan :execrows
DELETE FROM spots
WHERE spotted_at < sqlc.arg(cutoff_at)::timestamptz;

-- name: ListActiveSpots :many
SELECT
    id,
    source,
    callsign,
    reference,
    frequency_khz,
    band,
    mode,
    spotted_at,
    raw_payload,
    created_at,
    updated_at
FROM spots
WHERE spotted_at >= NOW() - INTERVAL '24 hours'
  AND (sqlc.narg(source_filter)::text IS NULL OR source = sqlc.narg(source_filter)::text)
  AND (sqlc.narg(band_filter)::text IS NULL OR band = sqlc.narg(band_filter)::text)
  AND (sqlc.narg(mode_filter)::text IS NULL OR mode = sqlc.narg(mode_filter)::text)
ORDER BY spotted_at DESC, id DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: ListNeededSpotsForUser :many
SELECT
    s.id,
    s.source,
    s.callsign,
    s.reference,
    s.frequency_khz,
    s.band,
    s.mode,
    s.spotted_at,
    s.raw_payload,
    s.created_at,
    s.updated_at
FROM spots s
WHERE s.spotted_at >= NOW() - INTERVAL '24 hours'
  AND (sqlc.narg(source_filter)::text IS NULL OR s.source = sqlc.narg(source_filter)::text)
  AND (sqlc.narg(band_filter)::text IS NULL OR s.band = sqlc.narg(band_filter)::text)
  AND (sqlc.narg(mode_filter)::text IS NULL OR s.mode = sqlc.narg(mode_filter)::text)
  AND NOT EXISTS (
      SELECT 1
      FROM activations a
      WHERE a.user_id = sqlc.arg(user_id)::bigint
        AND (
            (s.source = 'pota' AND a.program = 'POTA')
            OR (s.source = 'sota' AND a.program = 'SOTA')
        )
        AND UPPER(BTRIM(a.reference)) = UPPER(BTRIM(s.reference))
  )
ORDER BY s.spotted_at DESC, s.id DESC
LIMIT sqlc.arg(limit_count)::int;

-- name: ListSpotWatchRules :many
SELECT
    id,
    uuid,
    user_id,
    source,
    reference,
    mode,
    band,
    enabled,
    last_notified_at,
    created_at,
    updated_at
FROM spot_watch_rules
WHERE user_id = sqlc.arg(user_id)::bigint
ORDER BY created_at DESC;

-- name: CreateSpotWatchRule :one
INSERT INTO spot_watch_rules (
    user_id,
    source,
    reference,
    mode,
    band,
    enabled
)
VALUES (
    sqlc.arg(user_id)::bigint,
    sqlc.arg(source)::text,
    UPPER(sqlc.arg(reference)::text),
    sqlc.narg(mode)::text,
    sqlc.narg(band)::text,
    COALESCE(sqlc.narg(enabled)::boolean, TRUE)
)
RETURNING
    id,
    uuid,
    user_id,
    source,
    reference,
    mode,
    band,
    enabled,
    last_notified_at,
    created_at,
    updated_at;

-- name: UpdateSpotWatchRuleByUUID :one
UPDATE spot_watch_rules
SET
    source = COALESCE(sqlc.narg(source)::text, source),
    reference = CASE
        WHEN sqlc.narg(reference)::text IS NULL THEN reference
        ELSE UPPER(sqlc.narg(reference)::text)
    END,
    mode = sqlc.narg(mode)::text,
    band = sqlc.narg(band)::text,
    enabled = COALESCE(sqlc.narg(enabled)::boolean, enabled),
    updated_at = NOW()
WHERE uuid = sqlc.arg(rule_uuid)::uuid
  AND user_id = sqlc.arg(user_id)::bigint
RETURNING
    id,
    uuid,
    user_id,
    source,
    reference,
    mode,
    band,
    enabled,
    last_notified_at,
    created_at,
    updated_at;

-- name: DeleteSpotWatchRuleByUUID :execrows
DELETE FROM spot_watch_rules
WHERE uuid = sqlc.arg(rule_uuid)::uuid
  AND user_id = sqlc.arg(user_id)::bigint;

-- name: GetSpotNotificationPreference :one
SELECT
    id,
    user_id,
    enabled,
    cooldown_minutes,
    created_at,
    updated_at
FROM spot_notification_preferences
WHERE user_id = sqlc.arg(user_id)::bigint;

-- name: UpsertSpotNotificationPreference :one
INSERT INTO spot_notification_preferences (
    user_id,
    enabled,
    cooldown_minutes,
    updated_at
)
VALUES (
    sqlc.arg(user_id)::bigint,
    sqlc.arg(enabled)::boolean,
    sqlc.arg(cooldown_minutes)::int,
    NOW()
)
ON CONFLICT (user_id)
DO UPDATE SET
    enabled = EXCLUDED.enabled,
    cooldown_minutes = EXCLUDED.cooldown_minutes,
    updated_at = NOW()
RETURNING
    id,
    user_id,
    enabled,
    cooldown_minutes,
    created_at,
    updated_at;

-- name: ListMatchingSpotWatchRules :many
SELECT
    r.id,
    r.uuid,
    r.user_id,
    r.source,
    r.reference,
    r.mode,
    r.band,
    r.enabled,
    r.last_notified_at,
    r.created_at,
    r.updated_at,
    COALESCE(p.enabled, TRUE) AS notifications_enabled,
    COALESCE(p.cooldown_minutes, 30)::int AS cooldown_minutes
FROM spot_watch_rules r
LEFT JOIN spot_notification_preferences p ON p.user_id = r.user_id
WHERE r.enabled = TRUE
  AND r.source = sqlc.arg(source)::text
  AND UPPER(BTRIM(r.reference)) = UPPER(BTRIM(sqlc.arg(reference)::text))
  AND (
      sqlc.narg(mode)::text IS NULL
      OR r.mode IS NULL
      OR UPPER(BTRIM(r.mode)) = UPPER(BTRIM(sqlc.narg(mode)::text))
  )
  AND (
      sqlc.narg(band)::text IS NULL
      OR r.band IS NULL
      OR UPPER(BTRIM(r.band)) = UPPER(BTRIM(sqlc.narg(band)::text))
  );

-- name: UpdateSpotWatchRuleLastNotified :exec
UPDATE spot_watch_rules
SET last_notified_at = sqlc.arg(last_notified_at)::timestamptz,
    updated_at = NOW()
WHERE id = sqlc.arg(rule_id)::bigint;
