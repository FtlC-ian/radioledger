import { reactive, ref, watch } from 'vue'
import { useQuasar } from 'quasar'
import { apiDelete, apiGet, apiPost, apiPut, getApiErrorMessage } from 'src/api/client'

export type CredentialServiceKey = 'qrz' | 'eqsl' | 'clublog' | 'hamqth' | 'pota'
export type CredentialKind = 'api_key' | 'username_password'
export type QRZCredentialMode = 'api_key' | 'username_password'
export type CredentialStatus = 'connected' | 'unverified' | 'not_connected'
export type CredentialEndpointMode = 'browser' | 'sync'

export interface CredentialServiceDefinition {
  key: CredentialServiceKey
  label: string
  description: string
  credentialType: CredentialKind
  inputLabel?: string
  icon: string
}

export interface CredentialItem {
  service: CredentialServiceKey
  credential_type: CredentialKind
  is_active: boolean
  verified?: boolean
  verification_error?: string
  last_verified_at?: string | null
  updated_at?: string
  last_used_at?: string | null
  created_at?: string
}

type CredentialListData = CredentialItem[] | { items?: CredentialItem[]; credentials?: CredentialItem[] }

const credentialServiceKeys: CredentialServiceKey[] = ['qrz', 'eqsl', 'clublog', 'hamqth', 'pota']

export const credentialServices: CredentialServiceDefinition[] = [
  {
    key: 'qrz',
    label: 'QRZ.com',
    description: 'Supports either XML username/password or a Logbook API key, depending on the feature you use.',
    credentialType: 'api_key',
    inputLabel: 'QRZ API key',
    icon: 'travel_explore',
  },
  {
    key: 'eqsl',
    label: 'eQSL.cc',
    description: 'Enable eQSL authentication for confirmations and sync workflows.',
    credentialType: 'username_password',
    icon: 'mail',
  },
  {
    key: 'clublog',
    label: 'Club Log',
    description: 'Connect Club Log uploads and downstream integrations.',
    credentialType: 'api_key',
    inputLabel: 'Club Log API key',
    icon: 'route',
  },
  {
    key: 'hamqth',
    label: 'HamQTH',
    description: 'Provide HamQTH credentials for external lookup integrations.',
    credentialType: 'api_key',
    inputLabel: 'HamQTH API key',
    icon: 'account_tree',
  },
  {
    key: 'pota',
    label: 'Parks on the Air (POTA)',
    description: 'Allow POTA API access for park activity workflows.',
    credentialType: 'api_key',
    inputLabel: 'POTA API key',
    icon: 'park',
  },
]

export const qrzCredentialModeOptions = [
  { label: 'API key', value: 'api_key' },
  { label: 'Username + Password', value: 'username_password' },
]

function createCredentialDraftState() {
  return {
    qrz: { apiKey: '', username: '', password: '' },
    eqsl: { apiKey: '', username: '', password: '' },
    clublog: { apiKey: '', username: '', password: '' },
    hamqth: { apiKey: '', username: '', password: '' },
    pota: { apiKey: '', username: '', password: '' },
  }
}

function createCredentialBooleanState() {
  return {
    qrz: false,
    eqsl: false,
    clublog: false,
    hamqth: false,
    pota: false,
  }
}

function isCredentialServiceKey(value: string): value is CredentialServiceKey {
  return credentialServiceKeys.includes(value as CredentialServiceKey)
}

function isRouteNotFoundError(error: unknown) {
  const message = getApiErrorMessage(error, '').toLowerCase()
  return message.includes('not found') || message.includes('404') || message.includes('cannot')
}

function normalizeCredentialItem(item: CredentialItem | Record<string, unknown>): CredentialItem | null {
  const rawService = typeof item.service === 'string' ? item.service : ''
  if (!isCredentialServiceKey(rawService)) {
    return null
  }

  const rawLastVerifiedAt =
    typeof item.last_verified_at === 'string' || item.last_verified_at === null ? item.last_verified_at : undefined
  const rawVerified = typeof item.verified === 'boolean' ? item.verified : rawLastVerifiedAt != null

  return {
    service: rawService,
    credential_type: item.credential_type === 'username_password' ? 'username_password' : 'api_key',
    is_active: typeof item.is_active === 'boolean' ? item.is_active : true,
    verified: rawVerified,
    verification_error: typeof item.verification_error === 'string' ? item.verification_error : undefined,
    last_verified_at: rawLastVerifiedAt,
    updated_at: typeof item.updated_at === 'string' ? item.updated_at : undefined,
    last_used_at: typeof item.last_used_at === 'string' || item.last_used_at === null ? item.last_used_at : undefined,
    created_at: typeof item.created_at === 'string' ? item.created_at : undefined,
  }
}

function extractCredentialItems(data: CredentialListData | null | undefined): CredentialItem[] {
  if (!data) {
    return []
  }

  if (Array.isArray(data)) {
    return data
      .map((item) => normalizeCredentialItem(item))
      .filter((item): item is CredentialItem => Boolean(item))
  }

  const items = Array.isArray(data.items) ? data.items : Array.isArray(data.credentials) ? data.credentials : []
  return items
    .map((item) => normalizeCredentialItem(item))
    .filter((item): item is CredentialItem => Boolean(item))
}

export function useCredentials() {
  const $q = useQuasar()

  const credentials = ref<Partial<Record<CredentialServiceKey, CredentialItem>>>({})
  const credentialEndpointMode = ref<CredentialEndpointMode | null>(null)
  const qrzCredentialMode = ref<QRZCredentialMode>('api_key')

  const credentialDraft = reactive(createCredentialDraftState())
  const credentialSaving = reactive(createCredentialBooleanState())
  const credentialRemoving = reactive(createCredentialBooleanState())
  const credentialVerifying = reactive(createCredentialBooleanState())

  function getCredentialStatus(service: CredentialServiceKey): CredentialStatus {
    const credential = credentials.value[service]
    if (!credential?.is_active) {
      return 'not_connected'
    }
    if (credential.verified === false || !credential.last_verified_at) {
      return 'unverified'
    }
    return 'connected'
  }

  function getServiceStatusMeta(service: CredentialServiceKey) {
    const status = getCredentialStatus(service)
    if (status === 'connected') {
      return {
        label: 'Connected',
        color: 'positive',
        textColor: 'white',
        icon: 'check_circle',
      }
    }
    if (status === 'unverified') {
      return {
        label: 'Unverified',
        color: 'warning',
        textColor: 'black',
        icon: 'warning',
      }
    }
    return {
      label: 'Not connected',
      color: 'grey-6',
      textColor: 'white',
      icon: 'radio_button_unchecked',
    }
  }

  function isServiceConnected(service: CredentialServiceKey) {
    return getCredentialStatus(service) !== 'not_connected'
  }

  function canSaveCredential(service: CredentialServiceDefinition) {
    const draft = credentialDraft[service.key]
    if (service.key === 'qrz') {
      return qrzCredentialMode.value === 'username_password'
        ? Boolean(draft.username.trim() && draft.password.trim())
        : Boolean(draft.apiKey.trim())
    }
    if (service.credentialType === 'username_password') {
      return Boolean(draft.username.trim() && draft.password.trim())
    }
    return Boolean(draft.apiKey.trim())
  }

  function resetCredentialDraft(service: CredentialServiceKey) {
    credentialDraft[service].apiKey = ''
    credentialDraft[service].username = ''
    credentialDraft[service].password = ''
  }

  function credentialModeCandidates(): CredentialEndpointMode[] {
    if (credentialEndpointMode.value === 'browser') {
      return ['browser', 'sync']
    }
    if (credentialEndpointMode.value === 'sync') {
      return ['sync', 'browser']
    }
    return ['browser', 'sync']
  }

  function shouldTryFallbackMode(error: unknown, index: number, modes: CredentialEndpointMode[]) {
    if (index >= modes.length - 1) {
      return false
    }
    return isRouteNotFoundError(error)
  }

  async function loadCredentials() {
    const modes: CredentialEndpointMode[] = ['browser', 'sync']

    for (const mode of modes) {
      try {
        const path = mode === 'browser' ? '/v1/credentials' : '/v1/sync/credentials'
        const response = await apiGet<CredentialListData>(path)
        if (!response.success) {
          continue
        }

        const next: Partial<Record<CredentialServiceKey, CredentialItem>> = {}
        for (const item of extractCredentialItems(response.data)) {
          next[item.service] = item
        }
        credentials.value = next
        credentialEndpointMode.value = mode
        return
      } catch {
        // Try next credential endpoint shape.
      }
    }

    credentials.value = {}
  }

  async function saveCredential(service: CredentialServiceDefinition) {
    credentialSaving[service.key] = true
    try {
      const draft = credentialDraft[service.key]
      const credentialType: CredentialKind = service.key === 'qrz' ? qrzCredentialMode.value : service.credentialType
      const value =
        credentialType === 'username_password'
          ? service.key === 'qrz'
            ? `${draft.username.trim()}:${draft.password}`
            : JSON.stringify({
                username: draft.username.trim(),
                password: draft.password,
              })
          : service.key === 'qrz'
            ? JSON.stringify({ api_key: draft.apiKey.trim() })
            : draft.apiKey.trim()

      const payload = {
        credential_type: credentialType,
        value,
      }

      let saveResponse: CredentialItem | undefined
      let saveError: unknown = null
      const modes = credentialModeCandidates()

      for (let index = 0; index < modes.length; index += 1) {
        const mode = modes[index]
        try {
          if (mode === 'browser') {
            const response = await apiPost<CredentialItem, { service: CredentialServiceKey; credential_type: CredentialKind; value: string }>(
              '/v1/credentials',
              {
                service: service.key,
                ...payload,
              },
            )
            if (!response.success) {
              throw new Error(response.error || `Could not save ${service.label} credentials`)
            }
            saveResponse = response.data
            credentialEndpointMode.value = 'browser'
            break
          }

          const response = await apiPut<CredentialItem, typeof payload>(`/v1/sync/credentials/${service.key}`, payload)
          if (!response.success) {
            throw new Error(response.error || `Could not save ${service.label} credentials`)
          }
          saveResponse = response.data
          credentialEndpointMode.value = 'sync'
          break
        } catch (error) {
          saveError = error
          if (!shouldTryFallbackMode(error, index, modes)) {
            break
          }
        }
      }

      if (!saveResponse) {
        throw saveError || new Error(`Could not save ${service.label} credentials`)
      }

      resetCredentialDraft(service.key)
      await loadCredentials()
      if (saveResponse.verified === false) {
        $q.notify({
          type: 'warning',
          message: saveResponse.verification_error || `${service.label} credentials saved but could not be verified`,
        })
      } else {
        $q.notify({ type: 'positive', message: `${service.label} credentials saved` })
      }
    } catch (error) {
      $q.notify({ type: 'negative', message: getApiErrorMessage(error, 'Could not save credentials') })
    } finally {
      credentialSaving[service.key] = false
    }
  }

  async function reVerifyCredential(service: CredentialServiceDefinition) {
    credentialVerifying[service.key] = true
    try {
      const path = `/v1/sync/credentials/${service.key}/verify`
      const response = await apiPost<{ service: string; last_verified_at: string }>(path, {})
      if (!response.success) {
        throw new Error(response.error || `Could not verify ${service.label} credentials`)
      }

      await loadCredentials()
      $q.notify({ type: 'positive', message: `${service.label} credentials verified` })
    } catch (error) {
      $q.notify({ type: 'negative', message: getApiErrorMessage(error, 'Could not verify credentials') })
    } finally {
      credentialVerifying[service.key] = false
    }
  }

  async function removeCredential(service: CredentialServiceDefinition) {
    const confirmed = await new Promise<boolean>((resolve) => {
      $q.dialog({
        title: `Remove ${service.label} credentials?`,
        message: 'This will disconnect the service for your account.',
        cancel: true,
        ok: { color: 'negative', label: 'Remove' },
      })
        .onOk(() => resolve(true))
        .onCancel(() => resolve(false))
        .onDismiss(() => resolve(false))
    })

    if (!confirmed) {
      return
    }

    credentialRemoving[service.key] = true
    try {
      const modes = credentialModeCandidates()
      let removed = false
      let removeError: unknown = null

      for (let index = 0; index < modes.length; index += 1) {
        const mode = modes[index]
        try {
          const path = mode === 'browser' ? `/v1/credentials/${service.key}` : `/v1/sync/credentials/${service.key}`
          const response = await apiDelete(path)
          if (!response.success) {
            throw new Error(response.error || `Could not remove ${service.label} credentials`)
          }
          credentialEndpointMode.value = mode
          removed = true
          break
        } catch (error) {
          removeError = error
          if (!shouldTryFallbackMode(error, index, modes)) {
            break
          }
        }
      }

      if (!removed) {
        throw removeError || new Error(`Could not remove ${service.label} credentials`)
      }

      resetCredentialDraft(service.key)
      await loadCredentials()
      $q.notify({ type: 'positive', message: `${service.label} disconnected` })
    } catch (error) {
      $q.notify({ type: 'negative', message: getApiErrorMessage(error, 'Could not remove credentials') })
    } finally {
      credentialRemoving[service.key] = false
    }
  }

  watch(
    () => credentials.value.qrz?.credential_type,
    (credentialType) => {
      qrzCredentialMode.value = credentialType === 'username_password' ? 'username_password' : 'api_key'
    },
    { immediate: true },
  )

  return {
    credentials,
    credentialDraft,
    credentialRemoving,
    credentialSaving,
    credentialServices,
    credentialVerifying,
    getCredentialStatus,
    getServiceStatusMeta,
    isServiceConnected,
    canSaveCredential,
    loadCredentials,
    qrzCredentialMode,
    qrzCredentialModeOptions,
    reVerifyCredential,
    removeCredential,
    saveCredential,
  }
}
