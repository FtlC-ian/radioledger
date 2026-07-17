import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'
import { registerTestUser } from '../fixtures/auth'

const WEB = 'http://localhost:3000'
const API = 'http://localhost:9091'

test.describe('Notification System', () => {
  test.beforeEach(async ({ page }) => {
    await setupAuth(page, 'notif_' + Date.now())
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1000)
  })

  test('notification bell icon is visible in header', async ({ page }) => {
    await expect(page.getByRole('button', { name: /Notifications/i })).toBeVisible({ timeout: 6000 })
  })

  test('notification bell has correct aria-label', async ({ page }) => {
    await expect(page.locator('button[aria-label="Notifications"]')).toBeVisible({ timeout: 6000 })
  })

  test('clicking notification bell opens notification menu', async ({ page }) => {
    await page.getByRole('button', { name: /Notifications/i }).click()
    await page.waitForTimeout(500)
    // The notification menu/panel should appear
    await expect(page.locator('.notification-menu').or(page.locator('.q-menu'))).toBeVisible({ timeout: 4000 })
  })

  test('notification menu shows "no notifications" when empty', async ({ page }) => {
    await page.getByRole('button', { name: /Notifications/i }).click()
    await page.waitForTimeout(500)
    // Empty state message
    await expect(page.getByText(/No notifications|no notifications|nothing here/i)).toBeVisible({ timeout: 4000 })
  })

  test('unread count badge appears after creating notification via API', async ({ page }) => {
    const user = await setupAuth(page, 'notif_badge_' + Date.now())
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}`, 'X-User-ID': user.userId }

    // Create a notification via API
    await fetch(`${API}/v1/notifications`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({
        type: 'system',
        title: 'Test Notification',
        message: 'This is a test notification from the E2E suite.',
      }),
    })

    // Reload to pick up the notification
    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    // Should see a badge with count > 0
    await expect(page.locator('.q-badge').filter({ hasText: /[1-9]/ })).toBeVisible({ timeout: 8000 })
  })

  test('notification count shows correctly in badge', async ({ page }) => {
    const user = await setupAuth(page, 'notif_count_' + Date.now())
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}`, 'X-User-ID': user.userId }

    // Create 3 notifications
    for (let i = 1; i <= 3; i++) {
      await fetch(`${API}/v1/notifications`, {
        method: 'POST',
        headers: h,
        body: JSON.stringify({
          type: 'system',
          title: `Notification ${i}`,
          message: `Message ${i}`,
        }),
      })
    }

    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    // Badge should show 3
    await expect(page.locator('.q-badge').filter({ hasText: '3' })).toBeVisible({ timeout: 8000 })
  })

  test('mark all read removes badge', async ({ page }) => {
    const user = await setupAuth(page, 'notif_read_' + Date.now())
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}`, 'X-User-ID': user.userId }

    // Create a notification
    await fetch(`${API}/v1/notifications`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({ type: 'system', title: 'Read Test', message: 'Mark me read.' }),
    })

    await page.goto(WEB + '/#/')
    await page.waitForLoadState('networkidle')
    await page.waitForTimeout(1500)

    // Open the menu
    await page.getByRole('button', { name: /Notifications/i }).click()
    await page.waitForTimeout(500)

    // Click mark all read
    const markAllBtn = page.getByRole('button', { name: /Mark all read/i })
    await expect(markAllBtn).toBeVisible({ timeout: 4000 })
    await markAllBtn.click()
    await page.waitForTimeout(800)

    // Badge should disappear
    await expect(page.locator('.q-badge').filter({ hasText: /[1-9]/ })).not.toBeVisible({ timeout: 5000 })
  })
})

test.describe('Notification API', () => {
  test('can create and list notifications', async () => {
    const u = await registerTestUser('notif_api_' + Date.now())
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${u.token}`, 'X-User-ID': u.userId }

    const cr = await fetch(`${API}/v1/notifications`, {
      method: 'POST',
      headers: h,
      body: JSON.stringify({ type: 'system', title: 'API Test', message: 'Hello' }),
    })
    const cj = await cr.json()
    expect(cj.success).toBe(true)
    expect(cj.data.title).toBe('API Test')

    const lr = await fetch(`${API}/v1/notifications`, { headers: h })
    const lj = await lr.json()
    expect(lj.success).toBe(true)
    expect(lj.data.items.length).toBeGreaterThanOrEqual(1)
  })

  test('unread count endpoint works', async () => {
    const u = await registerTestUser('notif_cnt_' + Date.now())
    const h = { 'Content-Type': 'application/json', Authorization: `Bearer ${u.token}`, 'X-User-ID': u.userId }

    // Create 2 notifications
    for (let i = 0; i < 2; i++) {
      await fetch(`${API}/v1/notifications`, {
        method: 'POST',
        headers: h,
        body: JSON.stringify({ type: 'system', title: `N${i}`, message: `M${i}` }),
      })
    }

    const r = await fetch(`${API}/v1/notifications/unread-count`, { headers: h })
    const j = await r.json()
    expect(j.success).toBe(true)
    expect(j.data.count).toBe(2)
  })
})
