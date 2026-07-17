-- name: GetStatsSummary :one
SELECT
    COUNT(*)::bigint AS total_qsos,
    COUNT(DISTINCT q.callsign)::bigint AS unique_callsigns,
    COUNT(DISTINCT COALESCE(NULLIF(BTRIM(q.country), ''), d.name)) FILTER (
        WHERE COALESCE(NULLIF(BTRIM(q.country), ''), d.name) IS NOT NULL
    )::bigint AS unique_countries,
    COUNT(DISTINCT NULLIF(BTRIM(q.gridsquare), ''))::bigint AS unique_grids,
    MIN(q.datetime_on)::timestamptz AS first_qso,
    MAX(q.datetime_on)::timestamptz AS last_qso
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
LEFT JOIN dxcc_entities d ON d.entity_id = q.dxcc
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL;

-- name: GetStatsBands :many
SELECT
    q.band,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY q.band
ORDER BY count DESC, q.band ASC;

-- name: GetStatsModes :many
SELECT
    q.mode,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY q.mode
ORDER BY count DESC, q.mode ASC;

-- name: GetStatsTopCountries :many
SELECT
    COALESCE(NULLIF(BTRIM(q.country), ''), d.name) AS name,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
LEFT JOIN dxcc_entities d ON d.entity_id = q.dxcc
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND COALESCE(NULLIF(BTRIM(q.country), ''), d.name) IS NOT NULL
GROUP BY COALESCE(NULLIF(BTRIM(q.country), ''), d.name)
ORDER BY count DESC, COALESCE(NULLIF(BTRIM(q.country), ''), d.name) ASC
LIMIT 10;

-- name: GetStatsByYear :many
SELECT
    EXTRACT(YEAR FROM q.datetime_on AT TIME ZONE 'UTC')::int AS year,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY year
ORDER BY year ASC;

-- name: GetStatsByMonth :many
SELECT
    TO_CHAR(q.datetime_on AT TIME ZONE 'UTC', 'YYYY-MM') AS period,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY period
ORDER BY period ASC;

-- name: GetStatsTopCallsigns :many
SELECT
    q.callsign,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY q.callsign
ORDER BY count DESC, q.callsign ASC
LIMIT sqlc.arg(lim)::int;

-- name: GetStatsTopCountriesLimited :many
SELECT
    COALESCE(NULLIF(BTRIM(q.country), ''), d.name) AS name,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
LEFT JOIN dxcc_entities d ON d.entity_id = q.dxcc
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
  AND COALESCE(NULLIF(BTRIM(q.country), ''), d.name) IS NOT NULL
GROUP BY COALESCE(NULLIF(BTRIM(q.country), ''), d.name)
ORDER BY count DESC, COALESCE(NULLIF(BTRIM(q.country), ''), d.name) ASC
LIMIT sqlc.arg(lim)::int;

-- name: GetStatsOperatingPatterns :many
SELECT
    EXTRACT(DOW FROM q.datetime_on AT TIME ZONE 'UTC')::int AS day_of_week,
    EXTRACT(HOUR FROM q.datetime_on AT TIME ZONE 'UTC')::int AS hour_of_day,
    COUNT(*)::bigint AS count
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL
GROUP BY day_of_week, hour_of_day
ORDER BY day_of_week ASC, hour_of_day ASC;

-- name: GetStatsCountriesOverTime :many
WITH first_seen AS (
    SELECT
        COALESCE(NULLIF(BTRIM(q.country), ''), d.name) AS country_name,
        TO_CHAR(MIN(q.datetime_on AT TIME ZONE 'UTC'), 'YYYY-MM') AS first_period
    FROM qsos q
    JOIN logbooks l ON l.id = q.logbook_id
    LEFT JOIN dxcc_entities d ON d.entity_id = q.dxcc
    WHERE l.deleted_at IS NULL
      AND q.deleted_at IS NULL
      AND COALESCE(NULLIF(BTRIM(q.country), ''), d.name) IS NOT NULL
    GROUP BY COALESCE(NULLIF(BTRIM(q.country), ''), d.name)
), monthly_new AS (
    SELECT
        first_period AS period,
        COUNT(*)::bigint AS new_countries
    FROM first_seen
    GROUP BY first_period
)
SELECT
    period,
    SUM(new_countries) OVER (ORDER BY period ASC)::bigint AS unique_countries
FROM monthly_new
ORDER BY period ASC;

-- name: GetStatsOverview :one
SELECT
    COUNT(*)::bigint AS total_qsos,
    COUNT(DISTINCT q.callsign)::bigint AS unique_callsigns,
    COUNT(DISTINCT COALESCE(NULLIF(BTRIM(q.country), ''), d.name)) FILTER (
        WHERE COALESCE(NULLIF(BTRIM(q.country), ''), d.name) IS NOT NULL
    )::bigint AS unique_countries,
    COUNT(DISTINCT NULLIF(BTRIM(q.state), ''))::bigint AS unique_states,
    COUNT(DISTINCT NULLIF(BTRIM(q.gridsquare), ''))::bigint AS unique_grids,
    COUNT(DISTINCT q.band)::bigint AS bands_used,
    COUNT(DISTINCT q.mode)::bigint AS modes_used,
    MIN(q.datetime_on)::timestamptz AS first_qso,
    MAX(q.datetime_on)::timestamptz AS last_qso
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
LEFT JOIN dxcc_entities d ON d.entity_id = q.dxcc
WHERE l.deleted_at IS NULL
  AND q.deleted_at IS NULL;
