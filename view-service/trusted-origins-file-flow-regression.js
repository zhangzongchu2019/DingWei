// Trusted-origin Console upload and canonical file-link browser regression.
// Negative controls: --mutate-send-normalization and --mutate-own-url.
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

const rendererPath = path.join(__dirname, 'renderer-canary-sse.html');
const canonical = 'https://dw123.cmjrt.com';
const mutantSend = process.argv.includes('--mutate-send-normalization');
const mutantOwn = process.argv.includes('--mutate-own-url');
let html = fs.readFileSync(rendererPath, 'utf8');
if (mutantSend) html = html.replace(".replace(FILE_URL_RE, (url) => canonicalFileUrl(url));", '');
if (mutantOwn) html = html.replace("(u.origin === location.origin || (canonicalPublicOrigin() && u.origin === canonicalPublicOrigin()))", 'u.origin === location.origin');
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/Sx8AAAAASUVORK5CYII=', 'base64');

(async () => {
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  try {
    for (const port of [443, 50443]) {
      const origin = port === 443 ? canonical : `${canonical}:${port}`;
      const context = await browser.newContext({ ignoreHTTPSErrors: true });
      await context.addInitScript((value) => { globalThis.DW_CANONICAL_PUBLIC_ORIGIN = value; }, canonical);
      const page = await context.newPage();
      let submitted = null;
      let uploadRequestUrl = '';
      await page.route('**/*', async (route) => {
        const req = route.request();
        const u = new URL(req.url());
        if (u.pathname === '/renderer-canary-sse.html') return route.fulfill({ contentType: 'text/html', body: html });
        if (u.pathname.endsWith('/status')) return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ session: 'flow-session', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture' }) });
        if (u.pathname.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
        if (u.pathname.endsWith('/file-upload-ticket')) {
          return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ upload_url: `${origin}/files/upload`, ticket: 'fixture-ticket', session: 'flow-session' }) });
        }
        if (u.pathname === '/files/upload') {
          uploadRequestUrl = req.url();
          return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ url: `${canonical}/f/owner/session/image.png`, name: 'image.png', size: 68, media_type: 'image/png' }) });
        }
        if (u.pathname === '/f/owner/session/image.png') return route.fulfill({ contentType: 'image/png', body: png });
        if (u.pathname.endsWith('/input')) {
          submitted = JSON.parse(req.postData() || '{}');
          return route.fulfill({ contentType: 'application/json', body: '{"ok":true}' });
        }
        if (u.pathname.endsWith('/taskboard')) return route.fulfill({ status: 404, body: '' });
        return route.continue();
      });
      await page.goto(`${origin}/renderer-canary-sse.html`, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false);
      await page.setInputFiles('#filePicker', { name: 'image.png', mimeType: 'image/png', buffer: png });
      await page.waitForFunction(() => document.querySelector('#humanInput').value.includes('/f/owner/session/image.png'));
      assert.strictEqual(uploadRequestUrl, `${origin}/files/upload`, `browser upload endpoint on ${port}`);
      await page.waitForFunction(() => document.querySelector('#draftAttachments img')?.complete && document.querySelector('#draftAttachments img')?.naturalWidth > 0, null, { timeout: 10000 });
      const displayed = await page.locator('#draftAttachments img').getAttribute('src');
      assert.ok(displayed.startsWith(`${origin}/f/`), `draft preview should be local on ${port}: ${displayed}`);
      await page.evaluate((localOrigin) => {
        const input = document.getElementById('humanInput');
        input.value = input.value.replace('https://dw123.cmjrt.com/f/', `${localOrigin}/f/`);
      }, origin);
      await page.locator('#sendBtn').click();
      await page.waitForFunction(() => document.getElementById('humanInput').value === '');
      assert.ok(submitted && submitted.text.includes(`${canonical}/f/`), `sent body should persist canonical URL on ${port}: ${JSON.stringify(submitted)}`);
      await page.evaluate((text) => handleIncomingEvent({ kind: 'user_input', text, ts: Date.now() }), submitted.text);
      await page.waitForFunction(() => document.querySelector('.attachments img')?.getAttribute('src'));
      const bodySrc = await page.locator('.attachments img').first().getAttribute('src');
      assert.ok(bodySrc.startsWith(`${origin}/f/`), `sent image should display local on ${port}: ${bodySrc}`);
      console.log(`PASS trusted-origin file flow port=${port} upload=${uploadRequestUrl} draft=${displayed} stored=${submitted.text} rendered=${bodySrc}`);
      await context.close();
    }
  } finally { await browser.close(); }
})().catch((error) => { console.error(error.stack || error); process.exitCode = 1; });
