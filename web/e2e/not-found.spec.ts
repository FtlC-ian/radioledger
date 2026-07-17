/**
 * not-found.spec.ts
 *
 * P0 tests for the RadioLedger 404 / Not Found page.
 * Does NOT require authentication.
 *
 * The ErrorNotFound component renders at any unmatched hash route.
 */

import { test, expect } from '@playwright/test'

test.describe('404 Not Found Page', () => {
  test('navigating to a nonexistent hash route shows 404 content', async ({ page }) => {
    await page.goto('/#/some-nonexistent-page')
    // The ErrorNotFound component renders a big "404" on screen
    await expect(page.getByText('404')).toBeVisible()
  })

  test('404 page shows a helpful message', async ({ page }) => {
    await page.goto('/#/some-nonexistent-page')
    await expect(page.getByText(/nothing here/i)).toBeVisible()
  })

  test('404 page has a "Go Home" button', async ({ page }) => {
    await page.goto('/#/some-nonexistent-page')
    const homeBtn = page.getByRole('link', { name: /go home/i })
    await expect(homeBtn).toBeVisible()
  })

  test('"Go Home" button navigates away from the 404 page', async ({ page }) => {
    await page.goto('/#/some-nonexistent-page')
    const homeBtn = page.getByRole('link', { name: /go home/i })
    await homeBtn.click()
    // Should navigate to / (which redirects to /#/login or /#/dashboard)
    await expect(page).not.toHaveURL(/some-nonexistent-page/)
  })
})
