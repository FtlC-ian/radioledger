<template>
  <div class="cert-upload-flow">
    <!-- Step indicator -->
    <div class="row q-gutter-sm q-mb-lg items-center">
      <template v-for="(label, idx) in stepLabels" :key="idx">
        <div
          class="step-dot row items-center justify-center text-caption text-weight-medium"
          :class="{
            'step-dot--active': step === idx + 1,
            'step-dot--done': step > idx + 1,
            'step-dot--future': step < idx + 1,
          }"
        >
          <q-icon v-if="step > idx + 1" name="check" size="12px" />
          <span v-else>{{ idx + 1 }}</span>
        </div>
        <div class="text-caption" :class="step === idx + 1 ? 'text-primary text-weight-medium' : 'text-grey-5'">
          {{ label }}
        </div>
        <div v-if="idx < stepLabels.length - 1" class="step-connector" />
      </template>
    </div>

    <!-- No vault password step — it's auto-generated on import -->

    <!-- Step 1 — File selection -->
    <div v-if="step === 1">
      <div class="row items-center q-gutter-xs q-mb-sm">
        <div class="text-subtitle2">Select your certificate file</div>
        <InfoTooltip
          text="This is the .p12 file you exported from TQSL. Usually found in your TQSL folder. If you haven't exported it yet, open TQSL → Callsign Certificate → Save Callsign Certificate (.p12)"
        />
      </div>

      <!-- Drop zone -->
      <div
        class="drop-zone q-pa-lg text-center rounded-borders"
        :class="{ 'drop-zone--active': dragging, 'drop-zone--error': fileError }"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <template v-if="!selectedFile">
          <q-icon name="upload_file" size="40px" color="grey-5" class="q-mb-sm" />
          <div class="text-body2 text-grey-5">Drag your .p12 file here, or</div>
          <q-btn
            flat
            color="primary"
            label="Browse files"
            class="q-mt-sm"
            @click="triggerFilePicker"
          />
          <input
            ref="fileInputRef"
            type="file"
            accept=".p12,.pfx"
            style="display: none"
            @change="onFileInputChange"
          />
        </template>

        <template v-else>
          <div class="row items-center justify-center q-gutter-sm">
            <q-icon name="check_circle" size="24px" color="positive" />
            <div>
              <div class="text-body2 text-weight-medium">{{ selectedFile.name }}</div>
              <div class="text-caption text-grey-5">{{ formatFileSize(selectedFile.size) }}</div>
            </div>
            <q-btn flat dense icon="close" size="sm" color="grey-6" @click="clearFile" />
          </div>
        </template>
      </div>

      <div v-if="fileError" class="text-negative text-caption q-mt-sm">
        <q-icon name="error" size="14px" /> {{ fileError }}
      </div>

      <div class="row justify-end q-mt-md">
        <q-btn color="primary" label="Next" :disable="!selectedFile || !!fileError" @click="step = 2" />
      </div>
    </div>

    <!-- Step 2 — Certificate password -->
    <div v-if="step === 2">
      <div class="row items-center q-gutter-xs q-mb-sm">
        <div class="text-subtitle2">Certificate password</div>
        <InfoTooltip
          text="This is the password you set when you exported the certificate from TQSL. If you didn't set a password, leave this blank."
        />
      </div>

      <q-input
        v-model="certPassword"
        outlined
        dense
        label="Certificate password (optional)"
        :type="showCertPass ? 'text' : 'password'"
        autocomplete="off"
        hint="Not the same as your LoTW website login password"
        class="q-mb-md"
      >
        <template #append>
          <q-icon
            :name="showCertPass ? 'visibility_off' : 'visibility'"
            class="cursor-pointer"
            @click="showCertPass = !showCertPass"
          />
        </template>
      </q-input>

      <div class="row justify-between q-mt-md">
        <q-btn flat label="Back" @click="step = 1" />
        <q-btn color="primary" label="Next" @click="proceedToConfirm" />
      </div>
    </div>

    <!-- Step 3 — Confirm & Import -->
    <div v-if="step === 3">
      <div class="text-subtitle2 q-mb-md">Review and import</div>

      <!-- Error state -->
      <q-banner v-if="uploadError" rounded class="bg-negative text-white q-mb-md">
        <template #avatar><q-icon name="error" /></template>
        {{ uploadError }}
        <template #action>
          <q-btn flat color="white" label="Try again" @click="uploadError = ''; step = 1" />
        </template>
      </q-banner>

      <!-- Preview from mock / preflight -->
      <div v-if="previewCert" class="q-mb-md">
        <q-list bordered rounded>
          <q-item>
            <q-item-section avatar><q-icon name="person" color="primary" /></q-item-section>
            <q-item-section>
              <q-item-label caption>Callsign</q-item-label>
              <q-item-label class="text-weight-medium text-body1">{{ previewCert.callsign }}</q-item-label>
            </q-item-section>
          </q-item>
          <q-item>
            <q-item-section avatar><q-icon name="event" color="primary" /></q-item-section>
            <q-item-section>
              <q-item-label caption>Expires</q-item-label>
              <q-item-label>{{ formatDate(previewCert.cert_not_after) }}</q-item-label>
            </q-item-section>
          </q-item>
          <q-item v-if="previewCert.gridsquare">
            <q-item-section avatar><q-icon name="grid_on" color="primary" /></q-item-section>
            <q-item-section>
              <q-item-label caption>Grid square</q-item-label>
              <q-item-label>{{ previewCert.gridsquare }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </div>

      <div class="row justify-between q-mt-md">
        <q-btn flat label="Back" :disable="uploading" @click="step = 2" />
        <q-btn
          color="primary"
          icon="verified_user"
          label="Import Certificate"
          :loading="uploading"
          @click="doUpload"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import InfoTooltip from 'src/components/InfoTooltip.vue'
import type { CertInfo } from 'src/services/lotwApi'
import { LOTW_USE_MOCK, mockUploadCert } from 'src/composables/useLotwMock'
import * as lotwApi from 'src/services/lotwApi'

const emit = defineEmits<{
  (e: 'cert-imported', cert: CertInfo): void
}>()

const step = ref(1)
const stepLabels = ['Select file', 'Certificate password', 'Confirm']

const fileInputRef = ref<HTMLInputElement | null>(null)
const selectedFile = ref<File | null>(null)
const fileError = ref('')
const dragging = ref(false)

const certPassword = ref('')
const showCertPass = ref(false)

const previewCert = ref<CertInfo | null>(null)
const uploading = ref(false)
const uploadError = ref('')

function triggerFilePicker() {
  fileInputRef.value?.click()
}

function validateFile(file: File): string {
  const name = file.name.toLowerCase()
  if (!name.endsWith('.p12') && !name.endsWith('.pfx')) {
    return "This doesn't look like a certificate file. LoTW certificates are .p12 files, usually named something like YourCallsign_LoTW.p12"
  }
  if (file.size > 50 * 1024) {
    return "This file is too large to be a certificate. Make sure you're selecting your .p12 certificate file, not a log file."
  }
  return ''
}

function onFileInputChange(evt: Event) {
  const input = evt.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) setFile(file)
}

function onDrop(evt: DragEvent) {
  dragging.value = false
  const file = evt.dataTransfer?.files?.[0]
  if (file) setFile(file)
}

function setFile(file: File) {
  fileError.value = validateFile(file)
  selectedFile.value = file
}

function clearFile() {
  selectedFile.value = null
  fileError.value = ''
  if (fileInputRef.value) fileInputRef.value.value = ''
}

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleDateString()
}

function proceedToConfirm() {
  // In mock mode, set a preview cert without uploading yet
  if (LOTW_USE_MOCK) {
    previewCert.value = {
      callsign: 'W1AW',
      cert_not_after: new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString(),
      cert_not_before: new Date().toISOString(),
      expired: false,
      gridsquare: 'FN31',
    }
  }
  step.value = 3
}

function mapUploadError(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err)
  const lc = msg.toLowerCase()
  if (lc.includes('not a valid') || lc.includes('invalid p12') || lc.includes('parse')) {
    return "This file couldn't be read as a certificate. Make sure it's the .p12 file exported from TQSL, not a .tq8 log file or other document."
  }
  if (lc.includes('wrong_cert_password') || lc.includes('mac verify failure') || lc.includes('cert password')) {
    return "The certificate password is incorrect. This is the password you chose when exporting from TQSL — not your LoTW website login or your RadioLedger password."
  }
  if (lc.includes('not arrl') || lc.includes('not issued by arrl') || lc.includes('not an arrl')) {
    return "This certificate wasn't issued by ARRL's Logbook of the World. Make sure you're uploading your LoTW certificate, not a different type of security certificate."
  }
  if (lc.includes('expired')) {
    return `This certificate has expired. You'll need to request a renewed certificate from ARRL at https://lotw.arrl.org/lotw/password`
  }
  if (lc.includes('callsign') || lc.includes('extraction')) {
    return "We couldn't read the callsign from this certificate. The file may be corrupted. Try exporting a fresh copy from TQSL."
  }
  if (lc.includes('server') || lc.includes('500')) {
    return 'Something went wrong on our end. Please try again in a moment.'
  }
  return msg || 'Something went wrong. Please try again.'
}

async function doUpload() {
  if (!selectedFile.value) return
  uploading.value = true
  uploadError.value = ''
  try {
    let cert: CertInfo
    if (LOTW_USE_MOCK) {
      cert = await mockUploadCert(selectedFile.value, certPassword.value)
    } else {
      cert = await lotwApi.uploadCert(selectedFile.value, certPassword.value)
    }
    emit('cert-imported', cert)
    // Reset flow
    step.value = 1
    selectedFile.value = null
    certPassword.value = ''
    previewCert.value = null
  } catch (e) {
    uploadError.value = mapUploadError(e)
  } finally {
    uploading.value = false
  }
}
</script>

<style scoped>
.step-dot {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  font-size: 11px;
  flex-shrink: 0;
}
.step-dot--active {
  background: var(--q-primary);
  color: white;
}
.step-dot--done {
  background: var(--q-positive);
  color: white;
}
.step-dot--future {
  background: rgba(128, 128, 128, 0.25);
  color: var(--q-grey-6, #9e9e9e);
}
.step-connector {
  flex: 0 0 20px;
  height: 1px;
  background: rgba(128, 128, 128, 0.3);
}
.drop-zone {
  border: 2px dashed rgba(128, 128, 128, 0.35);
  min-height: 140px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  transition: border-color 0.2s, background 0.2s;
  cursor: default;
}
.drop-zone--active {
  border-color: var(--q-primary);
  background: rgba(var(--q-primary-rgb, 25, 118, 210), 0.06);
}
.drop-zone--error {
  border-color: var(--q-negative);
}
</style>
