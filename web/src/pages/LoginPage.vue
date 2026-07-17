<template>
  <q-layout view="hHh lpr fFf">
    <q-page-container>
      <q-page class="flex flex-center column q-pa-md" style="min-height: 100vh">
        <q-card data-testid="login-card" flat bordered style="width: min(440px, 95vw)">
          <q-card-section class="text-center q-pt-lg q-pb-sm">
            <img :src="logoTransparentUrl" alt="RadioLedger" style="max-width: 280px; height: auto;" class="q-mb-sm" />
            <div class="text-caption text-grey-5 q-mt-xs">UTC-first logging · ADIF export · self-host friendly</div>
          </q-card-section>

          <template v-if="auth.isOidc">
            <q-separator />

            <q-card-section class="q-pa-lg">
              <div class="text-body2 text-grey-5 text-center q-mb-lg">
                Sign in to your station log.
              </div>
              <q-input
                outlined
                dense
                label="Invite Code"
                v-model="oidcInviteCode"
                input-class="text-uppercase"
                maxlength="8"
                hint="Only needed when your deployment requires an invite code"
                class="q-mb-md"
              />
              <div class="column q-gutter-sm">
                <q-btn
                  color="primary"
                  label="Sign in with RadioLedger"
                  class="full-width"
                  :loading="loading"
                  icon="login"
                  @click="onOidcLogin"
                />
                <q-btn
                  outline
                  color="primary"
                  label="Create RadioLedger account"
                  class="full-width"
                  :loading="loading"
                  icon="person_add"
                  @click="onOidcRegister"
                />
              </div>
            </q-card-section>
          </template>

          <template v-else>
            <q-tabs v-model="tab" dense align="justify" class="text-grey" active-color="primary" indicator-color="primary">
              <q-tab name="login" label="Sign In" />
              <q-tab name="register" label="Register" />
            </q-tabs>

            <q-separator />

            <q-tab-panels v-model="tab" animated keep-alive>
              <!-- ── Sign In ─────────────────────────────────── -->
              <q-tab-panel name="login" class="q-pa-lg">
                <q-form @submit.prevent="onLogin" class="q-gutter-md">
                  <q-input
                    outlined
                    dense
                    label="Email"
                    type="email"
                    v-model="loginEmail"
                    autocomplete="email"
                    :rules="[(v) => Boolean(v) || 'Email is required']"
                  />
                  <q-input
                    outlined
                    dense
                    label="Password"
                    :type="loginShowPassword ? 'text' : 'password'"
                    v-model="loginPassword"
                    autocomplete="current-password"
                    :rules="[(v) => Boolean(v) || 'Password is required']"
                  >
                    <template #append>
                      <q-icon
                        :name="loginShowPassword ? 'visibility_off' : 'visibility'"
                        class="cursor-pointer"
                        @click="loginShowPassword = !loginShowPassword"
                      />
                    </template>
                  </q-input>
                  <q-btn
                    type="submit"
                    color="primary"
                    label="Sign In"
                    class="full-width"
                    :loading="loading"
                    icon="login"
                  />
                </q-form>
              </q-tab-panel>

              <!-- ── Register ───────────────────────────────── -->
              <q-tab-panel name="register" class="q-pa-lg">
                <q-form @submit.prevent="onRegister" class="q-gutter-md">
                  <q-input
                    outlined
                    dense
                    label="Email *"
                    type="email"
                    v-model="regEmail"
                    autocomplete="email"
                    :rules="[(v) => Boolean(v) || 'Email is required']"
                  />
                  <q-input
                    outlined
                    dense
                    label="Password *"
                    :type="regShowPassword ? 'text' : 'password'"
                    v-model="regPassword"
                    autocomplete="new-password"
                    hint="At least 8 characters"
                    :rules="[(v) => v.length >= 8 || 'Password must be at least 8 characters']"
                  >
                    <template #append>
                      <q-icon
                        :name="regShowPassword ? 'visibility_off' : 'visibility'"
                        class="cursor-pointer"
                        @click="regShowPassword = !regShowPassword"
                      />
                    </template>
                  </q-input>
                  <q-input
                    outlined
                    dense
                    label="Confirm Password *"
                    :type="regShowPassword ? 'text' : 'password'"
                    v-model="regConfirmPassword"
                    autocomplete="new-password"
                    :rules="[(v) => v === regPassword || 'Passwords do not match']"
                  />
                  <q-btn
                    type="submit"
                    color="primary"
                    label="Create Account"
                    class="full-width"
                    :loading="loading"
                    icon="person_add"
                  />
                </q-form>
              </q-tab-panel>
            </q-tab-panels>
          </template>
        </q-card>

        <div
          data-testid="login-legal-links"
          class="login-legal-links row justify-center items-center q-mt-md text-caption text-grey-6"
          style="width: min(440px, 95vw)"
        >
          <router-link to="/legal/terms" class="text-grey-6">Self-hosting notice</router-link>
          <span class="q-mx-sm">·</span>
          <router-link to="/legal/privacy" class="text-grey-6">Deployment privacy notice</router-link>
        </div>
      </q-page>
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useQuasar } from 'quasar'
import { useAuthStore } from 'src/stores/auth'
import logoTransparentUrl from 'src/assets/branding/logo-navbar.png'

const router = useRouter()
const $q = useQuasar()
const auth = useAuthStore()

const tab = ref<'login' | 'register'>('login')
const loading = ref(false)

const loginEmail = ref('')
const loginPassword = ref('')
const loginShowPassword = ref(false)
const oidcInviteCode = ref('')

const regEmail = ref('')
const regPassword = ref('')
const regConfirmPassword = ref('')
const regShowPassword = ref(false)

async function onOidcLogin() {
  loading.value = true
  try {
    await auth.loginWithOidc()
  } catch (e: unknown) {
    const err = e as { error?: string; message?: string }
    $q.notify({ type: 'negative', message: err?.error || err?.message || 'Sign-in failed' })
    loading.value = false
  }
}

async function onOidcRegister() {
  loading.value = true
  try {
    await auth.loginWithOidc({
      prompt: 'create',
      inviteCode: oidcInviteCode.value.trim().toUpperCase(),
    })
  } catch (e: unknown) {
    const err = e as { error?: string; message?: string }
    $q.notify({ type: 'negative', message: err?.error || err?.message || 'Registration failed' })
    loading.value = false
  }
}

async function onLogin() {
  if (!loginEmail.value.trim() || !loginPassword.value) return
  loading.value = true
  try {
    const resp = await auth.login(loginEmail.value.trim().toLowerCase(), loginPassword.value)
    if (resp.success) {
      $q.notify({ type: 'positive', message: 'Welcome back!' })
      await router.push(auth.needsOnboarding ? '/onboarding' : '/dashboard')
    } else {
      $q.notify({ type: 'negative', message: resp.error || resp.message || 'Login failed' })
    }
  } catch (e: unknown) {
    const err = e as { error?: string; message?: string }
    $q.notify({ type: 'negative', message: err?.error || err?.message || 'Login failed' })
  } finally {
    loading.value = false
  }
}

async function onRegister() {
  if (!regEmail.value.trim()) return
  if (regPassword.value.length < 8) {
    $q.notify({ type: 'negative', message: 'Password must be at least 8 characters' })
    return
  }
  if (regPassword.value !== regConfirmPassword.value) {
    $q.notify({ type: 'negative', message: 'Passwords do not match' })
    return
  }
  loading.value = true
  try {
    const resp = await auth.register(
      regEmail.value.trim().toLowerCase(),
      regPassword.value,
    )
    if (resp.success) {
      $q.notify({ type: 'positive', message: 'Account created — welcome to RadioLedger!' })
      await router.push(auth.needsOnboarding ? '/onboarding' : '/dashboard')
    } else {
      $q.notify({ type: 'negative', message: resp.error || resp.message || 'Registration failed' })
    }
  } catch (e: unknown) {
    const err = e as { error?: string; message?: string }
    $q.notify({ type: 'negative', message: err?.error || err?.message || 'Registration failed' })
  } finally {
    loading.value = false
  }
}
</script>
