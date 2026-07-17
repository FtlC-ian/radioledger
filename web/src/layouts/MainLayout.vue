<template>
  <q-layout view="lHh Lpr lFf">
    <q-header elevated class="app-header">
      <q-toolbar>
        <q-btn flat dense round icon="menu" aria-label="Menu" class="lt-md" @click="drawerOpen = !drawerOpen" />

        <q-toolbar-title class="row items-center no-wrap">
          <img :src="logoNavbarSrc" alt="RadioLedger" style="height: 40px;" />
        </q-toolbar-title>

        <q-btn flat round dense icon="notifications" aria-label="Notifications">
          <q-badge v-if="unreadCount > 0" color="negative" floating>{{ unreadCount }}</q-badge>

          <q-menu
            v-model="notificationMenuOpen"
            anchor="bottom right"
            self="top right"
            :offset="[0, 8]"
            class="notification-menu"
          >
            <q-list style="min-width: 340px; max-width: 420px">
              <q-item>
                <q-item-section>
                  <div class="text-subtitle2">Notifications</div>
                  <div class="text-caption text-grey-6">Unread: {{ unreadCount }}</div>
                </q-item-section>
                <q-item-section side>
                  <q-btn
                    flat
                    dense
                    size="sm"
                    label="Mark all read"
                    :disable="unreadCount === 0"
                    @click="markAllRead"
                  />
                </q-item-section>
              </q-item>

              <q-separator />

              <q-item v-if="notifications.length === 0">
                <q-item-section class="text-grey-6">No notifications yet</q-item-section>
              </q-item>

              <q-item
                v-for="item in notifications"
                :key="item.uuid"
                clickable
                v-ripple
                @click="openNotification(item)"
              >
                <q-item-section avatar>
                  <q-icon :name="item.is_read ? 'notifications_none' : 'notifications_active'" />
                </q-item-section>
                <q-item-section>
                  <q-item-label :class="{ 'text-weight-medium': !item.is_read }">{{ notificationTitle(item) }}</q-item-label>
                  <q-item-label caption>{{ notificationMessage(item) }}</q-item-label>
                  <q-item-label caption class="text-grey-6">{{ formatTimestamp(item.created_at) }}</q-item-label>
                </q-item-section>
                <q-item-section side>
                  <q-btn
                    flat
                    round
                    dense
                    size="sm"
                    icon="close"
                    aria-label="Dismiss"
                    @click.stop="dismissNotification(item)"
                  />
                </q-item-section>
              </q-item>
            </q-list>
          </q-menu>
        </q-btn>

        <q-toggle v-model="darkMode" checked-icon="dark_mode" unchecked-icon="light_mode" color="amber" />
      </q-toolbar>
    </q-header>

    <q-drawer
      v-model="drawerOpen"
      show-if-above
      bordered
      :width="240"
      :breakpoint="768"
      class="app-drawer"
    >
      <q-list padding>
        <q-item v-if="auth.isAuthenticated" class="q-pb-sm">
          <q-item-section avatar>
            <q-avatar color="primary" text-color="white" size="40px">
              {{ avatarInitial }}
            </q-avatar>
          </q-item-section>
          <q-item-section>
            <q-item-label class="text-bold">{{ auth.displayName }}</q-item-label>
            <q-item-label caption class="app-muted-text">{{ auth.email }}</q-item-label>
          </q-item-section>
        </q-item>

        <q-separator v-if="auth.isAuthenticated" class="q-mb-sm app-separator" />

        <q-item-label header class="app-nav-header">Navigation</q-item-label>

        <q-item v-for="item in visibleNavItems" :key="item.to" clickable v-ripple :to="item.to" exact active-class="bg-primary text-white">
          <q-item-section avatar>
            <q-icon :name="item.icon" />
          </q-item-section>
          <q-item-section>{{ item.label }}</q-item-section>
          <q-item-section v-if="item.to === '/sync' && totalPendingSync > 0" side>
            <q-badge color="warning" text-color="dark" :label="totalPendingSync" />
          </q-item-section>
        </q-item>

        <q-separator class="q-mt-sm q-mb-sm app-separator" />

        <q-item clickable v-ripple tag="a" href="/docs/" target="_blank" rel="noopener noreferrer" class="app-help-link">
          <q-item-section avatar>
            <q-icon name="help_outline" />
          </q-item-section>
          <q-item-section>Help &amp; Docs</q-item-section>
          <q-item-section side>
            <q-icon name="open_in_new" size="xs" class="app-muted-text" />
          </q-item-section>
        </q-item>

        <q-item clickable v-ripple @click="onLogout" class="app-signout">
          <q-item-section avatar>
            <q-icon name="logout" />
          </q-item-section>
          <q-item-section>Sign Out</q-item-section>
        </q-item>
      </q-list>
    </q-drawer>

    <q-page-container>
      <router-view />
    </q-page-container>

    <LotwSyncModal
      v-model="showSyncModal"
      :callsign="lotwCallsign"
      :pending-count="lotwPendingCount"
    />
  </q-layout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { LocalStorage, useQuasar } from 'quasar'
import { applyThemePreference, getStoredThemePreference } from 'src/utils/themePreference'
import { apiDelete, apiGet, apiPut, setLoadingBarHandlers } from 'src/api/client'
import { useAuthStore } from 'src/stores/auth'
import { useLogbookStore } from 'src/stores/logbook'
import logoNavbarDarkUrl from 'src/assets/branding/logo-all-white.png'
import logoNavbarLightUrl from 'src/assets/branding/logo-navbar.png'
import LotwSyncModal from 'src/components/LotwSyncModal.vue'
import {
  LOTW_USE_MOCK,
  mockPendingCount,
  mockSettings,
  mockUpdateSettings,
} from 'src/composables/useLotwMock'
import { clearLotwSession } from 'src/composables/useLotwSession'
import * as lotwApi from 'src/services/lotwApi'

interface NotificationPayload {
  title?: string
  message?: string
  route?: string
  import_job_uuid?: string
  [key: string]: unknown
}

interface NotificationItem {
  uuid: string
  type: string
  payload: NotificationPayload
  is_read: boolean
  read_at?: string
  created_at: string
}

const $q = useQuasar()
const router = useRouter()
const auth = useAuthStore()
const logbook = useLogbookStore()

const drawerOpen = ref(true)
const notificationMenuOpen = ref(false)
const notifications = ref<NotificationItem[]>([])
const unreadCount = ref(0)
const showSyncModal = ref(false)
const lotwPendingCount = ref(0)
const lotwCallsign = ref('')
// Total pending QSOs across all sync services — drives the nav badge.
const totalPendingSync = ref(0)

let notificationTimer: ReturnType<typeof setInterval> | null = null

const navItems = [
  { label: 'Dashboard', icon: 'dashboard', to: '/dashboard' },
  { label: 'Logbook', icon: 'table_rows', to: '/logbook' },
  { label: 'New QSO', icon: 'add_circle', to: '/qso/new' },
  { label: 'Import', icon: 'upload_file', to: '/import' },
  { label: 'Awards', icon: 'emoji_events', to: '/awards' },
  { label: 'Activations', icon: 'park', to: '/activations' },
  { label: 'Sync', icon: 'sync', to: '/sync' },
  { label: 'Admin Jobs', icon: 'admin_panel_settings', to: '/admin/jobs', adminOnly: true },
  { label: 'Settings', icon: 'settings', to: '/settings' },
]

const visibleNavItems = computed(() => navItems.filter((item) => !item.adminOnly || auth.isAdmin))

const darkMode = computed({
  get: () => $q.dark.isActive,
  set: (value: boolean) => {
    applyThemePreference($q, value ? 'dark' : 'light')
  },
})

const logoNavbarSrc = computed(() => (darkMode.value ? logoNavbarDarkUrl : logoNavbarLightUrl))

const avatarInitial = computed(() => {
  const cs = auth.callsign
  if (cs) return cs.charAt(0).toUpperCase()
  const dn = auth.displayName
  return dn ? dn.charAt(0).toUpperCase() : '?'
})

async function fetchUnreadCount() {
  try {
    const response = await apiGet<{ count: number }>('/v1/notifications/unread-count')
    unreadCount.value = response.success ? Number(response.data?.count || 0) : 0
  } catch {
    unreadCount.value = 0
  }
}

async function fetchNotifications() {
  try {
    const response = await apiGet<{ items?: NotificationItem[] }>('/v1/notifications?page=1&page_size=8')
    notifications.value = response.success && Array.isArray(response.data?.items) ? response.data.items : []
  } catch {
    notifications.value = []
  }
}

function notificationTitle(item: NotificationItem): string {
  const explicit = item.payload?.title
  if (typeof explicit === 'string' && explicit.trim() !== '') {
    return explicit
  }

  switch (item.type) {
    case 'import_complete':
      return 'Import complete'
    case 'import_failed':
      return 'Import failed'
    case 'sync_complete':
      return 'Sync complete'
    case 'qsl_confirmed':
      return 'QSL confirmed'
    case 'award_milestone':
      return 'Award Milestone'
    default:
      return 'Notification'
  }
}

function notificationMessage(item: NotificationItem): string {
  const explicit = item.payload?.message
  if (typeof explicit === 'string' && explicit.trim() !== '') {
    return explicit
  }

  if (item.type === 'award_milestone') {
    const label = item.payload?.label || item.payload?.award_type || 'Award'
    const milestone = item.payload?.milestone
    const worked = item.payload?.worked
    if (milestone && worked) {
      return `${label}: ${worked} worked (${milestone} milestone)`
    }
    if (milestone) {
      return `${label}: ${milestone} milestone reached!`
    }
    return String(label)
  }

  return item.type.replace(/_/g, ' ')
}

function notificationRoute(item: NotificationItem): string {
  const routeFromPayload = item.payload?.route
  if (typeof routeFromPayload === 'string' && routeFromPayload.trim() !== '') {
    return routeFromPayload
  }

  const importJobUUID = item.payload?.import_job_uuid
  if (typeof importJobUUID === 'string' && importJobUUID.trim() !== '') {
    return `/import?job=${encodeURIComponent(importJobUUID)}`
  }

  if (item.type === 'award_milestone') {
    return '/awards'
  }

  return '/dashboard'
}

function formatTimestamp(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

async function openNotification(item: NotificationItem) {
  if (!item.is_read) {
    try {
      await apiPut(`/v1/notifications/${item.uuid}/read`)
      item.is_read = true
      item.read_at = new Date().toISOString()
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    } catch {
      // Continue navigation even if mark-read fails.
    }
  }

  notificationMenuOpen.value = false
  await router.push(notificationRoute(item))
}

async function dismissNotification(item: NotificationItem) {
  try {
    await apiDelete(`/v1/notifications/${item.uuid}`)
    notifications.value = notifications.value.filter((candidate) => candidate.uuid !== item.uuid)
    if (!item.is_read) {
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    }
  } catch {
    $q.notify({ type: 'negative', message: 'Could not dismiss notification' })
  }
}

async function markAllRead() {
  try {
    await apiPut('/v1/notifications/read-all')
    unreadCount.value = 0
    notifications.value = notifications.value.map((item) => ({
      ...item,
      is_read: true,
      read_at: item.read_at || new Date().toISOString(),
    }))
  } catch {
    $q.notify({ type: 'negative', message: 'Could not mark notifications as read' })
  }
}

async function fetchSyncPendingCount() {
  try {
    const res = await apiGet<any>('/v1/sync/status?page=1&page_size=1')
    totalPendingSync.value = res.success && res.data?.services
      ? Object.values(res.data.services as Record<string, { pending_count?: number }>)
        .reduce((sum, service) => sum + Number(service?.pending_count || 0), 0)
      : 0
  } catch {
    totalPendingSync.value = 0
  }
}

async function refreshNotifications() {
  try {
    await Promise.all([fetchUnreadCount(), fetchSyncPendingCount()])
    if (notificationMenuOpen.value) {
      await fetchNotifications()
    }
  } catch {
    unreadCount.value = 0
    totalPendingSync.value = 0
    notifications.value = []
  }
}

async function checkLotwPending() {
  try {
    const settings = LOTW_USE_MOCK ? mockSettings : await lotwApi.getSettings()
    if (!settings?.has_cert || !settings?.auto_sync_prompt) {
      lotwPendingCount.value = 0
      lotwCallsign.value = auth.callsign || ''
      return
    }

    const pending = LOTW_USE_MOCK ? mockPendingCount : await lotwApi.getPendingCount()
    const pendingCount = Number(pending?.pending_count || 0)
    if (pendingCount <= 0) {
      lotwPendingCount.value = 0
      lotwCallsign.value = auth.callsign || ''
      return
    }

    lotwPendingCount.value = pendingCount
    lotwCallsign.value = auth.callsign || ''

    $q.notify({
      message: `📡 ${pendingCount} QSO${pendingCount !== 1 ? 's' : ''} haven't been synced to LoTW`,
      timeout: 0,
      position: 'bottom-right',
      actions: [
        {
          label: 'Sync Now',
          color: 'white',
          handler: () => { void router.push('/sync?action=sync-all') },
        },
        {
          label: 'Later',
          color: 'white',
        },
        {
          label: "Don't ask again",
          color: 'white',
          handler: async () => {
            try {
              if (LOTW_USE_MOCK) {
                await mockUpdateSettings({ auto_sync_prompt: false })
              } else {
                await lotwApi.updateSettings({ auto_sync_prompt: false })
              }
            } catch {
              // Best-effort
            }
          },
        },
      ],
    })
  } catch {
    lotwPendingCount.value = 0
    lotwCallsign.value = auth.callsign || ''
  }
}

async function onLogout() {
  clearLotwSession()
  logbook.clearLogbook()
  const shouldRedirectLocally = !auth.isOidc
  await auth.logout()
  if (shouldRedirectLocally) {
    await router.push('/login')
  }
}

onMounted(() => {
  const storedThemePreference = getStoredThemePreference()
  if (storedThemePreference) {
    applyThemePreference($q, storedThemePreference)
  } else {
    const legacyDarkMode = LocalStorage.getItem('radioledger.darkMode')
    if (typeof legacyDarkMode === 'boolean') {
      applyThemePreference($q, legacyDarkMode ? 'dark' : 'light')
      LocalStorage.remove('radioledger.darkMode')
    }
  }

  setLoadingBarHandlers(
    () => $q.loadingBar.start(),
    () => $q.loadingBar.stop(),
  )

  void refreshNotifications()
  void checkLotwPending()

  notificationTimer = setInterval(() => {
    void refreshNotifications()
  }, 30000)
})

watch(notificationMenuOpen, (open) => {
  if (open) {
    void fetchNotifications()
  }
})

onBeforeUnmount(() => {
  if (notificationTimer) {
    clearInterval(notificationTimer)
    notificationTimer = null
  }
})
</script>

<style scoped>
.app-header {
  background: var(--rl-color-header-bg);
  color: var(--rl-color-header-text);
  border-bottom: 1px solid var(--rl-color-border);
}

.app-drawer {
  background: var(--rl-color-sidebar-bg);
  color: var(--rl-color-sidebar-text);
  border-right: 1px solid var(--rl-color-sidebar-border);
}

.app-muted-text,
.app-nav-header,
.app-signout {
  color: var(--rl-color-sidebar-muted);
}

.app-separator {
  background: var(--rl-color-sidebar-border);
}
</style>
