/**
 * dashboard.spec.ts  — P1 authenticated tests for the Statistics Dashboard page.
 *
 * Route: /#/dashboard  (alias: /#/stats)
 */

import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

test.describe('Dashboard (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!)
  })

  test('stays on /#/dashboard after login (not redirected to login)', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/dashboard/)
  })

  test('page heading shows "Statistics Dashboard"', async ({ page }) => {
    await expect(page.locator('.text-h5', { hasText: 'Statistics Dashboard' })).toBeVisible()
  })

  test('overview stat cards are rendered', async ({ page }) => {
    // Quasar renders q-card-section as .q-card__section in the DOM.
    const statCards = page.locator('.q-card .q-card__section')
    await expect(statCards.first()).toBeVisible({ timeout: 10_000 })
  })

  test('"QSOs by Band" chart section is visible', async ({ page }) => {
    await expect(page.getByText('QSOs by Band', { exact: true })).toBeVisible({ timeout: 10_000 })
  })

  test('"QSOs by Mode" chart section is visible', async ({ page }) => {
    await expect(page.getByText('QSOs by Mode', { exact: true })).toBeVisible({ timeout: 10_000 })
  })

  test('Refresh button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /refresh/i })).toBeVisible()
  })
})
