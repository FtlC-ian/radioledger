<template>
  <q-icon
    name="info"
    class="info-tooltip-icon"
    size="16px"
    color="grey-5"
    style="cursor: pointer; vertical-align: middle;"
  >
    <q-popup-proxy
      transition-show="scale"
      transition-hide="scale"
      :offset="[0, 6]"
    >
      <q-card flat bordered class="info-tooltip-card">
        <q-card-section class="q-pa-sm">
          <div class="text-body2 text-weight-regular info-tooltip-text">{{ text }}</div>
          <div v-if="link" class="q-mt-xs">
            <a :href="link" target="_blank" rel="noopener noreferrer" class="text-primary text-caption">
              {{ linkText || 'Learn more' }}
            </a>
          </div>
        </q-card-section>
      </q-card>
    </q-popup-proxy>
  </q-icon>
</template>

<script setup lang="ts">
defineProps<{
  text: string
  link?: string
  linkText?: string
}>()
</script>

<style scoped>
.info-tooltip-icon {
  opacity: 0.75;
  transition: opacity 0.15s;
}
.info-tooltip-icon:hover {
  opacity: 1;
}
.info-tooltip-card {
  max-width: 300px;
}
.info-tooltip-text {
  line-height: 1.5;
}
</style>
