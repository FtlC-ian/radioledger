<template>
  <div>
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Sync Dashboard</div>
        <div class="text-body2 text-grey-5">Sync QSOs to external services and track per-QSO status.</div>
      </div>
      <q-btn color="primary" icon="refresh" label="Refresh" :loading="loading" @click="emit('refresh')" />
    </div>

    <div class="row q-col-gutter-md q-mb-md">
      <div v-for="svc in knownServices" :key="svc" class="col-12 col-sm-6 col-lg-3">
        <q-card flat bordered class="service-card column full-height">
          <q-card-section class="q-pb-xs">
            <div class="row items-start justify-between no-wrap">
              <div class="row items-center q-gutter-xs no-wrap">
                <q-icon :name="serviceIcon(svc)" size="18px" />
                <span class="text-subtitle2">{{ serviceLabel(svc) }}</span>
              </div>
              <div class="column items-end q-gutter-y-xs">
                <q-btn
                  flat dense no-caps
                  icon="settings"
                  :color="serviceConfigColor(svc)"
                  :label="serviceConfigLabel(svc)"
                  @click="emit('open-settings', svc)"
                >
                  <q-tooltip>{{ serviceConfigTooltip(svc) }}</q-tooltip>
                </q-btn>
                <q-badge
                  v-if="serviceHealthBadgeLabel(svc)"
                  :color="serviceHealthBadgeColor(svc)"
                  :label="serviceHealthBadgeLabel(svc)"
                />
              </div>
            </div>
          </q-card-section>

          <q-card-section class="q-pt-xs q-pb-xs col">
            <template v-if="servicePendingCount(svc) > 0">
              <span class="text-h5 text-weight-bold text-warning">{{ servicePendingCount(svc) }}</span>
              <span class="text-caption text-grey-5 q-ml-xs">pending sync</span>
            </template>
            <template v-else-if="syncProgress[svc] && (syncProgress[svc]?.total_count ?? 0) > 0">
              <div class="row items-center q-gutter-xs text-positive">
                <q-icon name="check_circle" size="16px" />
                <span class="text-caption">All synced</span>
              </div>
            </template>
            <template v-else>
              <div class="row items-center q-gutter-xs text-grey-5">
                <q-icon name="pending" size="16px" />
                <span class="text-caption">No QSOs queued</span>
              </div>
            </template>
          </q-card-section>

          <q-card-actions class="q-pt-none">
            <q-btn
              flat dense color="primary"
              :label="`Sync to ${serviceLabel(svc)}`"
              :loading="actionLoading === `bulk-${svc}`"
              :disable="servicePendingCount(svc) === 0 || !serviceConfigured(svc)"
              class="full-width"
              @click="emit('trigger-service-sync', svc)"
            />
            <div v-if="svc === 'lotw' && !lotwHasCert" class="full-width text-center q-mb-xs">
              <router-link to="/settings?tab=sync" class="text-caption text-primary">
                Upload a certificate first
              </router-link>
            </div>
          </q-card-actions>
        </q-card>
      </div>
    </div>

    <div class="row items-center q-gutter-md q-mb-lg">
      <q-btn
        color="primary" icon="sync" size="md"
        :label="totalPending > 0 ? `Sync All  ·  ${formatNumber(totalPending)} pending` : 'Sync All'"
        :loading="syncAllLoading"
        :disable="totalPending === 0"
        @click="emit('sync-all')"
      />
      <span v-if="totalPending === 0" class="text-body2 text-grey-5">Everything is in sync.</span>
    </div>

    <div v-if="progressCards.length" class="q-mb-md">
      <q-card flat bordered>
        <q-card-section>
          <div v-for="p in progressCards" :key="p.service" class="q-mb-sm">
            <q-banner v-if="p.is_stalled" dense class="text-white bg-warning q-mb-xs" rounded>
              <template #avatar>
                <q-icon name="warning" color="white" />
              </template>
              <strong>{{ serviceLabel(p.service) }} sync stalled</strong>, no activity for 60 s and no worker running.
              <template #action>
                <q-btn
                  flat color="white" icon="restart_alt" label="Retry"
                  :loading="actionLoading === `retry-${p.service}`"
                  @click="emit('retry-failed', p.service)"
                />
              </template>
            </q-banner>

            <div v-else class="row items-center q-gutter-sm text-body2">
              <q-icon v-if="p.failed_count > 0" name="error" color="negative" />
              <span v-if="p.failed_count > 0">
                {{ serviceLabel(p.service) }} sync finished with errors: {{ p.error_message || '' }}
                <router-link to="/settings?tab=sync" class="text-primary q-ml-xs">Check credentials in Settings.</router-link>
              </span>
              <span v-else-if="p.pending_count === 0 && p.uploaded_count > 0">
                {{ serviceLabel(p.service) }} sync complete, {{ formatNumber(p.uploaded_count) }} synced
              </span>
              <span v-else-if="p.is_running">
                Syncing to {{ serviceLabel(p.service) }}: {{ formatNumber(p.uploaded_count) }} / {{ formatNumber(p.total_count) }}
                ({{ p.percent }}%)
              </span>

              <q-btn
                v-if="p.is_running && p.pending_count > 0 && p.failed_count === 0"
                dense flat color="negative" icon="cancel" label="Cancel"
                :loading="actionLoading === `cancel-${p.service}`"
                @click="emit('cancel-sync', p.service)"
              />
              <q-btn
                v-if="p.failed_count > 0 && !p.is_running"
                dense flat color="warning" icon="restart_alt" label="Retry failed"
                :loading="actionLoading === `retry-${p.service}`"
                @click="emit('retry-failed', p.service)"
              />
            </div>

            <div v-if="p.error_message && !p.is_stalled" class="text-caption text-negative q-ml-lg">{{ p.error_message }}</div>
          </div>
        </q-card-section>
      </q-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { toRef } from 'vue'
import type { SyncProgress } from 'src/types/sync'
import { useSyncOverviewDisplay } from 'src/pages/sync/composables/useSyncOverviewDisplay'
import { formatNumber, serviceIcon, serviceLabel } from 'src/utils/syncHelpers'

const props = defineProps<{
  loading: boolean
  actionLoading: string
  syncAllLoading: boolean
  knownServices: readonly string[]
  serviceHealth: Record<string, string>
  syncProgress: Record<string, SyncProgress>
  activeSyncServices: Set<string>
  lotwHasCert: boolean
  lotwRawPendingCount: number
  totalPending: number
}>()

const emit = defineEmits<{
  refresh: []
  'open-settings': [service: string]
  'trigger-service-sync': [service: string]
  'sync-all': []
  'retry-failed': [service: string]
  'cancel-sync': [service: string]
}>()

const {
  progressCards,
  servicePendingCount,
  serviceConfigured,
  serviceConfigLabel,
  serviceConfigColor,
  serviceConfigTooltip,
  serviceHealthBadgeLabel,
  serviceHealthBadgeColor,
} = useSyncOverviewDisplay({
  serviceHealth: toRef(props, 'serviceHealth'),
  syncProgress: toRef(props, 'syncProgress'),
  activeSyncServices: toRef(props, 'activeSyncServices'),
  lotwHasCert: toRef(props, 'lotwHasCert'),
  lotwRawPendingCount: toRef(props, 'lotwRawPendingCount'),
})
</script>

<style scoped>
.service-card {
  transition: box-shadow 0.15s ease;
}
.service-card:hover {
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.15);
}
</style>
