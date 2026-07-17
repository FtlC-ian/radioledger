/**
 * Unit tests for app-shell.ts
 *
 * Tests the extracted tab management, refresh orchestration, settings loading,
 * and event wiring functions. Domain modules are mocked to isolate the shell
 * layer.
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// ─── Mock setup ───────────────────────────────────────────────────────────────
// Mock all domain modules so app-shell tests only exercise shell logic.

const mockRefreshRig = vi.fn().mockResolvedValue(undefined)
const mockPromptQsy = vi.fn()
const mockInitRigControlEvents = vi.fn()

vi.mock('../../src/rig-control', () => ({
  refreshRig: (...a: unknown[]) => mockRefreshRig(...a),
  promptQsy: (...a: unknown[]) => mockPromptQsy(...a),
  initRigControlEvents: (...a: unknown[]) => mockInitRigControlEvents(...a),
}))

const mockRefreshStats = vi.fn().mockResolvedValue(undefined)
const mockIsStatsLoaded = vi.fn().mockReturnValue(false)

vi.mock('../../src/stats-dashboard', () => ({
  refreshStats: (...a: unknown[]) => mockRefreshStats(...a),
  isStatsLoaded: (...a: unknown[]) => mockIsStatsLoaded(...a),
  resetStatsCache: vi.fn(),
  setLogger: vi.fn(),
}))

const mockRefreshAuth = vi.fn().mockResolvedValue(undefined)
const mockHandleLogin = vi.fn().mockResolvedValue(undefined)
const mockHandleLogout = vi.fn().mockResolvedValue(undefined)
const mockLoadAuthSettingsValues = vi.fn().mockResolvedValue(undefined)
const mockWireSettingsAuthListeners = vi.fn()

vi.mock('../../src/settings-auth', () => ({
  refreshAuth: (...a: unknown[]) => mockRefreshAuth(...a),
  handleLogin: (...a: unknown[]) => mockHandleLogin(...a),
  handleLogout: (...a: unknown[]) => mockHandleLogout(...a),
  loadAuthSettingsValues: (...a: unknown[]) => mockLoadAuthSettingsValues(...a),
  wireSettingsAuthListeners: (...a: unknown[]) => mockWireSettingsAuthListeners(...a),
  setAuthMode: vi.fn(),
  setLogger: vi.fn(),
  setRefreshAll: vi.fn(),
  setResetStatsCache: vi.fn(),
  setSwitchToTab: vi.fn(),
  setRefreshStats: vi.fn(),
  initAuthMode: vi.fn(),
}))

const mockRefreshUdp = vi.fn().mockResolvedValue(undefined)
const mockLoadUdpSettingsValues = vi.fn().mockResolvedValue(undefined)
const mockWireUdpListenerListeners = vi.fn()

vi.mock('../../src/udp-listener', () => ({
  refreshUdp: (...a: unknown[]) => mockRefreshUdp(...a),
  loadUdpSettingsValues: (...a: unknown[]) => mockLoadUdpSettingsValues(...a),
  wireUdpListenerListeners: (...a: unknown[]) => mockWireUdpListenerListeners(...a),
  setLogger: vi.fn(),
}))

const mockRigProfilesLoad = vi.fn().mockResolvedValue(undefined)
const mockRigProfileInterfaceChanged = vi.fn()
const mockRigProfileNew = vi.fn().mockResolvedValue(undefined)
const mockRigProfileSave = vi.fn().mockResolvedValue(undefined)
const mockRigProfileActivate = vi.fn().mockResolvedValue(undefined)
const mockRigProfileDelete = vi.fn().mockResolvedValue(undefined)
const mockRigProfileTest = vi.fn().mockResolvedValue(undefined)
const mockRigProfileRefreshPorts = vi.fn().mockResolvedValue(undefined)

vi.mock('../../src/rig-profiles', () => ({
  rigProfilesLoad: (...a: unknown[]) => mockRigProfilesLoad(...a),
  rigProfileInterfaceChanged: (...a: unknown[]) => mockRigProfileInterfaceChanged(...a),
  rigProfileNew: (...a: unknown[]) => mockRigProfileNew(...a),
  rigProfileSave: (...a: unknown[]) => mockRigProfileSave(...a),
  rigProfileActivate: (...a: unknown[]) => mockRigProfileActivate(...a),
  rigProfileDelete: (...a: unknown[]) => mockRigProfileDelete(...a),
  rigProfileTest: (...a: unknown[]) => mockRigProfileTest(...a),
  rigProfileRefreshPorts: (...a: unknown[]) => mockRigProfileRefreshPorts(...a),
}))

const mockClearQsoForm = vi.fn()
const mockHandleLogQso = vi.fn().mockResolvedValue(undefined)
const mockFreqToBand = vi.fn().mockReturnValue('20m')
const mockApplyDefaultRst = vi.fn()
const mockLoadWsjtxDecodePanelSettings = vi.fn().mockResolvedValue(undefined)
const mockSaveWsjtxDecodePanelSettings = vi.fn().mockResolvedValue(undefined)
const mockRefreshWsjtxDecodes = vi.fn().mockResolvedValue(undefined)
const mockOnWsjtxDecodeListChanged = vi.fn()

vi.mock('../../src/qso-form', () => ({
  clearQsoForm: (...a: unknown[]) => mockClearQsoForm(...a),
  handleLogQso: (...a: unknown[]) => mockHandleLogQso(...a),
  freqToBand: (...a: unknown[]) => mockFreqToBand(...a),
  applyDefaultRst: (...a: unknown[]) => mockApplyDefaultRst(...a),
  loadWsjtxDecodePanelSettings: (...a: unknown[]) => mockLoadWsjtxDecodePanelSettings(...a),
  saveWsjtxDecodePanelSettings: (...a: unknown[]) => mockSaveWsjtxDecodePanelSettings(...a),
  refreshWsjtxDecodes: (...a: unknown[]) => mockRefreshWsjtxDecodes(...a),
  onWsjtxDecodeListChanged: (...a: unknown[]) => mockOnWsjtxDecodeListChanged(...a),
}))

const mockWireWizardListeners = vi.fn()

vi.mock('../../src/setup-wizard', () => ({
  wireWizardListeners: (...a: unknown[]) => mockWireWizardListeners(...a),
}))

const mockLoadLogQsos = vi.fn().mockResolvedValue(undefined)
const mockApplyLogFilters = vi.fn()
const mockClearLogFilters = vi.fn()
const mockToggleLogColumnMenu = vi.fn()
const mockLogPrevPage = vi.fn()
const mockLogNextPage = vi.fn()
const mockCloseLogEditModal = vi.fn()
const mockSaveLogEditQso = vi.fn().mockResolvedValue(undefined)
const mockHydrateLogEditQso = vi.fn().mockResolvedValue(undefined)
const mockIsLogColumnMenuOpen = vi.fn().mockReturnValue(false)

vi.mock('../../src/log-view', () => ({
  loadLogQsos: (...a: unknown[]) => mockLoadLogQsos(...a),
  applyLogFilters: (...a: unknown[]) => mockApplyLogFilters(...a),
  clearLogFilters: (...a: unknown[]) => mockClearLogFilters(...a),
  toggleLogColumnMenu: (...a: unknown[]) => mockToggleLogColumnMenu(...a),
  logPrevPage: (...a: unknown[]) => mockLogPrevPage(...a),
  logNextPage: (...a: unknown[]) => mockLogNextPage(...a),
  closeLogEditModal: (...a: unknown[]) => mockCloseLogEditModal(...a),
  saveLogEditQso: (...a: unknown[]) => mockSaveLogEditQso(...a),
  hydrateLogEditQso: (...a: unknown[]) => mockHydrateLogEditQso(...a),
  isLogColumnMenuOpen: (...a: unknown[]) => mockIsLogColumnMenuOpen(...a),
  closeLogColumnMenuIfOpen: vi.fn(),
}))

const mockRefreshSync = vi.fn().mockResolvedValue(undefined)
const mockSyncNow = vi.fn().mockResolvedValue(undefined)
const mockInitSyncStatusEvents = vi.fn()

vi.mock('../../src/sync-status', () => ({
  refreshSync: (...a: unknown[]) => mockRefreshSync(...a),
  syncNow: (...a: unknown[]) => mockSyncNow(...a),
  initSyncStatusEvents: (...a: unknown[]) => mockInitSyncStatusEvents(...a),
}))

const mockListen = vi.fn().mockResolvedValue(() => {})
vi.mock('@tauri-apps/api/event', () => ({
  listen: (...a: unknown[]) => mockListen(...a),
}))

// ─── Module under test ────────────────────────────────────────────────────────

let initTabs: typeof import('../../src/app-shell').initTabs
let switchToTab: typeof import('../../src/app-shell').switchToTab
let refreshAll: typeof import('../../src/app-shell').refreshAll
let loadSettingsValues: typeof import('../../src/app-shell').loadSettingsValues
let wireEventListeners: typeof import('../../src/app-shell').wireEventListeners
let setupEventListeners: typeof import('../../src/app-shell').setupEventListeners

beforeAll(async () => {
  const mod = await import('../../src/app-shell')
  initTabs = mod.initTabs
  switchToTab = mod.switchToTab
  refreshAll = mod.refreshAll
  loadSettingsValues = mod.loadSettingsValues
  wireEventListeners = mod.wireEventListeners
  setupEventListeners = mod.setupEventListeners
})

beforeEach(() => {
  vi.clearAllMocks()
})

// ─── DOM setup helper ─────────────────────────────────────────────────────────

function setupTabDom(): void {
  document.body.innerHTML = `
    <button class="tab-btn active" data-tab="shack">Shack</button>
    <button class="tab-btn" data-tab="settings">Settings</button>
    <button class="tab-btn" data-tab="log">Log</button>
    <button class="tab-btn" data-tab="statistics">Statistics</button>
    <div id="tab-content-shack" class="tab-content active">Shack content</div>
    <div id="tab-content-settings" class="tab-content">Settings content</div>
    <div id="tab-content-log" class="tab-content">Log content</div>
    <div id="tab-content-statistics" class="tab-content">Statistics content</div>
  `
}

// ─── initTabs ─────────────────────────────────────────────────────────────────

describe('initTabs', () => {
  it('registers click listeners on all tab buttons', () => {
    setupTabDom()
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    // Click the settings tab
    tabs[1].click()
    expect(tabs[1].classList.contains('active')).toBe(true)
    expect(tabs[0].classList.contains('active')).toBe(false)
  })

  it('switches active tab content on click', () => {
    setupTabDom()
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    tabs[2].click() // Log tab
    expect(document.getElementById('tab-content-log')?.classList.contains('active')).toBe(true)
    expect(document.getElementById('tab-content-shack')?.classList.contains('active')).toBe(false)
  })

  it('calls handleTabActivated for "settings" on settings tab click', () => {
    setupTabDom()
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    tabs[1].click() // Settings tab
    expect(mockLoadAuthSettingsValues).toHaveBeenCalled()
  })

  it('calls loadLogQsos when log tab is clicked', () => {
    setupTabDom()
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    tabs[2].click() // Log tab
    expect(mockLoadLogQsos).toHaveBeenCalled()
  })

  it('calls refreshStats when statistics tab is clicked (and stats not yet loaded)', () => {
    setupTabDom()
    mockIsStatsLoaded.mockReturnValue(false)
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    tabs[3].click() // Statistics tab
    expect(mockRefreshStats).toHaveBeenCalledWith(false)
  })

  it('does not call refreshStats when statistics tab is clicked and stats already loaded', () => {
    setupTabDom()
    mockIsStatsLoaded.mockReturnValue(true)
    initTabs()
    const tabs = document.querySelectorAll<HTMLButtonElement>('.tab-btn')
    tabs[3].click()
    expect(mockRefreshStats).not.toHaveBeenCalled()
  })
})

// ─── switchToTab ─────────────────────────────────────────────────────────────

describe('switchToTab', () => {
  it('programmatically activates the specified tab', () => {
    setupTabDom()
    switchToTab('settings')
    expect(document.querySelector('.tab-btn[data-tab="settings"]')?.classList.contains('active')).toBe(true)
    expect(document.getElementById('tab-content-settings')?.classList.contains('active')).toBe(true)
  })

  it('deactivates other tabs', () => {
    setupTabDom()
    switchToTab('log')
    expect(document.querySelector('.tab-btn[data-tab="shack"]')?.classList.contains('active')).toBe(false)
    expect(document.getElementById('tab-content-shack')?.classList.contains('active')).toBe(false)
  })

  it('calls handleTabActivated for the target tab', () => {
    setupTabDom()
    switchToTab('settings')
    expect(mockLoadAuthSettingsValues).toHaveBeenCalled()
  })
})

// ─── refreshAll ───────────────────────────────────────────────────────────────

describe('refreshAll', () => {
  it('calls all refresh functions in parallel', async () => {
    await refreshAll()
    expect(mockRefreshAuth).toHaveBeenCalled()
    expect(mockRefreshUdp).toHaveBeenCalled()
    expect(mockRefreshRig).toHaveBeenCalled()
    expect(mockRefreshSync).toHaveBeenCalled()
    expect(mockRefreshWsjtxDecodes).toHaveBeenCalled()
  })

  it('calls loadLogQsos after all refreshes complete', async () => {
    await refreshAll()
    expect(mockLoadLogQsos).toHaveBeenCalled()
  })

  it('still calls loadLogQsos even if some refreshes fail', async () => {
    mockRefreshAuth.mockRejectedValueOnce(new Error('fail'))
    await refreshAll()
    expect(mockLoadLogQsos).toHaveBeenCalled()
  })
})

// ─── loadSettingsValues ───────────────────────────────────────────────────────

describe('loadSettingsValues', () => {
  it('loads auth settings, UDP settings, decode panel settings, and rig profiles', async () => {
    await loadSettingsValues()
    expect(mockLoadAuthSettingsValues).toHaveBeenCalled()
    expect(mockLoadUdpSettingsValues).toHaveBeenCalled()
    expect(mockLoadWsjtxDecodePanelSettings).toHaveBeenCalled()
    expect(mockRigProfilesLoad).toHaveBeenCalled()
  })
})

// ─── setupEventListeners ─────────────────────────────────────────────────────

describe('setupEventListeners', () => {
  it('initializes rig control events', () => {
    setupEventListeners()
    expect(mockInitRigControlEvents).toHaveBeenCalled()
  })

  it('initializes sync status events', () => {
    setupEventListeners()
    expect(mockInitSyncStatusEvents).toHaveBeenCalled()
  })

  it('subscribes to wsjtx-decode-list-changed event', () => {
    setupEventListeners()
    expect(mockListen).toHaveBeenCalledWith(
      'wsjtx-decode-list-changed',
      expect.any(Function)
    )
  })
})

// ─── wireEventListeners ──────────────────────────────────────────────────────

describe('wireEventListeners', () => {
  function setupFullDom(): void {
    document.body.innerHTML = `
      <button id="qso-log-btn">Log QSO</button>
      <button id="qso-clear-btn">Clear</button>
      <input id="qso-frequency" type="text" value="14.1">
      <select id="qso-band"><option value="20m">20m</option></select>
      <select id="qso-mode"><option value="SSB">SSB</option></select>
      <button id="login-btn">Login</button>
      <button id="logout-btn">Logout</button>
      <button id="rig-frequency-display">14.100</button>
      <button id="rig-refresh-btn">Refresh</button>
      <button id="sync-now-btn">Sync</button>
      <button id="stats-refresh-btn">Stats</button>
      <button id="rig-profile-add-btn">Add Profile</button>
      <button id="rp-save-btn">Save</button>
      <button id="rp-activate-btn">Activate</button>
      <button id="rp-delete-btn">Delete</button>
      <button id="rp-test-btn">Test</button>
      <button id="rp-refresh-ports-btn">Refresh Ports</button>
      <select id="rp-interface"></select>
      <input id="settings-wsjtx-decode-panel-enabled" type="checkbox">
      <button id="log-refresh-btn">Refresh Log</button>
      <button id="log-apply-filters">Apply</button>
      <button id="log-clear-filters">Clear</button>
      <button id="log-columns-toggle">Columns</button>
      <button id="log-prev-page">Prev</button>
      <button id="log-next-page">Next</button>
      <button id="log-edit-close-btn">Close</button>
      <button id="log-edit-save-btn">Save</button>
      <button id="log-edit-hydrate-btn">Hydrate</button>
      <div id="log-edit-overlay"></div>
      <div id="log-column-controls"></div>
    `
  }

  it('wires wizard and auth and UDP listeners', () => {
    setupFullDom()
    wireEventListeners()
    expect(mockWireWizardListeners).toHaveBeenCalled()
    expect(mockWireSettingsAuthListeners).toHaveBeenCalled()
    expect(mockWireUdpListenerListeners).toHaveBeenCalled()
  })

  it('wires QSO log button click to handleLogQso', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('qso-log-btn')!.click()
    expect(mockHandleLogQso).toHaveBeenCalled()
  })

  it('wires clear QSO form button', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('qso-clear-btn')!.click()
    expect(mockClearQsoForm).toHaveBeenCalled()
  })

  it('wires login/logout buttons', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('login-btn')!.click()
    expect(mockHandleLogin).toHaveBeenCalled()
    document.getElementById('logout-btn')!.click()
    expect(mockHandleLogout).toHaveBeenCalled()
  })

  it('wires rig-refresh button to refreshAll', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('rig-refresh-btn')!.click()
    expect(mockRefreshAuth).toHaveBeenCalled()
  })

  it('wires sync-now button to syncNow', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('sync-now-btn')!.click()
    expect(mockSyncNow).toHaveBeenCalled()
  })

  it('wires stats-refresh button to refreshStats', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('stats-refresh-btn')!.click()
    expect(mockRefreshStats).toHaveBeenCalled()
  })

  it('wires rig profile buttons', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('rig-profile-add-btn')!.click()
    expect(mockRigProfileNew).toHaveBeenCalled()
    document.getElementById('rp-save-btn')!.click()
    expect(mockRigProfileSave).toHaveBeenCalled()
    document.getElementById('rp-activate-btn')!.click()
    expect(mockRigProfileActivate).toHaveBeenCalled()
    document.getElementById('rp-delete-btn')!.click()
    expect(mockRigProfileDelete).toHaveBeenCalled()
    document.getElementById('rp-test-btn')!.click()
    expect(mockRigProfileTest).toHaveBeenCalled()
    document.getElementById('rp-refresh-ports-btn')!.click()
    expect(mockRigProfileRefreshPorts).toHaveBeenCalled()
  })

  it('wires log view buttons', () => {
    setupFullDom()
    wireEventListeners()
    document.getElementById('log-refresh-btn')!.click()
    expect(mockLoadLogQsos).toHaveBeenCalled()
    document.getElementById('log-apply-filters')!.click()
    expect(mockApplyLogFilters).toHaveBeenCalled()
    document.getElementById('log-clear-filters')!.click()
    expect(mockClearLogFilters).toHaveBeenCalled()
  })
})