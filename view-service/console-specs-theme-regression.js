// Browser gate for the public console theme crossing the live iframe boundary.
// S-R62-CONSOLE-SLIM：Specs 菜单入口已按业主裁定摘除（后端路由保留）。本套件原先钉在
// Specs iframe 上的「主题跨边界」判据改钉在保留直连路径（/console?task_manager=1 ⇒
// openTaskBoardWorkspace）打开的任务管理 iframe 上；原「四色块互异」判据换成
// 「面板底色随主题真实明暗变化」。判据本身不放宽：跟随、不重载、状态不丢、先暗后开。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const ROOT = path.join(__dirname, '..');
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';
const allowedMutations = new Set(['', 'same-block-colors', 'no-theme-receiver', 'reload-task-frame']);
if (!allowedMutations.has(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
function template(file, start, end) {
  const source = fs.readFileSync(file, 'utf8');
  const a = source.indexOf(start); assert.notStrictEqual(a, -1, `${start} missing`);
  const b = source.indexOf(end, a + start.length); assert.notStrictEqual(b, -1, `${end} missing`);
  return source.slice(a + start.length, b);
}
let shell = template(path.join(ROOT, 'internal/m8/console_auth.go'), 'const consolePageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleAgents');
shell = shell.split('{{CONSOLE_VERSION}}').join('test').split('{{CONSOLE_COMMIT}}').join('test').split('{{CONSOLE_VERSION_TEXT}}').join('test');
let board = template(path.join(ROOT, 'internal/m8/console_task_board_page.go'), 'const consoleTaskBoardPageHTML = `', '`\n');
function mutate(needle, replacement) {
  const before = `${shell}\n${board}`;
  if (MUTATION === 'same-block-colors') board = board.split(needle).join(replacement);
  if (MUTATION === 'no-theme-receiver') board = board.split(needle).join(replacement);
  if (MUTATION === 'reload-task-frame') shell = shell.split(needle).join(replacement);
  assert.notStrictEqual(`${shell}\n${board}`, before, `mutation ${MUTATION} changed no bytes`);
}
// 刀1：暗色面板底色改成与亮色同值 ⇒「面板必须真实变暗」断言红。
if (MUTATION === 'same-block-colors') mutate('--bg:#111316;--panel:#1b1f24;', '--bg:#111316;--panel:#f6f8fb;');
// 刀2：初始化后把 apply 换成空函数 ⇒ 广播不再落地，「跟随外壳」断言红（短超时+显式断言报因）。
if (MUTATION === 'no-theme-receiver') mutate('dwTaskInitTheme();', 'dwTaskInitTheme();dwTaskApplyTheme=function(){};');
// 刀3：外壳切主题时顺手重载任务管理 iframe ⇒「src 不变／状态不丢」断言红。
if (MUTATION === 'reload-task-frame') mutate("function dwThemeToggle(){\n  var cur='light';", "function dwThemeToggle(){\n  var taskFrame=document.querySelector('iframe[title=\"任务管理\"]');if(taskFrame)taskFrame.src=taskFrame.src+'#theme';\n  var cur='light';");

const json = (res, value) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
const html = (res, body) => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(body); };
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') return html(res, shell);
  if (url.pathname === '/console/tasks') return html(res, board);
  // S-R62-CONSOLE-SLIM：夹具与产品新形状同形（菜单只留 agent-groups）。
  if (url.pathname === '/api/console/platform') return json(res, { role: 'owner', actions: ['read'], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 管理', href: '#agent-groups', status: 'approved' }] });
  if (url.pathname === '/api/console/platform/users') return json(res, { users: [] });
  if (url.pathname === '/api/console/agents') return json(res, { agents: [] });
  if (url.pathname === '/api/console/agent-groups') return json(res, { groups: [], owned_agents: [] });
  if (url.pathname === '/api/console/task-board') return json(res, { specs: [], spec_page: 1, spec_page_size: 8, spec_total_pages: 1, questions_available: true, tasks: [], active_tasks: [], agents: [], viewer_key: 'fixture-owner', updated_at: '2026-11-18T00:00:00Z', task_page: 1, task_page_size: 20, task_total_pages: 1 });
  res.writeHead(404); res.end('not found');
});

const luminance = (rgb) => {
  const parts = String(rgb).match(/[\d.]+/g).slice(0, 3).map((n) => { const c = Number(n) / 255; return c <= .03928 ? c / 12.92 : ((c + .055) / 1.055) ** 2.4; });
  return .2126 * parts[0] + .7152 * parts[1] + .0722 * parts[2];
};
const boardState = () => ({
  theme: document.documentElement.dataset.theme,
  body: getComputedStyle(document.body).backgroundColor,
  panel: getComputedStyle(document.getElementById('spec-panel')).backgroundColor,
  marker: !!document.getElementById('state-must-survive'),
  value: (document.getElementById('blocker-where') || {}).value || '',
});
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  try {
    const page = await browser.newPage();
    // S-R62-CONSOLE-SLIM：菜单不再下发 task-board；走保留的直连 URL 自动打开路径。
    await page.goto(`${base}/console?task_manager=1`);
    await page.locator('#platform-items a', { hasText: 'Agent 管理' }).waitFor();
    assert.strictEqual(await page.locator('#platform-items a[data-item-id="task-board"]').count(), 0, '任务管理不得再出现在菜单中');
    const iframe = page.locator('iframe[title="任务管理"]');
    await iframe.waitFor({ state: 'attached' });
    const frame = page.frames().find((f) => f.url().includes('/console/tasks'));
    await frame.waitForFunction(() => document.documentElement.dataset.theme === 'light');
    // 状态保护样本：主题切换不得重载 iframe，既有 DOM 与输入必须原样保留。
    await frame.evaluate(() => {
      const marker = document.createElement('div'); marker.id = 'state-must-survive'; marker.textContent = 'kept';
      document.body.append(marker);
      document.getElementById('blocker-where').value = 'state-must-survive';
    });
    const before = await frame.evaluate(boardState);
    const srcBefore = await iframe.getAttribute('src');
    await page.locator('#themeBtn').click();
    // 🔴 短超时+显式断言：变异 no-theme-receiver 的红必须说出「哪条判据坏了」，不是一条 30s TimeoutError。
    try { await frame.waitForFunction(() => document.documentElement.dataset.theme === 'dark', null, { timeout: 3000 }); } catch (_) { /* 让判定交给下面的断言 */ }
    const after = await frame.evaluate(boardState);
    const srcAfter = await iframe.getAttribute('src');
    assert.strictEqual(after.theme, 'dark', 'iframe html data-theme must follow the shell');
    assert.ok(luminance(after.body) < luminance(before.body) * .5, `iframe body must actually darken: ${before.body} -> ${after.body}`);
    assert.notStrictEqual(after.panel, before.panel, `面板底色必须随主题真实变化: ${before.panel} -> ${after.panel}`);
    assert.ok(luminance(after.panel) < luminance(before.panel) * .5, `面板底色必须真实变暗: ${before.panel} -> ${after.panel}`);
    assert.strictEqual(srcAfter, srcBefore, `iframe src changed: ${srcBefore} -> ${srcAfter}`);
    assert.strictEqual(after.marker, true, 'existing iframe DOM state was lost during theme switch');
    assert.strictEqual(after.value, 'state-must-survive', 'existing iframe input state was lost during theme switch');
    // 🔵 业主的真实动线是【先切暗、再打开任务管理】。初值走 localStorage，这条动线必须有格钉住。
    const fresh = await browser.newPage();
    await fresh.goto(`${base}/console`);
    await fresh.locator('#themeBtn').click();
    await fresh.goto(`${base}/console?task_manager=1`);
    await fresh.locator('iframe[title="任务管理"]').waitFor({ state: 'attached' });
    const freshFrame = fresh.frames().find((f) => f.url().includes('/console/tasks'));
    try { await freshFrame.waitForFunction(() => document.documentElement.dataset.theme === 'dark', null, { timeout: 3000 }); } catch (_) { /* 让判定交给下面的断言 */ }
    const opened = await freshFrame.evaluate(boardState);
    assert.strictEqual(opened.theme, 'dark', '先切暗再打开的任务管理必须直接是当前主题,不能先白一下再变');
    assert.ok(luminance(opened.body) < luminance(before.body) * .5, `新开的任务管理必须直接是暗底: ${opened.body}`);
    await fresh.close();
    console.log(`PASS task-board theme: light=${before.panel} dark=${after.panel} src=${srcAfter} state=kept fresh-open=${opened.theme}`);
  } finally { await browser.close(); server.close(); }
})().catch((error) => { console.error(error); server.close(); process.exitCode = 1; });
