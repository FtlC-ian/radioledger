import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'

test.describe('Sync Page', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'sync_' + Date.now())
    await page.goto(WEB + '/#/sync')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1000)
  })

  test('sync page renders', async ({ page }) => {
    await expect(page.getByRole('main').getByText('Sync')).toBeVisible()
  })

  test('sync page description text is present', async ({ page }) => {
    await expect(page.getByText(/Synchronize your logbook/i)).toBeVisible()
  })

  test('service cards display for eQSL, ClubLog, LoTW', async ({ page }) => {
    // Wait for service cards to load
    await expect(page.locator('.service-card').first()).toBeVisible({ timeout: 8000 })
    const cardCount = await page.locator('.service-card').count()
    // Should have at least 3 service cards (eQSL, ClubLog, LoTW)
    expect(cardCount).toBeGreaterThanOrEqual(3)
  })

  test('Refresh button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Refresh/i })).toBeVisible({ timeout: 6000 })
  })

  test('sync history table is present', async ({ page }) => {
    await expect(page.locator('.q-table')).toBeVisible({ timeout: 8000 })
  })

  test('no crash on load - no error banner', async ({ page }) => {
    await expect(
      page.locator('.bg-negative').filter({ hasText: /error|failed/i })
    ).not.toBeVisible()
  })

  test('settings icon for eQSL navigates to Settings sync tab with correct URL params and highlights eQSL card', async ({ page }) => {
    // Wait for service cards to be rendered
    await expect(page.locator('.service-card').first()).toBeVisible({ timeout: 8000 })

    // Target the eQSL service card specifically — find the card whose subtitle is "eQSL"
    const eqslCard = page.locator('.service-card').filter({ has: page.locator('.text-subtitle2', { hasText: /^eQSL$/ }) })
    await expect(eqslCard).toBeVisible({ timeout: 8000 })

    // Click the settings/configure button within the eQSL card
    const configureBtn = eqslCard.getByRole('button', { name: /Set up|Configure/i })
    await expect(configureBtn).toBeVisible({ timeout: 6000 })
    await configureBtn.click()

    // Assert URL contains both tab=sync and service=eqsl (hash router puts params in the fragment)
    await page.waitForURL(/\/settings/, { timeout: 6000 })
    const url = page.url()
    expect(url).toMatch(/tab=sync/)
    expect(url).toMatch(/service=eqsl/)

    // Assert the eQSL credential card in Settings is highlighted
    // SettingsSyncServicesTab applies .service-card-highlighted to the matching card
    const settingsEqslCard = page.locator('.service-card-highlighted')
    await expect(settingsEqslCard).toBeVisible({ timeout: 6000 })

    // Confirm the highlighted card is specifically the eQSL one
    await expect(settingsEqslCard).toContainText(/eQSL/i)
  })

  test('sync page has service status badges', async ({ page }) => {
    // Service cards show status badges (e.g., "not_configured", "idle", etc.)
    await expect(page.locator('.q-badge').first()).toBeVisible({ timeout: 8000 })
  })
})
