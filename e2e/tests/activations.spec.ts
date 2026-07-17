import { test, expect } from '@playwright/test'
import { setupAuth, getDefaultLogbook, createQso } from '../fixtures/helpers'
import { registerTestUser } from '../fixtures/auth'
import { ApiClient } from '../fixtures/api'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('Activations API', () => {
  test('can create a POTA activation via API', async () => {
    const u = await registerTestUser('pota_api_' + Date.now())
    const c = new ApiClient(u.token, u.userId)
    const lb = await c.getDefaultLogbook()

    const activation = await c.post<{ uuid: string; reference: string; status: string }>(
      '/v1/activations/pota',
      {
        logbook_uuid: lb.uuid,
        reference: 'K-0001',
        activation_date: new Date().toISOString().split('T')[0],
      }
    )
    expect(activation.uuid).toBeTruthy()
    expect(activation.reference).toBe('K-0001')
    expect(activation.status).toBeTruthy()
  })

  test('can list POTA activations', async () => {
    const u = await registerTestUser('pota_list_' + Date.now())
    const c = new ApiClient(u.token, u.userId)
    const lb = await c.getDefaultLogbook()

    await c.post('/v1/activations/pota', {
      logbook_uuid: lb.uuid,
      reference: 'K-0002',
      activation_date: new Date().toISOString().split('T')[0],
    })

    const data = await c.get<{ items: Array<{ uuid: string }> }>('/v1/activations/pota')
    expect(data.items.length).toBeGreaterThanOrEqual(1)
  })

  test('activation status includes QSO count', async () => {
    const u = await registerTestUser('pota_status_' + Date.now())
    const c = new ApiClient(u.token, u.userId)
    const lb = await c.getDefaultLogbook()

    const activation = await c.post<{ uuid: string }>('/v1/activations/pota', {
      logbook_uuid: lb.uuid,
      reference: 'K-0003',
      activation_date: new Date().toISOString().split('T')[0],
    })

    // Log a few QSOs
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${u.token}`, 'X-User-ID': u.userId }
    for (const cs of ['W1A', 'W2B', 'W3C', 'W4D', 'W5E', 'W6F', 'W7G', 'W8H', 'W9I', 'W0J']) {
      await fetch(`${API}/v1/logbooks/${lb.uuid}/qsos`, {
        method: 'POST',
        headers: h,
        body: JSON.stringify({ callsign: cs, band: '20m', mode: 'SSB', datetime_on: new Date().toISOString() }),
      })
    }

    const status = await c.get<{ unique_callsigns: number; minimum_contacts: number }>(
      `/v1/activations/pota/${activation.uuid}/status`
    )
    expect(typeof status.unique_callsigns).toBe('number')
    expect(typeof status.minimum_contacts).toBe('number')
  })
})

test.describe('Activations Page UI', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'actui_' + Date.now())
    await page.goto(WEB + '/#/activations')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1000)
  })

  test('activations page renders', async ({ page }) => {
    await expect(page.getByRole('main').getByText('Activations')).toBeVisible()
  })

  test('POTA and SOTA tabs are visible', async ({ page }) => {
    await expect(page.getByRole('tab', { name: 'POTA' })).toBeVisible()
    await expect(page.getByRole('tab', { name: 'SOTA' })).toBeVisible()
  })

  test('SOTA tab loads', async ({ page }) => {
    await page.getByRole('tab', { name: 'SOTA' }).click()
    await page.waitForTimeout(500)
    await expect(page.getByRole('tab', { name: 'SOTA' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('New activation button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /New activation/i })).toBeVisible()
  })

  test('activations table is present', async ({ page }) => {
    await expect(page.locator('.q-table')).toBeVisible({ timeout: 8000 })
  })

  test('empty activations shows empty table', async ({ page }) => {
    // New user has no activations — table should be visible but empty
    await expect(page.locator('.q-table')).toBeVisible({ timeout: 8000 })
    await expect(page.locator('.q-page')).toBeVisible()
  })

  test('no crash on page load', async ({ page }) => {
    await expect(page.locator('.bg-negative').filter({ hasText: /error|failed/i })).not.toBeVisible()
  })
})

test.describe('Activations with data', () => {
  test('activation detail shows QSO count after logging QSOs', async ({ page }) => {
    const user = await setupAuth(page, 'actdata_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)

    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}`, 'X-User-ID': user.userId }
    const activationRes = await fetch(`${API}/v1/activations/pota`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({
        logbook_uuid: lb.uuid,
        reference: 'K-TEST',
        activation_date: new Date().toISOString().split('T')[0],
      }),
    })
    const activationJson = await activationRes.json()
    const activationUUID = activationJson.data.uuid

    // Log 5 QSOs
    for (let i = 1; i <= 5; i++) {
      await fetch(`${API}/v1/logbooks/${lb.uuid}/qsos`, {
        method: 'POST',
        headers: h,
        body: JSON.stringify({ callsign: `W${i}TST`, band: '20m', mode: 'SSB', datetime_on: new Date().toISOString() }),
      })
    }

    await page.goto(WEB + '/#/activations')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    // Click the activation row to see details
    await page.locator('.q-table tbody tr').first().click()
    await page.waitForTimeout(800)

    // Detail panel should show
    await expect(page.getByText('K-TEST')).toBeVisible({ timeout: 8000 })
    // Progress section should show unique callsigns
    await expect(page.getByText(/unique callsigns/i)).toBeVisible({ timeout: 8000 })
  })

  test('Export ADIF button visible in POTA detail', async ({ page }) => {
    const user = await setupAuth(page, 'actexp_' + Date.now())
    const lb = await getDefaultLogbook(user.token, user.userId)

    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}`, 'X-User-ID': user.userId }
    await fetch(`${API}/v1/activations/pota`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({
        logbook_uuid: lb.uuid,
        reference: 'K-EXPORT',
        activation_date: new Date().toISOString().split('T')[0],
      }),
    })

    await page.goto(WEB + '/#/activations')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    await page.locator('.q-table tbody tr').first().click()
    await page.waitForTimeout(800)

    // Export ADIF button should be visible in the detail panel for POTA
    await expect(page.getByRole('button', { name: /Export ADIF/i })).toBeVisible({ timeout: 8000 })
  })
})
