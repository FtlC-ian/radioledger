-- name: CreateContestSession :one
INSERT INTO contest_sessions (
    user_id,
    logbook_id,
    contest_id,
    station_callsign_id,
    name,
    starts_at,
    ends_at,
    category_operator,
    category_assisted,
    category_band,
    category_mode,
    category_power,
    category_station,
    category_time,
    category_transmitter,
    category_overlay,
    operators_line,
    club_name,
    location,
    soapbox,
    exchange_sent,
    exchange_template,
    status,
    cabrillo_version
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(logbook_id),
    sqlc.arg(contest_id),
    sqlc.narg(station_callsign_id),
    sqlc.arg(name),
    sqlc.narg(starts_at),
    sqlc.narg(ends_at),
    sqlc.arg(category_operator),
    sqlc.arg(category_assisted),
    sqlc.arg(category_band),
    sqlc.arg(category_mode),
    sqlc.arg(category_power),
    sqlc.arg(category_station),
    sqlc.arg(category_time),
    sqlc.arg(category_transmitter),
    sqlc.narg(category_overlay),
    sqlc.narg(operators_line),
    sqlc.narg(club_name),
    sqlc.narg(location),
    sqlc.narg(soapbox),
    sqlc.narg(exchange_sent),
    sqlc.arg(exchange_template),
    sqlc.arg(status),
    sqlc.arg(cabrillo_version)
)
RETURNING
    id, uuid, user_id, logbook_id, contest_id, name,
    starts_at, ends_at, category_operator, category_assisted, category_band,
    category_mode, category_power, category_station, category_time,
    category_transmitter, category_overlay, operators_line, club_name,
    location, soapbox, exchange_sent, exchange_template,
    serial_counter, status, cabrillo_version, created_at, updated_at;

-- name: GetContestSessionByUUID :one
SELECT
    cs.id,
    cs.uuid,
    cs.user_id,
    cs.logbook_id,
    l.uuid AS logbook_uuid,
    cs.contest_id,
    c.contest_code,
    c.name AS contest_name,
    c.cabrillo_name,
    sc.callsign AS my_callsign,
    cs.name,
    cs.starts_at,
    cs.ends_at,
    cs.category_operator,
    cs.category_assisted,
    cs.category_band,
    cs.category_mode,
    cs.category_power,
    cs.category_station,
    cs.category_time,
    cs.category_transmitter,
    cs.category_overlay,
    cs.operators_line,
    cs.club_name,
    cs.location,
    cs.soapbox,
    cs.claimed_score,
    cs.exchange_sent,
    cs.exchange_template,
    cs.serial_counter,
    cs.status,
    cs.cabrillo_version,
    cs.created_at,
    cs.updated_at
FROM contest_sessions cs
JOIN logbooks l ON l.id = cs.logbook_id
JOIN contests c ON c.id = cs.contest_id
LEFT JOIN station_callsigns sc ON sc.id = cs.station_callsign_id
WHERE cs.uuid = sqlc.arg(session_uuid);

-- name: ListContestSessions :many
SELECT
    cs.id,
    cs.uuid,
    cs.logbook_id,
    l.uuid AS logbook_uuid,
    cs.contest_id,
    c.contest_code,
    c.name AS contest_name,
    sc.callsign AS my_callsign,
    cs.name,
    cs.starts_at,
    cs.ends_at,
    cs.category_operator,
    cs.category_band,
    cs.category_mode,
    cs.category_power,
    cs.exchange_template,
    cs.serial_counter,
    cs.status,
    cs.created_at
FROM contest_sessions cs
JOIN logbooks l ON l.id = cs.logbook_id
JOIN contests c ON c.id = cs.contest_id
LEFT JOIN station_callsigns sc ON sc.id = cs.station_callsign_id
WHERE cs.user_id = sqlc.arg(user_id)
ORDER BY cs.created_at DESC;

-- name: UpdateContestSession :one
UPDATE contest_sessions
SET
    name                 = sqlc.arg(name),
    starts_at            = sqlc.narg(starts_at),
    ends_at              = sqlc.narg(ends_at),
    category_operator    = sqlc.arg(category_operator),
    category_assisted    = sqlc.arg(category_assisted),
    category_band        = sqlc.arg(category_band),
    category_mode        = sqlc.arg(category_mode),
    category_power       = sqlc.arg(category_power),
    category_station     = sqlc.arg(category_station),
    category_time        = sqlc.arg(category_time),
    category_transmitter = sqlc.arg(category_transmitter),
    category_overlay     = sqlc.narg(category_overlay),
    operators_line       = sqlc.narg(operators_line),
    club_name            = sqlc.narg(club_name),
    location             = sqlc.narg(location),
    soapbox              = sqlc.narg(soapbox),
    exchange_sent        = sqlc.narg(exchange_sent),
    exchange_template    = sqlc.arg(exchange_template),
    status               = sqlc.arg(status),
    updated_at           = NOW()
WHERE uuid = sqlc.arg(session_uuid)
RETURNING
    id, uuid, user_id, logbook_id, contest_id, name,
    starts_at, ends_at, category_operator, category_assisted, category_band,
    category_mode, category_power, category_station, category_time,
    category_transmitter, category_overlay, operators_line, club_name,
    location, soapbox, exchange_sent, exchange_template,
    serial_counter, status, cabrillo_version, created_at, updated_at;

-- name: IncrementSerialCounter :one
-- Atomically increment serial_counter and return the new value.
UPDATE contest_sessions
SET
    serial_counter = serial_counter + 1,
    updated_at     = NOW()
WHERE uuid = sqlc.arg(session_uuid)
RETURNING serial_counter;

-- name: CheckDupe :one
-- Returns the first non-dupe QSO for callsign+band in this contest session.
-- Uses idx_qsos_contest_dupe for <10ms response.
SELECT
    q.uuid                AS qso_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.datetime_on,
    cqe.sent_serial,
    cqe.recv_exchange,
    cqe.is_dupe
FROM contest_sessions sess
JOIN qsos q
    ON q.contest_session_id = sess.id
   AND UPPER(q.callsign) = UPPER(sqlc.arg(callsign)::text)
   AND q.band = sqlc.arg(band)
   AND q.deleted_at IS NULL
JOIN contest_qso_exchange cqe
    ON cqe.qso_id = q.id
   AND NOT cqe.is_dupe
WHERE sess.uuid = sqlc.arg(session_uuid)
ORDER BY q.datetime_on ASC
LIMIT 1;

-- name: InsertContestQSO :one
-- Insert a QSO linked to a contest session.
INSERT INTO qsos (
    logbook_id,
    created_by_user_id,
    callsign,
    band,
    mode,
    datetime_on,
    rst_sent,
    rst_rcvd,
    frequency_hz,
    contest_session_id,
    contest_id
)
SELECT
    cs.logbook_id,
    sqlc.arg(created_by_user_id)::bigint,
    UPPER(BTRIM(sqlc.arg(callsign)::text)),
    sqlc.arg(band)::text,
    sqlc.arg(mode)::text,
    sqlc.arg(datetime_on)::timestamptz,
    sqlc.narg(rst_sent)::text,
    sqlc.narg(rst_rcvd)::text,
    sqlc.narg(frequency_hz)::bigint,
    cs.id,
    c.contest_code
FROM contest_sessions cs
JOIN contests c ON c.id = cs.contest_id
WHERE cs.uuid = sqlc.arg(session_uuid)
RETURNING id, uuid, callsign, band, mode, datetime_on, rst_sent, rst_rcvd, frequency_hz, contest_session_id;

-- name: InsertContestQSOExchange :one
INSERT INTO contest_qso_exchange (
    qso_id,
    contest_session_id,
    sent_serial,
    recv_serial,
    sent_exchange,
    recv_exchange,
    is_dupe
)
SELECT
    sqlc.arg(qso_id)::bigint,
    cs.id,
    sqlc.narg(sent_serial)::integer,
    sqlc.narg(recv_serial)::integer,
    sqlc.narg(sent_exchange)::text,
    sqlc.narg(recv_exchange)::text,
    sqlc.arg(is_dupe)::boolean
FROM contest_sessions cs
WHERE cs.uuid = sqlc.arg(session_uuid)
RETURNING qso_id, contest_session_id, sent_serial, recv_serial, sent_exchange, recv_exchange, is_dupe, created_at;

-- name: GetContestStats :one
-- Live contest statistics for the session detail view.
SELECT
    COALESCE(COUNT(q.id) FILTER (WHERE NOT cqe.is_dupe), 0)::bigint        AS total_qsos,
    COALESCE(COUNT(q.id) FILTER (WHERE cqe.is_dupe), 0)::bigint             AS dupe_qsos,
    COALESCE(COUNT(DISTINCT UPPER(q.callsign)) FILTER (WHERE NOT cqe.is_dupe), 0)::bigint AS unique_callsigns,
    COALESCE(COUNT(q.id) FILTER (WHERE NOT cqe.is_dupe AND q.datetime_on >= NOW() - INTERVAL '60 minutes'), 0)::bigint AS rate_last_60min,
    COALESCE(COUNT(q.id) FILTER (WHERE NOT cqe.is_dupe AND q.datetime_on >= NOW() - INTERVAL '10 minutes'), 0)::bigint AS rate_last_10min,
    COALESCE(COUNT(q.id) FILTER (WHERE NOT cqe.is_dupe AND q.datetime_on >= NOW() - INTERVAL '1 minute'), 0)::bigint  AS rate_last_1min,
    MIN(q.datetime_on)                                                       AS first_qso_at,
    MAX(q.datetime_on)                                                       AS last_qso_at,
    cs.serial_counter
FROM contest_sessions cs
LEFT JOIN qsos q
    ON q.contest_session_id = cs.id
   AND q.deleted_at IS NULL
LEFT JOIN contest_qso_exchange cqe ON cqe.qso_id = q.id
WHERE cs.uuid = sqlc.arg(session_uuid)
GROUP BY cs.serial_counter;

-- name: GetContestQSOsForExport :many
-- All QSOs for a contest session (including dupes), ordered by time.
SELECT
    q.uuid          AS qso_uuid,
    q.callsign,
    q.band,
    q.mode,
    q.submode,
    q.datetime_on,
    q.rst_sent,
    q.rst_rcvd,
    q.frequency_hz,
    cqe.sent_serial,
    cqe.recv_serial,
    cqe.sent_exchange,
    cqe.recv_exchange,
    cqe.is_dupe,
    cqe.cabrillo_qso_line
FROM contest_sessions cs
JOIN qsos q
    ON q.contest_session_id = cs.id
   AND q.deleted_at IS NULL
JOIN contest_qso_exchange cqe ON cqe.qso_id = q.id
WHERE cs.uuid = sqlc.arg(session_uuid)
ORDER BY q.datetime_on ASC, q.id ASC;

-- name: GetContestForExport :one
-- Fetch all header fields needed to generate a Cabrillo export.
SELECT
    cs.uuid,
    cs.name,
    c.cabrillo_name       AS contest_cabrillo_name,
    c.contest_code,
    sc.callsign           AS my_callsign,
    cs.category_operator,
    cs.category_assisted,
    cs.category_band,
    cs.category_mode,
    cs.category_power,
    cs.category_station,
    cs.category_time,
    cs.category_transmitter,
    cs.category_overlay,
    cs.operators_line,
    cs.club_name,
    cs.location,
    cs.soapbox,
    cs.claimed_score,
    cs.exchange_sent,
    cs.cabrillo_version,
    cs.starts_at,
    cs.ends_at
FROM contest_sessions cs
JOIN contests c ON c.id = cs.contest_id
LEFT JOIN station_callsigns sc ON sc.id = cs.station_callsign_id
WHERE cs.uuid = sqlc.arg(session_uuid);

-- name: ListContests :many
SELECT id, contest_code, name, sponsor, cabrillo_name, exchange_schema, active
FROM contests
WHERE active = TRUE
ORDER BY name ASC;

-- name: UpsertContest :one
-- Auto-provision a contest record by ADIF contest code.
-- Inserts if not present; returns existing record if already there.
INSERT INTO contests (contest_code, name, cabrillo_name, exchange_schema)
VALUES (
    UPPER(BTRIM(sqlc.arg(contest_code)::text)),
    sqlc.arg(name)::text,
    UPPER(BTRIM(sqlc.arg(contest_code)::text)),
    '{}'::jsonb
)
ON CONFLICT (contest_code) DO UPDATE
    SET name = EXCLUDED.name
RETURNING id, contest_code, name, cabrillo_name, exchange_schema, active;
