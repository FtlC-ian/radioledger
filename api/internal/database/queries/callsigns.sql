-- Queries for user_callsigns (personal callsign history) and station_callsigns
-- (station identity, supporting M:N operator model) tables.
--
-- Schema reference: docs/SCHEMA.md § user_callsigns, station_callsigns,
--                                     station_callsign_operators

-- ─────────────────────────────────────────────────────────────────────────────
-- user_callsigns: personal callsign history (one per user, supports upgrades,
-- historical calls, and vanity changes).
-- ─────────────────────────────────────────────────────────────────────────────

-- name: CreateUserCallsign :one
-- Registers a new callsign for the authenticated user.
-- Callsigns are stored uppercase per ADIF and ham radio convention.
INSERT INTO user_callsigns (
    user_id,
    callsign,
    license_class,
    country,
    dxcc_entity,
    is_primary,
    valid_from,
    valid_to
)
VALUES (
    sqlc.arg(user_id),
    UPPER(sqlc.arg(callsign)),
    sqlc.narg(license_class),
    sqlc.narg(country),
    sqlc.narg(dxcc_entity),
    sqlc.arg(is_primary),
    sqlc.narg(valid_from),
    sqlc.narg(valid_to)
)
RETURNING
    id,
    uuid,
    user_id,
    callsign,
    license_class,
    country,
    dxcc_entity,
    is_primary,
    valid_from,
    valid_to,
    created_at;

-- name: ListUserCallsigns :many
-- Lists all callsigns for the authenticated user, primary first then newest first.
SELECT
    id,
    uuid,
    user_id,
    callsign,
    license_class,
    country,
    dxcc_entity,
    is_primary,
    valid_from,
    valid_to,
    created_at
FROM user_callsigns
WHERE user_id = sqlc.arg(user_id)
ORDER BY is_primary DESC, created_at DESC, id DESC;

-- name: GetUserCallsignByUUID :one
-- Returns a single user callsign by UUID, scoped via RLS.
SELECT
    id,
    uuid,
    user_id,
    callsign,
    license_class,
    country,
    dxcc_entity,
    is_primary,
    valid_from,
    valid_to,
    created_at
FROM user_callsigns
WHERE uuid = sqlc.arg(callsign_uuid);

-- name: UpdateUserCallsign :one
-- Updates a user callsign's metadata (primary flag, active status, validity dates).
-- Callsign text itself is immutable once created (create a new row for a new call).
UPDATE user_callsigns
SET
    is_primary    = sqlc.arg(is_primary),
    license_class = sqlc.narg(license_class),
    country       = sqlc.narg(country),
    dxcc_entity   = sqlc.narg(dxcc_entity),
    valid_from    = sqlc.narg(valid_from),
    valid_to      = sqlc.narg(valid_to)
WHERE uuid = sqlc.arg(callsign_uuid)
RETURNING
    id,
    uuid,
    user_id,
    callsign,
    license_class,
    country,
    dxcc_entity,
    is_primary,
    valid_from,
    valid_to,
    created_at;

-- name: DeleteUserCallsign :exec
-- Removes a user callsign. Hard delete is safe because callsigns have no
-- downstream FK references from QSOs (those use the text callsign column).
DELETE FROM user_callsigns
WHERE uuid = sqlc.arg(callsign_uuid);

-- ─────────────────────────────────────────────────────────────────────────────
-- station_callsigns: station identity for logging (supports M:N operator model).
-- ─────────────────────────────────────────────────────────────────────────────

-- name: CreateStationCallsign :one
-- Creates a station callsign identity entry.
INSERT INTO station_callsigns (
    user_id,
    callsign,
    callsign_type,
    description,
    valid_from,
    valid_to,
    active
)
VALUES (
    sqlc.arg(user_id),
    UPPER(sqlc.arg(callsign)),
    sqlc.arg(callsign_type),
    sqlc.narg(description),
    sqlc.narg(valid_from),
    sqlc.narg(valid_to),
    TRUE
)
RETURNING
    id,
    uuid,
    user_id,
    callsign,
    callsign_type,
    description,
    valid_from,
    valid_to,
    active,
    created_at,
    updated_at;

-- name: ListStationCallsigns :many
-- Lists all station callsigns for the authenticated user, active first.
SELECT
    id,
    uuid,
    user_id,
    callsign,
    callsign_type,
    description,
    valid_from,
    valid_to,
    active,
    created_at,
    updated_at
FROM station_callsigns
WHERE user_id = sqlc.arg(user_id)
ORDER BY active DESC, created_at DESC, id DESC;

-- name: GetStationCallsignByUUID :one
-- Returns a single station callsign by UUID, scoped via RLS.
SELECT
    id,
    uuid,
    user_id,
    callsign,
    callsign_type,
    description,
    valid_from,
    valid_to,
    active,
    created_at,
    updated_at
FROM station_callsigns
WHERE uuid = sqlc.arg(callsign_uuid);

-- name: UpdateStationCallsign :one
-- Updates a station callsign's metadata.
UPDATE station_callsigns
SET
    callsign_type = sqlc.arg(callsign_type),
    description   = sqlc.narg(description),
    valid_from    = sqlc.narg(valid_from),
    valid_to      = sqlc.narg(valid_to),
    active        = sqlc.arg(active),
    updated_at    = NOW()
WHERE uuid = sqlc.arg(callsign_uuid)
RETURNING
    id,
    uuid,
    user_id,
    callsign,
    callsign_type,
    description,
    valid_from,
    valid_to,
    active,
    created_at,
    updated_at;

-- name: DeleteStationCallsign :exec
-- Soft-deactivates a station callsign by clearing its active flag.
-- Hard-delete is not safe because QSOs may reference station_callsign_id.
UPDATE station_callsigns
SET active = FALSE, updated_at = NOW()
WHERE uuid = sqlc.arg(callsign_uuid);
