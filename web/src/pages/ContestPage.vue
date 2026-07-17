<template>
  <q-page class="q-pa-md contest-page">
    <q-banner v-if="error" rounded dense class="bg-negative text-white q-mb-md">
      {{ error }}
    </q-banner>

    <q-card flat bordered class="q-mb-md contest-header">
      <q-card-section class="row items-center justify-between q-col-gutter-md">
        <div class="col-12 col-md">
          <div class="row items-center q-gutter-sm">
            <div class="text-h6">{{ session?.name || 'Contest Logging' }}</div>
            <q-chip v-if="session" dense color="primary" text-color="white" icon="bolt">{{ session.contest_code }}</q-chip>
          </div>
          <div class="text-caption text-grey-5 q-mt-xs">
            {{ session ? `${session.category_mode} · ${session.category_power}` : 'Select a contest session to begin logging.' }}
          </div>
        </div>

        <div class="col-12 col-md-auto row items-center q-gutter-sm">
          <q-select
            v-model="sessionUUID"
            dense
            outlined
            emit-value
            map-options
            :options="sessionOptions"
            label="Contest session"
            class="session-select"
            @update:model-value="onSessionChange"
          />

          <q-btn flat dense icon="refresh" :loading="statsLoading || sessionLoading" @click="refreshCurrent" />
          <q-btn color="primary" icon="download" label="Cabrillo" :disable="!sessionUUID" @click="exportCabrillo" />
        </div>
      </q-card-section>
    </q-card>

    <div class="row q-col-gutter-md q-mb-md stats-strip">
      <div class="col-6 col-md-3">
        <q-card flat bordered class="stat-card">
          <q-card-section>
            <div class="text-caption text-grey-5">Rate</div>
            <div class="text-h4 text-weight-bold text-cyan-4">{{ stats.rate_per_hour.toFixed(0) }}</div>
            <div class="text-caption text-grey-5">QSOs / hr</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-6 col-md-3">
        <q-card flat bordered class="stat-card">
          <q-card-section>
            <div class="text-caption text-grey-5">Frequency</div>
            <div class="text-h4 text-weight-bold text-amber-4">{{ frequencyDisplay }}</div>
            <div class="text-caption text-grey-5">{{ entry.band }} {{ entry.mode }}</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-6 col-md-3">
        <q-card flat bordered class="stat-card">
          <q-card-section>
            <div class="text-caption text-grey-5">Total QSOs</div>
            <div class="text-h4 text-weight-bold">{{ stats.total_qsos }}</div>
            <div class="text-caption text-grey-5">Dupes: {{ stats.dupe_qsos }}</div>
          </q-card-section>
        </q-card>
      </div>
      <div class="col-6 col-md-3">
        <q-card flat bordered class="stat-card">
          <q-card-section>
            <div class="text-caption text-grey-5">Next serial</div>
            <div class="text-h4 text-weight-bold text-orange-4">{{ String(stats.serial_counter + 1).padStart(3, '0') }}</div>
            <div class="text-caption text-grey-5">Current: {{ String(stats.serial_counter).padStart(3, '0') }}</div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <div class="row q-col-gutter-md contest-workspace">
      <div class="col-12 col-xl-9 column q-gutter-md">
        <q-card flat bordered class="entry-card">
          <q-card-section>
            <div class="row q-col-gutter-sm items-center">
              <div class="col-12 col-md-4">
                <q-input
                  ref="callsignInputRef"
                  v-model="entry.callsign"
                  label="Callsign"
                  outlined
                  dense
                  input-class="text-uppercase text-h6 text-weight-bold"
                  class="callsign-input"
                  :class="callsignFieldClass"
                  maxlength="14"
                  autofocus
                  @update:model-value="onCallsignChange"
                >
                  <template #append>
                    <q-spinner v-if="dupeCheck.loading" size="1em" color="grey-5" />
                    <q-icon
                      v-else-if="dupeCheck.checked && entry.callsign.trim().length >= 3"
                      :name="dupeCheck.isDupe ? 'warning' : 'check_circle'"
                      :color="dupeCheck.isDupe ? 'negative' : 'positive'"
                    />
                  </template>
                </q-input>
              </div>

              <div class="col-12 col-md-3">
                <q-input v-model="entry.exchangeRcvd" :label="exchangeLabel" outlined dense />
              </div>

              <div class="col-6 col-md-2">
                <q-select v-model="entry.band" :options="BANDS" emit-value map-options outlined dense label="Band" />
              </div>

              <div class="col-6 col-md-2">
                <q-select v-model="entry.mode" :options="MODES" emit-value map-options outlined dense label="Mode" />
              </div>

              <div class="col-12 col-md-1">
                <q-btn
                  color="positive"
                  icon="add"
                  class="full-width"
                  :loading="logging"
                  :disable="!canLog || !sessionUUID"
                  @click="logQSO"
                />
              </div>
            </div>

            <div class="row q-col-gutter-sm q-mt-sm">
              <div class="col-12 col-md-3">
                <q-input v-model.number="entry.frequencyKhz" outlined dense type="number" label="Frequency (kHz)" />
              </div>
              <div class="col-6 col-md-2">
                <q-input v-model="entry.rstSent" outlined dense label="RST sent" />
              </div>
              <div class="col-6 col-md-2">
                <q-input v-model="entry.rstRcvd" outlined dense label="RST rcvd" />
              </div>
              <div class="col-12 col-md row items-center justify-end q-gutter-sm">
                <q-chip
                  v-if="dupeCheck.isDupe"
                  color="negative"
                  text-color="white"
                  icon="warning"
                  class="text-weight-bold"
                >
                  DUPE DETECTED
                </q-chip>
                <q-btn flat dense icon="backspace" label="Clear (Esc)" @click="resetEntry" />
                <q-btn color="primary" dense icon="keyboard_return" label="Log (Enter)" :disable="!canLog || !sessionUUID" @click="logQSO" />
              </div>
            </div>
          </q-card-section>
        </q-card>

        <q-card flat bordered class="log-card col">
          <q-card-section class="row items-center justify-between q-pb-sm">
            <div class="text-subtitle1">Logged QSOs</div>
            <div class="text-caption text-grey-5">Newest first · {{ loggedQSOs.length }} rows</div>
          </q-card-section>

          <q-table
            :rows="loggedQSOs"
            :columns="qsoColumns"
            row-key="uuid"
            flat
            dense
            virtual-scroll
            :rows-per-page-options="[0]"
            :pagination="{ rowsPerPage: 0 }"
            class="contest-log-table"
          >
            <template #body-cell-is_dupe="props">
              <q-td :props="props">
                <q-badge v-if="props.row.is_dupe" color="negative" label="DUPE" />
              </q-td>
            </template>
            <template #body-cell-datetime_on="props">
              <q-td :props="props">{{ formatUTCShort(props.row.datetime_on) }}</q-td>
            </template>
            <template #body-cell-sent_serial="props">
              <q-td :props="props">{{ props.row.sent_serial ? String(props.row.sent_serial).padStart(3, '0') : '—' }}</q-td>
            </template>
          </q-table>
        </q-card>
      </div>

      <div class="col-12 col-xl-3">
        <q-card flat bordered class="bandmap-card">
          <q-card-section>
            <div class="text-subtitle2 q-mb-sm">Band map</div>
            <div class="column q-gutter-xs">
              <div
                v-for="row in bandMap"
                :key="row.band"
                class="band-cell row items-center justify-between"
                :class="{ 'band-cell--active': entry.band === row.band, 'band-cell--worked': row.count > 0 }"
              >
                <span class="text-weight-medium">{{ row.band.toUpperCase() }}</span>
                <q-badge :color="row.count > 0 ? 'positive' : 'grey-6'" :label="row.count > 0 ? `worked ${row.count}` : 'needed'" />
              </div>
            </div>
          </q-card-section>
        </q-card>
      </div>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import api, { apiGet, apiPost } from 'src/api/client'

interface ContestSession {
  uuid: string
  name: string
  contest_code: string
  exchange_template: string
  exchange_sent?: string
  category_mode: string
  category_power: string
  serial_counter: number
  status: string
}

interface ContestStats {
  total_qsos: number
  dupe_qsos: number
  unique_callsigns: number
  serial_counter: number
  rate_per_hour: number
  rate_per_10min: number
  rate_per_min: number
  first_qso_at?: string
  last_qso_at?: string
}

interface LoggedQSO {
  uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
  rst_sent?: string
  rst_rcvd?: string
  sent_serial?: number
  sent_exchange?: string
  recv_exchange?: string
  is_dupe: boolean
}

interface DupeResult {
  dupe: boolean
  previous_qso?: {
    uuid: string
    callsign: string
    band: string
    mode: string
    datetime_on: string
    sent_serial?: number
    recv_exchange?: string
  }
}

const BANDS = [
  { label: '160m', value: '160m' },
  { label: '80m', value: '80m' },
  { label: '40m', value: '40m' },
  { label: '30m', value: '30m' },
  { label: '20m', value: '20m' },
  { label: '17m', value: '17m' },
  { label: '15m', value: '15m' },
  { label: '12m', value: '12m' },
  { label: '10m', value: '10m' },
  { label: '6m', value: '6m' },
  { label: '2m', value: '2m' },
]

const MODES = [
  { label: 'SSB', value: 'SSB' },
  { label: 'CW', value: 'CW' },
  { label: 'RTTY', value: 'RTTY' },
  { label: 'FT8', value: 'FT8' },
  { label: 'FT4', value: 'FT4' },
]

const qsoColumns = [
  { name: 'is_dupe', label: '', field: 'is_dupe', align: 'left' as const },
  { name: 'datetime_on', label: 'UTC', field: 'datetime_on', align: 'left' as const, sortable: true },
  { name: 'callsign', label: 'Callsign', field: 'callsign', align: 'left' as const, sortable: true },
  { name: 'band', label: 'Band', field: 'band', align: 'left' as const, sortable: true },
  { name: 'mode', label: 'Mode', field: 'mode', align: 'left' as const, sortable: true },
  { name: 'sent_serial', label: '#', field: 'sent_serial', align: 'right' as const, sortable: true },
  { name: 'sent_exchange', label: 'Sent', field: 'sent_exchange', align: 'left' as const },
  { name: 'recv_exchange', label: 'Rcvd', field: 'recv_exchange', align: 'left' as const },
]

const route = useRoute()
const router = useRouter()
const $q = useQuasar()

const error = ref('')
const sessionUUID = ref<string>('')
const session = ref<ContestSession | null>(null)
const sessions = ref<ContestSession[]>([])
const sessionLoading = ref(false)

const stats = ref<ContestStats>({
  total_qsos: 0,
  dupe_qsos: 0,
  unique_callsigns: 0,
  serial_counter: 0,
  rate_per_hour: 0,
  rate_per_10min: 0,
  rate_per_min: 0,
})
const statsLoading = ref(false)
const loggedQSOs = ref<LoggedQSO[]>([])
const logging = ref(false)

const entry = ref({
  callsign: '',
  exchangeRcvd: '',
  band: '20m',
  mode: 'SSB',
  frequencyKhz: null as number | null,
  rstSent: '',
  rstRcvd: '',
})

const dupeCheck = ref({
  loading: false,
  checked: false,
  isDupe: false,
})

const callsignInputRef = ref<{ focus: () => void } | null>(null)

let dupeTimer: ReturnType<typeof setTimeout> | null = null
let statsInterval: ReturnType<typeof setInterval> | null = null

const sessionOptions = computed(() =>
  sessions.value.map((item) => ({
    label: `${item.name} (${item.contest_code})`,
    value: item.uuid,
  })),
)

const canLog = computed(
  () => entry.value.callsign.trim().length >= 2 && entry.value.band && entry.value.mode && !logging.value,
)

const frequencyDisplay = computed(() => {
  if (!entry.value.frequencyKhz) {
    return '—'
  }
  if (entry.value.frequencyKhz >= 1000) {
    return `${(entry.value.frequencyKhz / 1000).toFixed(3)} MHz`
  }
  return `${entry.value.frequencyKhz.toFixed(1)} kHz`
})

const exchangeLabel = computed(() => {
  const labels: Record<string, string> = {
    serial: 'Exchange (serial)',
    grid: 'Exchange (grid)',
    state: 'Exchange (state)',
    zone: 'Exchange (zone)',
    custom: 'Exchange',
  }
  return labels[session.value?.exchange_template || 'serial'] || 'Exchange'
})

const callsignFieldClass = computed(() => {
  if (entry.value.callsign.trim().length < 3 || !dupeCheck.value.checked) {
    return ''
  }
  return dupeCheck.value.isDupe ? 'dupe-negative' : 'dupe-positive'
})

const bandMap = computed(() => {
  const counts: Record<string, number> = {}
  for (const qso of loggedQSOs.value) {
    if (!qso.is_dupe) {
      counts[qso.band] = (counts[qso.band] ?? 0) + 1
    }
  }

  return BANDS.map((band) => ({ band: band.value, count: counts[band.value] ?? 0 }))
})

async function loadSessions() {
  try {
    const response = await apiGet<{ items: ContestSession[] }>('/v1/contests')
    sessions.value = response.success && response.data?.items ? response.data.items : []

    const fromRoute = typeof route.params.uuid === 'string' ? route.params.uuid : ''
    if (fromRoute) {
      sessionUUID.value = fromRoute
    } else if (!sessionUUID.value && sessions.value.length > 0) {
      sessionUUID.value = sessions.value[0].uuid
      await router.replace(`/contests/${sessionUUID.value}`)
    }
  } catch {
    sessions.value = []
  }
}

async function loadSession() {
  if (!sessionUUID.value) {
    session.value = null
    return
  }

  sessionLoading.value = true
  error.value = ''
  try {
    const response = await apiGet<ContestSession>(`/v1/contests/${sessionUUID.value}`)
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to load contest session')
    }

    session.value = response.data
    if (session.value.category_mode && session.value.category_mode !== 'MIXED') {
      entry.value.mode = session.value.category_mode
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load contest session'
  } finally {
    sessionLoading.value = false
  }
}

async function refreshStats() {
  if (!sessionUUID.value) {
    return
  }

  statsLoading.value = true
  try {
    const response = await apiGet<ContestStats>(`/v1/contests/${sessionUUID.value}/stats`)
    if (response.success && response.data) {
      stats.value = response.data
    }
  } finally {
    statsLoading.value = false
  }
}

async function refreshCurrent() {
  await Promise.all([loadSession(), refreshStats()])
}

function scheduleDupeCheck() {
  if (!sessionUUID.value) {
    return
  }

  if (dupeTimer) {
    clearTimeout(dupeTimer)
  }

  dupeCheck.value.checked = false

  const callsign = entry.value.callsign.trim().toUpperCase()
  if (callsign.length < 3) {
    dupeCheck.value = { loading: false, checked: false, isDupe: false }
    return
  }

  dupeTimer = setTimeout(() => {
    void runDupeCheck(callsign, entry.value.band)
  }, 220)
}

async function runDupeCheck(callsign: string, band: string) {
  if (!sessionUUID.value) {
    return
  }

  dupeCheck.value.loading = true
  try {
    const response = await apiGet<DupeResult>(`/v1/contests/${sessionUUID.value}/check-dupe`, {
      params: { callsign, band },
    })

    if (response.success && response.data) {
      dupeCheck.value = {
        loading: false,
        checked: true,
        isDupe: Boolean(response.data.dupe),
      }
      return
    }
  } catch {
    // no-op
  }

  dupeCheck.value = {
    loading: false,
    checked: false,
    isDupe: false,
  }
}

async function logQSO() {
  if (!sessionUUID.value || !canLog.value) {
    return
  }

  logging.value = true
  try {
    const payload: Record<string, unknown> = {
      callsign: entry.value.callsign.trim().toUpperCase(),
      exchange_rcvd: entry.value.exchangeRcvd.trim(),
      band: entry.value.band,
      mode: entry.value.mode,
    }

    if (entry.value.frequencyKhz) {
      payload.frequency_hz = Math.round(entry.value.frequencyKhz * 1000)
    }
    if (entry.value.rstSent) {
      payload.rst_sent = entry.value.rstSent
    }
    if (entry.value.rstRcvd) {
      payload.rst_rcvd = entry.value.rstRcvd
    }

    const response = await apiPost<LoggedQSO, typeof payload>(`/v1/contests/${sessionUUID.value}/qso`, payload)
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to log QSO')
    }

    loggedQSOs.value.unshift(response.data)

    if (response.data.sent_serial) {
      stats.value.serial_counter = response.data.sent_serial
    }
    if (response.data.is_dupe) {
      stats.value.dupe_qsos += 1
      $q.notify({ type: 'warning', message: `DUPE: ${response.data.callsign} already worked on ${response.data.band}` })
    } else {
      stats.value.total_qsos += 1
    }

    resetEntry()
    await nextTick()
    callsignInputRef.value?.focus()
    await refreshStats()
  } catch (e) {
    $q.notify({ type: 'negative', message: e instanceof Error ? e.message : 'Failed to log QSO' })
  } finally {
    logging.value = false
  }
}

function resetEntry() {
  entry.value.callsign = ''
  entry.value.exchangeRcvd = ''
  entry.value.rstSent = ''
  entry.value.rstRcvd = ''
  dupeCheck.value = { loading: false, checked: false, isDupe: false }
}

async function exportCabrillo() {
  if (!sessionUUID.value) {
    return
  }

  try {
    const response = await api.get(`/v1/contests/${sessionUUID.value}/export/cabrillo`, {
      responseType: 'blob',
    })

    const blob = new Blob([response.data], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${session.value?.contest_code ?? 'contest'}.log`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)
  } catch {
    $q.notify({ type: 'negative', message: 'Cabrillo export failed' })
  }
}

function formatUTCShort(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return date.toISOString().slice(11, 16)
}

function onCallsignChange() {
  scheduleDupeCheck()
}

function onGlobalKeydown(event: KeyboardEvent) {
  const target = event.target as HTMLElement | null
  const isInputContext =
    target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target?.closest('.q-field') != null

  if (!isInputContext) {
    return
  }

  if (event.key === 'Enter') {
    event.preventDefault()
    void logQSO()
  }

  if (event.key === 'Escape') {
    event.preventDefault()
    resetEntry()
  }
}

async function onSessionChange(value: string) {
  if (!value) {
    return
  }

  await router.replace(`/contests/${value}`)
  loggedQSOs.value = []
  await refreshCurrent()
  await nextTick()
  callsignInputRef.value?.focus()
}

watch(
  () => route.params.uuid,
  async (value) => {
    if (typeof value === 'string' && value && value !== sessionUUID.value) {
      sessionUUID.value = value
      loggedQSOs.value = []
      await refreshCurrent()
    }
  },
)

watch(
  () => entry.value.band,
  () => {
    if (entry.value.callsign.trim().length >= 3) {
      scheduleDupeCheck()
    }
  },
)

onMounted(async () => {
  const fromRoute = typeof route.params.uuid === 'string' ? route.params.uuid : ''
  if (fromRoute) {
    sessionUUID.value = fromRoute
  }

  await loadSessions()
  await refreshCurrent()

  statsInterval = setInterval(() => {
    void refreshStats()
  }, 30_000)

  window.addEventListener('keydown', onGlobalKeydown)
})

onUnmounted(() => {
  if (statsInterval) {
    clearInterval(statsInterval)
    statsInterval = null
  }
  if (dupeTimer) {
    clearTimeout(dupeTimer)
    dupeTimer = null
  }

  window.removeEventListener('keydown', onGlobalKeydown)
})
</script>

<style scoped>
.contest-page {
  max-width: 1400px;
  margin: 0 auto;
}

.contest-header,
.stat-card,
.entry-card,
.log-card,
.bandmap-card {
  background: rgba(255, 255, 255, 0.02);
}

.session-select {
  min-width: 250px;
}

.callsign-input :deep(.q-field__control) {
  transition: border-color 120ms ease, box-shadow 120ms ease;
}

.callsign-input.dupe-positive :deep(.q-field__control) {
  border-color: rgba(16, 185, 129, 0.95);
  box-shadow: 0 0 0 1px rgba(16, 185, 129, 0.85);
}

.callsign-input.dupe-negative :deep(.q-field__control) {
  border-color: rgba(239, 68, 68, 0.95);
  box-shadow: 0 0 0 1px rgba(239, 68, 68, 0.85);
}

.contest-workspace {
  min-height: 560px;
}

.log-card {
  min-height: 420px;
}

.contest-log-table {
  height: 430px;
}

.band-cell {
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  padding: 10px;
}

.band-cell--worked {
  border-color: rgba(16, 185, 129, 0.45);
}

.band-cell--active {
  border-color: rgba(59, 130, 246, 0.85);
  background: rgba(59, 130, 246, 0.15);
}

@media (max-width: 1023px) {
  .session-select {
    min-width: 100%;
  }

  .contest-log-table {
    height: 340px;
  }
}
</style>
