import { test, expect, type Page } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

async function assertAdminRouteBehavior(page: Page) {
  const onAdminJobs = /\/#\/admin\/jobs$/.test(page.url())

  if (onAdminJobs) {
    await expect(page.locator('.text-h5', { hasText: 'Admin Job Dashboard' })).toBeVisible()
    return 'admin'
  }

  await expect(page).toHaveURL(/\/#\/dashboard$/)
  await expect(page.locator('.text-h5', { hasText: 'Statistics Dashboard' })).toBeVisible()
  return 'redirected'
}

test.describe('Admin jobs', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/admin/jobs')
  })

  test('route access either renders the admin dashboard or redirects non-admin users', async ({ page }) => {
    const mode = await assertAdminRouteBehavior(page)

    if (mode === 'redirected') {
      await expect(page.locator('.q-drawer .q-item', { hasText: 'Admin Jobs' })).toHaveCount(0)
    }
  })

  test('admin jobs page renders overview cards, filters, and the job table when accessible', async ({ page }) => {
    const mode = await assertAdminRouteBehavior(page)
    if (mode === 'redirected') {
      return
    }

    await expect(page.getByText('Callsign Source Overview', { exact: true })).toBeVisible()
    await expect(page.getByText('Sync Services', { exact: true })).toBeVisible()
    await expect(page.getByRole('switch', { name: 'Auto-refresh (5s)' })).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Job kind' })).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Trigger kind' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Trigger Now' })).toBeVisible()
    await expect(page.locator('.q-table')).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Kind' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'State' })).toBeVisible()
  })
})
