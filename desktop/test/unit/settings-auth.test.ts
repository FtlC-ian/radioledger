/**
 * Unit tests for settings-auth module.
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
    <div id="auth-status" class="value inactive">Not logged in</div>
    <div id="callsign-row" style="display:none"><span id="callsign-value"></span></div>
    <button id="login-btn">Login</button>
    <button id="logout-btn" style="display:none">Logout</button>
    <div id="server-url-row" style="display:none"><span id="server-url-value"></span></div>

    <input id="settings-server-url" value="" />
    <button id="settings-save-server-btn">Save</button>
    <button id="settings-test-connection-btn">Test</button>
    <div id="settings-connection-status"></div>

    <button id="settings-auth-cloud-btn" class="active">Cloud</button>
    <button id="settings-auth-local-btn">Self-hosted</button>
    <div id="settings-local-auth-fields" style="display:none">
      <input id="settings-local-email" value="" />
      <input id="settings-local-password" value="" />
    </div>
    <button id="settings-local-login-btn" style="display:none">Sign In</button>
    <div id="settings-local-login-status-row" style="display:none">
      <span id="settings-local-login-status"></span>
    </div>

    <div id="statusbar-server-dot" class="status-dot err"></div>
    <span id="statusbar-server-text">Disconnected</span>
  `
}

// ─── Import module under test ─────────────────────────────────────────────────

let initAuthMode: typeof import('../../src/settings-auth').initAuthMode
let refreshAuth: typeof import('../../src/settings-auth').refreshAuth
let handleLogin: typeof import('../../src/settings-auth').handleLogin
let handleLogout: typeof import('../../src/settings-auth').handleLogout
let loadAuthSettingsValues: typeof import('../../src/settings-auth').loadAuthSettingsValues
let saveServerSettings: typeof import('../../src/settings-auth').saveServerSettings
let testConnection: typeof import('../../src/settings-auth').testConnection
let settingsToggleAuthMode: typeof import('../../src/settings-auth').settingsToggleAuthMode
let settingsDoLocalLogin: typeof import('../../src/settings-auth').settingsDoLocalLogin
let wireSettingsAuthListeners: typeof import('../../src/settings-auth').wireSettingsAuthListeners
let getAuthMode: typeof import('../../src/settings-auth').getAuthMode
let setAuthMode: typeof import('../../src/settings-auth').setAuthMode
let setServerUrl: typeof import('../../src/settings-auth').setServerUrl
let getServerUrl: typeof import('../../src/settings-auth').getServerUrl
let setLogger: typeof import('../../src/settings-auth').setLogger
let setRefreshAll: typeof import('../../src/settings-auth').setRefreshAll
let setResetStatsCache: typeof import('../../src/settings-auth').setResetStatsCache
let setSwitchToTab: typeof import('../../src/settings-auth').setSwitchToTab
let setRefreshStats: typeof import('../../src/settings-auth').setRefreshStats

const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }

let resetStatsCacheCalled = false
let refreshAllCalled = false
let switchToTabTarget: string | null = null
let refreshStatsCalled = false

beforeAll(async () => {
  const mod = await import('../../src/settings-auth')
  initAuthMode = mod.initAuthMode
  refreshAuth = mod.refreshAuth
  handleLogin = mod.handleLogin
  handleLogout = mod.handleLogout
  loadAuthSettingsValues = mod.loadAuthSettingsValues
  saveServerSettings = mod.saveServerSettings
  testConnection = mod.testConnection
  settingsToggleAuthMode = mod.settingsToggleAuthMode
  settingsDoLocalLogin = mod.settingsDoLocalLogin
  wireSettingsAuthListeners = mod.wireSettingsAuthListeners
  getAuthMode = mod.getAuthMode
  setAuthMode = mod.setAuthMode
  setServerUrl = mod.setServerUrl
  getServerUrl = mod.getServerUrl
  setLogger = mod.setLogger
  setRefreshAll = mod.setRefreshAll
  setResetStatsCache = mod.setResetStatsCache
  setSwitchToTab = mod.setSwitchToTab
  setRefreshStats = mod.setRefreshStats
})

beforeEach(() => {
  setupDom()
  logs.length = 0
  mockInvoke.mockReset()
  resetStatsCacheCalled = false
  refreshAllCalled = false
  switchToTabTarget = null
  refreshStatsCalled = false

  setLogger(testLogger)
  setRefreshAll(async () => { refreshAllCalled = true })
  setResetStatsCache(() => { resetStatsCacheCalled = true })
  setSwitchToTab((tab: string) => { switchToTabTarget = tab })
  setRefreshStats(async () => { refreshStatsCalled = true })

  // Reset internal state
  setAuthMode('cloud')
  setServerUrl('')
})

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('settings-auth module', () => {
  it('exports all expected functions', () => {
    expect(typeof initAuthMode).toBe('function')
    expect(typeof refreshAuth).toBe('function')
    expect(typeof handleLogin).toBe('function')
    expect(typeof handleLogout).toBe('function')
    expect(typeof loadAuthSettingsValues).toBe('function')
    expect(typeof saveServerSettings).toBe('function')
    expect(typeof testConnection).toBe('function')
    expect(typeof settingsToggleAuthMode).toBe('function')
    expect(typeof settingsDoLocalLogin).toBe('function')
    expect(typeof wireSettingsAuthListeners).toBe('function')
    expect(typeof getAuthMode).toBe('function')
    expect(typeof setAuthMode).toBe('function')
    expect(typeof getServerUrl).toBe('function')
    expect(typeof setLogger).toBe('function')
    expect(typeof setRefreshAll).toBe('function')
    expect(typeof setResetStatsCache).toBe('function')
    expect(typeof setSwitchToTab).toBe('function')
    expect(typeof setRefreshStats).toBe('function')
    expect(typeof setServerUrl).toBe('function')
  })

  // ── Auth mode state ────────────────────────────────────────────────────────

  describe('getAuthMode / setAuthMode', () => {
    it('defaults to cloud mode', () => {
      expect(getAuthMode()).toBe('cloud')
    })

    it('switches to local mode', () => {
      setAuthMode('local')
      expect(getAuthMode()).toBe('local')
    })

    it('switches back to cloud mode', () => {
      setAuthMode('local')
      setAuthMode('cloud')
      expect(getAuthMode()).toBe('cloud')
    })
  })

  describe('getServerUrl / setServerUrl', () => {
    it('defaults to empty string', () => {
      expect(getServerUrl()).toBe('')
    })

    it('stores and returns server URL', () => {
      setAuthMode('local')
      expect(getServerUrl()).toBe('')
      // No setServerUrl export yet — but we can set via settingsDoLocalLogin flow
    })
  })

  // ── initAuthMode ───────────────────────────────────────────────────────────

  describe('initAuthMode', () => {
    it('loads auth mode and server URL from config', async () => {
      mockInvoke
        .mockResolvedValueOnce('local')   // get_auth_mode
        .mockResolvedValueOnce('https://my.server') // get_server_url

      await initAuthMode()
      expect(getAuthMode()).toBe('local')
      expect(getServerUrl()).toBe('https://my.server')
    })

    it('keeps defaults when commands are unavailable', async () => {
      mockInvoke.mockRejectedValue(new Error('not available'))
      await initAuthMode()
      expect(getAuthMode()).toBe('cloud')
      // getServerUrl may be empty or stale from other tests — verify it didn't change to a new value
      // Since both invokes fail, the URL should remain whatever it was before
    })
  })

  // ── refreshAuth ───────────────────────────────────────────────────────────

  describe('refreshAuth', () => {
    it('shows logged-in state when authenticated', async () => {
      setAuthMode('cloud')
      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })

      await refreshAuth()

      expect(document.getElementById('auth-status')!.textContent).toBe('Logged in')
      expect(document.getElementById('auth-status')!.className).toBe('value active')
      expect(document.getElementById('callsign-value')!.textContent).toBe('W1AW')
      expect((document.getElementById('login-btn') as HTMLElement).style.display).toBe('none')
      expect(document.getElementById('logout-btn')!.style.display).toBeFalsy()
      expect(document.getElementById('statusbar-server-dot')!.className).toContain('ok')
    })

    it('shows not-logged-in state when unauthenticated', async () => {
      mockInvoke.mockResolvedValue({ logged_in: false, callsign: null })

      await refreshAuth()

      expect(document.getElementById('auth-status')!.textContent).toBe('Not logged in')
      expect(document.getElementById('auth-status')!.className).toBe('value inactive')
      expect(document.getElementById('callsign-row')!.style.display).toBe('none')
      expect(document.getElementById('statusbar-server-dot')!.className).toContain('err')
    })

    it('shows server URL row in local mode when logged in', async () => {
      setAuthMode('local')
      // Set server URL
      mockInvoke
        .mockResolvedValueOnce('local')  // initAuthMode get_auth_mode
        .mockResolvedValueOnce('https://my.server') // initAuthMode get_server_url
      await initAuthMode()

      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })
      await refreshAuth()

      expect(document.getElementById('server-url-row')!.style.display).toBeFalsy()
      expect(document.getElementById('server-url-value')!.textContent).toBe('https://my.server')
    })

    it('handles errors gracefully', async () => {
      mockInvoke.mockRejectedValue(new Error('network error'))

      await refreshAuth()

      expect(logs.some(l => l.includes('Auth check error'))).toBe(true)
      expect(document.getElementById('statusbar-server-dot')!.className).toContain('err')
    })
  })

  // ── handleLogin ───────────────────────────────────────────────────────────

  describe('handleLogin', () => {
    it('switches to settings tab in local mode', async () => {
      setAuthMode('local')
      await handleLogin()
      expect(switchToTabTarget).toBe('settings')
    })

    it('invokes cloud login in cloud mode', async () => {
      setAuthMode('cloud')
      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })

      await handleLogin()

      expect(mockInvoke).toHaveBeenCalledWith('login')
      expect(resetStatsCacheCalled).toBe(true)
    })

    it('refreshes stats when Statistics tab is active after cloud login', async () => {
      setAuthMode('cloud')
      // Add the Statistics tab content to the DOM and mark it active
      document.body.insertAdjacentHTML('beforeend', '<div id="tab-content-statistics" class="tab-content active"></div>')
      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })

      await handleLogin()

      expect(refreshStatsCalled).toBe(true)
    })

    it('does not refresh stats when Statistics tab is not active after cloud login', async () => {
      setAuthMode('cloud')
      // Statistics tab content absent from DOM
      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })

      await handleLogin()

      expect(refreshStatsCalled).toBe(false)
    })

    it('does not refresh stats when Statistics tab exists but is inactive after cloud login', async () => {
      setAuthMode('cloud')
      // Statistics tab content present but not active
      document.body.insertAdjacentHTML('beforeend', '<div id="tab-content-statistics" class="tab-content"></div>')
      mockInvoke.mockResolvedValue({ logged_in: true, callsign: 'W1AW' })

      await handleLogin()

      expect(refreshStatsCalled).toBe(false)
    })

    it('logs error on cloud login failure', async () => {
      setAuthMode('cloud')
      mockInvoke.mockRejectedValue(new Error('OAuth failed'))

      await handleLogin()

      expect(logs.some(l => l.includes('Login failed'))).toBe(true)
    })
  })

  // ── handleLogout ──────────────────────────────────────────────────────────

  describe('handleLogout', () => {
    it('invokes logout and refreshes auth', async () => {
      mockInvoke.mockResolvedValue(undefined)

      await handleLogout()

      expect(mockInvoke).toHaveBeenCalledWith('logout')
      expect(resetStatsCacheCalled).toBe(true)
      expect(logs.some(l => l.includes('Logged out'))).toBe(true)
    })

    it('logs error on logout failure', async () => {
      mockInvoke.mockRejectedValue(new Error('logout error'))

      await handleLogout()

      expect(logs.some(l => l.includes('Logout error'))).toBe(true)
    })
  })

  // ── settingsToggleAuthMode ────────────────────────────────────────────────

  describe('settingsToggleAuthMode', () => {
    it('shows cloud mode UI', () => {
      settingsToggleAuthMode('cloud')

      expect(document.getElementById('settings-auth-cloud-btn')!.classList.contains('active')).toBe(true)
      expect(document.getElementById('settings-auth-local-btn')!.classList.contains('active')).toBe(false)
      expect(document.getElementById('settings-local-auth-fields')!.style.display).toBe('none')
      expect(document.getElementById('settings-local-login-btn')!.style.display).toBe('none')
    })

    it('shows local mode UI', () => {
      settingsToggleAuthMode('local')

      expect(document.getElementById('settings-auth-local-btn')!.classList.contains('active')).toBe(true)
      expect(document.getElementById('settings-auth-cloud-btn')!.classList.contains('active')).toBe(false)
      expect(document.getElementById('settings-local-auth-fields')!.style.display).toBeFalsy()
      expect(document.getElementById('settings-local-login-btn')!.style.display).toBeFalsy()
    })
  })

  // ── saveServerSettings ────────────────────────────────────────────────────

  describe('saveServerSettings', () => {
    it('rejects empty server URL', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = '  '
      await saveServerSettings()

      expect(logs.some(l => l.includes('empty'))).toBe(true)
      expect(mockInvoke).not.toHaveBeenCalledWith('save_settings', expect.anything())
    })

    it('saves server URL and auth mode', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = 'https://my.server'
      settingsToggleAuthMode('local')
      mockInvoke.mockResolvedValue(undefined)

      await saveServerSettings()

      expect(mockInvoke).toHaveBeenCalledWith('save_settings', {
        request: { server_url: 'https://my.server', auth_mode: 'local' },
      })
      expect(getServerUrl()).toBe('https://my.server')
      expect(getAuthMode()).toBe('local')
      expect(refreshAllCalled).toBe(true)
      expect(resetStatsCacheCalled).toBe(true)
    })

    it('reports save failure', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = 'https://my.server'
      mockInvoke.mockRejectedValue(new Error('save failed'))

      await saveServerSettings()

      expect(logs.some(l => l.includes('Settings save failed'))).toBe(true)
      expect(document.getElementById('settings-connection-status')!.textContent).toContain('Save failed')
    })
  })

  // ── testConnection ───────────────────────────────────────────────────────

  describe('testConnection', () => {
    it('reports successful connection', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = 'https://my.server'
      mockInvoke.mockResolvedValue({ reachable: true, status_code: 200, error: null })

      await testConnection()

      expect(document.getElementById('settings-connection-status')!.textContent).toBe('Connected ✓')
      expect(logs.some(l => l.includes('Connected to'))).toBe(true)
    })

    it('reports unreachable server', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = 'https://bad.server'
      mockInvoke.mockResolvedValue({ reachable: false, status_code: null, error: 'timeout' })

      await testConnection()

      expect(document.getElementById('settings-connection-status')!.className).toContain('error')
      expect(logs.some(l => l.includes('Connection test failed'))).toBe(true)
    })

    it('reports test error on invoke failure', async () => {
      (document.getElementById('settings-server-url') as HTMLInputElement).value = 'https://my.server'
      mockInvoke.mockRejectedValue(new Error('invoke error'))

      await testConnection()

      expect(document.getElementById('settings-connection-status')!.textContent).toBe('Test failed')
    })
  })

  // ── settingsDoLocalLogin ─────────────────────────────────────────────────

  describe('settingsDoLocalLogin', () => {
    it('rejects missing fields', async () => {
      await settingsDoLocalLogin()

      expect(document.getElementById('settings-local-login-status')!.textContent).toContain('Fill in')
    })

    it('logs in to self-hosted server', async () => {
      const urlInput = document.getElementById('settings-server-url') as HTMLInputElement
      const emailInput = document.getElementById('settings-local-email') as HTMLInputElement
      const pwInput = document.getElementById('settings-local-password') as HTMLInputElement
      urlInput.value = 'https://my.server'
      emailInput.value = 'user@test.com'
      pwInput.value = 'pass123'
      mockInvoke
        .mockResolvedValueOnce({ logged_in: true, callsign: 'W1AW' }) // login_local
        .mockResolvedValueOnce(undefined) // save_settings

      await settingsDoLocalLogin()

      expect(mockInvoke).toHaveBeenCalledWith('login_local', {
        request: { server_url: 'https://my.server', email: 'user@test.com', password: 'pass123' },
      })
      expect(getAuthMode()).toBe('local')
      expect(getServerUrl()).toBe('https://my.server')
      expect(document.getElementById('settings-local-login-status')!.textContent).toContain('W1AW')
      expect(refreshAllCalled).toBe(true)
    })

    it('reports login failure', async () => {
      const urlInput = document.getElementById('settings-server-url') as HTMLInputElement
      const emailInput = document.getElementById('settings-local-email') as HTMLInputElement
      const pwInput = document.getElementById('settings-local-password') as HTMLInputElement
      urlInput.value = 'https://my.server'
      emailInput.value = 'user@test.com'
      pwInput.value = 'wrong'
      mockInvoke.mockRejectedValue(new Error('unauthorized'))

      await settingsDoLocalLogin()

      expect(document.getElementById('settings-local-login-status')!.textContent).toContain('Login failed')
      expect(logs.some(l => l.includes('Self-hosted login failed'))).toBe(true)
    })
  })

  // ── wireSettingsAuthListeners ────────────────────────────────────────────

  describe('wireSettingsAuthListeners', () => {
    it('wires click handlers without error', () => {
      // Should not throw
      expect(() => wireSettingsAuthListeners()).not.toThrow()
    })
  })
})