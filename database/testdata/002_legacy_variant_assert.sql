DO $assert$
BEGIN
    IF EXISTS (SELECT 1 FROM qsos WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM qso_confirmations WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM award_progress WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM contest_multipliers WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM spot_watch_rules WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM contest_sessions WHERE category_band = '2200m')
       OR EXISTS (SELECT 1 FROM spots WHERE band = '2200m')
       OR EXISTS (SELECT 1 FROM award_tracking WHERE award_config->>'band' = '2200m')
       OR EXISTS (SELECT 1 FROM users WHERE upper(preferences->>'default_band') = '2200M') THEN
        RAISE EXCEPTION 'migration 002 left a legacy 2200m value';
    END IF;

    IF (SELECT count(*) FROM award_progress WHERE band = '2190m') <> 1
       OR (SELECT count(*) FROM contest_multipliers WHERE band = '2190m') <> 1
       OR (SELECT count(*) FROM spot_watch_rules WHERE band = '2190m') <> 1 THEN
        RAISE EXCEPTION 'migration 002 did not merge a band-key collision';
    END IF;

    IF (SELECT preferences->>'default_band' FROM users
        WHERE email = 'migration-002@example.invalid') <> '2190M' THEN
        RAISE EXCEPTION 'migration 002 did not rewrite the default band preference';
    END IF;

    IF (SELECT first_qso_id FROM contest_multipliers WHERE band = '2190m')
       IS DISTINCT FROM (SELECT id FROM qsos WHERE callsign = 'W5TEST') THEN
        RAISE EXCEPTION 'migration 002 did not preserve the earliest multiplier QSO';
    END IF;
END
$assert$;
