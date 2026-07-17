import { ref } from 'vue'
import type { ApiAdapter } from './useApi'
import { useApi } from './useApi'

export interface OverviewStats {
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

export interface BandEntry { band: string; count: number }
export interface ModeEntry { mode: string; count: number }
export interface PeriodEntry { period: string; count: number }
export interface CountriesOverTimeEntry { period: string; unique_countries: number }
export interface CallsignEntry { callsign: string; count: number }
export interface CountryEntry { name: string; count: number }
export interface PatternEntry { day_of_week: number; hour_of_day: number; count: number }

export function useStats(adapter: ApiAdapter) {
  const { get } = useApi(adapter)

  const overview = ref<OverviewStats | null>(null)
  const byBand = ref<BandEntry[]>([])
  const byMode = ref<ModeEntry[]>([])
  const byPeriod = ref<PeriodEntry[]>([])
  const countriesOverTime = ref<CountriesOverTimeEntry[]>([])
  const topCallsigns = ref<CallsignEntry[]>([])
  const topCountries = ref<CountryEntry[]>([])
  const operatingPatterns = ref<PatternEntry[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function loadAll() {
    loading.value = true
    error.value = null
    try {
      await Promise.allSettled([
        get<OverviewStats>('/v1/stats/overview').then((d) => { if (d) overview.value = d }),
        get<BandEntry[]>('/v1/stats/by-band').then((d) => { if (d) byBand.value = d }),
        get<ModeEntry[]>('/v1/stats/by-mode').then((d) => { if (d) byMode.value = d }),
        get<PeriodEntry[]>('/v1/stats/by-period', { group: 'month' }).then((d) => { if (d) byPeriod.value = d }),
        get<CountriesOverTimeEntry[]>('/v1/stats/countries-over-time').then((d) => { if (d) countriesOverTime.value = d }),
        get<CallsignEntry[]>('/v1/stats/top-callsigns', { limit: '20' }).then((d) => { if (d) topCallsigns.value = d }),
        get<CountryEntry[]>('/v1/stats/top-countries', { limit: '20' }).then((d) => { if (d) topCountries.value = d }),
        get<PatternEntry[]>('/v1/stats/operating-patterns').then((d) => { if (d) operatingPatterns.value = d }),
      ])
    } finally {
      loading.value = false
    }
  }

  return {
    overview, byBand, byMode, byPeriod, countriesOverTime,
    topCallsigns, topCountries, operatingPatterns,
    loading, error, loadAll,
  }
}
