import { defineBoot } from '#q-app/wrappers'
import { setUnauthorizedHandler } from 'src/api/client'
import { clearLotwSession } from 'src/composables/useLotwSession'
import { useAuthStore } from 'src/stores/auth'
import { useLogbookStore } from 'src/stores/logbook'

function currentPathname() {
  if (typeof window === 'undefined') {
    return '/'
  }

  const hashPath = window.location.hash.replace(/^#/, '')
  if (hashPath.startsWith('/')) {
    return hashPath.split('?')[0] || '/'
  }

  return window.location.pathname || '/'
}

export default defineBoot(async ({ router }) => {
  const auth = useAuthStore()
  const logbook = useLogbookStore()

  let redirectInFlight = false

  const redirectToLogin = () => {
    if (redirectInFlight) {
      return
    }

    redirectInFlight = true
    clearLotwSession()
    logbook.clearLogbook()
    auth.clearSession()

    const path = currentPathname()
    if (path === '/login' || path === '/auth/callback') {
      redirectInFlight = false
      return
    }

    void router.replace('/login').finally(() => {
      redirectInFlight = false
    })
  }

  setUnauthorizedHandler(redirectToLogin)

  if (!auth.isAuthenticated || currentPathname() === '/auth/callback') {
    return
  }

  try {
    const isValid = await auth.validateSession()
    if (!isValid) {
      redirectToLogin()
    }
  } catch {
    // Non-auth failures should not block app bootstrap.
  }
})
