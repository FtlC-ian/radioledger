import { ref, computed } from 'vue'

export interface User {
  uuid: string
  email: string
  callsign: string | null
  display_name: string | null
}

export interface AuthData {
  token: string
  user: User
}

export interface AuthAdapter {
  login(email: string): Promise<AuthData>
  register(email: string, callsign: string): Promise<AuthData>
  logout(): Promise<void>
  getMe(): Promise<User | null>
}

/**
 * HTTP adapter — communicates with the RadioLedger REST API.
 * Used by the web app (and any non-Tauri context).
 */
export function createHttpAuthAdapter(baseUrl = ''): AuthAdapter {
  const base = baseUrl || ''

  async function apiPost<T>(path: string, body: unknown): Promise<T> {
    const resp = await fetch(`${base}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const json = await resp.json()
    if (!json.success) throw new Error(json.message || json.error || 'Auth error')
    return json.data as T
  }

  return {
    async login(email: string): Promise<AuthData> {
      return apiPost<AuthData>('/v1/auth/login', { email })
    },
    async register(email: string, callsign: string): Promise<AuthData> {
      return apiPost<AuthData>('/v1/auth/register', { email, callsign })
    },
    async logout(): Promise<void> {
      localStorage.removeItem('rl_token')
      localStorage.removeItem('rl_user')
    },
    async getMe(): Promise<User | null> {
      const token = localStorage.getItem('rl_token')
      if (!token) return null
      try {
        const resp = await fetch(`${base}/v1/auth/me`, {
          headers: { Authorization: `Bearer ${token}` },
        })
        const json = await resp.json()
        if (!json.success) return null
        return json.data as User
      } catch {
        return null
      }
    },
  }
}

/**
 * Tauri adapter — calls Rust commands via invoke.
 * Used by the desktop app.
 */
export function createTauriAuthAdapter(invoke: (cmd: string, args?: unknown) => Promise<unknown>): AuthAdapter {
  return {
    async login(_email: string): Promise<AuthData> {
      const status = await invoke('login') as { logged_in: boolean; callsign: string | null }
      // Tauri login opens browser flow — map result to AuthData shape
      const token = await invoke('get_api_token') as string
      return {
        token,
        user: { uuid: '', email: '', callsign: status.callsign, display_name: status.callsign },
      }
    },
    async register(_email: string, _callsign: string): Promise<AuthData> {
      // Tauri doesn't support direct register — redirect to login
      return this.login(_email)
    },
    async logout(): Promise<void> {
      await invoke('logout')
    },
    async getMe(): Promise<User | null> {
      const status = await invoke('get_auth_status') as { logged_in: boolean; callsign: string | null }
      if (!status.logged_in) return null
      return { uuid: '', email: '', callsign: status.callsign, display_name: status.callsign }
    },
  }
}

export function useAuth(adapter: AuthAdapter) {
  const user = ref<User | null>(null)
  const token = ref<string | null>(localStorage.getItem('rl_token'))
  const loading = ref(false)
  const error = ref<string | null>(null)

  const isLoggedIn = computed(() => !!token.value && !!user.value)

  async function init() {
    if (!token.value) return
    user.value = await adapter.getMe()
    if (!user.value) token.value = null
  }

  async function login(email: string) {
    loading.value = true
    error.value = null
    try {
      const data = await adapter.login(email)
      token.value = data.token
      user.value = data.user
      localStorage.setItem('rl_token', data.token)
      localStorage.setItem('rl_user', JSON.stringify(data.user))
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function register(email: string, callsign: string) {
    loading.value = true
    error.value = null
    try {
      const data = await adapter.register(email, callsign)
      token.value = data.token
      user.value = data.user
      localStorage.setItem('rl_token', data.token)
      localStorage.setItem('rl_user', JSON.stringify(data.user))
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function logout() {
    await adapter.logout()
    token.value = null
    user.value = null
    localStorage.removeItem('rl_token')
    localStorage.removeItem('rl_user')
  }

  return { user, token, isLoggedIn, loading, error, init, login, register, logout }
}
