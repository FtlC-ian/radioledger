<template>
  <q-page class="q-pa-md activations-page">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Activations</div>
        <div class="text-body2 text-grey-5">Track your park and summit operations and export logs quickly.</div>
      </div>

      <div class="row q-gutter-sm">
        <q-btn flat icon="refresh" label="Refresh" :loading="loading" @click="refreshAll" />
        <q-btn color="primary" icon="add" label="New activation" @click="showCreateDialog = true" />
      </div>
    </div>

    <q-banner v-if="error" rounded dense class="bg-negative text-white q-mb-md">
      {{ error }}
    </q-banner>

    <q-card flat bordered class="q-mb-md tabs-shell">
      <q-tabs v-model="tab" dense align="left" active-color="primary" indicator-color="primary">
        <q-tab name="pota" label="POTA" icon="park" />
        <q-tab name="sota" label="SOTA" icon="terrain" />
      </q-tabs>
    </q-card>

    <div class="row q-col-gutter-md">
      <div class="col-12 col-lg-6">
        <q-skeleton v-if="loading" type="rect" height="240px" class="q-mb-sm" />

        <template v-else>
          <q-card v-if="activations.length === 0" flat bordered>
            <q-card-section class="text-center q-py-xl">
              <q-icon :name="tab === 'pota' ? 'park' : 'terrain'" size="48px" class="text-grey-6 q-mb-sm" />
              <div class="text-subtitle1 text-weight-medium">Plan your first activation!</div>
              <div class="text-caption text-grey-5">Create a {{ tab.toUpperCase() }} activation to start tracking progress.</div>
            </q-card-section>
          </q-card>

          <div v-else class="column q-gutter-sm">
            <q-card
              v-for="activation in activations"
              :key="activation.uuid"
              flat
              bordered
              class="activation-card cursor-pointer"
              :class="{ 'activation-card--active': selectedActivationUUID === activation.uuid }"
              @click="selectActivation(activation.uuid)"
            >
              <q-card-section>
                <div class="row items-start justify-between q-col-gutter-sm">
                  <div class="col">
                    <div class="text-subtitle1 text-weight-bold">{{ activation.reference }}</div>
                    <div class="text-caption text-grey-5">{{ formatDate(activation.activation_date) }}</div>
                  </div>
                  <div class="col-auto">
                    <q-badge :color="statusColor(activation.status)" :label="statusLabel(activation.status)" />
                  </div>
                </div>

                <div class="q-mt-sm text-caption text-grey-4">
                  {{ activation.qso_count }} QSOs · {{ activation.validation.unique_callsigns }}/{{ activation.validation.minimum_contacts }}
                </div>
                <q-linear-progress
                  class="q-mt-xs"
                  size="8px"
                  rounded
                  :value="progressValue(activation.validation)"
                  :color="progressValue(activation.validation) >= 1 ? 'positive' : 'primary'"
                />

                <div class="row q-gutter-xs q-mt-sm" v-if="activation.validation.warnings.length">
                  <q-chip
                    v-for="warning in activation.validation.warnings.slice(0, 2)"
                    :key="warning"
                    dense
                    size="sm"
                    color="amber-8"
                    text-color="black"
                  >
                    {{ warning }}
                  </q-chip>
                </div>
              </q-card-section>

              <q-separator />

              <q-card-actions align="right">
                <q-btn
                  v-if="tab === 'pota'"
                  flat
                  dense
                  icon="download"
                  label="Export"
                  :loading="exportingUUID === activation.uuid"
                  @click.stop="exportActivation(activation.uuid)"
                />
                <q-btn flat dense icon="open_in_new" label="View QSOs" @click.stop="selectActivation(activation.uuid)" />
              </q-card-actions>
            </q-card>
          </div>
        </template>
      </div>

      <div class="col-12 col-lg-6">
        <q-card flat bordered class="detail-card">
          <q-card-section v-if="selectedDetail">
            <div class="row items-start justify-between q-col-gutter-sm">
              <div class="col">
                <div class="text-h6">{{ selectedDetail.activation.reference }}</div>
                <div class="text-caption text-grey-5">{{ formatDate(selectedDetail.activation.activation_date) }}</div>
              </div>

              <div class="col-auto row q-gutter-xs items-center">
                <q-badge :color="statusColor(selectedDetail.activation.status)" :label="statusLabel(selectedDetail.activation.status)" />
                <q-btn
                  v-if="tab === 'pota'"
                  flat
                  dense
                  icon="download"
                  label="Export"
                  :loading="exportingUUID === selectedDetail.activation.uuid"
                  @click="exportActivation(selectedDetail.activation.uuid)"
                />
              </div>
            </div>

            <div class="q-mt-md">
              <div class="text-caption text-grey-5 q-mb-xs">
                Progress {{ selectedDetail.activation.validation.unique_callsigns }}/{{ selectedDetail.activation.validation.minimum_contacts }}
              </div>
              <q-linear-progress
                size="10px"
                rounded
                :value="progressValue(selectedDetail.activation.validation)"
                :color="progressValue(selectedDetail.activation.validation) >= 1 ? 'positive' : 'primary'"
              />
            </div>

            <div class="q-mt-md text-subtitle2">QSOs</div>
            <q-markup-table dense flat class="q-mt-xs qso-table">
              <thead>
                <tr>
                  <th class="text-left">UTC</th>
                  <th class="text-left">Callsign</th>
                  <th class="text-left">Band</th>
                  <th class="text-left">Mode</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="qso in selectedDetail.qsos" :key="qso.uuid">
                  <td>{{ formatDateTime(qso.datetime_on) }}</td>
                  <td>{{ qso.callsign }}</td>
                  <td>{{ qso.band }}</td>
                  <td>{{ qso.mode }}</td>
                </tr>
                <tr v-if="selectedDetail.qsos.length === 0">
                  <td colspan="4" class="text-grey-5">No QSOs linked yet.</td>
                </tr>
              </tbody>
            </q-markup-table>
          </q-card-section>

          <q-card-section v-else class="text-center q-py-xl">
            <q-icon name="playlist_add_check" size="44px" class="text-grey-6 q-mb-sm" />
            <div class="text-subtitle1 text-weight-medium">Select an activation</div>
            <div class="text-caption text-grey-5">Click an activation card to view its QSO list and export options.</div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <q-dialog v-model="showCreateDialog">
      <q-card style="min-width: min(560px, 96vw)">
        <q-card-section>
          <div class="text-h6">New {{ tab.toUpperCase() }} Activation</div>
          <div class="text-caption text-grey-5">Reference, date, and station location are enough to get started.</div>
        </q-card-section>

        <q-card-section class="q-gutter-md">
          <q-input
            v-model="createForm.reference"
            :label="tab === 'pota' ? 'Park reference (K-1234)' : 'Summit reference (W4C/WM-001)'"
            outlined
            dense
          />

          <q-input v-model="createForm.activation_date" type="date" label="Activation date" outlined dense />

          <q-select
            v-model="createForm.station_location_uuid"
            :options="locationOptions"
            emit-value
            map-options
            use-input
            fill-input
            outlined
            dense
            clearable
            label="Station location"
          />

          <q-input v-model="createForm.notes" type="textarea" autogrow outlined dense label="Notes (optional)" />
        </q-card-section>

        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn color="primary" label="Create activation" :loading="creating" @click="submitCreate" />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useQuasar } from 'quasar'
import { apiGet, apiPost } from 'src/api/client'

type ActivationValidation = {
  program: string
  reference: string
  activation_date: string
  status: string
  qso_count: number
  unique_callsigns: number
  minimum_contacts: number
  contacts_needed: number
  missing_required_fields: string[]
  s2s_count?: number
  warnings: string[]
  ready_to_submit: boolean
}

type Activation = {
  uuid: string
  logbook_uuid: string
  program: string
  reference: string
  activation_date: string
  station_location_uuid?: string
  notes?: string
  status: string
  qso_count: number
  unique_callsigns: number
  created_at: string
  updated_at: string
  validation: ActivationValidation
}

type ActivationQSO = {
  uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
}

type ActivationDetail = {
  activation: Activation
  qsos: ActivationQSO[]
}

type StationLocation = {
  uuid: string
  name: string
  callsign: string
}

type ExportPayload = {
  filename: string
  adif: string
  validation_warnings: string[]
}

const $q = useQuasar()

const tab = ref<'pota' | 'sota'>('pota')
const loading = ref(false)
const creating = ref(false)
const error = ref('')
const activations = ref<Activation[]>([])
const selectedActivationUUID = ref<string>('')
const selectedDetail = ref<ActivationDetail | null>(null)
const locations = ref<StationLocation[]>([])
const showCreateDialog = ref(false)
const exportingUUID = ref<string>('')

const createForm = ref({
  reference: '',
  activation_date: new Date().toISOString().slice(0, 10),
  station_location_uuid: '',
  notes: '',
})

const locationOptions = computed(() =>
  locations.value.map((location) => ({
    label: `${location.name} (${location.callsign})`,
    value: location.uuid,
  })),
)

function statusColor(status: string) {
  switch (status) {
    case 'valid':
      return 'positive'
    case 'submitted':
      return 'secondary'
    default:
      return 'warning'
  }
}

function statusLabel(status: string) {
  return status.replace('_', ' ')
}

async function refreshAll() {
  loading.value = true
  error.value = ''

  try {
    const listResponse = await apiGet<{ items: Activation[] }>(`/v1/activations/${tab.value}`)
    activations.value = listResponse.success && listResponse.data?.items ? listResponse.data.items : []

    if (selectedActivationUUID.value) {
      const stillExists = activations.value.some((item) => item.uuid === selectedActivationUUID.value)
      if (!stillExists) {
        selectedActivationUUID.value = ''
        selectedDetail.value = null
      }
    }

    if (selectedActivationUUID.value) {
      await fetchDetail(selectedActivationUUID.value)
    }
  } catch {
    error.value = 'Unable to load activations right now.'
  } finally {
    loading.value = false
  }
}

async function fetchLocations() {
  try {
    const response = await apiGet<{ items: StationLocation[] }>('/v1/locations')
    locations.value = response.success && response.data?.items ? response.data.items : []
  } catch {
    locations.value = []
  }
}

async function fetchDetail(activationUUID: string) {
  const response = await apiGet<ActivationDetail>(`/v1/activations/${tab.value}/${activationUUID}`)
  if (!response.success || !response.data) {
    throw new Error(response.error || 'Unable to fetch activation detail')
  }
  selectedDetail.value = response.data
}

async function selectActivation(activationUUID: string) {
  selectedActivationUUID.value = activationUUID
  try {
    await fetchDetail(activationUUID)
  } catch {
    $q.notify({ type: 'negative', message: 'Unable to load activation detail' })
  }
}

async function submitCreate() {
  creating.value = true
  try {
    const payload = {
      reference: createForm.value.reference,
      activation_date: createForm.value.activation_date,
      station_location_uuid: createForm.value.station_location_uuid || null,
      notes: createForm.value.notes || null,
    }

    const response = await apiPost<Activation, typeof payload>(`/v1/activations/${tab.value}`, payload)
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Failed to create activation')
    }

    showCreateDialog.value = false
    createForm.value.reference = ''
    createForm.value.notes = ''
    createForm.value.station_location_uuid = ''

    await refreshAll()
    selectedActivationUUID.value = response.data.uuid
    await fetchDetail(response.data.uuid)

    $q.notify({ type: 'positive', message: `${tab.value.toUpperCase()} activation created` })
  } catch (e) {
    $q.notify({ type: 'negative', message: e instanceof Error ? e.message : 'Failed to create activation' })
  } finally {
    creating.value = false
  }
}

async function exportActivation(activationUUID: string) {
  if (tab.value !== 'pota') {
    return
  }

  exportingUUID.value = activationUUID
  try {
    const response = await apiPost<ExportPayload>(`/v1/activations/pota/${activationUUID}/export`)
    if (!response.success || !response.data) {
      throw new Error(response.error || 'Export failed')
    }

    const blob = new Blob([response.data.adif || ''], { type: 'text/plain;charset=utf-8' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = response.data.filename || `pota-${activationUUID}.adi`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    if (response.data.validation_warnings?.length) {
      $q.notify({ type: 'warning', message: response.data.validation_warnings.join(' • ') })
    } else {
      $q.notify({ type: 'positive', message: 'POTA ADIF exported' })
    }
  } catch (e) {
    $q.notify({ type: 'negative', message: e instanceof Error ? e.message : 'Export failed' })
  } finally {
    exportingUUID.value = ''
  }
}

function formatDate(value: string) {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return value
  }
  return parsed.toLocaleDateString()
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toISOString().replace('T', ' ').slice(0, 16)
}

function progressValue(validation: ActivationValidation) {
  if (!validation.minimum_contacts) {
    return 0
  }
  return Math.min(1, validation.unique_callsigns / validation.minimum_contacts)
}

watch(tab, async () => {
  selectedActivationUUID.value = ''
  selectedDetail.value = null
  await refreshAll()
})

onMounted(async () => {
  await Promise.all([refreshAll(), fetchLocations()])
})
</script>

<style scoped>
.activations-page {
  max-width: 1240px;
  margin: 0 auto;
}

.tabs-shell {
  background: rgba(255, 255, 255, 0.02);
}

.activation-card {
  border: 1px solid rgba(255, 255, 255, 0.08);
  transition: border-color 140ms ease, transform 140ms ease;
}

.activation-card:hover {
  border-color: rgba(59, 130, 246, 0.7);
  transform: translateY(-1px);
}

.activation-card--active {
  border-color: rgba(16, 185, 129, 0.9);
}

.detail-card {
  min-height: 420px;
}

.qso-table {
  max-height: 420px;
  overflow: auto;
}
</style>
