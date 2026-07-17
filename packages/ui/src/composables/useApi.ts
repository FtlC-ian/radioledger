import { ref } from 'vue'

export interface ApiEnvelope<T> {
  success: boolean
  message: string
  data: T
  error?: string
}

export interface ApiAdapter {
  getToken: () => string | null
  getBaseUrl: () => string
}

export function createHttpAdapter(baseUrl = ''): ApiAdapter {
  return {
    getToken: () => localStorage.getItem('rl_token'),
    getBaseUrl: () => baseUrl,
  }
}

export function useApi(adapter: ApiAdapter) {
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function get<T>(path: string, params?: Record<string, string>): Promise<T | null> {
    loading.value = true
    error.value = null
    try {
      const token = adapter.getToken()
      const base = adapter.getBaseUrl()
      const url = new URL(path, base || window.location.origin)
      if (params) {
        for (const [k, v] of Object.entries(params)) url.searchParams.set(k, v)
      }
      const headers: Record<string, string> = {}
      if (token) headers['Authorization'] = `Bearer ${token}`

      const resp = await fetch(url.toString(), { headers })
      const json: ApiEnvelope<T> = await resp.json()
      if (!json.success) {
        error.value = json.message || json.error || 'API error'
        return null
      }
      return json.data
    } catch (e) {
      error.value = String(e)
      return null
    } finally {
      loading.value = false
    }
  }

  async function post<T>(path: string, body: unknown): Promise<T | null> {
    loading.value = true
    error.value = null
    try {
      const token = adapter.getToken()
      const base = adapter.getBaseUrl()
      const url = new URL(path, base || window.location.origin)
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (token) headers['Authorization'] = `Bearer ${token}`

      const resp = await fetch(url.toString(), {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
      })
      const json: ApiEnvelope<T> = await resp.json()
      if (!json.success) {
        error.value = json.message || json.error || 'API error'
        return null
      }
      return json.data
    } catch (e) {
      error.value = String(e)
      return null
    } finally {
      loading.value = false
    }
  }

  async function postForm<T>(path: string, formData: FormData): Promise<T | null> {
    loading.value = true
    error.value = null
    try {
      const token = adapter.getToken()
      const base = adapter.getBaseUrl()
      const url = new URL(path, base || window.location.origin)
      const headers: Record<string, string> = {}
      if (token) headers['Authorization'] = `Bearer ${token}`

      const resp = await fetch(url.toString(), {
        method: 'POST',
        headers,
        body: formData,
      })
      const json: ApiEnvelope<T> = await resp.json()
      if (!json.success) {
        error.value = json.message || json.error || 'API error'
        return null
      }
      return json.data
    } catch (e) {
      error.value = String(e)
      return null
    } finally {
      loading.value = false
    }
  }

  async function del<T>(path: string): Promise<T | null> {
    loading.value = true
    error.value = null
    try {
      const token = adapter.getToken()
      const base = adapter.getBaseUrl()
      const url = new URL(path, base || window.location.origin)
      const headers: Record<string, string> = {}
      if (token) headers['Authorization'] = `Bearer ${token}`

      const resp = await fetch(url.toString(), { method: 'DELETE', headers })
      const json: ApiEnvelope<T> = await resp.json()
      if (!json.success) {
        error.value = json.message || json.error || 'API error'
        return null
      }
      return json.data
    } catch (e) {
      error.value = String(e)
      return null
    } finally {
      loading.value = false
    }
  }

  return { get, post, postForm, del, loading, error }
}
