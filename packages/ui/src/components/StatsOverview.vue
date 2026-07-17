<script setup lang="ts">
import type { OverviewStats } from '../composables/useStats'

defineProps<{
  stats: OverviewStats | null
  loading?: boolean
}>()
</script>

<template>
  <div class="stats-overview">
    <div v-if="loading" class="loading">Loading statistics…</div>
    <div v-else-if="!stats" class="empty">No statistics available</div>
    <div v-else class="kpi-grid">
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.total_qsos.toLocaleString() }}</div>
        <div class="kpi-label">Total QSOs</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.unique_callsigns.toLocaleString() }}</div>
        <div class="kpi-label">Callsigns</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.unique_countries.toLocaleString() }}</div>
        <div class="kpi-label">Countries</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.unique_states.toLocaleString() }}</div>
        <div class="kpi-label">States</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.unique_grids.toLocaleString() }}</div>
        <div class="kpi-label">Grid Squares</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.bands_used.toLocaleString() }}</div>
        <div class="kpi-label">Bands</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-value">{{ stats.modes_used.toLocaleString() }}</div>
        <div class="kpi-label">Modes</div>
      </div>
      <div v-if="stats.first_qso" class="kpi-card kpi-wide">
        <div class="kpi-value kpi-value--sm">{{ new Date(stats.first_qso).toLocaleDateString() }}</div>
        <div class="kpi-label">First QSO</div>
      </div>
      <div v-if="stats.last_qso" class="kpi-card kpi-wide">
        <div class="kpi-value kpi-value--sm">{{ new Date(stats.last_qso).toLocaleDateString() }}</div>
        <div class="kpi-label">Last QSO</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 12px;
}

.kpi-card {
  background: var(--rl-surface, #16213e);
  border: 1px solid var(--rl-surface-2, #0f3460);
  border-radius: var(--rl-radius, 8px);
  padding: 14px 12px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.kpi-wide {
  grid-column: span 2;
}

.kpi-value {
  font-size: 1.6rem;
  font-weight: 700;
  color: var(--rl-accent, #e94560);
  line-height: 1.2;
}

.kpi-value--sm {
  font-size: 1.1rem;
}

.kpi-label {
  font-size: 0.72rem;
  color: var(--rl-text-dim, #9a9a9a);
  margin-top: 4px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.loading, .empty {
  color: var(--rl-text-dim, #9a9a9a);
  font-style: italic;
  padding: 20px 0;
  text-align: center;
}
</style>
