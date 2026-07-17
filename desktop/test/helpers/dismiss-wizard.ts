/**
 * If the setup wizard overlay is visible, click through all steps using
 * Skip/Next buttons until reaching the final step, then click Finish.
 * If the wizard is not visible, returns immediately.
 *
 * Navigation is driven by the wizard's app-controlled `active` class rather
 * than `browser.pause` or WebDriver visibility polling.  Linux/Wry CI has
 * reported active `display: block` panels as not displayed.
 *
 * See the public issue tracker for setup-wizard coverage.
 */

const DISMISS_STEP_TIMEOUT = 15000; // ms - Linux CI can be slow to attach handlers after launch.

async function waitForWizardStep(step: number): Promise<WebdriverIO.Element> {
  const stepEl = await $(`#wizard-step-${step}`);
  await browser.waitUntil(
    async () => ((await stepEl.getAttribute('class')) ?? '').split(/\s+/).includes('active'),
    {
      timeout: DISMISS_STEP_TIMEOUT,
      timeoutMsg: `wizard step ${step} did not become active`,
    },
  );
  return stepEl;
}

async function waitForWizardStepInactive(
  stepEl: WebdriverIO.Element,
  step: number,
): Promise<void> {
  await browser.waitUntil(
    async () => !((await stepEl.getAttribute('class')) ?? '').split(/\s+/).includes('active'),
    {
      timeout: DISMISS_STEP_TIMEOUT,
      timeoutMsg: `wizard step ${step} did not become inactive`,
    },
  );
}

export async function dismissWizardIfPresent(): Promise<void> {
  const overlay = await $('#wizard-overlay');

  if (!(await overlay.isExisting()) || !(await overlay.isDisplayed())) {
    return;
  }

  // Step 0 → step 1
  const getStartedBtn = await $('#wizard-step-0 .wizard-btn-primary');
  if (await getStartedBtn.isExisting()) {
    await getStartedBtn.scrollIntoView();
    await getStartedBtn.click();

    await waitForWizardStep(1);
  }

  // Steps 1–6: skip or click Next, then wait for the step to hide before
  // moving on so we never query a step mid-transition.
  for (let step = 1; step <= 6; step++) {
    const stepEl = await $(`#wizard-step-${step}`);

    if (!(await stepEl.isExisting())) {
      continue;
    }

    // Wait deterministically for the step to become active.
    await waitForWizardStep(step);

    const skipBtn = await stepEl.$('.wizard-btn-ghost');
    if (await skipBtn.isExisting()) {
      await skipBtn.scrollIntoView();
      await skipBtn.click();
    } else {
      const nextBtn = await stepEl.$('.wizard-btn-primary');
      if (await nextBtn.isExisting()) {
        await nextBtn.scrollIntoView();
        await nextBtn.click();
      }
    }

    await waitForWizardStepInactive(stepEl, step);
  }

  // Step 7 (summary) → Finish
  await waitForWizardStep(7);

  const finishBtn = await $('#wizard-finish-btn');
  if (await finishBtn.isExisting()) {
    await finishBtn.scrollIntoView();
    await finishBtn.click();
  }

  await overlay.waitForDisplayed({ reverse: true, timeout: 8000 });
}
