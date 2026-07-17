<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { Chart, DoughnutController, ArcElement, Tooltip, Legend } from 'chart.js'
import type { ModeEntry } from '../composables/useStats'

Chart.register(DoughnutController, ArcElement, Tooltip, Legend)

const PALETTE = ['#ef5350', '#ab47bc', '#42a5f5', '#66bb6a', '#ffca28', '#26c6da', '#ff7043']

const props = defineProps<{ data: ModeEntry[] }>()
const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart | null = null

function renderChart() {
  if (!canvasRef.value || !props.data.length) return
  if (chart) chart.destroy()
  chart = new Chart(canvasRef.value, {
    type: 'doughnut',
    data: {
      labels: props.data.map((d) => d.mode),
      datasets: [{
        data: props.data.map((d) => d.count),
        backgroundColor: PALETTE,
      }],
    },
    options: {
      responsive: true,
      plugins: { legend: { position: 'right', labels: { color: '#eaeaea', font: { size: 11 } } } },
    },
  })
}

onMounted(renderChart)
watch(() => props.data, renderChart)
</script>

<template>
  <div class="chart-card">
    <h3>QSOs by Mode</h3>
    <canvas ref="canvasRef" />
    <p v-if="!data.length" class="empty">No mode data</p>
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
