/**
 * settings.spec.ts
 *
 * Verifies that interactive controls work: UDP listener toggle, auth buttons,
 * sync controls on the Shack tab, and all Settings tab fields.
 */

import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Interactive Controls — Shack Tab', () => {
  before(async () => {
    await dismissWizardIfPresent();

    // Ensure we're on the Shack tab
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
    await browser.pause(500);
  });

  it('should display the WSJT-X UDP toggle button', async () => {
    const udpToggle = await $('[data-testid="udp-wsjtx-toggle"]');
    await expect(udpToggle).toExist();
  });

  it('should display the login button', async () => {
    const loginBtn = await $('#login-btn');
    await expect(loginBtn).toExist();
  });

  it('should display the sync now button', async () => {
    const syncBtn = await $('button*=Sync Now');
    await expect(syncBtn).toExist();
  });

  it('should display the refresh rig button', async () => {
    const refreshBtn = await $('button*=Refresh rig');
    await expect(refreshBtn).toExist();
  });

  it('should display statistics KPI cards', async () => {
    const totalQsos = await $('#stat-total-qsos');
    await expect(totalQsos).toExist();

    const uniqueCallsigns = await $('#stat-unique-callsigns');
    await expect(uniqueCallsigns).toExist();
  });

  it('should have a stats refresh button', async () => {
    const refreshStats = await $('button*=Refresh');
    await expect(refreshStats).toExist();
  });
});

describe('Settings Tab', () => {
  before(async () => {
    await dismissWizardIfPresent();

    // Navigate to the Settings tab
    const settingsTab = await $('[data-testid="tab-settings"]');
    await settingsTab.click();
    await browser.pause(500);
  });

  after(async () => {
    // Return to Shack tab after tests
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
  });

  it('should show the Settings tab as active', async () => {
    const settingsTab = await $('[data-testid="tab-settings"]');
    const classes = await settingsTab.getAttribute('class');
    expect(classes).toContain('active');
  });

  it('should render the Server Connection card', async () => {
    const serverCard = await $('#settings-server-card');
    await expect(serverCard).toExist();
  });

  it('should render the Server URL input field', async () => {
    const serverUrl = await $('#settings-server-url');
    await expect(serverUrl).toExist();

    // Should accept text input
    await serverUrl.setValue('https://example.radioledger.app');
    const val = await serverUrl.getValue();
    expect(val).toBe('https://example.radioledger.app');
  });

  it('should render the Save server button', async () => {
    const saveBtn = await $('#settings-save-server-btn');
    await expect(saveBtn).toExist();
  });

  it('should render the Test Connection button', async () => {
    const testBtn = await $('#settings-test-connection-btn');
    await expect(testBtn).toExist();
  });

  it('should render the UDP Listeners card', async () => {
    const udpCard = await $('#settings-udp-card');
    await expect(udpCard).toExist();
  });

  it('should render the WSJT-X UDP port input', async () => {
    const portInput = await $('[data-testid="settings-udp-wsjtx-port"]');
    await expect(portInput).toExist();

    // Should be a number input
    const type = await portInput.getAttribute('type');
    expect(type).toBe('number');
  });

  it('should render the FT8Battle relay toggle', async () => {
    const relayToggle = await $('[data-testid="settings-udp-ft8battle-relay"]');
    await expect(relayToggle).toExist();
  });

  it('should render the Save UDP Settings button', async () => {
    const saveUdpBtn = await $('#settings-save-udp-btn');
    await expect(saveUdpBtn).toExist();
  });
});
