import { defineRouter } from '#q-app/wrappers'
import { createMemoryHistory, createRouter, createWebHashHistory, createWebHistory } from 'vue-router'
import routes from './routes'

// In hash-mode routing, Zitadel redirects to /auth/callback?code=xxx&state=yyy
// but the hash router ignores the pathname and routes based on the # fragment.
// Before the router initializes, detect OIDC callback params and rewrite the hash
// so Vue Router navigates to /auth/callback with the params preserved in search.
if (typeof window !== 'undefined' && !process.env.SERVER) {
  const params = new URLSearchParams(window.location.search)
  if ((params.has('code') && params.has('state')) || params.has('error')) {
    // Rewrite to #/auth/callback — the search params stay in window.location.search
    // for AuthCallbackPage to read.
    if (!window.location.hash.includes('/auth/callback')) {
      window.location.hash = '#/auth/callback'
    }
  }
}

export default defineRouter(function () {
  const createHistory = process.env.SERVER
    ? createMemoryHistory
    : process.env.VUE_ROUTER_MODE === 'history'
      ? createWebHistory
      : createWebHashHistory

  const router = createRouter({
    scrollBehavior: () => ({ left: 0, top: 0 }),
    routes,
    history: createHistory(process.env.VUE_ROUTER_BASE),
  })

  router.beforeEach(async (to) => {
    if (to.path === '/auth/callback') {
      return true
    }

    // Lazy import to avoid initializing store before Pinia is ready.
    const { useAuthStore } = await import('src/stores/auth')
    const auth = useAuthStore()

    const requiresAuth = to.matched.some((record) => Boolean(record.meta?.requiresAuth))
    const requiresAdmin = to.matched.some((record) => Boolean(record.meta?.requiresAdmin))

    if (auth.isAuthenticated && !auth.userProfile) {
      try {
        await auth.fetchMe()
      } catch {
        // Best-effort here — bootstrap auth validation handles forced logout on 401.
      }
    }

    if (requiresAuth && !auth.isAuthenticated) {
      return '/login'
    }

    if (auth.isAuthenticated && to.path === '/login') {
      return auth.needsOnboarding ? '/onboarding' : '/dashboard'
    }

    if (auth.isAuthenticated && auth.needsOnboarding && to.path !== '/onboarding') {
      return '/onboarding'
    }

    if (auth.isAuthenticated && !auth.needsOnboarding && to.path === '/onboarding') {
      return '/dashboard'
    }

    if (requiresAdmin && !auth.isAdmin) {
      return '/dashboard'
    }
  })

  return router
})
