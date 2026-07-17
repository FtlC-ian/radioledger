import axios, { type AxiosError, type AxiosRequestConfig } from 'axios'
import type { ApiResponse } from 'src/types/qso'

const API_BASE_URL =
  process.env.RADIOLEDGER_API_BASE_URL || process.env.API_BASE_URL || ''

// Use empty string for production (relative paths), localhost:9091 for dev
// Explicit undefined check to allow empty string to pass through
const effectiveBaseURL =
  process.env.RADIOLEDGER_API_BASE_URL !== undefined || process.env.API_BASE_URL !== undefined
    ? API_BASE_URL
    : 'http://localhost:9091'

export interface ApiClientError<T = unknown> extends Partial<ApiResponse<T>> {
  status?: number
}

let authToken = ''
let onUnauthorizedCallback: (() => void) | null = null
let loadingBarStartFn: (() => void) | null = null
let loadingBarStopFn: (() => void) | null = null

// Restore auth token from localStorage on module load.
if (typeof localStorage !== 'undefined') {
  const stored = localStorage.getItem('radioledger.token')
  if (stored) authToken = stored
}

let userIdHeader =
  process.env.RADIOLEDGER_USER_ID ||
  process.env.VUE_APP_RADIOLEDGER_USER_ID ||
  (typeof localStorage !== 'undefined' ? localStorage.getItem('radioledger.user_id') || '' : '')

export function setUserIdHeader(userId: string | number | null | undefined) {
  const normalized = userId == null ? '' : String(userId).trim()
  userIdHeader = normalized || userIdHeader

  if (typeof localStorage !== 'undefined' && normalized) {
    localStorage.setItem('radioledger.user_id', normalized)
  }
}

export function setAuthToken(token: string | null | undefined) {
  authToken = token ?? ''
  if (typeof localStorage !== 'undefined') {
    if (authToken) {
      localStorage.setItem('radioledger.token', authToken)
    } else {
      localStorage.removeItem('radioledger.token')
    }
  }
}

export function getAuthToken() {
  return authToken
}

/** Register a callback invoked when any API call returns 401. */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorizedCallback = fn
}

export function isApiErrorStatus(error: unknown, status: number) {
  return Boolean(error && typeof error === 'object' && (error as ApiClientError).status === status)
}

export function getApiErrorMessage(error: unknown, fallback: string) {
  if (error && typeof error === 'object') {
    const apiError = error as ApiClientError
    if (typeof apiError.error === 'string' && apiError.error.trim()) {
      return apiError.error
    }
    if (typeof apiError.message === 'string' && apiError.message.trim()) {
      return apiError.message
    }
  }

  if (error instanceof Error && error.message.trim()) {
    return error.message
  }

  return fallback
}

/** Wire up Quasar LoadingBar so API calls drive the top progress bar. */
export function setLoadingBarHandlers(start: () => void, stop: () => void) {
  loadingBarStartFn = start
  loadingBarStopFn = stop
}

const api = axios.create({
  baseURL: effectiveBaseURL,
  timeout: 20000,
})

api.interceptors.request.use((config) => {
  config.headers = config.headers ?? {}

  if (authToken) {
    config.headers.Authorization = `Bearer ${authToken}`
  }

  if (userIdHeader) {
    config.headers['X-User-ID'] = userIdHeader
  }

  loadingBarStartFn?.()
  return config
})

api.interceptors.response.use(
  (response) => {
    loadingBarStopFn?.()
    return response
  },
  (error: AxiosError<ApiResponse<unknown>>) => {
    loadingBarStopFn?.()

    if (error.response?.status === 401 && onUnauthorizedCallback) {
      onUnauthorizedCallback()
    }

    if (error.response?.data && typeof error.response.data === 'object') {
      return Promise.reject({
        status: error.response.status,
        ...error.response.data,
      } satisfies ApiClientError)
    }

    return Promise.reject({
      status: error.response?.status,
      error: error.message,
      message: error.message,
    } satisfies ApiClientError)
  },
)

export async function apiGet<T>(url: string, config?: AxiosRequestConfig) {
  const response = await api.get<ApiResponse<T>>(url, config)
  return response.data
}

export async function apiPost<T, B = unknown>(url: string, body?: B, config?: AxiosRequestConfig) {
  const response = await api.post<ApiResponse<T>>(url, body, config)
  return response.data
}

export async function apiPut<T, B = unknown>(url: string, body?: B, config?: AxiosRequestConfig) {
  const response = await api.put<ApiResponse<T>>(url, body, config)
  return response.data
}

export async function apiPatch<T, B = unknown>(url: string, body?: B, config?: AxiosRequestConfig) {
  const response = await api.patch<ApiResponse<T>>(url, body, config)
  return response.data
}

export async function apiDelete<T>(url: string, config?: AxiosRequestConfig) {
  const response = await api.delete<ApiResponse<T>>(url, config)
  return response.data
}

export { api, API_BASE_URL }
export default api
