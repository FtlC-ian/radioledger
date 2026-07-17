/**
 * useSyncPolling — manages the client-side polling loop used while a sync
 * job is running on the server.
 *
 * Responsibilities:
 * - Start/stop the 2 s interval that refreshes sync progress.
 * - Track which service is "primary" (the one the user explicitly triggered).
 * - Show a Quasar notification when the sync completes, fails, or stalls.
 * - Guard against polling running forever (hard 120 s cap).
 *
 * This composable is intentionally side-effect-free outside of its
 * start/stop interface. The caller (SyncPage) owns the syncProgress ref
 * and passes a `loadStatus` callback so this composable does not need
 * its own API imports.
 *
 * Usage:
 * ```ts
 * const { startPolling, stopPolling } = useSyncPolling({
 *   syncProgress,
 *   activeSyncServices,
 *   knownServices,
 *   servicePendingCount,
 *   onPoll: () => Promise.all([loadStatus(), loadConflicts()]),
 * })
 * ```
 */

import { ref } from 'vue'
import { useQuasar } from 'quasar'
import type { SyncProgress } from 'src/types/sync'
import { formatNumber, serviceLabel } from 'src/utils/syncHelpers'

/** Maximum wall-clock time a polling session will run before auto-stopping (ms). */
const POLL_MAX_DURATION_MS = 120_000
/** Interval between each poll tick (ms). */
const POLL_INTERVAL_MS = 2_000

export interface UseSyncPollingOptions {
  /** Reactive map of per-service sync progress (read-only from this composable's perspective). */
  syncProgress: { value: Record<string, SyncProgress> }
  /**
   * Set of services that have been triggered in this UI session.
   * startPolling() adds to this set so callers don't need to manage it separately.
   */
  activeSyncServices: { value: Set<string> }
  /** Ordered list of all known service keys. */
  knownServices: readonly string[]
  /**
   * Returns the pending count for a service (may differ from syncProgress when
   * LoTW raw-pending fallback is in play — pass SyncPage's own function).
   */
  servicePendingCount: (svc: string) => number
  /**
   * Called on each tick. Should refresh syncProgress and any other reactive
   * state the page needs. Throwing or rejecting stops polling and shows a
   * warning notification.
   */
  onPoll: () => Promise<void>
}

export function useSyncPolling(opts: UseSyncPollingOptions) {
  const $q = useQuasar()

  const pollTimer = ref<number | null>(null)
  const pollStartedAt = ref(0)
  const pollPrimaryService = ref('')
  /** Set to true after the primary service's progress shows is_running=true at least once. */
  const pollSawRunning = ref(false)
  /** Guard flag so we only show one failure toast per sync run. */
  const failureToastShown = ref(false)

  /** Stop any running poll interval. Safe to call when already stopped. */
  function stopPolling() {
    if (pollTimer.value != null) {
      window.clearInterval(pollTimer.value)
      pollTimer.value = null
    }
  }

  /**
   * Inner tick function — runs on every interval.
   *
   * Decision tree:
   * 1. Call onPoll() — if it throws, stop + warn.
   * 2. Hard-timeout: if > 120 s since start, stop.
   * 3. If primary service is stalled: stop + warn.
   * 4. If primary service finished (pending=0): stop + notify result.
   * 5. If no services are running after we have seen running: stop.
   */
  async function tick() {
    try {
      await opts.onPoll()
    } catch {
      stopPolling()
      $q.notify({
        type: 'warning',
        message: 'Sync status refresh failed. Please refresh the page in a moment.',
      })
      return
    }

    const elapsed = Date.now() - pollStartedAt.value
    if (elapsed > POLL_MAX_DURATION_MS) {
      stopPolling()
      return
    }

    const progress = opts.syncProgress.value
    const primary = pollPrimaryService.value ? progress[pollPrimaryService.value] : undefined
    const anyRunning = Object.values(progress).some((s) => s?.is_running)

    if (primary?.is_running) pollSawRunning.value = true

    if (primary?.is_stalled) {
      stopPolling()
      $q.notify({
        type: 'warning',
        message: `${serviceLabel(pollPrimaryService.value)} sync appears stalled — no activity for 60s with no running worker.`,
      })
      return
    }

    if (primary && primary.pending_count === 0) {
      stopPolling()
      if (primary.failed_count > 0) {
        if (!failureToastShown.value) {
          failureToastShown.value = true
          $q.notify({
            type: primary.uploaded_count > 0 ? 'warning' : 'negative',
            message: `${serviceLabel(pollPrimaryService.value)} sync complete — ${formatNumber(primary.uploaded_count)} synced, ${formatNumber(primary.failed_count)} failed.`,
          })
        }
      } else if (primary.uploaded_count > 0) {
        $q.notify({
          type: 'positive',
          message: `${serviceLabel(pollPrimaryService.value)} sync complete — ${formatNumber(primary.uploaded_count)} QSOs synced.`,
        })
      }
      return
    }

    // If we ever saw running and nothing is running anymore, we're done.
    if (!anyRunning && pollSawRunning.value) stopPolling()
  }

  /**
   * Start the polling loop.
   *
   * @param service — the service key the user explicitly triggered, or '' for Sync All.
   *   When '' (Sync All), all services with pending work are added to activeSyncServices.
   */
  function startPolling(service = '') {
    stopPolling()
    pollPrimaryService.value = service
    pollStartedAt.value = Date.now()
    pollSawRunning.value = false
    failureToastShown.value = false

    // Track which services are actively syncing this session.
    if (service) {
      opts.activeSyncServices.value.add(service)
    } else {
      // Sync All — mark all services with pending work.
      opts.knownServices.forEach((s) => {
        if (opts.servicePendingCount(s) > 0) opts.activeSyncServices.value.add(s)
      })
    }

    pollTimer.value = window.setInterval(() => void tick(), POLL_INTERVAL_MS)
    // Fire immediately so UI feels responsive.
    void tick()
  }

  return { startPolling, stopPolling }
}
