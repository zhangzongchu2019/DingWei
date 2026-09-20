// Console browser harness: serves the real consolePageHTML embedded in
// internal/m8/console_auth.go and replaces only its HTTP dependencies.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML end marker missing');
  return source.slice(contentStart, end);
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

async function main() {
  const html = consoleHTMLFromProductionSource();
  const agents = [
    { session: 'owner-alpha-2642', online: true },
    { session: 'owner-offline-2642', online: false },
  ];
  const server = http.createServer((req, res) => {
    if (req.url === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      res.end(html);
      return;
    }
    if (req.url === '/api/console/agents') {
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ agents }));
      return;
    }
    if (req.url && req.url.startsWith('/view/')) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end('<!doctype html><title>fixture agent view</title>');
      return;
    }
    res.writeHead(404);
    res.end('not found');
  });

  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  let passed = 0;
  const failures = [];
  async function test(name, fn) {
    try {
      await fn();
      passed += 1;
      console.log(`  PASS ${name}`);
    } catch (error) {
      failures.push(`${name}: ${error.message}`);
      console.log(`  FAIL ${name}: ${error.message}`);
    }
  }

  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    page.setDefaultTimeout(1500);
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });

    await test('loads the real console document', async () => {
      assert.strictEqual(await page.title(), 'DingWei Agent Console');
      await page.locator('nav #a').waitFor({ state: 'attached' });
    });

    await test('renders online and offline agents from the console API', async () => {
      const online = page.locator('nav button[title="owner-alpha-2642"]');
      const offline = page.locator('nav button[title="owner-offline-2642"]');
      await online.waitFor({ state: 'visible' });
      await offline.waitFor({ state: 'attached' });
      assert.strictEqual(await online.isEnabled(), true);
      assert.strictEqual(await offline.isDisabled(), true);
    });

    await test('opens a real console tab and agent iframe', async () => {
      await page.locator('nav button[title="owner-alpha-2642"]').click();
      const tab = page.locator('#tabs .tab[title="owner-alpha-2642"]');
      const pane = page.locator('#panes iframe[title="Agent view: owner-alpha-2642"]');
      await tab.waitFor({ state: 'visible' });
      assert.strictEqual(await tab.evaluate((node) => node.classList.contains('on')), true);
      assert.strictEqual(new URL(await pane.getAttribute('src'), page.url()).pathname, '/view/owner-alpha-2642');
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
  if (failures.length) {
    console.error(failures.join('\n'));
    process.exit(1);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(2);
});
