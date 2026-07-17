<template>
  <q-card flat bordered>
    <q-card-section>
      <div class="text-h6 q-mb-sm">ADIF Import</div>
      <div class="text-body2 text-grey-6 q-mb-md">
        Drop a .adi/.adif file to import contacts. Maximum file size: 50MB.
      </div>

      <q-file
        outlined
        use-chips
        clearable
        accept=".adi,.adif,text/plain"
        v-model="file"
        label="Drop ADIF file or click to choose"
      >
        <template #prepend>
          <q-icon name="upload_file" />
        </template>
      </q-file>

      <!-- File size display -->
      <div v-if="file" class="text-caption text-grey-6 q-mt-sm">
        <q-icon name="insert_drive_file" size="xs" class="q-mr-xs" />
        {{ file.name }} — {{ humanSize(file.size) }}
      </div>

      <!-- File size warning -->
      <q-banner v-if="file && isFileTooLarge" class="bg-warning text-white q-mt-md" dense rounded>
        <template #avatar>
          <q-icon name="warning" color="white" />
        </template>
        File exceeds 50MB limit. Upload will fail. Please reduce file size or contact support.
      </q-banner>

      <q-linear-progress
        v-if="loading || progress > 0"
        class="q-mt-md"
        :value="Math.min(1, Math.max(0, progress))"
        color="primary"
        size="10px"
        rounded
      />

      <!-- Live import stats -->
      <div v-if="stats" class="row q-col-gutter-md q-mt-md">
        <div class="col-6 col-md-3">
          <div class="text-caption text-grey-6">Processed</div>
          <div class="text-subtitle2">{{ stats.processed }} / {{ stats.total }}</div>
        </div>
        <div class="col-6 col-md-3">
          <div class="text-caption text-grey-6">Imported</div>
          <div class="text-subtitle2 text-positive">{{ stats.imported }}</div>
        </div>
        <div class="col-6 col-md-3">
          <div class="text-caption text-grey-6">Duplicates</div>
          <div class="text-subtitle2 text-warning">{{ stats.duplicates }}</div>
        </div>
        <div class="col-6 col-md-3">
          <div class="text-caption text-grey-6">Errors</div>
          <div class="text-subtitle2 text-negative">{{ stats.errors }}</div>
        </div>
      </div>

      <div class="q-mt-md row q-gutter-sm">
        <q-btn
          color="primary"
          icon="upload"
          label="Upload"
          :disable="!file || loading || isFileTooLarge"
          :loading="loading"
          @click="onUpload"
        />
        <q-btn flat color="secondary" label="Clear" :disable="loading" @click="clearForm" />
      </div>
    </q-card-section>

    <template v-if="result">
      <q-separator />
      <q-card-section>
        <div class="text-subtitle1 q-mb-sm">
          <q-icon
            :name="result.status === 'failed' ? 'error' : 'check_circle'"
            :color="result.status === 'failed' ? 'negative' : 'positive'"
            class="q-mr-xs"
          />
          Import {{ result.status === 'failed' ? 'Failed' : 'Complete' }}
        </div>

        <div class="row q-col-gutter-md q-mb-sm">
          <div class="col-6 col-md-3">
            <q-card flat bordered class="text-center q-pa-sm">
              <div class="text-h5 text-positive">{{ result.imported ?? 0 }}</div>
              <div class="text-caption text-grey-6">Imported</div>
            </q-card>
          </div>
          <div class="col-6 col-md-3">
            <q-card flat bordered class="text-center q-pa-sm">
              <div class="text-h5 text-warning">{{ result.duplicates ?? result.duplicate ?? 0 }}</div>
              <div class="text-caption text-grey-6">Duplicates</div>
            </q-card>
          </div>
          <div class="col-6 col-md-3">
            <q-card flat bordered class="text-center q-pa-sm">
              <div class="text-h5 text-negative">{{ result.errors ?? 0 }}</div>
              <div class="text-caption text-grey-6">Errors</div>
            </q-card>
          </div>
        </div>

        <div v-if="result.message" class="text-caption text-grey-6 q-mb-md">{{ result.message }}</div>

        <q-btn
          v-if="result.status !== 'failed'"
          color="primary"
          icon="table_rows"
          label="View Logbook"
          to="/logbook"
          flat
        />
      </q-card-section>
    </template>
  </q-card>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const MAX_FILE_SIZE_MB = 50
const MAX_FILE_SIZE_BYTES = MAX_FILE_SIZE_MB * 1024 * 1024

interface ImportLiveStats {
  processed: number
  total: number
  imported: number
  duplicates: number
  errors: number
}

interface Props {
  loading: boolean
  progress: number
  result: Record<string, unknown> | null
  stats: ImportLiveStats | null
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'upload', file: File): void
}>()

const file = ref<File | null>(null)

const isFileTooLarge = computed(() => {
  return file.value ? file.value.size > MAX_FILE_SIZE_BYTES : false
})

function humanSize(bytes: number) {
  if (bytes < 1024) {
    return `${bytes} B`
  }

  const kb = bytes / 1024
  if (kb < 1024) {
    return `${kb.toFixed(1)} KB`
  }

  return `${(kb / 1024).toFixed(1)} MB`
}

function clearForm() {
  file.value = null
}

function onUpload() {
  if (!file.value || props.loading) {
    return
  }

  emit('upload', file.value)
}
</script>
