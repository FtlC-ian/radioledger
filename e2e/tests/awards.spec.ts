import { test, expect } from '@playwright/test'
import { setupAuth, getDefaultLogbook, createQso } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('Awards Page', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'awards_' + Date.now())
    await page.goto(WEB + '/#/awards')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1000)
  })

  test('awards page renders with heading', async ({ page }) => {
    await expect(page.getByRole('main').getByText('Awards')).toBeVisible()
  })

  test('DXCC tab is visible and active by default', async ({ page }) => {
    await expect(page.getByRole('tab', { name: 'DXCC' })).toBeVisible()
    // DXCC progress card should be visible
    await expect(page.getByText('DXCC Progress')).toBeVisible({ timeout: 8000 })
  })

  test('WAS tab loads', async ({ page }) => {
    await page.getByRole('tab', { name: 'WAS' }).click()
    await page.waitForTimeout(800)
    // Should switch to WAS panel — check the tab is now selected
    await expect(page.getByRole('tab', { name: 'WAS' })).toHaveAttribute('aria-selected', 'true')
    // WAS panel should render without crashing
    await expect(page.locator('.q-tab-panel[aria-hidden="false"]')).toBeVisible({ timeout: 6000 })
  })

  test('Grids tab loads', async ({ page }) => {
    await page.getByRole('tab', { name: 'Grids' }).click()
    await page.waitForTimeout(800)
    await expect(page.getByRole('tab', { name: 'Grids' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('.q-tab-panel[aria-hidden="false"]')).toBeVisible({ timeout: 6000 })
  })

  test('DXCC progress bar renders', async ({ page }) => {
    // The progress section includes a linear progress bar
    await expect(page.locator('.q-linear-progress').first()).toBeVisible({ timeout: 8000 })
  })

  test('DXCC country count shows 0/n worked on empty logbook', async ({ page }) => {
    // New user with empty logbook — worked should be 0
    await expect(page.getByText(/0\/\d+ worked/)).toBeVisible({ timeout: 8000 })
  })

  test('Refresh button triggers reload', async ({ page }) => {
    const btn = page.getByRole('button', { name: /Refresh/i })
    await expect(btn).toBeVisible()
    await btn.click()
    await page.waitForTimeout(500)
    // No crash — page still renders
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('no error banner on fresh empty logbook', async ({ page }) => {
    await expect(page.locator('.bg-negative').filter({ hasText: /error|failed/i })).not.toBeVisible()
  })
})

test.describe('Awards with QSO data', () => {
  test('DXCC country count increases after logging QSO', async ({ page }) => {
    const user = await setupAuth(page, 'awdxcc_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)
    // Log a QSO with a DXCC entity (Germany)
    await createQso(lb.uuid, user.token, user.userId, 'DL1ABC', '20m', 'SSB')

    await page.goto(WEB + '/#/awards')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    // Should show 1/n worked now
    await expect(page.getByText(/1\/\d+ worked/)).toBeVisible({ timeout: 10000 })
  })

  test('DXCC filter buttons are present', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'All' })).toBeVisible({ timeout: 8000 })
    await expect(page.getByRole('button', { name: 'Worked' })).toBeVisible({ timeout: 8000 })
    await expect(page.getByRole('button', { name: 'Needed' })).toBeVisible({ timeout: 8000 })
  })
})
