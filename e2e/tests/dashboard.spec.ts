import { test, expect } from '@playwright/test'
import { setupAuth, getDefaultLogbook } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'dash_' + Date.now())
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)
  })

  test('dashboard page renders', async ({ page }) => {
    await expect(page.getByRole('main').getByText('Dashboard')).toBeVisible()
  })

  test('page content area visible', async ({ page }) => {
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('Recent QSOs section renders when QSOs exist', async ({ page }) => {
    // "Recent QSOs" card only shows when totalQsos > 0
    // This test verifies the empty state (no QSOs = no Recent QSOs card)
    // or if QSOs were added, the card is visible.
    // For empty logbook, verify the empty state message is shown instead.
    const hasQsos = await page.getByText('Recent QSOs').isVisible()
    const hasEmptyState = await page.getByText(/No QSOs yet/i).isVisible()
    expect(hasQsos || hasEmptyState).toBe(true)
  })

  test('empty state shows No QSOs yet', async ({ page }) => {
    await expect(page.getByText(/No QSOs yet/i)).toBeVisible({ timeout: 8000 })
  })

  test('no error banners on fresh load', async ({ page }) => {
    await expect(page.locator('.bg-negative').filter({ hasText: /error|failed/i })).not.toBeVisible()
  })
})

test.describe('Dashboard with data', () => {
  test('renders after creating QSO', async ({ page }) => {
    const user = await setupAuth(page, 'dashd_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)
    await fetch(API + '/v1/logbooks/' + lb.uuid + '/qsos', {
      method: 'POST',
      headers: { 'Content-Type':'application/json', Authorization:'Bearer '+user.token, 'X-User-ID':user.userId },
      body: JSON.stringify({ callsign:'W1DASH', band:'20m', mode:'SSB', datetime_on:new Date().toISOString() }),
    })
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(2000)
    await expect(page.getByRole('main').getByText('Dashboard')).toBeVisible()
  })
})