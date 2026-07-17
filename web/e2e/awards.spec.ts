import { test, expect } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

test.describe('Awards (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/awards')
  })

  test('navigates to /#/awards and renders the page heading', async ({ page }) => {
    await expect(page).toHaveURL(/\/#\/awards$/)
    await expect(page.locator('.text-h5', { hasText: 'Awards' })).toBeVisible()
    await expect(page.getByText('Track DXCC, WAS, VUCC, WAZ, WPX, POTA, and SOTA progress.')).toBeVisible()
  })

  test('award tabs render for the major tracking views', async ({ page }) => {
    for (const tab of ['DXCC', 'WAS', 'GRIDS', 'WAZ', 'WPX', 'POTA', 'SOTA']) {
      await expect(page.getByRole('tab', { name: tab })).toBeVisible()
    }
  })

  test('default DXCC view renders progress and either rows or an empty state', async ({ page }) => {
    await expect(page.getByText('DXCC Progress', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'REFRESH' })).toBeVisible()

    const dxccTable = page.locator('.q-table').first()
    const dxccEmptyState = page.getByText('No DXCC progress yet', { exact: true })

    if (await dxccTable.count()) {
      await expect(dxccTable).toBeVisible()
      await expect(page.getByRole('columnheader', { name: 'Entity' })).toBeVisible()
    } else {
      await expect(dxccEmptyState).toBeVisible()
    }
  })

  test('switching tabs renders the expected section headings', async ({ page }) => {
    await page.getByRole('tab', { name: 'WAS' }).click()
    await expect(page.getByText('Worked All States', { exact: true })).toBeVisible()

    await page.getByRole('tab', { name: 'POTA' }).click()
    await expect(page.getByText('Hunted Parks', { exact: true })).toBeVisible()
    await expect(page.getByText('Activated Parks', { exact: true })).toBeVisible()

    await page.getByRole('tab', { name: 'SOTA' }).click()
    await expect(page.getByText('Chased Summits', { exact: true })).toBeVisible()
    await expect(page.getByText('Activated Summits', { exact: true })).toBeVisible()
  })
})
