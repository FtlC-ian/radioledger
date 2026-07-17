/**
 * import.spec.ts — authenticated tests for the Import page.
 *
 * Route: /#/import
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import { dirname, join } from 'path'
import { loginFromStorageState } from './helpers/auth'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const GOOD_FIXTURE = join(__dirname, 'fixtures', 'test-import.adi')
const MALFORMED_FIXTURE = join(__dirname, 'fixtures', 'test-import-malformed.adi')

const FIXTURE_QSOS = [
  { callsign: 'RLTST1', datetime: '2099-03-15T01:02:03.000Z' },
  { callsign: 'RLTST2', datetime: '2099-03-15T01:04:05.000Z' },
  { callsign: 'RLTST3', datetime: '2099-03-15T01:06:07.000Z' },
  { callsign: 'RLTST4', datetime: '2099-03-15T01:08:09.000Z' },
] as const

const FIXTURE_CALLSIGNS = FIXTURE_QSOS.map((qso) => qso.callsign)
const FIXTURE_TIMESTAMPS = new Set(FIXTURE_QSOS.map((qso) => qso.datetime))

interface ApiEnvelope<T> {
  success: boolean
  data: T
  error?: string
}

interface DefaultLogbookResponse {
  uuid: string
}

interface ImportJobAccepted {
  job_uuid: string
  status_url: string
}

interface ImportJobStatus {
  status: string
  imported: number
  duplicate: number
  skipped: number
  errors: number
}

interface ApiQso {
  uuid: string
  callsign: string
  datetime_on?: string
  qso_datetime?: string
}

interface QsoListPayload {
  items?: ApiQso[]
  qsos?: ApiQso[]
}

async function authHeaders(page: Page) {
  const token = await page.evaluate(() => localStorage.getItem('radioledger.token'))
  expect(token, 'expected auth token in localStorage').toBeTruthy()

  return {
    Authorization: `Bearer ${token}`,
  }
}

function apiUrl(baseURL: string, path: string) {
  return new URL(path, `${baseURL}/`).toString()
}

async function getDefaultLogbookUUID(page: Page, baseURL: string) {
  const response = await page.request.get(apiUrl(baseURL, '/v1/logbooks/default'), {
    headers: await authHeaders(page),
  })

  expect(response.ok(), 'expected /v1/logbooks/default to succeed').toBeTruthy()

  const body = (await response.json()) as ApiEnvelope<DefaultLogbookResponse>
  expect(body.success).toBeTruthy()
  expect(body.data.uuid).toBeTruthy()

  return body.data.uuid
}

function extractQsos(payload: QsoListPayload | ApiQso[] | null | undefined) {
  if (Array.isArray(payload)) {
    return payload
  }

  if (Array.isArray(payload?.items)) {
    return payload.items
  }

  if (Array.isArray(payload?.qsos)) {
    return payload.qsos
  }

  return []
}

function normalizeTimestamp(value: string | undefined) {
  if (!value) {
    return null
  }

  return new Date(value).toISOString()
}

async function findFixtureQsos(page: Page, baseURL: string, logbookUUID: string) {
  const headers = await authHeaders(page)
  const matches = new Map<string, ApiQso>()

  for (const callsign of FIXTURE_CALLSIGNS) {
    const response = await page.request.get(
      apiUrl(baseURL, `/v1/logbooks/${logbookUUID}/qsos?callsign=${encodeURIComponent(callsign)}&limit=100`),
      { headers },
    )

    expect(response.ok(), `expected QSO lookup for ${callsign} to succeed`).toBeTruthy()

    const body = (await response.json()) as ApiEnvelope<QsoListPayload | ApiQso[]>
    expect(body.success).toBeTruthy()

    for (const qso of extractQsos(body.data)) {
      const timestamp = normalizeTimestamp(qso.datetime_on || qso.qso_datetime)
      if (qso.callsign === callsign && timestamp && FIXTURE_TIMESTAMPS.has(timestamp)) {
        matches.set(qso.uuid, qso)
      }
    }
  }

  return Array.from(matches.values())
}

async function deleteFixtureQsos(page: Page, baseURL: string, logbookUUID: string) {
  const headers = await authHeaders(page)
  const fixtureQsos = await findFixtureQsos(page, baseURL, logbookUUID)

  for (const qso of fixtureQsos) {
    const response = await page.request.delete(apiUrl(baseURL, `/v1/logbooks/${logbookUUID}/qsos/${qso.uuid}`), {
      headers,
    })

    expect(response.ok(), `expected delete for fixture QSO ${qso.uuid} to succeed`).toBeTruthy()
  }
}

async function uploadFixture(page: Page, fixturePath: string) {
  await page.locator('input[type="file"]').first().setInputFiles(fixturePath)

  const [uploadResponse] = await Promise.all([
    page.waitForResponse(
      (response) => response.request().method() === 'POST' && response.url().includes('/v1/import/adif'),
    ),
    page.getByRole('button', { name: /^upload$/i }).click(),
  ])

  expect(uploadResponse.status()).toBe(202)

  const body = (await uploadResponse.json()) as ApiEnvelope<ImportJobAccepted>
  expect(body.success).toBeTruthy()
  expect(body.data.job_uuid).toBeTruthy()

  return body.data
}

async function getImportStatus(page: Page, baseURL: string, jobUUID: string) {
  const response = await page.request.get(apiUrl(baseURL, `/v1/import/${jobUUID}`), {
    headers: await authHeaders(page),
  })

  expect(response.ok(), `expected import status lookup for ${jobUUID} to succeed`).toBeTruthy()

  const body = (await response.json()) as ApiEnvelope<ImportJobStatus>
  expect(body.success).toBeTruthy()
  return body.data
}

async function waitForImportJobStatus(
  page: Page,
  baseURL: string,
  jobUUID: string,
  expectedStatuses: string[],
  timeoutMs = 60_000,
) {
  const startedAt = Date.now()
  let latestStatus: ImportJobStatus | null = null

  while (Date.now() - startedAt < timeoutMs) {
    latestStatus = await getImportStatus(page, baseURL, jobUUID)
    if (expectedStatuses.includes(latestStatus.status)) {
      return latestStatus
    }
    if (['complete', 'completed', 'success', 'error', 'failed', 'cancelled'].includes(latestStatus.status)) {
      throw new Error(
        `Import job ${jobUUID} reached unexpected terminal status ${latestStatus.status} ` +
          `(imported=${latestStatus.imported}, duplicate=${latestStatus.duplicate}, errors=${latestStatus.errors})`,
      )
    }
    await page.waitForTimeout(1500)
  }

  throw new Error(
    `Import job ${jobUUID} did not reach ${expectedStatuses.join(', ')} within ${timeoutMs}ms. ` +
      `Last observed status: ${latestStatus?.status ?? 'unknown'}`,
  )
}

async function waitForImportResult(page: Page, status: 'complete' | 'failed') {
  const expectedText = status === 'complete' ? 'Import Complete' : 'Import Failed'

  await expect
    .poll(
      async () => {
        const heading = page.locator('.text-subtitle1').filter({ hasText: expectedText }).first()
        return (await heading.count()) > 0 && (await heading.isVisible())
      },
      {
        timeout: 15_000,
        message: `Timed out waiting for ${expectedText}`,
      },
    )
    .toBe(true)

  return page.locator('.text-subtitle1').filter({ hasText: expectedText }).first()
}

test.describe('Import (authenticated)', () => {
  test.describe.configure({ mode: 'serial' })
  test.beforeEach(async ({ page, baseURL }) => {
    await loginFromStorageState(page, baseURL!, '#/import')
  })

  test('uploads a valid ADIF file and shows imported QSOs in the logbook', async ({ page, baseURL }) => {
    test.slow()

    const resolvedBaseURL = baseURL!
    const logbookUUID = await getDefaultLogbookUUID(page, resolvedBaseURL)

    await deleteFixtureQsos(page, resolvedBaseURL, logbookUUID)

    try {
      await expect(page).toHaveURL(/\/#\/import/)
      await expect(page.locator('.text-h5', { hasText: 'Import Contacts' })).toBeVisible()
      await expect(page.getByText('ADIF Import', { exact: true })).toBeVisible()

      const importJob = await uploadFixture(page, GOOD_FIXTURE)
      const importStatus = await waitForImportJobStatus(page, resolvedBaseURL, importJob.job_uuid, ['complete'])
      expect(importStatus.imported).toBe(FIXTURE_QSOS.length)

      const completeHeading = await waitForImportResult(page, 'complete')
      await expect(completeHeading).toBeVisible()

      const resultSection = page.locator('.q-card__section').filter({ hasText: 'Import Complete' }).last()
      await expect(resultSection).toContainText('Imported')
      await expect(resultSection).toContainText('4')
      await expect(page.getByRole('link', { name: /view logbook/i })).toBeVisible()

      await expect.poll(async () => (await findFixtureQsos(page, resolvedBaseURL, logbookUUID)).length, {
        timeout: 30_000,
        message: 'Imported fixture QSOs were not found via the API',
      }).toBe(FIXTURE_QSOS.length)

      await page.getByRole('link', { name: /view logbook/i }).click()
      await expect(page).toHaveURL(/\/#\/logbook/)
      await expect(page.getByText('Logbook Viewer', { exact: true })).toBeVisible()

      for (const { callsign } of FIXTURE_QSOS) {
        await expect(page.getByText(callsign, { exact: true }).first()).toBeVisible()
      }
    } finally {
      await deleteFixtureQsos(page, resolvedBaseURL, logbookUUID)
    }
  })

  test('shows a graceful zero-import result for a malformed ADIF file', async ({ page, baseURL }) => {
    test.slow()

    const resolvedBaseURL = baseURL!

    await expect(page).toHaveURL(/\/#\/import/)
    await expect(page.getByText('ADIF Import', { exact: true })).toBeVisible()

    const importJob = await uploadFixture(page, MALFORMED_FIXTURE)
    const importStatus = await waitForImportJobStatus(page, resolvedBaseURL, importJob.job_uuid, ['complete', 'completed', 'success'])
    expect(importStatus.imported).toBe(0)
    expect(importStatus.duplicate).toBe(0)
    expect(importStatus.errors).toBe(0)

    const completeHeading = await waitForImportResult(page, 'complete')
    await expect(completeHeading).toBeVisible()

    const resultSection = page.locator('.q-card__section').filter({ hasText: 'Import Complete' }).last()
    await expect(resultSection).toContainText('Imported')
    await expect(resultSection).toContainText('0')
    await expect(page.getByRole('link', { name: /view logbook/i })).toBeVisible()
  })
})
