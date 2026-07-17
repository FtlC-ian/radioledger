<script setup lang="ts">
import type { CallsignEntry, CountryEntry } from '../composables/useStats'

defineProps<{
  callsigns: CallsignEntry[]
  countries: CountryEntry[]
}>()
</script>

<template>
  <div class="tables-row">
    <div class="table-card">
      <h3>Top Callsigns</h3>
      <table class="stats-table">
        <thead>
          <tr><th>#</th><th>Callsign</th><th>QSOs</th></tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in callsigns" :key="row.callsign">
            <td class="num">{{ i + 1 }}</td>
            <td class="callsign">{{ row.callsign }}</td>
            <td>{{ row.count.toLocaleString() }}</td>
          </tr>
          <tr v-if="!callsigns.length">
            <td colspan="3" class="empty">No data</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="table-card">
      <h3>Top Countries</h3>
      <table class="stats-table">
        <thead>
          <tr><th>#</th><th>Country</th><th>QSOs</th></tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in countries" :key="row.name">
            <td class="num">{{ i + 1 }}</td>
            <td>{{ row.name }}</td>
            <td>{{ row.count.toLocaleString() }}</td>
          </tr>
          <tr v-if="!countries.length">
            <td colspan="3" class="empty">No data</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.tables-row {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.table-card {
  background: var(--rl-surface, #16213e);
  border: 1px solid var(--rl-surface-2, #0f3460);
  border-radius: var(--rl-radius, 8px);
  padding: 16px;
  overflow-x: auto;
}

h3 {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--rl-text-dim, #9a9a9a);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 12px;
}

.stats-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.82rem;
}

.stats-table thead th {
  color: var(--rl-text-dim, #9a9a9a);
  font-weight: 600;
  text-align: left;
  padding: 4px 8px;
  border-bottom: 1px solid var(--rl-surface-2, #0f3460);
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.stats-table tbody td {
  padding: 5px 8px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  color: var(--rl-text, #eaeaea);
}

.stats-table tbody tr:last-child td { border-bottom: none; }

.num { color: var(--rl-text-dim, #9a9a9a); }

.callsign {
  font-family: 'Courier New', Courier, monospace;
  font-weight: 600;
  color: var(--rl-accent, #e94560) !important;
}

.empty {
  color: var(--rl-text-dim, #9a9a9a);
  font-style: italic;
  text-align: center;
  padding: 12px;
}
</style>
