/**
 * login-page.spec.ts
 *
 * P0 tests for the RadioLedger login page.
 * Does NOT require authentication — tests the public-facing login UI only.
 *
 * The app uses hash routing, so the login page is at /#/login.
 */

import { test, expect } from '@playwright/test'

test.describe('Login Page', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/#/login')
  })

  test('page loads at /#/login', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/login/)
  })

  test('RadioLedger logo is visible', async ({ page }) => {
    const logo = page.locator('img[alt="RadioLedger"]')
    await expect(logo).toBeVisible()
  })

  test('sign-in button is visible', async ({ page }) => {
    // Covers both OIDC and email/password login modes
    const signinBtn = page
      .getByRole('button', { name: /sign in/i })
      .or(page.getByRole('tab', { name: /sign in/i }))
      .or(page.getByRole('button', { name: /sign in with radioledger/i }))
    await expect(signinBtn.first()).toBeVisible()
  })

  test('self-hosting notice link is visible below the card', async ({ page }) => {
    const tosLink = page.getByRole('link', { name: 'Self-hosting notice' })
    await expect(tosLink).toBeVisible()
  })

  test('deployment privacy notice link is visible below the card', async ({ page }) => {
    const privacyLink = page.getByRole('link', { name: 'Deployment privacy notice' })
    await expect(privacyLink).toBeVisible()
  })

  test('legal links render below the login card and stay centered', async ({ page }) => {
    const card = page.getByTestId('login-card')
    const legalLinks = page.getByTestId('login-legal-links')

    await expect(card).toBeVisible()
    await expect(legalLinks).toBeVisible()

    const cardBox = await card.boundingBox()
    const legalLinksBox = await legalLinks.boundingBox()

    expect(cardBox).not.toBeNull()
    expect(legalLinksBox).not.toBeNull()

    if (!cardBox || !legalLinksBox) {
      throw new Error('Could not measure login layout boxes')
    }

    expect(legalLinksBox.y).toBeGreaterThanOrEqual(cardBox.y + cardBox.height)

    const cardCenter = cardBox.x + cardBox.width / 2
    const legalLinksCenter = legalLinksBox.x + legalLinksBox.width / 2
    expect(Math.abs(legalLinksCenter - cardCenter)).toBeLessThanOrEqual(8)
  })

  test('clicking the self-hosting notice navigates to /#/legal/terms', async ({ page }) => {
    const tosLink = page.getByRole('link', { name: 'Self-hosting notice' })
    await tosLink.click()
    await expect(page).toHaveURL(/\/#\/legal\/terms/)
  })

  test('clicking the deployment privacy notice navigates to /#/legal/privacy', async ({ page }) => {
    const privacyLink = page.getByRole('link', { name: 'Deployment privacy notice' })
    await privacyLink.click()
    await expect(page).toHaveURL(/\/#\/legal\/privacy/)
  })
})
