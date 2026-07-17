-- Representative tenant data from the reviewed pre-Goose staging family.
-- Apply after bootstrap records version 1 and before Goose migration 002.
INSERT INTO bands (name, lower_freq, upper_freq, band_group, warc, is_common, sort_order)
VALUES ('2200m', 0.1357, 0.1378, 'LF', FALSE, FALSE, 99);

INSERT INTO users (email, preferences)
VALUES ('migration-002@example.invalid', '{"default_band":"2200M"}'::jsonb);

INSERT INTO logbooks (user_id, name, is_default)
SELECT id, 'Migration 002 fixture', TRUE
FROM users WHERE email = 'migration-002@example.invalid';

INSERT INTO qsos (logbook_id, callsign, band, mode, datetime_on)
SELECT l.id, 'W5TEST', '2200m', 'CW', NOW()
FROM logbooks l JOIN users u ON u.id = l.user_id
WHERE u.email = 'migration-002@example.invalid';
INSERT INTO qsos (logbook_id, callsign, band, mode, datetime_on)
SELECT l.id, 'W5CANON', '2190m', 'CW', NOW()
FROM logbooks l JOIN users u ON u.id = l.user_id
WHERE u.email = 'migration-002@example.invalid';

INSERT INTO qso_confirmations (
    qso_id, our_callsign, their_callsign, band, mode, qso_date, qso_time
)
SELECT id, 'K5TEST', 'W5TEST', '2200m', 'CW', CURRENT_DATE, CURRENT_TIME
FROM qsos WHERE callsign = 'W5TEST';

INSERT INTO award_progress (user_id, award_type, entity_key, band, mode)
SELECT id, 'dxcc', '291', '2200m', NULL
FROM users WHERE email = 'migration-002@example.invalid';
INSERT INTO award_progress (user_id, award_type, entity_key, band, mode)
SELECT id, 'dxcc', '291', '2190m', NULL
FROM users WHERE email = 'migration-002@example.invalid';

INSERT INTO award_tracking (user_id, award_type, award_config)
SELECT id, 'dxcc', '{"band":"2200m"}'::jsonb
FROM users WHERE email = 'migration-002@example.invalid';

INSERT INTO contests (contest_code, name, cabrillo_name)
VALUES ('MIG002', 'Migration 002 fixture', 'MIG002');
INSERT INTO contest_sessions (
    user_id, logbook_id, contest_id, name, category_operator, category_band
)
SELECT u.id, l.id, c.id, 'Migration 002 fixture', 'SINGLE-OP', '2200m'
FROM users u
JOIN logbooks l ON l.user_id = u.id
CROSS JOIN contests c
WHERE u.email = 'migration-002@example.invalid' AND c.contest_code = 'MIG002';

-- NULL and empty mode are equal in this table's COALESCE-based unique index.
INSERT INTO contest_multipliers (
    contest_session_id, multiplier_type, multiplier_key, band, mode,
    first_qso_id, worked_at
)
SELECT s.id, 'state', 'AR', '2200m', '', q.id, '2026-01-01T00:00:00Z'
FROM contest_sessions s CROSS JOIN qsos q
WHERE s.name = 'Migration 002 fixture' AND q.callsign = 'W5TEST';
INSERT INTO contest_multipliers (
    contest_session_id, multiplier_type, multiplier_key, band, mode,
    first_qso_id, worked_at
)
SELECT s.id, 'state', 'AR', '2190m', NULL, q.id, '2026-02-01T00:00:00Z'
FROM contest_sessions s CROSS JOIN qsos q
WHERE s.name = 'Migration 002 fixture' AND q.callsign = 'W5CANON';

INSERT INTO spot_watch_rules (user_id, source, reference, mode, band)
SELECT id, 'pota', 'K-1234', NULL, '2200m'
FROM users WHERE email = 'migration-002@example.invalid';
INSERT INTO spot_watch_rules (user_id, source, reference, mode, band)
SELECT id, 'pota', 'K-1234', '', '2190m'
FROM users WHERE email = 'migration-002@example.invalid';

INSERT INTO spots (source, callsign, reference, band, mode, spotted_at)
VALUES ('pota', 'W5TEST', 'K-1234', '2200m', 'CW', NOW());
