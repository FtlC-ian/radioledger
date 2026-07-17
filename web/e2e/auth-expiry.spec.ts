/**
 * auth-expiry.spec.ts — regression coverage for expired/dead auth tokens on reload.
 *
 * Starts from the cached authenticated storage state, corrupts the stored access token,
 * reloads the app, and verifies we land back on a usable login screen instead of a
 * crash/blank page.
 */

import { test, expect } from '@playwright/test'
import { readAuthFromStorageState } from './helpers/auth'
import { e2eTestEmail, e2eTestPassword } from './test-credentials'

test.describe('Auth expiry handling', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    const { token, user } = readAuthFromStorageState(baseURL!)

    await page.goto(baseURL!, { waitUntil: 'domcontentloaded' })
    await page.evaluate(
      ({ token, user }: { token: string; user: Record<string, unknown> }) => {
        localStorage.setItem('radioledger.token', token)
        localStorage.setItem('radioledger.user', JSON.stringify(user))
      },
      { token, user },
    )

    await page.goto('about:blank')
    await page.goto(`${baseURL!}/#/dashboard`, { waitUntil: 'networkidle' })
    await expect(page).toHaveURL(/\/#\/dashboard/)
  })

  test('redirects to login when stored auth is stale on reload and login can be started again', async ({
    page,
  }) => {
    const pageErrors: string[] = []
    page.on('pageerror', (error) => {
      pageErrors.push(error.message)
    })

    await expect(page.locator('.text-h5', { hasText: 'Statistics Dashboard' })).toBeVisible()

    await page.evaluate(() => {
      localStorage.setItem('radioledger.token', 'expired-oidc-access-token')
      localStorage.removeItem('radioledger.oidc.refresh_token')
      localStorage.removeItem('radioledger.oidc.id_token')
      localStorage.removeItem('radioledger.oidc.expires_at')
    })

    await page.reload()

    await expect(page).toHaveURL(/\/#\/login/, { timeout: 15_000 })
    await expect(page.getByTestId('login-card')).toBeVisible()
    await expect(page.getByRole('link', { name: /self-hosting notice/i })).toBeVisible()
    await expect(page.getByRole('link', { name: /deployment privacy notice/i })).toBeVisible()
    expect(pageErrors).toEqual([])

    const oidcLoginButton = page.getByRole('button', { name: /sign in with radioledger/i })

    if (await oidcLoginButton.isVisible()) {
      const appOrigin = new URL(page.url()).origin

      await Promise.all([
        page.waitForURL(
          (url) => url.origin !== appOrigin && url.pathname.startsWith('/ui/login/'),
          { timeout: 15_000 },
        ),
        oidcLoginButton.click(),
      ])

      await expect(page.getByLabel(/login name/i)).toBeVisible()
    } else {
      await page.getByLabel(/^email$/i).fill(e2eTestEmail)
      await page.getByLabel(/^password$/i).fill(e2eTestPassword)
      await page.getByRole('button', { name: /^sign in$/i }).click()
      await expect(page).toHaveURL(/\/#\/(dashboard|onboarding)/, { timeout: 15_000 })
    }
  })
})
