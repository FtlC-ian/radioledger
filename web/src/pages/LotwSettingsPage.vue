<template>
  <q-page class="q-pa-md lotw-settings-page">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">LoTW Integration</div>
        <div class="text-body2 text-grey-5">Manage your ARRL Logbook of the World certificate and sync settings.</div>
      </div>
      <q-chip v-if="LOTW_USE_MOCK" color="warning" text-color="dark" icon="science" label="MOCK MODE" />
    </div>

    <div class="column q-gutter-md">

      <!-- Certificate Management -->
      <q-card flat bordered>
        <q-card-section>
          <div class="row items-center q-gutter-xs q-mb-md">
            <div class="text-subtitle1 text-weight-medium">Certificate</div>
            <InfoTooltip text="Your LoTW certificate (.p12 file) is what ARRL uses to verify your identity. It's the same file you use with TQSL." />
          </div>

          <!-- Cert loaded state -->
          <template v-if="certInfo">
            <!-- Expiry warning -->
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

            <!-- Cert details -->
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
              <div v-if="certInfo.grid" class="col-12 col-sm-6 col-md-4">
                <div class="text-caption text-grey-5">Grid square</div>
                <div class="text-body1">{{ certInfo.grid }}</div>
              </div>
              <div v-if="certInfo.dxcc" class="col-12 col-sm-6 col-md-4">
                <div class="text-caption text-grey-5">DXCC</div>
                <div class="text-body1">{{ certInfo.dxcc }}</div>
              </div>
            </div>

            <!-- Cert actions -->
            <div class="row q-gutter-sm">
              <q-btn
                outline
                color="primary"
                icon="lock_reset"
                label="Change signing password"
                @click="showRotateDialog = true"
              />
              <q-btn
                outline
                color="negative"
                icon="delete"
                label="Remove certificate"
                :loading="removingCert"
                @click="confirmRemoveCert"
              />
            </div>
          </template>

          <!-- No cert — upload flow -->
          <template v-else>
            <div class="text-body2 text-grey-5 q-mb-md">
              No certificate uploaded yet. Upload your LoTW certificate to start syncing QSOs.
            </div>
            <CertUploadFlow @cert-imported="onCertImported" />
          </template>
        </q-card-section>
      </q-card>

      <!-- If cert is loaded but user wants to replace it -->
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
        flat
        dense
        color="primary"
        icon="upload_file"
        label="Replace certificate"
        class="self-start"
        @click="showReplaceUpload = true"
      />

      <!-- Sync Preferences -->
      <q-card flat bordered>
        <q-card-section>
          <div class="text-subtitle1 text-weight-medium q-mb-md">Sync Preferences</div>

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

      <!-- Sync History -->
      <q-card flat bordered>
        <q-card-section>
          <div class="text-subtitle1 text-weight-medium q-mb-md">Sync History</div>

          <q-table
            flat
            :rows="syncHistory"
            :columns="historyColumns"
            row-key="id"
            :loading="loadingHistory"
            :rows-per-page-options="[10, 25]"
            no-data-label="No sync jobs yet"
          >
            <template #body-cell-status="props">
              <q-td :props="props">
                <q-badge
                  :color="statusColor(props.row.status)"
                  :text-color="props.row.status === 'failed' ? 'white' : undefined"
                  :label="props.row.status"
                />
              </q-td>
            </template>

            <template #body-cell-created_at="props">
              <q-td :props="props">{{ formatDateTime(props.row.created_at) }}</q-td>
            </template>

            <template #body-cell-duration="props">
              <q-td :props="props">{{ formatDuration(props.row) }}</q-td>
            </template>

            <template #body-cell-expand="props">
              <q-td :props="props" class="text-right">
                <q-btn flat dense icon="expand_more" size="sm" @click="expandRow(props.row)" />
              </q-td>
            </template>
          </q-table>

          <q-dialog v-model="showHistoryDetail">
            <q-card style="min-width: min(480px, 94vw)">
              <q-card-section>
                <div class="text-h6">Sync Job #{{ selectedJob?.id }}</div>
              </q-card-section>
              <q-card-section v-if="selectedJob">
                <q-list dense>
                  <q-item>
                    <q-item-section>
                      <q-item-label caption>Status</q-item-label>
                      <q-item-label>
                        <q-badge :color="statusColor(selectedJob.status)" :label="selectedJob.status" />
                      </q-item-label>
                    </q-item-section>
                  </q-item>
                  <q-item>
                    <q-item-section>
                      <q-item-label caption>QSOs</q-item-label>
                      <q-item-label>{{ selectedJob.qso_count }}</q-item-label>
                    </q-item-section>
                  </q-item>
                  <q-item>
                    <q-item-section>
                      <q-item-label caption>Started</q-item-label>
                      <q-item-label>{{ formatDateTime(selectedJob.created_at) }}</q-item-label>
                    </q-item-section>
                  </q-item>
                  <q-item v-if="selectedJob.completed_at">
                    <q-item-section>
                      <q-item-label caption>Completed</q-item-label>
                      <q-item-label>{{ formatDateTime(selectedJob.completed_at) }}</q-item-label>
                    </q-item-section>
                  </q-item>
                  <q-item v-if="selectedJob.result">
                    <q-item-section>
                      <q-item-label caption>Result</q-item-label>
                      <q-item-label>{{ selectedJob.result }}</q-item-label>
                    </q-item-section>
                  </q-item>
                  <q-item v-if="selectedJob.error">
                    <q-item-section>
                      <q-item-label caption class="text-negative">Error</q-item-label>
                      <q-item-label class="text-negative">{{ mapSyncHistoryError(selectedJob.error) }}</q-item-label>
                    </q-item-section>
                  </q-item>
                </q-list>
              </q-card-section>
              <q-card-actions align="right">
                <q-btn flat label="Close" v-close-popup />
              </q-card-actions>
            </q-card>
          </q-dialog>
        </q-card-section>
      </q-card>
    </div>

    <!-- Change vault password dialog -->
    <q-dialog v-model="showRotateDialog">
      <q-card style="min-width: min(420px, 94vw)">
        <q-card-section>
          <div class="text-h6">Change signing password</div>
          <div class="text-caption text-grey-5 q-mt-xs">Enter your current password and choose a new one.</div>
        </q-card-section>
        <q-card-section class="q-gutter-md">
          <q-input
            v-model="rotateForm.oldPassword"
            outlined
            dense
            label="Current signing password"
            :type="showOldPass ? 'text' : 'password'"
            autocomplete="current-password"
          >
            <template #append>
              <q-icon
                :name="showOldPass ? 'visibility_off' : 'visibility'"
                class="cursor-pointer"
                @click="showOldPass = !showOldPass"
              />
            </template>
          </q-input>
          <q-input
            v-model="rotateForm.newPassword"
            outlined
            dense
            label="New signing password"
            :type="showNewPass ? 'text' : 'password'"
            autocomplete="new-password"
            hint="Minimum 8 characters"
          >
            <template #append>
              <q-icon
                :name="showNewPass ? 'visibility_off' : 'visibility'"
                class="cursor-pointer"
                @click="showNewPass = !showNewPass"
              />
            </template>
          </q-input>
          <q-input
            v-model="rotateForm.confirmPassword"
            outlined
            dense
            label="Confirm new password"
            :type="showNewPass ? 'text' : 'password'"
            :error="rotateForm.confirmPassword.length > 0 && rotateForm.newPassword !== rotateForm.confirmPassword"
            error-message="Passwords don't match"
            autocomplete="new-password"
          />
        </q-card-section>
        <q-card-actions align="right">
          <q-btn flat label="Cancel" v-close-popup />
          <q-btn
            color="primary"
            label="Update password"
            :loading="rotatingPassword"
            :disable="!rotateFormValid"
            @click="doRotatePassword"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useQuasar, type QTableColumn } from 'quasar'
import InfoTooltip from 'src/components/InfoTooltip.vue'
import CertUploadFlow from 'src/components/CertUploadFlow.vue'
import type { CertInfo, SyncJob } from 'src/services/lotwApi'
import {
  LOTW_USE_MOCK,
  mockCertInfo,
  mockDeleteCert,
  mockSettings,
  mockSyncHistory,
  mockUpdateSettings,
} from 'src/composables/useLotwMock'
import * as lotwApi from 'src/services/lotwApi'

const $q = useQuasar()

const certInfo = ref<CertInfo | null>(null)
const syncHistory = ref<SyncJob[]>([])
const autoSyncPrompt = ref(true)
const loadingHistory = ref(false)
const savingSettings = ref(false)
const removingCert = ref(false)
const showReplaceUpload = ref(false)
const showRotateDialog = ref(false)
const rotatingPassword = ref(false)
const showHistoryDetail = ref(false)
const selectedJob = ref<SyncJob | null>(null)
const showOldPass = ref(false)
const showNewPass = ref(false)

const rotateForm = ref({ oldPassword: '', newPassword: '', confirmPassword: '' })

const rotateFormValid = computed(
  () =>
    rotateForm.value.oldPassword.length >= 1 &&
    rotateForm.value.newPassword.length >= 8 &&
    rotateForm.value.newPassword === rotateForm.value.confirmPassword,
)

const certExpiresSoon = computed(() => {
  if (!certInfo.value) return false
  const expiryMs = new Date(certInfo.value.cert_not_after).getTime()
  const nowMs = Date.now()
  const ninetyDays = 90 * 24 * 60 * 60 * 1000
  return expiryMs - nowMs < ninetyDays
})

const historyColumns: QTableColumn<SyncJob>[] = [
  { name: 'id', label: 'Job', field: 'id', align: 'left', sortable: true },
  { name: 'created_at', label: 'Date', field: 'created_at', align: 'left', sortable: true },
  { name: 'qso_count', label: 'QSOs', field: 'qso_count', align: 'right', sortable: true },
  { name: 'status', label: 'Status', field: 'status', align: 'left', sortable: true },
  { name: 'duration', label: 'Duration', field: () => '', align: 'left' },
  { name: 'expand', label: '', field: () => '', align: 'right' },
]

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString()
}

function formatDateTime(iso: string) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

function formatDuration(job: SyncJob) {
  if (!job.completed_at) return '—'
  const ms = new Date(job.completed_at).getTime() - new Date(job.created_at).getTime()
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function statusColor(status: SyncJob['status']) {
  switch (status) {
    case 'completed': return 'positive'
    case 'failed': return 'negative'
    case 'signing':
    case 'uploading':
    case 'pending': return 'warning'
    default: return 'grey-6'
  }
}

function mapCertError(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e)
  const lc = msg.toLowerCase()
  if (lc.includes('wrong_vault_password') || lc.includes('vault password') || lc.includes('decrypt')) {
    return 'Incorrect signing password. Please try again.'
  }
  return 'Could not remove certificate. Please try again.'
}

function mapSyncHistoryError(error: string): string {
  const lc = error.toLowerCase()
  if (lc.includes('wrong_vault_password') || lc.includes('vault password') || lc.includes('decrypt')) {
    return 'Sync failed: incorrect signing password.'
  }
  if (lc.includes('cert') && lc.includes('expired')) {
    return 'Sync failed: LoTW certificate has expired.'
  }
  if (lc.includes('no cert') || lc.includes('no certificate')) {
    return 'Sync failed: no certificate found.'
  }
  if (lc.includes('arrl') || lc.includes('upload') || lc.includes('reject')) {
    return 'LoTW rejected the upload.'
  }
  return error || 'Unknown error'
}

function mapRotateError(e: unknown): string {
  const msg = e instanceof Error ? e.message : String(e)
  const lc = msg.toLowerCase()
  if (
    lc.includes('wrong_vault_password') ||
    lc.includes('vault password') ||
    lc.includes('decrypt') ||
    lc.includes('old_vault_password') ||
    lc.includes('incorrect')
  ) {
    return 'Current signing password is incorrect. Please try again.'
  }
  return 'Could not update password. Please try again.'
}

function expandRow(job: SyncJob) {
  selectedJob.value = job
  showHistoryDetail.value = true
}

function onCertImported(cert: CertInfo) {
  certInfo.value = cert
  showReplaceUpload.value = false
  $q.notify({ type: 'positive', message: `Certificate for ${cert.callsign} imported successfully.` })
}

async function loadData() {
  loadingHistory.value = true
  try {
    if (LOTW_USE_MOCK) {
      certInfo.value = mockCertInfo
      autoSyncPrompt.value = mockSettings.auto_sync_prompt
      syncHistory.value = mockSyncHistory
    } else {
      const [cert, settings, history] = await Promise.all([
        lotwApi.getCertInfo(),
        lotwApi.getSettings(),
        lotwApi.getSyncHistory(1, 25),
      ])
      certInfo.value = cert
      autoSyncPrompt.value = settings.auto_sync_prompt
      syncHistory.value = history
    }
  } catch {
    $q.notify({ type: 'warning', message: 'Could not load LoTW settings' })
  } finally {
    loadingHistory.value = false
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
  } catch {
    $q.notify({ type: 'negative', message: 'Could not save preference' })
  } finally {
    savingSettings.value = false
  }
}

function confirmRemoveCert() {
  $q.dialog({
    title: 'Remove certificate?',
    message:
      'Enter your signing password to confirm. Your certificate will be permanently deleted from RadioLedger.',
    prompt: {
      model: '',
      type: 'password',
      label: 'Signing password',
      attrs: { autocomplete: 'off' },
    },
    cancel: true,
    ok: { color: 'negative', label: 'Remove' },
  }).onOk(async (password: string) => {
    if (!password) {
      $q.notify({ type: 'negative', message: 'Signing password is required to remove the certificate.' })
      return
    }
    removingCert.value = true
    try {
      if (LOTW_USE_MOCK) {
        await mockDeleteCert(password)
      } else {
        await lotwApi.deleteCert(password)
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

async function doRotatePassword() {
  rotatingPassword.value = true
  try {
    if (!LOTW_USE_MOCK) {
      await lotwApi.rotatePassword(rotateForm.value.oldPassword, rotateForm.value.newPassword)
    } else {
      await new Promise((r) => setTimeout(r, 800))
    }
    showRotateDialog.value = false
    rotateForm.value = { oldPassword: '', newPassword: '', confirmPassword: '' }
    $q.notify({ type: 'positive', message: 'Signing password updated' })
  } catch (e) {
    $q.notify({ type: 'negative', message: mapRotateError(e) })
  } finally {
    rotatingPassword.value = false
  }
}

onMounted(loadData)
</script>

<style scoped>
.lotw-settings-page {
  max-width: 960px;
  margin: 0 auto;
}
</style>
