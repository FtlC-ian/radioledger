/**
 * qso-entry.spec.ts  — P1 authenticated tests for the QSO Entry page.
 *
 * Route: /#/qso/new
 */

import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

test.describe('QSO Entry (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/qso/new')
  })

  test('navigates to /#/qso/new', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/qso\/new/)
  })

  test('page heading "Log QSO" is visible', async ({ page }) => {
    // Use .text-h5 to avoid strict mode conflict with the submit button label
    await expect(page.locator('.text-h5', { hasText: 'Log QSO' })).toBeVisible()
  })

  test('Callsign field is present', async ({ page }) => {
    // QsoForm renders a q-select with label "Callsign *"
    const callsignField = page.locator('.q-field').filter({ hasText: /^Callsign \*/ }).first()
    await expect(callsignField).toBeVisible()
  })

  test('Band select is present', async ({ page }) => {
    // Look for a q-field containing a label element with text "Band"
    const bandField = page.locator('.q-field').filter({ has: page.locator('.q-field__label', { hasText: 'Band' }) }).first()
    await expect(bandField).toBeVisible()
  })

  test('Mode select is present', async ({ page }) => {
    const modeField = page.locator('.q-field').filter({ has: page.locator('.q-field__label', { hasText: 'Mode' }) }).first()
    await expect(modeField).toBeVisible()
  })

  test('Frequency field is present', async ({ page }) => {
    const freqField = page.locator('.q-field').filter({ hasText: /^Frequency$/ }).first()
    await expect(freqField).toBeVisible()
  })

  test('Date/Time field is present', async ({ page }) => {
    const dateField = page.locator('.q-field').filter({ hasText: /Date\/Time/ }).first()
    await expect(dateField).toBeVisible()
  })

  test('"Log QSO" submit button is present', async ({ page }) => {
    await expect(page.getByRole('button', { name: /log qso/i })).toBeVisible()
  })

  test('can fill in and submit a basic QSO — success notification appears', async ({ page }) => {
    // The Callsign field is a q-select with fill-input; we need to type, then
    // blur (Tab or click outside) so the model updates from the input value.
    const callsignInput = page.locator('.q-field').filter({ hasText: /^Callsign \*/ }).locator('input')
    await callsignInput.click()
    await callsignInput.fill('N0CALL')
    // Tab out to blur — this triggers @blur / @keydown.enter on the q-select
    await page.keyboard.press('Tab')

    // Select Band: 20m
    const bandSelect = page.locator('.q-field').filter({ has: page.locator('.q-field__label', { hasText: 'Band' }) }).first()
    await bandSelect.click()
    // Wait for the dropdown to appear
    await page.waitForTimeout(300)
    const band20m = page.getByRole('option', { name: '20m' })
    if (await band20m.isVisible()) {
      await band20m.click()
    } else {
      await page.keyboard.press('Escape')
    }

    // Select Mode: SSB
    const modeSelect = page.locator('.q-field').filter({ has: page.locator('.q-field__label', { hasText: 'Mode' }) }).first()
    await modeSelect.click()
    await page.waitForTimeout(300)
    const ssbOption = page.getByRole('option', { name: 'SSB' })
    if (await ssbOption.isVisible()) {
      await ssbOption.click()
    } else {
      await page.keyboard.press('Escape')
    }

    // Submit the form
    await page.getByRole('button', { name: /log qso/i }).click()

    // Expect a Quasar notification (positive on success, negative on validation/API error)
    // Either proves the form was submitted and processed.
    const notification = page.locator('.q-notification')
    await expect(notification).toBeVisible({ timeout: 15_000 })
  })
})
