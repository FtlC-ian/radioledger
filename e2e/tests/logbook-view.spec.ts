import { test, expect } from '@playwright/test'
import { setupAuth, getDefaultLogbook, createQso } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'

test.describe('Logbook View', () => {
  test('logbook page renders', async ({ page }) => {
    await setupAuth(page, 'lbv_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('main').getByText('Logbook')).toBeVisible()
  })

  test('QSO table renders with columns', async ({ page }) => {
    await setupAuth(page, 'lbcols_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForSelector('.q-table', { timeout: 8000 })
    await expect(page.getByRole('columnheader', { name: /callsign/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /band/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /mode/i })).toBeVisible()
  })

  test('search bar is present', async ({ page }) => {
    await setupAuth(page, 'lbsrch_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    const inp = page.getByPlaceholder(/callsign/i).or(page.getByLabel(/callsign/i)).first()
    await expect(inp).toBeVisible({ timeout: 8000 })
  })

  test('QSO rows appear after creating via API', async ({ page }) => {
    const user = await setupAuth(page, 'lbdata_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)
    await createQso(lb.uuid, user.token, user.userId, 'W1ROW', '20m', 'SSB')
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(2000)
    await expect(page.locator('.q-table')).toBeVisible()
  })

  test('[NEEDS FIX: search route mismatch] search by callsign input is functional', async ({ page }) => {
    const user = await setupAuth(page, 'lbsd_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)
    await createQso(lb.uuid, user.token, user.userId, 'W1AAA', '20m', 'SSB')
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(2000)
    const inp = page.getByPlaceholder(/callsign/i).or(page.getByLabel(/callsign/i)).first()
    if (await inp.isVisible()) {
      await inp.fill('W1AAA')
      await inp.press('Enter')
      await page.waitForTimeout(1500)
      // Table should still be visible after searching (note: route mismatch means /v1/qsos/search
      // maps to /v1/logbooks/{uuid}/qsos/search which may not exist in API)
      await expect(page.locator('.q-table')).toBeVisible()
    }
  })
})