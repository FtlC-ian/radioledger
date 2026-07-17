/**
 * Sync Status — server sync status display, manual sync trigger,
 * sync-status-changed event handling, and status-bar sync updates.
 *
 * Extracted from main.ts as part of the desktop decomposition (issue #194).
 * Manages the sync card in the Shack tab, the sync section of the
 * persistent status bar, and the Tauri event listener for sync changes.
 *
 * The host shell injects callbacks for logging and triggering a WSJT-X
 * decode refresh after a sync event.
 */

import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { formatError as _formatError } from './ui-helpers'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface SyncStatus {
  pending: number
  last_sync: string | null
  last_error: string | null
}

// ─── Injected callbacks ──────────────────────────────────────────────────────

let _log: (msg: string) => void = () => {}
let _refreshWsjtxDecodes: () => Promise<void> = async () => {}

/** Inject the shared activity-log helper from the shell bootstrap. */
export function setLogger(fn: (msg: string) => void): void {
  _log = fn
}

/** Inject a callback to refresh WSJT-X decodes after sync status changes. */
export function setRefreshWsjtxDecodes(fn: () => Promise<void>): void {
  _refreshWsjtxDecodes = fn
}

// ─── Status bar updater ───────────────────────────────────────────────────────

function updateStatusBarSync(pending: number, lastSync: string): void {
  const pendingEl = document.getElementById('statusbar-pending')
  const syncEl = document.getElementById('statusbar-last-sync')
  if (pendingEl) pendingEl.textContent = String(pending)
  if (syncEl) syncEl.textContent = lastSync
}

// ─── Sync status refresh & display ────────────────────────────────────────────

export async function refreshSync(): Promise<void> {
  try {
    const status: SyncStatus = await invoke('get_sync_status')
    applySyncStatus(status)
  } catch (err) {
    _log(`Sync status error: ${_formatError(err)}`)
  }
}

/** Apply a SyncStatus payload to the sync card and status bar. */
export function applySyncStatus(status: SyncStatus): void {
  document.getElementById('sync-pending')!.textContent = String(status.pending)
  const lastSyncText = status.last_sync ? new Date(status.last_sync).toLocaleString() : 'Never'
  document.getElementById('sync-last')!.textContent = lastSyncText

  const errRow = document.getElementById('sync-error-row')!
  const errEl = document.getElementById('sync-error')!
  if (status.last_error) {
    errRow.style.display = ''
    errEl.textContent = status.last_error
  } else {
    errRow.style.display = 'none'
  }

  // Status bar
  updateStatusBarSync(status.pending, lastSyncText)
}

// ─── Manual sync trigger ──────────────────────────────────────────────────────

export async function syncNow(): Promise<void> {
  _log('Manual sync triggered…')
  try {
    const status: SyncStatus = await invoke('sync_now')
    _log(`Sync complete — ${status.pending} pending`)
    await refreshSync()
  } catch (err) {
    _log(`Sync error: ${_formatError(err)}`)
  }
}

// ─── Tauri event listener ────────────────────────────────────────────────────

/** Subscribe to sync-status-changed Tauri events. Call from shell event setup. */
export function initSyncStatusEvents(): void {
  void listen<SyncStatus>('sync-status-changed', (event) => {
    applySyncStatus(event.payload)
    void _refreshWsjtxDecodes()
  })
}