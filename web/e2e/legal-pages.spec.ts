/**
 * legal-pages.spec.ts
 *
 * P0 tests for RadioLedger's self-hosted software and deployment privacy notices.
 * Does NOT require authentication.
 *
 * The app uses hash routing: /#/legal/terms and /#/legal/privacy.
 */

import { test, expect } from '@playwright/test'

test.describe('Legal Pages', () => {
  test.describe('Self-hosted software notice', () => {
    test.beforeEach(async ({ page }) => {
      await page.goto('/#/legal/terms')
    })

    test('page loads at /#/legal/terms', async ({ page }) => {
      await expect(page).toHaveURL(/\/#\/legal\/terms/)
    })

    test('renders actual content (not blank)', async ({ page }) => {
      // The legal page renders markdown — check the content container has text
      const content = page.locator('.legal-content')
      await expect(content).toBeVisible()
      const text = await content.innerText()
      expect(text.trim().length).toBeGreaterThan(100)
    })

    test('renders the self-hosted software heading', async ({ page }) => {
      // The markdown renders an H1 from the TERMS_OF_SERVICE asset
      await expect(page.getByRole('heading', { name: 'Self-hosted software notice' })).toBeVisible()
    })

    test('explains licensing and operator responsibility', async ({ page }) => {
      const content = page.locator('.legal-content')
      await expect(content).toContainText('GNU Affero General Public License')
      await expect(content).toContainText(
        'you are responsible for its accounts, authentication, backups, security controls',
      )
    })
  })

  test.describe('Deployment privacy notice', () => {
    test.beforeEach(async ({ page }) => {
      await page.goto('/#/legal/privacy')
    })

    test('page loads at /#/legal/privacy', async ({ page }) => {
      await expect(page).toHaveURL(/\/#\/legal\/privacy/)
    })

    test('renders actual content (not blank)', async ({ page }) => {
      const content = page.locator('.legal-content')
      await expect(content).toBeVisible()
      const text = await content.innerText()
      expect(text.trim().length).toBeGreaterThan(100)
    })

    test('renders the deployment privacy heading', async ({ page }) => {
      await expect(page.getByRole('heading', { name: 'Deployment privacy notice' })).toBeVisible()
    })

    test('explains deployment data responsibility', async ({ page }) => {
      const content = page.locator('.legal-content')
      await expect(content).toContainText('does not operate a hosted service')
      await expect(content).toContainText(
        'An administrator who deploys RadioLedger decides what data is collected',
      )
    })
  })
})
