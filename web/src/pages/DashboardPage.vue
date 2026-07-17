<template>
  <q-page class="q-pa-md">
    <div class="text-h5 q-mb-md">Dashboard</div>

    <!-- Stats cards with loading skeletons -->
    <div class="row q-col-gutter-md q-mb-md">
      <!-- Total QSOs -->
      <div class="col-12 col-sm-6 col-lg-4">
        <q-card bordered flat>
          <q-card-section>
            <div class="text-subtitle2 text-grey-6">Total QSOs</div>
            <q-skeleton v-if="logbook.statsLoading" type="text" width="80px" height="44px" />
            <div v-else class="text-h4 text-primary">{{ logbook.totalQsos }}</div>
          </q-card-section>
        </q-card>
      </div>

      <!-- Countries Worked -->
      <div class="col-12 col-sm-6 col-lg-4">
        <q-card bordered flat>
          <q-card-section>
            <div class="text-subtitle2 text-grey-6">Countries Worked</div>
            <q-skeleton v-if="logbook.statsLoading" type="text" width="60px" height="44px" />
            <div v-else class="text-h4 text-secondary">{{ logbook.countriesWorked }}</div>
          </q-card-section>
        </q-card>
      </div>

      <!-- Band Breakdown -->
      <div class="col-12 col-lg-4">
        <q-card bordered flat>
          <q-card-section>
            <div class="text-subtitle2 text-grey-6 q-mb-sm">Bands Breakdown</div>
            <div v-if="logbook.statsLoading" class="q-gutter-y-xs">
              <q-skeleton v-for="n in 3" :key="n" type="rect" height="18px" />
            </div>
            <div v-else-if="logbook.bandsBreakdown.length" class="q-gutter-y-xs">
              <div v-for="entry in logbook.bandsBreakdown" :key="entry.band">
                <div class="row justify-between text-caption">
                  <span>{{ entry.band }}</span>
                  <span>{{ entry.count }}</span>
                </div>
                <!-- Colored div bar — no charting lib needed -->
                <div class="band-bar-track">
                  <div
                    class="band-bar-fill"
                    :style="{ width: bandPercent(entry.count) + '%' }"
                  />
                </div>
              </div>
            </div>
            <div v-else class="text-caption text-grey-6">No band activity yet.</div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <!-- No data empty state -->
    <q-card v-if="!logbook.statsLoading && logbook.totalQsos === 0" flat bordered class="q-mb-md">
      <q-card-section class="text-center q-py-xl">
        <q-icon name="radio" size="64px" color="grey-6" class="q-mb-md" />
        <div class="text-h6 q-mb-sm">No QSOs yet</div>
        <div class="text-body2 text-grey-6 q-mb-lg">
          Import an ADIF file or log your first QSO to get started.
        </div>
        <div class="row justify-center q-gutter-md">
          <q-btn color="primary" icon="upload_file" label="Import ADIF" to="/import" />
          <q-btn color="secondary" icon="add_circle" label="Log a QSO" to="/qso/new" />
        </div>
      </q-card-section>
    </q-card>

    <!-- Recent QSOs -->
    <q-card v-if="logbook.totalQsos > 0 || logbook.loading" flat bordered>
      <q-card-section>
        <div class="text-subtitle1">Recent QSOs</div>
      </q-card-section>
      <q-separator />

      <q-list separator>
        <template v-if="logbook.loading && !recentQsos.length">
          <q-item v-for="n in 5" :key="n">
            <q-item-section>
              <q-skeleton type="text" />
              <q-skeleton type="text" width="60%" />
            </q-item-section>
          </q-item>
        </template>

        <q-item v-for="qso in recentQsos" :key="qso.uuid">
          <q-item-section>
            <q-item-label>
              <strong>{{ qso.callsign }}</strong>
              · {{ qso.band }} · {{ qso.mode }}
            </q-item-label>
            <q-item-label caption>
              {{ formatDateTime(qso.datetime_on || '') }}
              <span v-if="qso.country">· {{ qso.country }}</span>
            </q-item-label>
          </q-item-section>
        </q-item>
      </q-list>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useLogbookStore } from 'src/stores/logbook'

const logbook = useLogbookStore()

const maxBandCount = computed(() => Math.max(1, ...logbook.bandsBreakdown.map((e) => e.count)))

function bandPercent(count: number) {
  return Math.round((count / maxBandCount.value) * 100)
}

const recentQsos = computed(() =>
  [...logbook.qsos]
    .sort((a, b) => new Date(b.datetime_on || 0).getTime() - new Date(a.datetime_on || 0).getTime())
    .slice(0, 10),
)

onMounted(async () => {
  await Promise.all([
    logbook.fetchStats(),
    (async () => {
      if (!logbook.qsos.length) {
        await logbook.fetchQsos({ reset: true })
      }
    })(),
  ])
})

function formatDateTime(value: string) {
  return new Date(value).toLocaleString()
}
</script>

<style scoped>
.band-bar-track {
  height: 8px;
  background: var(--rl-color-border);
  border-radius: 4px;
  overflow: hidden;
}

.band-bar-fill {
  height: 100%;
  background: var(--q-accent);
  border-radius: 4px;
  transition: width 0.4s ease;
  min-width: 4px;
}
</style>
