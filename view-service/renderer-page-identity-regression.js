const { chromium } = require('playwright');
const fs = require('fs');
const assert = require('assert');

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext();
    const original = { page_id: 'shared-page', page_token: 'shared-token', code: 'alice-developer-0000-a3Kf9xQ2' };
    await context.addInitScript(identity => {
      sessionStorage.setItem('dw-view-page:alice-developer-0000', JSON.stringify(identity));
    }, original);
    let created = 0;
    const inputs = [];
    let rejectNextInput = false;
    await context.route('http://renderer.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') {
        return route.fulfill({ contentType: 'text/html', body: fs.readFileSync('renderer-canary-sse.html') });
      }
      if (path === '/status') {
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-developer-0000', auth_mode: 'view_page_v2', readonly: true,
          state: 'idle', datetime: new Date().toISOString(),
        }) });
      }
      if (path === '/page/resume') {
        const body = request.postDataJSON();
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: body.page_id, code: body.page_id === 'shared-page' ? original.code : `alice-developer-0000-newCode${created}`,
          state: 'unlocked', can_write: true,
        }) });
      }
      if (path === '/page') {
        created += 1;
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
          page_id: `new-page-${created}`, page_token: `new-token-${created}`,
          code: `alice-developer-0000-N${String(created).padStart(7, '0')}`, state: 'locked',
        }) });
      }
      if (path === '/input') {
        inputs.push(request.postDataJSON());
        const ok = !rejectNextInput;
        rejectNextInput = false;
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok }) });
      }
      if (path === '/events') return route.abort();
      return route.fulfill({ status: 404, body: 'not found' });
    });

    const first = await context.newPage();
    const second = await context.newPage();
    await Promise.all([
      first.goto('http://renderer.test/renderer', { waitUntil: 'domcontentloaded' }),
      second.goto('http://renderer.test/renderer', { waitUntil: 'domcontentloaded' }),
    ]);
    const earlyWrites = await Promise.allSettled([
      first.evaluate(() => submitInput('early-first')),
      second.evaluate(() => submitInput('early-second')),
    ]);
    assert.strictEqual(earlyWrites.filter(result => result.status === 'fulfilled').length, 1,
      'both cloned tabs wrote before identity uniqueness settled');
    assert.strictEqual(inputs.length, 1, 'cloned unlocked token was used twice during tie-break');
    assert.strictEqual(inputs[0].page_id, 'shared-page');
    await first.waitForTimeout(800);
    const identities = await Promise.all([first, second].map(page => page.evaluate(() =>
      JSON.parse(sessionStorage.getItem('dw-view-page:alice-developer-0000')))));
    assert.notStrictEqual(identities[0].page_id, identities[1].page_id, 'cloned tabs retained one page identity');
    assert.ok(identities.some(identity => identity.page_id === 'shared-page'), 'tie-break replaced both tabs');
    assert.ok(created >= 1, 'tie-break did not allocate a replacement identity');
    const runtimeIDs = await Promise.all([first, second].map(page => page.evaluate(() => pageIdentity?.page_id)));
    const writable = runtimeIDs[0] === 'shared-page' ? first : second;
    await writable.waitForFunction(() => stats.readonly === false);
    await writable.evaluate(() => submitInput('multi\nline'));
    assert.strictEqual(inputs.length, 2);
    assert.strictEqual(inputs[1].page_id, 'shared-page');
    assert.strictEqual(inputs[1].page_token, 'shared-token');
    assert.match(inputs[1].request_id, /^[0-9a-f]{32}$/);
    assert.strictEqual(inputs[1].text, 'multi\nline');
    rejectNextInput = true;
    await assert.rejects(
      writable.evaluate(() => submitInput('must surface business rejection')),
      /input returned ok=false/,
      'HTTP 200 with ok=false was treated as a successful input',
    );
    assert.strictEqual(inputs.length, 3, 'business rejection did not reach the input endpoint exactly once');
    assert.ok(!first.url().includes('shared-token') && !second.url().includes('shared-token'), 'page token leaked into URL');
    await writable.locator('#humanInput').fill('typing survives status refresh');
    await writable.locator('#humanInput').focus();
    await writable.locator('#humanInput').evaluate(input => input.setSelectionRange(7, 15));
    await writable.evaluate(() => {
      for (let tick = 0; tick < 7; tick += 1) {
        applyStatusData({
          auth_mode: 'view_page_v2', readonly: true, can_write: false,
          access: '只读', session_state: 'readonly',
        });
      }
    });
    const focusState = await writable.locator('#humanInput').evaluate(input => ({
      active: document.activeElement === input,
      disabled: input.disabled,
      value: input.value,
      selectionStart: input.selectionStart,
      selectionEnd: input.selectionEnd,
    }));
    assert.deepStrictEqual(focusState, {
      active: true, disabled: false, value: 'typing survives status refresh', selectionStart: 7, selectionEnd: 15,
    }, 'v2 status refresh stole focus, selection, or writable page authority');
    assert.strictEqual(await writable.locator('#copyUnlockBtn').isVisible(), false,
      'v2 status refresh flashed the unlock button');
    assert.strictEqual(await writable.evaluate(() => stats.readonly), false,
      'v2 status refresh overrode page-level write authority');

    const restoreContext = await browser.newContext();
    const stale = { page_id: 'expired-page', page_token: 'expired-token', code: 'alice-developer-0000-expired1' };
    await restoreContext.addInitScript(identity => {
      sessionStorage.setItem('dw-view-page:alice-developer-0000', JSON.stringify(identity));
    }, stale);
    let restoreCreated = 0;
    let expireCurrent = false;
    await restoreContext.route('http://restore.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') {
        return route.fulfill({ contentType: 'text/html', body: fs.readFileSync('renderer-canary-sse.html') });
      }
      if (path === '/status') {
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-developer-0000', auth_mode: 'view_page_v2', readonly: true, state: 'idle',
        }) });
      }
      if (path === '/events') return route.abort();
      if (path === '/page/resume') {
        const body = request.postDataJSON();
        const expired = body.page_id === 'expired-page' || expireCurrent;
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: body.page_id, code: expired ? stale.code : `alice-developer-0000-R${restoreCreated}`,
          state: expired ? 'expired' : 'locked', can_write: false,
        }) });
      }
      if (path === '/page') {
        restoreCreated += 1;
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
          page_id: `restore-page-${restoreCreated}`, page_token: `restore-token-${restoreCreated}`,
          code: `alice-developer-0000-R${String(restoreCreated).padStart(7, '0')}`, state: 'locked',
        }) });
      }
      return route.fulfill({ status: 404, body: 'not found' });
    });
    const restored = await restoreContext.newPage();
    await restored.goto('http://restore.test/renderer', { waitUntil: 'domcontentloaded' });
    await restored.waitForFunction(() => pageIdentity?.page_id === 'restore-page-1');
    assert.strictEqual(restoreCreated, 1, 'expired resume did not replace the stale identity');
    expireCurrent = true;
    await restored.evaluate(() => dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
    await restored.waitForFunction(() => pageIdentity?.page_id === 'restore-page-2');
    await restored.waitForFunction(() => pageIdentityUnique === true);
    assert.strictEqual(restoreCreated, 2, 'persisted pageshow did not revalidate and replace the expired identity');
    assert.strictEqual(await restored.evaluate(() => pageIdentityUnique), true,
      'replacement identity did not settle uniqueness after bfcache restore');
    await restored.evaluate(() => applyStatusData({ session_state: 'busy' }));
    assert.strictEqual(await restored.locator('#taskProgress').isVisible(), true);
    await restored.evaluate(() => applyStatusData({ session_state: 'idle' }));
    assert.strictEqual(await restored.locator('#taskProgress').isVisible(), false);
    await restoreContext.close();
    console.log('renderer page identity PASS: cloned-tab tie-break + credentialed input');
  } finally {
    await browser.close();
  }
})().catch(error => {
  console.error(error);
  process.exit(1);
});
