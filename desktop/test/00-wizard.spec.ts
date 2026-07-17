/**
 * Setup Wizard E2E tests.
 *
 * All step-to-step navigation waits deterministically for the next panel's
 * app-controlled `active` class rather than using fixed `browser.pause()`
 * delays or WebDriver visibility polling.  The old approach was brittle on
 * Linux CI where Wry sometimes reported an active `display: block` panel as
 * not displayed.
 *
 * See the public issue tracker for setup-wizard coverage.
 */

const STEP_TIMEOUT = 15000; // ms - Linux CI can be slow to attach handlers after launch.

async function waitForWizardStep(step: number): Promise<WebdriverIO.Element> {
  const stepEl = await $(`#wizard-step-${step}`);
  await browser.waitUntil(
    async () => ((await stepEl.getAttribute('class')) ?? '').split(/\s+/).includes('active'),
    {
      timeout: STEP_TIMEOUT,
      timeoutMsg: `wizard step ${step} did not become active`,
    },
  );
  return stepEl;
}

async function waitForWizardStepInactive(stepEl: WebdriverIO.Element, step: number): Promise<void> {
  await browser.waitUntil(
    async () => !((await stepEl.getAttribute('class')) ?? '').split(/\s+/).includes('active'),
    {
      timeout: STEP_TIMEOUT,
      timeoutMsg: `wizard step ${step} did not become inactive`,
    },
  );
}

async function waitForWizardHeading(stepEl: WebdriverIO.Element, expectedText: string): Promise<void> {
  const heading = await stepEl.$('.wizard-heading');
  await browser.waitUntil(
    async () => (await heading.getText()).includes(expectedText),
    {
      timeout: STEP_TIMEOUT,
      timeoutMsg: `wizard heading did not include ${expectedText}`,
    },
  );
}

describe('Setup Wizard', () => {
  it('should show the wizard overlay on first launch', async () => {
    const overlay = await $('#wizard-overlay');
    await overlay.waitForDisplayed({ timeout: STEP_TIMEOUT });
  });

  it('should show the welcome step initially', async () => {
    const step0 = await waitForWizardStep(0);
    await waitForWizardHeading(step0, 'Welcome');
  });

  it('should show progress dots', async () => {
    const dots = await $$('.wizard-step-dot');
    expect(dots.length).toBe(8);
  });

  it('should navigate to step 1 when clicking Get Started', async () => {
    const btn = await $('#wizard-step-0 .wizard-btn-primary');
    await btn.scrollIntoView();
    await btn.click();

    // Wait for step 1 to become visible rather than a fixed pause
    await waitForWizardStep(1);
  });

  it('should be able to skip through all steps to completion', async () => {
    for (let step = 1; step <= 6; step++) {
      // Wait deterministically for this step to be active; fail fast on timeout
      // rather than silently skipping an unready step.
      await waitForWizardStep(step);
      const stepEl = await $(`#wizard-step-${step}`);

      const skipBtn = await stepEl.$('.wizard-btn-ghost');
      if (await skipBtn.isExisting()) {
        await skipBtn.scrollIntoView();
        await skipBtn.click();
      } else {
        const nextBtn = await stepEl.$('.wizard-btn-primary');
        await nextBtn.scrollIntoView();
        await nextBtn.click();
      }

      await waitForWizardStepInactive(stepEl, step);
    }

    await waitForWizardStep(7);
  });

  it('should close wizard and show main app when clicking Finish', async () => {
    const finishBtn = await $('#wizard-finish-btn');
    await finishBtn.waitForDisplayed({ timeout: STEP_TIMEOUT });
    await finishBtn.scrollIntoView();
    await finishBtn.click();

    const overlay = await $('#wizard-overlay');
    await overlay.waitForDisplayed({ reverse: true, timeout: 8000 });

    const shackTab = await $('[data-testid="tab-shack"]');
    await shackTab.waitForDisplayed({ timeout: STEP_TIMEOUT });
  });
});
