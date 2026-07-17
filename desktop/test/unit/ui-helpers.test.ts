/**
 * Unit tests for ui-helpers.ts
 *
 * Tests the shared UI helper functions extracted from main.ts.
 */
import { describe, it, expect, beforeEach } from 'vitest'
import {
  formatError,
  escapeHtml,
  setDotClass,
  updateStatusBarServer,
  log,
} from '../../src/ui-helpers'

// ─── formatError ──────────────────────────────────────────────────────────────

describe('formatError', () => {
  it('formats string errors', () => {
    expect(formatError('something went wrong')).toBe('something went wrong')
  })

  it('formats Error objects', () => {
    expect(formatError(new Error('network failure'))).toBe('network failure')
  })

  it('formats objects with a message property', () => {
    expect(formatError({ message: 'server error' })).toBe('server error')
  })

  it('formats objects with an error property', () => {
    expect(formatError({ error: 'bad request' })).toBe('bad request')
  })

  it('formats other objects via JSON.stringify', () => {
    expect(formatError({ code: 42 })).toBe('{"code":42}')
  })

  it('formats null/undefined via String()', () => {
    expect(formatError(null)).toBe('null')
    expect(formatError(undefined)).toBe('undefined')
  })
})

// ─── escapeHtml ───────────────────────────────────────────────────────────────

describe('escapeHtml', () => {
  it('escapes ampersands', () => {
    expect(escapeHtml('a & b')).toBe('a &amp; b')
  })

  it('escapes angle brackets', () => {
    expect(escapeHtml('<script>alert("xss")</script>')).toBe(
      '&lt;script&gt;alert("xss")&lt;/script&gt;'
    )
  })

  it('returns plain strings unchanged', () => {
    expect(escapeHtml('hello world')).toBe('hello world')
  })
})

// ─── setDotClass ──────────────────────────────────────────────────────────────

describe('setDotClass', () => {
  beforeEach(() => {
    document.body.innerHTML = '<div id="test-dot" class="status-dot"></div>'
  })

  it('sets the dot class to ok', () => {
    setDotClass('test-dot', 'ok')
    expect(document.getElementById('test-dot')!.className).toBe('status-dot ok')
  })

  it('sets the dot class to err', () => {
    setDotClass('test-dot', 'err')
    expect(document.getElementById('test-dot')!.className).toBe('status-dot err')
  })

  it('sets the dot class to warn', () => {
    setDotClass('test-dot', 'warn')
    expect(document.getElementById('test-dot')!.className).toBe('status-dot warn')
  })

  it('clears extra class when cls is empty', () => {
    const dot = document.getElementById('test-dot')!
    dot.className = 'status-dot err'
    setDotClass('test-dot', '')
    expect(dot.className).toBe('status-dot')
  })

  it('does nothing if element does not exist', () => {
    expect(() => setDotClass('nonexistent', 'ok')).not.toThrow()
  })
})

// ─── updateStatusBarServer ────────────────────────────────────────────────────

describe('updateStatusBarServer', () => {
  beforeEach(() => {
    document.body.innerHTML = `
      <div id="statusbar-server-dot" class="status-dot"></div>
      <span id="statusbar-server-text">Disconnected</span>
    `
  })

  it('shows connected state', () => {
    updateStatusBarServer(true)
    expect(document.getElementById('statusbar-server-dot')!.className).toBe('status-dot ok')
    expect(document.getElementById('statusbar-server-text')!.textContent).toBe('Connected')
  })

  it('shows disconnected state', () => {
    updateStatusBarServer(false)
    expect(document.getElementById('statusbar-server-dot')!.className).toBe('status-dot err')
    expect(document.getElementById('statusbar-server-text')!.textContent).toBe('Disconnected')
  })
})

// ─── log ───────────────────────────────────────────────────────────────────────

describe('log', () => {
  it('appends a message to the log output', () => {
    document.body.innerHTML = '<div id="log-output"></div>'
    log('Test message')
    const logEl = document.getElementById('log-output')!
    expect(logEl.children.length).toBe(1)
    expect(logEl.children[0].textContent).toContain('Test message')
  })

  it('escapes HTML in log messages', () => {
    document.body.innerHTML = '<div id="log-output"></div>'
    log('<script>alert("xss")</script>')
    const logEl = document.getElementById('log-output')!
    expect(logEl.innerHTML).not.toContain('<script>')
  })
})