import { computed, type Ref } from 'vue'
import type { SyncProgress } from 'src/types/sync'
import { healthColor, serviceLabel } from 'src/utils/syncHelpers'

export type SyncOverviewDisplayPolicyOptions = {
  serviceHealth: Ref<Record<string, string>>
  syncProgress: Ref<Record<string, SyncProgress>>
  activeSyncServices: Ref<Set<string>>
  lotwHasCert: Ref<boolean>
  lotwRawPendingCount: Ref<number>
}

export type ProgressCard = {
  service: string
  pending_count: number
  uploaded_count: number
  failed_count: number
  total_count: number
  percent: number
  error_message?: string
  is_running: boolean
  is_stalled: boolean
}

export function useSyncOverviewDisplay(options: SyncOverviewDisplayPolicyOptions) {
  const progressCards = computed<ProgressCard[]>(() => {
    return Object.entries(options.syncProgress.value)
      .filter(([service, progress]) => {
        if (progress?.is_running || progress?.is_stalled) return true
        if ((progress?.failed_count ?? 0) > 0) return true
        if ((progress?.uploaded_count ?? 0) > 0 && (progress?.pending_count ?? 0) === 0 && options.activeSyncServices.value.has(service)) {
          return true
        }
        return false
      })
      .map(([service, progress]) => {
        const computedTotal = progress.pending_count + progress.uploaded_count + progress.failed_count
        const total = progress.total_count && progress.total_count > 0 ? progress.total_count : computedTotal
        const percent = total > 0 ? Math.round((progress.uploaded_count / total) * 100) : 0
        return {
          service,
          pending_count: progress.pending_count,
          uploaded_count: progress.uploaded_count,
          failed_count: progress.failed_count,
          total_count: total,
          percent,
          error_message: progress.error_message || progress.last_error,
          is_running: progress.is_running,
          is_stalled: progress.is_stalled,
        }
      })
  })

  function servicePendingCount(service: string): number {
    const fromProgress = options.syncProgress.value[service]?.pending_count ?? 0
    if (service === 'lotw') return Math.max(fromProgress, options.lotwRawPendingCount.value)
    return fromProgress
  }

  function serviceConfigured(service: string): boolean {
    if (service === 'lotw') return options.lotwHasCert.value
    const status = options.serviceHealth.value[service]
    return Boolean(status && status !== 'not_configured')
  }

  function serviceConfigLabel(service: string): string {
    return serviceConfigured(service) ? 'Configure' : 'Set up'
  }

  function serviceConfigColor(service: string): string {
    return serviceConfigured(service) ? 'positive' : 'warning'
  }

  function serviceConfigTooltip(service: string): string {
    if (service === 'lotw' && !options.lotwHasCert.value) {
      return 'LoTW is not set up yet. Open Settings → Sync Services to upload a certificate.'
    }
    if (!serviceConfigured(service)) {
      return `${serviceLabel(service)} is not set up yet. Open Settings → Sync Services to connect it.`
    }
    return `Open Settings → Sync Services to review or update ${serviceLabel(service)}.`
  }

  function serviceHealthBadgeLabel(service: string): string {
    const health = options.serviceHealth.value[service]
    const progress = options.syncProgress.value[service]

    if (service === 'lotw') {
      if (!options.lotwHasCert.value) return 'No certificate'
      if (!health || health === 'not_configured') return 'Certificate ready'
    }

    if (progress?.has_permanent_error) return 'Auth error'
    if ((progress?.failed_count ?? 0) > 0 && !progress?.is_running && options.activeSyncServices.value.has(service)) {
      return 'Sync errors'
    }

    switch (health) {
      case 'ok':
      case 'connected':
        return 'Ready'
      case 'rate_limited':
      case 'rate-limited':
        return 'Rate limited'
      case 'circuit_open':
      case 'circuit-open':
        return 'Unavailable'
      case 'error':
        return 'Error'
      default:
        return ''
    }
  }

  function serviceHealthBadgeColor(service: string): string {
    const health = options.serviceHealth.value[service]
    const progress = options.syncProgress.value[service]

    if (service === 'lotw') {
      if (!options.lotwHasCert.value) return 'warning'
      if (!health || health === 'not_configured') return 'positive'
    }
    if (progress?.has_permanent_error) return 'negative'
    if ((progress?.failed_count ?? 0) > 0 && !progress?.is_running && options.activeSyncServices.value.has(service)) {
      return 'warning'
    }

    return healthColor(health)
  }

  return {
    progressCards,
    servicePendingCount,
    serviceConfigured,
    serviceConfigLabel,
    serviceConfigColor,
    serviceConfigTooltip,
    serviceHealthBadgeLabel,
    serviceHealthBadgeColor,
  }
}
