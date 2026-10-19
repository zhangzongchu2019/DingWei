// Browser gate for Spec block colors and the public console theme crossing the live iframe boundary.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const ROOT = path.join(__dirname, '..');
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';
function template(file, start, end) {
  const source = fs.readFileSync(file, 'utf8');
  const a = source.indexOf(start); assert.notStrictEqual(a, -1, `${start} missing`);
  const b = source.indexOf(end, a + start.length); assert.notStrictEqual(b, -1, `${end} missing`);
  return source.slice(a + start.length, b);
}
let shell = template(path.join(ROOT, 'internal/m8/console_auth.go'), 'const consolePageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleAgents');
let specs = template(path.join(ROOT, 'internal/m8/console_specs_page.go'), 'const consoleSpecsPageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleSpecsPage');
specs = specs.replace('<script id=dw-md-core></script>', `<script>${fs.readFileSync(path.join(__dirname, 'md-render-core.js'), 'utf8')}</script>`);
function mutate(needle, replacement) {
  const before = `${shell}\n${specs}`;
  if (MUTATION === 'same-block-colors') specs = specs.split(needle).join(replacement);
  if (MUTATION === 'no-theme-receiver') specs = specs.split(needle).join(replacement);
  if (MUTATION === 'reload-spec-frame') shell = shell.split(needle).join(replacement);
  assert.notStrictEqual(`${shell}\n${specs}`, before, `mutation ${MUTATION} changed no bytes`);
}
if (MUTATION === 'same-block-colors') mutate('.spec-block-detail{background:#f8f5ff}.spec-block-new{background:#eef8f1}.spec-block-transfer{background:#fff7eb}', '.spec-block-detail{background:#f4f7fb}.spec-block-new{background:#f4f7fb}.spec-block-transfer{background:#f4f7fb}');
if (MUTATION === 'no-theme-receiver') mutate('dwSpecInitTheme();', 'dwSpecInitTheme();dwSpecApplyTheme=function(){};');
if (MUTATION === 'reload-spec-frame') mutate("function dwThemeToggle(){\n  var cur='light';", "function dwThemeToggle(){\n  var specFrame=document.querySelector('iframe[title=\"Specs workspace\"]');if(specFrame)specFrame.src=specFrame.src+'#theme';\n  var cur='light';");

const assetDir = path.join(ROOT, 'internal/m8/static/easymde');
const json = (res, value) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
const server = http.createServer((req, res) => {
    // R8 lookup added to the spec page; this legacy fixture has no active requests.
    if(req.method==='GET' && /^\/api\/console\/specs\/[^/?]+\/agent-review(?:\?|$)/.test(req.url)){res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({requests:[],agents:[]}));return;}

  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); return res.end(shell); }
  if (url.pathname === '/console/specs') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); return res.end(specs); }
  if (url.pathname === '/console/spec-assets/easymde.min.js') { res.writeHead(200, { 'Content-Type': 'text/javascript' }); return res.end(fs.readFileSync(path.join(assetDir, 'easymde.min.js'))); }
  if (url.pathname === '/console/spec-assets/easymde.min.css') { res.writeHead(200, { 'Content-Type': 'text/css' }); return res.end(fs.readFileSync(path.join(assetDir, 'easymde.min.css'))); }
  if (url.pathname === '/api/console/platform') return json(res, { role: 'owner', actions: ['read', 'submit'], items: [{ id: 'specs', kind: 'spec', title: 'Specs', href: '/console/specs' }] });
  if (url.pathname === '/api/console/agents') return json(res, { agents: [] });
  if (url.pathname === '/api/console/agent-groups') return json(res, { groups: [], owned_agents: [] });
  if (url.pathname === '/api/console/specs') return json(res, { role: 'owner', principal: 'owner-a', specs: [], page: 1, total_pages: 1 });
  res.writeHead(404); res.end('not found');
});

const luminance = (rgb) => {
  const parts = String(rgb).match(/[\d.]+/g).slice(0, 3).map((n) => { const c = Number(n) / 255; return c <= .03928 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4; });
  return .2126 * parts[0] + .7152 * parts[1] + .0722 * parts[2];
};
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  try {
    const page = await browser.newPage();
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a', { hasText: 'Specs' }).click();
    const iframe = page.locator('iframe[title="Specs workspace"]');
    await iframe.waitFor({ state: 'attached' });
    const frame = page.frames().find((f) => f.url().includes('/console/specs'));
    await frame.waitForSelector('#empty-create-spec');
    const colors = await frame.evaluate(() => ['list-screen', 'detail-screen', 'new-screen', 'transfer-screen'].map((id) => getComputedStyle(document.getElementById(id)).backgroundColor));
    assert.strictEqual(new Set(colors).size, 4, `light block background distinct count must equal 4: ${colors}`);
    await frame.locator('#empty-create-spec').click();
    await frame.locator('#new-title').fill('state-must-survive');
    const before = await frame.evaluate(() => ({ theme: document.documentElement.dataset.theme, body: getComputedStyle(document.body).backgroundColor, colors: ['list-screen', 'detail-screen', 'new-screen', 'transfer-screen'].map((id) => getComputedStyle(document.getElementById(id)).backgroundColor) }));
    const srcBefore = await iframe.getAttribute('src');
    await page.locator('#themeBtn').click();
    // 🔴 原先这里是默认 30s 超时,变异 no-theme-receiver 的红是一条 TimeoutError:
    //    等 30 秒、而且报文说不出「哪条判据坏了」。改成短超时后落到下面那条显式断言上。
    try { await frame.waitForFunction(() => document.documentElement.dataset.theme === 'dark', null, { timeout: 3000 }); } catch (_) { /* 让判定交给下面的断言 */ }
    const after = await frame.evaluate(() => ({ theme: document.documentElement.dataset.theme, body: getComputedStyle(document.body).backgroundColor, value: document.getElementById('new-title').value, colors: ['list-screen', 'detail-screen', 'new-screen', 'transfer-screen'].map((id) => getComputedStyle(document.getElementById(id)).backgroundColor) }));
    const srcAfter = await iframe.getAttribute('src');
    assert.strictEqual(after.theme, 'dark', 'iframe html data-theme must follow the shell');
    assert.ok(luminance(after.body) < luminance(before.body) * .5, `iframe body must actually darken: ${before.body} -> ${after.body}`);
    assert.strictEqual(new Set(after.colors).size, 4, `dark block background distinct count must equal 4: ${after.colors}`);
    assert.strictEqual(srcAfter, srcBefore, `iframe src changed: ${srcBefore} -> ${srcAfter}`);
    assert.strictEqual(after.value, 'state-must-survive', 'existing iframe state was lost during theme switch');
    // 🔵 业主的真实动线是【先切暗、再打开 Specs】,不是先开 Specs 再切主题。
    //    这条动线此前没有格:产品是对的(初值走 localStorage),但没有任何东西钉住它。
    const fresh = await browser.newPage();
    await fresh.goto(`http://127.0.0.1:${server.address().port}/console`);
    await fresh.locator('#themeBtn').click();
    await fresh.locator('#platform-items a', { hasText: 'Specs' }).click();
    await fresh.locator('iframe[title="Specs workspace"]').waitFor({ state: 'attached' });
    const freshFrame = fresh.frames().find((f) => f.url().includes('/console/specs'));
    await freshFrame.waitForSelector('#empty-create-spec');
    const opened = await freshFrame.evaluate(() => ({ theme: document.documentElement.dataset.theme, body: getComputedStyle(document.body).backgroundColor }));
    assert.strictEqual(opened.theme, 'dark', '先切暗再打开的 Specs 必须直接是当前主题,不能先白一下再变');
    assert.ok(luminance(opened.body) < luminance(before.body) * .5, `新开的 Specs 必须直接是暗底: ${opened.body}`);
    await fresh.close();
    console.log(`PASS spec theme: light=${colors.join('|')} dark=${after.colors.join('|')} src=${srcAfter} state=kept fresh-open=${opened.theme}`);
  } finally { await browser.close(); server.close(); }
})().catch((error) => { console.error(error); server.close(); process.exitCode = 1; });
