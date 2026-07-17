/**
 * App Shell — tab management, refresh orchestration, settings loading,
 * and DOM/Tauri event wiring.
 *
 * Extracted from main.ts as part of the desktop decomposition (issue #194).
 * This module owns the top-level coordination seam: which tab is active,
 * what happens on tab switch, how a full refresh is sequenced, and how
 * button clicks and Tauri events are bound to domain module functions.
 *
 * Domain logic (rig, sync, auth, UDP, QSO, log) lives in their own modules.
 * App-shell just wires them together and orchestrates the UI shell.
 */

import { listen } from '@tauri-apps/api/event'
import {
  refreshRig,
  promptQsy,
  initRigControlEvents,
} from './rig-control'
import { refreshStats, isStatsLoaded } from './stats-dashboard'
import {
  refreshAuth,
  handleLogin,
  handleLogout,
  loadAuthSettingsValues,
  wireSettingsAuthListeners,
} from './settings-auth'
import {
  refreshUdp,
  loadUdpSettingsValues,
  wireUdpListenerListeners,
} from './udp-listener'
import {
  rigProfilesLoad,
  rigProfileInterfaceChanged,
  rigProfileNew,
  rigProfileSave,
  rigProfileActivate,
  rigProfileDelete,
  rigProfileTest,
  rigProfileRefreshPorts,
} from './rig-profiles'
import {
  clearQsoForm,
  handleLogQso,
  freqToBand,
  applyDefaultRst,
  loadWsjtxDecodePanelSettings,
  saveWsjtxDecodePanelSettings,
  refreshWsjtxDecodes,
  onWsjtxDecodeListChanged,
} from './qso-form'
import { wireWizardListeners } from './setup-wizard'
import {
  loadLogQsos,
  applyLogFilters,
  clearLogFilters,
  toggleLogColumnMenu,
  logPrevPage,
  logNextPage,
  closeLogEditModal,
  saveLogEditQso,
  hydrateLogEditQso,
  isLogColumnMenuOpen,
  closeLogColumnMenuIfOpen,
} from './log-view'
import {
  refreshSync,
  syncNow,
  initSyncStatusEvents,
} from './sync-status'
import type { RecentDecode } from './qso-form'

// ─── Tab management ──────────────────────────────────────────────────────────

function handleTabActivated(tabName: string): void {
  if (tabName === 'settings') {
    void loadSettingsValues()
  }

  if (tabName === 'log') {
    void loadLogQsos()
  }

  if (tabName === 'statistics' && !isStatsLoaded()) {
    void refreshStats(false)
  }
}

/** Programmatically switch to a named tab (e.g. 'settings'). */
export function switchToTab(tabName: string): void {
  const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
  tabs.forEach(t => {
    t.classList.remove('active')
    t.setAttribute('aria-selected', 'false')
  })
  document.querySelectorAll<HTMLDivElement>('.tab-content').forEach(c => c.classList.remove('active'))

  const targetTab = document.querySelector<HTMLButtonElement>(`.tab-btn[data-tab="${tabName}"]`)
  if (targetTab) {
    targetTab.classList.add('active')
    targetTab.setAttribute('aria-selected', 'true')
  }
  document.getElementById(`tab-content-${tabName}`)?.classList.add('active')

  handleTabActivated(tabName)
}

/** Register click handlers on all .tab-btn elements. */
export function initTabs(): void {
  const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
  tabs.forEach(tab => {
    tab.addEventListener('click', () => {
      const tabName = tab.dataset.tab
      if (!tabName) return

      tabs.forEach(t => {
        t.classList.remove('active')
        t.setAttribute('aria-selected', 'false')
      })
      document.querySelectorAll<HTMLDivElement>('.tab-content').forEach(c => c.classList.remove('active'))

      tab.classList.add('active')
      tab.setAttribute('aria-selected', 'true')
      document.getElementById(`tab-content-${tabName}`)?.classList.add('active')

      handleTabActivated(tabName)
    })
  })
}

// ─── Refresh orchestration ────────────────────────────────────────────────────

/** Refresh all dashboard panels in parallel, then preload the log. */
export async function refreshAll(): Promise<void> {
  await Promise.allSettled([refreshAuth(), refreshUdp(), refreshRig(), refreshSync(), refreshWsjtxDecodes()])
  void loadLogQsos() // Preload log data
}

// ─── Settings tab loading ─────────────────────────────────────────────────────

/**
 * Pre-populate settings fields from whatever the Rust layer knows.
 * Auth/server fields are loaded by settings-auth.ts; UDP fields by
 * udp-listener.ts; decode-panel and rig-profiles are loaded here.
 */
export async function loadSettingsValues(): Promise<void> {
  // Auth/server settings — lives in settings-auth.ts now
  await loadAuthSettingsValues()

  // UDP settings — lives in udp-listener.ts now
  await loadUdpSettingsValues()

  // Load WSJT-X decode panel settings (module lives in qso-form.ts now)
  await loadWsjtxDecodePanelSettings()

  // Load rig profiles
  await rigProfilesLoad()
}

// ─── DOM event wiring ────────────────────────────────────────────────────────

function bindClick(id: string, handler: () => void): void {
  const el = document.getElementById(id)
  if (el) el.addEventListener('click', handler)
}

/** Bind all DOM button clicks and input events to their handlers. */
export function wireEventListeners(): void {
  // QSO entry form (handlers live in qso-form.ts)
  bindClick('qso-log-btn', () => { void handleLogQso() })
  bindClick('qso-clear-btn', clearQsoForm)
  const freqInput = document.getElementById('qso-frequency') as HTMLInputElement | null
  if (freqInput) {
    freqInput.addEventListener('input', () => {
      const mhz = parseFloat(freqInput.value)
      if (!isNaN(mhz)) {
        const band = freqToBand(mhz)
        if (band) {
          (document.getElementById('qso-band') as HTMLSelectElement).value = band
        }
      }
    })
  }
  const modeInput = document.getElementById('qso-mode') as HTMLSelectElement | null
  if (modeInput) {
    modeInput.addEventListener('change', () => {
      applyDefaultRst(modeInput.value)
    })
  }

  // Main app buttons
  bindClick('login-btn', () => { void handleLogin() })
  bindClick('logout-btn', () => { void handleLogout() })
  bindClick('rig-frequency-display', () => { void promptQsy() })
  bindClick('rig-refresh-btn', () => { void refreshAll() })
  bindClick('sync-now-btn', () => { void syncNow() })
  bindClick('stats-refresh-btn', () => { void refreshStats() })
  // Rig profile buttons
  bindClick('rig-profile-add-btn', () => { void rigProfileNew() })
  bindClick('rp-save-btn', () => { void rigProfileSave() })
  bindClick('rp-activate-btn', () => { void rigProfileActivate() })
  bindClick('rp-delete-btn', () => { void rigProfileDelete() })
  bindClick('rp-test-btn', () => { void rigProfileTest() })
  bindClick('rp-refresh-ports-btn', () => { void rigProfileRefreshPorts() })
  const rpInterface = document.getElementById('rp-interface') as HTMLSelectElement | null
  if (rpInterface) rpInterface.addEventListener('change', () => rigProfileInterfaceChanged())

  const decodePanelCheck = document.getElementById('settings-wsjtx-decode-panel-enabled') as HTMLInputElement | null
  if (decodePanelCheck) {
    decodePanelCheck.addEventListener('change', () => {
      void saveWsjtxDecodePanelSettings(decodePanelCheck.checked)
    })
  }

  // Log view
  bindClick('log-refresh-btn', () => { void loadLogQsos() })
  bindClick('log-apply-filters', applyLogFilters)
  bindClick('log-clear-filters', clearLogFilters)
  bindClick('log-columns-toggle', toggleLogColumnMenu)
  bindClick('log-prev-page', logPrevPage)
  bindClick('log-next-page', logNextPage)
  bindClick('log-edit-close-btn', closeLogEditModal)
  bindClick('log-edit-save-btn', () => { void saveLogEditQso() })
  bindClick('log-edit-hydrate-btn', () => { void hydrateLogEditQso() })

  const logEditOverlay = document.getElementById('log-edit-overlay')
  if (logEditOverlay) {
    logEditOverlay.addEventListener('click', (event) => {
      if (event.target === logEditOverlay) closeLogEditModal()
    })
  }

  document.addEventListener('click', (event) => {
    const target = event.target as Node | null
    const controls = document.getElementById('log-column-controls')
    if (!isLogColumnMenuOpen() || !controls || (target && controls.contains(target))) return
    closeLogColumnMenuIfOpen()
  })

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      closeLogColumnMenuIfOpen()
    }
  })

  // Wizard listeners are wired by setup-wizard module
  wireWizardListeners()

  // Settings — auth mode toggle (wired by settings-auth module)
  wireSettingsAuthListeners()

  // UDP listener toggles and settings (wired by udp-listener module)
  wireUdpListenerListeners()
}

// ─── Tauri event listeners ──────────────────────────────────────────────────

/** Subscribe to Tauri (backend) events that drive UI updates. */
export function setupEventListeners(): void {
  initRigControlEvents()
  initSyncStatusEvents()

  void listen<RecentDecode[]>('wsjtx-decode-list-changed', (event) => {
    onWsjtxDecodeListChanged(event.payload)
  })
}