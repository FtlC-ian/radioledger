/**
 * Settings / Auth — server URL, auth mode UI wiring, connection test,
 * local login form in settings, and save server settings flow.
 *
 * Extracted from main.ts as part of the desktop decomposition (issue #194).
 * Manages auth state (cloud vs. local mode), the server URL, and the
 * Settings tab auth card UI.  The Shack tab auth card is also refreshed
 * here because it is tightly coupled to auth state.
 *
 * The host shell injects callbacks for post-login / post-save actions
 * (refreshAll, resetStatsCache) and for switching to a different tab.
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError as _formatError, updateStatusBarServer as _updateStatusBarServer } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface AuthStatus {
  logged_in: boolean
  callsign: string | null
}

export interface ConnectionTestResult {
  reachable: boolean
  status_code: number | null
  error: string | null
}

// ─── Module state ─────────────────────────────────────────────────────────────

let currentAuthMode: 'cloud' | 'local' = 'cloud'
/** Server URL currently in use (updated from config on init and after settings changes). */
let currentServerUrl = ''

// ─── Injected callbacks ──────────────────────────────────────────────────────

let _log: (msg: string) => void = () => {}
let _refreshAll: () => Promise<void> = async () => {}
let _resetStatsCache: () => void = () => {}
let _switchToTab: (tabName: string) => void = () => {}
let _refreshStats: () => Promise<void> = async () => {}

/** Inject the shared activity-log helper from the shell bootstrap. */
export function setLogger(fn: (msg: string) => void): void {
  _log = fn
}

/** Inject refreshAll so auth actions can trigger a full app refresh. */
export function setRefreshAll(fn: () => Promise<void>): void {
  _refreshAll = fn
}

/** Inject resetStatsCache so login/logout can clear the stats cache. */
export function setResetStatsCache(fn: () => void): void {
  _resetStatsCache = fn
}

/** Inject switchToTab so handleLogin can redirect to Settings in local mode. */
export function setSwitchToTab(fn: (tabName: string) => void): void {
  _switchToTab = fn
}

/** Inject refreshStats so handleLogin can refresh the Statistics tab after cloud login. */
export function setRefreshStats(fn: () => Promise<void>): void {
  _refreshStats = fn
}

// ─── Auth mode / server URL accessors ─────────────────────────────────────────

/** Get the current auth mode (cloud or local). */
export function getAuthMode(): 'cloud' | 'local' {
  return currentAuthMode
}

/** Set the auth mode (e.g. after wizard finish). */
export function setAuthMode(mode: 'cloud' | 'local'): void {
  currentAuthMode = mode
}

/** Get the current server URL. */
export function getServerUrl(): string {
  return currentServerUrl
}

/** Set the current server URL. */
export function setServerUrl(url: string): void {
  currentServerUrl = url
}

// ─── Initialisation ───────────────────────────────────────────────────────────

/** Load the auth mode and server URL from config; update module-level variables. */
export async function initAuthMode(): Promise<void> {
  try {
    const mode: 'cloud' | 'local' = await invoke('get_auth_mode')
    currentAuthMode = mode
  } catch {
    // Command not available in dev mode — keep default (cloud)
  }

  try {
    const url: string = await invoke('get_server_url')
    currentServerUrl = url
  } catch {
    // Keep default empty string
  }
}

// ─── Auth card refresh (Shack tab) ────────────────────────────────────────────

export async function refreshAuth(): Promise<void> {
  try {
    const status: AuthStatus = await invoke('get_auth_status')
    const authEl = document.getElementById('auth-status')!
    const callsignRow = document.getElementById('callsign-row')!
    const callsignEl = document.getElementById('callsign-value')!
    const loginBtn = document.getElementById('login-btn')!
    const logoutBtn = document.getElementById('logout-btn')!
    const serverUrlRow = document.getElementById('server-url-row')
    const serverUrlValue = document.getElementById('server-url-value')

    if (status.logged_in) {
      authEl.textContent = 'Logged in'
      authEl.className = 'value active'
      callsignRow.style.display = ''
      callsignEl.textContent = status.callsign ?? '—'
      loginBtn.style.display = 'none'
      logoutBtn.style.display = ''

      // Show server URL row when in local (self-hosted) mode
      if (currentAuthMode === 'local' && serverUrlRow && serverUrlValue) {
        serverUrlRow.style.display = ''
        serverUrlValue.textContent = currentServerUrl || '—'
      } else if (serverUrlRow) {
        serverUrlRow.style.display = 'none'
      }
    } else {
      authEl.textContent = 'Not logged in'
      authEl.className = 'value inactive'
      callsignRow.style.display = 'none'
      if (serverUrlRow) serverUrlRow.style.display = 'none'
      loginBtn.style.display = ''
      logoutBtn.style.display = 'none'
    }

    // Status bar
    _updateStatusBarServer(status.logged_in)
  } catch (err) {
    _log(`Auth check error: ${_formatError(err)}`)
    _updateStatusBarServer(false)
  }
}

// ─── Login / Logout ──────────────────────────────────────────────────────────

export async function handleLogin(): Promise<void> {
  if (currentAuthMode === 'local') {
    // Local mode: the Shack tab has no login form — redirect to Settings tab
    _log('Local auth mode: switching to Settings tab to log in…')
    _switchToTab('settings')
  } else {
    // Cloud mode: OAuth2 PKCE flow
    _log('Opening browser for login…')
    try {
      const status: AuthStatus = await invoke('login')
      _resetStatsCache()
      _log(`Logged in as ${status.callsign ?? 'unknown'}`)
      await refreshAuth()
      if (document.getElementById('tab-content-statistics')?.classList.contains('active')) {
        await _refreshStats()
      }
    } catch (err) {
      _log(`Login failed: ${_formatError(err)}`)
    }
  }
}

export async function handleLogout(): Promise<void> {
  try {
    await invoke('logout')
    _resetStatsCache()
    _log('Logged out')
    await refreshAuth()
  } catch (err) {
    _log(`Logout error: ${_formatError(err)}`)
  }
}

// ─── Settings tab — auth/server loading ──────────────────────────────────────

/**
 * Load auth-mode and server-URL settings into the Settings tab fields.
 * Returns the loaded auth mode so the host can update its own state.
 *
 * This is only the auth/server portion — the host is still responsible
 * for loading UDP config, decode-panel settings, and rig profiles.
 */
export async function loadAuthSettingsValues(): Promise<'cloud' | 'local'> {
  // Load server URL from config
  try {
    const url: string = await invoke('get_server_url')
    const urlInput = document.getElementById('settings-server-url') as HTMLInputElement | null
    if (urlInput && url) urlInput.value = url
    currentServerUrl = url
  } catch {
    // keep placeholder
  }

  // Load auth mode from config and update the Settings toggle
  try {
    const mode: 'cloud' | 'local' = await invoke('get_auth_mode')
    settingsToggleAuthMode(mode)
    currentAuthMode = mode
    return mode
  } catch {
    // keep current state
    return currentAuthMode
  }
}

// ─── Settings tab — save server settings ─────────────────────────────────────

export async function saveServerSettings(): Promise<void> {
  const urlInput = document.getElementById('settings-server-url') as HTMLInputElement | null
  const url = urlInput?.value.trim()
  if (!url) {
    _log('Server URL cannot be empty')
    return
  }

  const statusEl = document.getElementById('settings-connection-status')

  // Determine auth mode from the Settings toggle button state
  const settingsMode: 'cloud' | 'local' =
    document.getElementById('settings-auth-local-btn')?.classList.contains('active') ? 'local' : 'cloud'

  try {
    await invoke('save_settings', { request: { server_url: url, auth_mode: settingsMode } })
    currentServerUrl = url
    currentAuthMode = settingsMode
    _resetStatsCache()

    if (statusEl) {
      statusEl.textContent = 'Settings saved ✓'
      statusEl.className = 'value active'
    }
    _log(`Settings saved — server: ${url}, mode: ${settingsMode}`)

    // Auto-refresh all app state so Shack tab reflects the new config immediately
    await _refreshAll()
  } catch (err) {
    if (statusEl) {
      statusEl.textContent = `Save failed: ${_formatError(err)}`
      statusEl.className = 'value error'
    }
    _log(`Settings save failed: ${_formatError(err)}`)
  }
}

// ─── Settings tab — connection test ──────────────────────────────────────────

export async function testConnection(): Promise<void> {
  const urlInput = document.getElementById('settings-server-url') as HTMLInputElement | null
  const url = urlInput?.value.trim() || 'https://radioledger.app'
  const statusEl = document.getElementById('settings-connection-status')

  _log(`Testing connection to ${url}…`)
  if (statusEl) {
    statusEl.textContent = 'Testing…'
    statusEl.className = 'value'
  }

  try {
    // Use a Tauri command instead of fetch() to avoid CSP/CORS restrictions in the webview.
    const result: ConnectionTestResult = await invoke('test_server_connection', { url })
    if (statusEl) {
      statusEl.textContent = result.reachable ? 'Connected ✓' : (result.error ?? 'Unreachable')
      statusEl.className = `value ${result.reachable ? 'active' : 'error'}`
    }
    _log(result.reachable ? `Connected to ${url}` : `Connection test failed: ${result.error ?? 'unreachable'}`)
  } catch (err) {
    if (statusEl) {
      statusEl.textContent = 'Test failed'
      statusEl.className = 'value error'
    }
    _log(`Connection test error: ${_formatError(err)}`)
  }
}

// ─── Settings tab — auth mode toggle ─────────────────────────────────────────

/** Toggle settings server card between Cloud and Self-hosted mode. */
export function settingsToggleAuthMode(mode: 'cloud' | 'local'): void {
  const cloudBtn = document.getElementById('settings-auth-cloud-btn')
  const localBtn = document.getElementById('settings-auth-local-btn')
  const localFields = document.getElementById('settings-local-auth-fields')
  const localLoginBtn = document.getElementById('settings-local-login-btn')

  if (mode === 'cloud') {
    cloudBtn?.classList.add('active')
    localBtn?.classList.remove('active')
    if (localFields) localFields.style.display = 'none'
    if (localLoginBtn) localLoginBtn.style.display = 'none'
  } else {
    localBtn?.classList.add('active')
    cloudBtn?.classList.remove('active')
    if (localFields) localFields.style.display = ''
    if (localLoginBtn) localLoginBtn.style.display = ''
  }
}

// ─── Settings tab — local login ──────────────────────────────────────────────

/** Sign in to a self-hosted server from the Settings tab. */
export async function settingsDoLocalLogin(): Promise<void> {
  const serverUrl = (document.getElementById('settings-server-url') as HTMLInputElement | null)?.value.trim()
  const email = (document.getElementById('settings-local-email') as HTMLInputElement | null)?.value.trim()
  const password = (document.getElementById('settings-local-password') as HTMLInputElement | null)?.value
  const statusRow = document.getElementById('settings-local-login-status-row')
  const statusEl = document.getElementById('settings-local-login-status')

  if (!serverUrl || !email || !password) {
    if (statusEl) statusEl.textContent = 'Fill in server URL, email, and password'
    if (statusRow) statusRow.style.display = ''
    return
  }

  if (statusEl) statusEl.textContent = 'Connecting…'
  if (statusRow) statusRow.style.display = ''

  try {
    const result: AuthStatus = await invoke('login_local', {
      request: { server_url: serverUrl, email, password },
    })
    if (statusEl) {
      statusEl.textContent = `Connected as ${result.callsign || email} ✓`
      statusEl.className = 'value active'
    }
    _log(`Signed in to self-hosted server as ${result.callsign ?? email}`)

    // Persist the server URL and auth mode to config so they survive restarts
    currentServerUrl = serverUrl
    currentAuthMode = 'local'
    try {
      await invoke('save_settings', { request: { server_url: serverUrl, auth_mode: 'local' } })
    } catch {
      // Non-fatal: log but continue
      _log('Warning: could not persist server URL to config')
    }

    // Refresh all app state — updates Shack tab auth card, status bar, and sync status
    await _refreshAll()
  } catch (err) {
    const msg = _formatError(err)
    if (statusEl) {
      statusEl.textContent = `Login failed: ${msg}`
      statusEl.className = 'value error'
    }
    _log(`Self-hosted login failed: ${msg}`)
  }
}

// ─── DOM wiring helper ────────────────────────────────────────────────────────

/** Wire settings/auth click handlers to the DOM. Call from the shell event wiring. */
export function wireSettingsAuthListeners(): void {
  const bindClick = (id: string, handler: () => void): void => {
    const el = document.getElementById(id)
    if (el) el.addEventListener('click', handler)
  }

  bindClick('settings-save-server-btn', () => { void saveServerSettings() })
  bindClick('settings-test-connection-btn', () => { void testConnection() })
  bindClick('settings-auth-cloud-btn', () => settingsToggleAuthMode('cloud'))
  bindClick('settings-auth-local-btn', () => settingsToggleAuthMode('local'))
  bindClick('settings-local-login-btn', () => { void settingsDoLocalLogin() })
}