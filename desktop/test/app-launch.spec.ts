/**
 * app-launch.spec.ts
 *
 * Verifies that the RadioLedger desktop app launches successfully and
 * the main window renders with the expected title and root element.
 */

import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('App Launch', () => {
  before(async () => {
    await dismissWizardIfPresent();
  });

  it('should launch and display the main window', async () => {
    // The app should be running — driver connects on beforeSession
    const title = await browser.getTitle();
    expect(title).toContain('RadioLedger');
  });

  it('should render the root app element', async () => {
    // The Vue app mounts at #app
    const rootEl = await $('#app');
    await expect(rootEl).toExist();
  });

  it('should not show an error screen on startup', async () => {
    // Basic sanity: no crash/error overlay visible
    const body = await $('body');
    const bodyText = await body.getText();
    expect(bodyText).not.toContain('panic');
    expect(bodyText).not.toContain('Error: ');
  });
});
