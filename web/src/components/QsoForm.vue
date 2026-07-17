<template>
  <q-form class="q-gutter-md" @submit.prevent="onSubmit">
    <div class="row q-col-gutter-md">
      <!-- Callsign with autocomplete -->
      <div class="col-12 col-md-6">
        <q-select
          outlined
          dense
          use-input
          hide-selected
          fill-input
          input-debounce="200"
          label="Callsign *"
          :model-value="form.callsign"
          :options="callsignOptions"
          :rules="[(val) => Boolean(val) || 'Callsign is required']"
          @update:model-value="onCallsignSelected"
          @input-value="onCallsignInput"
          @blur="onCallsignBlur"
          @keydown.enter.prevent="onCallsignBlur"
        >
          <template #no-option>
            <q-item>
              <q-item-section class="text-grey">
                {{ form.callsign.length >= 2 ? 'No cached callsigns found' : 'Type to search' }}
              </q-item-section>
            </q-item>
          </template>
          <template #option="scope">
            <q-item v-bind="scope.itemProps">
              <q-item-section>
                <q-item-label>{{ scope.opt.callsign }}</q-item-label>
                <q-item-label caption>
                  {{ scope.opt.full_name }}
                  <span v-if="scope.opt.grid" class="text-primary"> · {{ scope.opt.grid }}</span>
                </q-item-label>
              </q-item-section>
            </q-item>
          </template>
        </q-select>
      </div>

      <q-select
        class="col-6 col-md-3"
        outlined
        dense
        emit-value
        map-options
        label="Band"
        :options="bandOptions"
        v-model="form.band"
      />

      <q-select
        class="col-6 col-md-3"
        outlined
        dense
        emit-value
        map-options
        label="Mode"
        :options="modeOptions"
        v-model="form.mode"
      />

      <q-input class="col-6 col-md-4" outlined dense label="Frequency" type="number" v-model.number="form.frequency" />
      <q-input class="col-6 col-md-4" outlined dense label="Date/Time (UTC)" type="datetime-local" v-model="form.qso_datetime" />
      <q-input class="col-6 col-md-2" outlined dense label="RST Sent" v-model="form.rst_sent" />
      <q-input class="col-6 col-md-2" outlined dense label="RST Rcvd" v-model="form.rst_rcvd" />

      <q-input class="col-6 col-md-4" outlined dense label="Grid Square" v-model="form.grid" />
      <q-input class="col-6 col-md-4" outlined dense label="Power (W)" type="number" v-model.number="form.power" />
      <q-input class="col-12 col-md-4" outlined dense label="Country" v-model="form.country" />
      <q-input class="col-12" outlined dense type="textarea" label="Comment" v-model="form.comment" />
    </div>

    <!-- Callsign info card — shown when lookup returns data -->
    <q-card
      v-if="callsignInfo"
      flat
      bordered
      class="q-mt-sm callsign-info-card"
    >
      <q-card-section class="q-pa-sm">
        <div class="row items-center q-gutter-sm">
          <!-- Avatar / photo -->
          <q-avatar v-if="callsignInfo.image" size="48px">
            <img :src="callsignInfo.image" :alt="callsignInfo.callsign" />
          </q-avatar>
          <q-avatar v-else size="40px" color="primary" text-color="white" icon="person" />

          <div class="col">
            <div class="text-subtitle2 text-weight-bold">
              {{ callsignInfo.callsign }}
              <q-badge v-if="callsignInfo.class" color="teal" class="q-ml-xs">{{ callsignInfo.class }}</q-badge>
            </div>
            <div v-if="callsignInfo.full_name" class="text-body2">{{ callsignInfo.full_name }}</div>
            <div class="text-caption text-grey-7">
              <span v-if="callsignInfo.addr2">{{ callsignInfo.addr2 }}</span>
              <span v-if="callsignInfo.country"> · {{ callsignInfo.country }}</span>
              <span v-if="callsignInfo.grid" class="text-primary"> · {{ callsignInfo.grid }}</span>
              <span v-if="callsignInfo.land && callsignInfo.country !== callsignInfo.land">
                · {{ callsignInfo.land }}
              </span>
            </div>
          </div>

          <!-- QSL manager -->
          <div v-if="callsignInfo.qsl_mgr" class="text-caption text-grey-7">
            QSL: {{ callsignInfo.qsl_mgr }}
          </div>

          <!-- Loading spinner while looking up -->
          <q-spinner v-if="lookupLoading" color="primary" size="24px" />

          <!-- Close button -->
          <q-btn flat round dense icon="close" size="xs" @click="callsignInfo = null" />
        </div>
      </q-card-section>
    </q-card>

    <!-- Lookup error message (not found / no credentials) -->
    <q-banner
      v-if="lookupMessage && !callsignInfo"
      dense
      class="text-caption q-mt-xs"
      :class="lookupMessageType === 'info' ? 'bg-blue-1 text-blue-9' : 'bg-orange-1 text-orange-9'"
    >
      <template #avatar>
        <q-icon :name="lookupMessageType === 'info' ? 'info' : 'warning'" />
      </template>
      {{ lookupMessage }}
    </q-banner>

    <div class="row justify-end q-gutter-sm">
      <q-btn flat color="secondary" label="Cancel" @click="$emit('cancel')" />
      <q-btn color="primary" type="submit" :loading="loading" :label="submitLabel" />
    </div>
  </q-form>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import type { QsoPayload } from 'src/types/qso'
import { apiGet } from 'src/api/client'
import { useBandModePreferencesStore } from 'src/stores/bandModePreferences'

interface Props {
  modelValue?: Partial<QsoPayload>
  loading?: boolean
  submitLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  modelValue: () => ({}),
  loading: false,
  submitLabel: 'Save QSO',
})

const emit = defineEmits<{
  (e: 'submit', payload: QsoPayload): void
  (e: 'cancel'): void
}>()

// ─── Autocomplete ──────────────────────────────────────────────────────────

interface AutocompleteItem {
  callsign: string
  full_name?: string
  grid?: string
}

interface CallsignInfo {
  callsign: string
  full_name?: string
  fname?: string
  lname?: string
  addr2?: string
  country?: string
  grid?: string
  class?: string
  land?: string
  qsl_mgr?: string
  image?: string
  source?: string
}

const callsignOptions = ref<AutocompleteItem[]>([])
const callsignInfo = ref<CallsignInfo | null>(null)
const lookupLoading = ref(false)
const lookupMessage = ref('')
const lookupMessageType = ref<'info' | 'warning'>('info')

let autocompleteAbort: AbortController | null = null
let lookupDebounceTimer: ReturnType<typeof setTimeout> | null = null

async function fetchAutocomplete(prefix: string) {
  if (prefix.length < 2) {
    callsignOptions.value = []
    return
  }
  autocompleteAbort?.abort()
  autocompleteAbort = new AbortController()
  try {
    const resp = await apiGet<{ items: AutocompleteItem[] }>(
      `/v1/callsigns/autocomplete?q=${encodeURIComponent(prefix.toUpperCase())}`,
    )
    if (resp.success && resp.data?.items) {
      callsignOptions.value = resp.data.items
    }
  } catch {
    callsignOptions.value = []
  }
}

async function fetchCallsignInfo(callsign: string) {
  if (!callsign || callsign.length < 3) return

  lookupLoading.value = true
  lookupMessage.value = ''
  callsignInfo.value = null

  try {
    const resp = await apiGet<CallsignInfo>(`/v1/lookup/${encodeURIComponent(callsign.toUpperCase())}`)
    if (resp.success && resp.data) {
      const info = resp.data as CallsignInfo
      callsignInfo.value = info

      // Auto-fill grid and country from lookup (only if currently empty).
      if (info.grid && !form.grid) {
        form.grid = info.grid
      }
      if (info.country && !form.country) {
        form.country = info.country
      }
    } else {
      // success:false — show a soft message, not an error.
      const msg = resp.message || ''
      if (msg.toLowerCase().includes('not found')) {
        lookupMessage.value = `${callsign} not found in callbook`
        lookupMessageType.value = 'info'
      } else if (msg.toLowerCase().includes('credentials')) {
        lookupMessage.value = 'QRZ lookup unavailable — add QRZ credentials in Settings'
        lookupMessageType.value = 'info'
      } else {
        lookupMessage.value = msg
        lookupMessageType.value = 'warning'
      }
    }
  } catch {
    // Silent fail — lookup is best-effort enhancement, not required for QSO entry.
    lookupMessage.value = ''
  } finally {
    lookupLoading.value = false
  }
}

// Called when the user types in the callsign field (autocomplete input).
function onCallsignInput(value: string) {
  const upper = String(value || '').toUpperCase()
  form.callsign = upper

  // Clear stale info card when the callsign changes.
  if (callsignInfo.value && callsignInfo.value.callsign !== upper) {
    callsignInfo.value = null
    lookupMessage.value = ''
  }

  fetchAutocomplete(upper)
}

// Called when an autocomplete option is selected from the dropdown.
function onCallsignSelected(value: AutocompleteItem | string | null) {
  if (!value) return
  const callsign = typeof value === 'string' ? value.toUpperCase() : value.callsign.toUpperCase()
  form.callsign = callsign

  // Pre-fill from autocomplete data if available.
  if (typeof value === 'object' && value.grid && !form.grid) {
    form.grid = value.grid
  }

  // Trigger a full lookup for the name/location card.
  void fetchCallsignInfo(callsign)
}

// Called on tab/blur to do a full lookup of the typed callsign.
function onCallsignBlur() {
  const callsign = form.callsign.trim().toUpperCase()
  if (!callsign || callsign.length < 3) return
  if (callsignInfo.value?.callsign === callsign) return // already loaded

  // Debounce to avoid firing twice on select+blur.
  if (lookupDebounceTimer) clearTimeout(lookupDebounceTimer)
  lookupDebounceTimer = setTimeout(() => {
    void fetchCallsignInfo(callsign)
  }, 150)
}

// ─── Form ──────────────────────────────────────────────────────────────────

const bandModePreferences = useBandModePreferencesStore()
const fallbackBandOptions = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m', '2m', '70cm'].map(
  (band) => ({ label: band, value: band }),
)

// Common center frequencies (kHz) for each band — used for auto-fill.
const BAND_FREQ_KHZ: Record<string, number> = {
  '160m': 1900,
  '80m':  3750,
  '60m':  5332,
  '40m':  7150,
  '30m':  10125,
  '20m':  14225,
  '17m':  18118,
  '15m':  21200,
  '12m':  24950,
  '10m':  28400,
  '6m':   50200,
  '2m':   144200,
  '70cm': 432100,
}

const fallbackModeOptions = ['SSB', 'CW', 'FM', 'AM', 'FT8', 'FT4', 'RTTY', 'PSK31'].map((mode) => ({
  label: mode,
  value: mode,
}))

const bandOptions = computed(() => (bandModePreferences.bandOptions.length ? bandModePreferences.bandOptions : fallbackBandOptions))
const modeOptions = computed(() => (bandModePreferences.modeOptions.length ? bandModePreferences.modeOptions : fallbackModeOptions))

const form = reactive<QsoPayload>({
  qso_datetime: toDateTimeLocalUtc(new Date().toISOString()),
  callsign: '',
  band: '20m',
  mode: 'SSB',
  frequency: null,
  rst_sent: '59',
  rst_rcvd: '59',
  grid: '',
  country: '',
  power: null,
  comment: '',
})

onMounted(() => {
  void bandModePreferences.load()
})

watch(
  () => props.modelValue,
  (value) => {
    form.qso_datetime = toDateTimeLocalUtc(value.qso_datetime || new Date().toISOString())
    form.callsign = value.callsign || ''
    form.band = value.band || '20m'
    form.mode = value.mode || 'SSB'
    form.frequency = value.frequency ?? null
    form.rst_sent = value.rst_sent ?? '59'
    form.rst_rcvd = value.rst_rcvd ?? '59'
    form.grid = value.grid ?? ''
    form.country = value.country ?? ''
    form.power = value.power ?? null
    form.comment = value.comment ?? ''
  },
  { immediate: true },
)

watch(
  () => bandOptions.value,
  (options) => {
    if (!options.length) {
      return
    }
    if (!options.some((option) => option.value === form.band)) {
      form.band = options[0]?.value || form.band
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
    if (!options.some((option) => option.value === form.mode)) {
      form.mode = options[0]?.value || form.mode
    }
  },
  { immediate: true },
)

// Auto-fill common frequency when band changes (only if frequency not already set or matches a band default).
watch(
  () => form.band,
  (band) => {
    const defaultFreq = BAND_FREQ_KHZ[band]
    if (defaultFreq) {
      // Only auto-fill if the field is empty or was previously auto-filled.
      const currentBands = Object.values(BAND_FREQ_KHZ)
      if (!form.frequency || currentBands.includes(form.frequency)) {
        form.frequency = defaultFreq
      }
    }
  },
)

function toDateTimeLocalUtc(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return new Date().toISOString().slice(0, 16)
  }
  return date.toISOString().slice(0, 16)
}

function onSubmit() {
  emit('submit', {
    ...form,
    qso_datetime: new Date(form.qso_datetime).toISOString(),
    callsign: form.callsign.trim().toUpperCase(),
    band: form.band,
    mode: form.mode,
    grid: form.grid?.trim() || null,
    country: form.country?.trim() || null,
    comment: form.comment?.trim() || null,
  })
}
</script>

<style scoped>
.callsign-info-card {
  border-color: var(--q-primary);
  border-radius: 8px;
}
</style>
