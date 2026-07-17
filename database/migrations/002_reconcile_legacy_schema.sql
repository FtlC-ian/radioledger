-- +goose Up
-- Reconcile exact pre-Goose staging variants after bootstrap records migration 1.
-- This migration is intentionally idempotent so it can also repair early
-- self-hosted databases that recorded version 1 before the consolidated schema
-- and reference catalogs reached their current form.

-- The retired vault_cert credential type has no consumers in the current API.
-- Fail closed instead of invalidating any unexpected legacy records.
-- +goose StatementBegin
DO $do$
BEGIN
    IF EXISTS (
        SELECT 1 FROM user_service_credentials
        WHERE credential_type = 'vault_cert'
    ) THEN
        RAISE EXCEPTION 'cannot retire vault_cert credential type while records still use it';
    END IF;

    ALTER TABLE user_service_credentials
        DROP CONSTRAINT IF EXISTS user_service_credentials_credential_type_check;
    ALTER TABLE user_service_credentials
        ADD CONSTRAINT user_service_credentials_credential_type_check
        CHECK (credential_type IN (
            'api_key', 'username_password', 'session', 'oauth_token'
        ));
END
$do$;
-- +goose StatementEnd

-- Normalize the ADIF band catalog. Insert 2190m before rewriting references so
-- both the qsos and allocation foreign keys remain valid throughout.
INSERT INTO bands (name, lower_freq, upper_freq, band_group, warc, is_common, sort_order) VALUES
('2190m', 0.1357, 0.1378, 'LF', FALSE, FALSE, 50),
('630m', 0.472, 0.479, 'MF', FALSE, FALSE, 51),
('560m', 0.501, 0.504, 'MF', FALSE, FALSE, 52),
('160m', 1.800, 2.000, 'HF', FALSE, TRUE, 1),
('80m', 3.500, 4.000, 'HF', FALSE, TRUE, 2),
('60m', 5.060, 5.450, 'HF', TRUE, TRUE, 3),
('40m', 7.000, 7.300, 'HF', FALSE, TRUE, 4),
('30m', 10.100, 10.150, 'HF', TRUE, TRUE, 5),
('20m', 14.000, 14.350, 'HF', FALSE, TRUE, 6),
('17m', 18.068, 18.168, 'HF', TRUE, TRUE, 7),
('15m', 21.000, 21.450, 'HF', FALSE, TRUE, 8),
('12m', 24.890, 24.990, 'HF', TRUE, TRUE, 9),
('10m', 28.000, 29.700, 'HF', FALSE, TRUE, 10),
('8m', 40.000, 45.000, 'VHF', FALSE, FALSE, 53),
('6m', 50.000, 54.000, 'VHF', FALSE, TRUE, 11),
('5m', 54.000, 69.900, 'VHF', FALSE, FALSE, 54),
('4m', 70.000, 71.000, 'VHF', FALSE, FALSE, 12),
('2m', 144.000, 148.000, 'VHF', FALSE, TRUE, 13),
('1.25m', 222.000, 225.000, 'VHF', FALSE, TRUE, 14),
('70cm', 420.000, 450.000, 'UHF', FALSE, TRUE, 15),
('33cm', 902.000, 928.000, 'UHF', FALSE, FALSE, 16),
('23cm', 1240.000, 1300.000, 'UHF', FALSE, TRUE, 17),
('13cm', 2300.000, 2450.000, 'SHF', FALSE, FALSE, 18),
('9cm', 3300.000, 3500.000, 'SHF', FALSE, FALSE, 55),
('6cm', 5650.000, 5925.000, 'SHF', FALSE, FALSE, 56),
('3cm', 10000.000, 10500.000, 'SHF', FALSE, FALSE, 57),
('1.25cm', 24000.000, 24050.000, 'microwave', FALSE, FALSE, 58),
('6mm', 47000.000, 47200.000, 'microwave', FALSE, FALSE, 59),
('4mm', 75500.000, 81000.000, 'microwave', FALSE, FALSE, 60),
('2.5mm', 119980.000, 120020.000, 'microwave', FALSE, FALSE, 61),
('2mm', 142000.000, 149000.000, 'microwave', FALSE, FALSE, 62),
('1mm', 241000.000, 250000.000, 'microwave', FALSE, FALSE, 63),
('submm', 300000.000, 7500000.000, 'microwave', FALSE, FALSE, 64)
ON CONFLICT (name) DO UPDATE SET
    lower_freq = EXCLUDED.lower_freq,
    upper_freq = EXCLUDED.upper_freq,
    band_group = EXCLUDED.band_group,
    warc = EXCLUDED.warc,
    is_common = EXCLUDED.is_common,
    sort_order = EXCLUDED.sort_order;

-- Rewrite every durable denormalized band value, not only the FK-backed QSO
-- column. Merge the few tables whose uniqueness keys include band before the
-- rewrite so an operator who already entered 2190m data cannot block upgrade.
UPDATE award_progress AS canonical
SET dirty = TRUE,
    updated_at = NOW()
FROM award_progress AS legacy
WHERE legacy.band = '2200m'
  AND canonical.band = '2190m'
  AND canonical.user_id = legacy.user_id
  AND canonical.award_type = legacy.award_type
  AND canonical.entity_key = legacy.entity_key
  AND canonical.mode IS NOT DISTINCT FROM legacy.mode;
DELETE FROM award_progress AS legacy
WHERE legacy.band = '2200m'
  AND EXISTS (
      SELECT 1 FROM award_progress AS canonical
      WHERE canonical.band = '2190m'
        AND canonical.user_id = legacy.user_id
        AND canonical.award_type = legacy.award_type
        AND canonical.entity_key = legacy.entity_key
        AND canonical.mode IS NOT DISTINCT FROM legacy.mode
  );

UPDATE contest_multipliers AS canonical
SET value = GREATEST(canonical.value, legacy.value),
    first_qso_id = CASE
        WHEN legacy.worked_at < canonical.worked_at
            THEN COALESCE(legacy.first_qso_id, canonical.first_qso_id)
        ELSE COALESCE(canonical.first_qso_id, legacy.first_qso_id)
    END,
    worked_at = LEAST(canonical.worked_at, legacy.worked_at)
FROM contest_multipliers AS legacy
WHERE legacy.band = '2200m'
  AND canonical.band = '2190m'
  AND canonical.contest_session_id = legacy.contest_session_id
  AND canonical.multiplier_type = legacy.multiplier_type
  AND canonical.multiplier_key = legacy.multiplier_key
  AND COALESCE(canonical.mode, '') = COALESCE(legacy.mode, '');
DELETE FROM contest_multipliers AS legacy
WHERE legacy.band = '2200m'
  AND EXISTS (
      SELECT 1 FROM contest_multipliers AS canonical
      WHERE canonical.band = '2190m'
        AND canonical.contest_session_id = legacy.contest_session_id
        AND canonical.multiplier_type = legacy.multiplier_type
        AND canonical.multiplier_key = legacy.multiplier_key
        AND COALESCE(canonical.mode, '') = COALESCE(legacy.mode, '')
  );

UPDATE spot_watch_rules AS canonical
SET enabled = canonical.enabled OR legacy.enabled,
    last_notified_at = GREATEST(canonical.last_notified_at, legacy.last_notified_at),
    updated_at = GREATEST(canonical.updated_at, legacy.updated_at)
FROM spot_watch_rules AS legacy
WHERE legacy.band = '2200m'
  AND canonical.band = '2190m'
  AND canonical.user_id = legacy.user_id
  AND canonical.source = legacy.source
  AND canonical.reference = legacy.reference
  AND COALESCE(canonical.mode, '') = COALESCE(legacy.mode, '');
DELETE FROM spot_watch_rules AS legacy
WHERE legacy.band = '2200m'
  AND EXISTS (
      SELECT 1 FROM spot_watch_rules AS canonical
      WHERE canonical.band = '2190m'
        AND canonical.user_id = legacy.user_id
        AND canonical.source = legacy.source
        AND canonical.reference = legacy.reference
        AND COALESCE(canonical.mode, '') = COALESCE(legacy.mode, '')
  );

UPDATE qso_confirmations SET band = '2190m' WHERE band = '2200m';
UPDATE award_progress SET band = '2190m', dirty = TRUE WHERE band = '2200m';
UPDATE contest_multipliers SET band = '2190m' WHERE band = '2200m';
UPDATE spot_watch_rules SET band = '2190m' WHERE band = '2200m';
UPDATE contest_sessions SET category_band = '2190m' WHERE category_band = '2200m';
UPDATE spots SET band = '2190m' WHERE band = '2200m';
UPDATE award_tracking
SET award_config = jsonb_set(award_config, '{band}', '"2190m"'::jsonb)
WHERE award_config->>'band' = '2200m';
UPDATE users
SET preferences = jsonb_set(preferences, '{default_band}', '"2190M"'::jsonb)
WHERE upper(preferences->>'default_band') = '2200M';
UPDATE qsos SET band = '2190m' WHERE band = '2200m';
INSERT INTO band_region_allocations (
    itu_region, band_name, lower_freq, upper_freq, is_default_visible, notes
)
SELECT itu_region, '2190m', lower_freq, upper_freq, is_default_visible, notes
FROM band_region_allocations
WHERE band_name = '2200m'
ON CONFLICT (itu_region, band_name) DO UPDATE SET
    lower_freq = EXCLUDED.lower_freq,
    upper_freq = EXCLUDED.upper_freq,
    is_default_visible = EXCLUDED.is_default_visible,
    notes = EXCLUDED.notes;
DELETE FROM band_region_allocations WHERE band_name = '2200m';
DELETE FROM bands WHERE name = '2200m';

INSERT INTO band_region_allocations (
    itu_region, band_name, lower_freq, upper_freq, is_default_visible, notes
) VALUES
(1, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation'),
(2, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation'),
(3, '2190m', 0.1357, 0.1378, FALSE, 'WRC-12 secondary allocation')
ON CONFLICT (itu_region, band_name) DO UPDATE SET
    lower_freq = EXCLUDED.lower_freq,
    upper_freq = EXCLUDED.upper_freq,
    is_default_visible = EXCLUDED.is_default_visible,
    notes = EXCLUDED.notes;

-- Normalize the ADIF 3.1.7 mode catalog.
INSERT INTO modes (name, category, adif_mode, adif_submode, submodes, is_analog, is_popular, sort_order) VALUES
('FT8', 'DIGITAL', 'FT8', NULL, NULL, FALSE, TRUE, 1),
('SSB', 'PHONE', 'SSB', NULL, ARRAY['USB','LSB'], TRUE, TRUE, 2),
('CW', 'CW', 'CW', NULL, NULL, TRUE, TRUE, 3),
('FT4', 'DIGITAL', 'MFSK', 'FT4', NULL, FALSE, TRUE, 4),
('FM', 'PHONE', 'FM', NULL, NULL, TRUE, TRUE, 5),
('RTTY', 'DIGITAL', 'RTTY', NULL, ARRAY['ASCI'], FALSE, TRUE, 6),
('PSK31', 'DIGITAL', 'PSK', 'PSK31', NULL, FALSE, TRUE, 7),
('DMR', 'DIGITAL', 'DIGITALVOICE', 'DMR', NULL, FALSE, TRUE, 8),
('VARAHF', 'DIGITAL', 'DYNAMIC', 'VARA HF', NULL, FALSE, TRUE, 9),
('JS8', 'DIGITAL', 'MFSK', 'JS8', NULL, FALSE, TRUE, 10),
('WSPR', 'DIGITAL', 'WSPR', NULL, NULL, FALSE, TRUE, 11),
('C4FM', 'DIGITAL', 'DIGITALVOICE', 'C4FM', NULL, FALSE, TRUE, 12),
('DSTAR', 'DIGITAL', 'DIGITALVOICE', 'DSTAR', NULL, FALSE, TRUE, 13),
('SSTV', 'IMAGE', 'SSTV', NULL, NULL, FALSE, TRUE, 14),
('PACKET', 'DATA', 'PKT', NULL, NULL, FALSE, TRUE, 15),
('DIGITALVOICE', 'DIGITAL', 'DIGITALVOICE', NULL, ARRAY['C4FM','DMR','DSTAR','FREEDV','M17'], FALSE, FALSE, 16),
('DYNAMIC', 'DATA', 'DYNAMIC', NULL, ARRAY['FREEDATA','VARA HF','VARA SATELLITE','VARA FM 1200','VARA FM 9600'], FALSE, FALSE, 17),
('FSK', 'DATA', 'FSK', NULL, ARRAY['SCAMP_FAST','SCAMP_SLOW','SCAMP_VSLOW'], FALSE, FALSE, 18),
('MTONE', 'DATA', 'MTONE', NULL, ARRAY['SCAMP_OO','SCAMP_OO_SLW'], FALSE, FALSE, 19),
('OFDM', 'DATA', 'OFDM', NULL, ARRAY['RIBBIT_PIX','RIBBIT_SMS'], FALSE, FALSE, 20),
('PKT', 'DATA', 'PKT', NULL, NULL, FALSE, FALSE, 21),
('AM', 'PHONE', 'AM', NULL, NULL, TRUE, FALSE, 100),
('LSB', 'PHONE', 'SSB', 'LSB', NULL, TRUE, FALSE, 101),
('USB', 'PHONE', 'SSB', 'USB', NULL, TRUE, FALSE, 102),
('PSK63', 'DIGITAL', 'PSK', 'PSK63', NULL, FALSE, FALSE, 103),
('JT65', 'DIGITAL', 'JT65', NULL, ARRAY['JT65A','JT65B','JT65C'], FALSE, FALSE, 104),
('JT9', 'DIGITAL', 'JT9', NULL, NULL, FALSE, FALSE, 105),
('OLIVIA', 'DIGITAL', 'OLIVIA', NULL, NULL, FALSE, FALSE, 106),
('THOR', 'DIGITAL', 'THOR', NULL, NULL, FALSE, FALSE, 107),
('HELL', 'DIGITAL', 'HELL', NULL, ARRAY['FMHELL','HELL80','HFSK','PSKHELL'], FALSE, FALSE, 108),
('DOMINO', 'DIGITAL', 'DOMINO', NULL, ARRAY['DOMINOF'], FALSE, FALSE, 109),
('MFSK', 'DIGITAL', 'MFSK', NULL, ARRAY['FT4','FT2','JS8','Q65'], FALSE, FALSE, 110),
('ARDOP', 'DIGITAL', 'ARDOP', NULL, NULL, FALSE, FALSE, 111),
('FREEDV', 'DIGITAL', 'DIGITALVOICE', 'FREEDV', NULL, FALSE, FALSE, 112),
('M17', 'DIGITAL', 'DIGITALVOICE', 'M17', NULL, FALSE, FALSE, 113),
('Q65', 'DIGITAL', 'MFSK', 'Q65', NULL, FALSE, FALSE, 114),
('ATV', 'IMAGE', 'ATV', NULL, NULL, FALSE, FALSE, 115),
('FAX', 'IMAGE', 'FAX', NULL, NULL, FALSE, FALSE, 116),
('FT2', 'DIGITAL', 'MFSK', 'FT2', NULL, FALSE, FALSE, 117),
('FREEDATA', 'DATA', 'DYNAMIC', 'FREEDATA', NULL, FALSE, FALSE, 118),
('CHIP', 'DATA', 'CHIP', NULL, ARRAY['CHIP64','CHIP128'], FALSE, FALSE, 119),
('CLO', 'DIGITAL', 'CLO', NULL, NULL, FALSE, FALSE, 120),
('CONTESTI', 'DATA', 'CONTESTI', NULL, NULL, FALSE, FALSE, 121),
('FSK441', 'DIGITAL', 'FSK441', NULL, NULL, FALSE, FALSE, 122),
('ISCAT', 'DIGITAL', 'ISCAT', NULL, NULL, FALSE, FALSE, 123),
('JT4', 'DIGITAL', 'JT4', NULL, ARRAY['JT4A','JT4B','JT4C','JT4D','JT4E','JT4F','JT4G'], FALSE, FALSE, 124),
('JT44', 'DIGITAL', 'JT44', NULL, NULL, FALSE, FALSE, 125),
('JT6M', 'DIGITAL', 'JT6M', NULL, NULL, FALSE, FALSE, 126),
('MSK144', 'DIGITAL', 'MSK144', NULL, NULL, FALSE, FALSE, 127),
('MT63', 'DIGITAL', 'MT63', NULL, NULL, FALSE, FALSE, 128),
('OPERA', 'DIGITAL', 'OPERA', NULL, NULL, FALSE, FALSE, 129),
('PAC', 'DATA', 'PAC', NULL, ARRAY['PAC2','PAC3'], FALSE, FALSE, 130),
('PAX', 'DATA', 'PAX', NULL, ARRAY['PAX2'], FALSE, FALSE, 131),
('PSK', 'DIGITAL', 'PSK', NULL, ARRAY['PSK31','PSK63','FSK31','PSK10','PSK63F','PSK125','PSKAM10','PSKAM31','PSKAM50','PSKFEC31','QPSK31','QPSK63','QPSK125'], FALSE, FALSE, 132),
('PSK2K', 'DIGITAL', 'PSK2K', NULL, NULL, FALSE, FALSE, 133),
('Q15', 'DIGITAL', 'Q15', NULL, NULL, FALSE, FALSE, 134),
('QRA64', 'DIGITAL', 'QRA64', NULL, NULL, FALSE, FALSE, 135),
('ROS', 'DIGITAL', 'ROS', NULL, NULL, FALSE, FALSE, 136),
('RTTYM', 'DIGITAL', 'RTTYM', NULL, NULL, FALSE, FALSE, 137),
('T10', 'DIGITAL', 'T10', NULL, NULL, FALSE, FALSE, 138),
('THRB', 'DIGITAL', 'THRB', NULL, ARRAY['THRBX'], FALSE, FALSE, 139),
('TOR', 'DATA', 'TOR', NULL, ARRAY['AMTORFEC','GTOR'], FALSE, FALSE, 140),
('V4', 'DIGITAL', 'V4', NULL, NULL, FALSE, FALSE, 141),
('VOI', 'PHONE', 'VOI', NULL, NULL, TRUE, FALSE, 142),
('WINMOR', 'DATA', 'WINMOR', NULL, NULL, FALSE, FALSE, 143)
ON CONFLICT (name) DO UPDATE SET
    category = EXCLUDED.category,
    adif_mode = EXCLUDED.adif_mode,
    adif_submode = EXCLUDED.adif_submode,
    submodes = EXCLUDED.submodes,
    is_analog = EXCLUDED.is_analog,
    is_popular = EXCLUDED.is_popular,
    sort_order = EXCLUDED.sort_order;

-- Normalize role grants on application-owned objects without touching River's
-- independently managed tables and sequences.
-- +goose StatementBegin
DO $do$
DECLARE
    object_name TEXT;
    application_tables CONSTANT TEXT[] := ARRAY[
        'activations', 'api_keys', 'audit_log', 'award_progress',
        'award_tracking', 'band_region_allocations', 'bands', 'callsign_cache',
        'callsign_records', 'callsign_sync_runs', 'contest_multipliers',
        'contest_qso_exchange', 'contest_session_operators', 'contest_sessions',
        'contests', 'dxcc_entities', 'dxcc_prefixes', 'eqsl_sync_status',
        'import_job_errors', 'import_jobs', 'invite_keys', 'logbooks',
        'lotw_sync_jobs', 'lotw_sync_status', 'modes', 'notifications',
        'operator_profiles', 'operator_verifications', 'operators',
        'paper_qsl_batch_items', 'paper_qsl_batches', 'pota_parks',
        'psk_reception_reports', 'qsl_routes', 'qso_confirmations', 'qsos',
        'sota_summits', 'spot_notification_preferences', 'spot_watch_rules',
        'spots', 'station_callsign_operators', 'station_callsigns',
        'station_locations', 'sync_circuit_state', 'sync_conflicts',
        'sync_rate_limit_window', 'sync_status', 'system_settings',
        'user_callsigns', 'user_roles', 'user_service_credentials', 'users',
        'zip_centroids'
    ];
BEGIN
    FOREACH object_name IN ARRAY application_tables LOOP
        EXECUTE format('REVOKE ALL PRIVILEGES ON TABLE public.%I FROM radioledger_api, radioledger_worker', object_name);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.%I TO radioledger_api', object_name);
        EXECUTE format('GRANT SELECT ON TABLE public.%I TO radioledger_worker', object_name);
    END LOOP;

    FOR object_name IN
        SELECT sequence_name
        FROM information_schema.sequences s
        WHERE sequence_schema = 'public'
          AND EXISTS (
              SELECT 1
              FROM pg_class sequence_class
              JOIN pg_depend dependency ON dependency.objid = sequence_class.oid
              JOIN pg_class owner_table ON owner_table.oid = dependency.refobjid
              WHERE sequence_class.relname = s.sequence_name
                AND sequence_class.relnamespace = 'public'::REGNAMESPACE
                AND owner_table.relname = ANY (application_tables)
                AND dependency.deptype IN ('a', 'i')
          )
    LOOP
        EXECUTE format('REVOKE ALL PRIVILEGES ON SEQUENCE public.%I FROM radioledger_api, radioledger_worker', object_name);
        EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE public.%I TO radioledger_api, radioledger_worker', object_name);
    END LOOP;
END
$do$;
-- +goose StatementEnd

GRANT INSERT, UPDATE ON TABLE sync_status TO radioledger_worker;
GRANT INSERT ON TABLE audit_log TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE spots TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE award_progress TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE callsign_records TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE callsign_cache TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE callsign_sync_runs TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE ON TABLE notifications TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE, SELECT ON TABLE lotw_sync_jobs TO radioledger_worker;
GRANT INSERT, UPDATE, DELETE, SELECT ON TABLE lotw_sync_status TO radioledger_worker;
GRANT INSERT, UPDATE ON TABLE users TO radioledger_worker;
GRANT INSERT ON TABLE logbooks TO radioledger_worker;
GRANT SELECT, UPDATE ON TABLE invite_keys TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE import_jobs TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE import_job_errors TO radioledger_worker;
GRANT INSERT, UPDATE ON TABLE qsos TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE psk_reception_reports TO radioledger_worker;
GRANT SELECT, INSERT, UPDATE ON TABLE zip_centroids TO radioledger_worker;
GRANT INSERT, UPDATE, SELECT ON TABLE eqsl_sync_status TO radioledger_worker;

-- These tables were added after migration 001's broad API grant and are
-- intentionally read-only through the API role.
REVOKE INSERT, UPDATE, DELETE ON TABLE eqsl_sync_status FROM radioledger_api;
REVOKE INSERT, UPDATE, DELETE ON TABLE psk_reception_reports FROM radioledger_api;
REVOKE INSERT, UPDATE, DELETE ON TABLE zip_centroids FROM radioledger_api;

ALTER TABLE award_progress ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS award_progress_worker_all ON award_progress;
CREATE POLICY award_progress_worker_all ON award_progress
    FOR ALL TO radioledger_worker USING (TRUE) WITH CHECK (TRUE);

ALTER TABLE eqsl_sync_status ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS eqsl_sync_status_isolation ON eqsl_sync_status;
CREATE POLICY eqsl_sync_status_isolation ON eqsl_sync_status
    FOR SELECT TO radioledger_api USING (user_id = app_current_user_id());
DROP POLICY IF EXISTS eqsl_sync_status_worker_all ON eqsl_sync_status;
CREATE POLICY eqsl_sync_status_worker_all ON eqsl_sync_status
    FOR ALL TO radioledger_worker USING (TRUE) WITH CHECK (TRUE);

-- +goose Down
-- This migration canonicalizes historical schema and reference data. Reversing
-- it would reintroduce ambiguous legacy state, so down is intentionally a no-op.
SELECT 1;
