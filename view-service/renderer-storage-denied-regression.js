// 0.2.1.35-⑮: browser policy may deny Web Storage even when the page itself loads.
// The renderer must remain usable instead of dying during top-level evaluation.
const assert = require('assert');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

(async () => {
  const fixture = await startRendererServer();
  let browser;
  try {
    browser = await launchChromium();
    const context = await browser.newContext();
    await context.addInitScript(() => {
      for (const name of ['sessionStorage', 'localStorage']) {
        Object.defineProperty(window, name, {
          configurable: true,
          get() { throw new DOMException(`${name} denied by policy`, 'SecurityError'); },
        });
      }
    });
    await context.route('**/status', route => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify({
        session: 'storage-denied-2642', state: 'idle', readonly: true, can_write: false,
        owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0,
      }),
    }));
    await context.route('**/events', route => route.fulfill({
      status: 200, contentType: 'text/event-stream', body: '',
    }));
    const page = await context.newPage();
    const pageErrors = [];
    page.on('pageerror', err => pageErrors.push(err.message));
    await page.goto(`${fixture.url}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });

    const checks = regressionCollector();
    await checks.check('S1 storage denied does not abort renderer startup', async () => {
      await page.waitForFunction(() => typeof handleIncomingEvent === 'function' && typeof stats !== 'undefined', null, { timeout: 3000 });
      assert.deepStrictEqual(pageErrors, [], `top-level page errors: ${pageErrors.join(' | ')}`);
    });
    await checks.check('S2 status bar still renders', async () => {
      await page.waitForFunction(() => document.body.innerText.includes('storage-denied-2642'), null, { timeout: 3000 });
      assert.strictEqual(await page.locator('#clockStatus').count(), 1, 'status bar clock missing');
    });
    await checks.check('S3 SSE body still renders', async () => {
      await page.evaluate(() => {
        finishTimelineReplay(false);
        handleIncomingEvent({ kind: 'assistant_text', text: '存储被禁时正文仍可见', delivery_id: 'd-storage', turn_id: 't-storage', cursor: '1' });
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed' }, delivery_id: 'd-storage', turn_id: 't-storage', cursor: '2' });
      });
      await page.locator('#timeline').getByText('存储被禁时正文仍可见').waitFor({ state: 'visible' });
    });
    checks.finish();
    console.log('renderer storage-denied regression: ALL PASS (3/3)');
    await context.close();
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
