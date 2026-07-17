import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { apiGet } from 'src/api/client'

export interface BandVisibilityItem {
  name: string
  label: string
  band_group?: string
  is_common: boolean
  is_default_visible: boolean
  is_visible: boolean
  sort_order: number
}

export interface ModeVisibilityItem {
  name: string
  label: string
  category?: string
  is_popular: boolean
  is_default_visible: boolean
  is_visible: boolean
  sort_order: number
}

interface BandModeVisibilityResponse {
  itu_region?: number | null
  region_source?: 'explicit' | 'callsign_prefix' | 'unknown'
  bands: BandVisibilityItem[]
  modes: ModeVisibilityItem[]
  visible_bands?: string[]
  visible_modes?: string[]
}

export const useBandModePreferencesStore = defineStore('bandModePreferences', () => {
  const loading = ref(false)
  const loaded = ref(false)
  const ituRegion = ref<number | null>(null)
  const regionSource = ref<'explicit' | 'callsign_prefix' | 'unknown'>('unknown')
  const bands = ref<BandVisibilityItem[]>([])
  const modes = ref<ModeVisibilityItem[]>([])

  const visibleBands = computed(() => bands.value.filter((band) => band.is_visible))
  const visibleModes = computed(() => modes.value.filter((mode) => mode.is_visible))
  const bandOptions = computed(() => visibleBands.value.map((band) => ({ label: band.label, value: band.name })))
  const modeOptions = computed(() => visibleModes.value.map((mode) => ({ label: mode.label, value: mode.name })))
  const allBandOptions = computed(() => bands.value.map((band) => ({ label: band.label, value: band.name })))
  const allModeOptions = computed(() => modes.value.map((mode) => ({ label: mode.label, value: mode.name })))

  async function load(force = false) {
    if (loading.value) {
      return
    }
    if (loaded.value && !force) {
      return
    }

    loading.value = true
    try {
      const response = await apiGet<BandModeVisibilityResponse>('/v1/preferences/band-mode-visibility')
      if (response.success && response.data) {
        ituRegion.value = response.data.itu_region ?? null
        regionSource.value = response.data.region_source || 'unknown'
        bands.value = Array.isArray(response.data.bands) ? response.data.bands : []
        modes.value = Array.isArray(response.data.modes) ? response.data.modes : []
        loaded.value = true
      }
    } catch {
      reset()
    } finally {
      loading.value = false
    }
  }

  function reset() {
    loading.value = false
    loaded.value = false
    ituRegion.value = null
    regionSource.value = 'unknown'
    bands.value = []
    modes.value = []
  }

  return {
    loading,
    loaded,
    ituRegion,
    regionSource,
    bands,
    modes,
    visibleBands,
    visibleModes,
    bandOptions,
    modeOptions,
    allBandOptions,
    allModeOptions,
    load,
    reset,
  }
})
