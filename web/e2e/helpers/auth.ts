/**
 * auth.ts — Auth helper for P1 authenticated E2E tests.
 *
 * Auth credentials are obtained ONCE by global-setup.ts, which saves
 * Playwright browser storage state (localStorage + cookies) to e2e/.auth.json.
 *
 * Tests use loginFromStorageState() in their beforeEach.
 * It reads the cached token/user from .auth.json and injects them into the
 * page's localStorage via addInitScript before the Vue app boots.
 *
 * Using addInitScript is necessary because Playwright's storageState only
 * reliably covers cookies across contexts; for hash-SPA localStorage we
 * inject values before the page's own scripts run.
 */

import { readFileSync } from 'fs'
import { fileURLToPath } from 'url'
import { dirname, join } from 'path'
import type { Page } from '@playwright/test'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

export const AUTH_FILE = join(__dirname, '../.auth.json')

interface StorageEntry {
  name: string
  value: string
}

interface StorageState {
  origins: Array<{
    origin: string
    localStorage: StorageEntry[]
  }>
}

/**
 * Read cached auth credentials from .auth.json without injecting them.
 * Useful for tests that need the raw token/user (e.g. auth-expiry tests).
 */
export function readAuthFromStorageState(baseURL: string): {
  token: string
  user: Record<string, unknown>
} {
  const raw = readFileSync(AUTH_FILE, 'utf-8')
  const state = JSON.parse(raw) as StorageState

  const origin = new URL(baseURL).origin
  const entries = state.origins.find((o) => o.origin === origin)?.localStorage ?? []

  const token = entries.find((e) => e.name === 'radioledger.token')?.value
  const userRaw = entries.find((e) => e.name === 'radioledger.user')?.value

  if (!token || !userRaw) {
    throw new Error(
      `readAuthFromStorageState: no token/user found in ${AUTH_FILE} for origin ${origin}. ` +
        'Did globalSetup run? Make sure globalSetup is configured in playwright.config.ts.',
    )
  }

  return {
    token,
    user: JSON.parse(userRaw) as Record<string, unknown>,
  }
}

/**
 * Inject cached auth credentials into page localStorage via addInitScript,
 * then navigate to the target path.
 *
 * @param page         Playwright page
 * @param baseURL      Base URL from Playwright config (for example, http://localhost:9000)
 * @param targetPath   Hash path to navigate to (default: '#/dashboard')
 * @param userOverride Optional user-profile overrides applied before injection
 */
export async function loginFromStorageState(
  page: Page,
  baseURL: string,
  targetPath = '#/dashboard',
  userOverride?: Record<string, unknown>,
): Promise<void> {
  const { token, user: storedUser } = readAuthFromStorageState(baseURL)
  const user = { onboarding_complete: true, ...(userOverride ? { ...storedUser, ...userOverride } : storedUser) }

  await page.addInitScript(
    ({ token, user }: { token: string; user: Record<string, unknown> }) => {
      localStorage.setItem('radioledger.token', token)
      localStorage.setItem('radioledger.user', JSON.stringify(user))
    },
    { token, user },
  )

  await page.goto(`${baseURL}/${targetPath}`, { waitUntil: 'networkidle' })
}
