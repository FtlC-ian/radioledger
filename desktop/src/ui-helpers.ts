/**
 * UI Helpers — shared utility functions for the desktop UI layer.
 *
 * Extracted from main.ts as part of the desktop decomposition (issue #194).
 * Centralises formatError, escapeHtml, setDotClass, updateStatusBarServer,
 * and log so every module can import them directly instead of receiving
 * injected callbacks or duplicating local copies.
 */

// ─── Error formatting ─────────────────────────────────────────────────────────

/**
 * Convert an unknown catch value to a human-readable string.
 * Tauri command errors are objects (not strings), so bare `${err}` produces
 * "[object Object]". This helper extracts the real message.
 */
export function formatError(err: unknown): string {
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message
  if (err && typeof err === 'object') {
    const obj = err as Record<string, unknown>
    if (typeof obj.message === 'string') return obj.message
    if (typeof obj.error === 'string') return obj.error
    return JSON.stringify(err)
  }
  return String(err)
}

// ─── HTML escaping ─────────────────────────────────────────────────────────────

/** Escape &, <, > for safe interpolation into innerHTML. */
export function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// ─── Status bar dot helper ────────────────────────────────────────────────────

/** Set the CSS class on a status-bar indicator dot by element ID. */
export function setDotClass(dotId: string, cls: 'ok' | 'err' | 'warn' | ''): void {
  const dot = document.getElementById(dotId)
  if (!dot) return
  dot.className = `status-dot${cls ? ' ' + cls : ''}`
}

// ─── Status bar server indicator ──────────────────────────────────────────────

/** Update the server-connected indicator in the persistent status bar. */
export function updateStatusBarServer(connected: boolean): void {
  setDotClass('statusbar-server-dot', connected ? 'ok' : 'err')
  const textEl = document.getElementById('statusbar-server-text')
  if (textEl) textEl.textContent = connected ? 'Connected' : 'Disconnected'
}

// ─── Activity log ─────────────────────────────────────────────────────────────

/** Append a timestamped message to the on-screen activity log. */
export function log(msg: string): void {
  const out = document.getElementById('log-output')
  if (!out) return
  const ts = new Date().toTimeString().slice(0, 8)
  const line = document.createElement('div')
  line.className = 'log-line'
  line.innerHTML = `<span class="ts">${ts}</span>${escapeHtml(msg)}`
  out.appendChild(line)
  while (out.children.length > 100) out.removeChild(out.firstChild!)
  out.scrollTop = out.scrollHeight
}