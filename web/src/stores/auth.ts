import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { apiGet, apiPatch, apiPost, isApiErrorStatus, setAuthToken } from 'src/api/client'

export interface UserProfile {
  uuid: string
  email: string
  callsign?: string | null
  display_name?: string | null
  grid_square?: string | null
  onboarding_complete?: boolean
  timezone: string
  is_admin?: boolean
}

interface AuthResponse {
  token: string
  user: UserProfile
}

interface OidcDiscoveryDocument {
  authorization_endpoint: string
  token_endpoint: string
  end_session_endpoint?: string
}

interface OidcTokenResponse {
  access_token: string
  expires_in?: number
  id_token?: string
  refresh_token?: string
  scope?: string
  token_type?: string
}

interface OidcLoginOptions {
  prompt?: 'create'
  inviteCode?: string
}

const OIDC_AUTHORITY = (process.env.OIDC_AUTHORITY || '').replace(/\/$/, '')
const OIDC_CLIENT_ID = process.env.OIDC_CLIENT_ID || ''
const DEFAULT_OIDC_REDIRECT_URI =
  typeof window !== 'undefined' ? `${window.location.origin}/#/auth/callback` : ''
const OIDC_REDIRECT_URI = process.env.OIDC_REDIRECT_URI || DEFAULT_OIDC_REDIRECT_URI
const OIDC_SCOPE = 'openid profile email offline_access'

const STORAGE_KEYS = {
  token: 'radioledger.token',
  user: 'radioledger.user',
  refreshToken: 'radioledger.oidc.refresh_token',
  idToken: 'radioledger.oidc.id_token',
  expiresAt: 'radioledger.oidc.expires_at',
  pkceVerifier: 'radioledger.oidc.pkce_verifier',
  pkceState: 'radioledger.oidc.state',
  inviteCode: 'radioledger.invite_code',
} as const

let discoveryPromise: Promise<OidcDiscoveryDocument> | null = null

function readStorage(key: string): string | null {
  if (typeof localStorage === 'undefined') return null
  return localStorage.getItem(key)
}

function writeStorage(key: string, value: string | null | undefined) {
  if (typeof localStorage === 'undefined') return

  if (value == null || value === '') {
    localStorage.removeItem(key)
    return
  }

  localStorage.setItem(key, value)
}

function loadStoredUser(): UserProfile | null {
  const raw = readStorage(STORAGE_KEYS.user)
  if (!raw) return null

  try {
    return JSON.parse(raw) as UserProfile
  } catch {
    return null
  }
}

function loadStoredNumber(key: string): number | null {
  const raw = readStorage(key)
  if (!raw) return null

  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : null
}

function toBase64Url(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }

  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function randomBytes(length: number): Uint8Array {
  const bytes = new Uint8Array(length)
  crypto.getRandomValues(bytes)
  return bytes
}

function randomString(length = 32): string {
  return toBase64Url(randomBytes(length))
}

async function sha256Base64Url(input: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input))
  return toBase64Url(new Uint8Array(digest))
}

async function getOidcDiscovery(): Promise<OidcDiscoveryDocument> {
  if (!OIDC_AUTHORITY) {
    throw new Error('OIDC authority is not configured')
  }

  discoveryPromise ??= fetch(`${OIDC_AUTHORITY}/.well-known/openid-configuration`).then(async (response) => {
    if (!response.ok) {
      throw new Error('Failed to load OIDC configuration')
    }

    return (await response.json()) as OidcDiscoveryDocument
  })

  return discoveryPromise
}

async function exchangeOidcToken(params: URLSearchParams): Promise<OidcTokenResponse> {
  const discovery = await getOidcDiscovery()
  const response = await fetch(discovery.token_endpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: params.toString(),
  })

  const payload = (await response.json().catch(() => null)) as
    | (OidcTokenResponse & { error?: string; error_description?: string })
    | null

  if (!response.ok || !payload?.access_token) {
    throw new Error(payload?.error_description || payload?.error || 'OIDC token exchange failed')
  }

  return payload
}

export const useAuthStore = defineStore('auth', () => {
  const storedToken = readStorage(STORAGE_KEYS.token)

  const token = ref<string | null>(storedToken)
  const userProfile = ref<UserProfile | null>(loadStoredUser())
  const refreshToken = ref<string | null>(readStorage(STORAGE_KEYS.refreshToken))
  const idToken = ref<string | null>(readStorage(STORAGE_KEYS.idToken))
  const expiresAt = ref<number | null>(loadStoredNumber(STORAGE_KEYS.expiresAt))

  if (token.value) {
    setAuthToken(token.value)
  }

  const isAuthenticated = computed(() => Boolean(token.value))
  const isOidc = computed(() => Boolean(OIDC_AUTHORITY))
  const callsign = computed(() => userProfile.value?.callsign ?? '')
  const email = computed(() => userProfile.value?.email ?? '')
  const displayName = computed(
    () => userProfile.value?.display_name || userProfile.value?.callsign || userProfile.value?.email || 'Operator',
  )
  const isAdmin = computed(() => Boolean(userProfile.value?.is_admin))
  const needsOnboarding = computed(() => isAuthenticated.value && !userProfile.value?.onboarding_complete)

  function clearOidcTransaction() {
    writeStorage(STORAGE_KEYS.pkceVerifier, null)
    writeStorage(STORAGE_KEYS.pkceState, null)
  }

  function clearPendingInviteCode() {
    writeStorage(STORAGE_KEYS.inviteCode, null)
  }

  function persistOidcTokens(tokens: OidcTokenResponse) {
    refreshToken.value = tokens.refresh_token || null
    idToken.value = tokens.id_token || null
    expiresAt.value = tokens.expires_in ? Date.now() + tokens.expires_in * 1000 : null

    writeStorage(STORAGE_KEYS.refreshToken, refreshToken.value)
    writeStorage(STORAGE_KEYS.idToken, idToken.value)
    writeStorage(STORAGE_KEYS.expiresAt, expiresAt.value ? String(expiresAt.value) : null)
  }

  function setSession(newToken: string, profile: UserProfile) {
    token.value = newToken
    userProfile.value = profile
    setAuthToken(newToken)
    writeStorage(STORAGE_KEYS.token, newToken)
    writeStorage(STORAGE_KEYS.user, JSON.stringify(profile))
  }

  function clearSessionStorage() {
    writeStorage(STORAGE_KEYS.token, null)
    writeStorage(STORAGE_KEYS.user, null)
    writeStorage(STORAGE_KEYS.refreshToken, null)
    writeStorage(STORAGE_KEYS.idToken, null)
    writeStorage(STORAGE_KEYS.expiresAt, null)
    writeStorage('radioledger.logbook_uuid', null)
    clearOidcTransaction()
    clearPendingInviteCode()
  }

  function clearSession() {
    token.value = null
    userProfile.value = null
    refreshToken.value = null
    idToken.value = null
    expiresAt.value = null
    setAuthToken(null)
    clearSessionStorage()
  }

  function isAccessTokenExpired(skewMs = 30000) {
    return Boolean(expiresAt.value && Date.now() >= expiresAt.value - skewMs)
  }

  async function loadUserProfileFromApi() {
    if (!token.value) return null

    if (isOidc.value) {
      await refreshAccessToken()
    }

    const response = await apiGet<UserProfile>('/v1/auth/me')
    if (response.success && response.data) {
      userProfile.value = response.data
      writeStorage(STORAGE_KEYS.user, JSON.stringify(response.data))
      return response.data
    }

    return null
  }

  async function fetchMe() {
    if (!token.value) return null

    try {
      return await loadUserProfileFromApi()
    } catch (error) {
      if (isApiErrorStatus(error, 401)) {
        clearSession()
      } else {
        console.error('fetchMe failed', error)
      }
      // Ignore — profile fetch is best-effort.
    }

    return null
  }

  async function validateSession() {
    if (!token.value) return true

    try {
      return Boolean(await loadUserProfileFromApi())
    } catch (error) {
      if (isApiErrorStatus(error, 401)) {
        clearSession()
        return false
      }
      throw error
    }
  }

  async function completeProfile(callsign: string, gridSquare?: string | null) {
    const response = await apiPatch<UserProfile>('/v1/auth/profile', {
      callsign: callsign.trim().toUpperCase(),
      grid_square: gridSquare?.trim().toUpperCase() || null,
    })

    if (response.success && response.data) {
      userProfile.value = response.data
      writeStorage(STORAGE_KEYS.user, JSON.stringify(response.data))
    }

    return response
  }

  async function refreshAccessToken(force = false) {
    if (!isOidc.value || !refreshToken.value) return token.value
    if (!force && token.value && !isAccessTokenExpired()) return token.value

    const tokens = await exchangeOidcToken(
      new URLSearchParams({
        grant_type: 'refresh_token',
        client_id: OIDC_CLIENT_ID,
        refresh_token: refreshToken.value,
      }),
    )

    persistOidcTokens(tokens)
    token.value = tokens.access_token
    setAuthToken(tokens.access_token)
    writeStorage(STORAGE_KEYS.token, tokens.access_token)
    return tokens.access_token
  }

  async function login(loginEmail: string, loginPassword: string) {
    const response = await apiPost<AuthResponse>('/v1/auth/login', {
      email: loginEmail,
      password: loginPassword,
    })
    if (response.success && response.data) {
      setSession(response.data.token, response.data.user)
      await fetchMe()
    }
    return response
  }

  async function register(
    registerEmail: string,
    registerPassword: string,
    registerCallsign?: string,
    registerDisplayName?: string,
  ) {
    const response = await apiPost<AuthResponse>('/v1/auth/register', {
      email: registerEmail,
      password: registerPassword,
      callsign: registerCallsign || undefined,
      display_name: registerDisplayName || undefined,
    })
    if (response.success && response.data) {
      setSession(response.data.token, response.data.user)
      await fetchMe()
    }
    return response
  }

  async function changePassword(oldPassword: string, newPassword: string) {
    return await apiPost('/v1/auth/change-password', {
      old_password: oldPassword,
      new_password: newPassword,
    })
  }

  async function validateInviteCode(inviteCode: string) {
    return await apiPost<{ valid: boolean; required?: boolean }>('/v1/auth/validate-invite', {
      invite_code: inviteCode,
    })
  }

  async function consumeInviteCode(inviteCode: string) {
    return await apiPost<UserProfile>('/v1/auth/consume-invite', {
      invite_code: inviteCode,
    })
  }

  async function loginWithOidc(options: OidcLoginOptions = {}) {
    if (!isOidc.value) {
      throw new Error('OIDC is not configured')
    }
    if (!OIDC_CLIENT_ID) {
      throw new Error('OIDC client ID is not configured')
    }
    if (!OIDC_REDIRECT_URI) {
      throw new Error('OIDC redirect URI is not configured')
    }

    const inviteCode = options.inviteCode?.trim().toUpperCase() || ''
    if (options.prompt === 'create') {
      const validation = await validateInviteCode(inviteCode)
      if (!validation.success) {
        throw new Error(validation.error || validation.message || 'Invite validation failed')
      }
      writeStorage(STORAGE_KEYS.inviteCode, inviteCode)
    } else {
      clearPendingInviteCode()
    }

    const discovery = await getOidcDiscovery()
    const codeVerifier = randomString(64)
    const state = randomString(24)
    const codeChallenge = await sha256Base64Url(codeVerifier)

    writeStorage(STORAGE_KEYS.pkceVerifier, codeVerifier)
    writeStorage(STORAGE_KEYS.pkceState, state)

    const authorizeUrl = new URL(discovery.authorization_endpoint)
    authorizeUrl.searchParams.set('response_type', 'code')
    authorizeUrl.searchParams.set('client_id', OIDC_CLIENT_ID)
    authorizeUrl.searchParams.set('redirect_uri', OIDC_REDIRECT_URI)
    authorizeUrl.searchParams.set('scope', OIDC_SCOPE)
    authorizeUrl.searchParams.set('code_challenge', codeChallenge)
    authorizeUrl.searchParams.set('code_challenge_method', 'S256')
    authorizeUrl.searchParams.set('state', state)
    if (options.prompt) {
      authorizeUrl.searchParams.set('prompt', options.prompt)
    }

    window.location.assign(authorizeUrl.toString())
  }

  async function handleOidcCallback(code: string, state: string) {
    if (!isOidc.value) {
      throw new Error('OIDC is not configured')
    }

    const storedState = readStorage(STORAGE_KEYS.pkceState)
    const codeVerifier = readStorage(STORAGE_KEYS.pkceVerifier)

    if (!state || !storedState || state !== storedState) {
      clearOidcTransaction()
      throw new Error('Invalid OIDC state')
    }

    if (!codeVerifier) {
      clearOidcTransaction()
      throw new Error('Missing PKCE code verifier')
    }

    const tokens = await exchangeOidcToken(
      new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: OIDC_CLIENT_ID,
        code,
        redirect_uri: OIDC_REDIRECT_URI,
        code_verifier: codeVerifier,
      }),
    )

    clearOidcTransaction()
    persistOidcTokens(tokens)
    token.value = tokens.access_token
    setAuthToken(tokens.access_token)
    writeStorage(STORAGE_KEYS.token, tokens.access_token)

    const pendingInviteCode = readStorage(STORAGE_KEYS.inviteCode)?.trim().toUpperCase() || ''
    if (pendingInviteCode) {
      const consume = await consumeInviteCode(pendingInviteCode)
      if (!consume.success) {
        clearSession()
        throw new Error(consume.error || consume.message || 'Invite required')
      }
      clearPendingInviteCode()
    }

    const profile = await fetchMe()
    if (!profile) {
      clearSession()
      throw new Error('Signed in, but failed to load user profile')
    }
  }

  function loginStub(name: string = 'operator') {
    const stubProfile: UserProfile = {
      uuid: 'stub-user',
      email: `${name}@local`,
      callsign: name.toUpperCase(),
      display_name: name,
      onboarding_complete: true,
      timezone: 'UTC',
    }
    setSession('stub-token', stubProfile)
  }

  async function logout() {
    const shouldLogoutOidc = isOidc.value && Boolean(token.value)
    const currentIdToken = idToken.value

    clearSession()

    if (!shouldLogoutOidc || typeof window === 'undefined') {
      return
    }

    try {
      const discovery = await getOidcDiscovery()
      if (!discovery.end_session_endpoint) {
        return
      }

      const logoutUrl = new URL(discovery.end_session_endpoint)
      logoutUrl.searchParams.set('post_logout_redirect_uri', `${window.location.origin}/#/login`)
      if (OIDC_CLIENT_ID) {
        logoutUrl.searchParams.set('client_id', OIDC_CLIENT_ID)
      }
      if (currentIdToken) {
        logoutUrl.searchParams.set('id_token_hint', currentIdToken)
      }

      window.location.assign(logoutUrl.toString())
    } catch {
      // Fall back to local logout only.
    }
  }

  if (isOidc.value && token.value && refreshToken.value && isAccessTokenExpired()) {
    void refreshAccessToken(true).catch(() => {
      clearSession()
    })
  }

  return {
    token,
    userProfile,
    isAuthenticated,
    isOidc,
    isAdmin,
    needsOnboarding,
    callsign,
    email,
    displayName,
    login,
    loginWithOidc,
    handleOidcCallback,
    refreshAccessToken,
    register,
    changePassword,
    fetchMe,
    validateSession,
    completeProfile,
    clearSession,
    loginStub,
    logout,
  }
})
