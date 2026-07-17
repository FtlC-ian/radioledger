import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Tray Icon (Scaffold)', () => {
  before(async () => {
    await dismissWizardIfPresent();
  });

  it('should include tray guidance in setup flow', async () => {
    const wizardHint = await $('#wizard-step-7 .wizard-hint');
    await expect(wizardHint).toExist();
  });

  it.skip('should minimize app to tray', async () => {
    // TODO(issue-41): requires an app-level command/hook to trigger and verify hide-to-tray behavior.
  });

  it.skip('should restore app window from tray menu', async () => {
    // TODO(issue-41): requires tray event introspection support for WebDriver tests.
  });

  it.skip('should quit app from tray menu', async () => {
    // TODO(issue-41): requires deterministic tray command automation in test mode.
  });
});
