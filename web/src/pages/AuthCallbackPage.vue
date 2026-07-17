<template>
  <q-layout view="hHh lpr fFf">
    <q-page-container>
      <q-page class="flex flex-center q-pa-md">
        <q-card flat bordered style="width: min(440px, 95vw)">
          <q-card-section class="q-pa-xl text-center">
            <template v-if="loading">
              <q-spinner color="primary" size="40px" />
              <div class="text-h6 q-mt-md">Signing you in…</div>
              <div class="text-body2 text-grey-5 q-mt-sm">Finishing secure login with RadioLedger.</div>
            </template>

            <template v-else>
              <q-icon name="error_outline" color="negative" size="40px" />
              <div class="text-h6 q-mt-md">Sign-in failed</div>
              <div class="text-body2 text-grey-5 q-mt-sm">{{ errorMessage }}</div>
              <q-btn class="q-mt-lg" color="primary" label="Back to login" to="/login" />
            </template>
          </q-card-section>
        </q-card>
      </q-page>
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from 'src/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const loading = ref(true)
const errorMessage = ref('Unable to complete OIDC sign-in.')

onMounted(async () => {
  // In hash-mode routing, Zitadel redirects to /?code=xxx&state=yyy#/auth/callback
  // Vue Router only sees query params after the #, so route.query is empty.
  // Fall back to window.location.search for the real params.
  const params = new URLSearchParams(window.location.search)
  const code = (typeof route.query.code === 'string' ? route.query.code : '') || params.get('code') || ''
  const state = (typeof route.query.state === 'string' ? route.query.state : '') || params.get('state') || ''
  const error = (typeof route.query.error === 'string' ? route.query.error : '') || params.get('error') || ''
  const errorDescription =
    (typeof route.query.error_description === 'string' ? route.query.error_description : '') || params.get('error_description') || ''

  if (error) {
    loading.value = false
    errorMessage.value = errorDescription || error
    return
  }

  if (!code || !state) {
    loading.value = false
    errorMessage.value = 'Missing authorization code or state.'
    return
  }

  try {
    await auth.handleOidcCallback(code, state)
    await router.replace(auth.needsOnboarding ? '/onboarding' : '/dashboard')
  } catch (e: unknown) {
    const err = e as { error?: string; message?: string }
    loading.value = false
    errorMessage.value = err?.error || err?.message || 'Unable to complete OIDC sign-in.'
  }
})
</script>
