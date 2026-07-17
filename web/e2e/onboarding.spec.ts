import { test, expect, type Page } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

async function stubNeedsOnboarding(page: Page) {
  await page.route('**/v1/auth/me', async (route) => {
    const response = await route.fetch()
    const body = await response.json()

    await route.fulfill({
      response,
      json: {
        ...body,
        data: {
          ...(body.data ?? {}),
          callsign: '',
          onboarding_complete: false,
        },
      },
    })
  })
}

test.describe('Onboarding', () => {
  test('existing users are redirected from /#/onboarding to /#/dashboard', async ({
    page,
    baseURL,
  }) => {
    await loginFromStorageState(page, baseURL!, '#/onboarding')

    await expect(page).toHaveURL(/\/#\/dashboard$/)
    await expect(page.locator('.text-h5', { hasText: 'Statistics Dashboard' })).toBeVisible()
  })

  test('onboarding renders the callsign step when the local profile needs onboarding', async ({
    page,
    baseURL,
  }) => {
    await stubNeedsOnboarding(page)
    await loginFromStorageState(page, baseURL!, '#/onboarding', {
      callsign: '',
      display_name: 'Playwright Operator',
      onboarding_complete: false,
    })

    await expect(page).toHaveURL(/\/#\/onboarding$/)
    await expect(
      page.getByText('Set up the logbook you’ll use on the air', { exact: true }),
    ).toBeVisible()
    await expect(
      page.getByText(
        'Use the callsign you normally operate under. Portable and club callsigns can be added later.',
        { exact: true },
      ),
    ).toBeVisible()
    await expect(page.getByLabel('Callsign *')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Continue' })).toBeVisible()
  })

  test('callsign validation blocks advancing with an empty value', async ({ page, baseURL }) => {
    await stubNeedsOnboarding(page)
    await loginFromStorageState(page, baseURL!, '#/onboarding', {
      callsign: '',
      display_name: 'Playwright Operator',
      onboarding_complete: false,
    })

    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByText('Callsign is required', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Callsign *')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Complete Setup' })).not.toBeVisible()
  })
})
