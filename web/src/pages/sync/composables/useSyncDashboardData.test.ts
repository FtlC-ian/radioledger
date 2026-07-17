import { computed, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('src/api/client', () => ({
  apiGet: vi.fn(),
  getApiErrorMessage: vi.fn(),
}))

import { useSyncDashboardData } from './useSyncDashboardData'
import type { SyncConflict, SyncHistoryItem, SyncProgress, SyncStatusRow, SyncTableFilters, SyncTablePagination } from 'src/types/sync'

function createState() {
  const loading = ref(false)
  const serviceHealth = ref<Record<string, string>>({})
  const syncProgress = ref<Record<string, SyncProgress>>({})
  const items = ref<SyncStatusRow[]>([])
  const totalRows = ref(0)
  const history = ref<SyncHistoryItem[]>([])
  const conflicts = ref<SyncConflict[]>([])
  const lotwHasCert = ref(false)
  const lotwCallsign = ref('')
  const lotwRawPendingCount = ref(0)
  const filters = ref<SyncTableFilters>({
    callsign: '',
    service: '',
    status: '',
    dateFrom: '',
    dateTo: '',
  })
  const tablePagination = ref<SyncTablePagination>({ page: 2, rowsPerPage: 50, rowsNumber: 0 })
  const startPolling = vi.fn()
  const notify = vi.fn()
  const getApiErrorMessage = vi.fn((_error: unknown, fallback: string) => fallback)
  const apiGet = vi.fn()
  const getCertInfo = vi.fn()
  const getPendingCount = vi.fn()

  const state = useSyncDashboardData(
    {
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
      authCallsign: computed(() => 'N0CALL'),
      startPolling,
    },
    {
      apiGet,
      getApiErrorMessage,
      getCertInfo,
      getPendingCount,
      notify,
    },
  )

  return {
    state,
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
    startPolling,
    notify,
    getApiErrorMessage,
    apiGet,
    getCertInfo,
    getPendingCount,
  }
}

describe('useSyncDashboardData', () => {
  it('builds the sync status query from pagination and filters', async () => {
    const ctx = createState()
    ctx.filters.value = {
      callsign: 'W1AW',
      service: 'eqsl',
      status: 'pending',
      dateFrom: '2026-04-01',
      dateTo: '2026-04-30',
    }
    ctx.apiGet.mockResolvedValue({ success: true, data: { items: [], pagination: { total: 7 }, services: {} } })

    await ctx.state.loadStatus()

    expect(ctx.apiGet).toHaveBeenCalledWith(
      '/v1/sync/status?page=2&page_size=50&callsign=W1AW&service=eqsl&status=pending&date_from=2026-04-01&date_to=2026-04-30',
    )
    expect(ctx.totalRows.value).toBe(7)
    expect(ctx.tablePagination.value.rowsNumber).toBe(7)
  })

  it('loads LoTW certificate state and pending fallback count', async () => {
    const ctx = createState()
    ctx.getCertInfo.mockResolvedValue({ callsign: 'K1ABC' })
    ctx.getPendingCount.mockResolvedValue({ pending_count: 9, oldest_unsynced: '2026-04-01T00:00:00Z' })

    await ctx.state.loadLotwCert()

    expect(ctx.lotwHasCert.value).toBe(true)
    expect(ctx.lotwCallsign.value).toBe('K1ABC')
    expect(ctx.lotwRawPendingCount.value).toBe(9)
  })

  it('starts polling after a dashboard load when a service is already running', async () => {
    const ctx = createState()
    ctx.getCertInfo.mockResolvedValue(null)
    ctx.apiGet.mockImplementation(async (url: string) => {
      if (url === '/v1/sync/services') return { success: true, data: { services: { eqsl: 'connected' } } }
      if (url.startsWith('/v1/sync/status')) {
        return {
          success: true,
          data: {
            items: [],
            pagination: { total: 0 },
            services: {
              eqsl: {
                pending_count: 2,
                uploaded_count: 0,
                failed_count: 0,
                is_running: true,
                is_stalled: false,
              },
            },
          },
        }
      }
      if (url === '/v1/sync/history?limit=200') return { success: true, data: { items: [] } }
      if (url === '/v1/sync/conflicts?page=1&page_size=200') return { success: true, data: { items: [] } }
      throw new Error(`unexpected url ${url}`)
    })

    await ctx.state.loadAll()

    expect(ctx.startPolling).toHaveBeenCalledWith()
    expect(ctx.loading.value).toBe(false)
  })

  it('notifies when part of the dashboard payload fails but still clears loading', async () => {
    const ctx = createState()
    ctx.getCertInfo.mockResolvedValue(null)
    ctx.apiGet.mockImplementation(async (url: string) => {
      if (url === '/v1/sync/services') throw new Error('services down')
      if (url.startsWith('/v1/sync/status')) return { success: true, data: { items: [], pagination: { total: 0 }, services: {} } }
      if (url === '/v1/sync/history?limit=200') return { success: true, data: { items: [] } }
      if (url === '/v1/sync/conflicts?page=1&page_size=200') return { success: true, data: { items: [] } }
      throw new Error(`unexpected url ${url}`)
    })

    await ctx.state.loadAll()

    expect(ctx.notify).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'warning',
        message: 'Some sync data could not be loaded.',
      }),
    )
    expect(ctx.loading.value).toBe(false)
  })

  it('resetFilters restores default filters and restarts from page 1', async () => {
    const ctx = createState()
    ctx.filters.value = {
      callsign: 'W1AW',
      service: 'eqsl',
      status: 'pending',
      dateFrom: '2026-04-01',
      dateTo: '2026-04-30',
    }
    ctx.apiGet.mockResolvedValue({ success: true, data: { items: [], pagination: { total: 0 }, services: {} } })

    ctx.state.resetFilters()
    await Promise.resolve()

    expect(ctx.filters.value).toEqual({
      callsign: '',
      service: '',
      status: '',
      dateFrom: '',
      dateTo: '',
    })
    expect(ctx.tablePagination.value.page).toBe(1)
  })
})
