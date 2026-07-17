/**
 * Pure display-helper functions for the sync dashboard.
 *
 * All functions here are stateless and free of Vue reactivity.
 * They can be imported by SyncPage, SyncServiceCard, SyncHistoryDialog,
 * and any unit test without a component context.
 */

import type { ServiceStatus, SyncStatusRow } from 'src/types/sync'

// ── Service catalogue ──────────────────────────────────────────

/**
 * The canonical list of external sync services the app supports.
 * Update this when a new service is implemented end-to-end.
 */
export const KNOWN_SERVICES = ['eqsl', 'clublog', 'qrz', 'lotw'] as const
export type KnownService = (typeof KNOWN_SERVICES)[number]

// ── Display labels ────────────────────────────────────────────

/**
 * Human-readable label for a service key.
 * Falls back to the key uppercased so unknown services degrade gracefully.
 */
export function serviceLabel(svc: string): string {
  const labels: Record<string, string> = {
    eqsl: 'eQSL',
    clublog: 'Club Log',
    qrz: 'QRZ',
    lotw: 'LoTW',
    pota: 'POTA',
    sota: 'SOTA',
  }
  return labels[svc] ?? svc.toUpperCase()
}

/**
 * Material icon name for a service.
 * Falls back to 'sync' for unknown services.
 */
export function serviceIcon(service: string): string {
  const icons: Record<string, string> = {
    eqsl: 'mail',
    clublog: 'leaderboard',
    qrz: 'book',
    lotw: 'verified_user',
    pota: 'park',
    sota: 'terrain',
  }
  return icons[service] ?? 'sync'
}

// ── Health / status colors ────────────────────────────────────

/**
 * Quasar color token for a service health status string.
 * Used for badge colors on service cards.
 */
export function healthColor(status?: string): string {
  switch (status) {
    case 'ok':
    case 'connected':
      return 'positive'
    case 'rate_limited':
    case 'rate-limited':
      return 'warning'
    case 'circuit_open':
    case 'circuit-open':
    case 'error':
      return 'negative'
    default:
      return 'grey-6'
  }
}

/**
 * Material icon name for a per-QSO sync status value.
 * 'not_configured' → hollow circle (never been queued for this service).
 */
export function statusIcon(status: string): string {
  if (status === 'confirmed' || status === 'uploaded') return 'check_circle'
  if (status === 'pending') return 'schedule'
  if (status === 'error') return 'error'
  return 'radio_button_unchecked'
}

/**
 * Quasar color token for a per-QSO sync status value.
 */
export function statusColor(status: string): string {
  if (status === 'confirmed' || status === 'uploaded') return 'positive'
  if (status === 'pending') return 'warning'
  if (status === 'error') return 'negative'
  return 'grey-6'
}

// ── Row-level helpers ─────────────────────────────────────────

/**
 * Return the sync status string for a specific service on a QSO row.
 * Returns 'not_configured' when the service has never processed this QSO.
 */
export function statusFor(row: SyncStatusRow, service: string): string {
  const found = row.service_statuses?.find((s: ServiceStatus) => s.service === service)
  return found?.status || 'not_configured'
}

/**
 * True when a QSO row has any unfinished sync work.
 *
 * A row "needs sync" if:
 * - it has no service_statuses at all (hasn't been queued yet), OR
 * - at least one service is in pending/error/dirty/failed state.
 */
export function needsSync(row: SyncStatusRow): boolean {
  const statuses: ServiceStatus[] = Array.isArray(row.service_statuses) ? row.service_statuses : []
  if (statuses.length === 0) return true
  return statuses.some(
    (s) => s.status === 'pending' || s.status === 'error' || s.status === 'dirty' || s.status === 'failed',
  )
}

/**
 * Extract the first error message from a row's service_statuses.
 * Returns '—' when there are no errors (table cell placeholder).
 */
export function firstErrorMessage(row: SyncStatusRow): string {
  const statuses: ServiceStatus[] = Array.isArray(row.service_statuses) ? row.service_statuses : []
  const failed = statuses.find((s) => s.status === 'error' || s.status === 'failed')
  return failed?.error_message || '—'
}

/**
 * True when at least one service on the row is in an error/failed state.
 * Used to apply the red row highlight class.
 */
export function rowHasFailure(row: SyncStatusRow): boolean {
  const statuses: ServiceStatus[] = Array.isArray(row.service_statuses) ? row.service_statuses : []
  return statuses.some((s) => s.status === 'error' || s.status === 'failed')
}

// ── Formatting ────────────────────────────────────────────────

/**
 * Locale-formatted date/time string.
 * Returns '—' for empty/null inputs.
 */
export function formatDateTime(ts?: string): string {
  if (!ts) return '—'
  return new Date(ts).toLocaleString()
}

/**
 * Locale-formatted integer with thousands separators.
 * Treats null/undefined as 0.
 */
export function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value || 0)
}

// ── Conflict resolution ──────────────────────────────────────

/**
 * Build the default field-resolution map for a conflict record.
 * Every conflicting field defaults to service_a as the winner.
 *
 * Extracted here so it can be unit-tested and reused by SyncConflictDialog
 * on every open, including re-opens of the same conflict after canceling.
 */
export function buildDefaultResolution(
  fieldConflicts: Record<string, unknown>,
  serviceA: string,
): Record<string, string> {
  const r: Record<string, string> = {}
  for (const field of Object.keys(fieldConflicts)) {
    r[field] = serviceA
  }
  return r
}

/**
 * Stringify an arbitrary conflict field value for display.
 * Handles null/undefined, strings, and anything JSON-serialisable.
 */
export function stringifyValue(v: unknown): string {
  if (v == null) return '—'
  if (typeof v === 'string') return v
  return JSON.stringify(v)
}
