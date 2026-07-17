<template>
  <div class="column q-gutter-md">
    <q-chip v-if="LOTW_USE_MOCK" color="warning" text-color="dark" icon="science" label="MOCK MODE" class="self-start" />

    <q-banner v-if="!lotwAvailable" rounded class="bg-grey-2 text-dark" icon="warning">
      {{ lotwUnavailableMessage }}
    </q-banner>

    <template v-else>
      <!-- Certificate Management -->
      <q-card flat bordered>
      <q-card-section>
        <div class="row items-center q-gutter-xs q-mb-md">
          <div class="text-subtitle1 text-weight-medium">Certificate</div>
          <InfoTooltip text="Your LoTW certificate (.p12 file) is what ARRL uses to verify your identity. It's the same file you use with TQSL." />
        </div>

        <template v-if="certInfo">
          <q-banner
            v-if="certExpiresSoon"
            rounded
            class="q-mb-md"
            :class="certInfo.expired ? 'bg-negative text-white' : 'bg-warning text-dark'"
            icon="warning"
          >
            <template v-if="certInfo.expired">
              Your certificate expired on {{ formatDate(certInfo.cert_not_after) }}. You'll need to renew it before you can sync QSOs.
            </template>
            <template v-else>
              Your certificate expires on {{ formatDate(certInfo.cert_not_after) }}. Renew soon to avoid interruption.
            </template>
            <template #action>
              <q-btn
                flat
                :color="certInfo.expired ? 'white' : 'dark'"
                label="Renew on ARRL"
                href="https://lotw.arrl.org/lotw/password"
                target="_blank"
                rel="noopener noreferrer"
              />
            </template>
          </q-banner>

          <div class="row q-col-gutter-md q-mb-md">
            <div class="col-12 col-sm-6 col-md-4">
              <div class="text-caption text-grey-5">Callsign</div>
              <div class="text-body1 text-weight-medium">{{ certInfo.callsign }}</div>
            </div>
            <div class="col-12 col-sm-6 col-md-4">
              <div class="text-caption text-grey-5">Expires</div>
              <div class="row items-center q-gutter-xs">
                <div class="text-body1">{{ formatDate(certInfo.cert_not_after) }}</div>
                <q-badge v-if="certInfo.expired" color="negative" label="Expired" />
                <q-badge v-else-if="certExpiresSoon" color="warning" text-color="dark" label="Expiring soon" />
                <q-badge v-else color="positive" label="Valid" />
              </div>
            </div>
            <div v-if="certInfo.gridsquare" class="col-12 col-sm-6 col-md-4">
              <div class="text-caption text-grey-5">Grid square</div>
              <div class="text-body1">{{ certInfo.gridsquare }}</div>
            </div>
            <div v-if="certInfo.dxcc" class="col-12 col-sm-6 col-md-4">
              <div class="text-caption text-grey-5">DXCC</div>
              <div class="text-body1">{{ certInfo.dxcc }}</div>
            </div>
          </div>

          <div class="row q-gutter-sm">
            <q-btn
              outline color="negative" icon="delete"
              label="Remove certificate"
              :loading="removingCert"
              @click="confirmRemoveCert"
            />
          </div>
        </template>

        <template v-else>
          <div class="text-body2 text-grey-5 q-mb-md">
            No certificate uploaded yet. Upload your LoTW certificate to start syncing QSOs.
          </div>
          <CertUploadFlow @cert-imported="onCertImported" />
        </template>
      </q-card-section>
    </q-card>

    <!-- Replace certificate (shown only when a cert already exists) -->
    <q-card v-if="certInfo && showReplaceUpload" flat bordered>
      <q-card-section>
        <div class="row items-center justify-between q-mb-md">
          <div class="text-subtitle2 text-weight-medium">Replace certificate</div>
          <q-btn flat dense icon="close" @click="showReplaceUpload = false" />
        </div>
        <CertUploadFlow @cert-imported="onCertImported" />
      </q-card-section>
    </q-card>

    <q-btn
      v-if="certInfo && !showReplaceUpload"
      flat dense color="primary" icon="upload_file"
      label="Replace certificate"
      class="self-start"
      @click="showReplaceUpload = true"
    />

    <!-- Sync Preferences -->
      <q-card flat bordered>
        <q-card-section>
          <div class="text-subtitle1 text-weight-medium q-mb-md">Sync preferences</div>
          <div class="row items-center q-gutter-sm">
            <q-toggle
              v-model="autoSyncPrompt"
              color="primary"
              label="Notify me of unsynced QSOs when I log in"
              :disable="savingSettings"
              @update:model-value="saveSettings"
            />
            <InfoTooltip text="When enabled, you'll see a reminder when you have QSOs that haven't been sent to LoTW yet." />
          </div>
        </q-card-section>
      </q-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useQuasar } from 'quasar'
import { getApiErrorMessage } from 'src/api/client'
import InfoTooltip from 'src/components/InfoTooltip.vue'
import CertUploadFlow from 'src/components/CertUploadFlow.vue'
import type { CertInfo } from 'src/services/lotwApi'
import {
  LOTW_USE_MOCK,
  mockCertInfo,
  mockDeleteCert,
  mockSettings,
  mockUpdateSettings,
} from 'src/composables/useLotwMock'
import * as lotwApi from 'src/services/lotwApi'

const $q = useQuasar()

const certInfo = ref<CertInfo | null>(null)
const autoSyncPrompt = ref(true)
const savingSettings = ref(false)
const removingCert = ref(false)
const showReplaceUpload = ref(false)
const lotwAvailable = ref(true)
const lotwUnavailableMessage = ref('LoTW settings are temporarily unavailable.')

const certExpiresSoon = computed(() => {
  if (!certInfo.value) return false
  const expiryMs = new Date(certInfo.value.cert_not_after).getTime()
  return expiryMs - Date.now() < 90 * 24 * 60 * 60 * 1000
})

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString()
}

function mapCertError(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e)
  return msg || 'Could not remove certificate. Please try again.'
}

function onCertImported(cert: CertInfo) {
  certInfo.value = cert
  lotwAvailable.value = true
  showReplaceUpload.value = false
  $q.notify({ type: 'positive', message: `Certificate for ${cert.callsign} imported successfully.` })
  // Refresh to ensure reactive state is fully updated
  void loadData()
}

async function loadData() {
  try {
    if (LOTW_USE_MOCK) {
      certInfo.value = mockCertInfo
      autoSyncPrompt.value = mockSettings.auto_sync_prompt
    } else {
      const [cert, settings] = await Promise.all([lotwApi.getCertInfo(), lotwApi.getSettings()])
      certInfo.value = cert
      autoSyncPrompt.value = settings?.auto_sync_prompt ?? true
    }

    lotwAvailable.value = true
    lotwUnavailableMessage.value = 'LoTW settings are temporarily unavailable.'
  } catch (error) {
    certInfo.value = null
    autoSyncPrompt.value = true
    showReplaceUpload.value = false
    lotwAvailable.value = false
    lotwUnavailableMessage.value = getApiErrorMessage(error, 'LoTW settings are temporarily unavailable.')
    $q.notify({ type: 'warning', message: 'LoTW settings are temporarily unavailable right now.' })
  }
}

async function saveSettings() {
  savingSettings.value = true
  try {
    if (LOTW_USE_MOCK) {
      await mockUpdateSettings({ auto_sync_prompt: autoSyncPrompt.value })
    } else {
      await lotwApi.updateSettings({ auto_sync_prompt: autoSyncPrompt.value })
    }
  } catch (error) {
    $q.notify({ type: 'negative', message: getApiErrorMessage(error, 'Could not save preference') })
  } finally {
    savingSettings.value = false
  }
}

function confirmRemoveCert() {
  $q.dialog({
    title: 'Remove certificate?',
    message: 'This will permanently delete your LoTW certificate from RadioLedger. You will not be able to sync QSOs to LoTW until you re-upload it.',
    cancel: true,
    ok: { color: 'negative', label: 'Remove' },
  }).onOk(async () => {
    removingCert.value = true
    try {
      if (LOTW_USE_MOCK) {
        await mockDeleteCert()
      } else {
        await lotwApi.deleteCert()
      }
      certInfo.value = null
      $q.notify({ type: 'positive', message: 'Certificate removed' })
    } catch (e) {
      $q.notify({ type: 'negative', message: mapCertError(e) })
    } finally {
      removingCert.value = false
    }
  })
}

onMounted(loadData)
</script>
