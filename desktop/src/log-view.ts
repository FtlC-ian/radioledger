/**
 * Log View — logbook table, column preferences, pagination, filters,
 * edit modal, and hydrate/save flows.
 *
 * Extracted from main.ts to isolate the log-view responsibility cluster
 * and reduce the god-file.  All DOM interaction and Tauri invoke calls
 * related to rendering the logbook table, column preferences, sorting,
 * pagination, filtering, QSO editing, and callsign hydration live here.
 *
 * The module is initialised via `initLogColumns()`. The host shell injects
 * callbacks for post-save actions (refresh sync).
 */

import { invoke } from '@tauri-apps/api/core'
import { formatError as _formatError, escapeHtml as _escapeHtml } from './ui-helpers'
import {
  toLocalDateTimeInputValue,
  type CallsignLookupResult,
} from './qso-form'

// ─── Types ────────────────────────────────────────────────────────────────────

export interface CachedQso {
  id: number
  uuid: string
  logbook_uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
  rst_sent?: string
  rst_rcvd?: string
  name?: string
  qth?: string
  gridsquare?: string
  dxcc?: number
  country?: string
  cq_zone?: number
  itu_zone?: number
  continent?: string
  comment?: string
  notes?: string
  created_at: string
  updated_at: string
  synced_at: string
}

export interface UpdateQsoRequest {
  uuid: string
  logbook_uuid: string
  callsign: string
  band: string
  mode: string
  datetime_on: string
  rst_sent?: string | null
  rst_rcvd?: string | null
  name?: string | null
  qth?: string | null
  gridsquare?: string | null
  comment?: string | null
  notes?: string | null
}

export type LogColumnKey =
  | 'datetime_on'
  | 'callsign'
  | 'band'
  | 'mode'
  | 'rst_sent'
  | 'rst_rcvd'
  | 'name'
  | 'qth'
  | 'country'
  | 'notes'
  | 'dxcc'
  | 'cq_zone'
  | 'itu_zone'
  | 'gridsquare'
  | 'continent'

export interface LogColumnDefinition {
  key: LogColumnKey
  label: string
  defaultVisible: boolean
  sortable?: boolean
  format: (qso: CachedQso) => string
  sortValue: (qso: CachedQso) => string | number
}

// ─── Column definitions ───────────────────────────────────────────────────────

export const LOG_COLUMNS: LogColumnDefinition[] = [
  {
    key: 'datetime_on',
    label: 'Date/Time',
    defaultVisible: true,
    sortable: true,
    format: (qso) => new Date(qso.datetime_on).toLocaleString(),
    sortValue: (qso) => new Date(qso.datetime_on).getTime(),
  },
  { key: 'callsign', label: 'Callsign', defaultVisible: true, sortable: true, format: (qso) => qso.callsign, sortValue: (qso) => qso.callsign },
  { key: 'band', label: 'Band', defaultVisible: true, sortable: true, format: (qso) => qso.band || '—', sortValue: (qso) => qso.band || '' },
  { key: 'mode', label: 'Mode', defaultVisible: true, sortable: true, format: (qso) => qso.mode || '—', sortValue: (qso) => qso.mode || '' },
  { key: 'rst_sent', label: 'RST Sent', defaultVisible: true, sortable: true, format: (qso) => qso.rst_sent || '—', sortValue: (qso) => qso.rst_sent || '' },
  { key: 'rst_rcvd', label: 'RST Rcvd', defaultVisible: true, sortable: true, format: (qso) => qso.rst_rcvd || '—', sortValue: (qso) => qso.rst_rcvd || '' },
  { key: 'name', label: 'Name', defaultVisible: true, sortable: true, format: (qso) => qso.name || '—', sortValue: (qso) => qso.name || '' },
  { key: 'qth', label: 'QTH', defaultVisible: true, sortable: true, format: (qso) => qso.qth || '—', sortValue: (qso) => qso.qth || '' },
  { key: 'country', label: 'Country', defaultVisible: true, sortable: true, format: (qso) => qso.country || '—', sortValue: (qso) => qso.country || '' },
  {
    key: 'notes',
    label: 'Notes',
    defaultVisible: true,
    sortable: true,
    format: (qso) => qso.notes || qso.comment || '—',
    sortValue: (qso) => qso.notes || qso.comment || '',
  },
  { key: 'dxcc', label: 'DXCC #', defaultVisible: false, sortable: true, format: (qso) => qso.dxcc != null ? String(qso.dxcc) : '—', sortValue: (qso) => qso.dxcc ?? -1 },
  { key: 'cq_zone', label: 'CQ Zone', defaultVisible: false, sortable: true, format: (qso) => qso.cq_zone != null ? String(qso.cq_zone) : '—', sortValue: (qso) => qso.cq_zone ?? -1 },
  { key: 'itu_zone', label: 'ITU Zone', defaultVisible: false, sortable: true, format: (qso) => qso.itu_zone != null ? String(qso.itu_zone) : '—', sortValue: (qso) => qso.itu_zone ?? -1 },
  { key: 'gridsquare', label: 'Grid Square', defaultVisible: false, sortable: true, format: (qso) => qso.gridsquare || '—', sortValue: (qso) => qso.gridsquare || '' },
  { key: 'continent', label: 'Continent', defaultVisible: false, sortable: true, format: (qso) => qso.continent || '—', sortValue: (qso) => qso.continent || '' },
]

export const DEFAULT_VISIBLE_LOG_COLUMNS = LOG_COLUMNS.filter((column) => column.defaultVisible).map((column) => column.key)

// ─── Module state ─────────────────────────────────────────────────────────────

let logQsos: CachedQso[] = []
let logTotalCount = 0
let logCurrentPage = 1
const logPageSize = 50
let logSortField: LogColumnKey = 'datetime_on'
let logSortDir: 'asc' | 'desc' = 'desc'
let logFilters = { callsign: '', band: '', mode: '' }
let visibleLogColumns: LogColumnKey[] = [...DEFAULT_VISIBLE_LOG_COLUMNS]
let logColumnMenuOpen = false
let logEditingQsoUuid: string | null = null

// ─── Injected dependencies ────────────────────────────────────────────────────

/** Logger function — injected from the shell bootstrap so messages reach the activity log. */
let _log: (msg: string) => void = () => {}

/** Post-save callback — injected from the shell bootstrap to trigger sync refresh. */
let _onAfterSave: () => Promise<void> = async () => {}

/**
 * Inject the logger used for activity messages.
 * Must be called before other functions to ensure log output is routed correctly.
 */
export function setLogger(logger: (msg: string) => void): void {
  _log = logger
}

/**
 * Inject the post-save callback.
 * Called after a QSO update is saved so the host can refresh sync state.
 */
export function setOnAfterSave(fn: () => Promise<void>): void {
  _onAfterSave = fn
}

// ─── Column initialisation & preferences ──────────────────────────────────────

export async function initLogColumns(): Promise<void> {
  try {
    const response: { visible_columns?: string[] } = await invoke('get_logbook_columns')
    const requested = Array.isArray(response.visible_columns) ? response.visible_columns : []
    const validColumns = requested.filter((column): column is LogColumnKey =>
      LOG_COLUMNS.some((candidate) => candidate.key === column as LogColumnKey),
    )
    if (validColumns.length > 0) {
      visibleLogColumns = validColumns
    }
  } catch {
    visibleLogColumns = [...DEFAULT_VISIBLE_LOG_COLUMNS]
  }

  ensureAtLeastOneVisibleLogColumn()
  renderLogColumnPicker()
  renderLogTable(getSortedLogQsos())
}

function ensureAtLeastOneVisibleLogColumn(): void {
  if (visibleLogColumns.length === 0) {
    visibleLogColumns = [...DEFAULT_VISIBLE_LOG_COLUMNS]
  }
}

export function getLogColumnDefinition(key: LogColumnKey): LogColumnDefinition {
  const column = LOG_COLUMNS.find((candidate) => candidate.key === key)
  if (!column) throw new Error(`Unknown log column: ${key}`)
  return column
}

export function getVisibleLogColumns(): LogColumnDefinition[] {
  return visibleLogColumns.map(getLogColumnDefinition)
}

export function getSortedLogQsos(): CachedQso[] {
  const column = getLogColumnDefinition(logSortField)
  const sorted = [...logQsos]
  sorted.sort((a, b) => compareLogSortValues(column.sortValue(a), column.sortValue(b), logSortDir))
  return sorted
}

export function compareLogSortValues(a: string | number, b: string | number, dir: 'asc' | 'desc'): number {
  const factor = dir === 'asc' ? 1 : -1
  if (typeof a === 'number' && typeof b === 'number') {
    return (a - b) * factor
  }

  const left = String(a).toLocaleLowerCase()
  const right = String(b).toLocaleLowerCase()
  if (left < right) return -1 * factor
  if (left > right) return 1 * factor
  return 0
}

function setLogColumnMenuOpen(open: boolean): void {
  logColumnMenuOpen = open
  const menu = document.getElementById('log-columns-menu')
  const button = document.getElementById('log-columns-toggle')
  if (menu) menu.hidden = !open
  if (button) button.setAttribute('aria-expanded', open ? 'true' : 'false')
}

export function toggleLogColumnMenu(): void {
  setLogColumnMenuOpen(!logColumnMenuOpen)
}

function renderLogColumnPicker(): void {
  const list = document.getElementById('log-columns-list')
  if (!list) return

  list.innerHTML = ''
  for (const column of LOG_COLUMNS) {
    const item = document.createElement('label')
    item.className = 'log-columns-item'

    const checkbox = document.createElement('input')
    checkbox.type = 'checkbox'
    checkbox.checked = visibleLogColumns.includes(column.key)
    checkbox.addEventListener('change', () => {
      if (checkbox.checked) {
        if (!visibleLogColumns.includes(column.key)) {
          visibleLogColumns = [...visibleLogColumns, column.key]
        }
      } else {
        visibleLogColumns = visibleLogColumns.filter((key) => key !== column.key)
        ensureAtLeastOneVisibleLogColumn()
        if (!visibleLogColumns.includes(column.key)) {
          checkbox.checked = false
        }
      }

      if (!visibleLogColumns.includes(logSortField)) {
        logSortField = 'datetime_on'
        logSortDir = 'desc'
      }

      void saveLogColumnPreferences()
      renderLogColumnPicker()
      renderLogTable(getSortedLogQsos())
    })

    const text = document.createElement('span')
    text.textContent = column.label

    item.appendChild(checkbox)
    item.appendChild(text)
    list.appendChild(item)
  }
}

async function saveLogColumnPreferences(): Promise<void> {
  ensureAtLeastOneVisibleLogColumn()
  try {
    await invoke('save_logbook_columns', {
      request: {
        visible_columns: visibleLogColumns,
      },
    })
  } catch (err) {
    _log(`Failed to save logbook columns: ${_formatError(err)}`)
  }
}

// ─── QSO loading & table rendering ───────────────────────────────────────────

export async function loadLogQsos(): Promise<void> {
  try {
    const offset = (logCurrentPage - 1) * logPageSize

    // Pass filters to the backend for SQLite-level filtering.
    const qsos: CachedQso[] = await invoke('list_qsos', {
      limit: logPageSize,
      offset,
      callsign: logFilters.callsign || null,
      band: logFilters.band || null,
      mode: logFilters.mode || null,
    })

    const count: number = await invoke('count_qsos', {
      callsign: logFilters.callsign || null,
      band: logFilters.band || null,
      mode: logFilters.mode || null,
    })

    const totalCount: number = await invoke('count_qsos', {
      callsign: null,
      band: null,
      mode: null,
    })

    logQsos = qsos
    logTotalCount = count

    renderLogTable(getSortedLogQsos())
    updateLogStats(totalCount, count)
    updateLogPagination()

    const emptyState = document.getElementById('log-empty-state')
    const tableContainer = document.querySelector('.log-table-container') as HTMLElement | null
    if (count === 0) {
      if (emptyState) emptyState.style.display = 'block'
      if (tableContainer) tableContainer.style.display = 'none'
    } else {
      if (emptyState) emptyState.style.display = 'none'
      if (tableContainer) tableContainer.style.display = 'block'
    }
  } catch (err) {
    _log(`Failed to load logbook: ${_formatError(err)}`)
  }
}

export function renderLogTable(qsos: CachedQso[]): void {
  const table = document.getElementById('log-table') as HTMLTableElement | null
  if (!table) return

  table.innerHTML = ''

  const thead = document.createElement('thead')
  const headerRow = document.createElement('tr')
  for (const column of getVisibleLogColumns()) {
    const th = document.createElement('th')
    th.textContent = column.label
    if (column.sortable) {
      th.classList.add('sortable')
      th.setAttribute('data-sort', column.key)
      if (logSortField === column.key) {
        th.classList.add('active')
        th.textContent = `${column.label} ${logSortDir === 'asc' ? '▲' : '▼'}`
      }
      th.addEventListener('click', () => { handleLogSort(column.key) })
    }
    headerRow.appendChild(th)
  }
  thead.appendChild(headerRow)
  table.appendChild(thead)

  const tbody = document.createElement('tbody')
  for (const qso of qsos) {
    const row = document.createElement('tr')
    row.classList.add('log-row-clickable')
    row.addEventListener('click', () => { openLogEditModal(qso) })
    for (const column of getVisibleLogColumns()) {
      const cell = document.createElement('td')
      const formatted = _escapeHtml(column.format(qso))
      if (column.key === 'callsign') {
        cell.innerHTML = `<strong>${formatted}</strong>`
      } else {
        cell.innerHTML = formatted
      }
      row.appendChild(cell)
    }
    tbody.appendChild(row)
  }
  table.appendChild(tbody)
}

function updateLogStats(total: number, filtered: number): void {
  const totalEl = document.getElementById('log-total-count')
  if (totalEl) totalEl.textContent = `${total} QSOs`

  const filteredEl = document.getElementById('log-filtered-count')
  if (!filteredEl) return
  if (filtered < total) {
    filteredEl.textContent = `${filtered} filtered`
    filteredEl.style.display = ''
  } else {
    filteredEl.style.display = 'none'
  }
}

function updateLogPagination(): void {
  const totalPages = Math.max(1, Math.ceil(logTotalCount / logPageSize))
  const pageInfo = document.getElementById('log-page-info')
  if (pageInfo) pageInfo.textContent = `Page ${logCurrentPage} of ${totalPages}`

  const prevBtn = document.getElementById('log-prev-page') as HTMLButtonElement | null
  const nextBtn = document.getElementById('log-next-page') as HTMLButtonElement | null
  if (prevBtn) prevBtn.disabled = logCurrentPage === 1
  if (nextBtn) nextBtn.disabled = logCurrentPage >= totalPages
}

function handleLogSort(field: LogColumnKey): void {
  if (logSortField === field) {
    logSortDir = logSortDir === 'asc' ? 'desc' : 'asc'
  } else {
    logSortField = field
    logSortDir = field === 'datetime_on' ? 'desc' : 'asc'
  }

  renderLogTable(getSortedLogQsos())
}

// ─── Filters ─────────────────────────────────────────────────────────────────

export function applyLogFilters(): void {
  const callsignEl = document.getElementById('log-filter-callsign') as HTMLInputElement | null
  const bandEl = document.getElementById('log-filter-band') as HTMLSelectElement | null
  const modeEl = document.getElementById('log-filter-mode') as HTMLSelectElement | null

  logFilters.callsign = callsignEl?.value ?? ''
  logFilters.band = bandEl?.value ?? ''
  logFilters.mode = modeEl?.value ?? ''
  logCurrentPage = 1

  void loadLogQsos()
}

export function clearLogFilters(): void {
  const callsignEl = document.getElementById('log-filter-callsign') as HTMLInputElement | null
  const bandEl = document.getElementById('log-filter-band') as HTMLSelectElement | null
  const modeEl = document.getElementById('log-filter-mode') as HTMLSelectElement | null

  if (callsignEl) callsignEl.value = ''
  if (bandEl) bandEl.value = ''
  if (modeEl) modeEl.value = ''

  logFilters = { callsign: '', band: '', mode: '' }
  logCurrentPage = 1

  void loadLogQsos()
}

// ─── Pagination ───────────────────────────────────────────────────────────────

export function logPrevPage(): void {
  if (logCurrentPage > 1) {
    logCurrentPage--
    void loadLogQsos()
  }
}

export function logNextPage(): void {
  logCurrentPage++
  void loadLogQsos()
}

// ─── Edit modal ───────────────────────────────────────────────────────────────

function setLogEditStatus(message: string, isError = false): void {
  const el = document.getElementById('log-edit-status')
  if (!el) return
  el.textContent = message
  el.style.color = isError ? 'var(--danger, #f87171)' : 'var(--text-dim)'
}

export function openLogEditModal(qso: CachedQso): void {
  logEditingQsoUuid = qso.uuid

  ;(document.getElementById('log-edit-callsign') as HTMLInputElement | null)!.value = qso.callsign || ''
  ;(document.getElementById('log-edit-datetime') as HTMLInputElement | null)!.value = toLocalDateTimeInputValue(new Date(qso.datetime_on))
  ;(document.getElementById('log-edit-band') as HTMLSelectElement | null)!.value = qso.band || ''
  ;(document.getElementById('log-edit-mode') as HTMLSelectElement | null)!.value = qso.mode || ''
  ;(document.getElementById('log-edit-grid') as HTMLInputElement | null)!.value = qso.gridsquare || ''
  ;(document.getElementById('log-edit-rst-sent') as HTMLInputElement | null)!.value = qso.rst_sent || ''
  ;(document.getElementById('log-edit-rst-rcvd') as HTMLInputElement | null)!.value = qso.rst_rcvd || ''
  ;(document.getElementById('log-edit-name') as HTMLInputElement | null)!.value = qso.name || ''
  ;(document.getElementById('log-edit-qth') as HTMLInputElement | null)!.value = qso.qth || ''
  ;(document.getElementById('log-edit-comment') as HTMLTextAreaElement | null)!.value = qso.comment || ''
  ;(document.getElementById('log-edit-notes') as HTMLTextAreaElement | null)!.value = qso.notes || ''

  setLogEditStatus('')
  const overlay = document.getElementById('log-edit-overlay')
  if (overlay) overlay.style.display = 'flex'
}

export function closeLogEditModal(): void {
  logEditingQsoUuid = null
  const overlay = document.getElementById('log-edit-overlay')
  if (overlay) overlay.style.display = 'none'
  setLogEditStatus('')
}

export async function hydrateLogEditQso(): Promise<void> {
  const callsign = (document.getElementById('log-edit-callsign') as HTMLInputElement | null)?.value.trim().toUpperCase() || ''
  if (!callsign) {
    setLogEditStatus('Callsign required first', true)
    return
  }

  setLogEditStatus('Refreshing callsign data…')
  try {
    const result: CallsignLookupResult = await invoke('lookup_callsign', { callsign })
    const nameInput = document.getElementById('log-edit-name') as HTMLInputElement | null
    const qthInput = document.getElementById('log-edit-qth') as HTMLInputElement | null
    const gridInput = document.getElementById('log-edit-grid') as HTMLInputElement | null

    if (nameInput && result.full_name) nameInput.value = result.full_name
    const location = [result.state, result.country].filter(Boolean).join(', ')
    if (qthInput && location) qthInput.value = location
    if (gridInput && result.grid) gridInput.value = result.grid.toUpperCase()

    setLogEditStatus('Callsign data refreshed')
  } catch (err) {
    setLogEditStatus(`Lookup failed: ${_formatError(err)}`, true)
  }
}

export async function saveLogEditQso(): Promise<void> {
  const qso = logQsos.find((entry) => entry.uuid === logEditingQsoUuid)
  if (!qso) {
    setLogEditStatus('No QSO selected', true)
    return
  }

  const callsign = (document.getElementById('log-edit-callsign') as HTMLInputElement | null)?.value.trim().toUpperCase() || ''
  const datetimeVal = (document.getElementById('log-edit-datetime') as HTMLInputElement | null)?.value || ''
  const band = (document.getElementById('log-edit-band') as HTMLSelectElement | null)?.value || ''
  const mode = (document.getElementById('log-edit-mode') as HTMLSelectElement | null)?.value || ''
  const gridsquare = (document.getElementById('log-edit-grid') as HTMLInputElement | null)?.value.trim().toUpperCase() || ''
  const rstSent = (document.getElementById('log-edit-rst-sent') as HTMLInputElement | null)?.value.trim() || ''
  const rstRcvd = (document.getElementById('log-edit-rst-rcvd') as HTMLInputElement | null)?.value.trim() || ''
  const name = (document.getElementById('log-edit-name') as HTMLInputElement | null)?.value.trim() || ''
  const qth = (document.getElementById('log-edit-qth') as HTMLInputElement | null)?.value.trim() || ''
  const comment = (document.getElementById('log-edit-comment') as HTMLTextAreaElement | null)?.value.trim() || ''
  const notes = (document.getElementById('log-edit-notes') as HTMLTextAreaElement | null)?.value.trim() || ''

  if (!callsign || !datetimeVal || !band || !mode) {
    setLogEditStatus('Callsign, date/time, band, and mode are required', true)
    return
  }

  const request: UpdateQsoRequest = {
    uuid: qso.uuid,
    logbook_uuid: qso.logbook_uuid,
    callsign,
    band,
    mode,
    datetime_on: new Date(datetimeVal).toISOString(),
    rst_sent: rstSent || null,
    rst_rcvd: rstRcvd || null,
    name: name || null,
    qth: qth || null,
    gridsquare: gridsquare || null,
    comment: comment || null,
    notes: notes || null,
  }

  setLogEditStatus('Saving local edit and queueing sync…')
  try {
    await invoke('update_qso', { request })
    await _onAfterSave()
    await loadLogQsos()
    closeLogEditModal()
    _log(`QSO update queued: ${callsign}`)
  } catch (err) {
    setLogEditStatus(`Save failed: ${_formatError(err)}`, true)
  }
}

// ─── Column menu outside-click dismissal ─────────────────────────────────────

/** Returns true if the column menu is currently open. */
export function isLogColumnMenuOpen(): boolean {
  return logColumnMenuOpen
}

/** Close the column menu if it is open (for outside-click / Escape dismissal). */
export function closeLogColumnMenuIfOpen(): void {
  if (logColumnMenuOpen) {
    setLogColumnMenuOpen(false)
  }
}