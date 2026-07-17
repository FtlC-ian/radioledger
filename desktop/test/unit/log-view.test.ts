/**
 * Unit tests for log-view.ts
 *
 * Tests the extracted log-view module: column definitions, sorting,
 * preferences, QSO loading, pagination, filters, edit modal, and
 * hydrate/save flows.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock @tauri-apps/api/core before importing the module under test.
const mockInvoke = vi.fn()
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => mockInvoke(...args),
}))

// Mock qso-form (provides toLocalDateTimeInputValue and CallsignLookupResult).
vi.mock('../../src/qso-form', () => ({
  toLocalDateTimeInputValue: (date: Date) => {
    // Simple ISO-like datetime-local format for testing.
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
  },
  CallsignLookupResult: class {},
}))

let logView: typeof import('../../src/log-view')

beforeEach(async () => {
  vi.clearAllMocks()
  // Dynamic import to get fresh module state between tests.
  logView = await import('../../src/log-view')
  logView.setLogger(() => {})
  logView.setOnAfterSave(async () => {})
})

// ─── Column definitions ──────────────────────────────────────────────────────

describe('LOG_COLUMNS', () => {
  it('defines all expected column keys', () => {
    const keys = logView.LOG_COLUMNS.map((c) => c.key)
    expect(keys).toContain('datetime_on')
    expect(keys).toContain('callsign')
    expect(keys).toContain('band')
    expect(keys).toContain('mode')
    expect(keys).toContain('rst_sent')
    expect(keys).toContain('rst_rcvd')
    expect(keys).toContain('name')
    expect(keys).toContain('qth')
    expect(keys).toContain('country')
    expect(keys).toContain('notes')
    expect(keys).toContain('dxcc')
    expect(keys).toContain('cq_zone')
    expect(keys).toContain('itu_zone')
    expect(keys).toContain('gridsquare')
    expect(keys).toContain('continent')
  })

  it('has format and sortValue for every column', () => {
    for (const column of logView.LOG_COLUMNS) {
      expect(typeof column.format).toBe('function')
      expect(typeof column.sortValue).toBe('function')
    }
  })
})

describe('DEFAULT_VISIBLE_LOG_COLUMNS', () => {
  it('contains only valid column keys', () => {
    const allKeys = logView.LOG_COLUMNS.map((c) => c.key)
    for (const key of logView.DEFAULT_VISIBLE_LOG_COLUMNS) {
      expect(allKeys).toContain(key)
    }
  })

  it('has at least one column', () => {
    expect(logView.DEFAULT_VISIBLE_LOG_COLUMNS.length).toBeGreaterThan(0)
  })

  it('matches columns flagged defaultVisible', () => {
    const expected = logView.LOG_COLUMNS.filter((c) => c.defaultVisible).map((c) => c.key)
    expect(logView.DEFAULT_VISIBLE_LOG_COLUMNS).toEqual(expected)
  })
})

// ─── Sorting ──────────────────────────────────────────────────────────────────

describe('compareLogSortValues', () => {
  it('sorts numbers ascending', () => {
    expect(logView.compareLogSortValues(1, 2, 'asc')).toBeLessThan(0)
    expect(logView.compareLogSortValues(2, 1, 'asc')).toBeGreaterThan(0)
    expect(logView.compareLogSortValues(5, 5, 'asc')).toBe(0)
  })

  it('sorts numbers descending', () => {
    expect(logView.compareLogSortValues(1, 2, 'desc')).toBeGreaterThan(0)
    expect(logView.compareLogSortValues(2, 1, 'desc')).toBeLessThan(0)
  })

  it('sorts strings ascending (case-insensitive)', () => {
    expect(logView.compareLogSortValues('alpha', 'beta', 'asc')).toBeLessThan(0)
    expect(logView.compareLogSortValues('Beta', 'alpha', 'asc')).toBeGreaterThan(0)
  })

  it('sorts strings descending', () => {
    expect(logView.compareLogSortValues('alpha', 'beta', 'desc')).toBeGreaterThan(0)
  })
})

// ─── Column helper functions ─────────────────────────────────────────────────

describe('getLogColumnDefinition', () => {
  it('returns a definition for a valid key', () => {
    const def = logView.getLogColumnDefinition('callsign')
    expect(def.key).toBe('callsign')
    expect(def.label).toBe('Callsign')
  })

  it('throws for an unknown key', () => {
    expect(() => logView.getLogColumnDefinition('nonexistent' as any)).toThrow('Unknown log column')
  })
})

// ─── Module export surface ───────────────────────────────────────────────────

describe('module exports', () => {
  it('exports all required public functions', () => {
    const required = [
      'initLogColumns',
      'loadLogQsos',
      'applyLogFilters',
      'clearLogFilters',
      'toggleLogColumnMenu',
      'logPrevPage',
      'logNextPage',
      'closeLogEditModal',
      'saveLogEditQso',
      'hydrateLogEditQso',
      'isLogColumnMenuOpen',
      'closeLogColumnMenuIfOpen',
      'setLogger',
      'setOnAfterSave',
      'getSortedLogQsos',
      'getVisibleLogColumns',
      'compareLogSortValues',
      'getLogColumnDefinition',
    ]
    for (const name of required) {
      expect(typeof (logView as any)[name]).toBe('function')
    }
  })

  it('exports required types and constants', () => {
    expect(Array.isArray(logView.LOG_COLUMNS)).toBe(true)
    expect(Array.isArray(logView.DEFAULT_VISIBLE_LOG_COLUMNS)).toBe(true)
  })
})

// ─── Logger injection ────────────────────────────────────────────────────────

describe('setLogger', () => {
  it('routes log messages through the injected logger', async () => {
    const messages: string[] = []
    logView.setLogger((msg: string) => messages.push(msg))

    // Trigger a code path that calls _log — loadLogQsos failure.
    mockInvoke.mockRejectedValue(new Error('network error'))
    await logView.loadLogQsos()

    expect(messages.length).toBeGreaterThan(0)
    expect(messages[0]).toContain('network error')
  })
})

// ─── loadLogQsos ─────────────────────────────────────────────────────────────

describe('loadLogQsos', () => {
  it('invokes list_qsos and count_qsos with correct parameters', async () => {
    mockInvoke.mockImplementation((cmd: string, args?: any) => {
      if (cmd === 'list_qsos') return Promise.resolve([])
      if (cmd === 'count_qsos') return Promise.resolve(0)
      return Promise.resolve(null)
    })

    await logView.loadLogQsos()

    expect(mockInvoke).toHaveBeenCalledWith('list_qsos', expect.objectContaining({
      limit: 50,
      offset: 0,
      callsign: null,
      band: null,
      mode: null,
    }))

    // Two count_qsos calls: one filtered, one unfiltered.
    const countCalls = mockInvoke.mock.calls.filter((c: any[]) => c[0] === 'count_qsos')
    expect(countCalls.length).toBe(2)
  })

  it('passes non-empty filters to backend', async () => {
    // Apply filters first by calling applyLogFilters with DOM stubs
    const callsignEl = document.createElement('input')
    callsignEl.id = 'log-filter-callsign'
    callsignEl.value = 'W1AW'
    document.body.appendChild(callsignEl)

    const bandEl = document.createElement('select')
    bandEl.id = 'log-filter-band'
    bandEl.innerHTML = '<option value="20m">20m</option>'
    bandEl.value = '20m'
    document.body.appendChild(bandEl)

    const modeEl = document.createElement('select')
    modeEl.id = 'log-filter-mode'
    modeEl.innerHTML = '<option value="FT8">FT8</option>'
    modeEl.value = 'FT8'
    document.body.appendChild(modeEl)

    logView.applyLogFilters()

    mockInvoke.mockImplementation((cmd: string) => {
      if (cmd === 'list_qsos') return Promise.resolve([])
      if (cmd === 'count_qsos') return Promise.resolve(0)
      return Promise.resolve(null)
    })

    await logView.loadLogQsos()

    expect(mockInvoke).toHaveBeenCalledWith('list_qsos', expect.objectContaining({
      callsign: 'W1AW',
      band: '20m',
      mode: 'FT8',
    }))

    // Clean up
    callsignEl.remove()
    bandEl.remove()
    modeEl.remove()
  })
})

// ─── Edit modal ─────────────────────────────────────────────────────────────

describe('closeLogEditModal', () => {
  it('hides the edit overlay', () => {
    const overlay = document.createElement('div')
    overlay.id = 'log-edit-overlay'
    overlay.style.display = 'flex'
    document.body.appendChild(overlay)

    logView.closeLogEditModal()
    expect(overlay.style.display).toBe('none')

    overlay.remove()
  })

  it('clears edit status', () => {
    const statusEl = document.createElement('div')
    statusEl.id = 'log-edit-status'
    statusEl.textContent = 'some error'
    document.body.appendChild(statusEl)

    logView.closeLogEditModal()
    expect(statusEl.textContent).toBe('')

    statusEl.remove()
  })
})

// ─── saveLogEditQso ──────────────────────────────────────────────────────────

describe('saveLogEditQso', () => {
  it('shows error when no QSO is selected', async () => {
    const statusEl = document.createElement('div')
    statusEl.id = 'log-edit-status'
    document.body.appendChild(statusEl)

    await logView.saveLogEditQso()
    expect(statusEl.textContent).toContain('No QSO selected')

    statusEl.remove()
  })
})

// ─── Column menu ────────────────────────────────────────────────────────────

describe('isLogColumnMenuOpen / closeLogColumnMenuIfOpen', () => {
  it('reports closed by default', () => {
    expect(logView.isLogColumnMenuOpen()).toBe(false)
  })

  it('closeLogColumnMenuIfOpen is a no-op when already closed', () => {
    logView.closeLogColumnMenuIfOpen()
    expect(logView.isLogColumnMenuOpen()).toBe(false)
  })
})

// ─── Pagination helpers ─────────────────────────────────────────────────────

describe('logPrevPage / logNextPage', () => {
  it('logPrevPage does not go below page 1', async () => {
    // Page starts at 1; going prev should invoke loadLogQsos (but only if page > 1).
    mockInvoke.mockImplementation((cmd: string) => {
      if (cmd === 'list_qsos') return Promise.resolve([])
      if (cmd === 'count_qsos') return Promise.resolve(0)
      return Promise.resolve(null)
    })

    logView.logPrevPage()
    // Since we're on page 1, loadLogQsos should NOT have been called for prev
    const listCallsBefore = mockInvoke.mock.calls.filter((c: any[]) => c[0] === 'list_qsos').length
    expect(listCallsBefore).toBe(0)
  })

  it('logNextPage increments page and loads', async () => {
    mockInvoke.mockImplementation((cmd: string) => {
      if (cmd === 'list_qsos') return Promise.resolve([])
      if (cmd === 'count_qsos') return Promise.resolve(0)
      return Promise.resolve(null)
    })

    logView.logNextPage()
    // Should have triggered loadLogQsos
    await new Promise((r) => setTimeout(r, 50))

    const listCalls = mockInvoke.mock.calls.filter((c: any[]) => c[0] === 'list_qsos')
    expect(listCalls.length).toBeGreaterThan(0)
  })
})

// ─── clearLogFilters ────────────────────────────────────────────────────────

describe('clearLogFilters', () => {
  it('resets filter inputs and reloads', async () => {
    const callsignEl = document.createElement('input')
    callsignEl.id = 'log-filter-callsign'
    callsignEl.value = 'W1AW'
    document.body.appendChild(callsignEl)

    const bandEl = document.createElement('select')
    bandEl.id = 'log-filter-band'
    document.body.appendChild(bandEl)

    const modeEl = document.createElement('select')
    modeEl.id = 'log-filter-mode'
    document.body.appendChild(modeEl)

    mockInvoke.mockImplementation((cmd: string) => {
      if (cmd === 'list_qsos') return Promise.resolve([])
      if (cmd === 'count_qsos') return Promise.resolve(0)
      return Promise.resolve(null)
    })

    logView.clearLogFilters()
    await new Promise((r) => setTimeout(r, 50))

    expect(callsignEl.value).toBe('')

    callsignEl.remove()
    bandEl.remove()
    modeEl.remove()
  })
})

// ─── hydrateLogEditQso ──────────────────────────────────────────────────────

describe('hydrateLogEditQso', () => {
  it('shows error when callsign is empty', async () => {
    const callsignEl = document.createElement('input')
    callsignEl.id = 'log-edit-callsign'
    callsignEl.value = ''
    document.body.appendChild(callsignEl)

    const statusEl = document.createElement('div')
    statusEl.id = 'log-edit-status'
    document.body.appendChild(statusEl)

    await logView.hydrateLogEditQso()
    expect(statusEl.textContent).toContain('Callsign required')

    callsignEl.remove()
    statusEl.remove()
  })

  it('looks up callsign and fills form fields', async () => {
    const callsignEl = document.createElement('input')
    callsignEl.id = 'log-edit-callsign'
    callsignEl.value = 'W1AW'
    document.body.appendChild(callsignEl)

    const nameEl = document.createElement('input')
    nameEl.id = 'log-edit-name'
    document.body.appendChild(nameEl)

    const qthEl = document.createElement('input')
    qthEl.id = 'log-edit-qth'
    document.body.appendChild(qthEl)

    const gridEl = document.createElement('input')
    gridEl.id = 'log-edit-grid'
    document.body.appendChild(gridEl)

    const statusEl = document.createElement('div')
    statusEl.id = 'log-edit-status'
    document.body.appendChild(statusEl)

    mockInvoke.mockResolvedValueOnce({
      full_name: 'Hiram Percy Maxim',
      state: 'CT',
      country: 'USA',
      grid: 'FN31pr',
    })

    await logView.hydrateLogEditQso()

    expect(mockInvoke).toHaveBeenCalledWith('lookup_callsign', { callsign: 'W1AW' })
    expect(nameEl.value).toBe('Hiram Percy Maxim')
    expect(qthEl.value).toBe('CT, USA')
    expect(gridEl.value).toBe('FN31PR')

    callsignEl.remove()
    nameEl.remove()
    qthEl.remove()
    gridEl.remove()
    statusEl.remove()
  })

  it('shows error on lookup failure', async () => {
    const callsignEl = document.createElement('input')
    callsignEl.id = 'log-edit-callsign'
    callsignEl.value = 'INVALID'
    document.body.appendChild(callsignEl)

    const statusEl = document.createElement('div')
    statusEl.id = 'log-edit-status'
    document.body.appendChild(statusEl)

    mockInvoke.mockRejectedValueOnce(new Error('Not found'))

    await logView.hydrateLogEditQso()
    expect(statusEl.textContent).toContain('Lookup failed')

    callsignEl.remove()
    statusEl.remove()
  })
})