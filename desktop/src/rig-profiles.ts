/**
 * Rig Profile Management — CRUD UI for radio rig profiles.
 *
 * Extracted from main.ts to isolate rig profile ownership and reduce
 * the god-file.  All DOM interaction and Tauri invoke calls related to
 * listing, creating, updating, deleting, activating, and testing rig
 * profiles live here.
 *
 * The module is initialised via `initRigProfiles()` which loads existing
 * profiles and renders the sidebar list.  After activating a profile the
 * caller may need to refresh rig status, so an `onAfterActivate` callback
 * is accepted.
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

interface RigProfile {
  id: string
  name: string
  interface_type: 'hamlib' | 'flrig' | 'external' | 'tci'
  rig_model_id: number
  rig_model_name: string
  serial_port: string
  baud_rate: number
  data_bits: number
  stop_bits: number
  flow_control: 'none' | 'hardware' | 'software'
  parity: 'none' | 'even' | 'odd'
  civ_address: string | null
  ptt_type: 'none' | 'cat' | 'dtr' | 'rts'
  host: string
  port: number
  tci_receiver?: number
  poll_interval_ms: number
}

interface RigModel {
  id: number
  manufacturer: string
  name: string
  status: string
}

interface SerialPortInfo {
  port_name: string
  description: string
}

interface TestConnectionResult {
  success: boolean
  message: string
  frequency_hz: number | null
  mode: string | null
}

// ─── Module state ──────────────────────────────────────────────────────────────

let rigProfiles: RigProfile[] = []
let rigActiveProfileId: string | null = null
let rigEditingProfileId: string | null = null
let rigModels: RigModel[] = []

/** Logger injected by the host module via `setLogger`. */
let log: (msg: string) => void = (_msg: string) => {}

/** Callback invoked after a profile is activated (so host can refresh rig status). */
let onAfterActivate: () => Promise<void> = async () => {}

// ─── Public API ────────────────────────────────────────────────────────────────

/** Inject a logger function.  Called once during app init. */
export function setLogger(fn: (msg: string) => void): void {
  log = fn
}

/** Inject a post-activate callback.  Called once during app init. */
export function setOnAfterActivate(fn: () => Promise<void>): void {
  onAfterActivate = fn
}

/** Load rig profiles and the active profile ID from the backend, then render. */
export async function rigProfilesLoad(): Promise<void> {
  try {
    const [profiles, activeProfileId] = await Promise.all([
      invoke<RigProfile[]>('list_rig_profiles'),
      invoke<string | null>('get_active_rig_profile_id'),
    ])

    rigProfiles = profiles
    rigActiveProfileId = activeProfileId
    rigProfilesRender()

    // Load rig models in background for the dropdown.
    void rigProfileLoadModels()
  } catch (err) {
    log(`Failed to load rig profiles: ${formatError(err)}`)
  }
}

/** Render the rig profile sidebar list from current state. */
export function rigProfilesRender(): void {
  const list = document.getElementById('rig-profiles-list')
  if (!list) return

  list.innerHTML = ''
  if (rigProfiles.length === 0) {
    list.innerHTML = '<p class="hint" style="padding:0.5rem;font-size:0.85rem">No profiles yet.</p>'
  } else {
    for (const p of rigProfiles) {
      const btn = document.createElement('button')
      btn.type = 'button'
      btn.className = 'rig-profile-item' + (p.id === rigActiveProfileId ? ' active' : '')
      btn.dataset.id = p.id

      if (p.id === rigActiveProfileId) {
        const dot = document.createElement('span')
        dot.className = 'profile-active-dot'
        btn.appendChild(dot)
      }

      const nameSpan = document.createElement('span')
      nameSpan.textContent = p.name
      btn.appendChild(nameSpan)

      btn.addEventListener('click', () => {
        rigEditingProfileId = p.id
        rigProfileFormLoad(p)
      })
      list.appendChild(btn)
    }
  }

  rigProfileUpdateActivateButton()
}

/** Show/hide interface-specific fields when the user changes the interface dropdown. */
export function rigProfileInterfaceChanged(): void {
  const iface = (document.getElementById('rp-interface') as HTMLSelectElement)?.value
  const hFields = document.getElementById('rp-hamlib-fields')
  const fFields = document.getElementById('rp-flrig-fields')
  const eFields = document.getElementById('rp-external-fields')
  const tFields = document.getElementById('rp-tci-fields')

  if (hFields) hFields.style.display = iface === 'hamlib' ? '' : 'none'
  if (fFields) fFields.style.display = iface === 'flrig' ? '' : 'none'
  if (eFields) eFields.style.display = iface === 'external' ? '' : 'none'
  if (tFields) tFields.style.display = iface === 'tci' ? '' : 'none'

  const flrigPort = document.getElementById('rp-flrig-port') as HTMLInputElement | null
  const tciPort = document.getElementById('rp-tci-port') as HTMLInputElement | null
  if (iface === 'flrig' && flrigPort && (!flrigPort.value || (!rigEditingProfileId && flrigPort.value === '4532'))) {
    flrigPort.value = '12345'
  }
  if (iface === 'tci' && tciPort && (!tciPort.value || (!rigEditingProfileId && tciPort.value === '4532'))) {
    tciPort.value = '50001'
  }
}

/** Update the "Use This Profile" button state. */
export function rigProfileUpdateActivateButton(): void {
  const btn = document.getElementById('rp-activate-btn') as HTMLButtonElement | null
  if (!btn) return

  if (!rigEditingProfileId) {
    btn.disabled = true
    btn.textContent = 'Save Before Activating'
    return
  }

  const isActive = rigEditingProfileId === rigActiveProfileId
  btn.disabled = isActive
  btn.textContent = isActive ? 'Active Profile' : 'Use This Profile'
}

/** Load the list of rig models from the backend for the dropdown. */
export async function rigProfileLoadModels(): Promise<void> {
  try {
    rigModels = await invoke<RigModel[]>('get_rig_models')
    const sel = document.getElementById('rp-rig-model') as HTMLSelectElement | null
    if (!sel) return
    sel.innerHTML = '<option value="">Select rig…</option>'
    for (const m of rigModels) {
      const opt = document.createElement('option')
      opt.value = String(m.id)
      opt.textContent = `[${m.id}] ${m.manufacturer} ${m.name}`
      sel.appendChild(opt)
    }
  } catch {
    // rigctl not available — show empty list
  }
}

/** Refresh the serial port dropdown from the backend. */
export async function rigProfileRefreshPorts(): Promise<void> {
  try {
    const ports = await invoke<SerialPortInfo[]>('list_serial_ports')
    const sel = document.getElementById('rp-serial-port') as HTMLSelectElement | null
    if (!sel) return
    const current = sel.value
    sel.innerHTML = '<option value="">Select port…</option>'
    for (const p of ports) {
      const opt = document.createElement('option')
      opt.value = p.port_name
      opt.textContent = p.description ? `${p.port_name} (${p.description})` : p.port_name
      sel.appendChild(opt)
    }
    if (current) sel.value = current
  } catch (err) {
    log(`Serial port refresh: ${formatError(err)}`)
  }
}

/** Populate the rig profile form from an existing profile. */
export function rigProfileFormLoad(profile: RigProfile): void {
  const form = document.getElementById('rig-profile-form')
  const empty = document.getElementById('rig-profile-empty')
  if (form) form.style.display = ''
  if (empty) empty.style.display = 'none'

  const setVal = (id: string, val: string) => {
    const el = document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null
    if (el) el.value = val
  }

  setVal('rp-name', profile.name)
  setVal('rp-interface', profile.interface_type)
  setVal('rp-rig-model', String(profile.rig_model_id))
  setVal('rp-serial-port', profile.serial_port)
  setVal('rp-baud-rate', String(profile.baud_rate))
  setVal('rp-data-bits', String(profile.data_bits))
  setVal('rp-stop-bits', String(profile.stop_bits))
  setVal('rp-flow-control', profile.flow_control)
  setVal('rp-parity', profile.parity)
  setVal('rp-civ-address', profile.civ_address || '')
  setVal('rp-ptt-type', profile.ptt_type)
  setVal('rp-flrig-host', profile.host)
  setVal('rp-flrig-port', String(profile.port))
  setVal('rp-ext-host', profile.host)
  setVal('rp-ext-port', String(profile.port))
  setVal('rp-tci-host', profile.host)
  setVal('rp-tci-port', String(profile.port || 50001))
  setVal('rp-tci-receiver', String(profile.tci_receiver ?? 0))
  setVal('rp-poll-interval', String(profile.poll_interval_ms))

  rigProfileInterfaceChanged()
  rigProfileUpdateActivateButton()

  // Populate serial port dropdown if needed.
  void rigProfileRefreshPorts().then(() => {
    const sel = document.getElementById('rp-serial-port') as HTMLSelectElement | null
    if (sel && profile.serial_port) sel.value = profile.serial_port
  })

  const testResult = document.getElementById('rp-test-result')
  if (testResult) { testResult.style.display = 'none'; testResult.textContent = '' }
}

/** Reset the form to a blank new profile. */
export function rigProfileNew(): void {
  rigEditingProfileId = null
  const blankProfile: RigProfile = {
    id: '',
    name: 'New Profile',
    interface_type: 'hamlib',
    rig_model_id: 1,
    rig_model_name: '',
    serial_port: '',
    baud_rate: 9600,
    data_bits: 8,
    stop_bits: 1,
    flow_control: 'none',
    parity: 'none',
    civ_address: null,
    ptt_type: 'none',
    host: '127.0.0.1',
    port: 4532,
    poll_interval_ms: 1000,
  }
  rigProfileFormLoad(blankProfile)
}

/** Read the current form values into a RigProfile object. */
export function rigProfileReadForm(): RigProfile {
  const getVal = (id: string): string => {
    const el = document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null
    return el?.value?.trim() || ''
  }

  const iface = getVal('rp-interface') as RigProfile['interface_type']
  const isFlrig = iface === 'flrig'
  const isExternal = iface === 'external'
  const isTci = iface === 'tci'

  const host = isFlrig ? getVal('rp-flrig-host') || '127.0.0.1'
    : isExternal ? getVal('rp-ext-host') || '127.0.0.1'
    : isTci ? getVal('rp-tci-host') || '127.0.0.1'
    : '127.0.0.1'
  const portStr = isFlrig ? getVal('rp-flrig-port')
    : isExternal ? getVal('rp-ext-port')
    : isTci ? getVal('rp-tci-port')
    : '4532'
  const port = Number.parseInt(portStr, 10) || (isFlrig ? 12345 : isTci ? 50001 : 4532)

  const civRaw = getVal('rp-civ-address')

  return {
    id: rigEditingProfileId || '',
    name: getVal('rp-name') || 'Profile',
    interface_type: iface,
    rig_model_id: Number.parseInt(getVal('rp-rig-model'), 10) || 1,
    rig_model_name: '',
    serial_port: getVal('rp-serial-port'),
    baud_rate: Number.parseInt(getVal('rp-baud-rate'), 10) || 9600,
    data_bits: Number.parseInt(getVal('rp-data-bits'), 10) || 8,
    stop_bits: Number.parseInt(getVal('rp-stop-bits'), 10) || 1,
    flow_control: getVal('rp-flow-control') as RigProfile['flow_control'],
    parity: getVal('rp-parity') as RigProfile['parity'],
    civ_address: civRaw || null,
    ptt_type: getVal('rp-ptt-type') as RigProfile['ptt_type'],
    host,
    port,
    tci_receiver: isTci ? (Number.parseInt(getVal('rp-tci-receiver'), 10) || 0) : undefined,
    poll_interval_ms: Number.parseInt(getVal('rp-poll-interval'), 10) || 1000,
  }
}

/** Save (create or update) the profile currently being edited. */
export async function rigProfileSave(): Promise<void> {
  const profile = rigProfileReadForm()

  try {
    if (rigEditingProfileId) {
      const updated: RigProfile = await invoke('update_rig_profile', { profile })
      rigProfiles = rigProfiles.map(p => p.id === updated.id ? updated : p)
      log(`Rig profile updated: ${updated.name}`)
    } else {
      const created: RigProfile = await invoke('create_rig_profile', { profile })
      rigProfiles.push(created)
      rigEditingProfileId = created.id
      log(`Rig profile created: ${created.name}`)
    }
    rigProfilesRender()
    rigProfileUpdateActivateButton()
  } catch (err) {
    log(`Failed to save rig profile: ${formatError(err)}`)
  }
}

/** Activate the profile currently being edited. */
export async function rigProfileActivate(): Promise<void> {
  if (!rigEditingProfileId) {
    log('Save the profile before activating it')
    return
  }

  try {
    await invoke('set_active_rig_profile', { profileId: rigEditingProfileId })
    rigActiveProfileId = rigEditingProfileId
    rigProfilesRender()
    rigProfileUpdateActivateButton()
    log('Rig profile activated')
    await onAfterActivate()
  } catch (err) {
    log(`Failed to activate rig profile: ${formatError(err)}`)
  }
}

/** Delete the profile currently being edited (after user confirmation). */
export async function rigProfileDelete(): Promise<void> {
  if (!rigEditingProfileId) return
  if (!window.confirm('Delete this rig profile?')) return

  try {
    await invoke('delete_rig_profile', { profileId: rigEditingProfileId })
    rigProfiles = rigProfiles.filter(p => p.id !== rigEditingProfileId)
    if (rigActiveProfileId === rigEditingProfileId) rigActiveProfileId = null
    rigEditingProfileId = null

    const form = document.getElementById('rig-profile-form')
    const empty = document.getElementById('rig-profile-empty')
    if (form) form.style.display = 'none'
    if (empty) empty.style.display = ''

    rigProfilesRender()
    log('Rig profile deleted')
  } catch (err) {
    log(`Failed to delete rig profile: ${formatError(err)}`)
  }
}

/** Test the connection for the profile currently being edited. */
export async function rigProfileTest(): Promise<void> {
  const profile = rigProfileReadForm()
  const resultEl = document.getElementById('rp-test-result')
  if (!resultEl) return

  resultEl.textContent = 'Testing…'
  resultEl.className = 'hint'
  resultEl.style.display = ''

  try {
    const result: TestConnectionResult = await invoke('test_rig_connection', { profile })
    resultEl.textContent = result.message
      + (result.frequency_hz ? ` — ${result.frequency_hz} Hz` : '')
      + (result.mode ? `, ${result.mode}` : '')
    resultEl.className = result.success ? 'hint success' : 'hint error'
  } catch (err) {
    resultEl.textContent = `Error: ${formatError(err)}`
    resultEl.className = 'hint error'
  }
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// formatError → ui-helpers.ts