import { ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useSyncOverviewDisplay } from './useSyncOverviewDisplay'
import type { SyncProgress } from 'src/types/sync'

function createState(overrides: {
  serviceHealth?: Record<string, string>
  syncProgress?: Record<string, SyncProgress>
  activeSyncServices?: Set<string>
  lotwHasCert?: boolean
  lotwRawPendingCount?: number
} = {}) {
  return useSyncOverviewDisplay({
    serviceHealth: ref(overrides.serviceHealth ?? {}),
    syncProgress: ref(overrides.syncProgress ?? {}),
    activeSyncServices: ref(overrides.activeSyncServices ?? new Set<string>()),
    lotwHasCert: ref(overrides.lotwHasCert ?? true),
    lotwRawPendingCount: ref(overrides.lotwRawPendingCount ?? 0),
  })
}

describe('useSyncOverviewDisplay', () => {
  it('uses the raw LoTW pending count when rows have not been backfilled yet', () => {
    const state = createState({
      syncProgress: {
        lotw: {
          pending_count: 0,
          uploaded_count: 0,
          failed_count: 0,
          is_running: false,
          is_stalled: false,
        },
      },
      lotwRawPendingCount: 7,
    })

    expect(state.servicePendingCount('lotw')).toBe(7)
  })

  it('shows setup guidance for unconfigured non-LoTW services', () => {
    const state = createState({
      serviceHealth: { eqsl: 'not_configured' },
    })

    expect(state.serviceConfigured('eqsl')).toBe(false)
    expect(state.serviceConfigLabel('eqsl')).toBe('Set up')
    expect(state.serviceConfigColor('eqsl')).toBe('warning')
    expect(state.serviceConfigTooltip('eqsl')).toBe('eQSL is not set up yet. Open Settings → Sync Services to connect it.')
  })

  it('shows review/update guidance for configured non-LoTW services', () => {
    const state = createState({
      serviceHealth: { qrz: 'connected' },
    })

    expect(state.serviceConfigured('qrz')).toBe(true)
    expect(state.serviceConfigLabel('qrz')).toBe('Configure')
    expect(state.serviceConfigColor('qrz')).toBe('positive')
    expect(state.serviceConfigTooltip('qrz')).toBe('Open Settings → Sync Services to review or update QRZ.')
  })

  it('shows a certificate-ready badge for LoTW when a cert exists before service health is backfilled', () => {
    const state = createState({
      lotwHasCert: true,
      serviceHealth: {},
    })

    expect(state.serviceHealthBadgeLabel('lotw')).toBe('Certificate ready')
    expect(state.serviceHealthBadgeColor('lotw')).toBe('positive')
  })

  it('shows missing-certificate guidance for LoTW when not configured', () => {
    const state = createState({
      lotwHasCert: false,
      serviceHealth: { lotw: 'not_configured' },
    })

    expect(state.serviceConfigured('lotw')).toBe(false)
    expect(state.serviceConfigLabel('lotw')).toBe('Set up')
    expect(state.serviceConfigTooltip('lotw')).toBe(
      'LoTW is not set up yet. Open Settings → Sync Services to upload a certificate.',
    )
    expect(state.serviceHealthBadgeLabel('lotw')).toBe('No certificate')
    expect(state.serviceHealthBadgeColor('lotw')).toBe('warning')
  })

  it('lets permanent auth errors override the normal health badge state', () => {
    const state = createState({
      serviceHealth: { qrz: 'connected' },
      syncProgress: {
        qrz: {
          pending_count: 0,
          uploaded_count: 0,
          failed_count: 1,
          has_permanent_error: true,
          is_running: false,
          is_stalled: false,
        },
      },
      activeSyncServices: new Set(['qrz']),
    })

    expect(state.serviceHealthBadgeLabel('qrz')).toBe('Auth error')
    expect(state.serviceHealthBadgeColor('qrz')).toBe('negative')
  })

  it('surfaces sync errors only for services triggered in this session', () => {
    const progress: SyncProgress = {
      pending_count: 0,
      uploaded_count: 1,
      failed_count: 2,
      is_running: false,
      is_stalled: false,
      error_message: 'Auth failed',
    }

    const activeState = createState({
      syncProgress: { eqsl: progress },
      activeSyncServices: new Set(['eqsl']),
    })
    expect(activeState.serviceHealthBadgeLabel('eqsl')).toBe('Sync errors')
    expect(activeState.serviceHealthBadgeColor('eqsl')).toBe('warning')

    const inactiveState = createState({
      syncProgress: { eqsl: progress },
      activeSyncServices: new Set<string>(),
    })
    expect(inactiveState.serviceHealthBadgeLabel('eqsl')).toBe('')
    expect(inactiveState.serviceHealthBadgeColor('eqsl')).toBe('grey-6')
  })

  it('includes running, stalled, and failed services in progress cards only when they should be shown', () => {
    const state = createState({
      syncProgress: {
        eqsl: {
          pending_count: 4,
          uploaded_count: 1,
          failed_count: 0,
          total_count: 5,
          is_running: true,
          is_stalled: false,
        },
        qrz: {
          pending_count: 3,
          uploaded_count: 0,
          failed_count: 0,
          is_running: false,
          is_stalled: true,
        },
        clublog: {
          pending_count: 0,
          uploaded_count: 0,
          failed_count: 2,
          is_running: false,
          is_stalled: false,
          error_message: 'Upload failed',
        },
        lotw: {
          pending_count: 0,
          uploaded_count: 2,
          failed_count: 0,
          is_running: false,
          is_stalled: false,
        },
      },
      activeSyncServices: new Set<string>(),
    })

    expect(state.progressCards.value.map((card) => card.service)).toEqual(['eqsl', 'qrz', 'clublog'])
  })

  it('builds progress cards for completed active syncs and computes percent from fallback totals', () => {
    const state = createState({
      syncProgress: {
        eqsl: {
          pending_count: 0,
          uploaded_count: 3,
          failed_count: 1,
          is_running: false,
          is_stalled: false,
          last_error: 'One failed',
        },
      },
      activeSyncServices: new Set(['eqsl']),
    })

    expect(state.progressCards.value).toEqual([
      {
        service: 'eqsl',
        pending_count: 0,
        uploaded_count: 3,
        failed_count: 1,
        total_count: 4,
        percent: 75,
        error_message: 'One failed',
        is_running: false,
        is_stalled: false,
      },
    ])
  })
})
