import { describe, expect, it } from 'vitest'
import {
  buildDefaultResolution,
  firstErrorMessage,
  needsSync,
  rowHasFailure,
  serviceIcon,
  serviceLabel,
  statusColor,
  statusFor,
  statusIcon,
} from './syncHelpers'
import type { SyncStatusRow } from 'src/types/sync'

function row(overrides: Partial<SyncStatusRow> = {}): SyncStatusRow {
  return {
    qso_uuid: 'qso-1',
    callsign: 'W1AW',
    band: '20m',
    mode: 'FT8',
    datetime_on: '2026-04-10T15:00:00Z',
    has_conflict: false,
    service_statuses: [],
    ...overrides,
  }
}

describe('syncHelpers', () => {
  it('returns user-facing service labels and sensible fallbacks', () => {
    expect(serviceLabel('eqsl')).toBe('eQSL')
    expect(serviceLabel('clublog')).toBe('Club Log')
    expect(serviceLabel('mystery')).toBe('MYSTERY')
  })

  it('maps services and statuses to stable icons/colors', () => {
    expect(serviceIcon('lotw')).toBe('verified_user')
    expect(serviceIcon('unknown')).toBe('sync')
    expect(statusIcon('uploaded')).toBe('check_circle')
    expect(statusIcon('pending')).toBe('schedule')
    expect(statusColor('error')).toBe('negative')
    expect(statusColor('not_configured')).toBe('grey-6')
  })

  it('finds the status for a specific service and falls back when missing', () => {
    const syncRow = row({
      service_statuses: [
        { service: 'eqsl', status: 'uploaded' },
        { service: 'qrz', status: 'pending' },
      ],
    })

    expect(statusFor(syncRow, 'eqsl')).toBe('uploaded')
    expect(statusFor(syncRow, 'clublog')).toBe('not_configured')
  })

  it('treats rows with no statuses or unfinished statuses as needing sync', () => {
    expect(needsSync(row())).toBe(true)
    expect(needsSync(row({ service_statuses: [{ service: 'eqsl', status: 'pending' }] }))).toBe(true)
    expect(needsSync(row({ service_statuses: [{ service: 'eqsl', status: 'error' }] }))).toBe(true)
    expect(needsSync(row({ service_statuses: [{ service: 'eqsl', status: 'uploaded' }] }))).toBe(false)
  })

  it('extracts the first error message and failure highlight state', () => {
    const failedRow = row({
      service_statuses: [
        { service: 'eqsl', status: 'uploaded' },
        { service: 'qrz', status: 'error', error_message: 'Auth failed' },
      ],
    })

    expect(firstErrorMessage(failedRow)).toBe('Auth failed')
    expect(rowHasFailure(failedRow)).toBe(true)
    expect(firstErrorMessage(row({ service_statuses: [{ service: 'eqsl', status: 'uploaded' }] }))).toBe('—')
    expect(rowHasFailure(row({ service_statuses: [{ service: 'eqsl', status: 'uploaded' }] }))).toBe(false)
  })
})

describe('buildDefaultResolution', () => {
  it('defaults every conflicting field to service_a', () => {
    const r = buildDefaultResolution(
      { callsign: { eqsl: 'W1AW', qrz: 'W1AW/P' }, band: { eqsl: '20m', qrz: '40m' } },
      'eqsl',
    )
    expect(r).toEqual({ callsign: 'eqsl', band: 'eqsl' })
  })

  it('returns an empty object when there are no conflicting fields', () => {
    expect(buildDefaultResolution({}, 'eqsl')).toEqual({})
  })

  it('uses whichever service is passed as service_a', () => {
    const r = buildDefaultResolution({ mode: { eqsl: 'FT8', qrz: 'FT4' } }, 'qrz')
    expect(r).toEqual({ mode: 'qrz' })
  })

  /**
   * Regression guard: SyncConflictDialog must re-build the resolution map on
   * EVERY open (not just when the conflict prop changes by identity).  This
   * ensures that if a user cancels, changes one radio button mentally, and
   * re-opens the same conflict, they see the fresh defaults instead of their
   * abandoned partial selection.
   *
   * The fix in SyncConflictDialog.vue adds a second watch on `modelValue`
   * (open → true) that calls buildDefaultResolution afresh.  These tests
   * verify that buildDefaultResolution always produces a clean slate.
   */
  it('produces a clean slate on every call — mutating the result does not affect a subsequent call', () => {
    const fields = { callsign: { eqsl: 'W1AW', qrz: 'W1AW/P' } }
    const first = buildDefaultResolution(fields, 'eqsl')
    first.callsign = 'qrz'  // simulate user changing selection

    const second = buildDefaultResolution(fields, 'eqsl')
    expect(second.callsign).toBe('eqsl')  // second call is unaffected
  })

  it('handles multi-field conflicts correctly', () => {
    const fields = {
      callsign: { eqsl: 'W1AW', clublog: 'W1AW/M' },
      band: { eqsl: '20m', clublog: '40m' },
      mode: { eqsl: 'FT8', clublog: 'SSB' },
    }
    const r = buildDefaultResolution(fields, 'eqsl')
    expect(Object.keys(r)).toHaveLength(3)
    expect(Object.values(r).every((v) => v === 'eqsl')).toBe(true)
  })
})
