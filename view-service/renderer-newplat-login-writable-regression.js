// R-V2-VIEW-LOGIN-WRITABLE-20261004052331
// The capability response is materialized by the real Go new-platform handler. This harness
// stubs only view-service status/page/input/SSE around the production renderer; it does not
// mutate readonly state or invent a capability success response.
const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');

const ROOT = path.join(__dirname, '..');
const payloadPath = path.join(os.tmpdir(), `dw-view-login-cap-${process.pid}.json`);
try {
  execFileSync('go', ['test', './internal/m8', '-run', '^TestNewPlatformViewCapabilityBrowserPayload$', '-count=1'], {
    cwd: ROOT,
    env: { ...process.env, DW_VIEW_LOGIN_CAPABILITY_PAYLOAD_OUT: payloadPath },
    stdio: ['ignore', 'pipe', 'inherit'],
  });
  const realCapability = JSON.parse(fs.readFileSync(payloadPath, 'utf8'));
  if (realCapability.status === 200) {
    assert.strictEqual(realCapability.body.page_id, 'browser-view-page');
    assert.strictEqual(realCapability.input_status, 200);
    assert.strictEqual(realCapability.input_body.verified, '1');
    assert.strictEqual(realCapability.input_body.principal, 'zzc');
  }

  (async () => {
    const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
    try {
      const page = await browser.newPage();
      let capabilityRequests = 0;
      const inputs = [];
      await page.route('http://view-login.test/**', async route => {
        const req = route.request();
        const p = new URL(req.url()).pathname;
        if (p === '/renderer') return route.fulfill({ contentType: 'text/html', body: fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html')) });
        if (p === '/status') return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
          session: 'kimi.zzc', auth_mode: 'view_page_v2', readonly: true, can_write: false, state: 'idle',
        }) });
        if (p === '/page') return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
          page_id: 'browser-view-page', page_token: 'browser-page-token', code: 'KIMIZZZ1', state: 'locked', can_write: false,
        }) });
        if (p === '/page/resume') return route.fulfill({ status: 404, body: 'not found' });
        if (p === '/api/console/agents/kimi.zzc/capability') {
          capabilityRequests += 1;
          assert.deepStrictEqual(req.postDataJSON(), { page_id: 'browser-view-page' });
          return route.fulfill({ status: realCapability.status, contentType: 'application/json', body: JSON.stringify(realCapability.body) });
        }
        if (p === '/input') {
          inputs.push({ headers: req.headers(), body: req.postDataJSON() });
          return route.fulfill({ status: realCapability.input_status, contentType: 'application/json', body: JSON.stringify(realCapability.input_body) });
        }
        if (p === '/events') return route.abort();
        return route.fulfill({ status: 404, body: 'not found' });
      });
      await page.goto('http://view-login.test/renderer', { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => stats.authMode === 'view_page_v2' && stats.readonly === false);
      assert.strictEqual(await page.locator('#copyUnlockBtn').isVisible(), false, 'login capability still showed the unlock command');
      await page.evaluate(() => submitInput('owner typed'));
      assert.strictEqual(capabilityRequests, 1, 'renderer did not use the real capability response exactly once');
      assert.strictEqual(inputs.length, 1);
      assert.strictEqual(inputs[0].headers['x-dw-console-page'], 'browser-view-page');
      assert.strictEqual(inputs[0].headers['x-dw-console-capability'], realCapability.body.capability);
      assert.strictEqual(inputs[0].body.text, 'owner typed');
      console.log('PASS new-platform console login makes owned view writable without #unlock');
    } finally {
      await browser.close();
    }
  })().catch(err => { console.error(err); process.exitCode = 1; });
} finally {
  try { fs.unlinkSync(payloadPath); } catch (_) {}
}
