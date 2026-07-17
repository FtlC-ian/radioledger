<template>
  <q-dialog v-model="isOpen" persistent>
    <q-card style="min-width: min(440px, 94vw)">
      <q-card-section>
        <div class="text-h6">Sync to LoTW</div>
        <div v-if="callsign" class="text-body2 text-grey-5">
          Sync {{ pendingCount }} QSO{{ pendingCount !== 1 ? 's' : '' }} to LoTW as {{ callsign }}
        </div>
      </q-card-section>

      <!-- Progress state -->
      <q-card-section v-if="phase === 'progress'">
        <div class="column items-center q-gutter-md q-py-md">
          <q-spinner color="primary" size="48px" />
          <div class="text-body1">{{ progressLabel }}</div>
          <q-linear-progress indeterminate color="primary" rounded style="width: 100%; height: 6px" />
        </div>
      </q-card-section>

      <!-- Success state -->
      <q-card-section v-if="phase === 'success'">
        <div class="column items-center q-gutter-sm q-py-md">
          <q-icon name="check_circle" color="positive" size="52px" />
          <div class="text-body1 text-positive text-weight-medium">
            Successfully synced {{ syncedCount }} QSO{{ syncedCount !== 1 ? 's' : '' }} to LoTW!
          </div>
        </div>
      </q-card-section>

      <!-- Failure state -->
      <q-card-section v-if="phase === 'failed'">
        <div class="column items-center q-gutter-sm q-py-md">
          <q-icon name="error_outline" color="negative" size="52px" />
          <div class="text-body1 text-negative text-weight-medium text-center">{{ errorMessage }}</div>
        </div>
      </q-card-section>

      <q-card-actions align="right">
        <!-- Progress phase — no buttons -->

        <!-- Success phase -->
        <template v-if="phase === 'success'">
          <q-btn color="primary" label="Done" @click="close" />
        </template>

        <!-- Failure phase -->
        <template v-if="phase === 'failed'">
          <q-btn flat label="Cancel" @click="close" />
          <q-btn color="primary" outline label="Retry" icon="refresh" @click="startSync" />
        </template>
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SyncJob } from 'src/services/lotwApi'
import { LOTW_USE_MOCK, mockTriggerSync, mockGetSyncStatus } from 'src/composables/useLotwMock'
import * as lotwApi from 'src/services/lotwApi'

const props = defineProps<{
  modelValue: boolean
  callsign?: string
  pendingCount?: number
  qsoIds?: number[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', val: boolean): void
  (e: 'synced', count: number): void
}>()

type Phase = 'progress' | 'success' | 'failed'

const isOpen = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const phase = ref<Phase>('progress')
const progressLabel = ref('Signing QSOs...')
const syncedCount = ref(0)
const errorMessage = ref('')

// Auto-start sync whenever the dialog opens.
watch(isOpen, (open) => {
  if (open) {
    phase.value = 'progress'
    progressLabel.value = 'Signing QSOs...'
    errorMessage.value = ''
    syncedCount.value = 0
    void startSync()
  }
})

function close() {
  emit('update:modelValue', false)
}

function mapSyncError(err: unknown): string {
  let msg: string
  if (err instanceof Error) {
    msg = err.message
  } else if (typeof err === 'string') {
    msg = err
  } else if (err && typeof err === 'object' && 'message' in err) {
    msg = String((err as any).message)
  } else {
    msg = String(err)
  }
  const lc = msg.toLowerCase()
  if (lc.includes('no cert') || lc.includes('no certificate')) {
    return 'No certificate found. Please upload your LoTW certificate in LoTW Settings first.'
  }
  if (lc.includes('cert') && lc.includes('expired')) {
    return 'Your LoTW certificate has expired. Renew it at https://lotw.arrl.org/lotw/password'
  }
  if (lc.includes('no lotw vault password') || lc.includes('vault password')) {
    return 'LoTW signing key not found. Please re-import your certificate in LoTW Settings.'
  }
  if (lc.includes('arrl') || lc.includes('upload') || lc.includes('reject')) {
    return 'LoTW rejected the upload. This sometimes happens after a server outage — try again in a few minutes.'
  }
  return msg || 'Something went wrong. Please try again.'
}

async function pollUntilDone(jobId: number): Promise<SyncJob> {
  const maxAttempts = 30
  for (let i = 0; i < maxAttempts; i++) {
    await new Promise((r) => setTimeout(r, 2000))
    const job = LOTW_USE_MOCK
      ? await mockGetSyncStatus(jobId)
      : await lotwApi.getSyncStatus(jobId)

    if (job.status === 'signing') progressLabel.value = 'Signing QSOs...'
    if (job.status === 'uploading') progressLabel.value = 'Uploading to ARRL...'
    if (job.status === 'completed' || job.status === 'failed') return job
  }
  throw new Error('Sync timed out. Check back in the Sync Dashboard.')
}

async function startSync() {
  phase.value = 'progress'
  progressLabel.value = 'Signing QSOs...'

  try {
    const job = LOTW_USE_MOCK
      ? await mockTriggerSync(props.qsoIds)
      : await lotwApi.triggerSync(props.qsoIds)

    const finalJob = await pollUntilDone(job.id)

    if (finalJob.status === 'completed') {
      syncedCount.value = finalJob.qso_count
      phase.value = 'success'
      emit('synced', finalJob.qso_count)
    } else {
      throw new Error(finalJob.error || 'Sync failed')
    }
  } catch (e) {
    errorMessage.value = mapSyncError(e)
    phase.value = 'failed'
  }
}
</script>
