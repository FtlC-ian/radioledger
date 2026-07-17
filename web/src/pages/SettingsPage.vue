<template>
  <q-page class="q-pa-md settings-page">
    <div class="row items-center justify-between q-mb-md">
      <div>
        <div class="text-h5">Settings</div>
        <div class="text-body2 text-grey-5">Profile, logbook defaults, appearance, API access, and sync services.</div>
      </div>
      <q-btn
        v-if="activeTab === 'general'"
        color="primary"
        icon="save"
        label="Save changes"
        :loading="generalTabSaving"
        @click="saveGeneralSettings"
      />
    </div>

    <q-tabs
      v-model="activeTab"
      dense
      align="left"
      class="q-mb-md"
      active-color="primary"
      indicator-color="primary"
    >
      <q-tab name="general" label="General" icon="tune" />
      <q-tab name="sync" label="Sync Services" icon="sync" />
    </q-tabs>

    <div v-show="activeTab === 'general'">
      <SettingsGeneralTab ref="generalTabRef" />
    </div>

    <div v-show="activeTab === 'sync'">
      <SettingsSyncServicesTab :highlight-service="highlightService" />
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import SettingsGeneralTab from './settings/components/SettingsGeneralTab.vue'
import SettingsSyncServicesTab from './settings/components/SettingsSyncServicesTab.vue'

type SettingsGeneralTabInstance = InstanceType<typeof SettingsGeneralTab>

const route = useRoute()
const router = useRouter()
const activeTab = ref((route.query.tab as string) || 'general')
// The `service` query param is forwarded to SettingsSyncServicesTab so it can
// scroll to / highlight the relevant credential card when navigating from the
// Sync dashboard settings buttons.
const highlightService = ref((route.query.service as string) || '')
const generalTabRef = ref<SettingsGeneralTabInstance | null>(null)

const generalTabSaving = computed(() => generalTabRef.value?.saving ?? false)

watch(activeTab, (tab) => {
  void router.replace({
    query: { ...route.query, tab: tab === 'general' ? undefined : tab },
  })
})

watch(
  () => route.query.tab,
  (tab) => {
    activeTab.value = (tab as string) || 'general'
  },
)

watch(
  () => route.query.service,
  (service) => {
    highlightService.value = (service as string) || ''
  },
)

function saveGeneralSettings() {
  void generalTabRef.value?.savePreferences()
}
</script>

<style scoped>
.settings-page {
  max-width: 1220px;
  margin: 0 auto;
}
</style>
