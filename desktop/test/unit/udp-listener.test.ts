/**
 * Unit tests for udp-listener module.
 *
 * Verifies the exported API surface and core logic without a browser or
 * Tauri runtime. DOM and Tauri invoke calls are mocked.
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// ─── Mock setup ───────────────────────────────────────────────────────────────
const mockInvoke = vi.fn()
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => mockInvoke(...args),
}))

// ─── Minimal jsdom setup ─────────────────────────────────────────────────────

function setupDom(): void {
  document.body.innerHTML = `
    <!-- Shack tab UDP cards -->
    <span id="udp-wsjtx-status" class="value inactive">Stopped</span>
    <span id="udp-wsjtx-port">2237</span>
    <span id="udp-wsjtx-packets">0</span>
    <button id="udp-wsjtx-toggle-btn">Start Listener</button>

    <span id="udp-js8call-status" class="value inactive">Stopped</span>
    <span id="udp-js8call-port">2242</span>
    <span id="udp-js8call-packets">0</span>
    <button id="udp-js8call-toggle-btn">Start Listener</button>

    <span id="udp-n1mm-status" class="value inactive">Stopped</span>
    <span id="udp-n1mm-port">12060</span>
    <span id="udp-n1mm-packets">0</span>
    <button id="udp-n1mm-toggle-btn">Start Listener</button>

    <!-- Status bar -->
    <div id="statusbar-udp-dot" class="status-dot"></div>
    <span id="statusbar-udp-text">Stopped :2237</span>

    <!-- Settings tab UDP fields -->
    <input id="settings-udp-wsjtx-port" value="2237" />
    <input id="settings-udp-js8call-port" value="2242" />
    <input id="settings-udp-n1mm-port" value="12060" />
    <input id="settings-udp-wsjtx-multicast" type="checkbox" />
    <input id="settings-udp-wsjtx-multicast-group" value="224.0.0.73" />
    <div id="settings-udp-wsjtx-multicast-group-row" style="display:none"></div>
    <input id="settings-udp-wsjtx-auto-start" type="checkbox" />
    <input id="settings-udp-js8call-auto-start" type="checkbox" />
    <input id="settings-udp-n1mm-auto-start" type="checkbox" />
    <input id="settings-udp-ft8battle-relay" type="checkbox" />
    <button id="settings-save-udp-btn">Save UDP Settings</button>
  `
}

// ─── Import module under test ─────────────────────────────────────────────────

let refreshUdp: typeof import('../../src/udp-listener').refreshUdp
let toggleWsjtx: typeof import('../../src/udp-listener').toggleWsjtx
let toggleJs8call: typeof import('../../src/udp-listener').toggleJs8call
let toggleN1mm: typeof import('../../src/udp-listener').toggleN1mm
let loadUdpSettingsValues: typeof import('../../src/udp-listener').loadUdpSettingsValues
let saveUdpSettings: typeof import('../../src/udp-listener').saveUdpSettings
let wireUdpListenerListeners: typeof import('../../src/udp-listener').wireUdpListenerListeners
let setLogger: typeof import('../../src/udp-listener').setLogger

const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }

beforeAll(async () => {
  const mod = await import('../../src/udp-listener')
  refreshUdp = mod.refreshUdp
  toggleWsjtx = mod.toggleWsjtx
  toggleJs8call = mod.toggleJs8call
  toggleN1mm = mod.toggleN1mm
  loadUdpSettingsValues = mod.loadUdpSettingsValues
  saveUdpSettings = mod.saveUdpSettings
  wireUdpListenerListeners = mod.wireUdpListenerListeners
  setLogger = mod.setLogger
})

beforeEach(() => {
  setupDom()
  logs.length = 0
  mockInvoke.mockReset()

  setLogger(testLogger)
})

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('udp-listener module', () => {
  it('exports all expected functions', () => {
    expect(typeof refreshUdp).toBe('function')
    expect(typeof toggleWsjtx).toBe('function')
    expect(typeof toggleJs8call).toBe('function')
    expect(typeof toggleN1mm).toBe('function')
    expect(typeof loadUdpSettingsValues).toBe('function')
    expect(typeof saveUdpSettings).toBe('function')
    expect(typeof wireUdpListenerListeners).toBe('function')
    expect(typeof setLogger).toBe('function')
  })

  // ── refreshUdp ────────────────────────────────────────────────────────────

  describe('refreshUdp', () => {
    const udpStatusPayload = {
      wsjtx: { listening: true, port: 2237, bind: '0.0.0.0', packets_received: 42, multicast_group: null },
      js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
      n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
    }

    it('renders WSJT-X listening state', async () => {
      mockInvoke.mockResolvedValue(udpStatusPayload)

      await refreshUdp()

      expect(document.getElementById('udp-wsjtx-status')!.textContent).toBe('Listening')
      expect(document.getElementById('udp-wsjtx-status')!.className).toBe('value active')
      expect(document.getElementById('udp-wsjtx-port')!.textContent).toBe('2237')
      expect(document.getElementById('udp-wsjtx-packets')!.textContent).toBe('42')
      expect(document.getElementById('udp-wsjtx-toggle-btn')!.textContent).toBe('Stop Listener')
    })

    it('renders JS8Call stopped state', async () => {
      mockInvoke.mockResolvedValue(udpStatusPayload)

      await refreshUdp()

      expect(document.getElementById('udp-js8call-status')!.textContent).toBe('Stopped')
      expect(document.getElementById('udp-js8call-status')!.className).toBe('value inactive')
      expect(document.getElementById('udp-js8call-toggle-btn')!.textContent).toBe('Start Listener')
    })

    it('shows multicast group suffix when present', async () => {
      const withMcast = {
        ...udpStatusPayload,
        wsjtx: { ...udpStatusPayload.wsjtx, multicast_group: '224.0.0.73' },
      }
      mockInvoke.mockResolvedValue(withMcast)

      await refreshUdp()

      expect(document.getElementById('udp-wsjtx-status')!.textContent).toBe('Listening (multicast: 224.0.0.73)')
    })

    it('updates status bar with primary listening port', async () => {
      mockInvoke.mockResolvedValue(udpStatusPayload)

      await refreshUdp()

      expect(document.getElementById('statusbar-udp-dot')!.className).toContain('ok')
      expect(document.getElementById('statusbar-udp-text')!.textContent).toBe('Listening :2237')
    })

    it('updates status bar as stopped when nothing listening', async () => {
      const allStopped = {
        wsjtx: { listening: false, port: 2237, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
      }
      mockInvoke.mockResolvedValue(allStopped)

      await refreshUdp()

      expect(document.getElementById('statusbar-udp-dot')!.className).not.toContain('ok')
      expect(document.getElementById('statusbar-udp-dot')!.className).not.toContain('err')
      expect(document.getElementById('statusbar-udp-text')!.textContent).toBe('Stopped :12060')
    })

    it('handles errors gracefully', async () => {
      mockInvoke.mockRejectedValue(new Error('network error'))

      await refreshUdp()

      expect(logs.some(l => l.includes('UDP status error'))).toBe(true)
      expect(document.getElementById('statusbar-udp-dot')!.className).not.toContain('ok')
      expect(document.getElementById('statusbar-udp-dot')!.className).not.toContain('err')
    })
  })

  // ── toggleWsjtx ──────────────────────────────────────────────────────────

  describe('toggleWsjtx', () => {
    it('starts WSJT-X listener when stopped', async () => {
      // First call: refreshUdp returns "not listening" state
      mockInvoke
        .mockResolvedValueOnce({
          wsjtx: { listening: false, port: 2237, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        })

      await toggleWsjtx()

      expect(mockInvoke).toHaveBeenCalledWith('start_udp_listener', { port: null })
      expect(logs.some(l => l.includes('WSJT-X UDP listener started'))).toBe(true)
    })

    it('stops WSJT-X listener when running', async () => {
      // First set state to listening via refreshUdp
      mockInvoke
        .mockResolvedValueOnce({
          wsjtx: { listening: true, port: 2237, bind: '0.0.0.0', packets_received: 5, multicast_group: null },
          js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        })
      await refreshUdp()

      mockInvoke.mockReset()
      mockInvoke
        .mockResolvedValueOnce(undefined) // stop_udp_listener
        .mockResolvedValueOnce({
          wsjtx: { listening: false, port: 2237, bind: '0.0.0.0', packets_received: 5, multicast_group: null },
          js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        })

      await toggleWsjtx()

      expect(mockInvoke).toHaveBeenCalledWith('stop_udp_listener')
      expect(logs.some(l => l.includes('WSJT-X UDP listener stopped'))).toBe(true)
    })

    it('logs toggle errors', async () => {
      mockInvoke.mockRejectedValue(new Error('start failed'))

      await toggleWsjtx()

      expect(logs.some(l => l.includes('WSJT-X UDP toggle error'))).toBe(true)
    })
  })

  // ── toggleJs8call ────────────────────────────────────────────────────────

  describe('toggleJs8call', () => {
    it('starts JS8Call listener when stopped', async () => {
      mockInvoke
        .mockResolvedValueOnce({
          wsjtx: { listening: false, port: 2237, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        })

      await toggleJs8call()

      expect(mockInvoke).toHaveBeenCalledWith('start_js8call_listener', { port: null })
      expect(logs.some(l => l.includes('JS8Call UDP listener started'))).toBe(true)
    })

    it('logs toggle errors', async () => {
      mockInvoke.mockRejectedValue(new Error('js8call fail'))

      await toggleJs8call()

      expect(logs.some(l => l.includes('JS8Call UDP toggle error'))).toBe(true)
    })
  })

  // ── toggleN1mm ───────────────────────────────────────────────────────────

  describe('toggleN1mm', () => {
    it('starts N1MM listener when stopped', async () => {
      mockInvoke
        .mockResolvedValueOnce({
          wsjtx: { listening: false, port: 2237, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          js8call: { listening: false, port: 2242, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
          n1mm: { listening: false, port: 12060, bind: '0.0.0.0', packets_received: 0, multicast_group: null },
        })

      await toggleN1mm()

      expect(mockInvoke).toHaveBeenCalledWith('start_n1mm_listener', { port: null })
      expect(logs.some(l => l.includes('N1MM+ UDP listener started'))).toBe(true)
    })

    it('logs toggle errors', async () => {
      mockInvoke.mockRejectedValue(new Error('n1mm fail'))

      await toggleN1mm()

      expect(logs.some(l => l.includes('N1MM+ UDP toggle error'))).toBe(true)
    })
  })

  // ── loadUdpSettingsValues ────────────────────────────────────────────────

  describe('loadUdpSettingsValues', () => {
    it('loads UDP config into settings fields', async () => {
      mockInvoke.mockResolvedValue({
        wsjtx_port: 2238,
        wsjtx_auto_start: true,
        wsjtx_multicast_group: null,
        js8call_port: 2243,
        js8call_auto_start: false,
        n1mm_port: 12061,
        n1mm_auto_start: true,
        ft8battle_relay_enabled: true,
      })

      await loadUdpSettingsValues()

      expect((document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value).toBe('2238')
      expect((document.getElementById('settings-udp-js8call-port') as HTMLInputElement).value).toBe('2243')
      expect((document.getElementById('settings-udp-n1mm-port') as HTMLInputElement).value).toBe('12061')
      expect((document.getElementById('settings-udp-wsjtx-auto-start') as HTMLInputElement).checked).toBe(true)
      expect((document.getElementById('settings-udp-n1mm-auto-start') as HTMLInputElement).checked).toBe(true)
      expect((document.getElementById('settings-udp-ft8battle-relay') as HTMLInputElement).checked).toBe(true)
    })

    it('handles multicast group config', async () => {
      mockInvoke.mockResolvedValue({
        wsjtx_port: 2237,
        wsjtx_auto_start: false,
        wsjtx_multicast_group: '224.0.0.1',
        js8call_port: 2242,
        js8call_auto_start: false,
        n1mm_port: 12060,
        n1mm_auto_start: false,
        ft8battle_relay_enabled: false,
      })

      await loadUdpSettingsValues()

      expect((document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement).checked).toBe(true)
      expect((document.getElementById('settings-udp-wsjtx-multicast-group') as HTMLInputElement).value).toBe('224.0.0.1')
      expect(document.getElementById('settings-udp-wsjtx-multicast-group-row')!.style.display).toBeFalsy()
    })

    it('gracefully handles missing commands', async () => {
      mockInvoke.mockRejectedValue(new Error('not available'))

      // Should not throw
      await expect(loadUdpSettingsValues()).resolves.toBeUndefined()
    })
  })

  // ── saveUdpSettings ──────────────────────────────────────────────────────

  describe('saveUdpSettings', () => {
    it('rejects invalid port numbers', async () => {
      (document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value = '80'

      await saveUdpSettings()

      expect(logs.some(l => l.includes('1024 and 65535'))).toBe(true)
    })

    it('saves valid UDP settings', async () => {
      ;(document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value = '2237'
      ;(document.getElementById('settings-udp-js8call-port') as HTMLInputElement).value = '2242'
      ;(document.getElementById('settings-udp-n1mm-port') as HTMLInputElement).value = '12060'
      mockInvoke.mockResolvedValue(undefined)

      await saveUdpSettings()

      expect(mockInvoke).toHaveBeenCalledWith('save_udp_settings', {
        request: {
          wsjtx_port: 2237,
          js8call_port: 2242,
          n1mm_port: 12060,
          wsjtx_multicast_group: null,
          wsjtx_auto_start: false,
          js8call_auto_start: false,
          n1mm_auto_start: false,
          ft8battle_relay_enabled: false,
        },
      })
      expect(logs.some(l => l.includes('UDP settings saved'))).toBe(true)
    })

    it('sends multicast group when enabled', async () => {
      ;(document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value = '2237'
      ;(document.getElementById('settings-udp-js8call-port') as HTMLInputElement).value = '2242'
      ;(document.getElementById('settings-udp-n1mm-port') as HTMLInputElement).value = '12060'
      ;(document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement).checked = true
      ;(document.getElementById('settings-udp-wsjtx-multicast-group') as HTMLInputElement).value = '224.0.0.73'
      mockInvoke.mockResolvedValue(undefined)

      await saveUdpSettings()

      expect(mockInvoke).toHaveBeenCalledWith('save_udp_settings', {
        request: expect.objectContaining({
          wsjtx_multicast_group: '224.0.0.73',
        }),
      })
    })

    it('rejects invalid multicast group range', async () => {
      ;(document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value = '2237'
      ;(document.getElementById('settings-udp-js8call-port') as HTMLInputElement).value = '2242'
      ;(document.getElementById('settings-udp-n1mm-port') as HTMLInputElement).value = '12060'
      ;(document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement).checked = true
      ;(document.getElementById('settings-udp-wsjtx-multicast-group') as HTMLInputElement).value = '192.168.1.1'
      mockInvoke.mockResolvedValue(undefined)

      await saveUdpSettings()

      expect(logs.some(l => l.includes('224.0.0.0–239.255.255.255'))).toBe(true)
      expect(mockInvoke).not.toHaveBeenCalledWith('save_udp_settings', expect.anything())
    })

    it('reports save failure', async () => {
      ;(document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement).value = '2237'
      ;(document.getElementById('settings-udp-js8call-port') as HTMLInputElement).value = '2242'
      ;(document.getElementById('settings-udp-n1mm-port') as HTMLInputElement).value = '12060'
      mockInvoke.mockRejectedValue(new Error('save failed'))

      await saveUdpSettings()

      expect(logs.some(l => l.includes('Failed to save UDP settings'))).toBe(true)
    })
  })

  // ── wireUdpListenerListeners ────────────────────────────────────────────

  describe('wireUdpListenerListeners', () => {
    it('wires click handlers without error', () => {
      expect(() => wireUdpListenerListeners()).not.toThrow()
    })
  })
})