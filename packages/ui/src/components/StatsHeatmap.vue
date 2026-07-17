<script setup lang="ts">
import { computed } from 'vue'
import type { PatternEntry } from '../composables/useStats'

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

const props = defineProps<{ data: PatternEntry[] }>()

interface Cell {
  day: number
  hour: number
  count: number
  intensity: number
  label: string
}

const grid = computed<Cell[][]>(() => {
  const g: number[][] = Array.from({ length: 7 }, () => new Array(24).fill(0))
  let maxVal = 0
  for (const e of props.data) {
    if (e.day_of_week >= 0 && e.day_of_week < 7 && e.hour_of_day >= 0 && e.hour_of_day < 24) {
      g[e.day_of_week][e.hour_of_day] = e.count
      if (e.count > maxVal) maxVal = e.count
    }
  }
  return g.map((row, d) =>
    row.map((count, h) => ({
      day: d,
      hour: h,
      count,
      intensity: maxVal > 0 ? count / maxVal : 0,
      label: `${DAY_LABELS[d]} ${String(h).padStart(2, '0')}:00 — ${count} QSO${count !== 1 ? 's' : ''}`,
    }))
  )
})

function cellBg(intensity: number): string {
  return `rgba(79,195,247,${(intensity * 0.8).toFixed(2)})`
}
</script>

<template>
  <div class="heatmap-card">
    <h3>Operating Patterns (UTC)</h3>
    <p v-if="!data.length" class="empty">No data</p>
    <div v-else class="heatmap-wrap">
      <table class="heatmap-table">
        <thead>
          <tr>
            <th></th>
            <th v-for="h in 24" :key="h">{{ String(h - 1).padStart(2, '0') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, d) in grid" :key="d">
            <td class="day-label">{{ DAY_LABELS[d] }}</td>
            <td
              v-for="cell in row"
              :key="cell.hour"
              class="heatmap-cell"
              :style="{ background: cellBg(cell.intensity) }"
              :title="cell.label"
            ></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.heatmap-card {
  background: var(--rl-surface, #16213e);
  border: 1px solid var(--rl-surface-2, #0f3460);
  border-radius: var(--rl-radius, 8px);
  padding: 16px;
}

h3 {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--rl-text-dim, #9a9a9a);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 12px;
}

.heatmap-wrap {
  overflow-x: auto;
}

.heatmap-table {
  border-collapse: collapse;
  font-size: 0.7rem;
  min-width: 600px;
}

.heatmap-table th {
  color: var(--rl-text-dim, #9a9a9a);
  font-weight: 400;
  padding: 2px 3px;
  text-align: center;
  min-width: 22px;
}

.day-label {
  color: var(--rl-text-dim, #9a9a9a);
  font-weight: 600;
  padding-right: 8px;
  white-space: nowrap;
  font-size: 0.75rem;
}

.heatmap-cell {
  width: 22px;
  height: 18px;
  border-radius: 2px;
  cursor: default;
  transition: opacity 0.15s;
}

.heatmap-cell:hover {
  outline: 1px solid var(--rl-accent, #e94560);
}

.empty {
  color: var(--rl-text-dim, #9a9a9a);
  font-style: italic;
  font-size: 0.85rem;
}
</style>
