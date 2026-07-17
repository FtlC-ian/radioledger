<template>
  <q-card flat bordered class="q-mb-md">
    <q-card-section class="q-gutter-md row items-end">
      <q-input
        class="col-12 col-md"
        dense
        outlined
        debounce="350"
        label="Callsign"
        :model-value="localFilters.callsign"
        @update:model-value="onCallsignInput"
      />

      <q-select
        class="col-6 col-md"
        dense
        outlined
        clearable
        emit-value
        map-options
        label="Band"
        :options="bandOptions"
        :model-value="localFilters.band"
        @update:model-value="onFieldUpdate('band', $event as string | undefined)"
      />

      <q-select
        class="col-6 col-md"
        dense
        outlined
        clearable
        emit-value
        map-options
        label="Mode"
        :options="modeOptions"
        :model-value="localFilters.mode"
        @update:model-value="onFieldUpdate('mode', $event as string | undefined)"
      />

      <q-input
        class="col-6 col-md"
        dense
        outlined
        label="From (UTC)"
        type="date"
        :model-value="localFilters.dateFrom"
        @update:model-value="onFieldUpdate('dateFrom', $event as string | undefined)"
      />

      <q-input
        class="col-6 col-md"
        dense
        outlined
        label="To (UTC)"
        type="date"
        :model-value="localFilters.dateTo"
        @update:model-value="onFieldUpdate('dateTo', $event as string | undefined)"
      />

      <div class="col-12 col-md-auto row q-gutter-sm">
        <q-btn color="primary" label="Search" @click="emitApply" />
        <q-btn flat color="secondary" label="Reset" @click="resetFilters" />
      </div>
    </q-card-section>
  </q-card>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import type { QsoSearchFilters } from 'src/types/qso'

interface Props {
  modelValue: QsoSearchFilters
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: QsoSearchFilters): void
  (e: 'apply'): void
}>()

const bandOptions = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m', '2m', '70cm'].map(
  (band) => ({ label: band, value: band }),
)

const modeOptions = ['SSB', 'CW', 'FM', 'AM', 'FT8', 'FT4', 'RTTY', 'PSK31'].map((mode) => ({
  label: mode,
  value: mode,
}))

const localFilters = reactive<QsoSearchFilters>({ ...props.modelValue })

watch(
  () => props.modelValue,
  (value) => {
    Object.assign(localFilters, value)
  },
)

function emitUpdate() {
  emit('update:modelValue', { ...localFilters })
}

function emitApply() {
  emitUpdate()
  emit('apply')
}

function onCallsignInput(value: string | number | null) {
  localFilters.callsign = typeof value === 'string' ? value.toUpperCase() : ''
  emitApply()
}

function onFieldUpdate(field: keyof QsoSearchFilters, value: string | undefined) {
  localFilters[field] = value || undefined
  emitUpdate()
}

function resetFilters() {
  localFilters.callsign = undefined
  localFilters.band = undefined
  localFilters.mode = undefined
  localFilters.dateFrom = undefined
  localFilters.dateTo = undefined
  emitApply()
}
</script>
