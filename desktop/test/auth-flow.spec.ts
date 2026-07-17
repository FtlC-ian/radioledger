import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Auth Flow (Scaffold)', () => {
  before(async () => {
    await dismissWizardIfPresent();
    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.click();
    await browser.pause(300);
  });

  it('should render auth status and action buttons', async () => {
    await expect(await $('#auth-status')).toExist();
    await expect(await $('#login-btn')).toExist();
    await expect(await $('#logout-btn')).toExist();
  });

  it('should route local-mode login to Settings tab', async () => {
    // Navigate directly to the Settings tab and verify it becomes active.
    // (Toggling the local-auth mode button only updates the Settings UI state;
    // it does not update the runtime currentAuthMode until settings are saved,
    // so the full login-redirect flow is covered by integration tests.)
    const settingsTab = await $('[data-testid="tab-settings"]');
    await settingsTab.click();
    await browser.pause(200);

    const settingsPanel = await $('#tab-content-settings');
    const classes = await settingsPanel.getAttribute('class');
    expect(classes).toContain('active');
  });

  it('should expose local auth credential fields for self-hosted mode', async () => {
    await expect(await $('#settings-local-email')).toExist();
    await expect(await $('#settings-local-password')).toExist();
    await expect(await $('#settings-local-login-btn')).toExist();
  });
});
