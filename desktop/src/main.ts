/**
 * RadioLedger Desktop — bootstrap and dependency wiring.
 *
 * This is the application entry point. It owns only the composition root:
 * injecting cross-module callbacks and kicking off initialisation.
 * All orchestration (tabs, refresh, events, settings loading) lives in
 * app-shell.ts. Domain logic lives in its own modules.
 */

import {
  refreshRig,
  getCurrentRigStatus,
  setLogger as setRigControlLogger,
  setAutofillQsoDraft,
  setOnRigFrequencyChanged,
  setOnRigModeChanged,
} from './rig-control'
import { refreshStats, resetStatsCache, setLogger as setStatsLogger } from './stats-dashboard'
import {
  initAuthMode,
  setAuthMode,
  setLogger as setSettingsAuthLogger,
  setRefreshAll as setSettingsAuthRefreshAll,
  setResetStatsCache as setSettingsAuthResetStatsCache,
  setSwitchToTab as setSettingsAuthSwitchToTab,
  setRefreshStats as setSettingsAuthRefreshStats,
} from './settings-auth'
import { setLogger as setUdpListenerLogger } from './udp-listener'
import { setLogger as setRigProfilesLogger, setOnAfterActivate } from './rig-profiles'
import {
  autofillQsoDraft,
  initQsoForm,
  setupCallsignLookup,
  loadWsjtxDecodePanelSettings,
  refreshWsjtxDecodes,
  onRigFrequencyChanged,
  onRigModeChanged,
  setLogger as setQsoFormLogger,
  setGetCurrentRigStatus,
  setOnAfterLogQso,
} from './qso-form'
import {
  setLogger as setWizardLogger,
  setOnWizardFinish,
  attachWizardInit,
} from './setup-wizard'
import {
  loadLogQsos,
  initLogColumns,
  setLogger as setLogViewLogger,
  setOnAfterSave as setLogViewOnAfterSave,
} from './log-view'
import {
  refreshSync,
  setLogger as setSyncStatusLogger,
  setRefreshWsjtxDecodes as setSyncStatusRefreshWsjtxDecodes,
} from './sync-status'
import { log } from './ui-helpers'
import {
  initTabs,
  switchToTab,
  refreshAll,
  wireEventListeners,
  setupEventListeners,
} from './app-shell'
import type { RigStatusLike } from './qso-form'

// ─── Bootstrap ────────────────────────────────────────────────────────────────

window.addEventListener('DOMContentLoaded', () => {
  // ── Logger injection ──────────────────────────────────────────────────
  setStatsLogger(log)
  setRigProfilesLogger(log)
  setQsoFormLogger(log)
  setRigControlLogger(log)
  setWizardLogger(log)
  setLogViewLogger(log)
  setSyncStatusLogger(log)
  setSettingsAuthLogger(log)
  setUdpListenerLogger(log)

  // ── Cross-module callback injection ────────────────────────────────────
  setOnAfterActivate(() => refreshRig())
  setGetCurrentRigStatus(() => getCurrentRigStatus())
  setAutofillQsoDraft((status) => autofillQsoDraft(status as RigStatusLike))
  setOnRigFrequencyChanged(onRigFrequencyChanged)
  setOnRigModeChanged(onRigModeChanged)
  setOnAfterLogQso(async () => {
    await refreshSync()
    void loadLogQsos()
  })
  setOnWizardFinish((authMode) => {
    setAuthMode(authMode)
    void refreshAll()
  })
  setLogViewOnAfterSave(async () => { await refreshSync() })
  setSyncStatusRefreshWsjtxDecodes(async () => { void refreshWsjtxDecodes() })
  setSettingsAuthRefreshAll(refreshAll)
  setSettingsAuthResetStatsCache(resetStatsCache)
  setSettingsAuthSwitchToTab(switchToTab)
  setSettingsAuthRefreshStats(async () => { void refreshStats() })

  // ── Shell initialisation ──────────────────────────────────────────────
  initTabs()
  wireEventListeners()
  initQsoForm()
  setupCallsignLookup()
  void initLogColumns()
  void loadWsjtxDecodePanelSettings()
  void refreshAll()
  void initAuthMode()
  setupEventListeners()
  setInterval(() => { void refreshAll() }, 30_000)
})
attachWizardInit()