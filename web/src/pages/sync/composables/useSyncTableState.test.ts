import { computed, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useSyncTableState } from './useSyncTableState'
import type { SyncStatusRow, SyncTableFilters } from 'src/types/sync'

function row(overrides: Partial<SyncStatusRow> = {}): SyncStatusRow {
  return {
    qso_uuid: 'qso-1',
    callsign: 'W1AW',
    band: '20m',
    mode: 'FT8',
    datetime_on: '2026-04-10T15:00:00Z',
    has_conflict: false,
    service_statuses: [],
    ...overrides,
  }
}

function filters(overrides: Partial<SyncTableFilters> = {}): SyncTableFilters {
  return {
    callsign: '',
    service: '',
    status: '',
    dateFrom: '',
    dateTo: '',
    ...overrides,
  }
}

describe('useSyncTableState', () => {
  it('keeps just-synced rows visible when hideCompleted is enabled', () => {
    const items = ref<SyncStatusRow[]>([
      row({
        qso_uuid: 'pending-row',
        service_statuses: [{ service: 'eqsl', status: 'pending' }],
      }),
      row({
        qso_uuid: 'just-synced-row',
        service_statuses: [{ service: 'eqsl', status: 'uploaded' }],
      }),
      row({
        qso_uuid: 'completed-row',
        service_statuses: [{ service: 'eqsl', status: 'uploaded' }],
      }),
    ])
    const hideCompleted = ref(true)
    const justSyncedUUIDs = ref(new Set(['just-synced-row']))
    const state = useSyncTableState({
      items,
      hideCompleted,
      justSyncedUUIDs,
      filters: computed(() => filters()),
    })

    expect(state.displayedItems.value.map((item) => item.qso_uuid)).toEqual(['pending-row', 'just-synced-row'])
  })

  it('captures currently pending rows as just-synced candidates', () => {
    const items = ref<SyncStatusRow[]>([
      row({
        qso_uuid: 'pending-row',
        service_statuses: [{ service: 'eqsl', status: 'pending' }],
      }),
      row({
        qso_uuid: 'failed-row',
        service_statuses: [{ service: 'qrz', status: 'failed' }],
      }),
      row({
        qso_uuid: 'done-row',
        service_statuses: [{ service: 'clublog', status: 'uploaded' }],
      }),
    ])
    const justSyncedUUIDs = ref(new Set<string>())
    const state = useSyncTableState({
      items,
      hideCompleted: ref(false),
      justSyncedUUIDs,
      filters: computed(() => filters()),
    })

    state.captureCurrentPendingUUIDs()

    expect([...justSyncedUUIDs.value]).toEqual(['pending-row', 'failed-row'])
  })

  it('marks just-synced rows differently from failed rows', () => {
    const state = useSyncTableState({
      items: ref<SyncStatusRow[]>([]),
      hideCompleted: ref(false),
      justSyncedUUIDs: ref(new Set(['just-synced-row'])),
      filters: computed(() => filters()),
    })

    expect(
      state.tableRowClass(
        row({
          qso_uuid: 'just-synced-row',
          service_statuses: [{ service: 'eqsl', status: 'uploaded' }],
        }),
      ),
    ).toBe('sync-row-just-synced')

    expect(
      state.tableRowClass(
        row({
          qso_uuid: 'failed-row',
          service_statuses: [{ service: 'eqsl', status: 'failed' }],
        }),
      ),
    ).toBe('sync-row-failed')
  })

  it('turns off hideCompleted when a status filter is applied', async () => {
    const items = ref<SyncStatusRow[]>([])
    const hideCompleted = ref(true)
    const justSyncedUUIDs = ref(new Set<string>())
    const activeFilters = ref(filters())

    useSyncTableState({
      items,
      hideCompleted,
      justSyncedUUIDs,
      filters: activeFilters,
    })

    activeFilters.value = filters({ status: 'pending' })
    await Promise.resolve()

    expect(hideCompleted.value).toBe(false)
  })
})
