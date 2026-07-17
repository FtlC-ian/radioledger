/**
 * Setup Wizard — first-run configuration flow.
 *
 * Extracted from main.ts as part of the desktop decomposition
 * (issue #194).  Manages wizard state, step navigation, capture /
 * build-summary logic, auth toggle within wizard, login/sync/detect/
 * tqsl/rig test handlers, finish flow, and wizard status UI helpers.
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError } from './ui-helpers'

// ─── Wizard types ─────────────────────────────────────────────────────────────

interface WizardStatus {
  setup_complete: boolean
}

interface SoftwareDetection {
  detected: boolean
  port: number
  note: string
  installed: boolean
  binary_path: string | null
  install_hint: string | null
}

interface DetectedSoftware {
  wsjtx: SoftwareDetection
  js8call: SoftwareDetection
  n1mm: SoftwareDetection
  flrig: SoftwareDetection
  rigctld: SoftwareDetection
}

interface WizardConfig {
  auth_mode: 'cloud' | 'local'
  server_url: string | null
  wsjtx_enabled: boolean
  wsjtx_port: number
  js8call_enabled: boolean
  js8call_port: number
  n1mm_enabled: boolean
  n1mm_port: number
  lotw_tqsl_path: string | null
  lotw_station_location: string | null
  rig_preferred_method: string
  rig_host: string
  rig_flrig_port: number
  rig_rigctld_port: number
}

/** Tauri command response types used by wizard handlers. */
interface AuthStatus {
  logged_in: boolean
  callsign: string | null
}

interface SyncStatus {
  pending: number
  last_sync: string | null
  last_error: string | null
}

interface RigStatus {
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

// ─── Injected dependencies ────────────────────────────────────────────────────

let logger: (msg: string) => void = () => {}
let onWizardFinish: (authMode: 'cloud' | 'local') => void = () => {}

/** Inject a custom log function (same pattern as rig-profiles / stats-dashboard). */
export function setLogger(fn: (msg: string) => void): void {
  logger = fn
}

/**
 * Inject a callback that fires after the wizard finishes.
 * The callback receives the chosen auth mode so main.ts can update
 * its module-level state (e.g. currentAuthMode) and refresh.
 */
export function setOnWizardFinish(fn: (authMode: 'cloud' | 'local') => void): void {
  onWizardFinish = fn
}

// ─── Wizard state ─────────────────────────────────────────────────────────────

let wizardCurrentStep = 0
const WIZARD_TOTAL_STEPS = 8

// Collected config across steps
let wizardCollected: WizardConfig = {
  auth_mode: 'cloud',
  server_url: null,
  wsjtx_enabled: true,
  wsjtx_port: 2237,
  js8call_enabled: false,
  js8call_port: 2242,
  n1mm_enabled: false,
  n1mm_port: 12060,
  lotw_tqsl_path: null,
  lotw_station_location: null,
  rig_preferred_method: 'none',
  rig_host: '127.0.0.1',
  rig_flrig_port: 12345,
  rig_rigctld_port: 4532,
}

let wizardDetected: DetectedSoftware | null = null
let wizardLoginDone = false
let wizardSyncDone = false
let wizardLotwFound = false
let wizardRigConnected = false

// ─── Wizard init ──────────────────────────────────────────────────────────────

/**
 * Check wizard status on load; show overlay if setup is incomplete.
 */
export async function initWizard(): Promise<void> {
  try {
    const status: WizardStatus = await invoke('get_wizard_status')
    if (!status.setup_complete) {
      showWizard()
    }
  } catch (_err) {
    // Command not available (e.g. dev mode) — don't block the UI
  }
}

function showWizard(): void {
  const overlay = document.getElementById('wizard-overlay')
  if (overlay) {
    overlay.style.display = 'flex'
    wizardGoTo(0)
  }
}

function hideWizard(): void {
  const overlay = document.getElementById('wizard-overlay')
  if (overlay) {
    overlay.style.display = 'none'
  }
}

// ─── Navigation ───────────────────────────────────────────────────────────────

function wizardGoTo(step: number): void {
  if (step < 0 || step >= WIZARD_TOTAL_STEPS) return

  // Hide current
  const currentPanel = document.getElementById(`wizard-step-${wizardCurrentStep}`)
  currentPanel?.classList.remove('active')

  // Update dots
  const allDots = document.querySelectorAll<HTMLElement>('.wizard-step-dot')
  allDots.forEach((dot, i) => {
    dot.classList.remove('active', 'done')
    if (i < step) dot.classList.add('done')
    else if (i === step) dot.classList.add('active')
  })

  // Update progress aria
  const progress = document.getElementById('wizard-progress')
  if (progress) {
    progress.setAttribute('aria-valuenow', String(step + 1))
  }

  // Show next
  wizardCurrentStep = step
  const nextPanel = document.getElementById(`wizard-step-${step}`)
  nextPanel?.classList.add('active')

  // Step-specific setup
  if (step === 4) wizardPreFillUdp()
  if (step === 7) wizardBuildSummary()
}

export function wizardNext(): void {
  // Capture step-specific inputs before leaving
  wizardCaptureCurrentStep()
  wizardGoTo(wizardCurrentStep + 1)
}

export function wizardBack(): void {
  wizardGoTo(wizardCurrentStep - 1)
}

/** Capture form values from step before advancing. */
function wizardCaptureCurrentStep(): void {
  switch (wizardCurrentStep) {
    case 4: {
      const wsjtxEnabled = document.getElementById('wiz-wsjtx-enabled') as HTMLInputElement | null
      const wsjtxPort = document.getElementById('wiz-wsjtx-port') as HTMLInputElement | null
      const js8callEnabled = document.getElementById('wiz-js8call-enabled') as HTMLInputElement | null
      const js8callPort = document.getElementById('wiz-js8call-port') as HTMLInputElement | null
      const n1mmEnabled = document.getElementById('wiz-n1mm-enabled') as HTMLInputElement | null
      const n1mmPort = document.getElementById('wiz-n1mm-port') as HTMLInputElement | null

      if (wsjtxEnabled) wizardCollected.wsjtx_enabled = wsjtxEnabled.checked
      if (wsjtxPort) wizardCollected.wsjtx_port = parseInt(wsjtxPort.value, 10) || 2237
      if (js8callEnabled) wizardCollected.js8call_enabled = js8callEnabled.checked
      if (js8callPort) wizardCollected.js8call_port = parseInt(js8callPort.value, 10) || 2242
      if (n1mmEnabled) wizardCollected.n1mm_enabled = n1mmEnabled.checked
      if (n1mmPort) wizardCollected.n1mm_port = parseInt(n1mmPort.value, 10) || 12060
      break
    }
    case 5: {
      const lotwPath = document.getElementById('wiz-lotw-path') as HTMLInputElement | null
      const lotwLocation = document.getElementById('wiz-lotw-location') as HTMLInputElement | null
      wizardCollected.lotw_tqsl_path = lotwPath?.value.trim() || null
      wizardCollected.lotw_station_location = lotwLocation?.value.trim() || null
      break
    }
    case 6: {
      const rigMethod = document.getElementById('wiz-rig-method') as HTMLSelectElement | null
      const rigHost = document.getElementById('wiz-rig-host') as HTMLInputElement | null
      const flrigPort = document.getElementById('wiz-rig-flrig-port') as HTMLInputElement | null
      const rigctldPort = document.getElementById('wiz-rig-rigctld-port') as HTMLInputElement | null

      if (rigMethod) wizardCollected.rig_preferred_method = rigMethod.value
      if (rigHost) wizardCollected.rig_host = rigHost.value.trim() || '127.0.0.1'
      if (flrigPort) wizardCollected.rig_flrig_port = parseInt(flrigPort.value, 10) || 12345
      if (rigctldPort) wizardCollected.rig_rigctld_port = parseInt(rigctldPort.value, 10) || 4532
      break
    }
  }
}

// ─── Step actions ─────────────────────────────────────────────────────────────

/** Step 1: Toggle between Cloud and Self-hosted auth panels */
export function wizardToggleAuthMode(mode: 'cloud' | 'local'): void {
  const cloudBtn = document.getElementById('wizard-auth-cloud-btn')!
  const localBtn = document.getElementById('wizard-auth-local-btn')!
  const cloudPanel = document.getElementById('wizard-auth-cloud')!
  const localPanel = document.getElementById('wizard-auth-local')!

  wizardCollected.auth_mode = mode

  if (mode === 'cloud') {
    cloudBtn.classList.add('active')
    localBtn.classList.remove('active')
    cloudPanel.style.display = ''
    localPanel.style.display = 'none'
  } else {
    localBtn.classList.add('active')
    cloudBtn.classList.remove('active')
    cloudPanel.style.display = 'none'
    localPanel.style.display = ''
  }
}

/** Step 1 (local mode): Email/password login against a self-hosted server */
async function wizardDoLocalLogin(): Promise<void> {
  const serverUrl = (document.getElementById('wizard-server-url') as HTMLInputElement).value.trim()
  const email = (document.getElementById('wizard-local-email') as HTMLInputElement).value.trim()
  const password = (document.getElementById('wizard-local-password') as HTMLInputElement).value
  const statusEl = document.getElementById('wizard-local-login-status')!

  if (!serverUrl || !email || !password) {
    statusEl.textContent = 'Please fill in all fields'
    return
  }

  statusEl.textContent = 'Connecting…'

  try {
    const result: AuthStatus = await invoke('login_local', {
      request: { server_url: serverUrl, email, password },
    })

    statusEl.textContent = `Connected as ${result.callsign || email} ✓`
    wizardLoginDone = true
    // Persist server URL so it gets saved to config
    wizardCollected.server_url = serverUrl
    wizardCollected.auth_mode = 'local'
    // Change skip to "Next →" now that login succeeded
    const skipBtn = document.getElementById('wizard-step1-local-skip')
    if (skipBtn) skipBtn.textContent = 'Next →'
  } catch (err) {
    statusEl.textContent = `Login failed: ${formatError(err)}`
  }
}

/** Step 1: Login */
async function wizardDoLogin(): Promise<void> {
  const btn = document.getElementById('wizard-login-btn') as HTMLButtonElement | null
  const indicator = document.getElementById('wizard-login-indicator')
  const text = document.getElementById('wizard-login-text')
  const userBox = document.getElementById('wizard-login-user')
  const callsignEl = document.getElementById('wizard-login-callsign')

  if (btn) btn.disabled = true
  wizardSetStatus(indicator, text, 'working', 'Opening browser for login…')

  try {
    const status: AuthStatus = await invoke('login')
    wizardLoginDone = true
    wizardSetStatus(indicator, text, 'ok', `Signed in as ${status.callsign ?? 'unknown'} ✓`)
    if (userBox) userBox.style.display = 'flex'
    if (callsignEl) callsignEl.textContent = status.callsign ?? '—'
    if (btn) btn.textContent = 'Sign in again'
    // Change skip to "Next →" now that login succeeded
    const skipBtn = document.getElementById('wizard-step1-skip')
    if (skipBtn) skipBtn.textContent = 'Next →'
  } catch (err) {
    wizardSetStatus(indicator, text, 'error', `Login failed: ${formatError(err)}`)
  } finally {
    if (btn) btn.disabled = false
  }
}

/** Step 2: Server sync */
async function wizardDoSync(): Promise<void> {
  const btn = document.getElementById('wizard-sync-btn') as HTMLButtonElement | null
  const indicator = document.getElementById('wizard-sync-indicator')
  const text = document.getElementById('wizard-sync-text')

  if (btn) btn.disabled = true
  wizardSetStatus(indicator, text, 'working', 'Syncing logbook from server…')

  try {
    const status: SyncStatus = await invoke('sync_now')
    wizardSyncDone = true
    wizardSetStatus(indicator, text, 'ok', `Sync complete — ${status.pending} pending QSOs ✓`)
    // Change skip to "Next →" now that sync succeeded
    const skipBtn = document.getElementById('wizard-step2-skip')
    if (skipBtn) skipBtn.textContent = 'Next →'
  } catch (err) {
    wizardSetStatus(indicator, text, 'error', `Sync failed: ${formatError(err)}`)
  } finally {
    if (btn) btn.disabled = false
  }
}

/** Step 3: Detect software */
async function wizardDoDetect(): Promise<void> {
  const btn = document.getElementById('wizard-scan-btn') as HTMLButtonElement | null
  const indicator = document.getElementById('wizard-detect-indicator')
  const text = document.getElementById('wizard-detect-text')
  const results = document.getElementById('wizard-detect-results')

  if (btn) btn.disabled = true
  wizardSetStatus(indicator, text, 'working', 'Scanning ports…')

  try {
    const detected: DetectedSoftware = await invoke('detect_software')
    wizardDetected = detected

    wizardSetStatus(indicator, text, 'ok', 'Scan complete ✓')
    if (results) results.style.display = 'block'

    // Update running-state icons for UDP/TCP-probed entries (wsjtx, js8call, n1mm, flrig).
    const runningEntries: Array<{ key: Exclude<keyof DetectedSoftware, 'rigctld'>; iconId: string; noteId: string }> = [
      { key: 'wsjtx', iconId: 'wizard-detect-wsjtx-icon', noteId: 'wizard-detect-wsjtx-note' },
      { key: 'js8call', iconId: 'wizard-detect-js8call-icon', noteId: 'wizard-detect-js8call-note' },
      { key: 'n1mm', iconId: 'wizard-detect-n1mm-icon', noteId: 'wizard-detect-n1mm-note' },
      { key: 'flrig', iconId: 'wizard-detect-flrig-icon', noteId: 'wizard-detect-flrig-note' },
    ]
    for (const { key, iconId, noteId } of runningEntries) {
      const det = detected[key]
      const iconEl = document.getElementById(iconId)
      const noteEl = document.getElementById(noteId)
      if (iconEl) iconEl.textContent = det.detected ? '✅' : '⬜'
      if (noteEl) noteEl.textContent = det.note
    }

    // rigctld: show ✅ if installed (binary found), ⬜ if not — separate from running check.
    const rc = detected.rigctld
    const rcIcon = document.getElementById('wizard-detect-rigctld-icon')
    const rcNote = document.getElementById('wizard-detect-rigctld-note')
    const rcInstallBox = document.getElementById('wizard-detect-rigctld-install')
    const rcHint = document.getElementById('wizard-detect-rigctld-hint')

    if (rcIcon) {
      if (rc.installed) {
        rcIcon.textContent = '✅'
      } else {
        rcIcon.textContent = '⬜'
      }
    }
    if (rcNote) {
      rcNote.textContent = rc.installed
        ? `Installed${rc.binary_path ? ` — ${rc.binary_path}` : ''}${rc.detected ? ' (running)' : ''}`
        : rc.note
    }
    // Show install guidance when rigctld binary is not found.
    if (rcInstallBox) {
      rcInstallBox.style.display = rc.installed ? 'none' : 'block'
    }
    if (rcHint && rc.install_hint) {
      rcHint.textContent = rc.install_hint
    }

    // Pre-fill rig detection info
    if (detected.flrig.detected) {
      wizardCollected.rig_preferred_method = 'flrig'
      wizardCollected.rig_flrig_port = detected.flrig.port
      wizardRigConnected = true
    } else if (detected.rigctld.detected) {
      wizardCollected.rig_preferred_method = 'hamlib'
      wizardCollected.rig_rigctld_port = detected.rigctld.port
      wizardRigConnected = true
    }
  } catch (err) {
    wizardSetStatus(indicator, text, 'error', `Scan failed: ${formatError(err)}`)
  } finally {
    if (btn) btn.disabled = false
  }
}

/** Step 4: Pre-fill UDP config from detection results */
function wizardPreFillUdp(): void {
  if (!wizardDetected) return

  const wsjtxEnabled = document.getElementById('wiz-wsjtx-enabled') as HTMLInputElement | null
  const js8callEnabled = document.getElementById('wiz-js8call-enabled') as HTMLInputElement | null
  const n1mmEnabled = document.getElementById('wiz-n1mm-enabled') as HTMLInputElement | null

  if (wsjtxEnabled) wsjtxEnabled.checked = wizardDetected.wsjtx.detected
  if (js8callEnabled) js8callEnabled.checked = wizardDetected.js8call.detected
  if (n1mmEnabled) n1mmEnabled.checked = wizardDetected.n1mm.detected
}

/** Step 5: Detect tQSL */
async function wizardDetectTqsl(): Promise<void> {
  const btn = document.getElementById('wizard-lotw-detect-btn') as HTMLButtonElement | null
  const indicator = document.getElementById('wizard-lotw-indicator')
  const text = document.getElementById('wizard-lotw-text')
  const form = document.getElementById('wizard-lotw-form')
  const pathInput = document.getElementById('wiz-lotw-path') as HTMLInputElement | null

  if (btn) btn.disabled = true
  wizardSetStatus(indicator, text, 'working', 'Searching for tQSL installation…')

  try {
    const detection = await invoke('detect_tqsl') as { found: boolean; path: string | null; candidates_checked: string[] }

    if (detection.found) {
      wizardLotwFound = true
      wizardCollected.lotw_tqsl_path = detection.path
      if (form) form.style.display = 'block'
      if (pathInput) pathInput.value = detection.path ?? ''
      wizardSetStatus(indicator, text, 'ok', `tQSL found at ${detection.path} ✓`)
      if (btn) btn.textContent = 'Re-detect'
    } else {
      wizardSetStatus(indicator, text, 'error',
        `tQSL not found. Checked: ${detection.candidates_checked.join(', ')}`)
    }
  } catch (err) {
    wizardSetStatus(indicator, text, 'error', `Detection failed: ${formatError(err)}`)
  } finally {
    if (btn) btn.disabled = false
  }
}

/** Step 6: Test rig connection */
async function wizardTestRig(): Promise<void> {
  const indicator = document.getElementById('wizard-rig-indicator')
  const text = document.getElementById('wizard-rig-text')

  wizardCaptureCurrentStep() // capture current form values
  wizardSetStatus(indicator, text, 'working', 'Testing rig connection…')

  try {
    const status: RigStatus = await invoke('connect_rig', {
      params: {
        backend: wizardCollected.rig_preferred_method === 'none' ? null : wizardCollected.rig_preferred_method,
        host: wizardCollected.rig_host,
        port: wizardCollected.rig_preferred_method === 'flrig'
          ? wizardCollected.rig_flrig_port
          : wizardCollected.rig_rigctld_port,
      },
    })
    if (status.connected) {
      wizardRigConnected = true
      const freq = status.frequency_display || '—'
      const mode = status.mode || '—'
      wizardSetStatus(indicator, text, 'ok', `Connected via ${status.backend} — ${freq} ${mode} ✓`)
    } else {
      wizardSetStatus(indicator, text, 'error', status.last_error ?? 'Rig not responding')
    }
  } catch (err) {
    wizardSetStatus(indicator, text, 'error', `Connection failed: ${formatError(err)}`)
  }
}

/** Step 7: Build summary */
function wizardBuildSummary(): void {
  const container = document.getElementById('wizard-summary')
  if (!container) return

  const items: Array<{ icon: string; label: string }> = []

  // Login / auth mode
  if (wizardLoginDone) {
    if (wizardCollected.auth_mode === 'local') {
      const callsign = (document.getElementById('wizard-local-login-status')?.textContent ?? '').trim()
      const serverUrl = wizardCollected.server_url || 'self-hosted server'
      items.push({ icon: '🏠', label: `Self-hosted login: ${callsign || serverUrl}` })
    } else {
      const callsign = (document.getElementById('wizard-login-callsign')?.textContent ?? '').trim()
      items.push({ icon: '🔐', label: `Logged in${callsign ? ` as ${callsign}` : ''}` })
    }
  } else {
    items.push({ icon: '⏭️', label: 'Login skipped — sign in from the Shack tab' })
  }

  // Sync
  items.push({ icon: wizardSyncDone ? '☁️' : '⏭️', label: wizardSyncDone ? 'Logbook synced from server' : 'Sync skipped' })

  // UDP
  const udpParts: string[] = []
  if (wizardCollected.wsjtx_enabled) udpParts.push(`WSJT-X :${wizardCollected.wsjtx_port}`)
  if (wizardCollected.js8call_enabled) udpParts.push(`JS8Call :${wizardCollected.js8call_port}`)
  if (wizardCollected.n1mm_enabled) udpParts.push(`N1MM+ :${wizardCollected.n1mm_port}`)
  if (udpParts.length > 0) {
    items.push({ icon: '📡', label: `UDP listeners: ${udpParts.join(', ')}` })
  } else {
    items.push({ icon: '⏭️', label: 'UDP listeners: none enabled' })
  }

  // LoTW
  if (wizardLotwFound) {
    const loc = wizardCollected.lotw_station_location || 'no station location set'
    items.push({ icon: '🏆', label: `LoTW: tQSL found — ${loc}` })
  } else {
    items.push({ icon: '⏭️', label: 'LoTW: skipped (configure later in Settings)' })
  }

  // Rig
  if (wizardRigConnected) {
    items.push({ icon: '🎛️', label: `Rig control: ${wizardCollected.rig_preferred_method} @ ${wizardCollected.rig_host}` })
  } else {
    items.push({ icon: '⏭️', label: 'Rig control: not connected (configure later)' })
  }

  container.innerHTML = items
    .map(i => `<div class="wizard-summary-item">
      <span class="wizard-summary-icon">${i.icon}</span>
      <span>${i.label}</span>
    </div>`)
    .join('')
}

/** Step 7: Finish */
async function wizardFinish(): Promise<void> {
  const btn = document.getElementById('wizard-finish-btn') as HTMLButtonElement | null
  if (btn) btn.disabled = true

  try {
    // Capture any final form changes
    wizardCaptureCurrentStep()

    // Capture server URL from whichever auth panel was active
    if (wizardCollected.auth_mode === 'local') {
      const serverUrlInput = document.getElementById('wizard-server-url') as HTMLInputElement | null
      const url = serverUrlInput?.value.trim()
      if (url) wizardCollected.server_url = url
    }

    // Save the collected config
    await invoke('save_wizard_config', { wizardConfig: wizardCollected })

    // Mark wizard as complete
    await invoke('complete_wizard')

    // Hide overlay
    hideWizard()
    logger('Setup wizard complete — welcome to RadioLedger!')

    // Notify main.ts to update auth mode and refresh
    onWizardFinish(wizardCollected.auth_mode)
  } catch (err) {
    logger(`Wizard finish error: ${formatError(err)}`)
    if (btn) btn.disabled = false
  }
}

// ─── Utility ──────────────────────────────────────────────────────────────────

type WizardDotClass = 'idle' | 'working' | 'ok' | 'error'

function wizardSetStatus(
  dotEl: HTMLElement | null,
  textEl: HTMLElement | null,
  state: WizardDotClass,
  message: string,
): void {
  if (dotEl) {
    dotEl.className = `wizard-status-dot wizard-dot-${state}`
  }
  if (textEl) {
    textEl.textContent = message
  }
}

// ─── DOM event wiring ────────────────────────────────────────────────────────

/**
 * Wire all wizard button click handlers.
 * Called from the shell event wiring so that bindClick stays in one place.
 */
export function wireWizardListeners(): void {
  const bindClick = (id: string, handler: () => void): void => {
    const el = document.getElementById(id)
    if (el) el.addEventListener('click', handler)
  }

  // Wizard — Step 0
  bindClick('wizard-start-btn', wizardNext)

  // Wizard — Step 1 (auth mode toggle)
  bindClick('wizard-auth-cloud-btn', () => wizardToggleAuthMode('cloud'))
  bindClick('wizard-auth-local-btn', () => wizardToggleAuthMode('local'))

  // Wizard — Step 1 (cloud panel)
  bindClick('wizard-step1-back', wizardBack)
  bindClick('wizard-login-btn', () => { void wizardDoLogin() })
  bindClick('wizard-step1-skip', wizardNext)

  // Wizard — Step 1 (local panel)
  bindClick('wizard-step1-local-back', wizardBack)
  bindClick('wizard-local-login-btn', () => { void wizardDoLocalLogin() })
  bindClick('wizard-step1-local-skip', wizardNext)

  // Wizard — Step 2
  bindClick('wizard-step2-back', wizardBack)
  bindClick('wizard-sync-btn', () => { void wizardDoSync() })
  bindClick('wizard-step2-skip', wizardNext)

  // Wizard — Step 3
  bindClick('wizard-step3-back', wizardBack)
  bindClick('wizard-scan-btn', () => { void wizardDoDetect() })
  bindClick('wizard-step3-next', wizardNext)

  // Wizard — Step 4
  bindClick('wizard-step4-back', wizardBack)
  bindClick('wizard-step4-next', wizardNext)
  bindClick('wizard-step4-skip', wizardNext)

  // Wizard — Step 5
  bindClick('wizard-step5-back', wizardBack)
  bindClick('wizard-lotw-detect-btn', () => { void wizardDetectTqsl() })
  bindClick('wizard-step5-skip', wizardNext)

  // Wizard — Step 6
  bindClick('wizard-step6-back', wizardBack)
  bindClick('wizard-rig-test-btn', () => { void wizardTestRig() })
  bindClick('wizard-step6-skip', wizardNext)

  // Wizard — Step 7
  bindClick('wizard-step7-back', wizardBack)
  bindClick('wizard-finish-btn', () => { void wizardFinish() })
}

// ─── Wire wizard init into DOMContentLoaded ──────────────────────────────────

/**
 * Attach wizard init to DOMContentLoaded.
 * Call this from the shell bootstrap after the main init listener is set up.
 */
export function attachWizardInit(): void {
  document.addEventListener('DOMContentLoaded', () => {
    void initWizard()
  })
}