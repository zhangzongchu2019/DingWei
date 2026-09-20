// 0.2.1.35-⑮ terminal leg: extract terminalPageHTML from Go source so the
// browser test cannot drift onto a copied fixture.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'terminal.go');

function terminalHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const marker = 'const terminalPageHTML = `';
  const start = source.indexOf(marker);
  assert.notStrictEqual(start, -1, 'terminalPageHTML start marker missing');
  const contentStart = start + marker.length;
  const end = source.lastIndexOf('`');
  assert.notStrictEqual(end, -1, 'terminalPageHTML end marker missing');
  return source.slice(contentStart, end)
    .replaceAll('%%', '%')
    .replaceAll('%s', 'storage-denied-terminal-2642');
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

(async () => {
  const html = terminalHTMLFromProductionSource();
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://terminal.test').pathname;
    if (pathname === '/view/storage-denied-terminal-2642') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(html);
      return;
    }
    if (pathname === '/xterm/xterm.min.css') {
      res.writeHead(200, { 'Content-Type': 'text/css' }); res.end(''); return;
    }
    if (pathname === '/xterm/xterm.min.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' });
      res.end(`class Terminal { constructor(){this.cols=80;this.rows=24} loadAddon(){} open(el){el.dataset.xtermReady='1'} onData(){} write(){} dispose(){} }`);
      return;
    }
    if (pathname === '/xterm/addon-fit.min.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' });
      res.end(`window.FitAddon={FitAddon:class { fit(){} }};`);
      return;
    }
    res.writeHead(404); res.end('not found');
  });
  const port = await listen(server);
  let browser;
  try {
    browser = await launchChromium();
    const context = await browser.newContext();
    await context.addInitScript(() => {
      Object.defineProperty(window, 'localStorage', {
        configurable: true,
        get() { throw new DOMException('localStorage denied by policy', 'SecurityError'); },
      });
      class FixtureWebSocket {
        static OPEN = 1;
        constructor() { this.readyState = 0; }
        send() {}
        close() { this.readyState = 3; }
      }
      window.WebSocket = FixtureWebSocket;
    });
    const page = await context.newPage();
    const pageErrors = [];
    page.on('pageerror', err => pageErrors.push(err.message));
    await page.goto(`http://127.0.0.1:${port}/view/storage-denied-terminal-2642`, { waitUntil: 'domcontentloaded' });
    const checks = regressionCollector();
    await checks.check('T1 denied localStorage does not abort terminal script', async () => {
      assert.deepStrictEqual(pageErrors, [], `top-level page errors: ${pageErrors.join(' | ')}`);
      assert.strictEqual(await page.evaluate(() => typeof term !== 'undefined'), true, 'term binding missing');
    });
    await checks.check('T2 xterm instance opens into terminal host', async () => {
      assert.strictEqual(await page.locator('#terminal').getAttribute('data-xterm-ready'), '1');
      assert.strictEqual(await page.locator('#scrollback').inputValue(), '2000', 'denied storage must use default scrollback');
    });
    checks.finish();
    console.log('terminal storage-denied regression: ALL PASS (2/2)');
    await context.close();
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(finishRegression);
