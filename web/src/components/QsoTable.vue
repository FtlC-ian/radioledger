<template>
  <q-table
    flat
    bordered
    :rows="rows"
    :columns="columns"
    :loading="loading"
    row-key="uuid"
    :dense="$q.screen.lt.md"
    v-model:pagination="localPagination"
    @row-click="onRowClick"
  >
    <template #body-cell-datetime_on="props">
      <q-td :props="props">
        {{ formatDateTime(props.row.datetime_on || '') }}
      </q-td>
    </template>

    <template #bottom>
      <div class="full-width row items-center justify-between q-gutter-sm">
        <div class="text-caption text-grey-6">{{ rows.length }} QSOs loaded</div>

        <q-btn
          v-if="hasMore"
          color="primary"
          label="Load more"
          :loading="loading"
          :disable="loading"
          @click="$emit('load-more')"
        />
      </div>
    </template>
  </q-table>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { QTableColumn, QTableProps } from 'quasar'
import type { Qso } from 'src/types/qso'

interface Props {
  rows: Qso[]
  loading: boolean
  hasMore: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'row-click', row: Qso): void
  (e: 'load-more'): void
}>()

const columns: QTableColumn<Qso>[] = [
  { name: 'datetime_on', label: 'Date/Time', field: 'datetime_on', align: 'left', sortable: true },
  { name: 'callsign', label: 'Callsign', field: 'callsign', align: 'left', sortable: true },
  { name: 'band', label: 'Band', field: 'band', align: 'left', sortable: true },
  { name: 'mode', label: 'Mode', field: 'mode', align: 'left', sortable: true },
  {
    name: 'frequency',
    label: 'Frequency',
    field: (row) => {
      if (!row.frequency_hz) return ''
      const mhz = row.frequency_hz / 1_000_000
      // Always show kHz (3 decimals), only show sub-kHz digits if non-zero
      const full = mhz.toFixed(6)
      const sub = full.slice(-3)
      return sub === '000' ? mhz.toFixed(3) : full.replace(/0+$/, '')
    },
    align: 'right',
    sortable: true,
  },
  { name: 'rst_sent', label: 'RST Sent', field: 'rst_sent', align: 'center', sortable: true },
  { name: 'rst_rcvd', label: 'RST Rcvd', field: 'rst_rcvd', align: 'center', sortable: true },
  { name: 'gridsquare', label: 'Grid', field: 'gridsquare', align: 'left', sortable: true },
  { name: 'country', label: 'Country', field: 'country', align: 'left', sortable: true },
]

const localPagination = ref<QTableProps['pagination']>({
  sortBy: 'datetime_on',
  descending: true,
  rowsPerPage: 25,
})

watch(
  () => props.rows,
  () => {
    if (!props.rows.length) {
      localPagination.value = {
        ...localPagination.value,
        page: 1,
      }
    }
  },
)

function formatDateTime(value: string) {
  if (!value) {
    return ''
  }

  return new Date(value).toLocaleString()
}

function onRowClick(_: Event, row: Qso) {
  emit('row-click', row)
}
</script>
