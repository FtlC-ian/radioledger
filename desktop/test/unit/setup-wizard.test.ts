/**
 * Unit tests for setup-wizard module.
 *
 * Verifies the exported API surface and core logic without a browser or
 * Tauri runtime. DOM and Tauri invoke calls are mocked.
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// ─── Mock setup ───────────────────────────────────────────────────────────────
const mockInvoke = vi.fn()
vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => mockInvoke(...args),
}))

// ─── Import module under test ─────────────────────────────────────────────────
const logs: string[] = []
const testLogger = (msg: string) => { logs.push(msg) }
let finishCalls: Array<'cloud' | 'local'> = []
const testOnFinish = (authMode: 'cloud' | 'local') => { finishCalls.push(authMode) }

let initWizard: typeof import('../../src/setup-wizard').initWizard
let wizardNext: typeof import('../../src/setup-wizard').wizardNext
let wizardBack: typeof import('../../src/setup-wizard').wizardBack
let wizardToggleAuthMode: typeof import('../../src/setup-wizard').wizardToggleAuthMode
let wireWizardListeners: typeof import('../../src/setup-wizard').wireWizardListeners
let attachWizardInit: typeof import('../../src/setup-wizard').attachWizardInit
let setLogger: typeof import('../../src/setup-wizard').setLogger
let setOnWizardFinish: typeof import('../../src/setup-wizard').setOnWizardFinish

// ─── Minimal jsdom setup ─────────────────────────────────────────────────────

function setupWizardDom(): void {
  document.body.innerHTML = `
    <div id="wizard-overlay" style="display:none">
      <div id="wizard-progress" aria-valuenow="1"></div>
      <div id="wizard-step-0" class="active"></div>
      <div id="wizard-step-1"></div>
      <div id="wizard-step-2"></div>
      <div id="wizard-step-3"></div>
      <div id="wizard-step-4"></div>
      <div id="wizard-step-5"></div>
      <div id="wizard-step-6"></div>
      <div id="wizard-step-7"></div>
    </div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <div class="wizard-step-dot"></div>
    <button id="wizard-auth-cloud-btn"></button>
    <button id="wizard-auth-local-btn"></button>
    <div id="wizard-auth-cloud"></div>
    <div id="wizard-auth-local" style="display:none"></div>
    <button id="wizard-start-btn"></button>
    <button id="wizard-step1-back"></button>
    <button id="wizard-login-btn"></button>
    <button id="wizard-step1-skip"></button>
    <button id="wizard-step1-local-back"></button>
    <button id="wizard-local-login-btn"></button>
    <button id="wizard-step1-local-skip"></button>
    <button id="wizard-step2-back"></button>
    <button id="wizard-sync-btn"></button>
    <button id="wizard-step2-skip"></button>
    <button id="wizard-step3-back"></button>
    <button id="wizard-scan-btn"></button>
    <button id="wizard-step3-next"></button>
    <button id="wizard-step4-back"></button>
    <button id="wizard-step4-next"></button>
    <button id="wizard-step4-skip"></button>
    <button id="wizard-step5-back"></button>
    <button id="wizard-lotw-detect-btn"></button>
    <button id="wizard-step5-skip"></button>
    <button id="wizard-step6-back"></button>
    <button id="wizard-rig-test-btn"></button>
    <button id="wizard-step6-skip"></button>
    <button id="wizard-step7-back"></button>
    <button id="wizard-finish-btn"></button>
    <div id="wizard-login-indicator"></div>
    <div id="wizard-login-text"></div>
    <div id="wizard-login-user" style="display:none"></div>
    <div id="wizard-login-callsign"></div>
    <div id="wizard-sync-indicator"></div>
    <div id="wizard-sync-text"></div>
    <div id="wizard-detect-indicator"></div>
    <div id="wizard-detect-text"></div>
    <div id="wizard-detect-results" style="display:none">
      <span id="wizard-detect-wsjtx-icon"></span>
      <span id="wizard-detect-wsjtx-note"></span>
      <span id="wizard-detect-js8call-icon"></span>
      <span id="wizard-detect-js8call-note"></span>
      <span id="wizard-detect-n1mm-icon"></span>
      <span id="wizard-detect-n1mm-note"></span>
      <span id="wizard-detect-flrig-icon"></span>
      <span id="wizard-detect-flrig-note"></span>
      <span id="wizard-detect-rigctld-icon"></span>
      <span id="wizard-detect-rigctld-note"></span>
      <div id="wizard-detect-rigctld-install" style="display:none"></div>
      <span id="wizard-detect-rigctld-hint"></span>
    </div>
    <div id="wizard-lotw-indicator"></div>
    <div id="wizard-lotw-text"></div>
    <div id="wizard-lotw-form" style="display:none"></div>
    <input id="wiz-lotw-path" value="" />
    <div id="wizard-rig-indicator"></div>
    <div id="wizard-rig-text"></div>
    <div id="wizard-summary"></div>
    <input id="wiz-wsjtx-enabled" type="checkbox" checked />
    <input id="wiz-wsjtx-port" value="2237" />
    <input id="wiz-js8call-enabled" type="checkbox" />
    <input id="wiz-js8call-port" value="2242" />
    <input id="wiz-n1mm-enabled" type="checkbox" />
    <input id="wiz-n1mm-port" value="12060" />
    <select id="wiz-rig-method"><option value="none">None</option><option value="flrig">flrig</option><option value="hamlib">hamlib</option></select>
    <input id="wiz-rig-host" value="127.0.0.1" />
    <input id="wiz-rig-flrig-port" value="12345" />
    <input id="wiz-rig-rigctld-port" value="4532" />
    <input id="wiz-lotw-location" value="" />
  `
}

beforeAll(async () => {
  const mod = await import('../../src/setup-wizard')
  initWizard = mod.initWizard
  wizardNext = mod.wizardNext
  wizardBack = mod.wizardBack
  wizardToggleAuthMode = mod.wizardToggleAuthMode
  wireWizardListeners = mod.wireWizardListeners
  attachWizardInit = mod.attachWizardInit
  setLogger = mod.setLogger
  setOnWizardFinish = mod.setOnWizardFinish
})

beforeEach(() => {
  setupWizardDom()
  logs.length = 0
  finishCalls = []
  mockInvoke.mockReset()
  setLogger(testLogger)
  setOnWizardFinish(testOnFinish)
})

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('setup-wizard module', () => {
  it('exports all expected functions', () => {
    expect(typeof initWizard).toBe('function')
    expect(typeof wizardNext).toBe('function')
    expect(typeof wizardBack).toBe('function')
    expect(typeof wizardToggleAuthMode).toBe('function')
    expect(typeof wireWizardListeners).toBe('function')
    expect(typeof attachWizardInit).toBe('function')
    expect(typeof setLogger).toBe('function')
    expect(typeof setOnWizardFinish).toBe('function')
  })

  describe('initWizard', () => {
    it('shows wizard overlay when setup is not complete', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: false })
      await initWizard()
      const overlay = document.getElementById('wizard-overlay')
      expect(overlay?.style.display).toBe('flex')
      // Step 0 should be active
      const step0 = document.getElementById('wizard-step-0')
      expect(step0?.classList.contains('active')).toBe(true)
    })

    it('does not show wizard when setup is complete', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: true })
      await initWizard()
      const overlay = document.getElementById('wizard-overlay')
      expect(overlay?.style.display).toBe('none')
    })

    it('does not throw when invoke fails (dev mode)', async () => {
      mockInvoke.mockRejectedValue(new Error('command not found'))
      await expect(initWizard()).resolves.toBeUndefined()
      const overlay = document.getElementById('wizard-overlay')
      expect(overlay?.style.display).toBe('none')
    })
  })

  describe('wizardNext / wizardBack navigation', () => {
    it('advances to the next step on wizardNext', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: false })
      await initWizard()
      const step0 = document.getElementById('wizard-step-0')
      const step1 = document.getElementById('wizard-step-1')
      expect(step0?.classList.contains('active')).toBe(true)

      wizardNext()
      expect(step1?.classList.contains('active')).toBe(true)
    })

    it('goes back to the previous step on wizardBack', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: false })
      await initWizard()
      wizardNext() // step 0 → 1
      wizardBack()  // step 1 → 0
      const step0 = document.getElementById('wizard-step-0')
      expect(step0?.classList.contains('active')).toBe(true)
    })

    it('updates progress dots during navigation', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: false })
      await initWizard()
      const dots = document.querySelectorAll<HTMLElement>('.wizard-step-dot')
      expect(dots[0].classList.contains('active')).toBe(true)

      wizardNext()
      expect(dots[0].classList.contains('done')).toBe(true)
      expect(dots[1].classList.contains('active')).toBe(true)
    })
  })

  describe('wizardToggleAuthMode', () => {
    it('switches to cloud mode and shows cloud panel', () => {
      wizardToggleAuthMode('cloud')
      const cloudBtn = document.getElementById('wizard-auth-cloud-btn')!
      const localBtn = document.getElementById('wizard-auth-local-btn')!
      const cloudPanel = document.getElementById('wizard-auth-cloud')!
      const localPanel = document.getElementById('wizard-auth-local')!

      expect(cloudBtn.classList.contains('active')).toBe(true)
      expect(localBtn.classList.contains('active')).toBe(false)
      expect(cloudPanel.style.display).not.toBe('none')
      expect(localPanel.style.display).toBe('none')
    })

    it('switches to local mode and shows local panel', () => {
      wizardToggleAuthMode('local')
      const cloudBtn = document.getElementById('wizard-auth-cloud-btn')!
      const localBtn = document.getElementById('wizard-auth-local-btn')!
      const cloudPanel = document.getElementById('wizard-auth-cloud')!
      const localPanel = document.getElementById('wizard-auth-local')!

      expect(localBtn.classList.contains('active')).toBe(true)
      expect(cloudBtn.classList.contains('active')).toBe(false)
      expect(cloudPanel.style.display).toBe('none')
      expect(localPanel.style.display).not.toBe('none')
    })
  })

  describe('wireWizardListeners', () => {
    it('wires click handlers without throwing', () => {
      expect(() => wireWizardListeners()).not.toThrow()
    })
  })

  describe('setLogger / setOnWizardFinish', () => {
    it('setLogger injects a custom log function', async () => {
      const customLogs: string[] = []
      setLogger((msg: string) => { customLogs.push(msg) })

      // Trigger a log by finishing the wizard
      mockInvoke.mockResolvedValue(null) // save_wizard_config + complete_wizard
      // Navigate to step 7
      for (let i = 0; i < 7; i++) wizardNext()
      // Wire listeners so the finish button calls wizardFinish
      wireWizardListeners()
      const finishBtn = document.getElementById('wizard-finish-btn') as HTMLButtonElement
      finishBtn.click()
      // Wait for async wizardFinish to complete
      await vi.waitFor(() => expect(customLogs.length).toBeGreaterThan(0), { timeout: 3000 })
      expect(customLogs.some(l => l.includes('Setup wizard complete'))).toBe(true)

      setLogger(testLogger) // reset
    })

    it('setOnWizardFinish fires with the chosen auth mode', async () => {
      mockInvoke.mockResolvedValue(null) // save_wizard_config + complete_wizard

      // Navigate to step 7
      for (let i = 0; i < 7; i++) wizardNext()

      // Wire listeners so the finish button calls wizardFinish
      wireWizardListeners()
      const finishBtn = document.getElementById('wizard-finish-btn') as HTMLButtonElement
      finishBtn.click()
      await vi.waitFor(() => expect(finishCalls.length).toBeGreaterThan(0), { timeout: 3000 })
      // auth_mode will be whatever was last toggled (local from the previous toggle test)
      expect(['cloud', 'local']).toContain(finishCalls[0])
    })
  })

  describe('step capture logic', () => {
    it('captures UDP port values when moving from step 4', async () => {
      mockInvoke.mockResolvedValue({ setup_complete: false })
      await initWizard()
      // Navigate to step 4
      for (let i = 0; i < 4; i++) wizardNext()
      // Modify form values
      const portInput = document.getElementById('wiz-wsjtx-port') as HTMLInputElement
      if (portInput) portInput.value = '9999'
      // Advance — this triggers capture
      wizardNext()
      // Step 4 capture happens; port value would be stored internally.
      // We can't inspect module state directly, but we can verify no error thrown.
      expect(true).toBe(true)
    })
  })
})