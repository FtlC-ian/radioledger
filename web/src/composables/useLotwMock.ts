/**
 * useLotwMock — fake data for LoTW UI development.
 *
 * Replace these with real lotwApi.ts calls when the backend is wired up.
 * Toggle LOTW_USE_MOCK to false (or remove entirely) to switch to live API.
 */

import type { CertInfo, LotwSettings, SyncJob } from 'src/services/lotwApi'

// Controlled via VITE_LOTW_MOCK env variable.
// Set VITE_LOTW_MOCK=true in .env.development to enable; never set it in .env.production.
export const LOTW_USE_MOCK = import.meta.env.VITE_LOTW_MOCK === 'true'

// ---- Mock data ----

export const mockCertInfo: CertInfo = {
  callsign: 'W1AW',
  cert_not_before: new Date(Date.now() - 365 * 24 * 60 * 60 * 1000).toISOString(),
  cert_not_after: new Date(Date.now() + 45 * 24 * 60 * 60 * 1000).toISOString(), // 45 days from now — triggers warning
  expired: false,
  gridsquare: 'FN31',
  dxcc: '291',
  cqz: '5',
  ituz: '8',
}

// Swap to this to test the "no cert" state:
// export const mockCertInfo: CertInfo | null = null

export const mockSettings: LotwSettings = {
  has_cert: true,
  auto_sync_prompt: true,
}

export const mockPendingCount = { pending_count: 42, oldest_unsynced: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString() }

export const mockSyncHistory: SyncJob[] = [
  {
    id: 1,
    status: 'completed',
    qso_count: 158,
    result: '158 QSOs uploaded successfully',
    created_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(Date.now() - 2 * 24 * 60 * 60 * 1000 + 12000).toISOString(),
  },
  {
    id: 2,
    status: 'failed',
    qso_count: 23,
    error: 'LoTW upload rejected: certificate expired',
    created_at: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(Date.now() - 5 * 24 * 60 * 60 * 1000 + 4000).toISOString(),
  },
  {
    id: 3,
    status: 'completed',
    qso_count: 312,
    result: '312 QSOs uploaded successfully',
    created_at: new Date(Date.now() - 14 * 24 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(Date.now() - 14 * 24 * 60 * 60 * 1000 + 27000).toISOString(),
  },
]

// ---- Simulated async helpers ----

export async function mockUploadCert(
  _file: File,
  _certPassword: string,
): Promise<CertInfo> {
  await delay(1200)
  // Uncomment to simulate an error:
  // throw new Error('wrong_cert_password')
  return { ...mockCertInfo }
}

export async function mockTriggerSync(
  _qsoIds?: number[],
): Promise<SyncJob> {
  await delay(800)
  return {
    id: 99,
    status: 'pending',
    qso_count: mockPendingCount.pending_count,
    created_at: new Date().toISOString(),
  }
}

export async function mockGetSyncStatus(jobId: number): Promise<SyncJob> {
  await delay(600)
  // Simulate progress through states
  const states: SyncJob['status'][] = ['signing', 'uploading', 'completed']
  const idx = Math.min(Math.floor(Math.random() * states.length), states.length - 1)
  return {
    id: jobId,
    status: states[idx]!,
    qso_count: mockPendingCount.pending_count,
    result: idx === states.length - 1 ? `${mockPendingCount.pending_count} QSOs uploaded successfully` : undefined,
    created_at: new Date().toISOString(),
    completed_at: idx === states.length - 1 ? new Date().toISOString() : undefined,
  }
}

export async function mockUpdateSettings(settings: Partial<LotwSettings>): Promise<void> {
  await delay(300)
  Object.assign(mockSettings, settings)
}

export async function mockDeleteCert(): Promise<void> {
  await delay(400)
  // Certificate deletion succeeds in mock mode.
}

// ---- Utility ----

function delay(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms))
}
