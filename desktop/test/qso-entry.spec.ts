/**
 * qso-entry.spec.ts
 *
 * Verifies that the Log QSO tab renders correctly and accepts
 * input in key fields.
 */

import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('QSO Draft Card', () => {
  before(async () => {
    await dismissWizardIfPresent();

    const logQsoTab = await $('[data-testid="tab-log-qso"]');
    await logQsoTab.click();
    await browser.pause(500);
  });

  it('should render the QSO draft card', async () => {
    const draftCard = await $('#qso-draft-card');
    await expect(draftCard).toExist();
  });

  it('should have a callsign input field', async () => {
    const callsignInput = await $('#qso-callsign');
    await expect(callsignInput).toExist();
  });

  it('should accept input in the callsign field', async () => {
    const callsignInput = await $('#qso-callsign');
    await callsignInput.waitForClickable({ timeout: 5000 });
    await callsignInput.setValue('W1AW');

    const value = await callsignInput.getValue();
    expect(value).toBe('W1AW');
  });

  it('should have frequency and mode fields', async () => {
    const freqInput = await $('#qso-frequency');
    await expect(freqInput).toExist();

    const modeInput = await $('#qso-mode');
    await expect(modeInput).toExist();
  });

  it('should have a band select field', async () => {
    const bandSelect = await $('#qso-band');
    await expect(bandSelect).toExist();

    const tagName = await bandSelect.getTagName();
    expect(tagName.toLowerCase()).toBe('select');
  });

  it('should have a Log QSO button', async () => {
    const logBtn = await $('#qso-log-btn');
    await expect(logBtn).toExist();
  });

  it('should render the WSJT-X decode panel on the side', async () => {
    const panel = await $('#wsjtx-decode-panel');
    await expect(panel).toExist();
  });
});
