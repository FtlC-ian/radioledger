/**
 * navigation.spec.ts
 *
 * Verifies the tabbed layout: all three tabs exist, the Shack tab (default)
 * renders all expected cards, and switching tabs shows the right content.
 */

import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Tab Navigation', () => {
  before(async () => {
    await dismissWizardIfPresent();
    await browser.pause(500);
  });

  // ── Header & tabs ─────────────────────────────────────────────────────────

  it('should render the app header with logo', async () => {
    const header = await $('header');
    await expect(header).toExist();

    const title = await $('header img[alt="RadioLedger"]');
    // Logo image replaces h1 text
    await expect(title).toExist();
  });

  it('should render the Shack tab button', async () => {
    const shackTab = await $('[data-testid="tab-shack"]');
    await expect(shackTab).toExist();
  });

  it('should render the Log tab button', async () => {
    const logTab = await $('[data-testid="tab-log"]');
    await expect(logTab).toExist();
  });

  it('should render the Settings tab button', async () => {
    const settingsTab = await $('[data-testid="tab-settings"]');
    await expect(settingsTab).toExist();
  });

  // ── Shack tab (default) ───────────────────────────────────────────────────

  it('should show the Shack tab as active by default', async () => {
    const shackTab = await $('[data-testid="tab-shack"]');
    const classes = await shackTab.getAttribute('class');
    expect(classes).toContain('active');
  });

  it('should render the authentication card on the Shack tab', async () => {
    const authCard = await $('#auth-card');
    await expect(authCard).toExist();

    const authStatus = await $('#auth-status');
    await expect(authStatus).toExist();
  });

  it('should render the WSJT-X integration card on the Shack tab', async () => {
    const udpCard = await $('#udp-card');
    await expect(udpCard).toExist();

    const udpStatus = await $('[data-testid="udp-wsjtx-status"]');
    await expect(udpStatus).toExist();
  });

  it('should render the rig control card on the Shack tab', async () => {
    const rigCard = await $('#rig-card');
    await expect(rigCard).toExist();
  });

  it('should render the sync status card on the Shack tab', async () => {
    const syncCard = await $('#sync-card');
    await expect(syncCard).toExist();
  });

  it('should render the statistics dashboard section on the Shack tab', async () => {
    const statsSection = await $('#stats-section');
    await expect(statsSection).toExist();
  });

  it('should render the recent activity log on the Shack tab', async () => {
    const logOutput = await $('#log-output');
    await expect(logOutput).toExist();
  });

  // ── Tab switching ─────────────────────────────────────────────────────────

  it('should switch to the Log tab and show log table', async () => {
    const logTab = await $('[data-testid="tab-log"]');
    await logTab.click();
    await browser.pause(200);

    const logContent = await $('#tab-content-log');
    await expect(logContent).toExist();

    // Shack content should be hidden; Log content should be active
    const logClasses = await logContent.getAttribute('class');
    expect(logClasses).toContain('active');

    const shackContent = await $('#tab-content-shack');
    const shackClasses = await shackContent.getAttribute('class');
    expect(shackClasses).not.toContain('active');

    // Check for log table instead of placeholder
    const logTable = await $('#log-table');
    await expect(logTable).toExist();
  });

  it('should switch to the Settings tab and show config fields', async () => {
    const settingsTab = await $('[data-testid="tab-settings"]');
    await settingsTab.click();
    await browser.pause(200);

    const settingsContent = await $('#tab-content-settings');
    const settingsClasses = await settingsContent.getAttribute('class');
    expect(settingsClasses).toContain('active');

    // Settings fields should be visible
    const serverUrl = await $('#settings-server-url');
    await expect(serverUrl).toExist();

    const udpPort = await $('[data-testid="settings-udp-wsjtx-port"]');
    await expect(udpPort).toExist();
  });

  it('should switch back to the Shack tab', async () => {
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
    await browser.pause(200);

    const shackContent = await $('#tab-content-shack');
    const classes = await shackContent.getAttribute('class');
    expect(classes).toContain('active');

    // Cards should be visible again
    const authCard = await $('#auth-card');
    await expect(authCard).toExist();
  });
});
