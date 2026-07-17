/**
 * settings.spec.ts  — P1 authenticated tests for the Settings page.
 *
 * Route: /#/settings
 *
 * The page has two tabs: General (tune icon) and Sync Services (sync icon).
 */

import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

test.describe('Settings (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/settings')
  })

  test('navigates to /#/settings', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/settings/)
  })

  test('page heading "Settings" is visible', async ({ page }) => {
    await expect(page.locator('.text-h5', { hasText: 'Settings' })).toBeVisible()
  })

  test('"General" tab is visible', async ({ page }) => {
    await expect(page.getByRole('tab', { name: /general/i })).toBeVisible()
  })

  test('"Sync Services" tab is visible', async ({ page }) => {
    await expect(page.getByRole('tab', { name: /sync services/i })).toBeVisible()
  })

  test('Profile section heading is visible on the General tab (default)', async ({ page }) => {
    // Use text-subtitle1 to get the card heading exactly
    await expect(page.locator('.text-subtitle1', { hasText: 'Profile' }).first()).toBeVisible()
  })

  test('Callsign field is read-only on General tab', async ({ page }) => {
    // Use the exact aria-label to avoid collisions with eQSL field
    const callsignInput = page.locator('input[aria-label="Callsign"][readonly]')
    await expect(callsignInput).toBeVisible()
  })

  test('clicking "Sync Services" tab switches to sync content', async ({ page }) => {
    await page.getByRole('tab', { name: /sync services/i }).click()
    // The sync tab includes LoTW-related content or "Sync Services" section heading
    const syncContent = page.locator('.text-subtitle1, .text-h6').filter({ hasText: /sync services|lotw|hamlog|qrz/i }).first()
    await expect(syncContent).toBeVisible({ timeout: 5_000 })
  })

  test('clicking back to "General" tab restores profile content', async ({ page }) => {
    await page.getByRole('tab', { name: /sync services/i }).click()
    await page.getByRole('tab', { name: /general/i }).click()
    await expect(page.locator('.text-subtitle1', { hasText: 'Profile' }).first()).toBeVisible()
  })
})
