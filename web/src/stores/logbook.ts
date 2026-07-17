import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { apiDelete, apiGet, apiPost, apiPut } from 'src/api/client'
import type { CursorPage, LogbookStats, Qso, QsoPayload, QsoSearchFilters } from 'src/types/qso'

const DEFAULT_PAGE_SIZE = 25

type SortField = 'datetime_on' | 'callsign' | 'frequency' | 'mode' | 'band' | 'rst_sent' | 'rst_rcvd' | 'notes'

interface TablePagination {
  page: number
  rowsPerPage: number
  totalRows: number
  sortBy: SortField
  descending: boolean
}

type FetchQsoOptions = Partial<Pick<TablePagination, 'page' | 'rowsPerPage' | 'sortBy' | 'descending'>> & {
  reset?: boolean
}

function normalizeQso(raw: unknown): Qso {
  const item = (raw ?? {}) as Record<string, unknown>

  return {
    ...(item as Qso),
    datetime_on: (item.datetime_on as string | undefined) || '',
    gridsquare: (item.gridsquare as string | null | undefined) ?? null,
    notes: (item.notes as string | null | undefined) ?? (item.comment as string | null | undefined) ?? null,
  }
}

function normalizeQsoList(payload: unknown, pageSize: number): CursorPage<Qso> {
  const data = (payload ?? {}) as Record<string, unknown>

  const rawItems = Array.isArray(payload)
    ? payload
    : Array.isArray(data.items)
      ? data.items
      : Array.isArray(data.qsos)
        ? data.qsos
        : []

  const items = rawItems.map((item) => normalizeQso(item))

  const nextCursorRaw =
    (data.next_cursor as string | undefined) ||
    (data.nextCursor as string | undefined) ||
    (data.cursor as string | undefined)

  const hasMoreRaw = data.has_more as boolean | undefined
  const hasMore = typeof hasMoreRaw === 'boolean' ? hasMoreRaw : Boolean(nextCursorRaw || items.length >= pageSize)

  return {
    items,
    nextCursor: nextCursorRaw || null,
    hasMore,
  }
}

function bandsBreakdownFromQsos(qsos: Qso[]) {
  const counts = new Map<string, number>()

  for (const qso of qsos) {
    const key = qso.band || 'Unknown'
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }

  return Array.from(counts.entries())
    .map(([band, count]) => ({ band, count }))
    .sort((a, b) => b.count - a.count)
}

function formatDateFromFilter(value: string | undefined): string | undefined {
  if (!value) return undefined
  if (value.includes('T')) return new Date(value).toISOString()
  return new Date(`${value}T00:00:00.000Z`).toISOString()
}

function formatDateToFilter(value: string | undefined): string | undefined {
  if (!value) return undefined
  if (value.includes('T')) return new Date(value).toISOString()
  return new Date(`${value}T23:59:59.999Z`).toISOString()
}

function mapSortField(sortBy: string): string {
  if (sortBy === 'date_time') return 'datetime_on'
  if (sortBy === 'notes') return 'notes'
  return sortBy
}

function toApiPayload(payload: QsoPayload): Record<string, unknown> {
  return {
    callsign: payload.callsign.trim().toUpperCase(),
    band: payload.band,
    mode: payload.mode,
    datetime_on: payload.datetime_on || payload.qso_datetime,
    rst_sent: payload.rst_sent || null,
    rst_rcvd: payload.rst_rcvd || null,
    gridsquare: payload.gridsquare || payload.grid || null,
    dxcc: payload.dxcc ?? null,
    comment: payload.comment || null,
    notes: payload.notes || null,
    // NOTE: frequency field omitted - backend uses DisallowUnknownFields() and rejects it.
    // Re-add when backend adds frequency support to qsoUpsertRequest.
  }
}

export const useLogbookStore = defineStore('logbook', () => {
  const qsos = ref<Qso[]>([])
  const stats = ref<LogbookStats | null>(null)
  const statsLoading = ref<boolean>(false)
  const loading = ref<boolean>(false)
  const filters = ref<QsoSearchFilters>({})
  const error = ref<string | null>(null)

  const pagination = ref<TablePagination>({
    page: 1,
    rowsPerPage: DEFAULT_PAGE_SIZE,
    totalRows: 0,
    sortBy: 'datetime_on',
    descending: true,
  })

  const pageCursors = ref<Record<number, string | null>>({ 1: null })
  const hasMore = ref<boolean>(true)

  // Active logbook UUID — lazy-fetched from /v1/logbooks/default on first use.
  const activeLogbookUuid = ref<string | null>(
    typeof localStorage !== 'undefined' ? localStorage.getItem('radioledger.logbook_uuid') : null,
  )
  const logbookLoading = ref<boolean>(false)

  const totalQsos = computed(() => stats.value?.total_qsos ?? pagination.value.totalRows ?? qsos.value.length)
  const countriesWorked = computed(() => {
    if (stats.value) {
      return stats.value.unique_countries
    }

    return new Set(
      qsos.value
        .map((qso) => qso.country)
        .filter((country): country is string => Boolean(country && country.length > 0)),
    ).size
  })

  const bandsBreakdown = computed(() => {
    if (stats.value?.bands) {
      return Object.entries(stats.value.bands)
        .map(([band, count]) => ({ band, count }))
        .sort((a, b) => b.count - a.count)
    }

    return bandsBreakdownFromQsos(qsos.value)
  })

  /** Ensure we have a logbook UUID, fetching the default if needed. */
  async function ensureLogbook(): Promise<string> {
    if (activeLogbookUuid.value) return activeLogbookUuid.value

    logbookLoading.value = true
    try {
      const response = await apiGet<{ uuid: string; name: string }>('/v1/logbooks/default')
      if (response.success && response.data?.uuid) {
        activeLogbookUuid.value = response.data.uuid
        if (typeof localStorage !== 'undefined') {
          localStorage.setItem('radioledger.logbook_uuid', response.data.uuid)
        }
        return response.data.uuid
      }
      throw new Error(response.error || 'No default logbook found')
    } finally {
      logbookLoading.value = false
    }
  }

  async function fetchStats() {
    if (statsLoading.value) {
      return
    }

    statsLoading.value = true
    try {
      const response = await apiGet<LogbookStats>('/v1/stats')
      if (response.success && response.data) {
        stats.value = response.data
        if (!hasActiveFilters()) {
          pagination.value.totalRows = response.data.total_qsos
        }
      }
    } finally {
      statsLoading.value = false
    }
  }

  function hasActiveFilters(): boolean {
    return Boolean(
      filters.value.callsign || filters.value.band || filters.value.mode || filters.value.dateFrom || filters.value.dateTo,
    )
  }

  function updateTotalRowsFromPage(itemsCount: number) {
    const { page, rowsPerPage } = pagination.value
    if (!hasActiveFilters() && stats.value?.total_qsos != null) {
      pagination.value.totalRows = stats.value.total_qsos
      return
    }

    if (!hasMore.value) {
      pagination.value.totalRows = Math.max((page - 1) * rowsPerPage + itemsCount, 0)
      return
    }

    pagination.value.totalRows = Math.max(pagination.value.totalRows, page * rowsPerPage + 1)
  }

  function resetPagination() {
    pageCursors.value = { 1: null }
    hasMore.value = true
    pagination.value.page = 1
    pagination.value.totalRows = 0
  }

  async function fetchQsos(options: FetchQsoOptions = {}) {
    if (loading.value) {
      return
    }

    error.value = null

    const { reset = false, ...next } = options

    if (reset) {
      resetPagination()
    }

    pagination.value.page = Math.max(1, next.page ?? pagination.value.page)
    pagination.value.rowsPerPage = Math.max(1, next.rowsPerPage ?? pagination.value.rowsPerPage)
    pagination.value.sortBy = (next.sortBy ?? pagination.value.sortBy) as SortField
    pagination.value.descending = next.descending ?? pagination.value.descending

    const targetPage = pagination.value.page
    const cursor = pageCursors.value[targetPage] ?? null

    loading.value = true

    try {
      const logbookUuid = await ensureLogbook()
      const endpoint = `/v1/logbooks/${logbookUuid}/qsos`

      const params: Record<string, string | number> = {
        limit: pagination.value.rowsPerPage,
      }

      if (cursor) {
        params.after = cursor
      }

      if (filters.value.callsign) {
        params.callsign = filters.value.callsign
      }

      if (filters.value.band) {
        params.band = filters.value.band
      }

      if (filters.value.mode) {
        params.mode = filters.value.mode
      }

      const dateFrom = formatDateFromFilter(filters.value.dateFrom)
      const dateTo = formatDateToFilter(filters.value.dateTo)

      if (dateFrom) {
        params.date_from = dateFrom
      }

      if (dateTo) {
        params.date_to = dateTo
      }

      params.sort_by = mapSortField(pagination.value.sortBy)
      params.sort_order = pagination.value.descending ? 'desc' : 'asc'

      const response = await apiGet<unknown>(endpoint, { params })
      const page = normalizeQsoList(response.data, pagination.value.rowsPerPage)

      qsos.value = page.items
      hasMore.value = page.hasMore

      if (page.nextCursor) {
        pageCursors.value[targetPage + 1] = page.nextCursor
      } else {
        delete pageCursors.value[targetPage + 1]
      }

      updateTotalRowsFromPage(page.items.length)
    } catch (err) {
      const msg = (err as { error?: string; message?: string })?.error ||
        (err as { message?: string })?.message ||
        'Unable to load QSOs'
      error.value = msg
      qsos.value = []
      hasMore.value = false
      pagination.value.totalRows = 0
    } finally {
      loading.value = false
    }
  }

  async function loadMore() {
    if (!hasMore.value || loading.value) {
      return
    }
    const nextPage = pagination.value.page + 1
    await fetchQsos({ page: nextPage })
  }

  async function search(nextFilters: QsoSearchFilters) {
    filters.value = {
      callsign: nextFilters.callsign?.trim() || undefined,
      band: nextFilters.band || undefined,
      mode: nextFilters.mode || undefined,
      dateFrom: nextFilters.dateFrom || undefined,
      dateTo: nextFilters.dateTo || undefined,
    }

    resetPagination()
    await fetchQsos({ reset: true })
  }

  async function createQso(payload: QsoPayload) {
    const logbookUuid = await ensureLogbook()
    const response = await apiPost<Qso, Record<string, unknown>>(
      `/v1/logbooks/${logbookUuid}/qsos`,
      toApiPayload(payload),
    )

    if (response.success && response.data) {
      await fetchStats()
      pagination.value.page = 1
      pageCursors.value = { 1: null }
      await fetchQsos({ page: 1 })
    }

    return response
  }

  async function updateQso(uuid: string, payload: Partial<QsoPayload>) {
    const existingQso = qsos.value.find((q) => q.uuid === uuid)
    const logbookUuid = existingQso?.logbook_uuid || activeLogbookUuid.value || (await ensureLogbook())

    const response = await apiPut<Qso, Record<string, unknown>>(
      `/v1/logbooks/${logbookUuid}/qsos/${uuid}`,
      toApiPayload(payload as QsoPayload),
    )

    if (response.success) {
      await fetchQsos({ page: pagination.value.page })
      await fetchStats()
    }

    return response
  }

  async function deleteQso(uuid: string) {
    const existingQso = qsos.value.find((q) => q.uuid === uuid)
    const logbookUuid = existingQso?.logbook_uuid || activeLogbookUuid.value || (await ensureLogbook())

    const response = await apiDelete<unknown>(`/v1/logbooks/${logbookUuid}/qsos/${uuid}`)

    if (response.success) {
      await fetchQsos({ page: pagination.value.page })
      await fetchStats()
    }

    return response
  }

  /** Clear cached logbook UUID (e.g., on logout). */
  function clearLogbook() {
    activeLogbookUuid.value = null
    qsos.value = []
    stats.value = null
    filters.value = {}
    error.value = null
    resetPagination()
  }

  return {
    qsos,
    stats,
    loading,
    statsLoading,
    logbookLoading,
    hasMore,
    filters,
    error,
    pagination,
    activeLogbookUuid,
    totalQsos,
    countriesWorked,
    bandsBreakdown,
    ensureLogbook,
    fetchStats,
    fetchQsos,
    loadMore,
    search,
    createQso,
    updateQso,
    deleteQso,
    clearLogbook,
  }
})
