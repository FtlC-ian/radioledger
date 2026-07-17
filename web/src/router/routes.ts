import type { RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    component: () => import('layouts/MainLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', component: () => import('pages/StatsPage.vue') },
      { path: 'stats', component: () => import('pages/StatsPage.vue') },
      { path: 'logbook', component: () => import('pages/LogbookPage.vue') },
      { path: 'qso/new', component: () => import('pages/QsoEntryPage.vue') },
      { path: 'import', component: () => import('pages/ImportPage.vue') },
      { path: 'awards', component: () => import('pages/AwardsPage.vue') },
      { path: 'activations', component: () => import('pages/ActivationsPage.vue') },
      { path: 'contests', component: () => import('pages/ContestPage.vue') },
      { path: 'contests/:uuid', component: () => import('pages/ContestPage.vue') },
      { path: 'settings', component: () => import('pages/SettingsPage.vue') },
      // /settings/lotw is now folded into the Sync Services tab on the main Settings page.
      { path: 'settings/lotw', redirect: { path: '/settings', query: { tab: 'sync' } } },
      { path: 'sync', component: () => import('pages/SyncPage.vue') },
      {
        path: 'admin/jobs',
        component: () => import('pages/AdminJobsPage.vue'),
        meta: { requiresAuth: true, requiresAdmin: true },
      },
    ],
  },
  {
    path: '/login',
    component: () => import('pages/LoginPage.vue'),
  },
  {
    path: '/legal',
    component: () => import('layouts/MinimalLayout.vue'),
    children: [
      { path: 'terms', component: () => import('pages/LegalPage.vue') },
      { path: 'privacy', component: () => import('pages/LegalPage.vue') },
    ],
  },
  {
    path: '/auth/callback',
    component: () => import('pages/AuthCallbackPage.vue'),
  },
  {
    path: '/onboarding',
    component: () => import('pages/OnboardingPage.vue'),
    meta: { requiresAuth: true },
  },
  {
    path: '/:catchAll(.*)*',
    component: () => import('pages/ErrorNotFound.vue'),
  },
]

export default routes
