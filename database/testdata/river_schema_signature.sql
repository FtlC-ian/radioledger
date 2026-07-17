-- Stable, client-version-independent signature for River-owned schema objects
-- and grants. Used before and after application migration 002 in CI.
WITH signature_rows(signature) AS (
    SELECT format('class|%s|%s|%s', c.relkind, c.relname, COALESCE(c.relacl::text, ''))
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relname LIKE 'river\_%' ESCAPE '\'

    UNION ALL
    SELECT format('column|%s|%s|%s|%s|%s|%s', c.relname, a.attnum, a.attname,
                  format_type(a.atttypid, a.atttypmod), a.attnotnull,
                  COALESCE(pg_get_expr(d.adbin, d.adrelid), ''))
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attribute a ON a.attrelid = c.oid
    LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
    WHERE n.nspname = 'public' AND c.relname LIKE 'river\_%' ESCAPE '\'
      AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped

    UNION ALL
    SELECT format('constraint|%s|%s|%s', c.relname, con.conname,
                  pg_get_constraintdef(con.oid, TRUE))
    FROM pg_constraint con
    JOIN pg_class c ON c.oid = con.conrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relname LIKE 'river\_%' ESCAPE '\'

    UNION ALL
    SELECT format('index|%s|%s|%s', tablename, indexname, indexdef)
    FROM pg_indexes
    WHERE schemaname = 'public' AND tablename LIKE 'river\_%' ESCAPE '\'

    UNION ALL
    SELECT format('type|%s|%s|%s', t.typname, t.typtype,
                  COALESCE(string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder), ''))
    FROM pg_type t
    JOIN pg_namespace n ON n.oid = t.typnamespace
    LEFT JOIN pg_enum e ON e.enumtypid = t.oid
    WHERE n.nspname = 'public' AND t.typname LIKE 'river\_%' ESCAPE '\'
    GROUP BY t.typname, t.typtype

    UNION ALL
    SELECT format('function|%s|acl=%s|%s', p.oid::regprocedure::text,
                  COALESCE(p.proacl::text, ''), pg_get_functiondef(p.oid))
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public' AND p.proname LIKE 'river\_%' ESCAPE '\'

    UNION ALL
    SELECT format('trigger|%s|%s|%s', c.relname, t.tgname,
                  pg_get_triggerdef(t.oid, TRUE))
    FROM pg_trigger t
    JOIN pg_class c ON c.oid = t.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relname LIKE 'river\_%' ESCAPE '\'
      AND NOT t.tgisinternal

    UNION ALL
    SELECT format('policy|%s|%s|%s|%s|%s|%s|%s', tablename, policyname,
                  permissive, roles::text, cmd, COALESCE(qual, ''),
                  COALESCE(with_check, ''))
    FROM pg_policies
    WHERE schemaname = 'public' AND tablename LIKE 'river\_%' ESCAPE '\'
)
SELECT COALESCE(md5(string_agg(signature, E'\n' ORDER BY signature)), 'NO_RIVER_OBJECTS')
FROM signature_rows;
