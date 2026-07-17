<template>
  <q-dialog v-model="modelValue" @update:model-value="$emit('update:modelValue', $event)">
    <q-card style="min-width: min(900px, 95vw)">
      <q-card-section>
        <div class="text-h6">Sync History — {{ qso?.callsign }}</div>
      </q-card-section>

      <q-card-section>
        <q-list separator>
          <q-item v-for="item in historyForQso" :key="item.id">
            <q-item-section>
              <q-item-label>{{ serviceLabel(item.service) }} · {{ item.status }}</q-item-label>
              <q-item-label caption>{{ formatDateTime(item.datetime_on) }}</q-item-label>
              <q-item-label v-if="item.error" caption class="text-negative">{{ item.error }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-badge v-if="item.retry_count > 0" color="warning" :label="`retry ${item.retry_count}`" />
            </q-item-section>
          </q-item>
        </q-list>
      </q-card-section>

      <q-card-actions align="right">
        <q-btn flat label="Close" v-close-popup />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
/**
 * SyncHistoryDialog — shows per-service sync event history for a single QSO.
 *
 * Props:
 * - modelValue: v-model open state
 * - qso: the QSO row the user clicked (null when nothing selected)
 * - history: full history list from the page (filtered internally by qso_uuid)
 */
import { computed } from 'vue'
import { type SyncStatusRow, type SyncHistoryItem } from 'src/types/sync'
import { serviceLabel, formatDateTime } from 'src/utils/syncHelpers'

const props = defineProps<{
  modelValue: boolean
  qso: SyncStatusRow | null
  history: SyncHistoryItem[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

// modelValue is handled via the q-dialog v-model binding above; Quasar emits
// update:model-value on close which we re-emit up.
const modelValue = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

/** Only the history records that belong to the currently selected QSO. */
const historyForQso = computed(() =>
  props.history.filter((item) => item.qso_uuid === props.qso?.qso_uuid),
)
</script>
