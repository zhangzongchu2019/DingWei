// Browser-level north-star gate for the real console specs management page.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_specs_page.go');
const ASSET_DIR = path.join(__dirname, '..', 'internal', 'm8', 'static', 'easymde');
const MD_CORE = path.join(__dirname, 'md-render-core.js');
const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
const START = 'const consoleSpecsPageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleSpecsPage';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice('--mutate='.length) || '';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consoleSpecsPageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consoleSpecsPageHTML end marker missing');
  let html = source.slice(contentStart, end).replace(
    '<script id=dw-md-core></script>', () => `<script>${fs.readFileSync(MD_CORE, 'utf8')}</script>`);
  if (MUTATION === 'no-render') html = html.replace('renderSpec(data.revision.content_markdown);', '');
  if (MUTATION === 'no-wire') html = html.replace('wireSpecRender(root);', '');
  if (MUTATION === 'no-copy') html = html.replace("root.querySelectorAll('.copy-code')", "root.querySelectorAll('.copy-code-disabled')");
  if (MUTATION === 'no-server-error') html = html.replace("return prefix+' '+r.status+(detail?':'+detail:'');", "return prefix;");
  if (MUTATION && !['no-render', 'no-wire', 'no-copy', 'no-server-error'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
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
    if (req.url === '/view/parity-session') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(fs.readFileSync(RENDERER)); return;
    }
    if (req.url === '/view/parity-session/status') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ session: 'parity-session', auth_mode: 'legacy', readonly: true, state: 'idle' })); return;
    }
    if (req.url === '/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png') {
      res.writeHead(200, { 'Content-Type': 'image/png' });
      res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')); return;
    }
    if (req.url === '/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png') {
      res.writeHead(404, { 'Content-Type': 'text/plain' }); res.end('gone'); return;
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
      if (body.title === 'server-reject') {
        res.writeHead(400, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server create detail'); return;
      }
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
      if (body.content_markdown === 'server-save-reject') {
        res.writeHead(409, { 'Content-Type': 'text/plain; charset=utf-8' }); res.end('server save detail'); return;
      }
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
    const context = await browser.newContext();
    await context.addInitScript(() => { Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (text) => { globalThis.__copiedCode = text; } }, configurable: true }); });
    const page = await context.newPage(); page.setDefaultTimeout(2500);
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await page.goto(`http://127.0.0.1:${port}/console/specs`, { waitUntil: 'domcontentloaded' });
    const validNew = { title: 'Valid', markdown: '# Valid', acceptance: 'accepted', anchor: 'msg:valid' };
    async function setNew(values) {
      await page.fill('#new-title', values.title);
      await page.evaluate((value) => window.specEditors.new.value(value), values.markdown);
      await page.fill('#new-acceptance', values.acceptance);
      await page.fill('#new-anchor', values.anchor);
    }
    for (const [field, selector, label] of [
      ['title', '#new-title', 'Title'], ['markdown', '#new-content', 'Markdown'],
      ['acceptance', '#new-acceptance', 'Acceptance'], ['anchor', '#new-anchor', 'Owner message anchor'],
    ]) {
      await setNew({ ...validNew, [field]: '' });
      await page.click('#create-spec');
      await page.locator('#create-status').filter({ hasText: label }).waitFor({ state: 'visible' });
      assert.strictEqual(specs.length, 0, `missing ${field} must be blocked before POST`);
    }
    await setNew({ ...validNew, title: 'server-reject' });
    await page.click('#create-spec');
    await page.locator('#create-status').filter({ hasText: '创建失败 400:server create detail' }).waitFor({ state: 'visible' });
    assert.strictEqual(specs.length, 0, 'server-rejected create must not create a spec');
    assert.strictEqual(await page.locator('label .required').count(), 8, 'both create and edit forms must mark all four required fields');
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
      await page.locator('#spec-rendered h1').filter({ hasText: title }).waitFor({ state: 'visible' });
      const changed = title === 'Spec A'
        ? `# ${title} changed\n\n\`\`\`python\nprint("copy-me")\n\`\`\`\n\n![good](http://127.0.0.1:${port}/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png)\n\n![missing](http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png)`
        : `# ${title} changed`;
      await page.evaluate((value) => window.specEditors.edit.value(value), changed);
      await page.click('#save-spec');
      await page.locator('#edit-status').filter({ hasText: 'saved revision 2' }).waitFor({ state: 'visible' });
      if (title === 'Spec A') {
        await page.locator('#spec-rendered h1').filter({ hasText: 'Spec A changed' }).waitFor({ state: 'visible' });
        await page.locator('#spec-rendered img.md-img').waitFor({ state: 'visible' });
        await page.locator('#spec-rendered .md-image-fallback').waitFor({ state: 'visible' });
        assert.strictEqual(await page.locator('#spec-rendered img.md-img').count(), 1, 'working image must remain inline');
        assert.strictEqual(await page.locator('#spec-rendered .md-image-fallback').getAttribute('href'), `http://127.0.0.1:${port}/f/spec/missing/26/0921/ffffffffffffffffffffffffffffffff.png`, 'failed image must retain a direct download exit');
        assert.strictEqual(await page.locator('#spec-rendered img.md-img').evaluate((el) => getComputedStyle(el).maxHeight), '420px', 'shared renderer CSS contract must be present');
        await page.locator('#spec-rendered .copy-code').click();
        assert.strictEqual(await page.evaluate(() => globalThis.__copiedCode), 'print("copy-me")', 'Copy button must be wired to the rendered code');
        const timeline = await context.newPage();
        await timeline.goto(`http://127.0.0.1:${port}/view/parity-session`, { waitUntil: 'domcontentloaded' });
        const timelineRendered = await timeline.evaluate((markdown) => renderMarkdown(markdown), changed);
        const specCoreRendered = await page.evaluate((markdown) => DwMdCore.renderMarkdown(markdown), changed);
        assert.strictEqual(specCoreRendered, timelineRendered, 'spec and timeline must render identical bytes at the real spec origin');
        assert.strictEqual(await page.locator('#spec-rendered > .md').count(), 1, 'the spec page must mount the shared renderer output into its visible DOM');
        assert.strictEqual(await page.evaluate((origin) => DwMdCore.isOwnFileUrl(origin + '/f/spec/good/26/0921/0123456789abcdef0123456789abcdef.png'), `http://127.0.0.1:${port}`), true, 'shared own-file boundary must use the real spec page origin');
        await timeline.close();
      }
    }
    await page.evaluate(() => openSpec('spec-2'));
    await page.evaluate(() => window.specEditors.edit.value('server-save-reject'));
    await page.click('#save-spec');
    await page.waitForTimeout(100);
    assert.strictEqual(await page.locator('#edit-status').textContent(), '保存失败 409:server save detail', 'save must expose status and server response body');
    assert.strictEqual(specs.find((item) => item.title === 'Spec B').current_revision, 2, 'server-rejected save must not create a revision');
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
    assert.deepStrictEqual(pageErrors, [], `spec page emitted top-level errors: ${JSON.stringify(pageErrors)}`);
    console.log(`editor wrap metrics overflow=${wrapMetrics.overflow} lineOverhang=${wrapMetrics.lineOverhang}`);
    console.log('console specs browser: passed=14 failed=0');
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => { console.error(error); process.exit(1); });
