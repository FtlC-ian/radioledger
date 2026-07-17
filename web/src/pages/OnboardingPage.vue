<template>
  <q-layout view="hHh lpr fFf">
    <q-page-container>
      <q-page class="flex flex-center q-pa-md" style="min-height: 100vh">
        <q-card flat bordered style="width: min(720px, 96vw)">
          <q-card-section class="q-pa-xl">
            <div class="text-overline text-primary">Station setup</div>
            <div class="text-h5 text-weight-medium q-mt-sm">Set up the logbook you’ll use on the air</div>
            <div class="text-body2 text-grey-6 q-mt-sm">
              Start with your callsign and default station details. You can import ADIF, connect WSJT-X, and tune sync settings later.
            </div>
          </q-card-section>

          <q-separator />

          <q-card-section class="q-pa-lg q-pa-md-xl">
            <q-stepper v-model="step" flat animated alternative-labels :header-nav="false" color="primary">
              <q-step :name="1" title="Callsign" icon="radio" :done="step > 1">
                <div class="text-body2 text-grey-7 q-mb-md">
                  Use the callsign you normally operate under. Portable and club callsigns can be added later.
                </div>

                <q-input
                  v-model="callsign"
                  outlined
                  autofocus
                  label="Callsign *"
                  input-class="text-uppercase"
                  maxlength="10"
                  :loading="callsignChecking"
                  :error="Boolean(callsignError)"
                  :error-message="callsignError"
                  hint="3–10 characters, letters and numbers, like K1ABC or N0CALL"
                  @blur="void validateCallsign()"
                >
                  <template #append>
                    <q-icon v-if="callsignAvailable && !callsignChecking" name="check_circle" color="positive" />
                  </template>
                </q-input>

                <q-stepper-navigation class="q-mt-lg">
                  <q-btn color="primary" label="Continue" :loading="callsignChecking" @click="void goToGridStep()" />
                </q-stepper-navigation>
              </q-step>

              <q-step :name="2" title="Grid Square" icon="place" :done="step > 2">
                <div class="text-body2 text-grey-7 q-mb-md">
                  Add your home Maidenhead grid square if you know it. This helps with awards, maps, and exported ADIF.
                </div>

                <q-input
                  v-model="gridSquare"
                  outlined
                  label="Grid Square"
                  input-class="text-uppercase"
                  maxlength="6"
                  :error="Boolean(gridError)"
                  :error-message="gridError"
                  hint="Examples: EM35 or EM35FX"
                  @blur="validateGridSquare"
                />

                <div v-if="callsignLookupHint" class="text-caption text-primary q-mt-sm">
                  {{ callsignLookupHint }}
                </div>

                <q-stepper-navigation class="q-mt-lg">
                  <q-btn flat color="primary" label="Back" class="q-mr-sm" @click="step = 1" />
                  <q-btn color="primary" label="Continue" @click="goToLogbookStep" />
                </q-stepper-navigation>
              </q-step>

              <q-step :name="3" title="Default Logbook" icon="menu_book">
                <div class="text-body2 text-grey-7 q-mb-md">
                  Name your first logbook. Most operators start with their primary callsign, then add portable, contest, or club logs later.
                </div>

                <q-input
                  v-model="logbookName"
                  outlined
                  label="Default Logbook Name"
                  maxlength="80"
                  hint="You can rename this later in Logbooks settings."
                />

                <q-stepper-navigation class="q-mt-lg">
                  <q-btn flat color="primary" label="Back" class="q-mr-sm" @click="step = 2" />
                  <q-btn color="primary" label="Complete Setup" :loading="saving" @click="void completeSetup()" />
                </q-stepper-navigation>
              </q-step>
            </q-stepper>
          </q-card-section>
        </q-card>
      </q-page>
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import { apiGet, apiPut } from 'src/api/client'
import { useAuthStore } from 'src/stores/auth'

interface CallsignAvailabilityResponse {
  callsign: string
  available: boolean
  reason?: string
}

interface CallsignGridLookupResponse {
  callsign?: string | null
  grid_square?: string | null
  source?: string | null
  city?: string | null
  state?: string | null
}

interface LogbookResponse {
  uuid: string
  name: string
  callsign?: string | null
  description?: string | null
  is_default: boolean
}

const router = useRouter()
const $q = useQuasar()
const auth = useAuthStore()

const step = ref(1)
const saving = ref(false)
const callsignChecking = ref(false)
const callsignAvailable = ref(false)
const callsignLookupHint = ref('')

const callsign = ref(String(auth.userProfile?.callsign || ''))
const gridSquare = ref(String(auth.userProfile?.grid_square || ''))
const logbookName = ref('My Log')
const defaultLogbook = ref<LogbookResponse | null>(null)

const callsignError = ref('')
const gridError = ref('')

const normalizedCallsign = computed(() => callsign.value.trim().toUpperCase())
const normalizedGridSquare = computed(() => gridSquare.value.trim().toUpperCase())

function looksLikeCallsign(value: string) {
  return /^[A-Z0-9]{1,3}[0-9][A-Z0-9]{1,6}$/.test(value) && value.length >= 3 && value.length <= 10
}

function looksLikeGridSquare(value: string) {
  return value === '' || /^[A-R]{2}[0-9]{2}([A-X]{2})?$/.test(value)
}

async function loadDefaultLogbook() {
  try {
    const response = await apiGet<LogbookResponse>('/v1/logbooks/default')
    if (response.success && response.data) {
      defaultLogbook.value = response.data
      logbookName.value = response.data.name || 'My Log'
    }
  } catch {
    // Optional step — keep fallback defaults.
  }
}

async function maybeLookupGridSquare(call: string) {
  if (!call || gridSquare.value.trim()) {
    return
  }

  try {
    const response = await apiGet<CallsignGridLookupResponse>(`/v1/callsign/${encodeURIComponent(call)}/grid`)
    const lookupGrid = response.success ? String(response.data?.grid_square || '').trim().toUpperCase() : ''
    if (!lookupGrid) {
      return
    }

    gridSquare.value = lookupGrid

    const city = String(response.data?.city || '').trim()
    const state = String(response.data?.state || '').trim()
    if (city && state) {
      callsignLookupHint.value = `Based on your FCC address in ${city}, ${state}`
      return
    }

    callsignLookupHint.value = `We found ${lookupGrid} from the FCC callsign record and filled it in for you.`
  } catch {
    // Best-effort only.
  }
}

async function validateCallsign() {
  callsign.value = normalizedCallsign.value
  callsignError.value = ''
  callsignAvailable.value = false
  callsignLookupHint.value = ''

  if (!callsign.value) {
    callsignError.value = 'Callsign is required'
    return false
  }

  if (!looksLikeCallsign(callsign.value)) {
    callsignError.value = 'Enter a valid amateur radio callsign'
    return false
  }

  callsignChecking.value = true
  try {
    const response = await apiGet<CallsignAvailabilityResponse>(
      `/v1/auth/callsign-availability?callsign=${encodeURIComponent(callsign.value)}`,
    )

    if (!response.success || !response.data?.available) {
      callsignError.value = response.data?.reason || response.error || response.message || 'That callsign is unavailable'
      return false
    }

    callsignAvailable.value = true
    await maybeLookupGridSquare(callsign.value)
    return true
  } catch (error: unknown) {
    const err = error as { error?: string; message?: string }
    callsignError.value = err?.error || err?.message || 'Unable to validate callsign right now'
    return false
  } finally {
    callsignChecking.value = false
  }
}

function validateGridSquare() {
  gridSquare.value = normalizedGridSquare.value
  gridError.value = ''

  if (!looksLikeGridSquare(gridSquare.value)) {
    gridError.value = 'Use a 4 or 6 character Maidenhead grid, like EM35 or EM35FX'
    return false
  }

  return true
}

async function goToGridStep() {
  if (await validateCallsign()) {
    step.value = 2
  }
}

function goToLogbookStep() {
  if (validateGridSquare()) {
    step.value = 3
  }
}

async function updateDefaultLogbook(call: string) {
  const trimmedName = logbookName.value.trim() || defaultLogbook.value?.name || 'My Log'

  if (!defaultLogbook.value?.uuid) {
    return
  }

  await apiPut(`/v1/logbooks/${defaultLogbook.value.uuid}`, {
    name: trimmedName,
    callsign: call,
    description: defaultLogbook.value.description || undefined,
    is_default: true,
  })
}

async function completeSetup() {
  if (!(await validateCallsign())) {
    step.value = 1
    return
  }
  if (!validateGridSquare()) {
    step.value = 2
    return
  }

  saving.value = true
  try {
    const response = await auth.completeProfile(normalizedCallsign.value, normalizedGridSquare.value || null)
    if (!response.success) {
      throw new Error(response.error || response.message || 'Unable to finish setup')
    }

    try {
      await updateDefaultLogbook(normalizedCallsign.value)
    } catch (error: unknown) {
      const err = error as { error?: string; message?: string }
      $q.notify({
        type: 'warning',
        message: err?.error || err?.message || 'Profile saved, but we could not update your default logbook yet.',
      })
    }

    await auth.fetchMe()
    $q.notify({ type: 'positive', message: 'Setup complete — welcome aboard.' })
    await router.replace('/dashboard')
  } catch (error: unknown) {
    const err = error as { error?: string; message?: string }
    $q.notify({ type: 'negative', message: err?.error || err?.message || 'Unable to complete setup' })
  } finally {
    saving.value = false
  }
}

watch(callsign, () => {
  callsignAvailable.value = false
  callsignLookupHint.value = ''
  if (callsignError.value) {
    callsignError.value = ''
  }
})

watch(gridSquare, () => {
  if (gridError.value) {
    gridError.value = ''
  }
})

onMounted(async () => {
  if (!auth.isAuthenticated) {
    await router.replace('/login')
    return
  }
  if (!auth.needsOnboarding) {
    await router.replace('/dashboard')
    return
  }

  await loadDefaultLogbook()
})
</script>
