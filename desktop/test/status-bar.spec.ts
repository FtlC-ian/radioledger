/**
 * status-bar.spec.ts
 *
 * Verifies that the persistent status bar is visible from every tab
 * and contains all required status indicators.
 */

import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Persistent Status Bar', () => {
  before(async () => {
    await dismissWizardIfPresent();
    await browser.pause(500);
  });

  it('should render the status bar element', async () => {
    const statusBar = await $('[data-testid="status-bar"]');
    await expect(statusBar).toExist();
  });

  it('should render the server status indicator', async () => {
    const serverStatus = await $('[data-testid="status-server"]');
    await expect(serverStatus).toExist();
  });

  it('should render the UDP status indicator', async () => {
    const udpStatus = await $('[data-testid="status-udp"]');
    await expect(udpStatus).toExist();
  });

  it('should render the rig status indicator', async () => {
    const rigStatus = await $('[data-testid="status-rig"]');
    await expect(rigStatus).toExist();
  });

  it('should remain visible on the Log tab', async () => {
    const logTab = await $('[data-testid="tab-log"]');
    await logTab.click();
    await browser.pause(200);

    const statusBar = await $('[data-testid="status-bar"]');
    await expect(statusBar).toExist();

    // Return to Shack
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
  });

  it('should remain visible on the Settings tab', async () => {
    const settingsTab = await $('[data-testid="tab-settings"]');
    await settingsTab.click();
    await browser.pause(200);

    const statusBar = await $('[data-testid="status-bar"]');
    await expect(statusBar).toExist();

    // Return to Shack
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
  });

  it('should display server text', async () => {
    const serverText = await $('#statusbar-server-text');
    await expect(serverText).toExist();
  });

  it('should display UDP text', async () => {
    const udpText = await $('#statusbar-udp-text');
    await expect(udpText).toExist();
  });

  it('should display sync pending count', async () => {
    const pending = await $('#statusbar-pending');
    await expect(pending).toExist();
  });

  it('should display last sync time', async () => {
    const lastSync = await $('#statusbar-last-sync');
    await expect(lastSync).toExist();
  });
});
