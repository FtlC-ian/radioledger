/**
 * Shared types for the sync dashboard and related components.
 *
 * These types map the API response shapes returned by /v1/sync/* endpoints.
 * They are shared across SyncPage, SyncHistoryDialog, SyncConflictDialog,
 * and the useSyncPolling composable.
 */

/** Per-service sync status for a single QSO row, as returned inside SyncStatusRow. */
export type ServiceStatus = {
  service: string
  status: string
  error_message?: string
}

/**
 * Aggregate sync progress for a single external service.
 * Returned as part of the /v1/sync/status services map.
 */
export type SyncProgress = {
  pending_count: number
  uploaded_count: number
  failed_count: number
  /** Total QSOs in scope for the current/most-recent sync run. May be absent. */
  total_count?: number
  last_activity_at?: string
  last_error?: string
  error_message?: string
  /** True when auth or service-level failure is unrecoverable without re-configuring credentials. */
  has_permanent_error?: boolean
  /** True while the River worker is actively processing this service's queue. */
  is_running: boolean
  /**
   * True when the job started running but has had no activity for >60 s
   * with no worker seen alive. Indicates a worker crash or restart.
   */
  is_stalled: boolean
}

/**
 * A single row in the QSO sync status table.
 * service_statuses lists one entry per service that has ever processed this QSO.
 */
export type SyncStatusRow = {
  qso_uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
  has_conflict: boolean
  conflict_id?: number | null
  service_statuses: ServiceStatus[]
}

/** One history record — a single sync event for a specific QSO and service. */
export type SyncHistoryItem = {
  id: number
  qso_uuid: string
  service: string
  status: string
  datetime_on?: string
  error?: string
  retry_count: number
}

/** Field-level conflict values keyed by service name. */
export type ConflictFieldValues = Record<string, unknown>

/**
 * A sync conflict between two services for a QSO.
 * field_conflicts maps each conflicting field to { service_a_value, service_b_value }.
 */
export type SyncConflict = {
  id: number
  qso_uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
  service_a: string
  service_b: string
  field_conflicts: Record<string, ConflictFieldValues>
  status: string
  created_at: string
  updated_at: string
}

// ── API response envelopes ─────────────────────────────────────

export type SyncStatusResponse = {
  items?: SyncStatusRow[]
  pagination?: {
    total?: number
  }
  services?: Record<string, SyncProgress>
}

export type SyncTableFilters = {
  callsign: string
  service: string
  status: string
  dateFrom: string
  dateTo: string
}

export type SyncTablePagination = {
  page: number
  rowsPerPage: number
  rowsNumber: number
}

export type SyncHistoryResponse = {
  items?: SyncHistoryItem[]
}

export type SyncConflictsResponse = {
  items?: SyncConflict[]
}
