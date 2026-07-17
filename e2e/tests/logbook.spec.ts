import { test, expect } from '@playwright/test'
import { registerTestUser } from '../fixtures/auth'
import { ApiClient } from '../fixtures/api'
import { setupAuth } from '../fixtures/helpers'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('Logbook API', () => {
  test('default logbook exists after registration', async () => {
    const u = await registerTestUser('lb_' + Date.now())
    const c = new ApiClient(u.token, u.userId)
    const d = await c.get<{items:Array<{uuid:string;is_default:boolean}>}>('/v1/logbooks')
    expect(d.items.length).toBeGreaterThanOrEqual(1)
    expect(d.items.find(l => l.is_default)).toBeDefined()
  })

  test('can create a new logbook', async () => {
    const u = await registerTestUser('lb2_' + Date.now())
    const c = new ApiClient(u.token, u.userId)
    const lb = await c.post<{uuid:string;name:string}>('/v1/logbooks', { name:'Contest Log 2026' })
    expect(lb.uuid).toBeTruthy()
    expect(lb.name).toBe('Contest Log 2026')
  })

  test('logbooks are isolated between users', async () => {
    const u1 = await registerTestUser('lb3a_' + Date.now())
    const u2 = await registerTestUser('lb3b_' + Date.now())
    const c1 = new ApiClient(u1.token, u1.userId)
    const d1 = await c1.get<{items:Array<{uuid:string}>}>('/v1/logbooks')
    await fetch(API + '/v1/logbooks/' + d1.items[0].uuid + '/qsos', {
      method: 'POST',
      headers: { 'Content-Type':'application/json', Authorization:'Bearer '+u1.token, 'X-User-ID':u1.userId },
      body: JSON.stringify({ callsign:'W1ISO', band:'20m', mode:'SSB', datetime_on:new Date().toISOString() }),
    })
    const c2 = new ApiClient(u2.token, u2.userId)
    const d2 = await c2.get<{items:Array<{uuid:string}>}>('/v1/logbooks')
    const r = await fetch(API + '/v1/logbooks/' + d2.items[0].uuid + '/qsos', {
      headers: { Authorization:'Bearer '+u2.token, 'X-User-ID':u2.userId }
    })
    const j = await r.json()
    expect(j.data.items).toHaveLength(0)
  })
})

test.describe('Logbook UI', () => {
  test('logbook page renders', async ({ page }) => {
    await setupAuth(page, 'lbui_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('main').getByText('Logbook')).toBeVisible()
  })

  test('QSO table is present after logging a QSO', async ({ page }) => {
    const user = await setupAuth(page, 'lbcol_' + Date.now())
    // Create a QSO so the table renders (empty logbook shows empty state card, not table)
    const lb = await (await import('../fixtures/helpers')).getDefaultLogbook(user.token, user.userId)
    await (await import('../fixtures/helpers')).createQso(lb.uuid, user.token, user.userId, 'W1AA', '20m', 'SSB')
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForSelector('.q-table', { timeout: 8000 })
    await expect(page.locator('.q-table')).toBeVisible()
  })

  test('QSO table has column headers', async ({ page }) => {
    const user = await setupAuth(page, 'lbhdr_' + Date.now())
    // Create a QSO so the table renders
    const lb = await (await import('../fixtures/helpers')).getDefaultLogbook(user.token, user.userId)
    await (await import('../fixtures/helpers')).createQso(lb.uuid, user.token, user.userId, 'W1BB', '20m', 'SSB')
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    await page.waitForSelector('.q-table', { timeout: 8000 })
    await expect(page.getByRole('columnheader', { name: /callsign/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /band/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /mode/i })).toBeVisible()
  })
})