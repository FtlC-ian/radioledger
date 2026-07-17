-- Queries for station_locations table.
--
-- station_locations are the tQSL "station locations" required for LoTW integration.
-- One callsign can have many locations (Home, POTA Portable, DXpedition), each with
-- its own tQSL certificate. RLS isolates each user's locations.
--
-- Schema reference: docs/SCHEMA.md § station_locations

-- name: CreateStationLocation :one
-- Creates a new station location for the authenticated user.
-- The PostGIS location point is computed from lat/lon when both are provided,
-- falling back to the maidenhead_to_point() function on the grid_square.
INSERT INTO station_locations (
    user_id,
    name,
    callsign,
    grid_square,
    latitude,
    longitude,
    location,
    dxcc_entity,
    state,
    county,
    city,
    country,
    lotw_location_name,
    lotw_cert_expiry,
    is_default
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(name),
    UPPER(sqlc.arg(callsign)),
    UPPER(sqlc.arg(grid_square)),
    sqlc.narg(latitude),
    sqlc.narg(longitude),
    CASE
        WHEN sqlc.narg(latitude)::numeric IS NOT NULL
         AND sqlc.narg(longitude)::numeric IS NOT NULL
        THEN ST_SetSRID(
                ST_MakePoint(
                    sqlc.narg(longitude)::float8,
                    sqlc.narg(latitude)::float8
                ), 4326)
        ELSE maidenhead_to_point(sqlc.arg(grid_square))
    END,
    sqlc.narg(dxcc_entity),
    sqlc.narg(state),
    sqlc.narg(county),
    sqlc.narg(city),
    sqlc.narg(country),
    sqlc.narg(lotw_location_name),
    sqlc.narg(lotw_cert_expiry),
    sqlc.arg(is_default)
)
RETURNING
    id,
    uuid,
    user_id,
    name,
    callsign,
    grid_square,
    latitude,
    longitude,
    dxcc_entity,
    state,
    county,
    city,
    country,
    lotw_location_name,
    lotw_cert_expiry,
    is_default,
    created_at,
    updated_at;

-- name: GetStationLocationByUUID :one
-- Returns a station location by UUID, scoped to the authenticated user via RLS.
SELECT
    id,
    uuid,
    user_id,
    name,
    callsign,
    grid_square,
    latitude,
    longitude,
    dxcc_entity,
    state,
    county,
    city,
    country,
    lotw_location_name,
    lotw_cert_expiry,
    is_default,
    created_at,
    updated_at
FROM station_locations
WHERE uuid = sqlc.arg(location_uuid)
  AND deleted_at IS NULL;

-- name: ListStationLocations :many
-- Lists all active station locations for the authenticated user, newest first.
SELECT
    id,
    uuid,
    user_id,
    name,
    callsign,
    grid_square,
    latitude,
    longitude,
    dxcc_entity,
    state,
    county,
    city,
    country,
    lotw_location_name,
    lotw_cert_expiry,
    is_default,
    created_at,
    updated_at
FROM station_locations
WHERE user_id = sqlc.arg(user_id)
  AND deleted_at IS NULL
ORDER BY is_default DESC, created_at DESC, id DESC;

-- name: UpdateStationLocation :one
-- Updates a station location by UUID. Recomputes the PostGIS point when coordinates change.
UPDATE station_locations
SET
    name               = sqlc.arg(name),
    callsign           = UPPER(sqlc.arg(callsign)),
    grid_square        = UPPER(sqlc.arg(grid_square)),
    latitude           = sqlc.narg(latitude),
    longitude          = sqlc.narg(longitude),
    location           = CASE
                             WHEN sqlc.narg(latitude)::numeric IS NOT NULL
                              AND sqlc.narg(longitude)::numeric IS NOT NULL
                             THEN ST_SetSRID(
                                     ST_MakePoint(
                                         sqlc.narg(longitude)::float8,
                                         sqlc.narg(latitude)::float8
                                     ), 4326)
                             ELSE maidenhead_to_point(sqlc.arg(grid_square))
                         END,
    dxcc_entity        = sqlc.narg(dxcc_entity),
    state              = sqlc.narg(state),
    county             = sqlc.narg(county),
    city               = sqlc.narg(city),
    country            = sqlc.narg(country),
    lotw_location_name = sqlc.narg(lotw_location_name),
    lotw_cert_expiry   = sqlc.narg(lotw_cert_expiry),
    is_default         = sqlc.arg(is_default),
    updated_at         = NOW()
WHERE uuid = sqlc.arg(location_uuid)
  AND deleted_at IS NULL
RETURNING
    id,
    uuid,
    user_id,
    name,
    callsign,
    grid_square,
    latitude,
    longitude,
    dxcc_entity,
    state,
    county,
    city,
    country,
    lotw_location_name,
    lotw_cert_expiry,
    is_default,
    created_at,
    updated_at;

-- name: DeleteStationLocation :exec
-- Soft-deletes a station location. References from logbooks use ON DELETE SET NULL
-- so logbooks are not affected.
UPDATE station_locations
SET deleted_at = NOW(), updated_at = NOW()
WHERE uuid = sqlc.arg(location_uuid)
  AND deleted_at IS NULL;

-- name: UpdateCertExpiryByCallsign :execrows
-- Updates lotw_cert_expiry on all active station locations matching the callsign
-- for the authenticated user. Called by POST /v1/desktop/cert-expiry.
-- Returns the number of rows updated (0 = no matching location found).
UPDATE station_locations
SET
    lotw_cert_expiry = sqlc.arg(lotw_cert_expiry),
    updated_at       = NOW()
WHERE user_id       = sqlc.arg(user_id)
  AND callsign      = UPPER(sqlc.arg(callsign))
  AND deleted_at    IS NULL;

-- name: ListAllExpiringCerts :many
-- Worker query: returns all station locations with a non-null lotw_cert_expiry.
-- Used by CertExpiryCheckJob to find certificates approaching expiry.
-- Runs as radioledger_worker role (see migration 013 for the SELECT policy).
-- No user filter — intentionally queries across all users.
SELECT
    id,
    user_id,
    callsign,
    name,
    lotw_location_name,
    lotw_cert_expiry
FROM station_locations
WHERE lotw_cert_expiry IS NOT NULL
  AND deleted_at IS NULL
ORDER BY lotw_cert_expiry ASC;
