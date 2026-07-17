/**
 * Rig Control — rig status display, QSY prompt, and rig event handling.
 *
 * Extracted from main.ts to isolate the rig control domain cluster.
 * Handles refreshing rig status from the backend, applying it to the DOM,
 * prompting the user for QSY, and wiring Tauri events for rig frequency,
 * mode, and status changes.
 *
 * The module is initialised via `initRigControlEvents()` which subscribes
 * to Tauri events. The host injects callbacks for QSO-form autofill and
 * rig event forwarding via `setAutofillQsoDraft`, `setOnRigFrequencyChanged`,
 * `setOnRigModeChanged`.
 */

import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { formatError } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface RigStatus {
  connected: boolean
  backend: string | null
  host: string | null
  port: number | null
  frequency_hz: number | null
  frequency_display: string | null
  mode: string | null
  band: string | null
  bandwidth_hz: number | null
  s_meter: number | null
  power: number | null
  vfo: string | null
  strength: number | null
  last_error: string | null
}

export interface RigFrequencyChangedEvent {
  frequency_hz: number
  frequency_display: string
  band: string
}

export interface RigModeChangedEvent {
  mode: string
}

// ─── Module state ─────────────────────────────────────────────────────────────

let currentRigStatus: RigStatus | null = null

/** Return the current cached rig status (used by qso-form for autofill). */
export function getCurrentRigStatus(): RigStatus | null {
  return currentRigStatus
}

/** Reset module state (for testing). */
export function resetRigState(): void {
  currentRigStatus = null
}

// ─── Dependency injection ─────────────────────────────────────────────────────

let _log: (msg: string) => void = () => {}
let _autofillQsoDraft: (status: RigStatus) => void = () => {}
let _onRigFrequencyChanged: (payload: RigFrequencyChangedEvent) => void = () => {}
let _onRigModeChanged: (payload: RigModeChangedEvent) => void = () => {}

export function setLogger(fn: (msg: string) => void): void { _log = fn }
export function setAutofillQsoDraft(fn: (status: RigStatus) => void): void { _autofillQsoDraft = fn }
export function setOnRigFrequencyChanged(fn: (payload: RigFrequencyChangedEvent) => void): void { _onRigFrequencyChanged = fn }
export function setOnRigModeChanged(fn: (payload: RigModeChangedEvent) => void): void { _onRigModeChanged = fn }

// ─── Status bar updater ───────────────────────────────────────────────────────

/**
 * Update the rig section of the persistent status bar.
 * Exported so the shell or event handlers can call it.
 */
export function updateStatusBarRig(
  freq: string | null | undefined,
  mode: string | null | undefined,
  band: string | null | undefined,
): void {
  const freqEl = document.getElementById('statusbar-rig-freq')
  const modeEl = document.getElementById('statusbar-rig-mode')
  const bandEl = document.getElementById('statusbar-rig-band')
  if (freqEl) freqEl.textContent = freq || '—'
  if (modeEl) modeEl.textContent = mode ? `[${mode}]` : ''
  if (bandEl) bandEl.textContent = band || ''
}

// ─── Rig status refresh & display ─────────────────────────────────────────────

export async function refreshRig(): Promise<void> {
  try {
    const status: RigStatus = await invoke('get_rig_status')
    currentRigStatus = status
    applyRigStatus(status)
  } catch (err) {
    _log(`Rig status error: ${formatError(err)}`)
  }
}

export function applyRigStatus(status: RigStatus): void {
  const connectionEl = document.getElementById('rig-connection')!
  const backendEl = document.getElementById('rig-backend')!
  const freqEl = document.getElementById('rig-frequency-display')!
  const modeEl = document.getElementById('rig-mode')!
  const bandEl = document.getElementById('rig-band')!
  const powerEl = document.getElementById('rig-power')!
  const smeterValueEl = document.getElementById('rig-smeter-value')!
  const smeterFill = document.getElementById('rig-smeter-fill')!
  const rigErrorRow = document.getElementById('rig-error-row')!
  const rigError = document.getElementById('rig-error')!

  connectionEl.textContent = status.connected ? 'Connected' : 'Disconnected'
  connectionEl.className = `value ${status.connected ? 'active' : 'inactive'}`

  backendEl.textContent = status.backend ? `${status.backend}@${status.host}:${status.port}` : '—'
  freqEl.textContent = status.frequency_display || '---.---.---'
  modeEl.textContent = status.mode || '—'
  bandEl.textContent = status.band || '—'

  const power = typeof status.power === 'number' ? status.power : null
  powerEl.textContent = power == null ? '—' : `${power.toFixed(1)} W`

  const strength = typeof status.strength === 'number' ? status.strength : status.s_meter
  smeterValueEl.textContent = strength == null ? '—' : strength.toFixed(1)

  const normalized = strength == null ? 0 : Math.max(0, Math.min(100, strength))
  smeterFill.style.width = `${normalized}%`

  if (status.last_error) {
    rigErrorRow.style.display = ''
    rigError.textContent = status.last_error
  } else {
    rigErrorRow.style.display = 'none'
  }

  // Status bar
  updateStatusBarRig(status.frequency_display, status.mode, status.band)

  // QSO form autofill
  _autofillQsoDraft(status)
}

// ─── QSY prompt ───────────────────────────────────────────────────────────────

export async function promptQsy(): Promise<void> {
  if (!currentRigStatus?.connected) {
    _log('Rig is not connected — cannot QSY')
    return
  }

  const current = currentRigStatus.frequency_hz ? String(currentRigStatus.frequency_hz) : ''
  const input = window.prompt('Set rig frequency (Hz):', current)
  if (!input) return

  const next = Number(input)
  if (!Number.isFinite(next) || next <= 0) {
    _log('Invalid frequency value')
    return
  }

  try {
    const status: RigStatus = await invoke('set_rig_frequency', { freq: Math.round(next) })
    currentRigStatus = status
    applyRigStatus(status)
    _log(`QSY to ${status.frequency_display || Math.round(next)}`)
  } catch (err) {
    _log(`QSY failed: ${formatError(err)}`)
  }
}

// ─── Tauri event listeners ────────────────────────────────────────────────────

export function initRigControlEvents(): void {
  void listen<RigFrequencyChangedEvent>('rig_frequency_changed', (event) => {
    const payload = event.payload
    const freqEl = document.getElementById('rig-frequency-display')!
    const bandEl = document.getElementById('rig-band')!
    freqEl.textContent = payload.frequency_display
    bandEl.textContent = payload.band

    _onRigFrequencyChanged(payload)

    // Refresh status bar rig section
    const modeEl = document.getElementById('rig-mode')
    updateStatusBarRig(payload.frequency_display, modeEl?.textContent ?? null, payload.band)
  })

  void listen<RigModeChangedEvent>('rig_mode_changed', (event) => {
    const payload = event.payload
    const modeEl = document.getElementById('rig-mode')!
    modeEl.textContent = payload.mode

    _onRigModeChanged(payload)

    const freqEl = document.getElementById('rig-frequency-display')
    const bandEl = document.getElementById('rig-band')
    updateStatusBarRig(freqEl?.textContent ?? null, payload.mode, bandEl?.textContent ?? null)
  })

  void listen<RigStatus>('rig_status_changed', (event) => {
    currentRigStatus = event.payload
    applyRigStatus(event.payload)
  })
}