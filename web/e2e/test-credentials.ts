/**
 * Shared credentials for the documented local E2E seed user.
 *
 * The defaults must remain in sync with database/seeds/002_test_users.sql.
 * CI or a separately provisioned environment can override them without
 * committing environment-specific credentials.
 */
export const e2eTestEmail = process.env.PLAYWRIGHT_TEST_EMAIL ?? 'test@example.radioledger.local'
export const e2eTestPassword = process.env.PLAYWRIGHT_TEST_PASSWORD ?? 'TestPassword123!'
