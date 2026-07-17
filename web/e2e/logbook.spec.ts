/**
 * logbook.spec.ts  — P1 authenticated tests for the Logbook page.
 *
 * Route: /#/logbook
 */

import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

test.describe('Logbook (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/logbook')
  })

  test('navigates to /#/logbook', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/logbook/)
  })

  test('page heading "Logbook Viewer" is visible', async ({ page }) => {
    await expect(page.locator('.text-h5', { hasText: 'Logbook Viewer' })).toBeVisible()
  })

  test('"New QSO Page" button is visible', async ({ page }) => {
    await expect(page.getByRole('link', { name: /new qso page/i })).toBeVisible()
  })

  test('Quick Add QSO section is visible', async ({ page }) => {
    await expect(page.getByText('Quick Add QSO', { exact: true })).toBeVisible()
  })

  test('Callsign input is present in the Quick Add form', async ({ page }) => {
    // The quick-add form has a .q-field containing "Callsign"
    const callsignField = page.locator('.q-field').filter({ hasText: /^Callsign/ }).first()
    await expect(callsignField).toBeVisible()
  })

  test('table or empty state renders without error', async ({ page }) => {
    // Either a q-table (for existing QSOs) or the table bottom area should be present.
    const table = page.locator('.q-table')
    const hasTable = await table.count() > 0
    if (hasTable) {
      await expect(table.first()).toBeVisible({ timeout: 10_000 })
    } else {
      // The logbook card should at minimum be visible even if empty
      const logbookCard = page.locator('.logbook-page .q-card, .logbook-page .q-table__container').first()
      await expect(logbookCard).toBeVisible({ timeout: 10_000 })
    }
  })
})
