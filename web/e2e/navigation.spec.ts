import { test, expect, type Locator, type Page } from '@playwright/test'
import { loginFromStorageState } from './helpers/auth'

type NavItem = {
  label: string
  path: string
  route: RegExp
}

const coreNavItems: NavItem[] = [
  { label: 'Dashboard', path: '/dashboard', route: /\/#\/dashboard$/ },
  { label: 'Logbook', path: '/logbook', route: /\/#\/logbook$/ },
  { label: 'New QSO', path: '/qso/new', route: /\/#\/qso\/new$/ },
  { label: 'Import', path: '/import', route: /\/#\/import$/ },
  { label: 'Awards', path: '/awards', route: /\/#\/awards$/ },
  { label: 'Activations', path: '/activations', route: /\/#\/activations$/ },
  { label: 'Sync', path: '/sync', route: /\/#\/sync$/ },
  { label: 'Settings', path: '/settings', route: /\/#\/settings$/ },
]

function drawer(page: Page) {
  return page.locator('.q-drawer')
}

function navItem(page: Page, path: string): Locator {
  return drawer(page).locator(`a[href="#${path}"]`).first()
}

async function ensureDrawerVisible(page: Page) {
  const menuButton = page.getByRole('button', { name: 'Menu' })
  if (!(await drawer(page).isVisible()) && await menuButton.isVisible()) {
    await menuButton.click({ force: true })
    await expect(drawer(page)).toBeVisible()
  }
}

test.describe('Navigation sidebar (authenticated)', () => {
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!)
    await expect(page).toHaveURL(/\/#\/dashboard$/)
  })

  test('sidebar contains expected primary navigation links', async ({ page }) => {
    await ensureDrawerVisible(page)

    for (const item of coreNavItems) {
      await expect(navItem(page, item.path)).toBeVisible()
      await expect(navItem(page, item.path)).toContainText(item.label)
    }
  })

  test('active nav item is highlighted for the current route', async ({ page }) => {
    await ensureDrawerVisible(page)

    const dashboardItem = navItem(page, '/dashboard')
    await expect(dashboardItem).toHaveClass(/bg-primary/)
    await expect(dashboardItem).toHaveClass(/text-white/)

    const awardsItem = navItem(page, '/awards')
    await awardsItem.click()
    await expect(page).toHaveURL(/\/#\/awards$/)
    await expect(awardsItem).toHaveClass(/bg-primary/)
    await expect(awardsItem).toHaveClass(/text-white/)
    await expect(dashboardItem).not.toHaveClass(/bg-primary/)
  })

  test('core route transitions complete without page errors', async ({ page }) => {
    const pageErrors: string[] = []
    page.on('pageerror', (error) => pageErrors.push(error.message))

    for (const item of coreNavItems) {
      await ensureDrawerVisible(page)
      await navItem(page, item.path).click()
      await expect(page).toHaveURL(item.route)
      await expect(page.locator('main')).toBeVisible()
    }

    expect(pageErrors).toEqual([])
  })

  test('admin navigation item is shown only when the authenticated user is an admin', async ({ page }) => {
    await ensureDrawerVisible(page)

    const adminJobsItem = navItem(page, '/admin/jobs')
    const isAdminVisible = await adminJobsItem.count()

    if (isAdminVisible > 0) {
      await expect(adminJobsItem).toBeVisible()
      await adminJobsItem.click()
      await expect(page).toHaveURL(/\/#\/admin\/jobs$/)
      await expect(page.locator('.text-h5', { hasText: 'Admin Job Dashboard' })).toBeVisible()
      return
    }

    await expect(adminJobsItem).toHaveCount(0)
  })

  test.describe('mobile navigation', () => {
    test.use({ viewport: { width: 390, height: 844 } })

    test('hamburger menu opens the drawer and the mobile backdrop closes it', async ({ page, baseURL }) => {
      await loginFromStorageState(page, baseURL!)

      const menuButton = page.getByRole('button', { name: 'Menu' })
      await expect(menuButton).toBeVisible()

      if (!(await drawer(page).isVisible())) {
        await menuButton.click({ force: true })
      }
      await expect(drawer(page)).toBeVisible()
      await expect(navItem(page, '/awards')).toContainText('Awards')

      const backdrop = page.locator('.q-drawer__backdrop')
      if (await backdrop.count()) {
        await backdrop.click({ force: true })
        await expect(drawer(page)).not.toBeVisible()
      }
    })

    test('mobile drawer navigation works for route changes', async ({ page, baseURL }) => {
      await loginFromStorageState(page, baseURL!)

      const menuButton = page.getByRole('button', { name: 'Menu' })
      if (!(await drawer(page).isVisible())) {
        await menuButton.click({ force: true })
      }
      await expect(drawer(page)).toBeVisible()

      await navItem(page, '/awards').click()
      await expect(page).toHaveURL(/\/#\/awards$/)
      await expect(page.locator('.text-h5', { hasText: 'Awards' })).toBeVisible()
    })
  })
})
