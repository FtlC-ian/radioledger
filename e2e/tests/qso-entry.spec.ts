import { test, expect } from '@playwright/test'
import { setupAuth, getDefaultLogbook } from '../fixtures/helpers'
import { registerTestUser } from '../fixtures/auth'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('QSO Entry Form', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'qso_' + Date.now())
    await page.goto(WEB + '/#/qso/new')
    await page.waitForLoadState('networkidle')
  })

  test('New QSO page renders', async ({ page }) => {
    // Page heading is now "Log QSO" (appears in heading div + submit button - use heading)
    await expect(page.locator('.text-h5').filter({ hasText: /Log QSO|New QSO/ })).toBeVisible()
  })

  test('callsign field is present', async ({ page }) => {
    await expect(page.getByLabel(/Callsign/i)).toBeVisible()
  })

  test('band select is present', async ({ page }) => {
    await expect(page.getByLabel(/Band/i)).toBeVisible()
  })

  test('mode select is present', async ({ page }) => {
    await expect(page.getByLabel(/Mode/i)).toBeVisible()
  })

  test('RST fields are present', async ({ page }) => {
    await expect(page.getByLabel(/RST Sent/i)).toBeVisible()
    await expect(page.getByLabel(/RST Rcvd/i)).toBeVisible()
  })

  test('callsign auto-uppercases', async ({ page }) => {
    // q-select with use-input: click to focus then type character by character
    // to trigger Vue's input event handler (which does the uppercase conversion)
    const inp = page.getByLabel(/Callsign/i)
    await inp.click()
    await inp.pressSequentially('w1aw')
    await page.keyboard.press('Tab')
    await page.waitForTimeout(300)
    const val = await inp.inputValue()
    expect(val).toBe('W1AW')
  })

  test('can fill form fields', async ({ page }) => {
    // Use pressSequentially to trigger Vue event handlers on q-select with use-input
    const callsignInp = page.getByLabel(/Callsign/i)
    await callsignInp.click()
    await callsignInp.pressSequentially('DL5XY')
    await page.waitForTimeout(300)
    await page.getByLabel(/RST Sent/i).fill('59')
    expect(await callsignInp.inputValue()).toBe('DL5XY')
  })

  test('form validates callsign is required', async ({ page }) => {
    // Submit button label is "Log QSO" (customized by QsoEntryPage.vue)
    await page.getByRole('button', { name: /Log QSO|Save QSO|Create QSO/i }).first().click()
    await page.waitForTimeout(800)
    await expect(page.getByText(/required/i)).toBeVisible({ timeout: 5000 })
  })

  test('band dropdown opens', async ({ page }) => {
    await page.getByLabel(/Band/i).click()
    await page.waitForTimeout(300)
    await expect(page.getByRole('option').first()).toBeVisible({ timeout: 3000 })
    await page.keyboard.press('Escape')
  })

  test('mode dropdown opens', async ({ page }) => {
    await page.getByLabel(/Mode/i).click()
    await page.waitForTimeout(300)
    await expect(page.getByRole('option').first()).toBeVisible({ timeout: 3000 })
    await page.keyboard.press('Escape')
  })
})

test.describe('QSO CRUD via API', () => {
  test('create, read, update, delete', async () => {
    const user = await registerTestUser('qsocrud_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)
    const h = { 'Content-Type':'application/json', Authorization:'Bearer '+user.token, 'X-User-ID':user.userId }
    const base = API + '/v1/logbooks/' + lb.uuid + '/qsos'

    const cr = await fetch(base, { method:'POST', headers:h,
      body: JSON.stringify({ callsign:'W1CRUD', band:'20m', mode:'SSB', datetime_on:new Date().toISOString() }) })
    const cj = await cr.json()
    expect(cj.success).toBe(true)
    const uuid = cj.data.uuid

    const gr = await fetch(base + '/' + uuid, { headers:h })
    const gj = await gr.json()
    expect(gj.data.callsign).toBe('W1CRUD')

    const ur = await fetch(base + '/' + uuid, { method:'PUT', headers:h,
      body: JSON.stringify({ callsign:'W1CRUD', band:'40m', mode:'CW', datetime_on:new Date().toISOString() }) })
    const uj = await ur.json()
    expect(uj.data.band).toBe('40m')

    const dr = await fetch(base + '/' + uuid, { method:'DELETE', headers:h })
    const dj = await dr.json()
    expect(dj.success).toBe(true)
  })
})