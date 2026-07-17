import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'

test.describe('Settings Page', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'settings_' + Date.now())
    await page.goto(WEB + '/#/settings')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1000)
  })

  test('settings page renders', async ({ page }) => {
    await expect(page.getByRole('main').getByText('Settings')).toBeVisible()
  })

  test('profile settings section is present', async ({ page }) => {
    // Section subtitle "Profile" — use exact match to avoid matching description text
    await expect(page.locator('.text-subtitle1').filter({ hasText: 'Profile' }).first()).toBeVisible({ timeout: 6000 })
  })

  test('Timezone field renders', async ({ page }) => {
    await expect(page.getByLabel(/Timezone/i)).toBeVisible({ timeout: 6000 })
  })

  test('logbook preferences section is present', async ({ page }) => {
    // Section is titled "Logbook"
    await expect(page.getByText('Logbook').first()).toBeVisible({ timeout: 6000 })
  })

  test('appearance section with dark theme control is present', async ({ page }) => {
    await expect(page.locator('.text-subtitle1').filter({ hasText: 'Appearance' })).toBeVisible({ timeout: 6000 })
  })

  test('API keys section is visible', async ({ page }) => {
    await expect(page.locator('.text-subtitle1').filter({ hasText: 'API keys' })).toBeVisible({ timeout: 6000 })
    await expect(page.getByText(/No API keys yet/i)).toBeVisible({ timeout: 6000 })
  })

  test('Export data button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Export data/i })).toBeVisible({ timeout: 6000 })
  })

  test('Create key button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Create key/i })).toBeVisible({ timeout: 6000 })
  })

  test('Save changes button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Save changes/i })).toBeVisible({ timeout: 6000 })
  })

  test('Danger zone section is present', async ({ page }) => {
    await expect(page.getByText('Danger zone')).toBeVisible({ timeout: 6000 })
    await expect(page.getByRole('button', { name: /Delete account/i })).toBeVisible({ timeout: 6000 })
  })

  test('theme preference buttons are present', async ({ page }) => {
    // Appearance section should have theme toggle buttons
    await expect(page.locator('.q-btn-toggle').or(page.locator('[class*="btn-toggle"]'))).toBeVisible({ timeout: 6000 })
  })

  test('navigating to /settings?tab=sync&service=eqsl highlights the eQSL card', async ({ page }) => {
    await page.goto(WEB + '/#/settings?tab=sync&service=eqsl')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(600)
    // The eQSL card should have the highlighted border class
    const highlighted = page.locator('.service-card-highlighted')
    await expect(highlighted).toBeVisible({ timeout: 6000 })
    // And contain the eQSL label
    await expect(highlighted).toContainText('eQSL', { timeout: 4000 })
  })

  test('no error banner on settings page', async ({ page }) => {
    await expect(
      page.locator('.bg-negative').filter({ hasText: /error|failed/i })
    ).not.toBeVisible()
  })
})
