/**
 * QSO Entry Form — form init, callsign lookup/history, WSJT-X decode panel,
 * and QSO submission logic.
 *
 * Extracted from main.ts to isolate the QSO entry responsibility cluster and
 * reduce the god-file. All DOM interaction and Tauri invoke calls related to
 * the QSO form, callsign lookup, callsign history, frequency-to-band mapping,
 * RST defaults, WSJT-X live decode panel, and the "Log QSO" workflow live here.
 *
 * The module is initialised via `initQsoForm()` and `setupCallsignLookup()`.
 * The host shell injects callbacks for post-log actions (refresh sync, load
 * QSOs) and for accessing the current rig status.
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError, escapeHtml } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface CallsignLookupResult {
  callsign: string
  full_name?: string
  grid?: string
  dxcc?: number
  country?: string
  state?: string
  cq_zone?: number
  itu_zone?: number
  source?: string
}

export interface CallsignHistoryItem {
  datetime_on: string
  band: string
  mode: string
}

export interface DecodeLogStatus {
  state: 'new' | 'needed' | 'worked' | 'confirmed'
  label: string
  worked_count: number
  exact_match_count: number
  confirmed_match_count: number
  last_worked_at: string | null
}

export interface RecentDecode {
  callsign?: string | null
  message: string
  grid?: string | null
  distance_km?: number | null
  snr?: number | null
  frequency_hz?: number | null
  freq_mhz?: number | null
  mode?: string | null
  band?: string | null
  last_activity: string
  source: string
  log_status: DecodeLogStatus
}

export interface WsjtxDecodePanelSettings {
  enabled: boolean
}

/** Subset of RigStatus that the QSO form cares about. */
export interface RigStatusLike {
  connected: boolean
  frequency_hz: number | null
  frequency_display: string | null
  mode: string | null
  band: string | null
}

// ─── Module state ─────────────────────────────────────────────────────────────

let callsignLookupTimer: ReturnType<typeof setTimeout> | null = null
let wsjtxRecentDecodes: RecentDecode[] = []
let wsjtxDecodePanelEnabled = true
let wsjtxSelectedDecodeMessage: string | null = null
let wsjtxFormOverrideActive = false

/** Logger injected by the host module via `setLogger`. */
let log: (msg: string) => void = (_msg: string) => {}

/** Callback injected by the host to retrieve the current rig status. */
let getCurrentRigStatus: () => RigStatusLike | null = () => null

/** Callback injected by the host — called after a QSO is successfully queued. */
let onAfterLogQso: () => Promise<void> = async () => {}

// ─── Constants ────────────────────────────────────────────────────────────────

const PHONE_MODES = new Set(['SSB', 'USB', 'LSB', 'AM', 'FM', 'DSTAR', 'DMR', 'C4FM'])
const DATA_MODES = new Set(['CW', 'FT8', 'FT4', 'RTTY', 'PSK31', 'JS8', 'WSPR'])

// ─── Public API — dependency injection ────────────────────────────────────────

/** Inject a logger function. Called once during app init. */
export function setLogger(fn: (msg: string) => void): void {
  log = fn
}

/** Inject a getter for the current rig status (used by initQsoForm / autofillQsoDraft). */
export function setGetCurrentRigStatus(fn: () => RigStatusLike | null): void {
  getCurrentRigStatus = fn
}

/** Inject a callback invoked after a QSO is successfully queued (e.g. refreshSync + loadLogQsos). */
export function setOnAfterLogQso(fn: () => Promise<void>): void {
  onAfterLogQso = fn
}

// ─── Public API — form lifecycle ──────────────────────────────────────────────

/** Set the datetime-local input to the current local time and restore sensible defaults. */
export function initQsoForm(): void {
  const datetimeInput = document.getElementById('qso-datetime') as HTMLInputElement | null
  if (datetimeInput) {
    datetimeInput.value = toLocalDateTimeInputValue(new Date())
  }

  const rigStatus = getCurrentRigStatus()
  const modeInput = document.getElementById('qso-mode') as HTMLSelectElement | null
  const bandInput = document.getElementById('qso-band') as HTMLSelectElement | null
  if (modeInput && !modeInput.value) modeInput.value = rigStatus?.mode || 'SSB'
  if (bandInput && !bandInput.value) bandInput.value = rigStatus?.band || '20m'

  if (rigStatus) {
    autofillQsoDraft(rigStatus)
  }
  applyDefaultRst(modeInput?.value ?? rigStatus?.mode, true)
  clearCallsignHistory()
}

/** Reset all QSO entry form fields to their defaults. */
export function clearQsoForm(): void {
  ;(document.getElementById('qso-callsign') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-frequency') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-band') as HTMLSelectElement).value = ''
  ;(document.getElementById('qso-mode') as HTMLSelectElement).value = ''
  ;(document.getElementById('qso-rst-sent') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-rst-rcvd') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-name') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-qth') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-power') as HTMLInputElement).value = ''
  ;(document.getElementById('qso-notes') as HTMLTextAreaElement).value = ''
  document.getElementById('qso-form-status')!.textContent = ''
  if (callsignLookupTimer) {
    clearTimeout(callsignLookupTimer)
    callsignLookupTimer = null
  }
  wsjtxSelectedDecodeMessage = null
  wsjtxFormOverrideActive = false
  hideCallsignInfo()
  initQsoForm()
  renderWsjtxDecodePanel(wsjtxRecentDecodes)
}

/** Queue the QSO entry form for sync via Tauri. */
export async function handleLogQso(): Promise<void> {
  const callsign = (document.getElementById('qso-callsign') as HTMLInputElement).value.trim().toUpperCase()
  const band = (document.getElementById('qso-band') as HTMLSelectElement).value
  const mode = (document.getElementById('qso-mode') as HTMLSelectElement).value
  const datetimeVal = (document.getElementById('qso-datetime') as HTMLInputElement).value
  const rstSent = (document.getElementById('qso-rst-sent') as HTMLInputElement).value.trim()
  const rstRcvd = (document.getElementById('qso-rst-rcvd') as HTMLInputElement).value.trim()
  const frequencyVal = (document.getElementById('qso-frequency') as HTMLInputElement).value.trim()
  const powerVal = (document.getElementById('qso-power') as HTMLInputElement).value.trim()
  const name = (document.getElementById('qso-name') as HTMLInputElement).value.trim()
  const qth = (document.getElementById('qso-qth') as HTMLInputElement).value.trim()
  const notes = (document.getElementById('qso-notes') as HTMLTextAreaElement).value.trim()

  const statusEl = document.getElementById('qso-form-status')!

  if (!callsign) { statusEl.textContent = '⚠ Callsign required'; return }
  if (!band) { statusEl.textContent = '⚠ Band required'; return }
  if (!mode) { statusEl.textContent = '⚠ Mode required'; return }
  if (!datetimeVal) { statusEl.textContent = '⚠ Date/Time required'; return }

  const frequencyMhz = frequencyVal ? Number.parseFloat(frequencyVal) : null
  const powerW = powerVal ? Number.parseFloat(powerVal) : null
  if (frequencyVal && (!Number.isFinite(frequencyMhz) || (frequencyMhz ?? 0) <= 0)) {
    statusEl.textContent = '⚠ Frequency must be a valid MHz value'
    return
  }
  if (powerVal && (!Number.isFinite(powerW) || (powerW ?? 0) < 0)) {
    statusEl.textContent = '⚠ Power must be a valid watt value'
    return
  }

  statusEl.textContent = 'Queueing…'

  try {
    await invoke('create_qso', {
      request: {
        callsign,
        band,
        mode,
        datetime_on: new Date(datetimeVal).toISOString(),
        rst_sent: rstSent || null,
        rst_rcvd: rstRcvd || null,
        notes: notes || null,
        desktop_meta: {
          frequency_mhz: frequencyMhz,
          power_w: powerW,
          name: name || null,
          qth: qth || null,
        },
      },
    })

    log(`QSO queued: ${callsign} on ${band} ${mode}`)

    clearQsoForm()
    statusEl.textContent = '✓ QSO queued for sync'
    await onAfterLogQso()
    void refreshWsjtxDecodes()

    setTimeout(() => {
      const currentStatus = document.getElementById('qso-form-status')
      if (currentStatus?.textContent === '✓ QSO queued for sync') currentStatus.textContent = ''
    }, 3000)
  } catch (err) {
    statusEl.textContent = `✗ ${formatError(err)}`
    log(`Failed to queue QSO: ${formatError(err)}`)
  }
}

// ─── Public API — rig autofill ────────────────────────────────────────────────

/** Auto-fill QSO form fields from the current rig status (unless WSJT-X override is active). */
export function autofillQsoDraft(status: RigStatusLike): void {
  if (wsjtxFormOverrideActive) return

  const freqInput = document.getElementById('qso-frequency') as HTMLInputElement | null
  const modeInput = document.getElementById('qso-mode') as HTMLSelectElement | null
  const bandInput = document.getElementById('qso-band') as HTMLSelectElement | null

  if (!freqInput || !modeInput || !bandInput) return

  if (status.frequency_hz) freqInput.value = (status.frequency_hz / 1e6).toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
  if (status.mode) modeInput.value = status.mode
  if (status.band) bandInput.value = status.band
  applyDefaultRst(status.mode)
}

// ─── Public API — callsign lookup & history ────────────────────────────────────

/** Wire up the callsign input field with debounced lookup and history loading. */
export function setupCallsignLookup(): void {
  const callsignInput = document.getElementById('qso-callsign') as HTMLInputElement | null
  if (!callsignInput) return

  callsignInput.addEventListener('input', () => {
    if (callsignLookupTimer) clearTimeout(callsignLookupTimer)

    const call = callsignInput.value.trim().toUpperCase()
    callsignInput.value = call
    if (call.length < 3) {
      hideCallsignInfo()
      clearCallsignHistory()
      return
    }

    callsignLookupTimer = setTimeout(() => {
      void Promise.allSettled([doCallsignLookup(call), loadCallsignHistory(call)])
    }, 350)
  })

  callsignInput.addEventListener('blur', () => {
    const call = callsignInput.value.trim().toUpperCase()
    if (call.length >= 3) {
      void Promise.allSettled([doCallsignLookup(call), loadCallsignHistory(call)])
    }
  })
}

// ─── Public API — event handlers (called by shell/rig wiring on rig events) ───

/** Handle rig frequency changed event — update QSO form fields if no WSJT-X override. */
export function onRigFrequencyChanged(payload: { frequency_hz: number; frequency_display: string; band: string }): void {
  if (!wsjtxFormOverrideActive) {
    const qsoFrequency = document.getElementById('qso-frequency') as HTMLInputElement | null
    const qsoBand = document.getElementById('qso-band') as HTMLSelectElement | null
    if (qsoFrequency) qsoFrequency.value = (payload.frequency_hz / 1e6).toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
    if (qsoBand) qsoBand.value = payload.band
  }
}

/** Handle rig mode changed event — update QSO form mode field if no WSJT-X override. */
export function onRigModeChanged(payload: { mode: string }): void {
  if (!wsjtxFormOverrideActive) {
    const qsoMode = document.getElementById('qso-mode') as HTMLSelectElement | null
    if (qsoMode) qsoMode.value = payload.mode
  }
}

// ─── Public API — WSJT-X decode panel ─────────────────────────────────────────

/** Load WSJT-X decode panel settings from the backend. */
export async function loadWsjtxDecodePanelSettings(): Promise<void> {
  try {
    const settings: WsjtxDecodePanelSettings = await invoke('get_wsjtx_decode_panel_settings')
    wsjtxDecodePanelEnabled = settings.enabled
  } catch {
    wsjtxDecodePanelEnabled = true
  }

  const checkbox = document.getElementById('settings-wsjtx-decode-panel-enabled') as HTMLInputElement | null
  if (checkbox) checkbox.checked = wsjtxDecodePanelEnabled
  applyWsjtxDecodePanelVisibility()
}

/** Save the decode panel enabled/disabled setting and update visibility. */
export async function saveWsjtxDecodePanelSettings(enabled: boolean): Promise<void> {
  wsjtxDecodePanelEnabled = enabled
  applyWsjtxDecodePanelVisibility()
  try {
    await invoke('save_wsjtx_decode_panel_settings', { enabled })
    log(`WSJT-X live decode panel ${enabled ? 'enabled' : 'disabled'}`)
  } catch (err) {
    log(`Failed to save WSJT-X decode panel setting: ${formatError(err)}`)
  }
}

/** Refresh the WSJT-X decode list from the backend. */
export async function refreshWsjtxDecodes(): Promise<void> {
  try {
    const decodes: RecentDecode[] = await invoke('list_recent_wsjtx_decodes')
    wsjtxRecentDecodes = decodes
    renderWsjtxDecodePanel(decodes)
  } catch {
    renderWsjtxDecodePanel([])
  }
}

/** Handle wsjtx-decode-list-changed Tauri event. */
export function onWsjtxDecodeListChanged(decodes: RecentDecode[]): void {
  wsjtxRecentDecodes = decodes
  renderWsjtxDecodePanel(decodes)
}

// ─── Public API — pure helpers (exported for testability) ──────────────────────

/** Map an amateur radio frequency (MHz) to an ITU band name. */
export function freqToBand(mhz: number): string {
  if (mhz >= 1.8 && mhz <= 2.0) return '160m'
  if (mhz >= 3.5 && mhz <= 4.0) return '80m'
  if (mhz >= 5.3 && mhz <= 5.4) return '60m'
  if (mhz >= 7.0 && mhz <= 7.3) return '40m'
  if (mhz >= 10.1 && mhz <= 10.15) return '30m'
  if (mhz >= 14.0 && mhz <= 14.35) return '20m'
  if (mhz >= 18.068 && mhz <= 18.168) return '17m'
  if (mhz >= 21.0 && mhz <= 21.45) return '15m'
  if (mhz >= 24.89 && mhz <= 24.99) return '12m'
  if (mhz >= 28.0 && mhz <= 29.7) return '10m'
  if (mhz >= 50.0 && mhz <= 54.0) return '6m'
  if (mhz >= 70.0 && mhz <= 71.0) return '4m'
  if (mhz >= 144.0 && mhz <= 148.0) return '2m'
  if (mhz >= 222.0 && mhz <= 225.0) return '1.25m'
  if (mhz >= 420.0 && mhz <= 450.0) return '70cm'
  if (mhz >= 902.0 && mhz <= 928.0) return '33cm'
  if (mhz >= 1240.0 && mhz <= 1300.0) return '23cm'
  return ''
}

/** Convert a Date to a datetime-local input value string (local time). */
export function toLocalDateTimeInputValue(date: Date): string {
  const pad = (value: number): string => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** Return default RST values for a given mode. */
export function defaultRstForMode(mode: string | null | undefined): { sent: string, rcvd: string } {
  const normalized = (mode || '').trim().toUpperCase()
  if (DATA_MODES.has(normalized)) return { sent: '599', rcvd: '599' }
  if (PHONE_MODES.has(normalized)) return { sent: '59', rcvd: '59' }
  return { sent: '59', rcvd: '59' }
}

/** Apply default RST to the form inputs, optionally forcing even if user has custom values. */
export function applyDefaultRst(mode: string | null | undefined, force = false): void {
  const sentInput = document.getElementById('qso-rst-sent') as HTMLInputElement | null
  const rcvdInput = document.getElementById('qso-rst-rcvd') as HTMLInputElement | null
  if (!sentInput || !rcvdInput) return

  const defaults = defaultRstForMode(mode)
  const currentValuesAreDefaults = ['59', '599', ''].includes(sentInput.value.trim())
    && ['59', '599', ''].includes(rcvdInput.value.trim())

  if (force || currentValuesAreDefaults) {
    sentInput.value = defaults.sent
    rcvdInput.value = defaults.rcvd
  }
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

function clearCallsignHistory(): void {
  const panel = document.getElementById('qso-history-panel')
  const empty = document.getElementById('qso-history-empty')
  const tbody = document.getElementById('qso-history-body')
  const count = document.getElementById('qso-history-count')
  if (panel) panel.style.display = 'none'
  if (empty) empty.textContent = 'Enter a callsign to see previous contacts.'
  if (tbody) tbody.innerHTML = ''
  if (count) count.textContent = '0 previous QSOs'
}

function hideCallsignInfo(): void {
  const infoDiv = document.getElementById('qso-callsign-info')
  if (infoDiv) infoDiv.style.display = 'none'
}

async function doCallsignLookup(callsign: string): Promise<void> {
  try {
    const result: CallsignLookupResult = await invoke('lookup_callsign', { callsign })
    const infoDiv = document.getElementById('qso-callsign-info')
    const nameSpan = document.getElementById('qso-callsign-name')
    const locationSpan = document.getElementById('qso-callsign-location')

    if (!infoDiv || !nameSpan || !locationSpan) return

    const name = result.full_name || ''
    const locationParts: string[] = []
    if (result.state) locationParts.push(result.state)
    if (result.country) locationParts.push(result.country)
    const location = locationParts.join(', ')

    if (name || location) {
      nameSpan.textContent = name
      locationSpan.textContent = location ? `(${location})` : ''
      infoDiv.style.display = ''
    } else {
      hideCallsignInfo()
    }

    const nameInput = document.getElementById('qso-name') as HTMLInputElement | null
    const qthInput = document.getElementById('qso-qth') as HTMLInputElement | null
    if (name && nameInput && !nameInput.value.trim()) {
      nameInput.value = name
    }
    if (location && qthInput && !qthInput.value.trim()) {
      qthInput.value = location
    }
  } catch {
    hideCallsignInfo()
  }
}

async function loadCallsignHistory(callsign: string): Promise<void> {
  const panel = document.getElementById('qso-history-panel')
  const tbody = document.getElementById('qso-history-body')
  const empty = document.getElementById('qso-history-empty')
  const count = document.getElementById('qso-history-count')
  if (!panel || !tbody || !empty || !count) return

  try {
    const history: CallsignHistoryItem[] = await invoke('get_callsign_history', { callsign, limit: 8 })
    panel.style.display = ''
    tbody.innerHTML = ''
    count.textContent = `${history.length} previous QSO${history.length === 1 ? '' : 's'}`

    if (history.length === 0) {
      empty.textContent = `No previous QSOs with ${callsign} in local cache yet.`
      empty.style.display = 'block'
      return
    }

    empty.style.display = 'none'
    for (const entry of history) {
      const row = document.createElement('tr')
      row.innerHTML = `
        <td>${escapeHtml(new Date(entry.datetime_on).toLocaleString())}</td>
        <td>${escapeHtml(entry.band)}</td>
        <td>${escapeHtml(entry.mode)}</td>
      `
      tbody.appendChild(row)
    }
  } catch {
    panel.style.display = ''
    tbody.innerHTML = ''
    empty.textContent = 'Unable to load local callsign history.'
    empty.style.display = 'block'
    count.textContent = 'History unavailable'
  }
}

function applyWsjtxDecodePanelVisibility(): void {
  const panel = document.getElementById('wsjtx-decode-panel')
  if (!panel) return
  panel.style.display = wsjtxDecodePanelEnabled ? '' : 'none'
}

function renderWsjtxDecodePanel(decodes: RecentDecode[]): void {
  const panel = document.getElementById('wsjtx-decode-panel')
  const body = document.getElementById('wsjtx-decode-body')
  const empty = document.getElementById('wsjtx-decode-empty')
  const count = document.getElementById('wsjtx-decode-count')
  if (!panel || !body || !empty || !count) return

  applyWsjtxDecodePanelVisibility()
  body.innerHTML = ''

  if (decodes.length === 0) {
    count.textContent = 'Listening for decodes…'
    empty.style.display = 'block'
    empty.textContent = 'No recent WSJT-X decodes yet.'
    return
  }

  count.textContent = `${decodes.length} recent decode${decodes.length === 1 ? '' : 's'}`
  empty.style.display = 'none'

  for (const decode of decodes) {
    const row = document.createElement('tr')
    row.className = `decode-row-${decode.log_status.state}`
    if (wsjtxSelectedDecodeMessage === decode.message) row.classList.add('decode-row-selected')
    row.addEventListener('click', () => { applyWsjtxDecodeToForm(decode) })

    const distance = decode.distance_km != null ? `${decode.distance_km} km` : '—'
    const snr = decode.snr != null ? String(decode.snr) : '—'
    const lastActivity = formatDecodeTimestamp(decode.last_activity)
    const metaParts = [decode.callsign || null, decode.mode || '—', decode.band || '—'].filter(Boolean) as string[]

    row.innerHTML = `
      <td class="decode-callsign-cell">
        ${escapeHtml(decode.message)}
        <span class="decode-meta">${escapeHtml(metaParts.join(' · '))}</span>
      </td>
      <td>${escapeHtml(decode.grid || '—')}</td>
      <td>${escapeHtml(distance)}</td>
      <td>${escapeHtml(snr)}</td>
      <td>
        <span class="decode-status-badge decode-state-${decode.log_status.state}">${escapeHtml(decode.log_status.label)}</span>
        <span class="decode-meta">${escapeHtml(lastActivity)}</span>
      </td>
    `
    body.appendChild(row)
  }
}

function formatDecodeTimestamp(timestamp: string): string {
  const parsed = new Date(timestamp)
  if (Number.isNaN(parsed.getTime())) return '—'
  return parsed.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function applyWsjtxDecodeToForm(decode: RecentDecode): void {
  const callsignInput = document.getElementById('qso-callsign') as HTMLInputElement | null
  const frequencyInput = document.getElementById('qso-frequency') as HTMLInputElement | null
  const modeInput = document.getElementById('qso-mode') as HTMLSelectElement | null
  const bandInput = document.getElementById('qso-band') as HTMLSelectElement | null
  const statusEl = document.getElementById('qso-form-status')

  if (!callsignInput || !frequencyInput || !modeInput || !bandInput) return

  wsjtxSelectedDecodeMessage = decode.message
  wsjtxFormOverrideActive = true
  if (decode.callsign) {
    callsignInput.value = decode.callsign
  } else {
    callsignInput.value = ''
  }
  if (decode.freq_mhz != null) {
    frequencyInput.value = decode.freq_mhz.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
  }
  if (decode.mode) modeInput.value = decode.mode
  if (decode.band) bandInput.value = decode.band
  applyDefaultRst(decode.mode, true)
  callsignInput.dispatchEvent(new Event('input', { bubbles: true }))
  if (decode.callsign) {
    void Promise.allSettled([doCallsignLookup(decode.callsign), loadCallsignHistory(decode.callsign)])
    if (statusEl) statusEl.textContent = 'WSJT-X decode selected — rig autofill is paused until you clear the form.'
  } else if (statusEl) {
    statusEl.textContent = 'WSJT-X raw decode selected — no callsign was parsed, so only frequency, mode, and band were applied.'
  }
  renderWsjtxDecodePanel(wsjtxRecentDecodes)
}

// escapeHtml, formatError → ui-helpers.ts