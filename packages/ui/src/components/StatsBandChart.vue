<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { Chart, BarController, CategoryScale, LinearScale, BarElement, Tooltip } from 'chart.js'
import type { BandEntry } from '../composables/useStats'

Chart.register(BarController, CategoryScale, LinearScale, BarElement, Tooltip)

const props = defineProps<{
  data: BandEntry[]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart | null = null

function renderChart() {
  if (!canvasRef.value || !props.data.length) return
  if (chart) chart.destroy()
  chart = new Chart(canvasRef.value, {
    type: 'bar',
    data: {
      labels: props.data.map((d) => d.band),
      datasets: [{
        label: 'QSOs',
        data: props.data.map((d) => d.count),
        backgroundColor: '#4fc3f7',
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
    <h3>QSOs by Band</h3>
    <canvas ref="canvasRef" />
    <p v-if="!data.length" class="empty">No band data</p>
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
