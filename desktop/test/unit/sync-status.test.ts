/**
 * Unit tests for sync-status module.
 *
 * Verifies the exported API surface and core logic without a browser or
 * Tauri runtime. DOM and Tauri invoke/listen calls are mocked.
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// ─── Mock setup ───────────────────────────────────────────────────────────────
const mockInvoke = vi.fn()
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => mockInvoke(...args),
}))

const mockListen = vi.fn()
vi.mock('@tauri-apps/api/event', () => ({
  listen: (...args: unknown[]) => mockListen(...args),
}))

// ─── Minimal jsdom setup ─────────────────────────────────────────────────────

function setupDom(): void {
  document.body.innerHTML = `
    <div id="sync-pending">0</div>
    <div id="sync-last">Never</div>
    <div id="sync-error-row" style="display:none"></div>
    <div id="sync-error"></div>
    <div id="statusbar-pending">0</div>
    <div id="statusbar-last-sync">Never</div>
  `
}

// ─── Import module under test ─────────────────────────────────────────────────

const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }

let refreshSync: typeof import('../../src/sync-status').refreshSync
let syncNow: typeof import('../../src/sync-status').syncNow
let applySyncStatus: typeof import('../../src/sync-status').applySyncStatus
let initSyncStatusEvents: typeof import('../../src/sync-status').initSyncStatusEvents
let setLogger: typeof import('../../src/sync-status').setLogger
let setRefreshWsjtxDecodes: typeof import('../../src/sync-status').setRefreshWsjtxDecodes

beforeAll(async () => {
  const mod = await import('../../src/sync-status')
  refreshSync = mod.refreshSync
  syncNow = mod.syncNow
  applySyncStatus = mod.applySyncStatus
  initSyncStatusEvents = mod.initSyncStatusEvents
  setLogger = mod.setLogger
  setRefreshWsjtxDecodes = mod.setRefreshWsjtxDecodes
})

beforeEach(() => {
  setupDom()
  logs.length = 0
  mockInvoke.mockReset()
  mockListen.mockReset()
  setLogger(testLogger)
  setRefreshWsjtxDecodes(async () => {})
})

// ─── Sample data ─────────────────────────────────────────────────────────────

const SYNC_OK: import('../../src/sync-status').SyncStatus = {
  pending: 3,
  last_sync: '2026-04-17T12:00:00Z',
  last_error: null,
}

const SYNC_WITH_ERROR: import('../../src/sync-status').SyncStatus = {
  pending: 5,
  last_sync: '2026-04-17T11:30:00Z',
  last_error: 'Server unreachable',
}

const SYNC_NEVER: import('../../src/sync-status').SyncStatus = {
  pending: 0,
  last_sync: null,
  last_error: null,
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('sync-status module', () => {
  it('exports all expected functions', () => {
    expect(typeof refreshSync).toBe('function')
    expect(typeof syncNow).toBe('function')
    expect(typeof applySyncStatus).toBe('function')
    expect(typeof initSyncStatusEvents).toBe('function')
    expect(typeof setLogger).toBe('function')
    expect(typeof setRefreshWsjtxDecodes).toBe('function')
  })

  // ── applySyncStatus ─────────────────────────────────────────────────────

  describe('applySyncStatus', () => {
    it('displays pending count', () => {
      applySyncStatus(SYNC_OK)
      expect(document.getElementById('sync-pending')?.textContent).toBe('3')
    })

    it('displays formatted last_sync date', () => {
      applySyncStatus(SYNC_OK)
      const text = document.getElementById('sync-last')?.textContent ?? ''
      // Should be a locale-formatted date, not "Never"
      expect(text).not.toBe('Never')
    })

    it('displays "Never" when last_sync is null', () => {
      applySyncStatus(SYNC_NEVER)
      expect(document.getElementById('sync-last')?.textContent).toBe('Never')
    })

    it('hides error row when last_error is null', () => {
      applySyncStatus(SYNC_OK)
      expect(document.getElementById('sync-error-row')?.style.display).toBe('none')
    })

    it('shows error row with message when last_error is present', () => {
      applySyncStatus(SYNC_WITH_ERROR)
      expect(document.getElementById('sync-error-row')?.style.display).not.toBe('none')
      expect(document.getElementById('sync-error')?.textContent).toBe('Server unreachable')
    })

    it('updates status bar pending count', () => {
      applySyncStatus(SYNC_OK)
      expect(document.getElementById('statusbar-pending')?.textContent).toBe('3')
    })

    it('updates status bar last sync text', () => {
      applySyncStatus(SYNC_OK)
      const text = document.getElementById('statusbar-last-sync')?.textContent ?? ''
      expect(text).not.toBe('Never')
    })

    it('updates status bar with "Never" when last_sync is null', () => {
      applySyncStatus(SYNC_NEVER)
      expect(document.getElementById('statusbar-last-sync')?.textContent).toBe('Never')
    })
  })

  // ── refreshSync ──────────────────────────────────────────────────────────

  describe('refreshSync', () => {
    it('fetches sync status from backend and applies it', async () => {
      mockInvoke.mockResolvedValue(SYNC_OK)
      await refreshSync()

      expect(mockInvoke).toHaveBeenCalledWith('get_sync_status')
      expect(document.getElementById('sync-pending')?.textContent).toBe('3')
      expect(document.getElementById('statusbar-pending')?.textContent).toBe('3')
    })

    it('logs error when backend fails', async () => {
      mockInvoke.mockRejectedValue(new Error('network error'))
      await refreshSync()
      expect(logs.some(l => l.includes('Sync status error'))).toBe(true)
    })
  })

  // ── syncNow ──────────────────────────────────────────────────────────────

  describe('syncNow', () => {
    it('triggers sync and then refreshes', async () => {
      mockInvoke.mockResolvedValue(SYNC_OK)
      await syncNow()

      expect(mockInvoke).toHaveBeenCalledWith('sync_now')
      // After syncNow, refreshSync is also called (which calls get_sync_status)
      expect(mockInvoke).toHaveBeenCalledWith('get_sync_status')
      expect(logs.some(l => l.includes('Manual sync triggered'))).toBe(true)
      expect(logs.some(l => l.includes('Sync complete'))).toBe(true)
    })

    it('logs error when sync fails', async () => {
      mockInvoke.mockRejectedValue(new Error('timeout'))
      await syncNow()
      expect(logs.some(l => l.includes('Sync error'))).toBe(true)
    })
  })

  // ── initSyncStatusEvents ──────────────────────────────────────────────────

  describe('initSyncStatusEvents', () => {
    it('registers a listener for sync-status-changed', () => {
      mockListen.mockResolvedValue(() => {})
      initSyncStatusEvents()

      const eventNames = mockListen.mock.calls.map(c => c[0])
      expect(eventNames).toContain('sync-status-changed')
    })

    it('applies sync status and refreshes wsjtx decodes on event', async () => {
      const wsjtxCalls: string[] = []
      setRefreshWsjtxDecodes(async () => { wsjtxCalls.push('called') })

      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'sync-status-changed') {
          handler({ payload: SYNC_OK })
        }
        return () => {}
      })

      initSyncStatusEvents()

      expect(document.getElementById('sync-pending')?.textContent).toBe('3')
      expect(wsjtxCalls.length).toBe(1)
    })

    it('shows error row on sync event with last_error', async () => {
      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'sync-status-changed') {
          handler({ payload: SYNC_WITH_ERROR })
        }
        return () => {}
      })

      initSyncStatusEvents()

      expect(document.getElementById('sync-error-row')?.style.display).not.toBe('none')
      expect(document.getElementById('sync-error')?.textContent).toBe('Server unreachable')
    })
  })

  // ── Dependency injection ─────────────────────────────────────────────────

  describe('setLogger', () => {
    it('uses injected logger for error reporting', async () => {
      const customLogs: string[] = []
      setLogger((msg: string) => customLogs.push(msg))

      mockInvoke.mockRejectedValue(new Error('boom'))
      await refreshSync()
      expect(customLogs.some(l => l.includes('Sync status error'))).toBe(true)
    })
  })

  describe('setRefreshWsjtxDecodes', () => {
    it('invokes the injected callback on sync event', async () => {
      const calls: string[] = []
      setRefreshWsjtxDecodes(async () => { calls.push('wsjtx-refresh') })

      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'sync-status-changed') {
          handler({ payload: SYNC_OK })
        }
        return () => {}
      })

      initSyncStatusEvents()
      expect(calls).toEqual(['wsjtx-refresh'])
    })
  })
})