-- Queries for the callsign_cache table.
--
-- callsign_cache stores the results of QRZ/HamDB lookups so that we do not
-- hammer external callbook APIs during ADIF imports or rapid QSO entry.
-- A 100k-QSO import without this = 100k API calls; QRZ rate-limits aggressively.
--
-- Schema reference: docs/SCHEMA.md § callsign_cache
-- Cache TTL: 30 days (callsign data rarely changes; license class and grid are stable)
--
-- This table is NOT tenant-scoped (no user_id, no RLS). All users share the same
-- cache of public callbook data. The data is public information from QRZ/HamDB.

-- name: UpsertCallsignCache :one
-- Insert or update a callsign cache entry.
-- On conflict (same callsign), overwrites with fresh data and resets expiry.
INSERT INTO callsign_cache (callsign, data, source, fetched_at, expires_at)
VALUES (
    UPPER(sqlc.arg(callsign)),
    sqlc.arg(data),
    sqlc.arg(source),
    NOW(),
    sqlc.arg(expires_at)
)
ON CONFLICT (callsign) DO UPDATE
    SET data       = EXCLUDED.data,
        source     = EXCLUDED.source,
        fetched_at = NOW(),
        expires_at = EXCLUDED.expires_at
RETURNING callsign, data, source, fetched_at, expires_at;

-- name: GetCallsignCache :one
-- Returns a cached callsign record if it exists and has not expired.
-- Returns no rows if not found or expired.
SELECT callsign, data, source, fetched_at, expires_at
FROM callsign_cache
WHERE callsign = UPPER(sqlc.arg(callsign))
  AND expires_at > NOW();

-- name: AutocompleteCallsigns :many
-- Prefix search over cached callsigns for typeahead/autocomplete.
-- Returns the top 10 matches by callsign for the given prefix.
-- Only considers non-expired entries. Used by the QSO entry form.
SELECT
    callsign,
    data->>'full_name' AS full_name,
    data->>'grid'      AS grid
FROM callsign_cache
WHERE callsign LIKE UPPER(sqlc.arg(prefix)) || '%'
  AND expires_at > NOW()
ORDER BY callsign
LIMIT 10;

-- name: DeleteExpiredCallsignCache :exec
-- Removes all expired entries. Run periodically (e.g. daily background job).
DELETE FROM callsign_cache
WHERE expires_at <= NOW();
