import { beforeEach, describe, expect, it } from 'vitest';
import { clearMocks, mockIPC } from '@tauri-apps/api/mocks';
import { invoke } from '@tauri-apps/api/core';

describe('tauri-plugin-test style command mocks', () => {
  beforeEach(() => {
    clearMocks();
  });

  it('mocks Tauri invoke() commands in Node.js without launching the app binary', async () => {
    mockIPC((cmd, args) => {
      if (cmd === 'get_auth_status') {
        return { logged_in: true, callsign: 'W1AW' };
      }

      if (cmd === 'save_settings') {
        return { ok: true, args };
      }

      return null;
    });

    const auth = await invoke<{ logged_in: boolean; callsign: string }>('get_auth_status');
    expect(auth.logged_in).toBe(true);
    expect(auth.callsign).toBe('W1AW');

    const saveResult = await invoke<{ ok: boolean; args: unknown }>('save_settings', {
      request: { server_url: 'https://example.radioledger.app', auth_mode: 'local' },
    });
    expect(saveResult.ok).toBe(true);
  });
});
