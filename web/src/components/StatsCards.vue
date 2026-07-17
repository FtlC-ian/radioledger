<template>
  <div class="row q-col-gutter-md q-mb-md">
    <div class="col-12 col-sm-6 col-lg-4">
      <q-card bordered flat>
        <q-card-section>
          <div class="text-subtitle2 text-grey-6">Total QSOs</div>
          <div class="text-h4 text-primary">{{ totalQsos }}</div>
        </q-card-section>
      </q-card>
    </div>

    <div class="col-12 col-sm-6 col-lg-4">
      <q-card bordered flat>
        <q-card-section>
          <div class="text-subtitle2 text-grey-6">Countries Worked</div>
          <div class="text-h4 text-secondary">{{ countriesWorked }}</div>
        </q-card-section>
      </q-card>
    </div>

    <div class="col-12 col-lg-4">
      <q-card bordered flat>
        <q-card-section>
          <div class="text-subtitle2 text-grey-6 q-mb-sm">Bands Breakdown</div>
          <div v-if="bandsBreakdown.length" class="q-gutter-y-sm">
            <div v-for="entry in bandsBreakdown" :key="entry.band">
              <div class="row justify-between text-caption">
                <span>{{ entry.band }}</span>
                <span>{{ entry.count }}</span>
              </div>
              <q-linear-progress :value="entry.count / maxBandCount" color="accent" rounded size="8px" />
            </div>
          </div>
          <div v-else class="text-caption text-grey-6">No band activity yet.</div>
        </q-card-section>
      </q-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  totalQsos: number
  countriesWorked: number
  bandsBreakdown: Array<{ band: string; count: number }>
}

const props = defineProps<Props>()

const maxBandCount = computed(() => Math.max(1, ...props.bandsBreakdown.map((entry) => entry.count)))
</script>
