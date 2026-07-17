<template>
  <q-page class="q-pa-md">
    <div class="text-h5 q-mb-md">Log QSO</div>

    <q-card flat bordered>
      <q-card-section>
        <!-- Changing the key forces QsoForm to remount (resets its internal state) -->
        <QsoForm
          :key="formKey"
          submit-label="Log QSO"
          :loading="submitting"
          @submit="createQso"
        />
      </q-card-section>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useQuasar } from 'quasar'
import QsoForm from 'src/components/QsoForm.vue'
import { useLogbookStore } from 'src/stores/logbook'
import type { QsoPayload } from 'src/types/qso'

const $q = useQuasar()
const logbook = useLogbookStore()

const submitting = ref(false)
const formKey = ref(0)

async function createQso(payload: QsoPayload) {
  submitting.value = true

  try {
    const response = await logbook.createQso(payload)

    if (response.success) {
      $q.notify({
        type: 'positive',
        message: `QSO with ${payload.callsign} logged on ${payload.band} ${payload.mode}`,
        timeout: 3000,
      })
      // Increment key to remount the form — resets date/time to now, clears callsign, etc.
      formKey.value++
    } else {
      $q.notify({ type: 'negative', message: response.error || 'Unable to log QSO' })
    }
  } catch {
    $q.notify({ type: 'negative', message: 'Unable to log QSO' })
  } finally {
    submitting.value = false
  }
}
</script>
