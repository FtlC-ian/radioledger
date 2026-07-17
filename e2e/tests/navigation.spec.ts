import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'
const WEB = 'http://localhost:3000'

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'nav_' + Date.now())
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
  })

  test('sidebar Navigation label is visible', async ({ page }) => {
    await expect(page.getByText('Navigation')).toBeVisible()
  })

  test('sidebar shows all nav items', async ({ page }) => {
    const drawer = page.locator('.q-drawer')
    for (const label of ['Logbook','New QSO','Import','Awards','Settings']) {
      await expect(drawer.getByText(label, { exact: true })).toBeVisible()
    }
  })

  test('sidebar navigates to Logbook', async ({ page }) => {
    await page.locator('.q-drawer').getByText('Logbook', { exact: true }).click()
    await page.waitForURL(/logbook/)
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('sidebar navigates to New QSO', async ({ page }) => {
    await page.locator('.q-drawer').getByText('New QSO', { exact: true }).click()
    await page.waitForURL(/qso\/new/)
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('sidebar navigates to Import', async ({ page }) => {
    await page.locator('.q-drawer').getByText('Import', { exact: true }).click()
    await page.waitForURL(/import/)
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('sidebar navigates to Awards', async ({ page }) => {
    await page.locator('.q-drawer').getByText('Awards', { exact: true }).click()
    await page.waitForURL(/awards/)
    await page.waitForLoadState('networkidle')
    // Awards page shows either content or an error banner - either means navigation worked
    await expect(page.getByText('Awards').or(page.locator('.q-page'))).toBeVisible({ timeout: 15000 })
  })

  test('sidebar navigates to Settings', async ({ page }) => {
    await page.locator('.q-drawer').getByText('Settings', { exact: true }).click()
    await page.waitForURL(/settings/)
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('sidebar navigates back to Dashboard', async ({ page }) => {
    // Navigate away first
    await page.locator('.q-drawer').getByText('Logbook', { exact: true }).click()
    await page.waitForURL(/logbook/)
    // Then back to Dashboard
    await page.locator('.q-drawer').getByText('Dashboard', { exact: true }).click()
    await page.waitForURL(/dashboard/)
    await expect(page.locator('main').getByText('Dashboard')).toBeVisible()
  })

  test('app header shows RadioLedger title', async ({ page }) => {
    await expect(page.getByText('RadioLedger')).toBeVisible()
  })

  test('dark mode toggle is present', async ({ page }) => {
    await expect(page.locator('.q-toggle')).toBeVisible()
  })

  test('dark mode toggle switches theme', async ({ page }) => {
    const body = page.locator('body')
    const before = await body.getAttribute('class')
    await page.locator('.q-toggle').click()
    await page.waitForTimeout(300)
    const after = await body.getAttribute('class')
    expect(after).not.toEqual(before)
  })

  test('hamburger menu visible on mobile viewport', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('button', { name: /menu/i })).toBeVisible()
  })

  test('hamburger menu opens sidebar on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /menu/i }).click()
    // Quasar drawer opens with animation — wait for it to be shown
    await expect(page.locator('.q-drawer')).toHaveClass(/q-drawer--on-top/, { timeout: 3000 })
  })

  test('unknown route shows 404 page', async ({ page }) => {
    await page.goto(WEB + '/#/no-such-route')
    await page.waitForLoadState('networkidle')
    await expect(page.getByText('404')).toBeVisible()
  })
})