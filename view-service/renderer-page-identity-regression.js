const { chromium } = require('playwright');
const fs = require('fs');
const assert = require('assert');

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext();
    const original = { page_id: 'shared-page', page_token: 'shared-token', code: 'alice-dev55-2642-a3Kf9xQ2' };
    await context.addInitScript(identity => {
      sessionStorage.setItem('dw-view-page:alice-dev55-2642', JSON.stringify(identity));
    }, original);
    let created = 0;
    const inputs = [];
    await context.route('http://renderer.test/**', async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      if (path === '/renderer') {
        return route.fulfill({ contentType: 'text/html', body: fs.readFileSync('renderer-canary-sse.html') });
      }
      if (path === '/status') {
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'alice-dev55-2642', auth_mode: 'view_page_v2', readonly: true,
          state: 'idle', datetime: new Date().toISOString(),
        }) });
      }
      if (path === '/page/resume') {
        const body = request.postDataJSON();
        return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          page_id: body.page_id, code: body.page_id === 'shared-page' ? original.code : `alice-dev55-2642-newCode${created}`,
          state: 'unlocked', can_write: true,
        }) });
      }
      if (path === '/page') {
        created += 1;
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
          page_id: `new-page-${created}`, page_token: `new-token-${created}`,
          code: `alice-dev55-2642-N${String(created).padStart(7, '0')}`, state: 'locked',
        }) });
      }
      if (path === '/input') {
        inputs.push(request.postDataJSON());
        return route.fulfill({ contentType: 'application/json', body: '{"ok":true}' });
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
    await first.waitForTimeout(800);
    const identities = await Promise.all([first, second].map(page => page.evaluate(() =>
      JSON.parse(sessionStorage.getItem('dw-view-page:alice-dev55-2642')))));
    assert.notStrictEqual(identities[0].page_id, identities[1].page_id, 'cloned tabs retained one page identity');
    assert.ok(identities.some(identity => identity.page_id === 'shared-page'), 'tie-break replaced both tabs');
    assert.ok(created >= 1, 'tie-break did not allocate a replacement identity');
    const runtimeIDs = await Promise.all([first, second].map(page => page.evaluate(() => pageIdentity?.page_id)));
    const writable = runtimeIDs[0] === 'shared-page' ? first : second;
    await writable.waitForFunction(() => stats.readonly === false);
    await writable.evaluate(() => submitInput('multi\nline'));
    assert.strictEqual(inputs.length, 1);
    assert.strictEqual(inputs[0].page_id, 'shared-page');
    assert.strictEqual(inputs[0].page_token, 'shared-token');
    assert.match(inputs[0].request_id, /^[0-9a-f]{32}$/);
    assert.strictEqual(inputs[0].text, 'multi\nline');
    assert.ok(!first.url().includes('shared-token') && !second.url().includes('shared-token'), 'page token leaked into URL');
    console.log('renderer page identity PASS: cloned-tab tie-break + credentialed input');
  } finally {
    await browser.close();
  }
})().catch(error => {
  console.error(error);
  process.exit(1);
});
