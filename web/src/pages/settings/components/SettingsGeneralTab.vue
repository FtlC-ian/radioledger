<template>
  <div class="column q-gutter-md">
    <q-card flat bordered>
      <q-card-section>
        <div class="text-subtitle1 text-weight-medium q-mb-md">Profile</div>

        <div class="row q-col-gutter-md">
          <q-input class="col-12 col-md-4" :model-value="profile.callsign || '—'" outlined dense label="Callsign" readonly>
            <template #hint>
              {{ profile.callsign ? 'Registered callsign is read-only.' : 'Set during registration.' }}
            </template>
          </q-input>

          <q-input class="col-12 col-md-4" :model-value="profile.email || '—'" outlined dense label="Email" readonly />

          <q-input class="col-12 col-md-4" v-model="form.display_name" outlined dense label="Display name" />

          <q-select
            class="col-12 col-md-6"
            v-model="form.timezone"
            :options="timezoneOptions"
            use-input
            fill-input
            hide-selected
            input-debounce="0"
            outlined
            dense
            label="Timezone"
            @filter="filterTimezoneOptions"
          />

          <q-input class="col-12 col-md-6" v-model="form.default_grid" outlined dense label="Default grid (optional)" />
        </div>
      </q-card-section>
    </q-card>

    <q-card flat bordered>
      <q-card-section>
        <div class="text-subtitle1 text-weight-medium q-mb-md">Logbook</div>

        <div class="row q-col-gutter-md">
          <q-select
            class="col-6 col-md-3"
            v-model="form.default_band"
            :options="bandOptions"
            emit-value
            map-options
            outlined
            dense
            label="Default band"
          />

          <q-select
            class="col-6 col-md-3"
            v-model="form.default_mode"
            :options="modeOptions"
            emit-value
            map-options
            outlined
            dense
            label="Default mode"
          />

          <q-input
            class="col-6 col-md-3"
            v-model.number="form.default_power"
            type="number"
            outlined
            dense
            label="Default power (W)"
          />

          <q-input
            class="col-6 col-md-3"
            v-model.number="form.dedup_window"
            type="number"
            outlined
            dense
            label="Dedup window (sec)"
          />
        </div>

        <div class="row q-col-gutter-md q-mt-sm">
          <q-select
            class="col-12 col-md-6"
            v-model="form.itu_region"
            :options="ituRegionOptions"
            emit-value
            map-options
            outlined
            dense
            label="ITU region for band defaults"
            hint="Auto-detect works from your callsign prefix when possible. Override it here if needed."
          />
          <div class="col-12 col-md-6 text-caption text-grey-6 self-center">
            Current detection source: {{ bandModePreferences.regionSource === 'explicit' ? 'manual override' : bandModePreferences.regionSource === 'callsign_prefix' ? 'callsign prefix' : 'fallback defaults' }}
          </div>
        </div>

        <div class="row q-col-gutter-md q-mt-sm">
          <div class="col-12 col-md-6">
            <q-card flat bordered>
              <q-card-section>
                <div class="text-subtitle2 q-mb-sm">Visible bands</div>
                <div class="row q-col-gutter-sm">
                  <div v-for="band in bandModePreferences.bands" :key="band.name" class="col-6 col-sm-4">
                    <q-checkbox v-model="form.visible_bands" :val="band.name" :label="band.label" />
                  </div>
                </div>
              </q-card-section>
            </q-card>
          </div>
          <div class="col-12 col-md-6">
            <q-card flat bordered>
              <q-card-section>
                <div class="text-subtitle2 q-mb-sm">Visible modes</div>
                <div class="row q-col-gutter-sm">
                  <div v-for="mode in bandModePreferences.modes" :key="mode.name" class="col-6 col-sm-4">
                    <q-checkbox v-model="form.visible_modes" :val="mode.name" :label="mode.label" />
                  </div>
                </div>
              </q-card-section>
            </q-card>
          </div>
        </div>
      </q-card-section>
    </q-card>

    <q-card flat bordered>
      <q-card-section>
        <div class="text-subtitle1 text-weight-medium q-mb-md">Appearance</div>

        <div class="row q-col-gutter-md items-start">
          <div class="col-12 col-md-5">
            <q-btn-toggle
              v-model="form.ui_theme"
              spread
              unelevated
              color="primary"
              toggle-color="primary"
              :options="[
                { label: 'Dark', value: 'dark' },
                { label: 'Light', value: 'light' },
                { label: 'System', value: 'system' },
              ]"
            />
          </div>

          <div class="col-12 col-md-7">
            <q-card flat bordered class="theme-preview" :class="themePreviewClass">
              <q-card-section>
                <div class="text-caption text-grey-6">Theme preview</div>
                <div class="text-subtitle2 q-mt-xs">{{ themePreviewLabel }}</div>
                <div class="text-caption q-mt-xs">A quick preview of your current theme preference.</div>
              </q-card-section>
            </q-card>
          </div>
        </div>
      </q-card-section>
    </q-card>

    <q-card flat bordered>
      <q-card-section>
        <div class="row items-center justify-between q-mb-md">
          <div>
            <div class="text-subtitle1 text-weight-medium">API keys</div>
            <div class="text-caption text-grey-5">Create personal keys for automation and integrations.</div>
          </div>
          <q-btn color="primary" icon="key" label="Create key" @click="showCreateKeyDialog = true" />
        </div>

        <q-table
          :rows="apiKeys"
          :columns="apiKeyColumns"
          row-key="uuid"
          dense
          flat
          no-data-label="No API keys yet"
          :rows-per-page-options="[10, 25, 50]"
        >
          <template #body-cell-created_at="props">
            <q-td :props="props">{{ formatDateTime(props.row.created_at) }}</q-td>
          </template>
          <template #body-cell-last_used_at="props">
            <q-td :props="props">{{ props.row.last_used_at ? formatDateTime(props.row.last_used_at) : 'Never' }}</q-td>
          </template>
          <template #body-cell-actions="props">
            <q-td :props="props" class="text-right">
              <q-btn flat dense color="negative" icon="delete" @click="revokeApiKey(props.row.uuid)" />
            </q-td>
          </template>
        </q-table>
      </q-card-section>
    </q-card>

    <q-card flat bordered class="danger-zone">
      <q-card-section>
        <div class="text-subtitle1 text-weight-medium q-mb-sm text-negative">Danger zone</div>
        <div class="text-caption text-grey-5 q-mb-md">Export your data or permanently schedule account deletion.</div>
        <div class="row q-gutter-sm">
          <q-btn color="secondary" icon="download" label="Export data" @click="exportAllData" />
          <q-btn color="negative" outline icon="delete_forever" label="Delete account" @click="confirmDeleteAccount" />
        </div>
      </q-card-section>
    </q-card>

    <q-dialog v-model="showCreateKeyDialog">
      <q-card style="min-width: min(440px, 94vw)">
        <q-card-section>
          <div class="text-h6">Create API key</div>
        </q-card-section>

        <q-card-section class="q-gutter-md">
          <q-input v-model="keyDraft.name" outlined dense label="Key name" />
          <q-select
            v-model="keyDraft.scopes"
            outlined
            dense
            multiple
            emit-value
            map-options
            label="Scopes"
            :options="scopeOptions"
          />
        </q-card-section>

        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn color="primary" label="Create" :loading="creatingKey" :disable="!keyDraft.name.trim()" @click="createApiKey" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { type QTableColumn, useQuasar } from 'quasar'
import api, { apiDelete, apiGet, apiPost, apiPut } from 'src/api/client'
import { useBandModePreferencesStore } from 'src/stores/bandModePreferences'
import { applyThemePreference, getStoredThemePreference } from 'src/utils/themePreference'

interface PreferencesResponse {
  display_name: string | null
  timezone: string
  default_grid: string | null
  default_band: string
  default_mode: string
  default_power: number | null
  ui_theme: 'dark' | 'light' | 'system'
  dedup_window: number
  sync_enabled: boolean
  desktop_udp_port: number
  desktop_rig_port: number
  itu_region?: number | null
  visible_bands?: string[]
  visible_modes?: string[]
}

interface ApiKeyItem {
  uuid: string
  name: string
  key_prefix: string
  scopes?: string[]
  created_at: string
  last_used_at?: string
}

interface UserProfile {
  email?: string
  callsign?: string
}

const $q = useQuasar()
const bandModePreferences = useBandModePreferencesStore()

const saving = ref(false)
const creatingKey = ref(false)
const showCreateKeyDialog = ref(false)

const form = reactive<PreferencesResponse>({
  display_name: null,
  timezone: 'UTC',
  default_grid: null,
  default_band: '20M',
  default_mode: 'SSB',
  default_power: 100,
  ui_theme: 'dark',
  dedup_window: 300,
  sync_enabled: false,
  desktop_udp_port: 2237,
  desktop_rig_port: 4532,
  itu_region: null,
  visible_bands: [],
  visible_modes: [],
})

const profile = ref<UserProfile>({})
const apiKeys = ref<ApiKeyItem[]>([])

const keyDraft = reactive({
  name: 'Desktop automation',
  scopes: ['read', 'write'] as string[],
})

const fallbackBandOptions = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m', '2m', '70cm'].map(
  (band) => ({ label: band.toUpperCase(), value: band.toUpperCase() }),
)

const fallbackModeOptions = ['SSB', 'CW', 'FM', 'AM', 'FT8', 'FT4', 'RTTY', 'PSK31'].map((mode) => ({
  label: mode,
  value: mode,
}))

const bandOptions = computed(() =>
  bandModePreferences.allBandOptions.length
    ? bandModePreferences.allBandOptions.map((option) => ({ label: option.label.toUpperCase(), value: option.value.toUpperCase() }))
    : fallbackBandOptions,
)

const modeOptions = computed(() =>
  bandModePreferences.allModeOptions.length ? bandModePreferences.allModeOptions : fallbackModeOptions,
)

const ituRegionOptions = [
  { label: 'Auto-detect from callsign / DXCC', value: null },
  { label: 'Region 1 — Europe, Africa, Middle East', value: 1 },
  { label: 'Region 2 — Americas', value: 2 },
  { label: 'Region 3 — Asia / Pacific', value: 3 },
]

const scopeOptions = [
  { label: 'Read', value: 'read' },
  { label: 'Write', value: 'write' },
  { label: 'Import', value: 'import' },
  { label: 'Export', value: 'export' },
]

const apiKeyColumns: QTableColumn<ApiKeyItem>[] = [
  { name: 'key_prefix', label: 'Prefix', field: 'key_prefix', align: 'left', sortable: true },
  { name: 'name', label: 'Name', field: 'name', align: 'left', sortable: true },
  { name: 'created_at', label: 'Created', field: 'created_at', align: 'left', sortable: true },
  { name: 'last_used_at', label: 'Last used', field: 'last_used_at', align: 'left', sortable: true },
  { name: 'actions', label: '', field: () => '', align: 'right' },
]

const allTimezones =
  typeof Intl !== 'undefined' && typeof Intl.supportedValuesOf === 'function'
    ? Intl.supportedValuesOf('timeZone')
    : ['UTC', 'America/Chicago', 'America/New_York', 'Europe/London', 'Asia/Tokyo']

const timezoneOptions = ref<string[]>([...allTimezones])

const themePreviewClass = computed(() => {
  if (form.ui_theme === 'light') {
    return 'theme-preview--light'
  }
  if (form.ui_theme === 'system') {
    return 'theme-preview--system'
  }
  return 'theme-preview--dark'
})

const themePreviewLabel = computed(() => {
  if (form.ui_theme === 'light') {
    return 'Light mode'
  }
  if (form.ui_theme === 'system') {
    return 'System preference'
  }
  return 'Dark mode'
})

watch(
  () => form.ui_theme,
  (theme) => {
    applyThemePreference($q, theme)
  },
)

watch(
  () => bandOptions.value,
  (options) => {
    if (!options.length) {
      return
    }
    if (!options.some((option) => option.value === form.default_band)) {
      form.default_band = options[0]?.value || form.default_band
    }
    const validBands = new Set(bandModePreferences.bands.map((band) => band.name))
    form.visible_bands = (form.visible_bands || []).filter((band) => validBands.has(band))
    if (!form.visible_bands.length) {
      form.visible_bands = bandModePreferences.visibleBands.map((band) => band.name)
    }
  },
  { immediate: true },
)

watch(
  () => modeOptions.value,
  (options) => {
    if (!options.length) {
      return
    }
    if (!options.some((option) => option.value === form.default_mode)) {
      form.default_mode = options[0]?.value || form.default_mode
    }
    const validModes = new Set(bandModePreferences.modes.map((mode) => mode.name))
    form.visible_modes = (form.visible_modes || []).filter((mode) => validModes.has(mode))
    if (!form.visible_modes.length) {
      form.visible_modes = bandModePreferences.visibleModes.map((mode) => mode.name)
    }
  },
  { immediate: true },
)

function filterTimezoneOptions(val: string, update: (fn: () => void) => void) {
  update(() => {
    if (!val) {
      timezoneOptions.value = [...allTimezones]
      return
    }
    const needle = val.toLowerCase()
    timezoneOptions.value = allTimezones.filter((tz) => tz.toLowerCase().includes(needle))
  })
}

function syncVisibilityFormFromStore(preferences?: PreferencesResponse) {
  form.itu_region = preferences?.itu_region ?? bandModePreferences.ituRegion ?? null
  form.visible_bands = preferences?.visible_bands?.length
    ? [...preferences.visible_bands]
    : bandModePreferences.visibleBands.map((band) => band.name)
  form.visible_modes = preferences?.visible_modes?.length
    ? [...preferences.visible_modes]
    : bandModePreferences.visibleModes.map((mode) => mode.name)
}

async function loadProfile() {
  try {
    const response = await apiGet<UserProfile>('/v1/auth/me')
    if (response.success && response.data) {
      profile.value = response.data
    }
  } catch {
    profile.value = {}
  }
}

async function loadPreferences() {
  try {
    const response = await apiGet<PreferencesResponse>('/v1/preferences')
    if (response.success && response.data) {
      Object.assign(form, response.data)
      syncVisibilityFormFromStore(response.data)

      const storedThemePreference = getStoredThemePreference()
      if (storedThemePreference) {
        form.ui_theme = storedThemePreference
      }

      applyThemePreference($q, form.ui_theme)
    }
  } catch {
    const storedThemePreference = getStoredThemePreference()
    if (storedThemePreference) {
      form.ui_theme = storedThemePreference
      applyThemePreference($q, storedThemePreference)
    }
    $q.notify({ type: 'warning', message: 'Could not load preferences' })
  }
}

async function savePreferences() {
  saving.value = true
  try {
    if (!form.visible_bands?.length) {
      throw new Error('Select at least one visible band')
    }
    if (!form.visible_modes?.length) {
      throw new Error('Select at least one visible mode')
    }

    const payload = {
      display_name: form.display_name,
      timezone: form.timezone,
      default_grid: form.default_grid,
      default_band: form.default_band,
      default_mode: form.default_mode,
      default_power: form.default_power,
      ui_theme: form.ui_theme,
      dedup_window: form.dedup_window,
      sync_enabled: form.sync_enabled,
      desktop_udp_port: form.desktop_udp_port,
      desktop_rig_port: form.desktop_rig_port,
      itu_region: form.itu_region,
      visible_bands: form.visible_bands,
      visible_modes: form.visible_modes,
    }

    const response = await apiPut<PreferencesResponse, typeof payload>('/v1/preferences', payload)
    if (!response.success) {
      throw new Error(response.error || 'Could not save preferences')
    }

    await bandModePreferences.load(true)
    syncVisibilityFormFromStore(response.data)
    $q.notify({ type: 'positive', message: 'Preferences saved' })
  } catch (error) {
    $q.notify({ type: 'negative', message: error instanceof Error ? error.message : 'Could not save preferences' })
  } finally {
    saving.value = false
  }
}

async function loadApiKeys() {
  try {
    const response = await apiGet<{ items: ApiKeyItem[] }>('/v1/api-keys')
    apiKeys.value = response.success && response.data?.items ? response.data.items : []
  } catch {
    apiKeys.value = []
  }
}

async function createApiKey() {
  creatingKey.value = true
  try {
    const response = await apiPost<{ key: string; uuid: string }, { name: string; scopes: string[] }>('/v1/api-keys', {
      name: keyDraft.name,
      scopes: keyDraft.scopes,
    })

    if (!response.success || !response.data?.key) {
      throw new Error(response.error || 'Could not create API key')
    }

    showCreateKeyDialog.value = false
    await loadApiKeys()

    $q.dialog({
      title: 'API key created',
      message: `Copy this key now (it will not be shown again):\n\n${response.data.key}`,
    })
  } catch (error) {
    $q.notify({ type: 'negative', message: error instanceof Error ? error.message : 'Could not create API key' })
  } finally {
    creatingKey.value = false
  }
}

async function revokeApiKey(uuid: string) {
  const confirmed = await new Promise<boolean>((resolve) => {
    $q.dialog({
      title: 'Revoke API key?',
      message: 'This key will stop working immediately.',
      cancel: true,
      ok: { color: 'negative', label: 'Revoke' },
    })
      .onOk(() => resolve(true))
      .onCancel(() => resolve(false))
      .onDismiss(() => resolve(false))
  })

  if (!confirmed) {
    return
  }

  try {
    await apiDelete(`/v1/api-keys/${uuid}`)
    await loadApiKeys()
    $q.notify({ type: 'positive', message: 'API key revoked' })
  } catch {
    $q.notify({ type: 'negative', message: 'Could not revoke API key' })
  }
}

async function exportAllData() {
  try {
    const response = await api.get('/v1/export/adif', { responseType: 'blob' })
    const blob = new Blob([response.data], { type: 'application/octet-stream' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `radioledger-export-${new Date().toISOString().slice(0, 10)}.adi`
    anchor.click()
    URL.revokeObjectURL(url)
  } catch {
    $q.notify({ type: 'negative', message: 'Could not export ADIF data' })
  }
}

function confirmDeleteAccount() {
  $q.dialog({
    title: 'Delete account?',
    message: 'Type DELETE to confirm account deletion.',
    prompt: {
      model: '',
      type: 'text',
      isValid: (val: string) => val === 'DELETE',
    },
    cancel: true,
    persistent: true,
    ok: {
      label: 'Delete account',
      color: 'negative',
    },
  }).onOk(async () => {
    try {
      const response = await apiDelete('/v1/auth/me')
      if (response.success) {
        $q.notify({ type: 'positive', message: 'Account scheduled for deletion' })
      } else {
        $q.notify({ type: 'negative', message: response.error || 'Could not delete account' })
      }
    } catch {
      $q.notify({ type: 'negative', message: 'Could not delete account' })
    }
  })
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

onMounted(async () => {
  await Promise.all([bandModePreferences.load(), loadProfile(), loadPreferences(), loadApiKeys()])
  syncVisibilityFormFromStore(form)
})

defineExpose({
  savePreferences,
  saving,
})
</script>

<style scoped>
.theme-preview {
  border-radius: 12px;
}

.theme-preview--dark {
  background: linear-gradient(135deg, rgba(30, 58, 138, 0.35), rgba(6, 78, 59, 0.32));
}

.theme-preview--light {
  background: linear-gradient(135deg, rgba(226, 232, 240, 0.9), rgba(248, 250, 252, 0.95));
  color: #1f2937;
}

.theme-preview--system {
  background: linear-gradient(135deg, rgba(30, 41, 59, 0.4), rgba(148, 163, 184, 0.35));
}

.danger-zone {
  border-color: rgba(239, 68, 68, 0.55);
}
</style>
