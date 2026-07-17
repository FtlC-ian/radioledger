-- Test users for development and E2E testing. The primary E2E user is
-- onboarding-complete and has a default logbook so authenticated workflows
-- (including QSO entry and ADIF import) can run without test-order coupling.
-- Only load in dev/staging environments (never production).
--
-- Password for test@example.radioledger.local: TestPassword123!
-- Password for admin@radioledger.test: TestPass123!
-- bcrypt hashes generated with cost 10.
--
-- Usage:
--   psql $DATABASE_URL -f database/seeds/002_test_users.sql
--
-- Verify hash: SELECT email, callsign FROM users WHERE email IN ('test@example.radioledger.local', 'admin@radioledger.test');

INSERT INTO users (email, password_hash, callsign, display_name, timezone, email_verified_at, onboarding_complete)
VALUES
  ('test@example.radioledger.local',
   '$2y$10$q6hIYEkHbnEdqoh34Je5E.gw.Dur768kqRK4/7BrUEE5RfV.ix7f.',
   'W5TST', 'Test Operator', 'America/Chicago', NOW(), TRUE),
  ('admin@radioledger.test',
   '$2y$10$RIjVAkX2ydyRdrnwEwuPLejCy6xFMQLIzGh6nrTuquKBfwQBdMaDm',
   'W5ADM', 'Admin Operator', 'UTC', NOW(), TRUE)
ON CONFLICT DO NOTHING;

INSERT INTO logbooks (user_id, name, callsign, is_default, logbook_type, dedup_window_seconds)
SELECT id, 'E2E Test Logbook', callsign, TRUE, 'general', 60
FROM users
WHERE email = 'test@example.radioledger.local'
  AND NOT EXISTS (
    SELECT 1
    FROM logbooks
    WHERE user_id = users.id
      AND is_default = TRUE
      AND deleted_at IS NULL
  );
