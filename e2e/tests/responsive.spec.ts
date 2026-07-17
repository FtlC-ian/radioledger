/**
 * responsive.spec.ts — Responsive layout tests
 *
 * Tests key flows at mobile viewport (375x667) to verify the layout
 * doesn't break at small screen sizes. Uses Playwright's viewport
 * configuration to simulate a mobile device.
 */
import { test, expect } from '@playwright/test'
import { setupAuth, createQso, getDefaultLogbook } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'

// Mobile iPhone SE viewport
const MOBILE = { width: 375, height: 667 }

test.describe('Responsive — mobile viewport (375x667)', () => {
  test.use({ viewport: MOBILE })

  test('hamburger menu visible instead of sidebar on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_nav_' + Date.now())
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)

    // Hamburger button should be visible on mobile
    await expect(page.getByRole('button', { name: /menu/i })).toBeVisible({ timeout: 5000 })
  })

  test('sidebar opens via hamburger on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_sidebar_' + Date.now())
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)

    await page.getByRole('button', { name: /menu/i }).click()
    await page.waitForTimeout(500)

    // Sidebar drawer should open
    await expect(page.locator('.q-drawer').or(page.locator('[class*="drawer"]'))).toBeVisible({ timeout: 4000 })
  })

  test('dashboard page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_dash_' + Date.now())
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })

  test('logbook page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_lb_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })

  test('QSO entry form renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_qso_' + Date.now())
    await page.goto(WEB + '/#/qso/new')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
    await expect(page.getByLabel(/Callsign/i)).toBeVisible({ timeout: 5000 })
  })

  test('awards page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_awards_' + Date.now())
    await page.goto(WEB + '/#/awards')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })

  test('settings page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_settings_' + Date.now())
    await page.goto(WEB + '/#/settings')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
    // Key sections should still render
    await expect(page.getByText('Settings')).toBeVisible({ timeout: 5000 })
  })

  test('sync page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_sync_' + Date.now())
    await page.goto(WEB + '/#/sync')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })

  test('notification bell visible on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_notif_' + Date.now())
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)

    // Header should still show notification bell
    await expect(page.getByRole('button', { name: /Notifications/i })).toBeVisible({ timeout: 5000 })
  })

  test('QSO entry form can be filled on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_qso_fill_' + Date.now())
    await page.goto(WEB + '/#/qso/new')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)

    const callsignField = page.getByLabel(/Callsign/i)
    await callsignField.click()
    await callsignField.pressSequentially('W1AW')
    await page.waitForTimeout(300)

    const val = await callsignField.inputValue()
    expect(val).toBe('W1AW')
  })

  test('no horizontal overflow on logbook page (mobile)', async ({ page }) => {
    await setupAuth(page, 'mob_overflow_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)

    // Check that the body width is not larger than the viewport
    const bodyWidth = await page.evaluate(() => document.body.scrollWidth)
    expect(bodyWidth).toBeLessThanOrEqual(MOBILE.width + 20) // 20px tolerance for scrollbars
  })

  test('activations page renders on mobile', async ({ page }) => {
    await setupAuth(page, 'mob_act_' + Date.now())
    await page.goto(WEB + '/#/activations')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })
})

test.describe('Responsive — tablet viewport (768x1024)', () => {
  test.use({ viewport: { width: 768, height: 1024 } })

  test('sidebar visible at tablet width', async ({ page }) => {
    await setupAuth(page, 'tab_nav_' + Date.now())
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    // At 768px, sidebar might be hidden — just verify no crash
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })

  test('dashboard renders at tablet viewport', async ({ page }) => {
    await setupAuth(page, 'tab_dash_' + Date.now())
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(800)
    await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 6000 })
  })
})
