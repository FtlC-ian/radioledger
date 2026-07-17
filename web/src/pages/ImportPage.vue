<template>
  <q-page class="q-pa-md">
    <div class="text-h5 q-mb-md">Import Contacts</div>

    <ImportUpload :loading="loading" :progress="progress" :result="result" :stats="liveStats" @upload="uploadFile" />

    <q-card flat bordered class="q-mt-md">
      <q-card-section class="row items-center justify-between q-gutter-md">
        <div>
          <div class="text-h6">QRZ Migration</div>
          <div class="text-body2 text-grey-6">Import your full QRZ logbook in one step.</div>
        </div>
        <q-btn color="secondary" icon="cloud_download" label="Import from QRZ" :disable="loading" @click="openQRZDialog" />
      </q-card-section>
    </q-card>

    <q-dialog v-model="qrzDialog">
      <q-card style="min-width: 420px; max-width: 95vw">
        <q-card-section>
          <div class="text-h6">Import from QRZ</div>
          <div class="text-body2 text-grey-7 q-mt-sm">
            <template v-if="qrzCheckingCredentials">Checking saved QRZ credentials…</template>
            <template v-else-if="hasSavedQRZCredential">Import all QSOs from your saved QRZ Logbook API key.</template>
            <template v-else-if="savedQRZCredentialType === 'username_password'">
              A saved QRZ username/password was found, but full logbook import still needs a QRZ Logbook API key.
            </template>
            <template v-else>No saved QRZ Logbook API key found. Enter your QRZ API key.</template>
          </div>
        </q-card-section>

        <q-card-section class="q-pt-none">
          <q-input
            v-if="!hasSavedQRZCredential"
            v-model="qrzAPIKey"
            outlined
            type="password"
            autocomplete="off"
            label="QRZ Logbook API Key"
            hint="Found in QRZ Logbook settings"
          />
        </q-card-section>

        <q-card-actions align="right">
          <q-btn flat label="Cancel" :disable="qrzLoading" v-close-popup />
          <q-btn
            color="primary"
            label="Start Import"
            :loading="qrzLoading"
            :disable="qrzCheckingCredentials || (!hasSavedQRZCredential && !qrzAPIKey.trim())"
            @click="startQRZImport"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useQuasar } from 'quasar'
import ImportUpload from 'src/components/ImportUpload.vue'
import api, { API_BASE_URL, apiGet } from 'src/api/client'
import { absoluteApiUrl } from 'src/api/url'
import { useCredentials } from 'src/composables/useCredentials'
import type { ApiResponse } from 'src/types/qso'

interface ImportJobAccepted {
  job_uuid?: string
  status_url?: string
}

interface ImportLiveStats {
  processed: number
  total: number
  imported: number
  duplicates: number
  errors: number
}

interface StreamTokenResponse {
  token?: string
}

const $q = useQuasar()
const route = useRoute()

const loading = ref(false)
const progress = ref(0)
const result = ref<Record<string, unknown> | null>(null)
const liveStats = ref<ImportLiveStats | null>(null)

const defaultLogbookUUID = ref<string | null>(null)
const qrzDialog = ref(false)
const qrzLoading = ref(false)
const qrzCheckingCredentials = ref(false)
const qrzAPIKey = ref('')

const { credentials, loadCredentials } = useCredentials()
const savedQRZCredentialType = computed(() => credentials.value.qrz?.credential_type || null)
const hasSavedQRZCredential = computed(() => savedQRZCredentialType.value === 'api_key')

let activeJobUUID = ''
let activeImportLabel = 'ADIF'
let pollTimer: ReturnType<typeof setInterval> | null = null
let eventSource: EventSource | null = null
let streamCompleted = false

function isTerminalImportStatus(status: string) {
  return status === 'complete' || status === 'completed' || status === 'success' || status === 'error' || status === 'failed' || status === 'cancelled'
}

function closeEventSource() {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function resetTracking() {
  closeEventSource()
  stopPolling()
  streamCompleted = false
}

function asNumber(value: unknown, fallback = 0): number {
  const n = Number(value)
  return Number.isFinite(n) ? n : fallback
}

function updateFromProgressPayload(payload: Record<string, unknown>) {
  const imported = asNumber(payload.imported)
  const duplicates = asNumber(payload.duplicates ?? payload.duplicate)
  const errors = asNumber(payload.errors)
  const total = Math.max(asNumber(payload.total, imported + duplicates + errors), 1)
  const processed = asNumber(payload.processed, imported + duplicates + errors)
  const percent = asNumber(payload.percent, (processed / total) * 100)

  liveStats.value = {
    processed,
    total,
    imported,
    duplicates,
    errors,
  }

  progress.value = Math.min(1, Math.max(0, percent / 100))
}

function updateFromStatusPayload(payload: Record<string, unknown>) {
  const imported = asNumber(payload.imported)
  const duplicates = asNumber(payload.duplicate ?? payload.duplicates)
  const errors = asNumber(payload.errors)
  const skipped = asNumber(payload.skipped)
  const total = Math.max(asNumber(payload.total_records, imported + duplicates + errors + skipped), 1)
  const processed = imported + duplicates + errors + skipped
  const percent = asNumber(payload.pct_complete ?? payload.percent, (processed / total) * 100)

  liveStats.value = {
    processed,
    total,
    imported,
    duplicates,
    errors,
  }

  progress.value = Math.min(1, Math.max(0, percent / 100))
}

async function ensureDefaultLogbookUUID(): Promise<string> {
  if (defaultLogbookUUID.value) {
    return defaultLogbookUUID.value
  }

  const response = await apiGet<{ uuid: string }>('/v1/logbooks/default')
  if (!response.success || !response.data?.uuid) {
    throw new Error(response.error || 'No default logbook found')
  }

  const uuid = response.data.uuid
  defaultLogbookUUID.value = uuid
  return uuid
}

function finalizeImportSuccess(payload: Record<string, unknown>) {
  loading.value = false
  progress.value = 1

  const imported = asNumber(payload.imported)
  result.value = {
    status: 'completed',
    imported,
    duplicates: asNumber(payload.duplicates ?? payload.duplicate),
    errors: asNumber(payload.errors),
    message: payload.message ?? `${activeImportLabel} import complete`,
  }

  $q.notify({
    type: 'positive',
    message: `Imported ${imported.toLocaleString()} QSOs from ${activeImportLabel}`,
  })
}

function finalizeImportFailure(payload: Record<string, unknown>, fallbackMessage: string) {
  loading.value = false
  result.value = {
    status: 'failed',
    imported: asNumber(payload.imported),
    duplicates: asNumber(payload.duplicates ?? payload.duplicate),
    errors: asNumber(payload.errors),
    message: payload.error ?? payload.message ?? fallbackMessage,
  }
  $q.notify({ type: 'negative', message: String(payload.error ?? payload.message ?? fallbackMessage) })
}

function startPolling(statusURL: string) {
  stopPolling()
  pollTimer = setInterval(() => {
    void pollImportStatus(statusURL)
  }, 1500)
}

function extractPayloadData(response: ApiResponse<Record<string, unknown>>): Record<string, unknown> | null {
  if (!response.success || !response.data || typeof response.data !== 'object') {
    return null
  }

  return response.data
}

function parseSSEData(event: MessageEvent): Record<string, unknown> {
  try {
    return JSON.parse(String(event.data || '{}')) as Record<string, unknown>
  } catch {
    return {}
  }
}

async function requestStreamToken(jobUUID: string): Promise<string | null> {
  try {
    const response = await api.post<ApiResponse<StreamTokenResponse>>('/v1/stream-token', {
      path: `/v1/import/${jobUUID}/stream`,
    })
    if (!response.data.success) {
      return null
    }

    const token = String(response.data.data?.token || '').trim()
    return token || null
  } catch {
    return null
  }
}

async function pollImportStatusOnce(statusUrl: string): Promise<boolean> {
  try {
    const response = await apiGet<Record<string, unknown>>(statusUrl)
    const payload = extractPayloadData(response)
    if (!payload) {
      return false
    }

    updateFromStatusPayload(payload)

    const status = String(payload.status || '').toLowerCase()
    if (!isTerminalImportStatus(status)) {
      return false
    }

    resetTracking()
    if (status === 'complete' || status === 'completed' || status === 'success') {
      finalizeImportSuccess(payload)
    } else {
      finalizeImportFailure(payload, 'Import failed')
    }
    return true
  } catch {
    // Ignore one-shot preflight errors and continue with SSE/poll fallback.
  }

  return false
}

async function handleSSEError(event: Event, statusURL: string) {
  if (streamCompleted) {
    return
  }

  closeEventSource()

  const finished = await pollImportStatusOnce(statusURL)
  if (finished) {
    return
  }

  if (event instanceof MessageEvent && String(event.data || '').trim() !== '') {
    const payload = parseSSEData(event)
    updateFromProgressPayload(payload)
    resetTracking()
    finalizeImportFailure(payload, 'Import failed')
    return
  }

  if (!pollTimer) {
    startPolling(statusURL)
  }
}

async function startSSE(jobUUID: string, statusURL: string) {
  closeEventSource()

  const token = await requestStreamToken(jobUUID)
  if (!token) {
    return
  }

  const streamURL = new URL(absoluteApiUrl(`/v1/import/${jobUUID}/stream`, API_BASE_URL, window.location.origin))
  streamURL.searchParams.set('stream_token', token)

  streamCompleted = false
  eventSource = new EventSource(streamURL.toString())

  eventSource.addEventListener('progress', (event: Event) => {
    if (!(event instanceof MessageEvent)) {
      return
    }
    updateFromProgressPayload(parseSSEData(event))
  })

  eventSource.addEventListener('complete', (event: Event) => {
    if (!(event instanceof MessageEvent)) {
      return
    }

    streamCompleted = true
    closeEventSource()
    stopPolling()

    const payload = parseSSEData(event)
    updateFromProgressPayload({ ...payload, percent: 100 })
    finalizeImportSuccess(payload)
  })

  eventSource.onerror = (event: Event) => {
    void handleSSEError(event, statusURL)
  }
}

async function trackImport(jobUUID: string, statusURL: string, label = 'ADIF') {
  activeJobUUID = jobUUID
  activeImportLabel = label
  loading.value = true

  const finished = await pollImportStatusOnce(statusURL)
  if (finished) {
    return
  }

  startPolling(statusURL)
  void startSSE(jobUUID, statusURL)
}

async function uploadFile(file: File) {
  loading.value = true
  progress.value = 0
  result.value = null
  liveStats.value = null
  resetTracking()

  try {
    const logbookUUID = await ensureDefaultLogbookUUID()

    const formData = new FormData()
    formData.append('file', file)
    formData.append('logbook_uuid', logbookUUID)

    const response = await api.post<ApiResponse<ImportJobAccepted>>('/v1/import/adif', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    })

    if (!response.data.success) {
      throw new Error(response.data.error || 'Import failed')
    }

    const payload = response.data.data || {}
    const jobUUID = String(payload.job_uuid || '')
    const statusUrl = String(payload.status_url || (jobUUID ? `/v1/import/${jobUUID}` : ''))

    if (!jobUUID || !statusUrl) {
      loading.value = false
      progress.value = 1
      result.value = payload as Record<string, unknown>
      return
    }

    void trackImport(jobUUID, statusUrl, 'ADIF')
  } catch {
    loading.value = false
    $q.notify({ type: 'negative', message: 'Import upload failed' })
  }
}

async function openQRZDialog() {
  qrzDialog.value = true
  qrzAPIKey.value = ''
  qrzCheckingCredentials.value = true

  try {
    await loadCredentials()
  } finally {
    qrzCheckingCredentials.value = false
  }
}

async function startQRZImport() {
  if (qrzLoading.value) {
    return
  }

  qrzLoading.value = true
  loading.value = true
  progress.value = 0
  result.value = null
  liveStats.value = null
  resetTracking()

  try {
    const logbookUUID = await ensureDefaultLogbookUUID()

    const payload: Record<string, unknown> = {
      logbook_uuid: logbookUUID,
    }

    // Only include api_key if user is providing new credentials
    if (!hasSavedQRZCredential.value) {
      payload.api_key = qrzAPIKey.value.trim()
    }

    const response = await api.post<ApiResponse<ImportJobAccepted>>('/v1/import/qrz', payload)
    if (!response.data.success) {
      throw new Error(response.data.error || 'QRZ import failed')
    }

    const data = response.data.data || {}
    const jobUUID = String(data.job_uuid || '')
    const statusUrl = String(data.status_url || (jobUUID ? `/v1/import/${jobUUID}` : ''))
    if (!jobUUID || !statusUrl) {
      throw new Error('QRZ import did not return a valid job status URL')
    }

    qrzDialog.value = false
    void trackImport(jobUUID, statusUrl, 'QRZ')
  } catch (error) {
    loading.value = false
    $q.notify({ type: 'negative', message: error instanceof Error ? error.message : 'QRZ import failed' })
  } finally {
    qrzLoading.value = false
  }
}

async function pollImportStatus(statusUrl: string) {
  try {
    const response = await apiGet<Record<string, unknown>>(statusUrl)
    const payload = extractPayloadData(response)
    if (!payload) {
      return
    }

    updateFromStatusPayload(payload)

    const status = String(payload.status || '').toLowerCase()
    if (!isTerminalImportStatus(status)) {
      return
    }

    resetTracking()
    if (status === 'complete' || status === 'completed' || status === 'success') {
      finalizeImportSuccess(payload)
      return
    }

    finalizeImportFailure(payload, 'Import failed')
  } catch {
    // Keep the spinner alive and let the next poll/SSE event recover from transient failures.
  }
}

async function openImportJobFromRoute(jobUUID: string) {
  if (!jobUUID || jobUUID === activeJobUUID || loading.value) {
    return
  }

  progress.value = 0
  result.value = null
  liveStats.value = null
  resetTracking()

  const statusURL = `/v1/import/${jobUUID}`
  void trackImport(jobUUID, statusURL)
}

onMounted(() => {
  void ensureDefaultLogbookUUID().catch(() => {
    // Ignore eagerly; upload path will show a user-facing error if missing.
  })

  const jobFromRoute = route.query.job
  if (typeof jobFromRoute === 'string' && jobFromRoute.trim() !== '') {
    void openImportJobFromRoute(jobFromRoute)
  }
})

watch(
  () => route.query.job,
  (job) => {
    if (typeof job === 'string' && job.trim() !== '') {
      void openImportJobFromRoute(job)
    }
  },
)

onBeforeUnmount(() => {
  resetTracking()
})
</script>
