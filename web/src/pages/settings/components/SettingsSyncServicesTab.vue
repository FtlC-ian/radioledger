<template>
  <div class="column q-gutter-lg sync-services-tab">
    <div>
      <div class="row items-center q-gutter-sm q-mb-md">
        <q-icon name="verified_user" color="primary" size="22px" />
        <div class="text-h6">LoTW — Logbook of the World</div>
        <q-chip dense color="blue-grey-7" text-color="white" label="ARRL" />
      </div>
      <LotwCertSection />
    </div>

    <q-separator />

    <div>
      <div class="text-h6 q-mb-xs">API Credentials</div>
      <div class="text-caption text-grey-5 q-mb-md">
        Connect external services for QSO syncing and callbook lookups.
      </div>

      <div class="column q-gutter-md">
        <q-card
          v-for="service in credentialServices"
          :key="service.key"
          :ref="(el) => setCardRef(el, service.key)"
          flat
          bordered
          :class="highlightService === service.key ? 'service-card-highlighted' : ''"
        >
          <q-card-section>
            <div class="row q-col-gutter-md items-start">
              <div class="col-12 col-md-5">
                <div class="row items-center q-gutter-sm q-mb-xs">
                  <q-icon :name="service.icon" color="primary" size="20px" />
                  <div class="text-subtitle2 text-weight-medium">{{ service.label }}</div>
                  <q-badge
                    :color="getServiceStatusMeta(service.key).color"
                    :text-color="getServiceStatusMeta(service.key).textColor"
                    class="q-gutter-xs"
                  >
                    <q-icon :name="getServiceStatusMeta(service.key).icon" size="14px" />
                    <span>{{ getServiceStatusMeta(service.key).label }}</span>
                  </q-badge>
                </div>
                <div class="text-caption text-grey-6">{{ service.description }}</div>
                <div v-if="credentials[service.key]?.last_verified_at" class="text-caption q-mt-xs text-positive">
                  Last verified: {{ formatDateTime(credentials[service.key]?.last_verified_at || '') }}
                </div>
                <div
                  v-else-if="getCredentialStatus(service.key) === 'unverified'"
                  class="text-caption q-mt-xs text-warning"
                >
                  Credential saved but verification has not passed yet.
                </div>
                <div
                  v-if="credentials[service.key]?.verification_error && getCredentialStatus(service.key) === 'unverified'"
                  class="text-caption q-mt-xs text-warning"
                >
                  {{ credentials[service.key]?.verification_error }}
                </div>
              </div>

              <div class="col-12 col-md-7">
                <div v-if="service.key === 'qrz'" class="q-gutter-sm">
                  <q-btn-toggle
                    v-model="qrzCredentialMode"
                    spread no-caps unelevated
                    toggle-color="primary" color="grey-3" text-color="dark"
                    :options="qrzCredentialModeOptions"
                  />
                  <div v-if="qrzCredentialMode === 'api_key'" class="row q-col-gutter-sm items-center">
                    <q-input
                      v-model="credentialDraft[service.key].apiKey"
                      class="col"
                      type="password"
                      outlined
                      dense
                      label="QRZ Logbook API key"
                      hint="Found in QRZ Logbook settings. Required for logbook import and upload features."
                      autocomplete="off"
                    />
                  </div>
                  <div v-else class="row q-col-gutter-sm">
                    <q-input
                      v-model="credentialDraft[service.key].username"
                      class="col-12 col-md-6"
                      outlined
                      dense
                      label="QRZ Username"
                      autocomplete="username"
                    />
                    <q-input
                      v-model="credentialDraft[service.key].password"
                      class="col-12 col-md-6"
                      type="password"
                      outlined
                      dense
                      label="QRZ Password"
                      autocomplete="current-password"
                    />
                  </div>
                  <div class="text-caption text-grey-6">
                    <template v-if="qrzCredentialMode === 'api_key'">
                      RadioLedger will wrap raw API keys into the backend JSON format automatically.
                    </template>
                    <template v-else>
                      RadioLedger will store these as the backend's expected username:password format automatically.
                    </template>
                  </div>
                </div>

                <div v-else-if="service.credentialType === 'api_key'" class="row q-col-gutter-sm items-center">
                  <q-input
                    v-model="credentialDraft[service.key].apiKey"
                    class="col"
                    type="password"
                    outlined
                    dense
                    :label="service.inputLabel"
                    autocomplete="off"
                  />
                </div>

                <div v-else class="row q-col-gutter-sm">
                  <q-input
                    v-model="credentialDraft[service.key].username"
                    class="col-12 col-md-6"
                    outlined
                    dense
                    label="eQSL Username / Callsign"
                    autocomplete="off"
                  />
                  <q-input
                    v-model="credentialDraft[service.key].password"
                    class="col-12 col-md-6"
                    type="password"
                    outlined
                    dense
                    label="eQSL Password"
                    autocomplete="off"
                  />
                </div>

                <div class="row q-gutter-sm q-mt-sm">
                  <q-btn
                    color="primary"
                    icon="save"
                    label="Save"
                    :loading="credentialSaving[service.key]"
                    :disable="!canSaveCredential(service)"
                    @click="saveCredential(service)"
                  />
                  <q-btn
                    v-if="isServiceConnected(service.key)"
                    color="secondary"
                    outline
                    icon="verified"
                    label="Re-verify"
                    :loading="credentialVerifying[service.key]"
                    @click="reVerifyCredential(service)"
                  />
                  <q-btn
                    v-if="isServiceConnected(service.key)"
                    color="negative"
                    outline
                    icon="delete"
                    label="Remove"
                    :loading="credentialRemoving[service.key]"
                    @click="removeCredential(service)"
                  />
                </div>
              </div>
            </div>
          </q-card-section>
        </q-card>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import LotwCertSection from 'src/components/LotwCertSection.vue'
import { useCredentials } from 'src/composables/useCredentials'

const props = withDefaults(defineProps<{ highlightService?: string }>(), { highlightService: '' })

const {
  canSaveCredential,
  credentialDraft,
  credentialRemoving,
  credentialSaving,
  credentials,
  credentialServices,
  credentialVerifying,
  getCredentialStatus,
  getServiceStatusMeta,
  isServiceConnected,
  loadCredentials,
  qrzCredentialMode,
  qrzCredentialModeOptions,
  reVerifyCredential,
  removeCredential,
  saveCredential,
} = useCredentials()

// Card element refs keyed by service key, used to scroll to the highlighted card.
const cardRefs = ref<Record<string, Element | null>>({})

function setCardRef(el: unknown, key: string) {
  // el can be a Vue component instance or a raw DOM element
  const dom = el && typeof el === 'object' && '$el' in el
    ? (el as { $el: Element }).$el
    : (el as Element | null)
  cardRefs.value[key] = dom ?? null
}

async function scrollToHighlighted(service: string) {
  if (!service) return
  await nextTick()
  const el = cardRefs.value[service]
  if (el) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

onMounted(async () => {
  await loadCredentials()
  void scrollToHighlighted(props.highlightService)
})

watch(() => props.highlightService, (service) => {
  void scrollToHighlighted(service)
})
</script>

<style scoped>
.sync-services-tab {
  max-width: 960px;
}

/* Card highlighted via ?service= query param when navigating from Sync dashboard */
.service-card-highlighted {
  border-color: var(--q-primary) !important;
  box-shadow: 0 0 0 2px var(--q-primary);
  transition: box-shadow 0.2s ease;
}
</style>
