-- Baseline pre-Goose consolidated schemas at migration version 1.
--
-- Before v1.0, RadioLedger shipped the complete schema as 001_initial_schema.sql
-- without recording Goose history. The fingerprint covers the complete set of
-- application tables and their columns, constraints, indexes, RLS policies,
-- triggers and functions. An older or partial schema fails closed.
DO $bootstrap$
DECLARE
    actual_schema_fingerprint TEXT;
    allowed_schema_fingerprints CONSTANT TEXT[] := ARRAY[
        -- Current consolidated migration 001.
        '336e95c6e36dc5ab923c1498708b3790',
        -- Reviewed pre-Goose staging variant. Its column order is harmless;
        -- migration 002 reconciles grants, policies, and reference catalogs.
        '16022b86d70c52669d30dbeff29deb07'
    ];
BEGIN
    -- API and worker containers can start together. Serialize the entire
    -- transition even before the Goose table exists.
    PERFORM pg_advisory_xact_lock(hashtext('radioledger_legacy_goose_baseline'));

    IF to_regclass('public.users') IS NULL THEN
        RETURN;
    END IF;

    -- Once Goose owns the schema history, later migrations are authoritative;
    -- the version-1 fingerprint is only for the one-time legacy transition.
    IF to_regclass('public.goose_db_version') IS NOT NULL THEN
        IF EXISTS (
            SELECT 1 FROM goose_db_version
            WHERE version_id > 0 AND is_applied
        ) THEN
            RETURN;
        END IF;
    END IF;

    WITH expected_tables(table_name) AS (
        VALUES
            ('activations'), ('api_keys'), ('audit_log'), ('award_progress'),
            ('award_tracking'), ('band_region_allocations'), ('bands'),
            ('callsign_cache'), ('callsign_records'), ('callsign_sync_runs'),
            ('contest_multipliers'), ('contest_qso_exchange'),
            ('contest_session_operators'), ('contest_sessions'), ('contests'),
            ('dxcc_entities'), ('dxcc_prefixes'), ('eqsl_sync_status'),
            ('import_job_errors'), ('import_jobs'), ('invite_keys'), ('logbooks'),
            ('lotw_sync_jobs'), ('lotw_sync_status'), ('modes'), ('notifications'),
            ('operator_profiles'), ('operator_verifications'), ('operators'),
            ('paper_qsl_batch_items'), ('paper_qsl_batches'), ('pota_parks'),
            ('psk_reception_reports'), ('qsl_routes'), ('qso_confirmations'),
            ('qsos'), ('sota_summits'), ('spot_notification_preferences'),
            ('spot_watch_rules'), ('spots'), ('station_callsign_operators'),
            ('station_callsigns'), ('station_locations'), ('sync_circuit_state'),
            ('sync_conflicts'), ('sync_rate_limit_window'), ('sync_status'),
            ('system_settings'), ('user_callsigns'), ('user_roles'),
            ('user_service_credentials'), ('users'), ('zip_centroids')
    ), signature_rows(signature) AS (
        SELECT format('table|%s|rls=%s|force=%s', c.relname,
                      c.relrowsecurity, c.relforcerowsecurity)
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN expected_tables e ON e.table_name = c.relname
        WHERE n.nspname = 'public' AND c.relkind = 'r'
        UNION ALL
        SELECT format('role|%s|login=%s|super=%s|inherit=%s|createrole=%s|createdb=%s|replication=%s|bypassrls=%s',
                      rolname, rolcanlogin, rolsuper, rolinherit, rolcreaterole,
                      rolcreatedb, rolreplication, rolbypassrls)
        FROM pg_roles
        WHERE rolname IN ('radioledger_api', 'radioledger_worker')
        UNION ALL
        SELECT 'extension|' || extname
        FROM pg_extension
        WHERE extname IN ('postgis', 'pgcrypto')
        UNION ALL
        SELECT format('table_privileges|%s|api=%s|worker=%s', c.relname,
                      ARRAY[
                          has_table_privilege('radioledger_api', c.oid, 'SELECT'),
                          has_table_privilege('radioledger_api', c.oid, 'INSERT'),
                          has_table_privilege('radioledger_api', c.oid, 'UPDATE'),
                          has_table_privilege('radioledger_api', c.oid, 'DELETE'),
                          has_table_privilege('radioledger_api', c.oid, 'TRUNCATE'),
                          has_table_privilege('radioledger_api', c.oid, 'REFERENCES'),
                          has_table_privilege('radioledger_api', c.oid, 'TRIGGER')
                      ]::TEXT,
                      ARRAY[
                          has_table_privilege('radioledger_worker', c.oid, 'SELECT'),
                          has_table_privilege('radioledger_worker', c.oid, 'INSERT'),
                          has_table_privilege('radioledger_worker', c.oid, 'UPDATE'),
                          has_table_privilege('radioledger_worker', c.oid, 'DELETE'),
                          has_table_privilege('radioledger_worker', c.oid, 'TRUNCATE'),
                          has_table_privilege('radioledger_worker', c.oid, 'REFERENCES'),
                          has_table_privilege('radioledger_worker', c.oid, 'TRIGGER')
                      ]::TEXT)
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN expected_tables e ON e.table_name = c.relname
        WHERE n.nspname = 'public' AND c.relkind = 'r'
        UNION ALL
        SELECT format('sequence|%s|type=%s|start=%s|increment=%s|max=%s|min=%s|cache=%s|cycle=%s|api=%s|worker=%s',
                      c.relname, format_type(s.seqtypid, NULL), s.seqstart,
                      s.seqincrement, s.seqmax, s.seqmin, s.seqcache, s.seqcycle,
                      ARRAY[
                          has_sequence_privilege('radioledger_api', c.oid, 'USAGE'),
                          has_sequence_privilege('radioledger_api', c.oid, 'SELECT'),
                          has_sequence_privilege('radioledger_api', c.oid, 'UPDATE')
                      ]::TEXT,
                      ARRAY[
                          has_sequence_privilege('radioledger_worker', c.oid, 'USAGE'),
                          has_sequence_privilege('radioledger_worker', c.oid, 'SELECT'),
                          has_sequence_privilege('radioledger_worker', c.oid, 'UPDATE')
                      ]::TEXT)
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN pg_sequence s ON s.seqrelid = c.oid
        WHERE n.nspname = 'public' AND c.relkind = 'S'
          AND EXISTS (
              SELECT 1
              FROM pg_depend dep
              JOIN pg_class owner_table ON owner_table.oid = dep.refobjid
              JOIN pg_namespace owner_namespace ON owner_namespace.oid = owner_table.relnamespace
              JOIN expected_tables e ON e.table_name = owner_table.relname
              WHERE dep.classid = 'pg_class'::REGCLASS
                AND dep.objid = c.oid
                AND dep.refclassid = 'pg_class'::REGCLASS
                AND owner_namespace.nspname = 'public'
                AND dep.deptype IN ('a', 'i')
          )
        UNION ALL
        SELECT format('schema_privileges|api_usage=%s|api_create=%s|worker_usage=%s|worker_create=%s',
                      has_schema_privilege('radioledger_api', 'public', 'USAGE'),
                      has_schema_privilege('radioledger_api', 'public', 'CREATE'),
                      has_schema_privilege('radioledger_worker', 'public', 'USAGE'),
                      has_schema_privilege('radioledger_worker', 'public', 'CREATE'))
        UNION ALL
        SELECT format('column|%s|%s|%s|notnull=%s|default=%s', c.relname, a.attnum,
                      a.attname || ':' || format_type(a.atttypid, a.atttypmod),
                      a.attnotnull, COALESCE(pg_get_expr(d.adbin, d.adrelid), ''))
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN expected_tables e ON e.table_name = c.relname
        JOIN pg_attribute a ON a.attrelid = c.oid
        LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
        WHERE n.nspname = 'public' AND c.relkind = 'r'
          AND a.attnum > 0 AND NOT a.attisdropped
        UNION ALL
        SELECT format('constraint|%s|%s|%s', c.relname, con.conname,
                      pg_get_constraintdef(con.oid, TRUE))
        FROM pg_constraint con
        JOIN pg_class c ON c.oid = con.conrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN expected_tables e ON e.table_name = c.relname
        WHERE n.nspname = 'public'
        UNION ALL
        SELECT format('index|%s|%s|%s', tablename, indexname, indexdef)
        FROM pg_indexes i
        JOIN expected_tables e ON e.table_name = i.tablename
        WHERE schemaname = 'public'
        UNION ALL
        SELECT format('policy|%s|%s|%s|%s|%s|%s|%s', p.tablename, p.policyname,
                      p.permissive, p.roles::TEXT, p.cmd, COALESCE(p.qual, ''),
                      COALESCE(p.with_check, ''))
        FROM pg_policies p
        JOIN expected_tables e ON e.table_name = p.tablename
        WHERE p.schemaname = 'public'
        UNION ALL
        SELECT format('trigger|%s|%s|%s', c.relname, t.tgname, pg_get_triggerdef(t.oid, TRUE))
        FROM pg_trigger t
        JOIN pg_class c ON c.oid = t.tgrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        JOIN expected_tables e ON e.table_name = c.relname
        WHERE n.nspname = 'public' AND NOT t.tgisinternal
        UNION ALL
        SELECT 'function|' || p.oid::REGPROCEDURE::TEXT || '|api_exec=' ||
               has_function_privilege('radioledger_api', p.oid, 'EXECUTE') ||
               '|worker_exec=' || has_function_privilege('radioledger_worker', p.oid, 'EXECUTE') ||
               '|' || pg_get_functiondef(p.oid)
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'public'
          AND p.proname IN (
              'app_current_user_id', 'app_role_rank', 'app_has_logbook_min_role',
              'find_qso_matches', 'maidenhead_to_point', 'update_qso_locations',
              'update_user_location', 'enforce_qso_identity_scope',
              'mark_sync_dirty_on_qso_edit', 'enforce_paper_qsl_item_scope',
              'ensure_logbook_owner_role'
          )
          AND NOT EXISTS (
              SELECT 1 FROM pg_depend dep
              WHERE dep.classid = 'pg_proc'::REGCLASS AND dep.objid = p.oid
                AND dep.deptype = 'e'
          )
        UNION ALL
        SELECT 'data|bands|' || JSONB_BUILD_ARRAY(
            name, lower_freq, upper_freq, band_group, warc, is_common, sort_order
        )::TEXT
        FROM bands
        UNION ALL
        SELECT 'data|modes|' || JSONB_BUILD_ARRAY(
            name, category, adif_mode, adif_submode, submodes,
            is_analog, is_popular, sort_order
        )::TEXT
        FROM modes
        UNION ALL
        SELECT 'data|dxcc_entities|' || JSONB_BUILD_ARRAY(
            entity_id, name, lotw_entity_name, prefix, continent, cq_zone,
            itu_zone, latitude, longitude, deleted, valid_from, valid_to
        )::TEXT
        FROM dxcc_entities
        UNION ALL
        SELECT 'data|dxcc_prefixes|' || JSONB_BUILD_ARRAY(prefix, entity_id, source)::TEXT
        FROM dxcc_prefixes
        UNION ALL
        SELECT 'data|band_region_allocations|' || JSONB_BUILD_ARRAY(
            itu_region, band_name, lower_freq, upper_freq, is_default_visible, notes
        )::TEXT
        FROM band_region_allocations
    )
    SELECT MD5(STRING_AGG(signature, E'\n' ORDER BY signature))
    INTO actual_schema_fingerprint
    FROM signature_rows;

    IF NOT (actual_schema_fingerprint = ANY (allowed_schema_fingerprints)) THEN
        RAISE EXCEPTION 'legacy schema fingerprint mismatch (allowed %, got %); refusing to baseline migration 001',
            allowed_schema_fingerprints, actual_schema_fingerprint;
    END IF;

    CREATE TABLE IF NOT EXISTS goose_db_version (
        id          INTEGER PRIMARY KEY GENERATED BY DEFAULT AS IDENTITY,
        version_id  BIGINT NOT NULL,
        is_applied  BOOLEAN NOT NULL,
        tstamp      TIMESTAMP NOT NULL DEFAULT NOW()
    );

    LOCK TABLE goose_db_version IN SHARE ROW EXCLUSIVE MODE;
    IF NOT EXISTS (
        SELECT 1
        FROM goose_db_version
        WHERE version_id > 0
          AND is_applied
    ) THEN
        INSERT INTO goose_db_version (version_id, is_applied)
        VALUES (1, TRUE);
    END IF;
END
$bootstrap$;
