import { apiGet, apiPost, apiPut } from 'src/api/client'

// ---- Error extraction helper ----
// The backend wraps errors in a JSend envelope: { success: false, message: "...", error: "..." }
// When Axios rejects a non-2xx response it rejects with response.data, so `res.error` could be
// a plain object rather than a string. This helper normalises both cases.
function extractError(res: any, fallback: string): string {
  const e = res?.error
  if (typeof e === 'string') return e
  if (typeof e === 'object' && e !== null && typeof e.message === 'string') return e.message
  return res?.message || fallback
}

// ---- Types ----

export interface CertInfo {
  callsign: string
  cert_not_before: string
  cert_not_after: string
  expired: boolean
  dxcc?: string
  gridsquare?: string
  cqz?: string
  ituz?: string
  qso_start?: string
  qso_end?: string
}

export interface SyncJob {
  id: number
  status: 'pending' | 'signing' | 'uploading' | 'completed' | 'failed'
  qso_count: number
  result?: string
  error?: string
  created_at: string
  completed_at?: string
}

export interface LotwSettings {
  has_cert: boolean
  auto_sync_prompt: boolean
}

// ---- API Functions ----

export async function uploadCert(
  file: File,
  certPassword: string,
): Promise<CertInfo> {
  const formData = new FormData()
  formData.append('cert', file)
  formData.append('cert_password', certPassword)
  const res = await apiPost<CertInfo>('/v1/lotw/cert', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  if (!res.success || !res.data) throw new Error(extractError(res, 'Upload failed'))
  return res.data
}

export async function getCertInfo(): Promise<CertInfo | null> {
  try {
    const res = await apiGet<CertInfo | null>('/v1/lotw/cert')
    if (!res.success) return null
    return res.data ?? null
  } catch (err: unknown) {
    // 404 = no certificate uploaded yet — that's a normal state, not an error
    if (err && typeof err === 'object' && 'status' in err && (err as { status?: number }).status === 404) {
      return null
    }
    throw err
  }
}

export async function deleteCert(): Promise<void> {
  const res = await apiPost('/v1/lotw/cert/delete', {})
  if (!res.success) throw new Error(extractError(res, 'Could not remove certificate'))
}

export async function rotatePassword(oldPassword: string, newPassword: string): Promise<void> {
  const res = await apiPost('/v1/lotw/cert/rotate-password', {
    old_vault_password: oldPassword,
    new_vault_password: newPassword,
  })
  if (!res.success) throw new Error(extractError(res, 'Could not update password'))
}

export async function triggerSync(qsoIds?: number[]): Promise<SyncJob> {
  const res = await apiPost<any>('/v1/lotw/sync', qsoIds?.length ? { qso_ids: qsoIds } : {})
  if (!res.success || !res.data) throw new Error(extractError(res, 'Sync failed'))
  // Backend returns job_id, normalize to id for the SyncJob interface
  const data = res.data
  return {
    ...data,
    id: data.id ?? data.job_id,
  } as SyncJob
}

export async function getSyncStatus(jobId: number): Promise<SyncJob> {
  const res = await apiGet<SyncJob>(`/v1/lotw/sync/status?job_id=${jobId}`)
  if (!res.success || !res.data) throw new Error(extractError(res, 'Could not get sync status'))
  return res.data
}

export async function getPendingCount(): Promise<{ pending_count: number; oldest_unsynced: string }> {
  const res = await apiGet<{ pending_count: number; oldest_unsynced: string }>('/v1/lotw/sync/pending')
  if (!res.success || !res.data) return { pending_count: 0, oldest_unsynced: '' }
  return res.data
}

export async function getSettings(): Promise<LotwSettings> {
  const res = await apiGet<LotwSettings>('/v1/lotw/settings')
  if (!res.success || !res.data) return { has_cert: false, auto_sync_prompt: true }
  return res.data
}

export async function updateSettings(settings: Partial<LotwSettings>): Promise<void> {
  const res = await apiPut('/v1/lotw/settings', settings)
  if (!res.success) throw new Error(extractError(res, 'Could not update settings'))
}

export async function getSyncHistory(page: number, limit: number): Promise<SyncJob[]> {
  const res = await apiGet<{ items: SyncJob[] }>(`/v1/lotw/sync/history?page=${page}&limit=${limit}`)
  if (!res.success || !res.data) return []
  return res.data.items ?? []
}
