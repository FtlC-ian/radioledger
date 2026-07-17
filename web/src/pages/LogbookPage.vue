<template>
  <q-page class="q-pa-md logbook-page">
    <div class="row items-center justify-between q-mb-md q-gutter-sm">
      <div>
        <div class="text-h5">Logbook Viewer</div>
        <div class="text-caption text-grey-6">Sortable table, filters, and quick QSO logging</div>
      </div>
      <div class="row items-center q-gutter-sm">
        <q-btn flat round color="secondary" icon="settings" aria-label="Choose visible columns">
          <q-menu anchor="bottom right" self="top right">
            <q-list style="min-width: 240px">
              <q-item>
                <q-item-section>
                  <q-item-label>Visible columns</q-item-label>
                  <q-item-label caption>Match the desktop logbook view.</q-item-label>
                </q-item-section>
              </q-item>
              <q-separator />
              <q-item v-for="column in columnToggleOptions" :key="column.value" tag="label" clickable>
                <q-item-section avatar>
                  <q-checkbox
                    :model-value="visibleColumns.includes(column.value)"
                    @update:model-value="toggleVisibleColumn(column.value, Boolean($event))"
                  />
                </q-item-section>
                <q-item-section>
                  <q-item-label>{{ column.label }}</q-item-label>
                </q-item-section>
              </q-item>
              <q-separator />
              <q-item clickable @click="resetVisibleColumns">
                <q-item-section avatar>
                  <q-icon name="restart_alt" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>Reset to defaults</q-item-label>
                </q-item-section>
              </q-item>
            </q-list>
          </q-menu>
        </q-btn>
        <q-btn color="primary" icon="add_circle" label="New QSO Page" to="/qso/new" unelevated />
      </div>
    </div>

    <q-card flat bordered class="q-mb-md">
      <q-card-section>
        <div class="text-subtitle2 q-mb-sm">Quick Add QSO</div>
        <q-form ref="quickAddForm" class="row q-col-gutter-sm" @submit.prevent="submitQuickAdd">
          <q-input
            class="col-12 col-sm-6 col-md-3"
            dense
            outlined
            label="Callsign *"
            v-model="quickAdd.callsign"
            @update:model-value="quickAdd.callsign = String($event || '').toUpperCase()"
            :rules="[(v) => !!String(v || '').trim() || 'Callsign is required']"
          />
          <q-input
            class="col-6 col-sm-3 col-md-2"
            dense
            outlined
            label="Frequency"
            type="number"
            v-model.number="quickAdd.frequency"
            @update:model-value="syncBandFromFrequency"
          />
          <q-select
            class="col-6 col-sm-3 col-md-2"
            dense
            outlined
            emit-value
            map-options
            label="Mode"
            :options="modeOptions"
            v-model="quickAdd.mode"
          />
          <q-select
            class="col-6 col-sm-3 col-md-2"
            dense
            outlined
            emit-value
            map-options
            label="Band"
            :options="bandOptions"
            v-model="quickAdd.band"
          />
          <q-input class="col-3 col-sm-2 col-md-1" dense outlined label="RST S" v-model="quickAdd.rst_sent" />
          <q-input class="col-3 col-sm-2 col-md-1" dense outlined label="RST R" v-model="quickAdd.rst_rcvd" />
          <q-input
            class="col-12 col-sm-7 col-md-3"
            dense
            outlined
            type="datetime-local"
            label="Date/Time (UTC)"
            v-model="quickAdd.datetime_on"
          />
          <q-input class="col-12 col-sm-9 col-md-7" dense outlined label="Notes" v-model="quickAdd.notes" />
          <div class="col-12 col-sm-3 col-md-2 row items-center justify-end">
            <q-btn color="primary" unelevated type="submit" label="Log QSO" :loading="quickAddLoading" />
          </div>
        </q-form>
      </q-card-section>
    </q-card>

    <q-card flat bordered class="q-mb-md">
      <q-card-section>
        <div class="row q-col-gutter-sm items-end">
          <q-input
            class="col-12 col-md-4"
            dense
            outlined
            debounce="250"
            label="Callsign"
            v-model="filters.callsign"
            @update:model-value="filters.callsign = String($event || '').toUpperCase()"
          />
          <q-select
            class="col-6 col-md-2"
            dense
            outlined
            clearable
            emit-value
            map-options
            label="Band"
            :options="bandOptions"
            v-model="filters.band"
          />
          <q-select
            class="col-6 col-md-2"
            dense
            outlined
            clearable
            emit-value
            map-options
            label="Mode"
            :options="modeOptions"
            v-model="filters.mode"
          />
          <q-input class="col-6 col-md-2" dense outlined type="date" label="From" v-model="filters.dateFrom" />
          <q-input class="col-6 col-md-2" dense outlined type="date" label="To" v-model="filters.dateTo" />
        </div>
        <div class="row q-gutter-sm q-mt-sm">
          <q-btn color="primary" label="Apply Filters" @click="applyFilters" />
          <q-btn flat color="secondary" label="Reset" @click="resetFilters" />
        </div>
      </q-card-section>
    </q-card>

    <q-banner v-if="logbook.error" class="bg-negative text-white q-mb-md" rounded>
      {{ logbook.error }}
    </q-banner>

    <q-table
      flat
      bordered
      row-key="uuid"
      :rows="logbook.qsos"
      :columns="columns"
      :visible-columns="visibleColumns"
      :loading="logbook.loading"
      :dense="$q.screen.lt.md"
      v-model:pagination="tablePagination"
      @request="onTableRequest"
      @row-click="onRowClick"
    >
      <template #body-cell-date_time="props">
        <q-td :props="props">{{ formatDate(props.row.datetime_on) }}</q-td>
      </template>

      <template #body-cell-frequency="props">
        <q-td :props="props" class="text-right">{{ formatFrequency(props.row.frequency) }}</q-td>
      </template>

      <template #body-cell-notes="props">
        <q-td :props="props" class="ellipsis notes-cell">{{ props.row.notes || props.row.comment || '—' }}</q-td>
      </template>

      <template #no-data>
        <div class="full-width row flex-center q-pa-xl text-grey-6 text-center">
          <div>
            <q-icon name="table_rows" size="48px" class="q-mb-sm" />
            <div class="text-subtitle1">No QSOs found</div>
            <div class="text-caption q-mb-sm">Try changing filters or log your first QSO.</div>
            <q-btn color="primary" outline label="Clear Filters" @click="resetFilters" />
          </div>
        </div>
      </template>
    </q-table>

    <q-dialog v-model="showEditDialog" :maximized="$q.screen.lt.md">
      <q-card style="min-width: min(900px, 95vw)">
        <q-card-section class="row items-center justify-between">
          <div class="text-h6">Edit QSO</div>
          <q-btn flat round icon="close" v-close-popup />
        </q-card-section>
        <q-separator />
        <q-card-section>
          <q-form class="row q-col-gutter-sm" @submit.prevent="saveEdit">
            <q-input
              class="col-12 col-sm-6 col-md-3"
              dense
              outlined
              label="Callsign *"
              v-model="editForm.callsign"
              @update:model-value="editForm.callsign = String($event || '').toUpperCase()"
              :rules="[(v) => !!String(v || '').trim() || 'Callsign is required']"
            />
            <q-input class="col-6 col-sm-3 col-md-2" dense outlined label="Frequency" type="number" v-model.number="editForm.frequency" />
            <q-select class="col-6 col-sm-3 col-md-2" dense outlined emit-value map-options label="Mode" :options="editModeOptions" v-model="editForm.mode" />
            <q-select class="col-6 col-sm-3 col-md-2" dense outlined emit-value map-options label="Band" :options="editBandOptions" v-model="editForm.band" />
            <q-input class="col-3 col-sm-2 col-md-1" dense outlined label="RST S" v-model="editForm.rst_sent" />
            <q-input class="col-3 col-sm-2 col-md-1" dense outlined label="RST R" v-model="editForm.rst_rcvd" />
            <q-input class="col-12 col-sm-7 col-md-3" dense outlined type="datetime-local" label="Date/Time (UTC)" v-model="editForm.datetime_on" />
            <q-input class="col-12" dense outlined type="textarea" autogrow label="Notes" v-model="editForm.notes" />
            <div class="col-12 row justify-between q-mt-sm">
              <q-btn flat color="negative" icon="delete" label="Delete" :loading="deleteLoading" @click="confirmDelete" />
              <div class="row q-gutter-sm">
                <q-btn flat label="Cancel" v-close-popup />
                <q-btn color="primary" unelevated type="submit" label="Save" :loading="saveLoading" />
              </div>
            </div>
          </q-form>
        </q-card-section>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useQuasar, type QForm, type QTableColumn, type QTableProps } from 'quasar'
import { useLogbookStore } from 'src/stores/logbook'
import { useBandModePreferencesStore } from 'src/stores/bandModePreferences'
import type { Qso, QsoPayload, QsoSearchFilters } from 'src/types/qso'

const LOGBOOK_VISIBLE_COLUMNS_STORAGE_KEY = 'radioledger-logbook-columns'
const DEFAULT_VISIBLE_COLUMNS = ['date_time', 'callsign', 'frequency', 'mode', 'band', 'rst_sent', 'rst_rcvd', 'notes'] as const

type LogbookColumnName =
  | 'date_time'
  | 'callsign'
  | 'frequency'
  | 'mode'
  | 'band'
  | 'rst_sent'
  | 'rst_rcvd'
  | 'name'
  | 'qth'
  | 'country'
  | 'notes'
  | 'dxcc'
  | 'cq_zone'
  | 'itu_zone'
  | 'gridsquare'
  | 'continent'

const $q = useQuasar()
const logbook = useLogbookStore()
const bandModePreferences = useBandModePreferencesStore()

const fallbackBandOptions = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m', '2m', '70cm'].map(
  (value) => ({ label: value, value }),
)
const fallbackModeOptions = ['SSB', 'CW', 'FT8', 'FT4', 'PSK31', 'RTTY', 'AM', 'FM', 'JS8', 'WSPR', 'JT65', 'Other'].map(
  (value) => ({ label: value, value }),
)

const bandOptions = computed(() => (bandModePreferences.bandOptions.length ? bandModePreferences.bandOptions : fallbackBandOptions))
const modeOptions = computed(() => (bandModePreferences.modeOptions.length ? bandModePreferences.modeOptions : fallbackModeOptions))

const filters = reactive<QsoSearchFilters>({ ...logbook.filters })
const quickAddForm = ref<QForm | null>(null)
const quickAddLoading = ref(false)
const showEditDialog = ref(false)
const saveLoading = ref(false)
const deleteLoading = ref(false)
const selectedQso = ref<Qso | null>(null)

const tablePagination = ref<QTableProps['pagination']>({
  page: logbook.pagination.page,
  rowsPerPage: logbook.pagination.rowsPerPage,
  rowsNumber: logbook.pagination.totalRows,
  sortBy: logbook.pagination.sortBy,
  descending: logbook.pagination.descending,
})

const columns: QTableColumn<Qso>[] = [
  { name: 'date_time', label: 'Date/Time', field: (row) => row.datetime_on || '', sortable: true, align: 'left' },
  { name: 'callsign', label: 'Callsign', field: 'callsign', sortable: true, align: 'left' },
  { name: 'frequency', label: 'Frequency', field: (row) => row.frequency ?? null, sortable: true, align: 'right' },
  { name: 'mode', label: 'Mode', field: 'mode', sortable: true, align: 'left' },
  { name: 'band', label: 'Band', field: 'band', sortable: true, align: 'left' },
  { name: 'rst_sent', label: 'RST Sent', field: 'rst_sent', sortable: true, align: 'center' },
  { name: 'rst_rcvd', label: 'RST Rcvd', field: 'rst_rcvd', sortable: true, align: 'center' },
  { name: 'name', label: 'Name', field: 'name', sortable: true, align: 'left' },
  { name: 'qth', label: 'QTH', field: 'qth', sortable: true, align: 'left' },
  { name: 'country', label: 'Country', field: 'country', sortable: true, align: 'left' },
  { name: 'notes', label: 'Notes', field: (row) => row.notes || row.comment || '', sortable: true, align: 'left' },
  { name: 'dxcc', label: 'DXCC #', field: (row) => row.dxcc ?? null, sortable: true, align: 'center' },
  { name: 'cq_zone', label: 'CQ Zone', field: (row) => row.cq_zone ?? null, sortable: true, align: 'center' },
  { name: 'itu_zone', label: 'ITU Zone', field: (row) => row.itu_zone ?? null, sortable: true, align: 'center' },
  { name: 'gridsquare', label: 'Grid Square', field: 'gridsquare', sortable: true, align: 'left' },
  { name: 'continent', label: 'Continent', field: 'continent', sortable: true, align: 'center' },
]

const allColumnNames = columns.map((column) => column.name) as LogbookColumnName[]
const columnToggleOptions = columns.map((column) => ({
  label: column.label,
  value: column.name as LogbookColumnName,
}))
const visibleColumns = ref<LogbookColumnName[]>([...DEFAULT_VISIBLE_COLUMNS])

const quickAdd = reactive({
  callsign: '',
  frequency: null as number | null,
  mode: 'SSB',
  band: '20m',
  rst_sent: '59',
  rst_rcvd: '59',
  datetime_on: toDateTimeLocalUtc(new Date().toISOString()),
  notes: '',
})

const editForm = reactive({
  callsign: '',
  frequency: null as number | null,
  mode: 'SSB',
  band: '20m',
  rst_sent: '59',
  rst_rcvd: '59',
  datetime_on: toDateTimeLocalUtc(new Date().toISOString()),
  notes: '',
})

const editBandOptions = computed(() => withCurrentOption(bandOptions.value, editForm.band))
const editModeOptions = computed(() => withCurrentOption(modeOptions.value, editForm.mode))

watch(
  () => logbook.pagination,
  () => {
    tablePagination.value = {
      ...(tablePagination.value || {}),
      page: logbook.pagination.page,
      rowsPerPage: logbook.pagination.rowsPerPage,
      rowsNumber: logbook.pagination.totalRows,
      sortBy: logbook.pagination.sortBy,
      descending: logbook.pagination.descending,
    }
  },
  { deep: true },
)

watch(
  () => visibleColumns.value,
  (next) => {
    if (typeof localStorage === 'undefined') {
      return
    }
    localStorage.setItem(LOGBOOK_VISIBLE_COLUMNS_STORAGE_KEY, JSON.stringify(next))
  },
  { deep: true },
)

watch(
  () => quickAdd.mode,
  (mode) => {
    const rst = mode === 'CW' ? '599' : '59'
    quickAdd.rst_sent = rst
    quickAdd.rst_rcvd = rst
  },
)

watch(
  () => editForm.mode,
  (mode) => {
    const rst = mode === 'CW' ? '599' : '59'
    if (!editForm.rst_sent || editForm.rst_sent === '59' || editForm.rst_sent === '599') editForm.rst_sent = rst
    if (!editForm.rst_rcvd || editForm.rst_rcvd === '59' || editForm.rst_rcvd === '599') editForm.rst_rcvd = rst
  },
)

watch(
  () => bandOptions.value,
  (options) => {
    if (!options.length) {
      return
    }

    const firstBand = options[0]?.value
    if (firstBand && !options.some((option) => option.value === quickAdd.band)) {
      quickAdd.band = firstBand
    }
    if (firstBand && !options.some((option) => option.value === editForm.band)) {
      editForm.band = firstBand
    }
    if (filters.band && !options.some((option) => option.value === filters.band)) {
      filters.band = undefined
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

    const firstMode = options[0]?.value
    if (firstMode && !options.some((option) => option.value === quickAdd.mode)) {
      quickAdd.mode = firstMode
    }
    if (firstMode && !options.some((option) => option.value === editForm.mode)) {
      editForm.mode = firstMode
    }
    if (filters.mode && !options.some((option) => option.value === filters.mode)) {
      filters.mode = undefined
    }
  },
  { immediate: true },
)

const hasActiveFilters = computed(() =>
  Boolean(filters.callsign || filters.band || filters.mode || filters.dateFrom || filters.dateTo),
)

onMounted(async () => {
  visibleColumns.value = loadVisibleColumns()
  await Promise.all([bandModePreferences.load(), logbook.fetchStats()])
  await logbook.fetchQsos({ reset: true })
})

function loadVisibleColumns(): LogbookColumnName[] {
  if (typeof localStorage === 'undefined') {
    return [...DEFAULT_VISIBLE_COLUMNS]
  }

  const raw = localStorage.getItem(LOGBOOK_VISIBLE_COLUMNS_STORAGE_KEY)
  if (!raw) {
    return [...DEFAULT_VISIBLE_COLUMNS]
  }

  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) {
      return [...DEFAULT_VISIBLE_COLUMNS]
    }

    const validColumns = parsed.filter((value): value is LogbookColumnName =>
      typeof value === 'string' && allColumnNames.includes(value as LogbookColumnName),
    )

    return validColumns.length > 0 ? validColumns : [...DEFAULT_VISIBLE_COLUMNS]
  } catch {
    return [...DEFAULT_VISIBLE_COLUMNS]
  }
}

function toggleVisibleColumn(columnName: LogbookColumnName, enabled: boolean) {
  if (enabled) {
    if (!visibleColumns.value.includes(columnName)) {
      visibleColumns.value = allColumnNames.filter((name) => name === columnName || visibleColumns.value.includes(name))
    }
    return
  }

  if (visibleColumns.value.length <= 1) {
    $q.notify({ type: 'warning', message: 'Keep at least one logbook column visible' })
    return
  }

  visibleColumns.value = visibleColumns.value.filter((name) => name !== columnName)
}

function resetVisibleColumns() {
  visibleColumns.value = [...DEFAULT_VISIBLE_COLUMNS]
}

function toDateTimeLocalUtc(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return new Date().toISOString().slice(0, 16)
  return date.toISOString().slice(0, 16)
}

function fromDateTimeLocalUtc(value: string): string {
  if (!value) return new Date().toISOString()
  return new Date(`${value}:00.000Z`).toISOString()
}

function withCurrentOption(options: { label: string; value: string }[], currentValue: string) {
  if (!currentValue || options.some((option) => option.value === currentValue)) {
    return options
  }
  return [{ label: `${currentValue} (current)`, value: currentValue }, ...options]
}

function normalizeFrequencyHz(value: number | null | undefined): number | null {
  if (!value || Number.isNaN(value)) return null
  if (value >= 1_000_000) return value
  if (value >= 1_000) return value * 1_000
  return value * 1_000_000
}

function inferBandFromFrequencyHz(frequencyHz: number | null | undefined): string | null {
  const hz = normalizeFrequencyHz(frequencyHz)
  if (!hz) return null
  if (hz >= 1_800_000 && hz < 2_000_000) return '160m'
  if (hz >= 3_500_000 && hz < 4_000_000) return '80m'
  if (hz >= 5_300_000 && hz < 5_500_000) return '60m'
  if (hz >= 7_000_000 && hz < 7_300_000) return '40m'
  if (hz >= 10_100_000 && hz < 10_150_000) return '30m'
  if (hz >= 14_000_000 && hz < 14_350_000) return '20m'
  if (hz >= 18_068_000 && hz < 18_168_000) return '17m'
  if (hz >= 21_000_000 && hz < 21_450_000) return '15m'
  if (hz >= 24_890_000 && hz < 24_990_000) return '12m'
  if (hz >= 28_000_000 && hz < 29_700_000) return '10m'
  if (hz >= 50_000_000 && hz < 54_000_000) return '6m'
  if (hz >= 144_000_000 && hz < 148_000_000) return '2m'
  if (hz >= 420_000_000 && hz < 450_000_000) return '70cm'
  return null
}

function syncBandFromFrequency() {
  const inferred = inferBandFromFrequencyHz(quickAdd.frequency)
  if (inferred) {
    quickAdd.band = inferred
  }
}

function buildPayload(source: typeof quickAdd | typeof editForm): QsoPayload {
  return {
    callsign: source.callsign.trim().toUpperCase(),
    mode: source.mode,
    band: source.band,
    frequency: normalizeFrequencyHz(source.frequency),
    rst_sent: source.rst_sent || null,
    rst_rcvd: source.rst_rcvd || null,
    datetime_on: fromDateTimeLocalUtc(source.datetime_on),
    notes: source.notes || null,
    comment: source.notes || null,
  }
}

function resetQuickAdd() {
  quickAdd.callsign = ''
  quickAdd.frequency = null
  quickAdd.mode = 'SSB'
  quickAdd.band = '20m'
  quickAdd.rst_sent = '59'
  quickAdd.rst_rcvd = '59'
  quickAdd.datetime_on = toDateTimeLocalUtc(new Date().toISOString())
  quickAdd.notes = ''
}

async function submitQuickAdd() {
  if (!quickAdd.callsign.trim()) {
    $q.notify({ type: 'negative', message: 'Callsign is required' })
    return
  }

  quickAddLoading.value = true
  try {
    const response = await logbook.createQso(buildPayload(quickAdd))
    if (response.success) {
      resetQuickAdd()
      // Reset validation state after clearing the form model so Quasar does not
      // re-evaluate the callsign rule against the now-empty value and flash an
      // erroneous "Callsign is required" error on a successfully-logged QSO.
      await nextTick()
      quickAddForm.value?.resetValidation()
      $q.notify({ type: 'positive', message: 'QSO logged' })
      await logbook.fetchQsos({ page: 1 })
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Unable to create QSO' })
    }
  } catch {
    $q.notify({ type: 'negative', message: 'Unable to create QSO' })
  } finally {
    quickAddLoading.value = false
  }
}

async function applyFilters() {
  await logbook.search({ ...filters })
}

function resetFilters() {
  filters.callsign = undefined
  filters.band = undefined
  filters.mode = undefined
  filters.dateFrom = undefined
  filters.dateTo = undefined
  void logbook.search({})
}

async function onTableRequest(props: { pagination: NonNullable<QTableProps['pagination']> }) {
  const pagination = props.pagination
  tablePagination.value = {
    ...tablePagination.value,
    ...pagination,
    rowsNumber: logbook.pagination.totalRows,
  }

  await logbook.fetchQsos({
    page: pagination.page,
    rowsPerPage: pagination.rowsPerPage,
    sortBy: (pagination.sortBy as string) || 'datetime_on',
    descending: !!pagination.descending,
  })
}

function onRowClick(_: Event, row: Qso) {
  selectedQso.value = row
  editForm.callsign = row.callsign || ''
  editForm.frequency = row.frequency ?? null
  editForm.mode = row.mode || 'SSB'
  editForm.band = row.band || '20m'
  editForm.rst_sent = row.rst_sent || (row.mode === 'CW' ? '599' : '59')
  editForm.rst_rcvd = row.rst_rcvd || (row.mode === 'CW' ? '599' : '59')
  editForm.datetime_on = toDateTimeLocalUtc(row.datetime_on || new Date().toISOString())
  editForm.notes = row.notes || row.comment || ''
  showEditDialog.value = true
}

async function saveEdit() {
  if (!selectedQso.value) return

  saveLoading.value = true
  try {
    const response = await logbook.updateQso(selectedQso.value.uuid, buildPayload(editForm))
    if (response.success) {
      showEditDialog.value = false
      $q.notify({ type: 'positive', message: 'QSO updated' })
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Unable to update QSO' })
    }
  } catch {
    $q.notify({ type: 'negative', message: 'Unable to update QSO' })
  } finally {
    saveLoading.value = false
  }
}

function confirmDelete() {
  if (!selectedQso.value) return

  $q.dialog({
    title: 'Delete QSO',
    message: `Delete QSO with ${selectedQso.value.callsign}? This cannot be undone.`,
    cancel: true,
    ok: { label: 'Delete', color: 'negative' },
  }).onOk(() => {
    void deleteSelectedQso()
  })
}

async function deleteSelectedQso() {
  if (!selectedQso.value) return

  deleteLoading.value = true
  try {
    const response = await logbook.deleteQso(selectedQso.value.uuid)
    if (response.success) {
      showEditDialog.value = false
      $q.notify({ type: 'positive', message: 'QSO deleted' })
      if (hasActiveFilters.value) {
        await logbook.search({ ...filters })
      }
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Unable to delete QSO' })
    }
  } catch {
    $q.notify({ type: 'negative', message: 'Unable to delete QSO' })
  } finally {
    deleteLoading.value = false
  }
}

function formatFrequency(value: number | null | undefined): string {
  if (!value) return '—'
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(3)} MHz`
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(1)} kHz`
  }
  return `${value}`
}

function formatDate(value: string | undefined): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${date.toLocaleDateString()} ${date.toLocaleTimeString()}`
}
</script>

<style scoped>
.logbook-page :deep(.q-table__middle) {
  max-height: calc(100vh - 360px);
}

.notes-cell {
  max-width: 320px;
}
</style>
