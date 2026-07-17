import { computed, watch, type Ref } from 'vue'
import type { SyncStatusRow, SyncTableFilters } from 'src/types/sync'
import { needsSync, rowHasFailure } from 'src/utils/syncHelpers'

interface UseSyncTableStateOptions {
  items: Ref<SyncStatusRow[]>
  hideCompleted: Ref<boolean>
  justSyncedUUIDs: Ref<Set<string>>
  filters: Ref<SyncTableFilters>
}

export function useSyncTableState(opts: UseSyncTableStateOptions) {
  const displayedItems = computed(() => {
    if (!opts.hideCompleted.value) return opts.items.value
    return opts.items.value.filter((row) => needsSync(row) || opts.justSyncedUUIDs.value.has(row.qso_uuid))
  })

  watch(() => opts.filters.value.status, (status) => {
    if (status) opts.hideCompleted.value = false
  })

  function captureCurrentPendingUUIDs() {
    opts.items.value.forEach((row) => {
      if (needsSync(row)) opts.justSyncedUUIDs.value.add(row.qso_uuid)
    })
  }

  function tableRowClass(row: SyncStatusRow) {
    if (opts.justSyncedUUIDs.value.has(row.qso_uuid) && !needsSync(row)) return 'sync-row-just-synced'
    return rowHasFailure(row) ? 'sync-row-failed' : ''
  }

  return {
    displayedItems,
    captureCurrentPendingUUIDs,
    tableRowClass,
  }
}
