/**
 * Unit tests for rig-control module.
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
    <div id="rig-connection" class="value inactive">Disconnected</div>
    <div id="rig-backend">—</div>
    <div id="rig-frequency-display">---.---.---</div>
    <div id="rig-mode">—</div>
    <div id="rig-band">—</div>
    <div id="rig-power">—</div>
    <div id="rig-smeter-value">—</div>
    <div id="rig-smeter-fill" style="width:0%"></div>
    <div id="rig-error-row" style="display:none"></div>
    <div id="rig-error"></div>
    <div id="statusbar-rig-freq">—</div>
    <div id="statusbar-rig-mode"></div>
    <div id="statusbar-rig-band"></div>
  `
}

// ─── Import module under test ─────────────────────────────────────────────────

const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }

let refreshRig: typeof import('../../src/rig-control').refreshRig
let applyRigStatus: typeof import('../../src/rig-control').applyRigStatus
let promptQsy: typeof import('../../src/rig-control').promptQsy
let getCurrentRigStatus: typeof import('../../src/rig-control').getCurrentRigStatus
let updateStatusBarRig: typeof import('../../src/rig-control').updateStatusBarRig
let initRigControlEvents: typeof import('../../src/rig-control').initRigControlEvents
let setLogger: typeof import('../../src/rig-control').setLogger
let setAutofillQsoDraft: typeof import('../../src/rig-control').setAutofillQsoDraft
let setOnRigFrequencyChanged: typeof import('../../src/rig-control').setOnRigFrequencyChanged
let setOnRigModeChanged: typeof import('../../src/rig-control').setOnRigModeChanged
let resetRigState: typeof import('../../src/rig-control').resetRigState

beforeAll(async () => {
  const mod = await import('../../src/rig-control')
  refreshRig = mod.refreshRig
  applyRigStatus = mod.applyRigStatus
  promptQsy = mod.promptQsy
  getCurrentRigStatus = mod.getCurrentRigStatus
  updateStatusBarRig = mod.updateStatusBarRig
  initRigControlEvents = mod.initRigControlEvents
  setLogger = mod.setLogger
  setAutofillQsoDraft = mod.setAutofillQsoDraft
  setOnRigFrequencyChanged = mod.setOnRigFrequencyChanged
  setOnRigModeChanged = mod.setOnRigModeChanged
  resetRigState = mod.resetRigState
})

beforeEach(() => {
  setupDom()
  logs.length = 0
  mockInvoke.mockReset()
  mockListen.mockReset()
  resetRigState()
  setLogger(testLogger)
  setAutofillQsoDraft(() => {})
  setOnRigFrequencyChanged(() => {})
  setOnRigModeChanged(() => {})
})

// ─── Sample data ─────────────────────────────────────────────────────────────

const CONNECTED_RIG_STATUS = {
  connected: true,
  backend: 'hamlib',
  host: '127.0.0.1',
  port: 4532,
  frequency_hz: 14074000,
  frequency_display: '14.074.000',
  mode: 'FT8',
  band: '20m',
  bandwidth_hz: 3000,
  s_meter: 5.0,
  power: 50.0,
  vfo: 'VFO-A',
  strength: 42,
  last_error: null,
}

const DISCONNECTED_RIG_STATUS = {
  connected: false,
  backend: null,
  host: null,
  port: null,
  frequency_hz: null,
  frequency_display: null,
  mode: null,
  band: null,
  bandwidth_hz: null,
  s_meter: null,
  power: null,
  vfo: null,
  strength: null,
  last_error: null,
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('rig-control module', () => {
  it('exports all expected functions', () => {
    expect(typeof refreshRig).toBe('function')
    expect(typeof applyRigStatus).toBe('function')
    expect(typeof promptQsy).toBe('function')
    expect(typeof getCurrentRigStatus).toBe('function')
    expect(typeof updateStatusBarRig).toBe('function')
    expect(typeof initRigControlEvents).toBe('function')
    expect(typeof setLogger).toBe('function')
    expect(typeof setAutofillQsoDraft).toBe('function')
    expect(typeof setOnRigFrequencyChanged).toBe('function')
    expect(typeof setOnRigModeChanged).toBe('function')
    expect(typeof resetRigState).toBe('function')
  })

  // ── getCurrentRigStatus ──────────────────────────────────────────────────

  describe('getCurrentRigStatus', () => {
    it('returns null before any refresh', () => {
      expect(getCurrentRigStatus()).toBeNull()
    })

    it('returns the status after refreshRig succeeds', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()
      expect(getCurrentRigStatus()).toEqual(CONNECTED_RIG_STATUS)
    })
  })

  // ── refreshRig ──────────────────────────────────────────────────────────

  describe('refreshRig', () => {
    it('fetches rig status from backend and applies it', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()

      expect(mockInvoke).toHaveBeenCalledWith('get_rig_status')
      expect(document.getElementById('rig-connection')?.textContent).toBe('Connected')
      expect(document.getElementById('rig-mode')?.textContent).toBe('FT8')
      expect(document.getElementById('rig-band')?.textContent).toBe('20m')
    })

    it('logs error when backend fails', async () => {
      mockInvoke.mockRejectedValue(new Error('network error'))
      await refreshRig()
      expect(logs.some(l => l.includes('Rig status error'))).toBe(true)
    })
  })

  // ── applyRigStatus ─────────────────────────────────────────────────────

  describe('applyRigStatus', () => {
    it('displays connected status with green indicator', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      const el = document.getElementById('rig-connection')!
      expect(el.textContent).toBe('Connected')
      expect(el.className).toContain('active')
    })

    it('displays disconnected status', () => {
      applyRigStatus(DISCONNECTED_RIG_STATUS as any)
      const el = document.getElementById('rig-connection')!
      expect(el.textContent).toBe('Disconnected')
      expect(el.className).toContain('inactive')
    })

    it('shows backend info when connected', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-backend')?.textContent).toBe('hamlib@127.0.0.1:4532')
    })

    it('shows dash when backend is null', () => {
      applyRigStatus(DISCONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-backend')?.textContent).toBe('—')
    })

    it('displays frequency, mode, and band', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-frequency-display')?.textContent).toBe('14.074.000')
      expect(document.getElementById('rig-mode')?.textContent).toBe('FT8')
      expect(document.getElementById('rig-band')?.textContent).toBe('20m')
    })

    it('shows default frequency when null', () => {
      applyRigStatus(DISCONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-frequency-display')?.textContent).toBe('---.---.---')
    })

    it('displays power in watts', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-power')?.textContent).toBe('50.0 W')
    })

    it('shows dash for power when null', () => {
      applyRigStatus(DISCONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-power')?.textContent).toBe('—')
    })

    it('displays S-meter strength value', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-smeter-value')?.textContent).toBe('42.0')
    })

    it('shows dash for strength when null', () => {
      applyRigStatus(DISCONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-smeter-value')?.textContent).toBe('—')
    })

    it('fills S-meter bar width based on strength', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-smeter-fill')?.style.width).toBe('42%')
    })

    it('shows error row when last_error is present', () => {
      const statusWithError = { ...CONNECTED_RIG_STATUS, last_error: 'Timeout' }
      applyRigStatus(statusWithError as any)
      expect(document.getElementById('rig-error-row')?.style.display).not.toBe('none')
      expect(document.getElementById('rig-error')?.textContent).toBe('Timeout')
    })

    it('hides error row when last_error is null', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('rig-error-row')?.style.display).toBe('none')
    })

    it('calls autofillQsoDraft with the status', () => {
      const autofillCalls: any[] = []
      setAutofillQsoDraft((status) => autofillCalls.push(status))

      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(autofillCalls).toHaveLength(1)
      expect(autofillCalls[0]).toEqual(CONNECTED_RIG_STATUS)
    })

    it('updates the status bar', () => {
      applyRigStatus(CONNECTED_RIG_STATUS as any)
      expect(document.getElementById('statusbar-rig-freq')?.textContent).toBe('14.074.000')
      expect(document.getElementById('statusbar-rig-mode')?.textContent).toBe('[FT8]')
      expect(document.getElementById('statusbar-rig-band')?.textContent).toBe('20m')
    })
  })

  // ── updateStatusBarRig ─────────────────────────────────────────────────

  describe('updateStatusBarRig', () => {
    it('updates status bar elements', () => {
      updateStatusBarRig('7.074', 'CW', '40m')
      expect(document.getElementById('statusbar-rig-freq')?.textContent).toBe('7.074')
      expect(document.getElementById('statusbar-rig-mode')?.textContent).toBe('[CW]')
      expect(document.getElementById('statusbar-rig-band')?.textContent).toBe('40m')
    })

    it('shows dash for null frequency', () => {
      updateStatusBarRig(null, 'SSB', '20m')
      expect(document.getElementById('statusbar-rig-freq')?.textContent).toBe('—')
    })

    it('shows empty for null mode', () => {
      updateStatusBarRig('14.074', null, '20m')
      expect(document.getElementById('statusbar-rig-mode')?.textContent).toBe('')
    })

    it('shows empty string for null band', () => {
      updateStatusBarRig('14.074', 'FT8', null)
      expect(document.getElementById('statusbar-rig-band')?.textContent).toBe('')
    })
  })

  // ── promptQsy ──────────────────────────────────────────────────────────

  describe('promptQsy', () => {
    it('logs and returns when rig is not connected', async () => {
      // currentRigStatus is null initially
      await promptQsy()
      expect(logs.some(l => l.includes('not connected'))).toBe(true)
      expect(mockInvoke).not.toHaveBeenCalled()
    })

    it('logs and returns when rig is disconnected', async () => {
      mockInvoke.mockResolvedValue(DISCONNECTED_RIG_STATUS)
      await refreshRig()
      await promptQsy()
      expect(logs.some(l => l.includes('not connected'))).toBe(true)
    })

    it('aborts when user cancels prompt', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig() // set currentRigStatus to connected

      vi.spyOn(window, 'prompt').mockReturnValue(null)
      await promptQsy()
      expect(mockInvoke).not.toHaveBeenCalledWith('set_rig_frequency', expect.any(Object))
    })

    it('rejects invalid frequency values', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()

      vi.spyOn(window, 'prompt').mockReturnValue('0')
      await promptQsy()
      expect(logs.some(l => l.includes('Invalid frequency'))).toBe(true)
    })

    it('rejects negative frequency values', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()

      vi.spyOn(window, 'prompt').mockReturnValue('-100')
      await promptQsy()
      expect(logs.some(l => l.includes('Invalid frequency'))).toBe(true)
    })

    it('sets rig frequency and applies new status on valid input', async () => {
      const qsyResult = { ...CONNECTED_RIG_STATUS, frequency_hz: 7074000, frequency_display: '7.074.000', band: '40m' }
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()

      mockInvoke.mockResolvedValue(qsyResult)
      vi.spyOn(window, 'prompt').mockReturnValue('7074000')
      await promptQsy()

      expect(mockInvoke).toHaveBeenCalledWith('set_rig_frequency', { freq: 7074000 })
      expect(document.getElementById('rig-band')?.textContent).toBe('40m')
      expect(logs.some(l => l.includes('QSY to'))).toBe(true)
    })

    it('logs error when set_rig_frequency fails', async () => {
      mockInvoke.mockResolvedValue(CONNECTED_RIG_STATUS)
      await refreshRig()

      mockInvoke.mockRejectedValue(new Error('rig not responding'))
      vi.spyOn(window, 'prompt').mockReturnValue('14074000')
      await promptQsy()

      expect(logs.some(l => l.includes('QSY failed'))).toBe(true)
    })
  })

  // ── initRigControlEvents ───────────────────────────────────────────────

  describe('initRigControlEvents', () => {
    it('registers listeners for rig_frequency_changed, rig_mode_changed, and rig_status_changed', () => {
      mockListen.mockResolvedValue(() => {})
      initRigControlEvents()

      const eventNames = mockListen.mock.calls.map(c => c[0])
      expect(eventNames).toContain('rig_frequency_changed')
      expect(eventNames).toContain('rig_mode_changed')
      expect(eventNames).toContain('rig_status_changed')
    })

    it('calls onRigFrequencyChanged when rig_frequency_changed fires', async () => {
      const freqChangedCalls: any[] = []
      setOnRigFrequencyChanged((payload) => freqChangedCalls.push(payload))

      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'rig_frequency_changed') {
          handler({ payload: { frequency_hz: 7074000, frequency_display: '7.074', band: '40m' } })
        }
        return () => {}
      })

      initRigControlEvents()

      expect(freqChangedCalls).toHaveLength(1)
      expect(freqChangedCalls[0].band).toBe('40m')
    })

    it('calls onRigModeChanged when rig_mode_changed fires', async () => {
      const modeChangedCalls: any[] = []
      setOnRigModeChanged((payload) => modeChangedCalls.push(payload))

      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'rig_mode_changed') {
          handler({ payload: { mode: 'CW' } })
        }
        return () => {}
      })

      initRigControlEvents()

      expect(modeChangedCalls).toHaveLength(1)
      expect(modeChangedCalls[0].mode).toBe('CW')
    })

    it('updates currentRigStatus when rig_status_changed fires', async () => {
      mockListen.mockImplementation(async (_event: string, handler: (e: any) => void) => {
        if (_event === 'rig_status_changed') {
          handler({ payload: CONNECTED_RIG_STATUS })
        }
        return () => {}
      })

      initRigControlEvents()

      expect(getCurrentRigStatus()).toEqual(CONNECTED_RIG_STATUS)
    })
  })

  // ── Dependency injection ───────────────────────────────────────────────

  describe('setLogger', () => {
    it('uses injected logger for error reporting', async () => {
      const customLogs: string[] = []
      setLogger((msg: string) => customLogs.push(msg))

      mockInvoke.mockRejectedValue(new Error('boom'))
      await refreshRig()
      expect(customLogs.some(l => l.includes('Rig status error'))).toBe(true)
    })
  })

})