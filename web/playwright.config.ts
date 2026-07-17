import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E configuration for the RadioLedger web app.
 *
 * Tests run against PLAYWRIGHT_BASE_URL (default: http://localhost:9000, Quasar dev port).
 * For a deployed environment: PLAYWRIGHT_BASE_URL=https://your-radioledger.example pnpm exec playwright test
 */
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  maxFailures: process.env.CI ? 1 : 0,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:9000',
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
