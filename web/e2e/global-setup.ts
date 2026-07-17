/**
 * global-setup.ts — Playwright global setup.
 *
 * Runs ONCE before the entire test suite (not per-worker).
 * Prefers API login for self-hosted/non-OIDC environments, then falls back to
 * pre-issued auth material from environment variables for OIDC-only deployments.
 *
 * All authenticated tests read cached credentials from e2e/.auth.json via
 * loginFromStorageState() in their beforeEach.
 */

import { chromium, type FullConfig } from '@playwright/test'
import { existsSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, join } from 'path'
import { e2eTestEmail, e2eTestPassword } from './test-credentials'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

export const AUTH_FILE = join(__dirname, '.auth.json')

interface AuthUser extends Record<string, unknown> {
  callsign?: string | null
  onboarding_complete?: boolean
}

interface LoginResponse {
  data: { token: string; user: AuthUser }
}

/**
 * Generate a random amateur radio callsign in the format W5 + 3 alphanumeric chars.
 * Example: W5X4K
 */
function randomCallsign(): string {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZ0123456789'
  let suffix = ''
  for (let i = 0; i < 3; i++) {
    suffix += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return `W5${suffix}`
}

function normalizeUser(user: AuthUser): AuthUser {
  if (!user.callsign) {
    user.callsign = null
  }
  return user
}

function getEnvAuth() {
  const token = process.env.PLAYWRIGHT_AUTH_TOKEN?.trim()
  const userRaw = process.env.PLAYWRIGHT_AUTH_USER?.trim()

  if (!token || !userRaw) {
    return null
  }

  const user = normalizeUser(JSON.parse(userRaw) as AuthUser)
  return { token, user }
}

async function tryApiLogin(baseURL: string) {
  const browser = await chromium.launch()

  try {
    const context = await browser.newContext({ baseURL })
    const page = await context.newPage()

    const response = await page.request.post(`${baseURL}/v1/auth/login`, {
      data: { email: e2eTestEmail, password: e2eTestPassword },
    })

    if (!response.ok()) {
      return null
    }

    const body = (await response.json()) as LoginResponse
    return {
      token: body.data.token,
      user: normalizeUser(body.data.user),
    }
  } finally {
    await browser.close()
  }
}

/**
 * Complete the onboarding flow via Playwright if the app redirects to #/onboarding.
 * Uses a realistic random callsign, accepts whatever grid square the lookup provides
 * (or skips it), and keeps the default logbook name.
 */
async function completeOnboardingIfNeeded(
  page: import('@playwright/test').Page,
  baseURL: string,
): Promise<void> {
  const url = page.url()
  if (!url.includes('#/onboarding') && !url.includes('/onboarding')) {
    return
  }

  // Step 1: Callsign
  const callsignInput = page.getByLabel('Callsign *')
  await callsignInput.waitFor({ state: 'visible', timeout: 10_000 })

  const callsign = randomCallsign()
  await callsignInput.fill(callsign)
  await callsignInput.blur()

  // Wait for callsign availability check to complete
  await page.waitForFunction(
    () => !document.querySelector('.q-field--loading'),
    { timeout: 10_000 },
  ).catch(() => {
    // If no loading indicator appeared, that's fine — check passed quickly
  })

  // Click Continue to advance to grid square step
  await page.getByRole('button', { name: 'Continue' }).click()

  // If there's an error (callsign unavailable), try again with a new one
  const errorVisible = await page.getByText('That callsign is unavailable').isVisible().catch(() => false)
  if (errorVisible || await page.getByLabel('Callsign *').isVisible().catch(() => false)) {
    // Try a few more callsigns if the first one was taken
    for (let attempt = 0; attempt < 5; attempt++) {
      const retryCallsign = randomCallsign()
      await callsignInput.fill(retryCallsign)
      await callsignInput.blur()
      await page.waitForFunction(
        () => !document.querySelector('.q-field--loading'),
        { timeout: 10_000 },
      ).catch(() => {})
      await page.getByRole('button', { name: 'Continue' }).click()
      const stillError = await page.getByLabel('Callsign *').isVisible().catch(() => false)
      if (!stillError) break
    }
  }

  // Step 2: Grid Square — wait for step 2 to appear
  await page.waitForFunction(
    () => {
      const buttons = Array.from(document.querySelectorAll('button'))
      return buttons.some((b) => b.textContent?.includes('Continue') || b.textContent?.includes('Back'))
    },
    { timeout: 10_000 },
  ).catch(() => {})

  // The grid square may have been auto-populated from callsign lookup — just continue
  await page.getByRole('button', { name: 'Continue' }).last().click()

  // Step 3: Default Logbook — wait for "Complete Setup" button
  await page.getByRole('button', { name: 'Complete Setup' }).waitFor({ state: 'visible', timeout: 10_000 })
  await page.getByRole('button', { name: 'Complete Setup' }).click()

  // Wait for redirect to dashboard
  await page.waitForURL(/\/#\/dashboard/, { timeout: 15_000 })
}

export default async function globalSetup(config: FullConfig) {
  if (process.env.PLAYWRIGHT_REUSE_AUTH === '1' && existsSync(AUTH_FILE)) {
    return
  }

  const baseURL = config.projects[0]?.use?.baseURL ?? 'http://localhost:9000'

  const auth = (await tryApiLogin(baseURL)) ?? getEnvAuth()

  if (!auth) {
    throw new Error(
      `globalSetup: could not authenticate against ${baseURL}. ` +
        'API login is unavailable and PLAYWRIGHT_AUTH_TOKEN/PLAYWRIGHT_AUTH_USER were not provided.',
    )
  }

  const browser = await chromium.launch()
  const context = await browser.newContext({ baseURL })
  const page = await context.newPage()

  await page.addInitScript(
    ({ token, user }: { token: string; user: Record<string, unknown> }) => {
      localStorage.setItem('radioledger.token', token)
      localStorage.setItem('radioledger.user', JSON.stringify(user))
    },
    auth,
  )

  await page.goto(`${baseURL}/#/dashboard`, { waitUntil: 'networkidle' })

  // If the app redirected to onboarding, complete the flow before caching state.
  await completeOnboardingIfNeeded(page, baseURL)

  await context.storageState({ path: AUTH_FILE })
  await browser.close()
}
