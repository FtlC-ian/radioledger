import { useQuasar } from 'quasar'
import type { Ref } from 'vue'
import { apiPost } from 'src/api/client'
import { formatNumber, serviceLabel } from 'src/utils/syncHelpers'

interface UseSyncActionsOptions {
  actionLoading: Ref<string>
  syncAllLoading: Ref<boolean>
  knownServices: readonly string[]
  lotwHasCert: Ref<boolean>
  pendingSyncAllAfterLotw: Ref<boolean>
  showLotwModal: Ref<boolean>
  captureCurrentPendingUUIDs: () => void
  servicePendingCount: (service: string) => number
  startPolling: (service?: string) => void
  stopPolling: () => void
  loadStatus: () => Promise<void>
  routerPush: (to: string) => Promise<unknown> | void
}

type SyncActionNotification = {
  type: string
  message: string
  timeout?: number
  spinner?: boolean
  actions?: Array<{ label: string; color: string; handler: () => void }>
}

interface UseSyncActionsDeps {
  apiPost?: typeof apiPost
  notify?: (opts: SyncActionNotification) => void | (() => void)
}

export function useSyncActions(
  options: UseSyncActionsOptions,
  deps: UseSyncActionsDeps = {},
) {
  const $q = deps.notify ? null : useQuasar()
  const notify = deps.notify ?? ((opts: SyncActionNotification) => $q.notify(opts))
  const post = deps.apiPost ?? apiPost

  async function verifyCredentials(service: string) {
    const dismiss = notify({
      spinner: true,
      timeout: 0,
      message: `Checking ${serviceLabel(service)} credentials…`,
      type: 'info',
    }) as (() => void) | void
    try {
      const res = await post<{ success: boolean; error?: string }>('/v1/sync/verify-credentials', { service })
      if (!res.success) throw new Error(res.error || 'Could not verify credentials')
      if (!res.data?.success) throw new Error(res.data?.error || `${serviceLabel(service)} authentication failed.`)
    } finally {
      dismiss?.()
    }
  }

  async function bulkSync(service: string) {
    options.actionLoading.value = `bulk-${service}`
    try {
      await verifyCredentials(service)
      const res = await post('/v1/sync/bulk-upload', { service })
      if (!res.success) throw new Error(res.error || 'Sync failed')
      notify({ type: 'info', message: `Syncing to ${serviceLabel(service)}…`, timeout: 2000 })
      options.startPolling(service)
    } catch (e) {
      const message =
        e instanceof Error
          ? (service === 'qrz' ? 'QRZ authentication failed. Check API key in Settings.' : e.message)
          : 'Sync failed'
      notify({
        type: 'negative',
        message,
        timeout: 5000,
        actions: ['qrz', 'eqsl', 'clublog'].includes(service)
          ? [{ label: 'Settings', color: 'white', handler: () => void options.routerPush('/settings?tab=sync') }]
          : undefined,
      })
    } finally {
      options.actionLoading.value = ''
    }
  }

  async function triggerServiceSync(service: string) {
    if (service === 'lotw') {
      options.captureCurrentPendingUUIDs()
      options.showLotwModal.value = true
      return
    }

    options.captureCurrentPendingUUIDs()
    await bulkSync(service)
  }

  async function syncMultipleServices(services: string[]) {
    if (!services.length) return

    options.syncAllLoading.value = true
    let startedAnyUpload = false
    try {
      for (const service of services) {
        try {
          await verifyCredentials(service)
          const res = await post('/v1/sync/bulk-upload', { service })
          if (!res.success) throw new Error(res.error || `Sync failed for ${serviceLabel(service)}`)
          startedAnyUpload = true
        } catch (e) {
          notify({
            type: 'warning',
            message: `${serviceLabel(service)}: ${e instanceof Error ? e.message : 'Sync failed'}`,
            timeout: 4000,
          })
        }
      }
      if (startedAnyUpload) options.startPolling()
    } finally {
      options.syncAllLoading.value = false
    }
  }

  async function doSyncAll() {
    options.captureCurrentPendingUUIDs()
    const lotwPending = options.servicePendingCount('lotw')
    const otherServices = options.knownServices.filter((s) => s !== 'lotw' && options.servicePendingCount(s) > 0)

    if (lotwPending > 0 && options.lotwHasCert.value) {
      options.pendingSyncAllAfterLotw.value = true
      options.showLotwModal.value = true
    } else {
      await syncMultipleServices(otherServices)
    }
  }

  async function onLotwSynced(_count: number) {
    void options.loadStatus()
    options.startPolling('lotw')

    if (options.pendingSyncAllAfterLotw.value) {
      options.pendingSyncAllAfterLotw.value = false
      const otherServices = options.knownServices.filter((s) => s !== 'lotw' && options.servicePendingCount(s) > 0)
      await syncMultipleServices(otherServices)
    }
  }

  function onLotwModalClosed(open: boolean) {
    if (!open && options.pendingSyncAllAfterLotw.value) {
      options.pendingSyncAllAfterLotw.value = false
      notify({ type: 'info', message: 'LoTW sync cancelled, other services were not synced.', timeout: 3000 })
    }
  }

  async function retryFailed(service: string) {
    options.actionLoading.value = `retry-${service}`
    try {
      const res = await post('/v1/sync/retry', { service })
      if (!res.success) throw new Error(res.error || 'Retry failed')
      notify({ type: 'positive', message: `Retry queued for ${serviceLabel(service)}` })
      options.startPolling(service)
    } catch (e) {
      notify({ type: 'negative', message: e instanceof Error ? e.message : 'Retry failed' })
    } finally {
      options.actionLoading.value = ''
    }
  }

  async function cancelSync(service: string) {
    options.actionLoading.value = `cancel-${service}`
    try {
      const res = await post<{ cancelled?: number }>('/v1/sync/cancel', { service })
      if (!res.success) throw new Error(res.error || 'Cancel failed')
      const cancelled = Number(res.data?.cancelled || 0)
      notify({
        type: 'positive',
        message: `${serviceLabel(service)} sync cancelled (${formatNumber(cancelled)} pending cleared).`,
      })
      await options.loadStatus()
      options.stopPolling()
    } catch (e) {
      notify({ type: 'negative', message: e instanceof Error ? e.message : 'Cancel failed' })
    } finally {
      options.actionLoading.value = ''
    }
  }

  return {
    verifyCredentials,
    triggerServiceSync,
    bulkSync,
    doSyncAll,
    syncMultipleServices,
    onLotwSynced,
    onLotwModalClosed,
    retryFailed,
    cancelSync,
  }
}
