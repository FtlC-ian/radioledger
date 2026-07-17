import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('src/api/client', () => ({
  apiPost: vi.fn(),
}))

import { useSyncActions } from './useSyncActions'

function createState(overrides: {
  servicePendingCount?: (service: string) => number
  lotwHasCert?: boolean
  knownServices?: readonly string[]
} = {}) {
  const actionLoading = ref('')
  const syncAllLoading = ref(false)
  const pendingSyncAllAfterLotw = ref(false)
  const showLotwModal = ref(false)
  const startPolling = vi.fn()
  const stopPolling = vi.fn()
  const loadStatus = vi.fn(async () => {})
  const captureCurrentPendingUUIDs = vi.fn()
  const routerPush = vi.fn()
  const notify = vi.fn(() => vi.fn())
  const apiPost = vi.fn()

  const state = useSyncActions(
    {
      actionLoading,
      syncAllLoading,
      knownServices: overrides.knownServices ?? ['eqsl', 'clublog', 'qrz', 'lotw'],
      lotwHasCert: ref(overrides.lotwHasCert ?? true),
      pendingSyncAllAfterLotw,
      showLotwModal,
      captureCurrentPendingUUIDs,
      servicePendingCount: overrides.servicePendingCount ?? (() => 0),
      startPolling,
      stopPolling,
      loadStatus,
      routerPush,
    },
    {
      apiPost,
      notify,
    },
  )

  return {
    state,
    actionLoading,
    syncAllLoading,
    pendingSyncAllAfterLotw,
    showLotwModal,
    startPolling,
    stopPolling,
    loadStatus,
    captureCurrentPendingUUIDs,
    routerPush,
    notify,
    apiPost,
  }
}

describe('useSyncActions', () => {
  it('opens the LoTW modal instead of bulk-uploading LoTW directly', async () => {
    const ctx = createState()

    await ctx.state.triggerServiceSync('lotw')

    expect(ctx.captureCurrentPendingUUIDs).toHaveBeenCalled()
    expect(ctx.showLotwModal.value).toBe(true)
    expect(ctx.apiPost).not.toHaveBeenCalled()
  })

  it('queues sync-all through the LoTW modal when LoTW still has pending work', async () => {
    const ctx = createState({
      servicePendingCount: (service) => (service === 'lotw' ? 3 : service === 'eqsl' ? 2 : 0),
      lotwHasCert: true,
    })

    await ctx.state.doSyncAll()

    expect(ctx.captureCurrentPendingUUIDs).toHaveBeenCalled()
    expect(ctx.pendingSyncAllAfterLotw.value).toBe(true)
    expect(ctx.showLotwModal.value).toBe(true)
    expect(ctx.apiPost).not.toHaveBeenCalled()
  })

  it('syncs other services after a LoTW modal completion during sync-all', async () => {
    const ctx = createState({
      servicePendingCount: (service) => (service === 'eqsl' ? 2 : service === 'qrz' ? 1 : 0),
    })
    ctx.pendingSyncAllAfterLotw.value = true
    ctx.apiPost.mockImplementation(async (url: string) => {
      if (url === '/v1/sync/verify-credentials') return { success: true, data: { success: true } }
      if (url === '/v1/sync/bulk-upload') return { success: true, data: {} }
      throw new Error(`unexpected url ${url}`)
    })

    await ctx.state.onLotwSynced(5)

    expect(ctx.loadStatus).toHaveBeenCalled()
    expect(ctx.startPolling).toHaveBeenNthCalledWith(1, 'lotw')
    expect(ctx.startPolling).toHaveBeenNthCalledWith(2)

    const bulkCalls = ctx.apiPost.mock.calls.filter(([url]) => url === '/v1/sync/bulk-upload')
    expect(bulkCalls).toEqual([
      ['/v1/sync/bulk-upload', { service: 'eqsl' }],
      ['/v1/sync/bulk-upload', { service: 'qrz' }],
    ])
    expect(ctx.pendingSyncAllAfterLotw.value).toBe(false)
  })

  it('does not start sync-all polling when every service fails before upload starts', async () => {
    const ctx = createState({
      servicePendingCount: (service) => (service === 'eqsl' || service === 'qrz' ? 1 : 0),
      lotwHasCert: false,
    })
    ctx.apiPost.mockImplementation(async (url: string, body?: { service?: string }) => {
      if (url === '/v1/sync/verify-credentials') {
        return { success: true, data: { success: false, error: `${body?.service} auth failed` } }
      }
      throw new Error(`unexpected url ${url}`)
    })

    await ctx.state.doSyncAll()

    expect(ctx.startPolling).not.toHaveBeenCalled()
    expect(ctx.apiPost).not.toHaveBeenCalledWith('/v1/sync/bulk-upload', expect.anything())
    expect(ctx.notify).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'warning',
        message: 'eQSL: eqsl auth failed',
      }),
    )
    expect(ctx.notify).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'warning',
        message: 'QRZ: qrz auth failed',
      }),
    )
  })

  it('cancelling the LoTW modal during sync-all clears the deferred state and notifies', () => {
    const ctx = createState()
    ctx.pendingSyncAllAfterLotw.value = true

    ctx.state.onLotwModalClosed(false)

    expect(ctx.pendingSyncAllAfterLotw.value).toBe(false)
    expect(ctx.notify).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'info',
        message: 'LoTW sync cancelled, other services were not synced.',
      }),
    )
  })

  it('cancels an in-flight sync, reloads status, and stops polling', async () => {
    const ctx = createState()
    ctx.apiPost.mockResolvedValue({ success: true, data: { cancelled: 4 } })

    await ctx.state.cancelSync('eqsl')

    expect(ctx.apiPost).toHaveBeenCalledWith('/v1/sync/cancel', { service: 'eqsl' })
    expect(ctx.loadStatus).toHaveBeenCalled()
    expect(ctx.stopPolling).toHaveBeenCalled()
    expect(ctx.actionLoading.value).toBe('')
  })
})
