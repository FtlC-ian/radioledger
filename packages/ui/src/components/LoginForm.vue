<script setup lang="ts">
import { ref } from 'vue'

const emit = defineEmits<{
  login: [email: string]
  register: [email: string, callsign: string]
}>()

const mode = ref<'login' | 'register'>('login')
const email = ref('')
const callsign = ref('')
const submitting = ref(false)

const props = defineProps<{
  error?: string | null
  loading?: boolean
}>()

async function submit() {
  if (!email.value) return
  submitting.value = true
  try {
    if (mode.value === 'login') {
      emit('login', email.value)
    } else {
      emit('register', email.value, callsign.value)
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="login-form">
    <div class="login-header">
      <div class="logo-icon">📻</div>
      <h1>RadioLedger</h1>
      <p class="subtitle">Your amateur radio logbook in the cloud</p>
    </div>

    <div class="tab-row">
      <button
        :class="['tab-btn', { active: mode === 'login' }]"
        @click="mode = 'login'"
        type="button"
      >Sign In</button>
      <button
        :class="['tab-btn', { active: mode === 'register' }]"
        @click="mode = 'register'"
        type="button"
      >Create Account</button>
    </div>

    <form @submit.prevent="submit">
      <div class="field">
        <label for="rl-email">Email address</label>
        <input
          id="rl-email"
          v-model="email"
          type="email"
          placeholder="you@example.com"
          required
          autocomplete="email"
        />
      </div>

      <div v-if="mode === 'register'" class="field">
        <label for="rl-callsign">Callsign</label>
        <input
          id="rl-callsign"
          v-model="callsign"
          type="text"
          placeholder="W1AW"
          required
          autocomplete="off"
          style="text-transform: uppercase"
          @input="callsign = callsign.toUpperCase()"
        />
      </div>

      <div v-if="props.error" class="error-msg">{{ props.error }}</div>

      <button type="submit" class="submit-btn" :disabled="props.loading || submitting">
        <span v-if="props.loading || submitting">Please wait…</span>
        <span v-else-if="mode === 'login'">Sign In</span>
        <span v-else>Create Account</span>
      </button>
    </form>

    <p class="magic-link-note">
      We'll send you a magic link — no password needed.
    </p>
  </div>
</template>

<style scoped>
.login-form {
  background: var(--rl-surface, #16213e);
  border: 1px solid var(--rl-surface-2, #0f3460);
  border-radius: var(--rl-radius, 8px);
  padding: 40px;
  width: 100%;
  max-width: 420px;
}

.login-header {
  text-align: center;
  margin-bottom: 28px;
}

.logo-icon {
  font-size: 40px;
  margin-bottom: 8px;
}

h1 {
  font-size: 1.6rem;
  font-weight: 700;
  margin-bottom: 4px;
  color: var(--rl-text, #eaeaea);
}

.subtitle {
  color: var(--rl-text-dim, #9a9a9a);
  font-size: 0.9rem;
}

.tab-row {
  display: flex;
  gap: 0;
  margin-bottom: 24px;
  background: var(--rl-bg, #1a1a2e);
  border-radius: var(--rl-radius, 8px);
  padding: 4px;
}

.tab-btn {
  flex: 1;
  background: transparent;
  border: none;
  color: var(--rl-text-dim, #9a9a9a);
  padding: 8px 16px;
  border-radius: 6px;
  font-size: 0.9rem;
  cursor: pointer;
  transition: all 0.15s;
}

.tab-btn.active {
  background: var(--rl-accent, #e94560);
  color: #fff;
}

.tab-btn:hover:not(.active) {
  background: var(--rl-surface-2, #0f3460);
}

.field {
  margin-bottom: 16px;
}

.field label {
  display: block;
  margin-bottom: 6px;
  font-size: 0.85rem;
  color: var(--rl-text-dim, #9a9a9a);
  font-weight: 500;
}

.field input {
  width: 100%;
  background: var(--rl-bg, #1a1a2e);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: var(--rl-radius, 8px);
  color: var(--rl-text, #eaeaea);
  padding: 10px 14px;
  font-size: 0.95rem;
  transition: border-color 0.15s;
}

.field input:focus {
  outline: none;
  border-color: var(--rl-accent, #e94560);
}

.error-msg {
  background: rgba(244, 67, 54, 0.1);
  border: 1px solid var(--rl-error, #f44336);
  color: var(--rl-error, #f44336);
  border-radius: var(--rl-radius, 8px);
  padding: 10px 14px;
  font-size: 0.85rem;
  margin-bottom: 16px;
}

.submit-btn {
  width: 100%;
  background: var(--rl-accent, #e94560);
  border: none;
  color: #fff;
  font-size: 1rem;
  font-weight: 600;
  padding: 12px;
  border-radius: var(--rl-radius, 8px);
  cursor: pointer;
  transition: background 0.15s;
}

.submit-btn:hover:not(:disabled) {
  background: #c73050;
}

.submit-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.magic-link-note {
  text-align: center;
  color: var(--rl-text-dim, #9a9a9a);
  font-size: 0.8rem;
  margin-top: 16px;
}
</style>
