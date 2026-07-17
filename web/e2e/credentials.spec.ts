/**
 * credentials.spec.ts — E2E tests for the Settings → Sync Services credentials flow.
 *
 * Covers:
 *  - Navigation to Sync Services tab, visibility of service cards
 *  - Save button disabled validation (empty fields)
 *  - Saving API key (Club Log) and username/password (eQSL) via POST /v1/credentials
 *  - Success notification on save
 *  - Warning notification when credential saves but verification fails
 *  - Error notification when save API returns failure
 *  - Credentials persist after reload (Connected status badge + Re-verify/Remove buttons)
 *  - Endpoint contract: /v1/credentials is called, NOT /v1/user/credentials
 *
 * All save/persist tests use page.route() mocking — no real credentials are written
 * to the staging database, and no real service tokens are used.
 */

import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

// ── Dummy test values (known-bad, safe for tests) ──────────────────────────────
const DUMMY_CLUBLOG_KEY = 'test-clublog-key-0000000000'
const DUMMY_POTA_KEY = 'test-pota-key-0000000000'
const DUMMY_EQSL_USER = 'TEST_E2E_USER'
const DUMMY_EQSL_PASS = 'TestE2EPass_do_not_use'

// ── Helper: build a fake CredentialItem response payload ──────────────────────
function makeCredential(
  service: string,
  credentialType: string = 'api_key',
  verified: boolean = false,
) {
  return {
    service,
    credential_type: credentialType,
    is_active: true,
    verified,
    verification_error: verified ? null : 'Test credential — not verified by design',
    last_verified_at: verified ? new Date().toISOString() : null,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }
}

// ── Shared mock: return an empty credential list for GET /v1/credentials ───────
async function mockEmptyCredentials(page: import('@playwright/test').Page) {
  await page.route('**/v1/credentials', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: [] }),
      })
    }
    // Let POSTs and other methods fall through to the next handler or network.
    return route.continue()
  })
  // Block the sync-endpoint fallback so tests don't depend on it
  await page.route('**/v1/sync/credentials', (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ success: false, error: 'not found' }),
    }),
  )
}

// =============================================================================
// Test suite
// =============================================================================

test.describe('Settings → Sync Services credentials flow', () => {
  // ── UI / Navigation tests (no route mocking needed) ─────────────────────

  test.describe('tab navigation and structure', () => {
    test.beforeEach(async ({ page, baseURL }) => {
      await loginFromStorageState(page, baseURL!, '#/settings')
    })

    test('navigates to Sync Services tab and shows "API Credentials" heading', async ({ page }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      await expect(
        page.locator('.text-h6', { hasText: /API Credentials/i }),
      ).toBeVisible({ timeout: 8_000 })
    })

    test('all five credential service cards are visible on Sync Services tab', async ({ page }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      for (const label of ['QRZ.com', 'eQSL.cc', 'Club Log', 'HamQTH', 'Parks on the Air']) {
        await expect(
          page.locator('.q-card').filter({ hasText: label }).first(),
        ).toBeVisible({ timeout: 8_000 })
      }
    })
  })

  // ── Disabled-button validation (empty draft — no API mocking needed) ─────

  test.describe('save button validation — empty fields', () => {
    test.beforeEach(async ({ page, baseURL }) => {
      // Mock credential list to avoid flakes from staging API state
      await mockEmptyCredentials(page)
      await loginFromStorageState(page, baseURL!, '#/settings')
    })

    test('Club Log Save button is disabled when API key is empty', async ({ page }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })
      await expect(card.getByRole('button', { name: /^save$/i })).toBeDisabled()
    })

    test('eQSL Save button is disabled when username/password are empty', async ({ page }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'eQSL.cc' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })
      await expect(card.getByRole('button', { name: /^save$/i })).toBeDisabled()
    })

    test('Club Log Save button enables once an API key is typed', async ({ page }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })
      await card.locator('input[type="password"]').fill(DUMMY_CLUBLOG_KEY)
      await expect(card.getByRole('button', { name: /^save$/i })).toBeEnabled()
    })

    test('eQSL Save button enables only after both username AND password are filled', async ({
      page,
    }) => {
      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'eQSL.cc' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      const usernameInput = card.locator('input[autocomplete="off"]').first()
      const passwordInput = card.locator('input[type="password"]')

      // Only username → still disabled
      await usernameInput.fill(DUMMY_EQSL_USER)
      await expect(card.getByRole('button', { name: /^save$/i })).toBeDisabled()

      // Both fields filled → enabled
      await passwordInput.fill(DUMMY_EQSL_PASS)
      await expect(card.getByRole('button', { name: /^save$/i })).toBeEnabled()
    })
  })

  // ── Save flow with mocked API responses ──────────────────────────────────

  test.describe('credential save flow (mocked API)', () => {
    test.beforeEach(async ({ page, baseURL }) => {
      await mockEmptyCredentials(page)
      await loginFromStorageState(page, baseURL!, '#/settings')
    })

    test('saves Club Log API key via POST /v1/credentials and shows success notification', async ({
      page,
    }) => {
      const captured: { method: string; path: string; body: unknown }[] = []

      // Override POST handler — returns verified credential so positive toast fires
      await page.route('**/v1/credentials', async (route) => {
        const method = route.request().method()
        if (method === 'POST') {
          const body = await route.request().postDataJSON()
          captured.push({ method, path: '/v1/credentials', body })
          return route.fulfill({
            status: 201,
            contentType: 'application/json',
            body: JSON.stringify({ success: true, data: makeCredential('clublog', 'api_key', true) }),
          })
        }
        return route.continue()
      })

      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      await card.locator('input[type="password"]').fill(DUMMY_CLUBLOG_KEY)
      await card.getByRole('button', { name: /^save$/i }).click()

      // Success notification must appear (positive: "Club Log credentials saved")
      await expect(
        page.locator('.q-notification').filter({ hasText: /club log.*saved|credentials saved/i }),
      ).toBeVisible({ timeout: 8_000 })

      // Endpoint contract: exactly one POST to /v1/credentials with correct fields
      expect(captured).toHaveLength(1)
      expect(captured[0].path).toBe('/v1/credentials')
      expect(captured[0].body).toMatchObject({
        service: 'clublog',
        credential_type: 'api_key',
      })
    })

    test('saves eQSL username+password credentials and shows success notification', async ({
      page,
    }) => {
      const captured: { body: unknown }[] = []

      await page.route('**/v1/credentials', async (route) => {
        const method = route.request().method()
        if (method === 'POST') {
          const body = await route.request().postDataJSON()
          captured.push({ body })
          return route.fulfill({
            status: 201,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true,
              // verified: true so the app emits the positive "saved" notification
              data: makeCredential('eqsl', 'username_password', true),
            }),
          })
        }
        return route.continue()
      })

      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'eQSL.cc' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      const usernameInput = card.locator('input[autocomplete="off"]').first()
      const passwordInput = card.locator('input[type="password"]')
      await usernameInput.fill(DUMMY_EQSL_USER)
      await passwordInput.fill(DUMMY_EQSL_PASS)

      await card.getByRole('button', { name: /^save$/i }).click()

      await expect(
        page.locator('.q-notification').filter({ hasText: /eqsl.*saved|credentials saved/i }),
      ).toBeVisible({ timeout: 8_000 })

      expect(captured).toHaveLength(1)
      expect(captured[0].body).toMatchObject({
        service: 'eqsl',
        credential_type: 'username_password',
      })
    })

    test('shows warning notification when credential saves but verification fails', async ({
      page,
    }) => {
      await page.route('**/v1/credentials', async (route) => {
        if (route.request().method() === 'POST') {
          return route.fulfill({
            status: 201,
            contentType: 'application/json',
            body: JSON.stringify({
              success: true,
              data: {
                ...makeCredential('clublog', 'api_key'),
                verified: false,
                verification_error: 'Invalid API key',
              },
            }),
          })
        }
        return route.continue()
      })

      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      await card.locator('input[type="password"]').fill(DUMMY_CLUBLOG_KEY)
      await card.getByRole('button', { name: /^save$/i }).click()

      // The app emits a warning notification when verified === false
      await expect(
        page.locator('.q-notification').filter({ hasText: /invalid api key|saved but could not|unverified/i }).first(),
      ).toBeVisible({ timeout: 8_000 })
    })

    test('shows error notification when save API returns a failure response', async ({ page }) => {
      await page.route('**/v1/credentials', async (route) => {
        if (route.request().method() === 'POST') {
          return route.fulfill({
            status: 422,
            contentType: 'application/json',
            body: JSON.stringify({ success: false, error: 'Invalid credential format' }),
          })
        }
        return route.continue()
      })
      // Block sync fallback so the error path completes predictably
      await page.route('**/v1/sync/credentials/**', (route) =>
        route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not found' }),
        }),
      )

      await page.getByRole('tab', { name: /sync services/i }).click()
      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      await card.locator('input[type="password"]').fill(DUMMY_CLUBLOG_KEY)
      await card.getByRole('button', { name: /^save$/i }).click()

      // The app emits a negative notification on API failure
      await expect(
        page.locator('.q-notification').filter({ hasText: /invalid|could not save|credential format/i }).first(),
      ).toBeVisible({ timeout: 8_000 })
    })
  })

  // ── Credentials persist after reload ─────────────────────────────────────

  test.describe('credential persistence (mocked API)', () => {
    test('status badge shows "Connected" on reload when server has a verified credential', async ({
      page,
      baseURL,
    }) => {
      const savedCred = makeCredential('clublog', 'api_key', true)

      // Mock GET to return saved credential before initial navigation
      await page.route('**/v1/credentials', (route) => {
        if (route.request().method() === 'GET') {
          return route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ success: true, data: [savedCred] }),
          })
        }
        return route.continue()
      })
      await page.route('**/v1/sync/credentials', (route) =>
        route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'not found' }),
        }),
      )

      await loginFromStorageState(page, baseURL!, '#/settings')
      await page.getByRole('tab', { name: /sync services/i }).click()

      const card = page.locator('.q-card').filter({ hasText: 'Club Log' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      // Badge should show "Connected" (verified credential)
      await expect(card.locator('.q-badge', { hasText: /connected/i })).toBeVisible({ timeout: 8_000 })

      // Re-verify and Remove buttons appear when service is connected
      await expect(card.getByRole('button', { name: /re-verify/i })).toBeVisible()
      await expect(card.getByRole('button', { name: /remove/i })).toBeVisible()
    })

    test('status badge shows "Unverified" when credential is saved but not yet verified', async ({
      page,
      baseURL,
    }) => {
      const unverifiedCred = makeCredential('hamqth', 'api_key', false)

      await page.route('**/v1/credentials', (route) => {
        if (route.request().method() === 'GET') {
          return route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ success: true, data: [unverifiedCred] }),
          })
        }
        return route.continue()
      })
      await page.route('**/v1/sync/credentials', (route) =>
        route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) }),
      )

      await loginFromStorageState(page, baseURL!, '#/settings')
      await page.getByRole('tab', { name: /sync services/i }).click()

      const card = page.locator('.q-card').filter({ hasText: 'HamQTH' }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })
      await expect(card.locator('.q-badge', { hasText: /unverified/i })).toBeVisible({ timeout: 8_000 })
    })
  })

  // ── Endpoint contract test ────────────────────────────────────────────────

  test.describe('API endpoint contract', () => {
    test('credential save calls /v1/credentials (not /v1/user/credentials)', async ({
      page,
      baseURL,
    }) => {
      const wrongEndpointHits: string[] = []

      // Listen for any request to the wrong endpoint shape
      page.on('request', (req) => {
        if (req.url().includes('/v1/user/credentials')) {
          wrongEndpointHits.push(req.url())
        }
      })

      await mockEmptyCredentials(page)
      await page.route('**/v1/credentials', async (route) => {
        if (route.request().method() === 'POST') {
          return route.fulfill({
            status: 201,
            contentType: 'application/json',
            body: JSON.stringify({ success: true, data: makeCredential('pota', 'api_key') }),
          })
        }
        return route.continue()
      })

      await loginFromStorageState(page, baseURL!, '#/settings')
      await page.getByRole('tab', { name: /sync services/i }).click()

      const card = page.locator('.q-card').filter({ hasText: /Parks on the Air/i }).first()
      await expect(card).toBeVisible({ timeout: 8_000 })

      await card.locator('input[type="password"]').fill(DUMMY_POTA_KEY)
      await card.getByRole('button', { name: /^save$/i }).click()

      // Wait for save to complete (notification is the signal)
      await page.locator('.q-notification').first().waitFor({ timeout: 8_000 })

      // Contract: /v1/user/credentials must NEVER be called
      expect(wrongEndpointHits).toHaveLength(0)
    })
  })
})
