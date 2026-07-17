<template>
  <q-dialog v-model="modelValue" @update:model-value="$emit('update:modelValue', $event)">
    <q-card style="min-width: min(960px, 95vw)">
      <q-card-section>
        <div class="text-h6">Resolve Conflict — {{ conflict?.callsign }}</div>
      </q-card-section>

      <q-card-section v-if="conflict">
        <q-list separator>
          <q-item v-for="field in Object.keys(conflict.field_conflicts || {})" :key="field">
            <q-item-section>
              <q-item-label class="text-weight-medium">{{ field }}</q-item-label>
              <q-item-label caption>
                {{ conflict.service_a }}: {{ stringifyValue(conflict.field_conflicts[field]?.[conflict.service_a]) }}
                · {{ conflict.service_b }}: {{ stringifyValue(conflict.field_conflicts[field]?.[conflict.service_b]) }}
              </q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-option-group
                v-model="localResolution[field]"
                inline
                :options="[
                  { label: conflict.service_a.toUpperCase(), value: conflict.service_a },
                  { label: conflict.service_b.toUpperCase(), value: conflict.service_b },
                ]"
              />
            </q-item-section>
          </q-item>
        </q-list>
      </q-card-section>

      <q-card-actions align="right">
        <q-btn flat label="Cancel" v-close-popup />
        <q-btn color="primary" label="Resolve" :loading="loading" @click="submit" />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
/**
 * SyncConflictDialog — lets the user pick which service's value wins for
 * each conflicting field on a QSO that two services disagree on.
 *
 * Props:
 * - modelValue: v-model open state
 * - conflict: the SyncConflict record to resolve (null when nothing selected)
 * - loading: true while the parent is submitting the resolution to the API
 *
 * Emits:
 * - update:modelValue: standard v-model close
 * - submit: when the user clicks Resolve; payload is the per-field resolution map
 */
import { computed, ref, watch } from 'vue'
import { type SyncConflict } from 'src/types/sync'
import { buildDefaultResolution, stringifyValue } from 'src/utils/syncHelpers'

const props = defineProps<{
  modelValue: boolean
  conflict: SyncConflict | null
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'submit', resolution: Record<string, string>): void
}>()

const modelValue = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

/**
 * Per-field resolution choices: field → winning service key.
 * Pre-populated with service_a as the default choice whenever the dialog opens
 * (matching the original page behavior). We watch both `conflict` (for prop
 * identity changes) AND `modelValue` becoming true (for re-open of the same
 * conflict after cancel), so selections are always reset to defaults on open.
 */
const localResolution = ref<Record<string, string>>({})

function initResolution(c: SyncConflict | null) {
  if (!c) return
  localResolution.value = buildDefaultResolution(c.field_conflicts || {}, c.service_a)
}

// Reset when a different conflict is loaded.
watch(() => props.conflict, initResolution, { immediate: true })

// Reset when the dialog re-opens for the same conflict (cancel → reopen).
watch(
  () => props.modelValue,
  (open) => {
    if (open) initResolution(props.conflict)
  },
)

function submit() {
  emit('submit', { ...localResolution.value })
}

// Expose localResolution so component tests can read/write selections
// without reimplementing the watcher contract.
defineExpose({ localResolution })
</script>
