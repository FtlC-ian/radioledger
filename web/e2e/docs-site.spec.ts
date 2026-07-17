/**
 * docs-site.spec.ts
 *
 * P0 tests for the RadioLedger VitePress documentation site at /docs/.
 *
 * NOTE: This is NOT hash-routed. The docs site is a separate VitePress static
 * site served by nginx at the /docs/ path prefix. Routes here use normal paths.
 */

import { test, expect } from '@playwright/test'

test.describe('Docs Site (/docs/)', () => {
  test('docs home page loads', async ({ page }) => {
    await page.goto('/docs/')
    await expect(page).toHaveURL(/\/docs\//)
    // VitePress renders a #app div with a Layout class
    await expect(page.locator('#app')).toBeVisible()
  })

  test('page has VitePress layout styling', async ({ page }) => {
    await page.goto('/docs/')
    // VitePress home page has the .VPHome class
    await expect(page.locator('.VPHome').first()).toBeVisible()
  })

  test('RadioLedger logo appears in the nav', async ({ page }) => {
    await page.goto('/docs/')
    // VitePress renders the logo as a .VPImage.logo img
    const logo = page.locator('.VPNavBarTitle .VPImage.logo, .VPNavBarTitle img')
    await expect(logo.first()).toBeVisible()
  })

  test('nav title shows "RadioLedger Docs"', async ({ page }) => {
    await page.goto('/docs/')
    const title = page.locator('.VPNavBarTitle .title')
    await expect(title).toContainText('RadioLedger Docs')
  })

  test('Getting Started nav link is present', async ({ page }) => {
    await page.goto('/docs/')
    const link = page.getByRole('link', { name: /getting started/i }).first()
    await expect(link).toBeVisible()
  })

  test('Getting Started link navigates to /docs/getting-started/', async ({ page }) => {
    await page.goto('/docs/')
    // Click the nav link (first match in the nav bar)
    const link = page.locator('nav .VPNavBarMenuLink', { hasText: /getting started/i })
    await link.click()
    await expect(page).toHaveURL(/\/docs\/getting-started\//)
  })

  test('Getting Started page renders content', async ({ page }) => {
    await page.goto('/docs/getting-started/')
    // VitePress content renders inside .vp-doc
    await expect(page.locator('.vp-doc').first()).toBeVisible()
  })

  test('User Guide nav link navigates to /docs/user-guide/', async ({ page }) => {
    await page.goto('/docs/')
    const link = page.locator('nav .VPNavBarMenuLink', { hasText: /user guide/i })
    await link.click()
    await expect(page).toHaveURL(/\/docs\/user-guide\//)
  })

  test('Self-Hosting nav link navigates to /docs/self-hosting/', async ({ page }) => {
    await page.goto('/docs/')
    const link = page.locator('nav .VPNavBarMenuLink', { hasText: /self.hosting/i })
    await link.click()
    await expect(page).toHaveURL(/\/docs\/self-hosting\//)
  })
})
