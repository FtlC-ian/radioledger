/**
 * Unit tests for stats-dashboard module.
 *
 * Verifies the exported API surface and core logic without a browser or Tauri runtime.
 * DOM and Tauri invoke calls are mocked.
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// ─── Mock setup ───────────────────────────────────────────────────────────────
// We must mock @tauri-apps/api/core before importing the module under test.
const mockInvoke = vi.fn()
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => mockInvoke(...args),
}))

// Mock Chart globally (loaded from CDN at runtime)
;(globalThis as any).Chart = vi.fn()

// ─── Import module under test ─────────────────────────────────────────────────
const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }

// Dynamic import deferred to beforeAll so mocks are in place
// (top-level await is incompatible with CommonJS module setting — TS1378)
let refreshStats: typeof import('../../src/stats-dashboard').refreshStats
let resetStatsCache: typeof import('../../src/stats-dashboard').resetStatsCache
let isStatsLoaded: typeof import('../../src/stats-dashboard').isStatsLoaded
let setLogger: typeof import('../../src/stats-dashboard').setLogger

beforeAll(async () => {
  const mod = await import('../../src/stats-dashboard')
  refreshStats = mod.refreshStats
  resetStatsCache = mod.resetStatsCache
  isStatsLoaded = mod.isStatsLoaded
  setLogger = mod.setLogger
})

// ─── Helpers ──────────────────────────────────────────────────────────────────

/** Minimal jsdom setup for stats functions that touch document.getElementById */
function setupDom(): void {
  document.body.innerHTML = `
    <span id="stat-total-qsos"></span>
    <span id="stat-unique-callsigns"></span>
    <span id="stat-unique-countries"></span>
    <span id="stat-unique-states"></span>
    <span id="stat-unique-grids"></span>
    <span id="stat-bands-used"></span>
    <span id="stat-modes-used"></span>
    <span id="stat-first-qso"></span>
    <span id="stat-last-qso"></span>
    <canvas id="chart-band"></canvas>
    <canvas id="chart-mode"></canvas>
    <canvas id="chart-period"></canvas>
    <canvas id="chart-cot"></canvas>
    <button id="stats-refresh-btn">↻ Refresh</button>
  `
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('stats-dashboard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    logs.length = 0
    setLogger(testLogger)
    resetStatsCache()
    setupDom()
  })

  describe('resetStatsCache / isStatsLoaded', () => {
    it('starts with stats not loaded', () => {
      expect(isStatsLoaded()).toBe(false)
    })

    it('resetStatsCache sets loaded to false', async () => {
      // Simulate a successful refresh to set statsLoaded = true
      mockInvoke.mockImplementation((cmd: string) => {
        if (cmd === 'get_auth_status') return { logged_in: true, callsign: 'W1AW' }
        if (cmd === 'api_get') {
          return JSON.stringify({ success: true, message: 'ok', data: [] })
        }
        return null
      })

      await refreshStats(true)
      expect(isStatsLoaded()).toBe(true)

      resetStatsCache()
      expect(isStatsLoaded()).toBe(false)
    })
  })

  describe('setLogger', () => {
    it('uses the injected logger', async () => {
      const otherLogs: string[] = []
      setLogger((msg: string) => { otherLogs.push(msg) })

      mockInvoke.mockImplementation((cmd: string) => {
        if (cmd === 'get_auth_status') return { logged_in: false, callsign: null }
        return null
      })

      await refreshStats(true)
      expect(otherLogs).toContain('Stats: not logged in — skipping')

      // Restore
      setLogger(testLogger)
    })
  })

  describe('refreshStats', () => {
    it('returns false when not logged in', async () => {
      mockInvoke.mockImplementation((cmd: string) => {
        if (cmd === 'get_auth_status') return { logged_in: false, callsign: null }
        return null
      })

      const result = await refreshStats(true)
      expect(result).toBe(false)
      expect(isStatsLoaded()).toBe(false)
      expect(logs).toContain('Stats: not logged in — skipping')
    })

    it('calls api endpoints and sets statsLoaded on success', async () => {
      const invokedCommands: string[] = []
      mockInvoke.mockImplementation((cmd: string, args?: any) => {
        invokedCommands.push(cmd)
        if (cmd === 'get_auth_status') return { logged_in: true, callsign: 'W1AW' }
        if (cmd === 'api_get') {
          // Return minimal valid data for each endpoint
          const path = args?.path ?? ''
          if (path.includes('overview')) {
            return JSON.stringify({
              success: true, message: 'ok',
              data: {
                total_qsos: 42, unique_callsigns: 10, unique_countries: 5,
                unique_states: 3, unique_grids: 7, bands_used: 4,
                modes_used: 2, first_qso: '2024-01-01', last_qso: '2024-12-31',
              },
            })
          }
          // All other endpoints return empty success arrays
          return JSON.stringify({ success: true, message: 'ok', data: [] })
        }
        return null
      })

      const result = await refreshStats(true)
      expect(result).toBe(true)
      expect(isStatsLoaded()).toBe(true)
      expect(logs).toContain('Refreshing statistics dashboard…')
      expect(logs).toContain('Statistics refreshed')

      // Verify overview data was rendered
      const totalQsos = document.getElementById('stat-total-qsos')
      expect(totalQsos?.textContent).toBe('42')
    })

    it('disables and re-enables refresh button during load', async () => {
      const btn = document.getElementById('stats-refresh-btn') as HTMLButtonElement

      // Make invoke async so we can check intermediate state
      let resolveInvoke: (v: unknown) => void
      mockInvoke.mockImplementation((cmd: string) => {
        if (cmd === 'get_auth_status') {
          return new Promise((r) => { resolveInvoke = r })
        }
        return null
      })

      const promise = refreshStats(true)

      // Button should be disabled while loading
      expect(btn.disabled).toBe(true)
      expect(btn.textContent).toBe('Refreshing…')

      // Resolve the auth check (not logged in)
      resolveInvoke!({ logged_in: false, callsign: null })
      await promise

      // Button should be re-enabled
      expect(btn.disabled).toBe(false)
      expect(btn.textContent).toBe('↻ Refresh')
    })

    it('skips refresh when not forced and already loaded', async () => {
      // First, set up a successful load
      mockInvoke.mockImplementation((cmd: string) => {
        if (cmd === 'get_auth_status') return { logged_in: true, callsign: 'W1AW' }
        if (cmd === 'api_get') {
          return JSON.stringify({ success: true, message: 'ok', data: [] })
        }
        return null
      })

      await refreshStats(true)
      expect(isStatsLoaded()).toBe(true)

      // Now call refreshStats(false) — should skip without hitting the API endpoints again
      // Note: get_auth_status is still called (auth check happens before cache check)
      // but no api_get calls should be made
      const apiGetCallsBefore = mockInvoke.mock.calls.filter((c: string[]) => c[0] === 'api_get').length
      const result = await refreshStats(false)
      expect(result).toBe(true)
      const apiGetCallsAfter = mockInvoke.mock.calls.filter((c: string[]) => c[0] === 'api_get').length
      expect(apiGetCallsAfter).toBe(apiGetCallsBefore)
    })
  })
})