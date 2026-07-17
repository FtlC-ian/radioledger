/**
 * UDP Listener — WSJT-X / JS8Call / N1MM+ listener status, toggle flows,
 * persisted UDP settings loading/saving, and source-status rendering.
 *
 * Extracted from main.ts as part of the desktop decomposition (issue #194).
 * Manages the UDP listener booleans, the Shack tab source-status cards,
 * and the Settings tab UDP configuration form.
 *
 * The host shell injects callbacks for logging and other shared UI hooks.
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError as _formatError, setDotClass as _setDotClass } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface SourceStatus {
  listening: boolean
  port: number
  bind: string
  packets_received: number
  multicast_group: string | null
}

export interface UdpStatus {
  wsjtx: SourceStatus
  js8call: SourceStatus
  n1mm: SourceStatus
}

// ─── Module state ─────────────────────────────────────────────────────────────

let wsjtxListening = false
let js8callListening = false
let n1mmListening = false

// ─── Injected callbacks ──────────────────────────────────────────────────────

let _log: (msg: string) => void = () => {}

/** Inject the shared activity-log helper from the shell bootstrap. */
export function setLogger(fn: (msg: string) => void): void {
  _log = fn
}

// ─── Status bar updater ───────────────────────────────────────────────────────

function updateStatusBarUdp(listening: boolean, port: number): void {
  _setDotClass('statusbar-udp-dot', listening ? 'ok' : '')
  const textEl = document.getElementById('statusbar-udp-text')
  if (textEl) textEl.textContent = listening ? `Listening :${port}` : `Stopped :${port}`
}

// ─── Source-status rendering ──────────────────────────────────────────────────

function applySourceStatus(
  source: SourceStatus,
  statusId: string,
  portId: string,
  packetsId: string,
  toggleBtnId: string,
  _toggleFn: string,
): void {
  const statusEl = document.getElementById(statusId)
  const portEl = document.getElementById(portId)
  const packetsEl = document.getElementById(packetsId)
  const toggleBtn = document.getElementById(toggleBtnId)

  if (statusEl) {
    const mcSuffix = source.multicast_group ? ` (multicast: ${source.multicast_group})` : ''
    statusEl.textContent = source.listening ? `Listening${mcSuffix}` : 'Stopped'
    statusEl.className = `value ${source.listening ? 'active' : 'inactive'}`
  }
  if (portEl) portEl.textContent = String(source.port)
  if (packetsEl) packetsEl.textContent = String(source.packets_received)
  if (toggleBtn) toggleBtn.textContent = source.listening ? 'Stop Listener' : 'Start Listener'
}

// ─── Refresh / toggle ─────────────────────────────────────────────────────────

export async function refreshUdp(): Promise<void> {
  try {
    const status: UdpStatus = await invoke('get_udp_status')
    wsjtxListening = status.wsjtx.listening
    js8callListening = status.js8call.listening
    n1mmListening = status.n1mm.listening

    applySourceStatus(
      status.wsjtx,
      'udp-wsjtx-status', 'udp-wsjtx-port', 'udp-wsjtx-packets', 'udp-wsjtx-toggle-btn',
      'toggleWsjtx',
    )
    applySourceStatus(
      status.js8call,
      'udp-js8call-status', 'udp-js8call-port', 'udp-js8call-packets', 'udp-js8call-toggle-btn',
      'toggleJs8call',
    )
    applySourceStatus(
      status.n1mm,
      'udp-n1mm-status', 'udp-n1mm-port', 'udp-n1mm-packets', 'udp-n1mm-toggle-btn',
      'toggleN1mm',
    )

    // Status bar reflects WSJT-X as primary indicator (most common source)
    const anyListening = status.wsjtx.listening || status.js8call.listening || status.n1mm.listening
    const primaryPort = status.wsjtx.listening
      ? status.wsjtx.port
      : status.js8call.listening
        ? status.js8call.port
        : status.n1mm.port
    updateStatusBarUdp(anyListening, primaryPort)
  } catch (err) {
    _log(`UDP status error: ${_formatError(err)}`)
    updateStatusBarUdp(false, 2237)
  }
}

export async function toggleWsjtx(): Promise<void> {
  try {
    if (wsjtxListening) {
      await invoke('stop_udp_listener')
      _log('WSJT-X UDP listener stopped')
    } else {
      await invoke('start_udp_listener', { port: null })
      _log('WSJT-X UDP listener started')
    }
    await refreshUdp()
  } catch (err) {
    _log(`WSJT-X UDP toggle error: ${_formatError(err)}`)
  }
}

export async function toggleJs8call(): Promise<void> {
  try {
    if (js8callListening) {
      await invoke('stop_js8call_listener')
      _log('JS8Call UDP listener stopped')
    } else {
      await invoke('start_js8call_listener', { port: null })
      _log('JS8Call UDP listener started')
    }
    await refreshUdp()
  } catch (err) {
    _log(`JS8Call UDP toggle error: ${_formatError(err)}`)
  }
}

export async function toggleN1mm(): Promise<void> {
  try {
    if (n1mmListening) {
      await invoke('stop_n1mm_listener')
      _log('N1MM+ UDP listener stopped')
    } else {
      await invoke('start_n1mm_listener', { port: null })
      _log('N1MM+ UDP listener started on port 12060')
    }
    await refreshUdp()
  } catch (err) {
    _log(`N1MM+ UDP toggle error: ${_formatError(err)}`)
  }
}

// ─── Settings — UDP config loading ────────────────────────────────────────────

/**
 * Load persisted UDP settings from config into the Settings tab fields.
 * Uses best-effort invokes; falls back to defaults silently if the
 * command isn't implemented yet.
 */
export async function loadUdpSettingsValues(): Promise<void> {
  try {
    const udpCfg: {
      wsjtx_port: number
      wsjtx_auto_start: boolean
      wsjtx_multicast_group: string | null
      js8call_port: number
      js8call_auto_start: boolean
      n1mm_port: number
      n1mm_auto_start: boolean
      ft8battle_relay_enabled: boolean
    } = await invoke('get_udp_config')

    const wsjtxPortInput = document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement | null
    const js8callPortInput = document.getElementById('settings-udp-js8call-port') as HTMLInputElement | null
    const n1mmPortInput = document.getElementById('settings-udp-n1mm-port') as HTMLInputElement | null
    if (wsjtxPortInput) wsjtxPortInput.value = String(udpCfg.wsjtx_port)
    if (js8callPortInput) js8callPortInput.value = String(udpCfg.js8call_port)
    if (n1mmPortInput) n1mmPortInput.value = String(udpCfg.n1mm_port)

    const wsjtxAutoStart = document.getElementById('settings-udp-wsjtx-auto-start') as HTMLInputElement | null
    const js8callAutoStart = document.getElementById('settings-udp-js8call-auto-start') as HTMLInputElement | null
    const n1mmAutoStart = document.getElementById('settings-udp-n1mm-auto-start') as HTMLInputElement | null
    const ft8battleRelay = document.getElementById('settings-udp-ft8battle-relay') as HTMLInputElement | null
    if (wsjtxAutoStart) wsjtxAutoStart.checked = udpCfg.wsjtx_auto_start
    if (js8callAutoStart) js8callAutoStart.checked = udpCfg.js8call_auto_start
    if (n1mmAutoStart) n1mmAutoStart.checked = udpCfg.n1mm_auto_start
    if (ft8battleRelay) ft8battleRelay.checked = udpCfg.ft8battle_relay_enabled

    const multicastCheck = document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement | null
    const multicastGroupInput = document.getElementById('settings-udp-wsjtx-multicast-group') as HTMLInputElement | null
    const multicastGroupRow = document.getElementById('settings-udp-wsjtx-multicast-group-row')
    if (multicastCheck && multicastGroupInput && multicastGroupRow) {
      const group = udpCfg.wsjtx_multicast_group
      multicastCheck.checked = !!group
      multicastGroupInput.value = group ?? '224.0.0.73'
      multicastGroupRow.style.display = group ? '' : 'none'
    }
  } catch {
    // commands not available yet — leave placeholders
  }
}

// ─── Settings — UDP config saving ────────────────────────────────────────────

export async function saveUdpSettings(): Promise<void> {
  const wsjtxPortInput = document.getElementById('settings-udp-wsjtx-port') as HTMLInputElement | null
  const js8callPortInput = document.getElementById('settings-udp-js8call-port') as HTMLInputElement | null
  const n1mmPortInput = document.getElementById('settings-udp-n1mm-port') as HTMLInputElement | null
  const multicastCheck = document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement | null
  const multicastGroupInput = document.getElementById('settings-udp-wsjtx-multicast-group') as HTMLInputElement | null
  const wsjtxAutoStartInput = document.getElementById('settings-udp-wsjtx-auto-start') as HTMLInputElement | null
  const js8callAutoStartInput = document.getElementById('settings-udp-js8call-auto-start') as HTMLInputElement | null
  const n1mmAutoStartInput = document.getElementById('settings-udp-n1mm-auto-start') as HTMLInputElement | null
  const ft8battleRelayInput = document.getElementById('settings-udp-ft8battle-relay') as HTMLInputElement | null

  const wsjtxPort = wsjtxPortInput ? parseInt(wsjtxPortInput.value, 10) : NaN
  const js8callPort = js8callPortInput ? parseInt(js8callPortInput.value, 10) : NaN
  const n1mmPort = n1mmPortInput ? parseInt(n1mmPortInput.value, 10) : NaN

  const valid = (p: number) => Number.isFinite(p) && p >= 1024 && p <= 65535
  if (!valid(wsjtxPort) || !valid(js8callPort) || !valid(n1mmPort)) {
    _log('All UDP ports must be between 1024 and 65535')
    return
  }

  // Multicast group: only send if checkbox is checked and a value is provided.
  const multicastEnabled = multicastCheck?.checked ?? false
  const multicastGroup = multicastEnabled
    ? (multicastGroupInput?.value.trim() || '224.0.0.73')
    : null

  // Basic multicast address validation when enabled.
  if (multicastEnabled && multicastGroup) {
    const parts = multicastGroup.split('.')
    const first = parseInt(parts[0] ?? '', 10)
    if (parts.length !== 4 || first < 224 || first > 239) {
      _log(`Multicast group must be in the 224.0.0.0–239.255.255.255 range (got ${multicastGroup})`)
      return
    }
  }

  const wsjtxAutoStart = wsjtxAutoStartInput?.checked ?? false
  const js8callAutoStart = js8callAutoStartInput?.checked ?? false
  const n1mmAutoStart = n1mmAutoStartInput?.checked ?? false
  const ft8battleRelayEnabled = ft8battleRelayInput?.checked ?? false

  try {
    await invoke('save_udp_settings', {
      request: {
        wsjtx_port: wsjtxPort,
        js8call_port: js8callPort,
        n1mm_port: n1mmPort,
        wsjtx_multicast_group: multicastGroup,
        wsjtx_auto_start: wsjtxAutoStart,
        js8call_auto_start: js8callAutoStart,
        n1mm_auto_start: n1mmAutoStart,
        ft8battle_relay_enabled: ft8battleRelayEnabled,
      },
    })

    const mcSuffix = multicastGroup ? `, multicast: ${multicastGroup}` : ''
    const ft8battleSuffix = ft8battleRelayEnabled ? ', FT8Battle relay: on' : ', FT8Battle relay: off'
    _log(`UDP settings saved — WSJT-X: ${wsjtxPort}, JS8Call: ${js8callPort}, N1MM+: ${n1mmPort}${mcSuffix}${ft8battleSuffix} — restart listeners to apply`)
  } catch (err) {
    _log(`Failed to save UDP settings: ${_formatError(err)}`)
  }
}

// ─── DOM wiring helper ────────────────────────────────────────────────────────

/** Wire UDP listener click handlers to the DOM. Call from the shell event wiring. */
export function wireUdpListenerListeners(): void {
  const bindClick = (id: string, handler: () => void): void => {
    const el = document.getElementById(id)
    if (el) el.addEventListener('click', handler)
  }

  bindClick('udp-wsjtx-toggle-btn', () => { void toggleWsjtx() })
  bindClick('udp-js8call-toggle-btn', () => { void toggleJs8call() })
  bindClick('udp-n1mm-toggle-btn', () => { void toggleN1mm() })
  bindClick('settings-save-udp-btn', () => { void saveUdpSettings() })

  // Show/hide multicast group input when checkbox changes.
  const multicastCheck = document.getElementById('settings-udp-wsjtx-multicast') as HTMLInputElement | null
  const multicastGroupRow = document.getElementById('settings-udp-wsjtx-multicast-group-row')
  if (multicastCheck && multicastGroupRow) {
    multicastCheck.addEventListener('change', () => {
      multicastGroupRow.style.display = multicastCheck.checked ? '' : 'none'
    })
  }
}