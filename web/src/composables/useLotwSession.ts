/**
 * useLotwSession — module-scoped LoTW session state.
 *
 * The vault password is kept at module scope (survives component teardown),
 * but is explicitly cleared on logout via clearLotwSession().
 * This prevents the password from leaking to a new user who logs in on the
 * same browser tab.
 */

import { ref } from 'vue'

// Module-level ref — lives for the lifetime of the JS module (i.e., the tab),
// but is cleared on logout.
const _sessionVaultPassword = ref('')

export function useLotwSession() {
  return {
    sessionVaultPassword: _sessionVaultPassword,
  }
}

/** Call this on every logout path to wipe the cached vault password. */
export function clearLotwSession() {
  _sessionVaultPassword.value = ''
}
