import { defineConfig, devices } from '@playwright/test'

/**
 * playwright.docs.config.ts
 *
 * Playwright E2E configuration for the RadioLedger VitePress documentation site.
 *
 * Runs against the VitePress preview server (built static site) at port 4173.
 * The VitePress config sets base: '/docs/', so tests hit http://localhost:4173/docs/.
 *
 * Usage:
 *   # Build and preview docs:
 *   cd radioledger/docs && pnpm run docs:build && pnpm run docs:preview -- --port 4173 --host &
 *   # Wait for server, then:
 *   cd radioledger/web && pnpm exec playwright test --config playwright.docs.config.ts
 *
 * CI: see the public GitHub Actions workflow for docs checks.
 *
 * NOTE: This config has NO globalSetup — docs tests are anonymous (no login required).
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: ['**/docs-site.spec.ts'],
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: process.env.DOCS_BASE_URL ?? 'http://localhost:4173',
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // No webServer block: the CI job starts vitepress preview before running playwright.
  // For local use, start the preview server manually first.
})
