<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { Chart, LineController, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Filler } from 'chart.js'
import type { CountriesOverTimeEntry } from '../composables/useStats'

Chart.register(LineController, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Filler)

const props = defineProps<{ data: CountriesOverTimeEntry[] }>()
const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart | null = null

function renderChart() {
  if (!canvasRef.value || !props.data.length) return
  if (chart) chart.destroy()
  chart = new Chart(canvasRef.value, {
    type: 'line',
    data: {
      labels: props.data.map((d) => d.period),
      datasets: [{
        label: 'Unique Countries',
        data: props.data.map((d) => d.unique_countries),
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

onMounted(renderChart)
watch(() => props.data, renderChart)
</script>

<template>
  <div class="chart-card">
    <h3>Cumulative Countries</h3>
    <canvas ref="canvasRef" />
    <p v-if="!data.length" class="empty">No data</p>
  </div>
</template>

<style scoped>
.chart-card {
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

.empty {
  color: var(--rl-text-dim, #9a9a9a);
  font-style: italic;
  font-size: 0.85rem;
}
</style>
