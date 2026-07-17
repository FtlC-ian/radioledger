import { dismissWizardIfPresent } from './helpers/dismiss-wizard';

describe('Logbook View (Scaffold)', () => {
  before(async () => {
    await dismissWizardIfPresent();
    const logTab = await $('[data-testid="tab-log"]');
    await logTab.click();
    await browser.pause(300);
  });

  it('should render logbook table and pagination controls', async () => {
    await expect(await $('#log-table')).toExist();
    await expect(await $('#log-prev-page')).toExist();
    await expect(await $('#log-next-page')).toExist();
  });

  it('should render filtering controls', async () => {
    await expect(await $('#log-filter-callsign')).toExist();
    await expect(await $('#log-filter-band')).toExist();
    await expect(await $('#log-filter-mode')).toExist();
    await expect(await $('#log-apply-filters')).toExist();
  });

  it('should open the column picker', async () => {
    const toggle = await $('#log-columns-toggle');
    await toggle.waitForExist();
    await browser.execute((el: HTMLElement) => el.click(), toggle);
    await expect(await $('#log-columns-menu')).toBeDisplayed();
    await expect(await $('#log-columns-list')).toExist();
  });

  it('should support sorting by clicking sortable headers', async () => {
    const callsignHeader = await $('th[data-sort="callsign"]');
    await callsignHeader.waitForExist();
    // Use JS click — webkit under xvfb reports table cells as not visible
    await browser.execute((el: HTMLElement) => el.click(), callsignHeader);
    await browser.pause(200);

    const classes = await callsignHeader.getAttribute('class');
    expect(classes).toContain('active');
  });
});
