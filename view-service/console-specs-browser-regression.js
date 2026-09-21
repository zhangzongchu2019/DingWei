// Browser-level north-star gate for the real console specs management page.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_specs_page.go');
const ASSET_DIR = path.join(__dirname, '..', 'internal', 'm8', 'static', 'easymde');
const START = 'const consoleSpecsPageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleSpecsPage';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consoleSpecsPageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consoleSpecsPageHTML end marker missing');
  return source.slice(contentStart, end);
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

async function requestBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  return chunks.length ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : {};
}

async function main() {
  const html = productionHTML();
  const specs = [];
  const revisions = new Map();
  const server = http.createServer(async (req, res) => {
    if (req.url === '/console/specs') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return;
    }
    if (req.url === '/console/spec-assets/easymde.min.js') {
      res.writeHead(200, { 'Content-Type': 'text/javascript' }); res.end(fs.readFileSync(path.join(ASSET_DIR, 'easymde.min.js'))); return;
    }
    if (req.url === '/console/spec-assets/easymde.min.css') {
      res.writeHead(200, { 'Content-Type': 'text/css' }); res.end(fs.readFileSync(path.join(ASSET_DIR, 'easymde.min.css'))); return;
    }
    if (req.url === '/api/console/specs' && req.method === 'GET') {
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ specs })); return;
    }
    if (req.url === '/api/console/specs' && req.method === 'POST') {
      const body = await requestBody(req);
      const item = { id: `spec-${specs.length + 1}`, title: body.title, state: 'draft', current_revision: 1 };
      specs.push(item); revisions.set(item.id, { ...body, spec_id: item.id, revision: 1,
        acceptance_json: JSON.stringify(body.acceptance), input_anchors_json: JSON.stringify(body.input_anchors) });
      res.writeHead(201, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(item)); return;
    }
    const match = String(req.url || '').match(/^\/api\/console\/specs\/([^/]+)$/);
    if (match && req.method === 'GET') {
      const item = specs.find((candidate) => candidate.id === match[1]);
      if (!item) { res.writeHead(404); res.end(); return; }
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ spec: item, revision: revisions.get(item.id) })); return;
    }
    if (match && req.method === 'PATCH') {
      const body = await requestBody(req); const item = specs.find((candidate) => candidate.id === match[1]);
      item.current_revision += 1; item.state = 'needs_improvement';
      revisions.set(item.id, { ...body, spec_id: item.id, revision: item.current_revision,
        acceptance_json: JSON.stringify(body.acceptance), input_anchors_json: JSON.stringify(body.input_anchors) });
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ revision: item.current_revision })); return;
    }
    res.writeHead(404); res.end('not found');
  });

  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  try {
    const page = await browser.newPage(); page.setDefaultTimeout(2500);
    await page.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
    for (const title of ['Spec A', 'Spec B']) {
      await page.fill('#new-title', title);
      await page.evaluate((value) => window.specEditors.new.value(value), `# ${title}`);
      await page.fill('#new-acceptance', `${title} accepted`);
      await page.fill('#new-anchor', `msg:${title.slice(-1)}`);
      await page.click('#create-spec');
      await page.locator(`#spec-list [data-title="${title}"]`).waitFor({ state: 'visible' });
    }
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 2, 'true page must show both specs at once');
    for (const title of ['Spec A', 'Spec B']) {
      await page.locator(`#spec-list [data-title="${title}"] .edit-spec`).click();
      await page.waitForFunction((want) => document.getElementById('edit-title').value === want, title);
      assert.strictEqual(await page.inputValue('#edit-title'), title, `edit form must open ${title}`);
      await page.evaluate((value) => window.specEditors.edit.value(value), `# ${title} changed`);
      await page.click('#save-spec');
      await page.locator('#edit-status').filter({ hasText: 'saved revision 2' }).waitFor({ state: 'visible' });
    }
    await page.reload({ waitUntil: 'domcontentloaded' });
    assert.strictEqual(await page.locator('#spec-list [data-spec-id]').count(), 2, 'reload must retain both specs');
    const editorContract = await page.evaluate(() => ({
      spell: window.specEditors.new.options.spellChecker,
      fa: window.specEditors.new.options.autoDownloadFontAwesome,
      autosave: window.specEditors.new.options.autosave.enabled,
      previewButtons: document.querySelectorAll('.editor-toolbar .preview,.editor-toolbar .side-by-side').length,
      autosaveKeys: Object.keys(localStorage).filter((key) => /easymde|smde/i.test(key)),
      lineTag: document.querySelector('.CodeMirror-line').tagName,
      lineWhiteSpace: getComputedStyle(document.querySelector('.CodeMirror-line')).whiteSpace,
    }));
    assert.deepStrictEqual(editorContract, { spell: false, fa: false, autosave: false, previewButtons: 0, autosaveKeys: [], lineTag: 'PRE', lineWhiteSpace: 'pre-wrap' });
    const button = page.locator('.editor-toolbar .dw-bold').first();
    const normal = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    await button.hover();
    const hover = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    assert.notStrictEqual(hover, normal, 'toolbar hover background must be explicit and visible');
    await button.evaluate((el) => el.classList.add('active'));
    const active = await button.evaluate((el) => getComputedStyle(el).backgroundColor);
    assert.notStrictEqual(active, normal, 'toolbar active background must be explicit and visible');
    const wrapMetrics = await page.evaluate(() => {
      window.specEditors.new.value('x'.repeat(800));
      const scroll = document.querySelector('.CodeMirror-scroll');
      const line = document.querySelector('.CodeMirror-line');
      return { overflow: scroll.scrollWidth - scroll.clientWidth,
        lineOverhang: Math.max(0, Math.round(line.getBoundingClientRect().right - scroll.getBoundingClientRect().right)) };
    });
    assert.strictEqual(wrapMetrics.overflow, 0, `editor line overflowed: ${JSON.stringify(wrapMetrics)}`);
    assert.strictEqual(wrapMetrics.lineOverhang, 0, `editor line overhung viewport: ${JSON.stringify(wrapMetrics)}`);
    console.log(`editor wrap metrics overflow=${wrapMetrics.overflow} lineOverhang=${wrapMetrics.lineOverhang}`);
    console.log('console specs browser: passed=2 failed=0');
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => { console.error(error); process.exit(1); });
