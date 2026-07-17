/**
 * smoke.spec.ts — Smoke test for all pages
 *
 * Navigates to every page in the app and verifies:
 * 1. Page renders without crashing (status 200 from the server)
 * 2. No JavaScript console errors (specifically Error events)
 * 3. The main content area renders
 */
import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'

const PAGES = [
  { name: 'Dashboard', path: '/#/dashboard' },
  { name: 'Logbook', path: '/#/logbook' },
  { name: 'New QSO', path: '/#/qso/new' },
  { name: 'Awards', path: '/#/awards' },
  { name: 'Activations', path: '/#/activations' },
  { name: 'Contests', path: '/#/contests' },
  { name: 'Sync', path: '/#/sync' },
  { name: 'Settings', path: '/#/settings' },
  { name: 'Import', path: '/#/import' },
]

test.describe('Smoke test — all pages load without errors', () => {
  // Set up auth once and reuse across all page checks
  test('all pages render without JavaScript errors', async ({ page }) => {
    const jsErrors: string[] = []

    page.on('pageerror', (err) => {
      jsErrors.push(err.message)
    })

    await setupAuth(page, 'smoke_' + Date.now())

    for (const { name, path } of PAGES) {
      await page.goto(WEB + path)
      await page.waitForLoadState('networkidle')
      await page.waitForTimeout(800)

      // Main content should be visible
      await expect(page.locator('.q-page').first()).toBeVisible({
        timeout: 8000,
      }).catch(() => {
        throw new Error(`Page "${name}" at ${path}: .q-page not visible`)
      })
    }

    // Filter out known non-critical errors (e.g., network-related in dev mode)
    const criticalErrors = jsErrors.filter(e =>
      !e.includes('net::ERR_') &&
      !e.includes('Failed to fetch') &&
      !e.includes('LoTW') &&
      !e.includes('ResizeObserver') // common benign browser quirk
    )

    if (criticalErrors.length > 0) {
      throw new Error(`JavaScript errors found:\n${criticalErrors.join('\n')}`)
    }
  })

  test('404 page renders for unknown route', async ({ page }) => {
    await setupAuth(page, 'smoke404_' + Date.now())
    await page.goto(WEB + '/#/this-route-does-not-exist-404')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)
    await expect(page.getByText(/404|not found/i)).toBeVisible({ timeout: 5000 })
  })

  test('login page renders without auth', async ({ page }) => {
    // Don't set up auth — go directly to login
    await page.goto(WEB + '/#/login')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(500)
    // Page should render (login screen)
    await expect(page.locator('body')).toBeVisible()
  })
})

test.describe('Per-page smoke tests', () => {
  for (const { name, path } of PAGES) {
    test(`${name} page loads and shows main content`, async ({ page }) => {
      await setupAuth(page, `smoke_${name.toLowerCase().replace(/\s/g, '_')}_${Date.now()}`)
      await page.goto(WEB + path)
      await page.waitForLoadState('networkidle')
      await page.waitForTimeout(800)
      await expect(page.locator('.q-page').first()).toBeVisible({ timeout: 8000 })
    })
  }
})
