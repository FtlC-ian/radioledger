import { useQuasar } from 'quasar'
import type { Ref } from 'vue'
import { getApiErrorMessage, apiGet } from 'src/api/client'
import { LOTW_USE_MOCK, mockCertInfo } from 'src/composables/useLotwMock'
import * as lotwApi from 'src/services/lotwApi'
import type {
  SyncConflict,
  SyncConflictsResponse,
  SyncHistoryItem,
  SyncHistoryResponse,
  SyncProgress,
  SyncStatusResponse,
  SyncStatusRow,
  SyncTablePagination,
  SyncTableFilters,
} from 'src/types/sync'

interface UseSyncDashboardDataOptions {
  loading: Ref<boolean>
  serviceHealth: Ref<Record<string, string>>
  syncProgress: Ref<Record<string, SyncProgress>>
  items: Ref<SyncStatusRow[]>
  totalRows: Ref<number>
  history: Ref<SyncHistoryItem[]>
  conflicts: Ref<SyncConflict[]>
  lotwHasCert: Ref<boolean>
  lotwCallsign: Ref<string>
  lotwRawPendingCount: Ref<number>
  filters: Ref<SyncTableFilters>
  tablePagination: Ref<SyncTablePagination>
  authCallsign: Ref<string | undefined>
  startPolling: (service?: string) => void
}

interface UseSyncDashboardDataDeps {
  apiGet?: typeof apiGet
  getApiErrorMessage?: typeof getApiErrorMessage
  getCertInfo?: typeof lotwApi.getCertInfo
  getPendingCount?: typeof lotwApi.getPendingCount
  notify?: (opts: { type: string; message: string }) => void
}

export function useSyncDashboardData(
  options: UseSyncDashboardDataOptions,
  deps: UseSyncDashboardDataDeps = {},
) {
  const $q = deps.notify ? null : useQuasar()
  const notify = deps.notify ?? ((opts: { type: string; message: string }) => $q.notify(opts))
  const get = deps.apiGet ?? apiGet
  const getErrorMessage = deps.getApiErrorMessage ?? getApiErrorMessage
  const getCertInfo = deps.getCertInfo ?? (() => (LOTW_USE_MOCK ? Promise.resolve(mockCertInfo) : lotwApi.getCertInfo()))
  const getPendingCount = deps.getPendingCount ?? lotwApi.getPendingCount

  async function loadStatus() {
    const p = options.tablePagination.value
    const q = new URLSearchParams()
    q.set('page', String(p.page))
    q.set('page_size', String(p.rowsPerPage))
    if (options.filters.value.callsign) q.set('callsign', options.filters.value.callsign)
    if (options.filters.value.service) q.set('service', options.filters.value.service)
    if (options.filters.value.status) q.set('status', options.filters.value.status)
    if (options.filters.value.dateFrom) q.set('date_from', options.filters.value.dateFrom)
    if (options.filters.value.dateTo) q.set('date_to', options.filters.value.dateTo)

    try {
      const res = await get<SyncStatusResponse>(`/v1/sync/status?${q.toString()}`)
      options.items.value = res.success && Array.isArray(res.data?.items) ? res.data.items : []
      options.totalRows.value = Number(res.data?.pagination?.total || 0)
      options.tablePagination.value.rowsNumber = options.totalRows.value
      options.syncProgress.value = ((res.success ? res.data?.services : {}) || {}) as Record<string, SyncProgress>
    } catch {
      options.items.value = []
      options.totalRows.value = 0
      options.tablePagination.value.rowsNumber = 0
      options.syncProgress.value = {}
      throw new Error('Could not load sync status')
    }
  }

  async function loadServices() {
    try {
      const res = await get<{ services: Record<string, string> }>('/v1/sync/services')
      options.serviceHealth.value = res.success ? res.data?.services || {} : {}
    } catch {
      options.serviceHealth.value = {}
      throw new Error('Could not load sync services')
    }
  }

  async function loadHistory() {
    try {
      const res = await get<SyncHistoryResponse>('/v1/sync/history?limit=200')
      options.history.value = res.success && res.data?.items ? res.data.items : []
    } catch {
      options.history.value = []
      throw new Error('Could not load sync history')
    }
  }

  async function loadConflicts() {
    try {
      const res = await get<SyncConflictsResponse>('/v1/sync/conflicts?page=1&page_size=200')
      options.conflicts.value = res.success && res.data?.items ? res.data.items : []
    } catch {
      options.conflicts.value = []
      throw new Error('Could not load sync conflicts')
    }
  }

  async function loadLotwCert() {
    try {
      const certInfo = await getCertInfo()
      options.lotwHasCert.value = !!certInfo
      options.lotwCallsign.value = certInfo?.callsign || options.authCallsign.value || ''
    } catch {
      options.lotwHasCert.value = false
      options.lotwCallsign.value = options.authCallsign.value || ''
    }

    if (options.lotwHasCert.value) {
      try {
        const pending = await getPendingCount()
        options.lotwRawPendingCount.value = pending.pending_count
      } catch {
        options.lotwRawPendingCount.value = 0
      }
    } else {
      options.lotwRawPendingCount.value = 0
    }
  }

  async function loadAll() {
    options.loading.value = true
    try {
      const results = await Promise.allSettled([
        loadServices(),
        loadStatus(),
        loadHistory(),
        loadConflicts(),
        loadLotwCert(),
      ])
      const firstFailure = results.find((result) => result.status === 'rejected')

      if (Object.values(options.syncProgress.value).some((service) => service?.is_running)) {
        options.startPolling()
      }

      if (firstFailure?.status === 'rejected') {
        notify({
          type: 'warning',
          message: getErrorMessage(firstFailure.reason, 'Some sync data could not be loaded.'),
        })
      }
    } finally {
      options.loading.value = false
    }
  }

  function applyFilters() {
    options.tablePagination.value.page = 1
    void loadStatus()
  }

  function resetFilters() {
    options.filters.value = { callsign: '', service: '', status: '', dateFrom: '', dateTo: '' }
    applyFilters()
  }

  function onTableRequest(props: { pagination: { page: number; rowsPerPage: number } }) {
    options.tablePagination.value.page = props.pagination.page
    options.tablePagination.value.rowsPerPage = props.pagination.rowsPerPage
    void loadStatus()
  }

  return {
    loadStatus,
    loadServices,
    loadHistory,
    loadConflicts,
    loadLotwCert,
    loadAll,
    applyFilters,
    resetFilters,
    onTableRequest,
  }
}
