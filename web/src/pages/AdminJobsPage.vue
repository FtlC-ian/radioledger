<template>
  <q-page class="q-pa-md admin-jobs-page">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Admin Job Dashboard</div>
        <div class="text-body2 text-grey-6">River job visibility, manual retry/trigger, and sync health overview.</div>
      </div>
      <div class="row items-center q-gutter-sm">
        <q-toggle v-model="autoRefresh" label="Auto-refresh (5s)" />
        <q-btn color="primary" icon="refresh" label="Refresh" :loading="loading" @click="refreshAll" />
      </div>
    </div>

    <div class="q-mb-lg">
      <div class="text-subtitle1 q-mb-sm">Callsign Source Overview</div>
      <div class="row q-col-gutter-md">
        <div v-for="source in sourceCards" :key="source.source" class="col-12 col-sm-6 col-md-4 col-lg-3">
          <q-card flat bordered>
            <q-card-section class="q-pb-sm row items-center justify-between">
              <div class="text-subtitle2">{{ source.flag }} {{ source.source.toUpperCase() }}</div>
              <q-badge :color="source.statusColor" :label="source.statusLabel" />
            </q-card-section>
            <q-card-section class="q-pt-none">
              <div class="text-body2">Records: {{ formatNumber(source.record_count) }}</div>
              <div class="text-caption text-grey-7">Last: {{ formatDateTime(source.last_sync_at) }}</div>
              <div class="text-caption text-grey-7">Next: {{ formatDateTime(source.next_scheduled_sync) }}</div>
            </q-card-section>
          </q-card>
        </div>
      </div>
    </div>

    <div class="q-mb-lg">
      <div class="text-subtitle1 q-mb-sm">Sync Services</div>
      <div class="row q-col-gutter-md">
        <div v-for="service in serviceCards" :key="service.service" class="col-12 col-md-4">
          <q-card flat bordered>
            <q-card-section class="q-pb-sm">
              <div class="text-subtitle2">{{ service.service.toUpperCase() }}</div>
            </q-card-section>
            <q-card-section class="q-pt-none">
              <div class="text-body2">Pending: {{ formatNumber(service.pending_count) }}</div>
              <div class="text-body2">Uploaded: {{ formatNumber(service.uploaded_count) }}</div>
              <div class="text-body2">Failed: {{ formatNumber(service.failed_count) }}</div>
              <div class="text-caption text-grey-7 q-mt-xs">Last activity: {{ formatDateTime(service.last_activity_at) }}</div>
              <q-linear-progress
                v-if="service.total > 0"
                class="q-mt-sm"
                rounded
                size="10px"
                :value="service.uploaded_count / service.total"
                :color="service.failed_count > 0 ? 'negative' : 'primary'"
              />
            </q-card-section>
          </q-card>
        </div>
      </div>
    </div>

    <q-card flat bordered>
      <q-card-section>
        <div class="row q-col-gutter-sm items-center">
          <div class="col-12 col-md-4">
            <q-tabs v-model="stateFilter" dense align="left" no-caps inline-label class="text-primary" active-color="primary">
              <q-tab v-for="tab in stateTabs" :key="tab.value" :name="tab.value" :label="tab.label" />
            </q-tabs>
          </div>
          <div class="col-12 col-md-3">
            <q-select
              v-model="kindFilter"
              dense
              outlined
              emit-value
              map-options
              :options="kindOptions"
              label="Job kind"
              @update:model-value="loadJobs"
            />
          </div>
          <div class="col-12 col-md-5 row q-gutter-sm justify-end">
            <q-select v-model="triggerKind" dense outlined emit-value map-options :options="syncTriggerOptions" label="Trigger kind" style="min-width: 220px" />
            <q-btn color="secondary" icon="play_arrow" label="Trigger Now" :disable="!triggerKind" :loading="actionLoading === 'trigger'" @click="triggerSync" />
          </div>
        </div>
      </q-card-section>

      <q-separator />

      <q-table
        flat
        row-key="id"
        :rows="jobs"
        :columns="columns"
        :loading="loading"
        :pagination="{ rowsPerPage: 0 }"
        hide-pagination
      >
        <template #body-cell-state="props">
          <q-td :props="props">
            <q-badge :color="stateColor(props.row.state)" :label="props.row.state" />
          </q-td>
        </template>

        <template #body-cell-created_at="props">
          <q-td :props="props">{{ formatDateTime(props.row.created_at) }}</q-td>
        </template>

        <template #body-cell-duration="props">
          <q-td :props="props">{{ props.row.duration || '—' }}</q-td>
        </template>

        <template #body-cell-errors="props">
          <q-td :props="props">
            <span v-if="!props.row.errors?.length" class="text-grey-6">—</span>
            <q-chip v-else color="negative" text-color="white" dense>{{ props.row.errors[0] }}</q-chip>
          </q-td>
        </template>

        <template #body-cell-actions="props">
          <q-td :props="props">
            <q-btn
              v-if="isFailedState(props.row.state)"
              flat
              dense
              color="warning"
              icon="restart_alt"
              label="Retry"
              :loading="actionLoading === `retry-${props.row.id}`"
              @click="retryJob(props.row.id)"
            />
          </q-td>
        </template>
      </q-table>

      <q-card-actions align="between" class="q-pa-md">
        <div class="text-caption text-grey-7">Showing {{ jobs.length }} job(s)</div>
        <div class="row q-gutter-sm">
          <q-btn flat label="Previous" :disable="pagination.offset === 0" @click="prevPage" />
          <q-btn flat label="Next" :disable="jobs.length < pagination.limit" @click="nextPage" />
        </div>
      </q-card-actions>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useQuasar } from 'quasar'
import { apiGet, apiPost } from 'src/api/client'

type AdminJob = {
  id: number
  kind: string
  state: string
  created_at: string
  duration?: string
  errors: string[]
}

type SourceOverview = {
  source: string
  flag: string
  record_count: number
  last_sync_at?: string
  next_scheduled_sync?: string
}

type ServiceOverview = {
  service: string
  pending_count: number
  uploaded_count: number
  failed_count: number
  last_activity_at?: string
}

const $q = useQuasar()
const loading = ref(false)
const actionLoading = ref('')
const autoRefresh = ref(false)
const timer = ref<number | null>(null)

const jobs = ref<AdminJob[]>([])
const overviewSources = ref<SourceOverview[]>([])
const overviewServices = ref<ServiceOverview[]>([])

const stateFilter = ref('all')
const kindFilter = ref('')
const triggerKind = ref('fcc_weekly_sync')
const knownKinds = ref<string[]>([])

const pagination = ref({ limit: 50, offset: 0 })

const columns = [
  { name: 'kind', label: 'Kind', field: 'kind', align: 'left' },
  { name: 'state', label: 'State', field: 'state', align: 'left' },
  { name: 'created_at', label: 'Created', field: 'created_at', align: 'left' },
  { name: 'duration', label: 'Duration', field: 'duration', align: 'left' },
  { name: 'errors', label: 'Errors', field: 'errors', align: 'left' },
  { name: 'actions', label: 'Actions', field: 'id', align: 'right' },
]

const stateTabs = [
  { label: 'All', value: 'all' },
  { label: 'Running', value: 'running' },
  { label: 'Failed', value: 'failed' },
  { label: 'Completed', value: 'completed' },
  { label: 'Scheduled', value: 'scheduled' },
]

const syncTriggerOptions = [
  { label: 'FCC weekly sync', value: 'fcc_weekly_sync' },
  { label: 'FCC daily sync', value: 'fcc_daily_sync' },
  { label: 'ISED weekly sync', value: 'ised_weekly_sync' },
  { label: 'ACMA weekly sync', value: 'acma_weekly_sync' },
  { label: 'ANFR weekly sync', value: 'anfr_weekly_sync' },
  { label: 'IFT weekly sync', value: 'ift_weekly_sync' },
  { label: 'RDI weekly sync', value: 'rdi_weekly_sync' },
  { label: 'Ofcom weekly sync', value: 'ofcom_weekly_sync' },
  { label: 'BNetzA weekly sync', value: 'bnetza_weekly_sync' },
  { label: 'JJ1WTL monthly sync', value: 'jj1wtl_monthly_sync' },
]

const kindOptions = computed(() => [
  { label: 'All kinds', value: '' },
  ...knownKinds.value.map((kind) => ({ label: kind, value: kind })),
])

const sourceCards = computed(() => {
  const now = Date.now()
  return overviewSources.value.map((source) => {
    const lastTs = source.last_sync_at ? new Date(source.last_sync_at).getTime() : 0
    let statusLabel = 'Never synced'
    let statusColor = 'negative'

    if (lastTs > 0) {
      const ageMs = now - lastTs
      if (ageMs < 2 * 24 * 60 * 60 * 1000) {
        statusLabel = 'Recent'
        statusColor = 'positive'
      } else if (ageMs < 7 * 24 * 60 * 60 * 1000) {
        statusLabel = 'Stale'
        statusColor = 'warning'
      } else {
        statusLabel = 'Old'
        statusColor = 'negative'
      }
    }

    return { ...source, statusLabel, statusColor }
  })
})

const serviceCards = computed(() => {
  return overviewServices.value.map((service) => ({
    ...service,
    total: service.pending_count + service.uploaded_count + service.failed_count,
  }))
})

watch(stateFilter, () => {
  pagination.value.offset = 0
  void loadJobs()
})

watch(kindFilter, () => {
  pagination.value.offset = 0
  void loadJobs()
})

watch(autoRefresh, (enabled) => {
  if (enabled) {
    startTimer()
  } else {
    stopTimer()
  }
})

onMounted(() => {
  void refreshAll()
})

onUnmounted(() => {
  stopTimer()
})

async function refreshAll() {
  loading.value = true
  try {
    await Promise.all([loadJobs(), loadOverview()])
  } finally {
    loading.value = false
  }
}

async function loadJobs() {
  const params = new URLSearchParams()
  if (stateFilter.value !== 'all') params.set('state', stateFilter.value)
  if (kindFilter.value) params.set('kind', kindFilter.value)
  params.set('limit', String(pagination.value.limit))
  params.set('offset', String(pagination.value.offset))

  const response = await apiGet<{ items: AdminJob[] }>(`/v1/admin/jobs?${params.toString()}`)
  if (response.success && response.data) {
    jobs.value = response.data.items || []
    knownKinds.value = Array.from(new Set([...knownKinds.value, ...jobs.value.map((job) => job.kind)])).sort()
  }
}

async function loadOverview() {
  const response = await apiGet<{ sources: SourceOverview[]; services: ServiceOverview[] }>('/v1/admin/sync/overview')
  if (response.success && response.data) {
    overviewSources.value = response.data.sources || []
    overviewServices.value = response.data.services || []
  }
}

async function retryJob(id: number) {
  actionLoading.value = `retry-${id}`
  try {
    const response = await apiPost(`/v1/admin/jobs/${id}/retry`)
    if (response.success) {
      $q.notify({ type: 'positive', message: 'Job retry queued.' })
      await loadJobs()
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Retry failed.' })
    }
  } finally {
    actionLoading.value = ''
  }
}

async function triggerSync() {
  if (!triggerKind.value) return
  actionLoading.value = 'trigger'
  try {
    const response = await apiPost('/v1/admin/sync/trigger', { kind: triggerKind.value })
    if (response.success) {
      $q.notify({ type: 'positive', message: `${triggerKind.value} queued.` })
      await refreshAll()
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Trigger failed.' })
    }
  } finally {
    actionLoading.value = ''
  }
}

function stateColor(state: string) {
  if (state === 'completed') return 'positive'
  if (state === 'running') return 'warning'
  if (isFailedState(state)) return 'negative'
  if (state === 'scheduled' || state === 'available') return 'grey-6'
  return 'grey-7'
}

function isFailedState(state: string) {
  return ['discarded', 'cancelled', 'failed', 'error', 'retryable'].includes(state)
}

function startTimer() {
  stopTimer()
  timer.value = window.setInterval(() => {
    void refreshAll()
  }, 5000)
}

function stopTimer() {
  if (timer.value != null) {
    window.clearInterval(timer.value)
    timer.value = null
  }
}

function prevPage() {
  pagination.value.offset = Math.max(0, pagination.value.offset - pagination.value.limit)
  void loadJobs()
}

function nextPage() {
  pagination.value.offset += pagination.value.limit
  void loadJobs()
}

function formatDateTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString()
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value || 0)
}
</script>
