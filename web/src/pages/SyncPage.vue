<template>
  <q-page class="q-pa-md sync-page">
    <SyncServiceOverview
      :loading="loading"
      :action-loading="actionLoading"
      :sync-all-loading="syncAllLoading"
      :known-services="knownServices"
      :service-health="serviceHealth"
      :sync-progress="syncProgress"
      :active-sync-services="activeSyncServices"
      :lotw-has-cert="lotwHasCert"
      :lotw-raw-pending-count="lotwRawPendingCount"
      :total-pending="totalPending"
      @refresh="loadAll"
      @open-settings="openSyncSettings"
      @trigger-service-sync="triggerServiceSync"
      @sync-all="doSyncAll"
      @retry-failed="retryFailed"
      @cancel-sync="cancelSync"
    />

    <!-- ── Filters ───────────────────────────────────────────── -->
    <div class="row q-col-gutter-sm q-mb-sm">
      <div class="col-12 col-md-2">
        <q-input v-model="filters.callsign" dense outlined label="Callsign" @keyup.enter="applyFilters" />
      </div>
      <div class="col-12 col-md-2">
        <q-select v-model="filters.service" dense outlined emit-value map-options label="Service" :options="serviceFilterOptions" />
      </div>
      <div class="col-12 col-md-2">
        <q-select v-model="filters.status" dense outlined emit-value map-options label="Status" :options="statusFilterOptions" />
      </div>
      <div class="col-12 col-md-2">
        <q-input v-model="filters.dateFrom" dense outlined type="date" label="From" />
      </div>
      <div class="col-12 col-md-2">
        <q-input v-model="filters.dateTo" dense outlined type="date" label="To" />
      </div>
      <div class="col-12 col-md-2 row q-gutter-sm items-center">
        <q-btn color="primary" label="Apply" @click="applyFilters" />
        <q-btn flat label="Reset" @click="resetFilters" />
      </div>
    </div>

    <!-- ── Completed-record toggle ───────────────────────────── -->
    <div class="row items-center q-gutter-sm q-mb-md">
      <q-toggle
        v-model="hideCompleted"
        dense color="primary"
        label="Hide fully synced QSOs"
      />
      <span v-if="hideCompleted && justSyncedUUIDs.size > 0" class="text-caption text-grey-5">
        (keeping {{ justSyncedUUIDs.size }} recently synced visible)
      </span>
      <span v-if="hideCompleted && displayedItems.length === 0 && !loading" class="text-caption text-positive">
        <q-icon name="check_circle" size="14px" /> No pending QSOs — toggle to see all records.
      </span>
    </div>

    <!-- ── QSO Status Table ───────────────────────────────────── -->
    <q-table
      :table-row-class-fn="tableRowClass"
      flat bordered row-key="qso_uuid"
      :rows="displayedItems"
      :columns="columns"
      :rows-per-page-options="[10, 25, 50]"
      :pagination="tablePagination"
      :loading="loading"
      :no-data-label="hideCompleted ? 'No pending QSOs — toggle \'Hide fully synced QSOs\' to see all' : 'No QSOs found'"
      @request="onTableRequest"
      @row-click="openHistory"
    >
      <template #body-cell-services="props">
        <q-td :props="props">
          <div class="row q-gutter-xs">
            <q-icon
              v-for="svc in knownServices"
              :key="svc"
              :name="statusIcon(statusFor(props.row, svc))"
              :color="justSyncedUUIDs.has(props.row.qso_uuid) && statusFor(props.row, svc) === 'uploaded'
                ? 'positive'
                : statusColor(statusFor(props.row, svc))"
              size="18px"
            >
              <q-tooltip>
                {{ serviceLabel(svc) }}: {{ statusFor(props.row, svc) }}
                <template v-if="justSyncedUUIDs.has(props.row.qso_uuid)"> (just synced)</template>
              </q-tooltip>
            </q-icon>
            <q-badge
              v-if="justSyncedUUIDs.has(props.row.qso_uuid)"
              color="positive" label="synced" dense class="q-ml-xs"
            />
          </div>
        </q-td>
      </template>

      <template #body-cell-conflict="props">
        <q-td :props="props">
          <q-btn
            v-if="props.row.has_conflict && props.row.conflict_id"
            flat dense color="negative" icon="warning" label="Resolve"
            @click.stop="openConflict(props.row.conflict_id)"
          />
          <span v-else class="text-grey-5">—</span>
        </q-td>
      </template>
    </q-table>

    <!-- ── QSO Sync History Dialog ───────────────────────────── -->
    <SyncHistoryDialog
      v-model="historyDialog"
      :qso="selectedQSO"
      :history="history"
    />

    <!-- ── Conflict Resolution Dialog ───────────────────────── -->
    <SyncConflictDialog
      v-model="conflictDialog"
      :conflict="activeConflict"
      :loading="actionLoading === 'resolve'"
      @submit="submitResolution"
    />

    <!-- ── LoTW Sync Modal ───────────────────────────────────── -->
    <LotwSyncModal
      v-model="showLotwModal"
      :callsign="lotwCallsign"
      :pending-count="servicePendingCount('lotw')"
      @synced="onLotwSynced"
      @update:model-value="onLotwModalClosed"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onActivated, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import { apiPost } from 'src/api/client'
import SyncConflictDialog from 'src/components/SyncConflictDialog.vue'
import SyncHistoryDialog from 'src/components/SyncHistoryDialog.vue'
import LotwSyncModal from 'src/components/LotwSyncModal.vue'
import { useSyncPolling } from 'src/composables/useSyncPolling'
import SyncServiceOverview from 'src/pages/sync/components/SyncServiceOverview.vue'
import { useSyncActions } from 'src/pages/sync/composables/useSyncActions'
import { useSyncDashboardData } from 'src/pages/sync/composables/useSyncDashboardData'
import { useSyncOverviewDisplay } from 'src/pages/sync/composables/useSyncOverviewDisplay'
import { useSyncTableState } from 'src/pages/sync/composables/useSyncTableState'
import { useAuthStore } from 'src/stores/auth'
import {
  type SyncConflict,
  type SyncHistoryItem,
  type SyncProgress,
  type SyncStatusRow,
  type SyncTableFilters,
  type SyncTablePagination,
} from 'src/types/sync'
import {
  KNOWN_SERVICES,
  firstErrorMessage,
  formatDateTime,
  serviceLabel,
  statusColor,
  statusFor,
  statusIcon,
} from 'src/utils/syncHelpers'

const $q = useQuasar()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const loading = ref(false)
const actionLoading = ref('')
const syncAllLoading = ref(false)

// ── Service / data state ──────────────────────────────────────
const knownServices = KNOWN_SERVICES
const serviceHealth = ref<Record<string, string>>({})
const syncProgress = ref<Record<string, SyncProgress>>({})
const items = ref<SyncStatusRow[]>([])
const totalRows = ref(0)
const history = ref<SyncHistoryItem[]>([])
const conflicts = ref<SyncConflict[]>([])

// ── LoTW cert ─────────────────────────────────────────────────
const lotwHasCert = ref(false)
const lotwCallsign = ref('')
const showLotwModal = ref(false)
// Raw LoTW pending count from /v1/lotw/sync/pending (based on lotw_sent_at IS NULL).
// Used as a fallback when sync_status rows haven't been backfilled yet.
const lotwRawPendingCount = ref(0)
// When Sync All triggers LoTW modal, remember to sync the other services after it closes.
const pendingSyncAllAfterLotw = ref(false)

// ── Hide-completed / just-synced tracking ────────────────────
// Default to hiding fully synced QSOs so the table focuses on what needs attention.
// This state is NOT persisted — navigate away and come back, it resets.
const hideCompleted = ref(true)
const justSyncedUUIDs = ref(new Set<string>())

// ── Active sync tracking ──────────────────────────────────────
// Tracks which services have had a sync triggered in this session.
// Used to show completed-sync summary cards even after pending_count → 0.
const activeSyncServices = ref(new Set<string>())

// ── Filters ───────────────────────────────────────────────────
const filters = ref<SyncTableFilters>({
  callsign: '',
  service: '',
  status: '',
  dateFrom: '',
  dateTo: '',
})

const tablePagination = ref<SyncTablePagination>({ page: 1, rowsPerPage: 25, rowsNumber: 0 })

const columns = [
  { name: 'callsign', label: 'Callsign', field: 'callsign', align: 'left' },
  { name: 'band', label: 'Band', field: 'band', align: 'left' },
  { name: 'mode', label: 'Mode', field: 'mode', align: 'left' },
  { name: 'datetime_on', label: 'Date/Time', field: (r: SyncStatusRow) => formatDateTime(r.datetime_on), align: 'left' },
  { name: 'services', label: 'Services', field: 'service_statuses', align: 'left' },
  { name: 'error_message', label: 'Error', field: (r: SyncStatusRow) => firstErrorMessage(r), align: 'left' },
  { name: 'conflict', label: 'Conflict', field: 'has_conflict', align: 'left' },
]

const serviceFilterOptions = [
  { label: 'All services', value: '' },
  ...knownServices.map((s) => ({ label: serviceLabel(s), value: s })),
]

const statusFilterOptions = [
  { label: 'All statuses', value: '' },
  { label: 'Pending', value: 'pending' },
  { label: 'Uploaded', value: 'uploaded' },
  { label: 'Confirmed', value: 'confirmed' },
  { label: 'Error', value: 'error' },
]

// ── Dialogs ───────────────────────────────────────────────────
const historyDialog = ref(false)
const selectedQSO = ref<SyncStatusRow | null>(null)
const conflictDialog = ref(false)
const activeConflict = ref<SyncConflict | null>(null)

// ── Computed ──────────────────────────────────────────────────
const totalPending = computed(() =>
  Object.values(syncProgress.value).reduce((sum, p) => sum + (p?.pending_count || 0), 0),
)

const { displayedItems, captureCurrentPendingUUIDs, tableRowClass } = useSyncTableState({
  items,
  hideCompleted,
  justSyncedUUIDs,
  filters,
})

const { servicePendingCount } = useSyncOverviewDisplay({
  serviceHealth,
  syncProgress,
  activeSyncServices,
  lotwHasCert,
  lotwRawPendingCount,
})

const { startPolling, stopPolling } = useSyncPolling({
  syncProgress,
  activeSyncServices,
  knownServices,
  servicePendingCount,
  onPoll: async () => {
    await Promise.all([loadStatus(), loadConflicts()])
  },
})

function openSyncSettings(service: string) {
  void router.push({ path: '/settings', query: { tab: 'sync', service } })
}

const {
  loadStatus,
  loadConflicts,
  loadAll,
  applyFilters,
  resetFilters: resetDashboardFilters,
  onTableRequest,
} = useSyncDashboardData({
  loading,
  serviceHealth,
  syncProgress,
  items,
  totalRows,
  history,
  conflicts,
  lotwHasCert,
  lotwCallsign,
  lotwRawPendingCount,
  filters,
  tablePagination,
  authCallsign: computed(() => auth.callsign),
  startPolling,
})

function resetFilters() {
  hideCompleted.value = true
  resetDashboardFilters()
}

const {
  triggerServiceSync,
  doSyncAll,
  onLotwSynced,
  onLotwModalClosed,
  retryFailed,
  cancelSync,
} = useSyncActions({
  actionLoading,
  syncAllLoading,
  knownServices,
  lotwHasCert,
  pendingSyncAllAfterLotw,
  showLotwModal,
  captureCurrentPendingUUIDs,
  servicePendingCount,
  startPolling,
  stopPolling,
  loadStatus,
  routerPush: (to) => router.push(to),
})

// ── Dialog helpers ────────────────────────────────────────────
function openHistory(_evt: MouseEvent, row: SyncStatusRow) {
  selectedQSO.value = row
  historyDialog.value = true
}

function openConflict(id: number) {
  const found = conflicts.value.find((c) => c.id === id)
  if (!found) {
    $q.notify({ type: 'warning', message: 'Conflict details unavailable. Refresh first.' })
    return
  }
  activeConflict.value = found
  conflictDialog.value = true
}

async function submitResolution(resolution: Record<string, string>) {
  if (!activeConflict.value) return
  actionLoading.value = 'resolve'
  try {
    const res = await apiPost(`/v1/sync/conflicts/${activeConflict.value.id}/resolve`, { fields: resolution })
    if (!res.success) throw new Error(res.error || 'Could not resolve conflict')
    $q.notify({ type: 'positive', message: 'Conflict resolved' })
    conflictDialog.value = false
    await loadAll()
  } catch (e) {
    $q.notify({ type: 'negative', message: e instanceof Error ? e.message : 'Could not resolve conflict' })
  } finally {
    actionLoading.value = ''
  }
}

onMounted(async () => {
  await loadAll()
  if (route.query.action === 'sync-all') {
    // Strip the query param so a refresh doesn't re-trigger, then auto-sync.
    void router.replace({ path: '/sync' })
    await doSyncAll()
  }
})
onActivated(loadAll)  // Reload when navigating back from Settings
onUnmounted(stopPolling)
</script>

<style scoped>
.sync-page {
  max-width: 1400px;
  margin: 0 auto;
}

:deep(.sync-row-failed) {
  background: rgba(244, 67, 54, 0.08);
}
:deep(.sync-row-just-synced) {
  background: rgba(76, 175, 80, 0.07);
}
</style>
