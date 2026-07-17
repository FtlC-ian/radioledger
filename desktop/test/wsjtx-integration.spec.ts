import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('WSJT-X Integration (Scaffold)', () => {
  before(async () => {
    await dismissWizardIfPresent();
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
    await browser.pause(300);
  });

  it('should render listener status, port, packet count and toggle', async () => {
    await expect(await $('[data-testid="udp-wsjtx-status"]')).toExist();
    await expect(await $('[data-testid="udp-wsjtx-port"]')).toExist();
    await expect(await $('[data-testid="udp-wsjtx-packets"]')).toExist();
    await expect(await $('[data-testid="udp-wsjtx-toggle"]')).toExist();
  });

  it('should toggle WSJT-X listener button label on click', async () => {
    const toggleBtn = await $('[data-testid="udp-wsjtx-toggle"]');
    const before = await toggleBtn.getText();

    await toggleBtn.click();
    await browser.pause(500);

    const after = await toggleBtn.getText();
    expect(after).not.toBe(before);
  });

  it('should expose the decode panel setting in Settings', async () => {
    const settingsTab = await $('[data-testid="tab-settings"]');
    await settingsTab.click();
    await browser.pause(300);

    const decodeToggle = await $('[data-testid="settings-wsjtx-decode-panel-enabled"]');
    await expect(decodeToggle).toExist();
  });
});
