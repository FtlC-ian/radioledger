import { test, expect } from '@playwright/test'
import { setupAuth } from '../fixtures/helpers'
import { registerTestUser } from '../fixtures/auth'

const WEB = 'http://localhost:3000'

test.describe('Authentication', () => {
  test('app root redirects to login or dashboard', async ({ page }) => {
    await page.goto(WEB + '/')
    await page.waitForLoadState('networkidle')
    expect(page.url()).toMatch(/dashboard|login/)
  })

  test('unauthenticated user is redirected to login page', async ({ page }) => {
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    // Should redirect to login since no auth token is present
    // Use getByRole to avoid strict-mode violation (Sign In appears in both tab + button)
    await expect(page.getByRole('tab', { name: 'Sign In' })).toBeVisible({ timeout: 6000 })
  })

  test('login page renders with Sign In and Register tabs', async ({ page }) => {
    await page.goto(WEB + '/#/login')
    await page.waitForLoadState('networkidle')
    await expect(page.getByRole('tab', { name: 'Sign In' })).toBeVisible({ timeout: 6000 })
    await expect(page.getByRole('tab', { name: 'Register' })).toBeVisible({ timeout: 6000 })
  })

  test('login page has email input', async ({ page }) => {
    await page.goto(WEB + '/#/login')
    await page.waitForLoadState('networkidle')
    await expect(page.getByLabel('Email')).toBeVisible({ timeout: 6000 })
  })

  test('login with valid email navigates to dashboard', async ({ page }) => {
    // Register a user first via API
    const u = await registerTestUser('login_' + Date.now())

    await page.goto(WEB + '/#/login')
    await page.waitForLoadState('networkidle')

    await page.getByLabel('Email').fill(u.email)
    await page.getByRole('button', { name: 'Sign In' }).click()
    await page.waitForURL(/dashboard/, { timeout: 10000 })
    await expect(page).toHaveURL(/dashboard/)
  })

  test('register tab creates account and navigates to dashboard', async ({ page }) => {
    await page.goto(WEB + '/#/login')
    await page.waitForLoadState('networkidle')

    await page.getByRole('tab', { name: 'Register' }).click()
    await page.waitForTimeout(300)

    const uniqueEmail = `reg_${Date.now()}@e2e.invalid`
    await page.getByLabel('Email *').fill(uniqueEmail)
    await page.getByRole('button', { name: 'Create Account' }).click()
    await page.waitForURL(/dashboard/, { timeout: 10000 })
    await expect(page).toHaveURL(/dashboard/)
  })

  test('setupAuth enables direct navigation to authenticated pages', async ({ page }) => {
    await setupAuth(page, 'auth_direct_' + Date.now())
    await page.goto(WEB + '/#/dashboard')
    await page.waitForLoadState('networkidle')
    await expect(page.locator('main').getByText('Dashboard')).toBeVisible({ timeout: 8000 })
  })

  test('routes accessible after setupAuth', async ({ page }) => {
    await setupAuth(page, 'auth_routes_' + Date.now())
    await page.goto(WEB + '/#/logbook')
    await page.waitForLoadState('networkidle')
    // Empty logbook shows empty state, not a table; just verify the page renders
    await expect(page.locator('.q-page')).toBeVisible({ timeout: 8000 })
    await expect(page.getByRole('main').getByText('Logbook')).toBeVisible({ timeout: 5000 })
  })
})
