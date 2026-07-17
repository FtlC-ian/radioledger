<script setup lang="ts">
export interface LogEntry {
  ts: string
  message: string
  level?: 'info' | 'warn' | 'error'
}

defineProps<{
  entries: LogEntry[]
  maxLines?: number
}>()
</script>

<template>
  <div class="activity-log">
    <h3>Activity Log</h3>
    <div class="log-output">
      <div
        v-for="(entry, i) in entries"
        :key="i"
        :class="['log-line', entry.level || 'info']"
      >
        <span class="ts">{{ entry.ts }}</span>
        {{ entry.message }}
      </div>
      <div v-if="!entries.length" class="empty">No recent activity</div>
    </div>
  </div>
</template>

<style scoped>
.activity-log {
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

.log-output {
  background: var(--rl-bg, #1a1a2e);
  border-radius: var(--rl-radius, 8px);
  padding: 12px;
  font-family: 'SF Mono', 'Fira Code', 'Courier New', monospace;
  font-size: 12px;
  min-height: 100px;
  max-height: 250px;
  overflow-y: auto;
  border: 1px solid var(--rl-surface-2, #0f3460);
}

.log-line {
  padding: 2px 0;
  line-height: 1.6;
  color: var(--rl-text-dim, #9a9a9a);
}

.log-line.warn { color: var(--rl-warning, #ff9800); }
.log-line.error { color: var(--rl-error, #f44336); }

.ts {
  color: rgba(255, 255, 255, 0.3);
  margin-right: 8px;
}

.empty {
  color: var(--rl-text-dim, #9a9a9a);
  font-style: italic;
}
</style>
