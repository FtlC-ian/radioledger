<template>
  <q-page class="q-pa-md awards-page">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Awards</div>
        <div class="text-body2 text-grey-5">Track DXCC, WAS, VUCC, WAZ, WPX, POTA, and SOTA progress.</div>
      </div>
      <q-btn flat icon="refresh" label="Refresh" :loading="loading || refreshing" @click="handleRefresh" />
    </div>

    <q-banner v-if="error" dense rounded class="bg-negative text-white q-mb-md">
      {{ error }}
    </q-banner>

    <q-card flat bordered class="q-mb-md tabs-shell">
      <q-tabs v-model="tab" dense class="awards-tabs" active-color="primary" indicator-color="primary" align="left">
        <q-tab name="dxcc" label="DXCC" icon="public" />
        <q-tab name="was" label="WAS" icon="flag" />
        <q-tab name="grids" label="Grids" icon="grid_on" />
        <q-tab name="waz" label="WAZ" icon="language" />
        <q-tab name="wpx" label="WPX" icon="tag" />
        <q-tab name="pota" label="POTA" icon="park" />
        <q-tab name="sota" label="SOTA" icon="terrain" />
      </q-tabs>
    </q-card>

    <q-tab-panels v-model="tab" animated keep-alive>
      <q-tab-panel name="dxcc" class="q-pa-none">
        <q-skeleton v-if="loading" type="rect" height="320px" class="q-mb-md" />

        <template v-else>
          <q-card flat bordered class="q-mb-md progress-card">
            <q-card-section>
              <div class="row items-center justify-between q-col-gutter-md">
                <div class="col-12 col-md">
                  <div class="text-subtitle1 text-weight-medium">DXCC Progress</div>
                  <div class="text-h4 text-weight-bold q-mt-xs">
                    {{ dxcc.worked }} / {{ dxcc.total_entities }}
                    <span class="text-subtitle1 text-grey-5">— {{ dxccPercent.toFixed(1) }}%</span>
                  </div>
                  <div class="text-caption awards-meta q-mt-xs">{{ dxcc.needed }} needed · {{ dxcc.confirmed }} confirmed</div>
                </div>
                <div class="col-12 col-md-auto">
                  <q-btn-toggle
                    v-model="dxccFilter"
                    unelevated
                    color="primary"
                    toggle-color="primary"
                    no-caps
                    spread
                    :options="[
                      { label: 'All', value: 'all' },
                      { label: 'Worked', value: 'worked' },
                      { label: 'Needed', value: 'needed' },
                    ]"
                  />
                </div>
              </div>
              <q-linear-progress class="q-mt-md" size="14px" color="positive" track-color="grey-8" :value="dxccProgress" rounded />
            </q-card-section>
          </q-card>

          <q-card v-if="dxcc.entities.length === 0" flat bordered>
            <q-card-section class="text-center q-py-xl">
              <q-icon name="travel_explore" size="44px" class="text-grey-6 q-mb-sm" />
              <div class="text-subtitle1 text-weight-medium">No DXCC progress yet</div>
              <div class="text-caption text-grey-5">Log your first QSO to start filling the world map.</div>
            </q-card-section>
          </q-card>

          <template v-else>
            <q-card flat bordered>
              <q-card-section>
                <div class="row q-col-gutter-sm items-center q-mb-sm">
                  <div class="col-12 col-md-5">
                    <q-input v-model="dxccSearch" dense outlined clearable debounce="200" placeholder="Filter entities">
                      <template #prepend><q-icon name="search" /></template>
                    </q-input>
                  </div>
                </div>

                <q-table
                  flat
                  dense
                  :rows="filteredDxccRows"
                  :columns="dxccColumns"
                  row-key="entity_id"
                  :pagination="dxccPagination"
                  :rows-per-page-options="[10, 25, 50, 100]"
                  :filter="dxccSearch"
                >
                  <template #body-cell-worked="props">
                    <q-td :props="props">
                      <q-badge :color="props.row.worked ? 'positive' : 'grey-6'" :label="props.row.worked ? 'Worked' : 'Needed'" />
                    </q-td>
                  </template>
                  <template #body-cell-bands="props">
                    <q-td :props="props">{{ props.row.bands.length ? props.row.bands.join(', ') : '—' }}</q-td>
                  </template>
                  <template #body-cell-first_qso="props">
                    <q-td :props="props">{{ props.row.first_qso ? formatDate(props.row.first_qso) : '—' }}</q-td>
                  </template>
                </q-table>
              </q-card-section>
            </q-card>
          </template>
        </template>
      </q-tab-panel>

      <q-tab-panel name="was" class="q-pa-none">
        <q-skeleton v-if="loading" type="rect" height="320px" class="q-mb-md" />

        <template v-else>
          <q-card flat bordered class="q-mb-md progress-card">
            <q-card-section>
              <div class="text-subtitle1 text-weight-medium">Worked All States</div>
              <div class="text-h4 text-weight-bold q-mt-xs">
                {{ was.worked }} / {{ was.total_states }}
                <span class="text-subtitle1 text-grey-5">— {{ wasPercent.toFixed(1) }}%</span>
              </div>
              <q-linear-progress class="q-mt-md" size="14px" color="positive" track-color="grey-8" :value="wasProgress" rounded />
            </q-card-section>
          </q-card>

          <q-card v-if="was.states.length === 0" flat bordered>
            <q-card-section class="text-center q-py-xl">
              <q-icon name="map" size="44px" class="text-grey-6 q-mb-sm" />
              <div class="text-subtitle1 text-weight-medium">No state contacts yet</div>
              <div class="text-caption text-grey-5">Your WAS map will populate as you work new states.</div>
            </q-card-section>
          </q-card>

          <template v-else>
            <q-card flat bordered>
              <q-card-section>
                <q-table flat dense :rows="was.states" :columns="wasColumns" row-key="code" :pagination="wasPagination" :rows-per-page-options="[10, 25, 50]">
                  <template #body-cell-worked="props">
                    <q-td :props="props">
                      <q-badge :color="props.row.worked ? 'positive' : 'grey-6'" :label="props.row.worked ? 'Worked' : 'Needed'" />
                    </q-td>
                  </template>
                  <template #body-cell-first_qso="props">
                    <q-td :props="props">{{ props.row.first_qso ? formatDate(props.row.first_qso) : '—' }}</q-td>
                  </template>
                </q-table>
              </q-card-section>
            </q-card>
          </template>
        </template>
      </q-tab-panel>

      <q-tab-panel name="grids" class="q-pa-none">
        <q-skeleton v-if="loading" type="rect" height="320px" class="q-mb-md" />
        <template v-else>
          <q-card flat bordered class="q-mb-md progress-card">
            <q-card-section>
              <div class="text-subtitle1 text-weight-medium">Grid Squares (VUCC)</div>
              <div class="text-h4 text-weight-bold q-mt-xs">
                {{ grids.worked }} / {{ grids.target }}
                <span class="text-subtitle1 text-grey-5">— {{ grids.progress_pct.toFixed(1) }}%</span>
              </div>
              <q-linear-progress class="q-mt-md" size="14px" color="positive" track-color="grey-8" :value="gridsProgress" rounded />
            </q-card-section>
          </q-card>

          <q-card v-if="grids.grid_squares.length === 0" flat bordered>
            <q-card-section class="text-center q-py-xl">
              <q-icon name="grid_4x4" size="44px" class="text-grey-6 q-mb-sm" />
              <div class="text-subtitle1 text-weight-medium">No grid squares logged</div>
            </q-card-section>
          </q-card>

          <q-card v-else flat bordered>
            <q-card-section>
              <q-table flat dense :rows="grids.grid_squares" :columns="gridColumns" row-key="grid_square" :pagination="gridsPagination" :rows-per-page-options="[10, 25, 50]">
                <template #body-cell-first_qso="props"><q-td :props="props">{{ props.row.first_qso ? formatDate(props.row.first_qso) : '—' }}</q-td></template>
                <template #body-cell-last_qso="props"><q-td :props="props">{{ props.row.last_qso ? formatDate(props.row.last_qso) : '—' }}</q-td></template>
              </q-table>
            </q-card-section>
          </q-card>
        </template>
      </q-tab-panel>

      <q-tab-panel name="waz" class="q-pa-none">
        <q-card flat bordered class="q-mb-md progress-card">
          <q-card-section>
            <div class="text-subtitle1 text-weight-medium">Worked All Zones (CQ WAZ)</div>
            <div class="text-h4 text-weight-bold q-mt-xs">{{ waz.worked }} / 40 <span class="text-subtitle1 text-grey-5">— {{ wazPercent.toFixed(1) }}%</span></div>
            <div class="text-caption text-grey-5">{{ waz.confirmed }} confirmed zones</div>
            <q-linear-progress class="q-mt-md" size="14px" color="positive" track-color="grey-8" :value="wazProgress" rounded />
          </q-card-section>
        </q-card>

        <q-card flat bordered class="q-mb-md">
          <q-card-section>
            <div class="text-subtitle2 text-weight-medium q-mb-sm">CQ Zone Status</div>
            <div class="zone-grid">
              <q-chip
                v-for="zone in wazZones"
                :key="zone.zone"
                dense
                square
                :color="zone.worked ? (zone.confirmed ? 'positive' : 'warning') : 'grey-7'"
                text-color="white"
              >
                {{ zone.zone }}
              </q-chip>
            </div>
          </q-card-section>
        </q-card>

        <q-card flat bordered>
          <q-card-section>
            <q-table flat dense :rows="waz.rows" :columns="awardColumns" row-key="entity_key" :pagination="awardPagination" :rows-per-page-options="[10,25,50]">
              <template #body-cell-worked="props"><q-td :props="props"><q-badge :color="props.row.worked ? 'positive' : 'grey-6'" :label="props.row.worked ? 'Worked' : 'Needed'" /></q-td></template>
              <template #body-cell-confirmed="props"><q-td :props="props"><q-badge :color="props.row.confirmed ? 'positive' : 'grey-6'" :label="props.row.confirmed ? 'Yes' : 'No'" /></q-td></template>
              <template #body-cell-last_qso_at="props"><q-td :props="props">{{ props.row.last_qso_at ? formatDate(props.row.last_qso_at) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>
      </q-tab-panel>

      <q-tab-panel name="wpx" class="q-pa-none">
        <q-card flat bordered class="q-mb-md progress-card">
          <q-card-section>
            <div class="text-subtitle1 text-weight-medium">Worked Prefixes (CQ WPX)</div>
            <div class="text-h4 text-weight-bold q-mt-xs">{{ wpx.worked }} <span class="text-subtitle1 text-grey-5">prefixes worked</span></div>
            <div class="text-caption text-grey-5">{{ wpx.confirmed }} confirmed prefixes</div>
          </q-card-section>
        </q-card>

        <q-card flat bordered>
          <q-card-section>
            <q-table flat dense :rows="wpx.rows" :columns="awardColumns" row-key="entity_key" :pagination="awardPagination" :rows-per-page-options="[10,25,50]">
              <template #body-cell-worked="props"><q-td :props="props"><q-badge :color="props.row.worked ? 'positive' : 'grey-6'" :label="props.row.worked ? 'Worked' : 'Needed'" /></q-td></template>
              <template #body-cell-confirmed="props"><q-td :props="props"><q-badge :color="props.row.confirmed ? 'positive' : 'grey-6'" :label="props.row.confirmed ? 'Yes' : 'No'" /></q-td></template>
              <template #body-cell-last_qso_at="props"><q-td :props="props">{{ props.row.last_qso_at ? formatDate(props.row.last_qso_at) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>
      </q-tab-panel>

      <q-tab-panel name="pota" class="q-pa-none">
        <div class="row q-col-gutter-md q-mb-md">
          <div class="col-12 col-md-3"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Parks Hunted</div><div class="text-h5 text-weight-bold">{{ pota.parks_hunted }}</div></q-card-section></q-card></div>
          <div class="col-12 col-md-3"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Parks Activated</div><div class="text-h5 text-weight-bold">{{ pota.parks_activated }}</div></q-card-section></q-card></div>
          <div class="col-12 col-md-3"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Activation Logs</div><div class="text-h5 text-weight-bold">{{ pota.activations_total }}</div></q-card-section></q-card></div>
          <div class="col-12 col-md-3"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Valid Activations</div><div class="text-h5 text-weight-bold">{{ pota.valid_activations }}</div></q-card-section></q-card></div>
        </div>

        <q-card flat bordered class="q-mb-md">
          <q-card-section>
            <div class="text-subtitle2 text-weight-medium q-mb-sm">Hunted Parks</div>
            <q-table flat dense :rows="potaHunter.rows" :columns="awardColumns" row-key="entity_key" :pagination="awardPagination" :rows-per-page-options="[10,25,50]">
              <template #body-cell-confirmed="props"><q-td :props="props"><q-badge :color="props.row.confirmed ? 'positive' : 'grey-6'" :label="props.row.confirmed ? 'Yes' : 'No'" /></q-td></template>
              <template #body-cell-last_qso_at="props"><q-td :props="props">{{ props.row.last_qso_at ? formatDate(props.row.last_qso_at) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>

        <q-card flat bordered>
          <q-card-section>
            <div class="text-subtitle2 text-weight-medium q-mb-sm">Activated Parks</div>
            <q-table flat dense :rows="potaActivator.rows" :columns="awardColumns" row-key="entity_key" :pagination="awardPagination" :rows-per-page-options="[10,25,50]">
              <template #body-cell-last_qso_at="props"><q-td :props="props">{{ props.row.last_qso_at ? formatDate(props.row.last_qso_at) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>
      </q-tab-panel>

      <q-tab-panel name="sota" class="q-pa-none">
        <div class="row q-col-gutter-md q-mb-md">
          <div class="col-12 col-md-6"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Summits Chased</div><div class="text-h5 text-weight-bold">{{ sota.summits_chased }}</div></q-card-section></q-card></div>
          <div class="col-12 col-md-6"><q-card flat bordered class="progress-card"><q-card-section><div class="text-caption text-grey-5">Summits Activated</div><div class="text-h5 text-weight-bold">{{ sota.summits_activated }}</div></q-card-section></q-card></div>
        </div>

        <q-card flat bordered class="q-mb-md">
          <q-card-section>
            <div class="text-subtitle2 text-weight-medium q-mb-sm">Chased Summits</div>
            <q-table flat dense :rows="sota.chased" :columns="sotaColumns" row-key="summit_ref" :rows-per-page-options="[10,25,50]">
              <template #body-cell-confirmed="props"><q-td :props="props"><q-badge :color="props.row.confirmed ? 'positive' : 'grey-6'" :label="props.row.confirmed ? 'Yes' : 'No'" /></q-td></template>
              <template #body-cell-first_qso="props"><q-td :props="props">{{ props.row.first_qso ? formatDate(props.row.first_qso) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>

        <q-card flat bordered>
          <q-card-section>
            <div class="text-subtitle2 text-weight-medium q-mb-sm">Activated Summits</div>
            <q-table flat dense :rows="sota.activated" :columns="sotaColumns" row-key="summit_ref" :rows-per-page-options="[10,25,50]">
              <template #body-cell-confirmed="props"><q-td :props="props">—</q-td></template>
              <template #body-cell-first_qso="props"><q-td :props="props">{{ props.row.first_qso ? formatDate(props.row.first_qso) : '—' }}</q-td></template>
            </q-table>
          </q-card-section>
        </q-card>
      </q-tab-panel>
    </q-tab-panels>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { apiGet, apiPost } from 'src/api/client'

type DxccQso = { uuid: string; callsign: string; band: string; mode: string; datetime_on: string }
type DxccEntity = {
  entity_id: number
  name: string
  prefix: string
  continent: string
  worked: boolean
  confirmed: boolean
  bands: string[]
  first_qso?: string
  qso_count: number
  qsos?: DxccQso[]
}
type DxccPayload = {
  total_entities: number
  worked: number
  confirmed: number
  needed: number
  by_band: Record<string, { worked: number; confirmed: number }>
  entities: DxccEntity[]
}

type WasState = { code: string; name: string; worked: boolean; qso_count: number; first_qso?: string }
type WasPayload = { total_states: number; worked: number; needed: number; states: WasState[] }

type GridRow = { grid_square: string; qso_count: number; first_qso?: string; last_qso?: string }
type GridsPayload = { target: number; worked: number; needed: number; progress_pct: number; grid_squares: GridRow[] }

type AwardDetailRow = {
  entity_key: string
  worked: boolean
  confirmed: boolean
  qso_count: number
  last_qso_at?: string
}
type AwardDetailPayload = {
  award_type: string
  worked: number
  confirmed: number
  target?: number
  needed?: number
  progress_pct?: number
  rows: AwardDetailRow[]
}

type PotaPayload = {
  parks_activated: number
  parks_hunted: number
  activations_total: number
  valid_activations: number
}

type SotaRow = { summit_ref: string; qso_count: number; first_qso?: string; confirmed?: boolean }
type SotaPayload = { summits_chased: number; summits_activated: number; chased: SotaRow[]; activated: SotaRow[] }

const tab = ref<'dxcc' | 'was' | 'grids' | 'waz' | 'wpx' | 'pota' | 'sota'>('dxcc')
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')

const dxccFilter = ref<'all' | 'worked' | 'needed'>('all')
const dxccSearch = ref('')

const dxcc = ref<DxccPayload>({ total_entities: 0, worked: 0, confirmed: 0, needed: 0, by_band: {}, entities: [] })
const was = ref<WasPayload>({ total_states: 50, worked: 0, needed: 50, states: [] })
const grids = ref<GridsPayload>({ target: 100, worked: 0, needed: 100, progress_pct: 0, grid_squares: [] })
const waz = ref<AwardDetailPayload>({ award_type: 'waz', worked: 0, confirmed: 0, target: 40, needed: 40, progress_pct: 0, rows: [] })
const wpx = ref<AwardDetailPayload>({ award_type: 'wpx', worked: 0, confirmed: 0, rows: [] })
const pota = ref<PotaPayload>({ parks_activated: 0, parks_hunted: 0, activations_total: 0, valid_activations: 0 })
const potaHunter = ref<AwardDetailPayload>({ award_type: 'pota_hunter', worked: 0, confirmed: 0, rows: [] })
const potaActivator = ref<AwardDetailPayload>({ award_type: 'pota_activator', worked: 0, confirmed: 0, rows: [] })
const sota = ref<SotaPayload>({ summits_chased: 0, summits_activated: 0, chased: [], activated: [] })

const dxccPagination = ref({ sortBy: 'name', descending: false, page: 1, rowsPerPage: 25 })
const wasPagination = ref({ sortBy: 'code', descending: false, page: 1, rowsPerPage: 25 })
const gridsPagination = ref({ sortBy: 'qso_count', descending: true, page: 1, rowsPerPage: 25 })
const awardPagination = ref({ sortBy: 'qso_count', descending: true, page: 1, rowsPerPage: 25 })

const dxccColumns = [
  { name: 'name', label: 'Entity', field: 'name', align: 'left', sortable: true },
  { name: 'prefix', label: 'Prefix', field: 'prefix', align: 'left', sortable: true },
  { name: 'continent', label: 'Continent', field: 'continent', align: 'left', sortable: true },
  { name: 'worked', label: 'Status', field: 'worked', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'bands', label: 'Bands', field: 'bands', align: 'left', sortable: false },
  { name: 'first_qso', label: 'First QSO', field: 'first_qso', align: 'left', sortable: true },
]
const wasColumns = [
  { name: 'code', label: 'State', field: 'code', align: 'left', sortable: true },
  { name: 'name', label: 'Name', field: 'name', align: 'left', sortable: true },
  { name: 'worked', label: 'Status', field: 'worked', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'first_qso', label: 'First QSO', field: 'first_qso', align: 'left', sortable: true },
]
const gridColumns = [
  { name: 'grid_square', label: 'Grid', field: 'grid_square', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'first_qso', label: 'First QSO', field: 'first_qso', align: 'left', sortable: true },
  { name: 'last_qso', label: 'Last QSO', field: 'last_qso', align: 'left', sortable: true },
]
const awardColumns = [
  { name: 'entity_key', label: 'Entity', field: 'entity_key', align: 'left', sortable: true },
  { name: 'worked', label: 'Worked', field: 'worked', align: 'left', sortable: true },
  { name: 'confirmed', label: 'Confirmed', field: 'confirmed', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'last_qso_at', label: 'Last QSO', field: 'last_qso_at', align: 'left', sortable: true },
]
const sotaColumns = [
  { name: 'summit_ref', label: 'Summit', field: 'summit_ref', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'confirmed', label: 'Confirmed', field: 'confirmed', align: 'left', sortable: true },
  { name: 'first_qso', label: 'First QSO', field: 'first_qso', align: 'left', sortable: true },
]

const dxccProgress = computed(() => (dxcc.value.total_entities > 0 ? dxcc.value.worked / dxcc.value.total_entities : 0))
const dxccPercent = computed(() => dxccProgress.value * 100)
const wasProgress = computed(() => (was.value.total_states > 0 ? was.value.worked / was.value.total_states : 0))
const wasPercent = computed(() => wasProgress.value * 100)
const gridsProgress = computed(() => (grids.value.target > 0 ? grids.value.worked / grids.value.target : 0))
const wazProgress = computed(() => (waz.value.target && waz.value.target > 0 ? waz.value.worked / waz.value.target : 0))
const wazPercent = computed(() => wazProgress.value * 100)

const wazZones = computed(() => {
  const worked = new Map(waz.value.rows.map((r) => [toNumber(r.entity_key), r]))
  return Array.from({ length: 40 }, (_, i) => {
    const zone = i + 1
    const row = worked.get(zone)
    return { zone, worked: Boolean(row?.worked), confirmed: Boolean(row?.confirmed) }
  })
})

const filteredDxccRows = computed(() => {
  const rows =
    dxccFilter.value === 'worked'
      ? dxcc.value.entities.filter((row) => row.worked)
      : dxccFilter.value === 'needed'
        ? dxcc.value.entities.filter((row) => !row.worked)
        : dxcc.value.entities
  if (!dxccSearch.value.trim()) {
    return rows
  }
  const needle = dxccSearch.value.trim().toLowerCase()
  return rows.filter((row) => [row.name, row.prefix, row.continent].some((field) => field.toLowerCase().includes(needle)))
})

// Trigger server-side recalculation then reload.  Without this, the Refresh
// button only re-fetches the cached award_progress rows — WAZ/WPX stay empty
// until the periodic worker happens to run.
async function handleRefresh() {
  refreshing.value = true
  error.value = ''
  try {
    await apiPost('/v1/awards/refresh')
  } catch {
    // Non-fatal: fall through to fetchAwards even if the enqueue fails.
  } finally {
    refreshing.value = false
  }
  await fetchAwards()
}

async function fetchAwards() {
  loading.value = true
  error.value = ''
  try {
    const [dxccResp, wasResp, gridsResp, wazResp, wpxResp, potaResp, potaHunterResp, potaActivatorResp, sotaResp] = await Promise.all([
      apiGet<DxccPayload>('/v1/awards/dxcc'),
      apiGet<WasPayload>('/v1/awards/was'),
      apiGet<GridsPayload>('/v1/awards/grids'),
      apiGet<AwardDetailPayload>('/v1/awards/waz'),
      apiGet<AwardDetailPayload>('/v1/awards/wpx'),
      apiGet<PotaPayload>('/v1/awards/pota'),
      apiGet<AwardDetailPayload>('/v1/awards/pota_hunter'),
      apiGet<AwardDetailPayload>('/v1/awards/pota_activator'),
      apiGet<SotaPayload>('/v1/awards/sota'),
    ])

    if (dxccResp.success) dxcc.value = normalizeDxccPayload(dxccResp.data)
    if (wasResp.success) was.value = normalizeWasPayload(wasResp.data)
    if (gridsResp.success) grids.value = normalizeGridsPayload(gridsResp.data)
    if (wazResp.success) waz.value = normalizeAwardDetailPayload(wazResp.data, 'waz', 40)
    if (wpxResp.success) wpx.value = normalizeAwardDetailPayload(wpxResp.data, 'wpx')
    if (potaResp.success) pota.value = normalizePotaPayload(potaResp.data)
    if (potaHunterResp.success) potaHunter.value = normalizeAwardDetailPayload(potaHunterResp.data, 'pota_hunter')
    if (potaActivatorResp.success) potaActivator.value = normalizeAwardDetailPayload(potaActivatorResp.data, 'pota_activator')
    if (sotaResp.success) sota.value = normalizeSotaPayload(sotaResp.data)

  } catch {
    error.value = 'Unable to load award progress right now.'
  } finally {
    loading.value = false
  }
}

function normalizeDxccPayload(payload: unknown): DxccPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<DxccPayload>) : {}
  const entities = Array.isArray(source.entities) ? source.entities : []
  return {
    total_entities: toNumber(source.total_entities),
    worked: toNumber(source.worked),
    confirmed: toNumber(source.confirmed),
    needed: toNumber(source.needed),
    by_band: isRecord(source.by_band) ? source.by_band : {},
    entities: entities.map((entity) => ({
      entity_id: toNumber(entity?.entity_id),
      name: toStringSafe(entity?.name),
      prefix: toStringSafe(entity?.prefix),
      continent: toStringSafe(entity?.continent),
      worked: Boolean(entity?.worked),
      confirmed: Boolean(entity?.confirmed),
      bands: Array.isArray(entity?.bands) ? entity.bands.filter((b): b is string => typeof b === 'string') : [],
      first_qso: toOptionalString(entity?.first_qso),
      qso_count: toNumber(entity?.qso_count),
      qsos: Array.isArray(entity?.qsos)
        ? entity.qsos.map((qso) => ({
            uuid: toStringSafe(qso?.uuid),
            callsign: toStringSafe(qso?.callsign),
            band: toStringSafe(qso?.band),
            mode: toStringSafe(qso?.mode),
            datetime_on: toStringSafe(qso?.datetime_on),
          }))
        : [],
    })),
  }
}

function normalizeWasPayload(payload: unknown): WasPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<WasPayload>) : {}
  const states = Array.isArray(source.states) ? source.states : []
  return {
    total_states: toNumber(source.total_states, 50),
    worked: toNumber(source.worked),
    needed: toNumber(source.needed),
    states: states.map((state) => ({
      code: toStringSafe(state?.code),
      name: toStringSafe(state?.name),
      worked: Boolean(state?.worked),
      qso_count: toNumber(state?.qso_count),
      first_qso: toOptionalString(state?.first_qso),
    })),
  }
}

function normalizeGridsPayload(payload: unknown): GridsPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<GridsPayload>) : {}
  const squares = Array.isArray(source.grid_squares) ? source.grid_squares : []
  return {
    target: toNumber(source.target, 100),
    worked: toNumber(source.worked),
    needed: toNumber(source.needed),
    progress_pct: toNumber(source.progress_pct),
    grid_squares: squares.map((square) => ({
      grid_square: toStringSafe(square?.grid_square),
      qso_count: toNumber(square?.qso_count),
      first_qso: toOptionalString(square?.first_qso),
      last_qso: toOptionalString(square?.last_qso),
    })),
  }
}

function normalizeAwardDetailPayload(payload: unknown, awardType: string, defaultTarget = 0): AwardDetailPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<AwardDetailPayload>) : {}
  const rows = Array.isArray(source.rows) ? source.rows : []
  return {
    award_type: toStringSafe(source.award_type) || awardType,
    worked: toNumber(source.worked),
    confirmed: toNumber(source.confirmed),
    target: source.target !== undefined ? toNumber(source.target) : defaultTarget,
    needed: source.needed !== undefined ? toNumber(source.needed) : undefined,
    progress_pct: source.progress_pct !== undefined ? toNumber(source.progress_pct) : undefined,
    rows: rows.map((row) => ({
      entity_key: toStringSafe(row?.entity_key),
      worked: Boolean(row?.worked),
      confirmed: Boolean(row?.confirmed),
      qso_count: toNumber(row?.qso_count),
      last_qso_at: toOptionalString(row?.last_qso_at),
    })),
  }
}

function normalizePotaPayload(payload: unknown): PotaPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<PotaPayload>) : {}
  return {
    parks_activated: toNumber(source.parks_activated),
    parks_hunted: toNumber(source.parks_hunted),
    activations_total: toNumber(source.activations_total),
    valid_activations: toNumber(source.valid_activations),
  }
}

function normalizeSotaPayload(payload: unknown): SotaPayload {
  const source = payload && typeof payload === 'object' ? (payload as Partial<SotaPayload>) : {}
  const chased = Array.isArray(source.chased) ? source.chased : []
  const activated = Array.isArray(source.activated) ? source.activated : []
  return {
    summits_chased: toNumber(source.summits_chased),
    summits_activated: toNumber(source.summits_activated),
    chased: chased.map((row) => ({
      summit_ref: toStringSafe(row?.summit_ref),
      qso_count: toNumber(row?.qso_count),
      first_qso: toOptionalString(row?.first_qso),
      confirmed: row?.confirmed === undefined ? undefined : Boolean(row?.confirmed),
    })),
    activated: activated.map((row) => ({
      summit_ref: toStringSafe(row?.summit_ref),
      qso_count: toNumber(row?.qso_count),
      first_qso: toOptionalString(row?.first_qso),
      confirmed: row?.confirmed === undefined ? undefined : Boolean(row?.confirmed),
    })),
  }
}

function toNumber(value: unknown, fallback = 0): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '' && Number.isFinite(Number(value))) return Number(value)
  return fallback
}
function toStringSafe(value: unknown): string {
  return typeof value === 'string' ? value : ''
}
function toOptionalString(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined
}
function isRecord(value: unknown): value is Record<string, { worked: number; confirmed: number }> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}
function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString()
}

onMounted(fetchAwards)
</script>

<style scoped>
.awards-page { max-width: 1220px; margin: 0 auto; }
.tabs-shell { background: var(--rl-color-surface-soft); }
.progress-card { background: linear-gradient(140deg, rgba(30, 64, 175, 0.2), rgba(16, 185, 129, 0.08)); }
.awards-tabs { color: var(--rl-color-text-muted); }
.awards-tabs :deep(.q-tab__icon) { color: var(--rl-color-text-muted); }
.awards-tabs :deep(.q-tab--active .q-tab__icon) { color: var(--q-primary); }
.awards-meta { color: var(--rl-color-text-muted); }
.map-card-section {
  background: var(--rl-color-surface-soft);
  border: 1px solid var(--rl-color-border);
  border-radius: 10px;
}
.zone-grid { display: grid; grid-template-columns: repeat(10, minmax(0, 1fr)); gap: 8px; }
</style>
