/**
 * Statistics dashboard — fetches data from the RadioLedger stats API and
 * renders charts + tables using Chart.js (loaded via CDN in index.html).
 *
 * Extracted from main.ts to isolate stats ownership and reduce the god-file.
 */

import { invoke } from '@tauri-apps/api/core'

// ─── Types ───────────────────────────────────────────────────────────────────

interface AuthStatus {
  logged_in: boolean
  callsign: string | null
}

interface ApiEnvelope<T> {
  success: boolean
  message: string
  data: T
  error?: string
}

interface OverviewStats {
  total_qsos: number
  unique_callsigns: number
  unique_countries: number
  unique_states: number
  unique_grids: number
  bands_used: number
  modes_used: number
  first_qso?: string
  last_qso?: string
}

interface BandEntry { band: string; count: number }
interface ModeEntry { mode: string; count: number }
interface PeriodEntry { period: string; count: number }
interface CountriesOverTimeEntry { period: string; unique_countries: number }
interface CallsignEntry { callsign: string; count: number }
interface CountryEntry { name: string; count: number }
interface PatternEntry { day_of_week: number; hour_of_day: number; count: number }

// ─── Chart.js type shim ──────────────────────────────────────────────────────

declare const Chart: any

// ─── Module state ─────────────────────────────────────────────────────────────

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

let bandChart: any = null
let modeChart: any = null
let periodChart: any = null
let cotChart: any = null
let statsLoaded = false
let statsRefreshPromise: Promise<boolean> | null = null

/**
 * External logger callback — injected from the shell bootstrap to avoid circular imports.
 * Defaults to console.log so the module works standalone in tests.
 */
let logger: (msg: string) => void = (msg: string) => { console.log(msg) }

/** Set the logger used by stats-dashboard functions. */
export function setLogger(fn: (msg: string) => void): void {
  logger = fn
}

/** Reset the cached stats state (call after auth change or settings save). */
export function resetStatsCache(): void {
  statsLoaded = false
}

/** Whether stats data has been loaded at least once. */
export function isStatsLoaded(): boolean {
  return statsLoaded
}

// ─── API helpers ──────────────────────────────────────────────────────────────

async function statsApiGet<T>(path: string, params?: Record<string, string>): Promise<T | null> {
  try {
    const authStatus: AuthStatus = await invoke('get_auth_status')
    if (!authStatus.logged_in) return null
    let apiPath = path
    if (params) {
      const qs = new URLSearchParams(params).toString()
      apiPath = qs ? `${path}?${qs}` : path
    }
    const body: string = await invoke('api_get', { path: apiPath })
    const json: ApiEnvelope<T> = JSON.parse(body)
    if (!json.success) return null
    return json.data
  } catch {
    return null
  }
}

// ─── DOM helpers ──────────────────────────────────────────────────────────────

function setStatText(id: string, value: string | number): void {
  const el = document.getElementById(id)
  if (el) el.textContent = String(value)
}

function makeCanvas(id: string): HTMLCanvasElement | null {
  return document.getElementById(id) as HTMLCanvasElement | null
}

// ─── Dashboard loaders ───────────────────────────────────────────────────────

async function loadStatsOverview(): Promise<void> {
  const data = await statsApiGet<OverviewStats>('/v1/stats/overview')
  if (!data) return
  setStatText('stat-total-qsos', data.total_qsos.toLocaleString())
  setStatText('stat-unique-callsigns', data.unique_callsigns.toLocaleString())
  setStatText('stat-unique-countries', data.unique_countries.toLocaleString())
  setStatText('stat-unique-states', data.unique_states.toLocaleString())
  setStatText('stat-unique-grids', data.unique_grids.toLocaleString())
  setStatText('stat-bands-used', data.bands_used.toLocaleString())
  setStatText('stat-modes-used', data.modes_used.toLocaleString())
  if (data.first_qso) setStatText('stat-first-qso', new Date(data.first_qso).toLocaleDateString())
  if (data.last_qso) setStatText('stat-last-qso', new Date(data.last_qso).toLocaleDateString())
}

async function loadBandChart(): Promise<void> {
  const data = await statsApiGet<BandEntry[]>('/v1/stats/by-band')
  if (!data || data.length === 0) return
  const canvas = makeCanvas('chart-band')
  if (!canvas || typeof Chart === 'undefined') return
  if (bandChart) bandChart.destroy()
  bandChart = new Chart(canvas, {
    type: 'bar',
    data: {
      labels: data.map((d) => d.band),
      datasets: [{ label: 'QSOs', data: data.map((d) => d.count), backgroundColor: '#f59e0b' }],
    },
    options: {
      responsive: true,
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true, ticks: { precision: 0 } } },
    },
  })
}

async function loadModeChart(): Promise<void> {
  const data = await statsApiGet<ModeEntry[]>('/v1/stats/by-mode')
  if (!data || data.length === 0) return
  const canvas = makeCanvas('chart-mode')
  if (!canvas || typeof Chart === 'undefined') return
  if (modeChart) modeChart.destroy()
  const palette = ['#f59e0b', '#ef5350', '#ab47bc', '#66bb6a', '#ffca28', '#ff7043', '#f97316']
  modeChart = new Chart(canvas, {
    type: 'doughnut',
    data: {
      labels: data.map((d) => d.mode),
      datasets: [{ data: data.map((d) => d.count), backgroundColor: palette }],
    },
    options: { responsive: true, plugins: { legend: { position: 'right' } } },
  })
}

async function loadPeriodChart(): Promise<void> {
  const data = await statsApiGet<PeriodEntry[]>('/v1/stats/by-period', { group: 'month' })
  if (!data || data.length === 0) return
  const canvas = makeCanvas('chart-period')
  if (!canvas || typeof Chart === 'undefined') return
  if (periodChart) periodChart.destroy()
  periodChart = new Chart(canvas, {
    type: 'line',
    data: {
      labels: data.map((d) => d.period),
      datasets: [{
        label: 'QSOs',
        data: data.map((d) => d.count),
        borderColor: '#f59e0b',
        backgroundColor: 'rgba(245,158,11,0.22)',
        fill: true,
        tension: 0.3,
      }],
    },
    options: {
      responsive: true,
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true, ticks: { precision: 0 } } },
    },
  })
}

async function loadCountriesOverTimeChart(): Promise<void> {
  const data = await statsApiGet<CountriesOverTimeEntry[]>('/v1/stats/countries-over-time')
  if (!data || data.length === 0) return
  const canvas = makeCanvas('chart-cot')
  if (!canvas || typeof Chart === 'undefined') return
  if (cotChart) cotChart.destroy()
  cotChart = new Chart(canvas, {
    type: 'line',
    data: {
      labels: data.map((d) => d.period),
      datasets: [{
        label: 'Unique Countries',
        data: data.map((d) => d.unique_countries),
        borderColor: '#66bb6a',
        backgroundColor: 'rgba(102,187,106,0.2)',
        fill: true,
        tension: 0.3,
      }],
    },
    options: {
      responsive: true,
      plugins: { legend: { display: false } },
      scales: { y: { beginAtZero: true, ticks: { precision: 0 } } },
    },
  })
}

async function loadTopCallsigns(): Promise<void> {
  const data = await statsApiGet<CallsignEntry[]>('/v1/stats/top-callsigns', { limit: '20' })
  if (!data || data.length === 0) return
  const tbody = document.getElementById('table-top-callsigns')
  if (!tbody) return
  tbody.innerHTML = data
    .map((d, i) =>
      `<tr><td>${i + 1}</td><td class="callsign-cell">${d.callsign}</td><td>${d.count.toLocaleString()}</td></tr>`)
    .join('')
}

async function loadTopCountries(): Promise<void> {
  const data = await statsApiGet<CountryEntry[]>('/v1/stats/top-countries', { limit: '20' })
  if (!data || data.length === 0) return
  const tbody = document.getElementById('table-top-countries')
  if (!tbody) return
  tbody.innerHTML = data
    .map((d, i) =>
      `<tr><td>${i + 1}</td><td>${d.name}</td><td>${d.count.toLocaleString()}</td></tr>`)
    .join('')
}

async function loadOperatingPatterns(): Promise<void> {
  const data = await statsApiGet<PatternEntry[]>('/v1/stats/operating-patterns')
  if (!data || data.length === 0) return

  const grid: number[][] = Array.from({ length: 7 }, () => new Array(24).fill(0))
  let maxVal = 0
  for (const entry of data) {
    const d = entry.day_of_week
    const h = entry.hour_of_day
    if (d >= 0 && d < 7 && h >= 0 && h < 24) {
      grid[d][h] = entry.count
      if (entry.count > maxVal) maxVal = entry.count
    }
  }

  const container = document.getElementById('heatmap-container')
  if (!container) return

  let html = '<table class="heatmap-table"><thead><tr><th></th>'
  for (let h = 0; h < 24; h++) html += `<th>${h.toString().padStart(2, '0')}</th>`
  html += '</tr></thead><tbody>'
  for (let d = 0; d < 7; d++) {
    html += `<tr><td class="heatmap-day">${DAY_LABELS[d]}</td>`
    for (let h = 0; h < 24; h++) {
      const val = grid[d][h]
      const intensity = maxVal > 0 ? val / maxVal : 0
      const alpha = Math.round(intensity * 200)
      const bg = `rgba(245,158,11,${(alpha / 255).toFixed(2)})`
      const title = `${DAY_LABELS[d]} ${h.toString().padStart(2, '0')}:00 — ${val} QSO${val !== 1 ? 's' : ''}`
      html += `<td class="heatmap-cell" style="background:${bg}" title="${title}"></td>`
    }
    html += '</tr>'
  }
  html += '</tbody></table>'
  container.innerHTML = html
}

// ─── Public refresh ───────────────────────────────────────────────────────────

/** Refresh all stats panels. Skips if already loaded (pass force=true to override). */
export async function refreshStats(force = true): Promise<boolean> {
  if (statsRefreshPromise) return statsRefreshPromise

  statsRefreshPromise = (async () => {
    const refreshBtn = document.getElementById('stats-refresh-btn') as HTMLButtonElement | null
    const previousLabel = refreshBtn?.textContent ?? '↻ Refresh'
    if (refreshBtn) {
      refreshBtn.disabled = true
      refreshBtn.textContent = 'Refreshing…'
    }

    try {
      logger('Refreshing statistics dashboard…')

      const authStatus: AuthStatus = await invoke('get_auth_status')
      if (!authStatus.logged_in) {
        statsLoaded = false
        logger('Stats: not logged in — skipping')
        return false
      }

      if (!force && statsLoaded) return true

      await Promise.allSettled([
        loadStatsOverview(),
        loadBandChart(),
        loadModeChart(),
        loadPeriodChart(),
        loadCountriesOverTimeChart(),
        loadTopCallsigns(),
        loadTopCountries(),
        loadOperatingPatterns(),
      ])
      statsLoaded = true
      logger('Statistics refreshed')
      return true
    } finally {
      if (refreshBtn) {
        refreshBtn.disabled = false
        refreshBtn.textContent = previousLabel
      }
      statsRefreshPromise = null
    }
  })()

  return statsRefreshPromise
}