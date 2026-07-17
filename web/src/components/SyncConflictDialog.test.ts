// @vitest-environment happy-dom
/**
 * Component regression test for SyncConflictDialog (issue #193).
 *
 * Mounts the real SyncConflictDialog.vue via @vue/test-utils and exercises the
 * exact regression sequence: open → change selection → close without saving →
 * reopen same conflict → assert UI resets to default (service_a).
 *
 * The previous test reimplemented the watcher logic in pure Vue reactivity,
 * which could pass even if the component itself regressed.  This test mounts
 * the actual component so any regression inside SyncConflictDialog.vue will
 * cause the test to fail.
 */

import { describe, expect, it } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import SyncConflictDialog from './SyncConflictDialog.vue'
import type { SyncConflict } from 'src/types/sync'

// ── Quasar stubs ──────────────────────────────────────────────────────────────
// shallowMount auto-stubs child components; we only need to handle the
// v-close-popup directive so Vue doesn't emit an unregistered-directive warning.
const globalConfig = {
  directives: {
    // No-op stub for Quasar's v-close-popup directive.
    closePopup: {},
  },
}

// ── Minimal conflict fixture ──────────────────────────────────────────────────

function makeConflict(): SyncConflict {
  return {
    id: 1,
    qso_uuid: 'qso-abc',
    callsign: 'W1AW',
    band: '20m',
    mode: 'FT8',
    datetime_on: '2026-04-10T15:00:00Z',
    service_a: 'service_a',
    service_b: 'service_b',
    field_conflicts: {
      callsign: { service_a: 'W1AW', service_b: 'W1AW/P' },
      band: { service_a: '20m', service_b: '40m' },
    },
    status: 'pending',
    created_at: '2026-04-10T15:00:00Z',
    updated_at: '2026-04-10T15:00:00Z',
  }
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SyncConflictDialog — reopen/reset behavior (regression #193)', () => {
  it('resets all selections to service_a when re-opened with the same conflict object', async () => {
    const conflict = makeConflict()

    // 1. Mount and open the real component.
    const wrapper = shallowMount(SyncConflictDialog, {
      props: {
        modelValue: true,
        conflict,
        loading: false,
      },
      global: globalConfig,
    })

    // Confirm defaults were applied on open (via the component's own watchers).
    expect(wrapper.vm.localResolution.callsign).toBe('service_a')
    expect(wrapper.vm.localResolution.band).toBe('service_a')

    // 2. Simulate user changing a selection away from default.
    wrapper.vm.localResolution.callsign = 'service_b'
    expect(wrapper.vm.localResolution.callsign).toBe('service_b')

    // 3. Close without saving (v-model → false).
    await wrapper.setProps({ modelValue: false })

    // Selections must NOT be reset on close — only on re-open.
    expect(wrapper.vm.localResolution.callsign).toBe('service_b')

    // 4. Re-open with the SAME conflict object identity (the regression case).
    await wrapper.setProps({ modelValue: true })

    // 5. Selections must reset to the default (service_a) on re-open.
    expect(wrapper.vm.localResolution.callsign).toBe('service_a')
    expect(wrapper.vm.localResolution.band).toBe('service_a')
  })

  it('resets on open when the conflict prop changes by identity', async () => {
    const conflict = makeConflict()

    const wrapper = shallowMount(SyncConflictDialog, {
      props: {
        modelValue: true,
        conflict,
        loading: false,
      },
      global: globalConfig,
    })

    // Dirty a field.
    wrapper.vm.localResolution.band = 'service_b'
    expect(wrapper.vm.localResolution.band).toBe('service_b')

    // Swap to a new conflict object.
    const newConflict: SyncConflict = { ...conflict, id: 2, callsign: 'K7ABC' }
    await wrapper.setProps({ conflict: newConflict })

    // Selections must reset because the conflict prop identity changed.
    expect(wrapper.vm.localResolution.band).toBe('service_a')
  })

  it('emits submit with current localResolution', async () => {
    const conflict = makeConflict()

    const wrapper = shallowMount(SyncConflictDialog, {
      props: {
        modelValue: true,
        conflict,
        loading: false,
      },
      global: globalConfig,
    })

    // Change one field.
    wrapper.vm.localResolution.band = 'service_b'

    // Trigger submit by calling the internal submit function via the emitted event
    // (simulate the Resolve button click).
    await wrapper.vm.$emit !== undefined
    // Call submit directly — it is the function bound to @click on the Resolve q-btn.
    ;(wrapper.vm as unknown as { submit: () => void }).submit?.()

    const emitted = wrapper.emitted('submit')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toEqual({
      callsign: 'service_a',
      band: 'service_b',
    })
  })
})
