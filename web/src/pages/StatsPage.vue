<template>
  <q-page class="q-pa-md">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Statistics Dashboard</div>
        <div class="text-caption text-grey-6">Operating trends, activity patterns, and top DX insights</div>
      </div>
      <q-btn flat dense icon="refresh" label="Refresh" :loading="loading" @click="loadDashboard" />
    </div>

    <div class="row q-col-gutter-md q-mb-md">
      <div v-for="card in overviewCards" :key="card.label" class="col-12 col-sm-6 col-lg-3">
        <q-card bordered flat>
          <q-card-section>
            <div class="text-caption text-grey-6">{{ card.label }}</div>
            <q-skeleton v-if="loading" type="text" width="72px" height="38px" />
            <div v-else class="text-h5">{{ card.value }}</div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <div class="row q-col-gutter-md">
      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section>
            <div class="text-subtitle1">QSOs by Band</div>
            <div class="text-caption text-grey-6">Distribution by amateur band</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loading" type="rect" height="280px" />
            <Bar v-else :data="bandChartData" :options="barOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section>
            <div class="text-subtitle1">QSOs by Mode</div>
            <div class="text-caption text-grey-6">Mode operating mix (SSB, CW, FT8, ...)</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loading" type="rect" height="280px" />
            <Doughnut v-else :data="modeChartData" :options="doughnutOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section class="row items-center justify-between">
            <div>
              <div class="text-subtitle1">QSOs Over Time</div>
              <div class="text-caption text-grey-6">Growth trend by month/year</div>
            </div>
            <q-btn-toggle
              v-model="periodGrouping"
              dense
              unelevated
              toggle-color="primary"
              :options="[
                { label: 'Month', value: 'month' },
                { label: 'Year', value: 'year' },
              ]"
              @update:model-value="loadPeriodSeries"
            />
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loadingPeriod || loading" type="rect" height="280px" />
            <Line v-else :data="qsoTrendData" :options="lineOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section>
            <div class="text-subtitle1">Countries Worked Over Time</div>
            <div class="text-caption text-grey-6">Cumulative countries worked by month</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loading" type="rect" height="280px" />
            <Line v-else :data="countriesTrendData" :options="lineOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section>
            <div class="text-subtitle1">Top 10 Callsigns</div>
            <div class="text-caption text-grey-6">Most frequently worked stations</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loading" type="rect" height="280px" />
            <Bar v-else :data="topCallsignsData" :options="horizontalBarOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-lg-6">
        <q-card bordered flat class="chart-card">
          <q-card-section>
            <div class="text-subtitle1">Top 10 Countries</div>
            <div class="text-caption text-grey-6">Most active DX entities worked</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <q-skeleton v-if="loading" type="rect" height="280px" />
            <Bar v-else :data="topCountriesData" :options="horizontalBarOptions" />
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12">
        <q-card bordered flat>
          <q-card-section>
            <div class="text-subtitle1">Operating Patterns Heatmap</div>
            <div class="text-caption text-grey-6">Hour of day vs day of week activity intensity</div>
          </q-card-section>
          <q-separator />
          <q-card-section>
            <div v-if="loading" class="q-gutter-y-sm">
              <q-skeleton v-for="n in 8" :key="n" type="rect" height="20px" />
            </div>
            <div v-else class="heatmap-wrap">
              <div class="heatmap-grid" :style="{ '--heatmap-cols': dayLabels.length }">
                <div class="heatmap-header heatmap-corner"></div>
                <div v-for="day in dayLabels" :key="day" class="heatmap-header">{{ day }}</div>

                <template v-for="hour in 24" :key="`h-${hour - 1}`">
                  <div class="heatmap-hour">{{ `${String(hour - 1).padStart(2, '0')}:00` }}</div>
                  <div
                    v-for="day in 7"
                    :key="`${hour - 1}-${day - 1}`"
                    class="heatmap-cell"
                    :title="cellTooltip(day - 1, hour - 1)"
                    :style="{ backgroundColor: heatColor(day - 1, hour - 1) }"
                  >
                    <span>{{ heatmapValue(day - 1, hour - 1) || '' }}</span>
                  </div>
                </template>
              </div>
            </div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <q-banner v-if="errorMessage" rounded dense inline-actions class="bg-red-1 text-negative q-mt-md">
      {{ errorMessage }}
      <template #action>
        <q-btn flat color="negative" label="Retry" @click="loadDashboard" />
      </template>
    </q-banner>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Bar, Doughnut, Line } from 'vue-chartjs'
import {
  ArcElement,
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
  type ChartData,
  type ChartOptions,
} from 'chart.js'
import { apiGet } from 'src/api/client'
import { useQuasar } from 'quasar'

ChartJS.register(CategoryScale, LinearScale, BarElement, ArcElement, PointElement, LineElement, Filler, Tooltip, Legend)

type PeriodGrouping = 'month' | 'year'

interface OverviewStats {
  total_qsos: number
  unique_callsigns: number
  unique_countries: number
  unique_states: number
  unique_grids: number
  bands_used: number
  modes_used: number
}

interface BandEntry {
  band: string
  count: number
}

interface ModeEntry {
  mode: string
  count: number
}

interface PeriodEntry {
  period: string
  count: number
}

interface CountryPeriodEntry {
  period: string
  unique_countries: number
}

interface CallsignEntry {
  callsign: string
  count: number
}

interface CountryEntry {
  name: string
  count: number
}

interface HeatmapEntry {
  day_of_week: number
  hour_of_day: number
  count: number
}

const $q = useQuasar()

const loading = ref(true)
const loadingPeriod = ref(false)
const errorMessage = ref('')
const periodGrouping = ref<PeriodGrouping>('month')

const overview = ref<OverviewStats | null>(null)
const byBand = ref<BandEntry[]>([])
const byMode = ref<ModeEntry[]>([])
const byPeriod = ref<PeriodEntry[]>([])
const countriesOverTime = ref<CountryPeriodEntry[]>([])
const topCallsigns = ref<CallsignEntry[]>([])
const topCountries = ref<CountryEntry[]>([])
const heatmap = ref<HeatmapEntry[]>([])

const chartPalette = ['#4FC3F7', '#7E57C2', '#66BB6A', '#FFCA28', '#FF7043', '#26A69A', '#EF5350', '#9CCC65']
const dayLabels = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

interface ThemeTokens {
  text: string
  muted: string
  border: string
  heatmapEmpty: string
}

const themeTokens = ref<ThemeTokens>({
  text: '#1f2937',
  muted: '#6b7280',
  border: 'rgba(15, 23, 42, 0.12)',
  heatmapEmpty: 'rgba(15, 23, 42, 0.06)',
})

function readThemeTokens() {
  const styles = getComputedStyle(document.body)
  themeTokens.value = {
    text: styles.getPropertyValue('--rl-color-text').trim() || '#1f2937',
    muted: styles.getPropertyValue('--rl-color-text-muted').trim() || '#6b7280',
    border: styles.getPropertyValue('--rl-color-border').trim() || 'rgba(15, 23, 42, 0.12)',
    heatmapEmpty: styles.getPropertyValue('--rl-color-heatmap-empty').trim() || 'rgba(15, 23, 42, 0.06)',
  }
}

const overviewCards = computed(() => [
  { label: 'Total QSOs', value: formatNumber(overview.value?.total_qsos ?? 0) },
  { label: 'Countries Worked', value: formatNumber(overview.value?.unique_countries ?? 0) },
  { label: 'Modes Used', value: formatNumber(overview.value?.modes_used ?? 0) },
  { label: 'Bands Used', value: formatNumber(overview.value?.bands_used ?? 0) },
])

const barOptions = computed<ChartOptions<'bar'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
  },
  scales: {
    x: {
      ticks: {
        color: themeTokens.value.muted,
      },
      grid: {
        color: themeTokens.value.border,
      },
    },
    y: {
      ticks: {
        precision: 0,
        color: themeTokens.value.muted,
      },
      grid: {
        color: themeTokens.value.border,
      },
    },
  },
}))

const horizontalBarOptions = computed<ChartOptions<'bar'>>(() => ({
  ...barOptions.value,
  indexAxis: 'y',
}))

const lineOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  tension: 0.25,
  plugins: {
    legend: { display: false },
  },
  scales: {
    x: {
      ticks: {
        color: themeTokens.value.muted,
      },
      grid: {
        color: themeTokens.value.border,
      },
    },
    y: {
      beginAtZero: true,
      ticks: {
        precision: 0,
        color: themeTokens.value.muted,
      },
      grid: {
        color: themeTokens.value.border,
      },
    },
  },
}))

const doughnutOptions = computed<ChartOptions<'doughnut'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      position: 'bottom',
      labels: {
        color: themeTokens.value.muted,
      },
    },
  },
}))

const bandChartData = computed<ChartData<'bar'>>(() => ({
  labels: byBand.value.map((entry) => entry.band),
  datasets: [
    {
      label: 'QSOs',
      data: byBand.value.map((entry) => entry.count),
      backgroundColor: byBand.value.map((_, i) => chartPalette[i % chartPalette.length]),
      borderRadius: 6,
    },
  ],
}))

const modeChartData = computed<ChartData<'doughnut'>>(() => ({
  labels: byMode.value.map((entry) => entry.mode),
  datasets: [
    {
      data: byMode.value.map((entry) => entry.count),
      backgroundColor: byMode.value.map((_, i) => chartPalette[i % chartPalette.length]),
      borderWidth: 0,
    },
  ],
}))

const qsoTrendData = computed<ChartData<'line'>>(() => ({
  labels: byPeriod.value.map((entry) => entry.period),
  datasets: [
    {
      label: 'QSOs',
      data: byPeriod.value.map((entry) => entry.count),
      borderColor: '#42A5F5',
      backgroundColor: 'rgba(66, 165, 245, 0.2)',
      fill: true,
    },
  ],
}))

const countriesTrendData = computed<ChartData<'line'>>(() => ({
  labels: countriesOverTime.value.map((entry) => entry.period),
  datasets: [
    {
      label: 'Countries',
      data: countriesOverTime.value.map((entry) => entry.unique_countries),
      borderColor: '#66BB6A',
      backgroundColor: 'rgba(102, 187, 106, 0.2)',
      fill: true,
    },
  ],
}))

const topCallsignsData = computed<ChartData<'bar'>>(() => ({
  labels: topCallsigns.value.map((entry) => entry.callsign),
  datasets: [
    {
      label: 'QSOs',
      data: topCallsigns.value.map((entry) => entry.count),
      backgroundColor: '#7E57C2',
      borderRadius: 6,
    },
  ],
}))

const topCountriesData = computed<ChartData<'bar'>>(() => ({
  labels: topCountries.value.map((entry) => entry.name),
  datasets: [
    {
      label: 'QSOs',
      data: topCountries.value.map((entry) => entry.count),
      backgroundColor: '#26A69A',
      borderRadius: 6,
    },
  ],
}))

const heatmapLookup = computed(() => {
  const lookup = new Map<string, number>()
  for (const entry of heatmap.value) {
    lookup.set(`${entry.day_of_week}-${entry.hour_of_day}`, entry.count)
  }
  return lookup
})

const maxHeatmap = computed(() => {
  const values = Array.from(heatmapLookup.value.values())
  return values.length ? Math.max(...values) : 0
})

function heatmapValue(dayOfWeek: number, hourOfDay: number): number {
  return heatmapLookup.value.get(`${dayOfWeek}-${hourOfDay}`) ?? 0
}

function heatColor(dayOfWeek: number, hourOfDay: number): string {
  const value = heatmapValue(dayOfWeek, hourOfDay)
  const max = maxHeatmap.value
  if (value === 0 || max === 0) {
    return themeTokens.value.heatmapEmpty
  }

  const intensity = value / max
  const alpha = 0.15 + intensity * 0.75
  return `rgba(76, 175, 80, ${alpha.toFixed(2)})`
}

function cellTooltip(dayOfWeek: number, hourOfDay: number): string {
  const value = heatmapValue(dayOfWeek, hourOfDay)
  return `${dayLabels[dayOfWeek]} ${String(hourOfDay).padStart(2, '0')}:00 — ${value} QSOs`
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

async function loadPeriodSeries() {
  loadingPeriod.value = true
  try {
    const response = await apiGet<PeriodEntry[]>(`/v1/stats/by-period?period=${periodGrouping.value}`)
    if (response.success && response.data) {
      byPeriod.value = response.data
    }
  } catch {
    errorMessage.value = 'Unable to load QSO trend series right now.'
  } finally {
    loadingPeriod.value = false
  }
}

async function loadDashboard() {
  loading.value = true
  errorMessage.value = ''

  try {
    const [
      overviewRes,
      byBandRes,
      byModeRes,
      countriesOverTimeRes,
      topCallsignsRes,
      topCountriesRes,
      heatmapRes,
      periodRes,
    ] = await Promise.all([
      apiGet<OverviewStats>('/v1/stats/overview'),
      apiGet<BandEntry[]>('/v1/stats/by-band'),
      apiGet<ModeEntry[]>('/v1/stats/by-mode'),
      apiGet<CountryPeriodEntry[]>('/v1/stats/countries-over-time'),
      apiGet<CallsignEntry[]>('/v1/stats/top-callsigns?limit=10'),
      apiGet<CountryEntry[]>('/v1/stats/top-countries?limit=10'),
      apiGet<HeatmapEntry[]>('/v1/stats/activity-heatmap'),
      apiGet<PeriodEntry[]>(`/v1/stats/by-period?period=${periodGrouping.value}`),
    ])

    overview.value = overviewRes.data || null
    byBand.value = byBandRes.data || []
    byMode.value = byModeRes.data || []
    countriesOverTime.value = countriesOverTimeRes.data || []
    topCallsigns.value = topCallsignsRes.data || []
    topCountries.value = topCountriesRes.data || []
    heatmap.value = heatmapRes.data || []
    byPeriod.value = periodRes.data || []
  } catch {
    errorMessage.value = 'Unable to load statistics dashboard data. Please retry.'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  readThemeTokens()
  void loadDashboard()
})

watch(() => $q.dark.isActive, () => {
  readThemeTokens()
})
</script>

<style scoped>
.chart-card {
  min-height: 380px;
}

.chart-card :deep(canvas) {
  min-height: 260px;
}

.heatmap-wrap {
  overflow-x: auto;
}

.heatmap-grid {
  display: grid;
  grid-template-columns: 72px repeat(var(--heatmap-cols), minmax(60px, 1fr));
  gap: 6px;
  min-width: 640px;
}

.heatmap-header {
  text-align: center;
  font-size: 0.75rem;
  color: var(--rl-color-text-muted);
}

.heatmap-hour {
  font-size: 0.75rem;
  color: var(--rl-color-text-muted);
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-right: 4px;
}

.heatmap-cell {
  border-radius: 4px;
  min-height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.68rem;
  color: var(--rl-color-heatmap-cell-text);
}

.heatmap-cell span {
  opacity: 0.9;
}
</style>
