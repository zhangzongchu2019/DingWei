// G3 · 「平台管理」Tab 外壳 + 第一期「新用户注册邀请」的浏览器闸。
// 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 组长令 2026-10-01(平台管理 Tab 外壳归 G3,承载 N3 注册邀请) · 接口按 G3 设计件 3.3
//
// 装置同 console-agent-mgmt-*-regression.js:console 页从生产源码实时抠,Hub 用本地桩。
// 口径:真键盘(click + keyboard.type);pageerror 与 console error 两路监听,末尾断言为空(只放行桩有意返回的 4xx/5xx 资源行)。
//
// MUTATION=<name> 删除对应守卫,对应那条断言必须红(判"首红是不是该条",后续用例共享桩状态会级联):
//    keep-code-after-done · persist-code · show-hub-text · fake-capabilities · forbidden-wrong-text
//    row-code-ignores-expiry · row-code-no-timer · row-code-for-nonpending · row-code-persist · row-copy-wrong-text · row-code-without-expiry-key · no-invite-toggle · invite-toggle-static-arrow · invite-collapsed-keeps-visible
//    bind-persist · bind-hub-text · bind-no-expiry-timer · bind-copy-without-prefix · bind-done-keeps-code · bind-sends-body · bind-no-already-bound-wording(绑定飞书)
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome;NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const source = fs.readFileSync(path.join(__dirname, '..', 'internal/m8/console_auth.go'), 'utf8');
const start = source.indexOf('const consolePageHTML = `');
const end = source.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents', start);
assert(start >= 0 && end > start, 'cannot extract consolePageHTML from production source');
let html = source.slice(start + 'const consolePageHTML = `'.length, end);
const mutation = process.env.MUTATION || '';
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  assert.strictEqual(hits, 1, `mutation anchor must match exactly once: ${from} hits=${hits}`);
  html = html.replace(from, to);
}
const MUTATIONS = ['logout-without-post', 'logout-ignores-failure', 'no-logout-button', 'fixed-table-layout', 'keep-code-after-done', 'persist-code', 'show-hub-text', 'fake-capabilities', 'forbidden-wrong-text', 'row-code-ignores-expiry', 'row-code-no-timer', 'row-code-for-nonpending', 'row-code-persist', 'row-copy-wrong-text', 'row-code-without-expiry-key', 'no-invite-toggle', 'invite-toggle-static-arrow', 'invite-collapsed-keeps-visible', 'bind-persist', 'bind-hub-text', 'bind-no-expiry-timer', 'bind-copy-without-prefix', 'bind-done-keeps-code', 'bind-sends-body', 'bind-no-already-bound-wording'];
if (mutation === 'logout-without-post') mutate("try{var r=await fetch('/console/logout',{method:'POST'});if(r.ok||r.status===401){location.assign('/console/login');return;}}catch(_){}", "location.assign('/console/login');return;");
if (mutation === 'logout-ignores-failure') mutate("if(r.ok||r.status===401){location.assign('/console/login');return;}", "location.assign('/console/login');return;");
if (mutation === 'no-logout-button') mutate('<button id=logoutBtn type=button>退出</button>', '');
if (mutation === 'row-code-ignores-expiry') mutate("||exp<=Date.now())return;", "||false)return;");
if (mutation === 'row-code-no-timer') mutate("adminRowTimers.push(setTimeout(function(){box.remove();},exp-Date.now()+50));", "");
if (mutation === 'row-code-for-nonpending') mutate("if(i===4&&u.state==='pending_bind')adminRowCode(u,td);", "if(i===4)adminRowCode(u,td);");
if (mutation === 'row-code-persist') mutate("var box=document.createElement('div');box.className='admin-row-codebox';", "try{localStorage.setItem('row-code',code);}catch(_){}var box=document.createElement('div');box.className='admin-row-codebox';");
if (mutation === 'row-copy-wrong-text') mutate("await navigator.clipboard.writeText(code);fb.textContent='已复制';", "await navigator.clipboard.writeText(code+' ');fb.textContent='已复制';");
if (mutation === 'row-code-without-expiry-key') mutate("if(!code||!Number.isFinite(exp)||exp<=Date.now())return;", "if(!code)return;");
if (mutation === 'no-invite-toggle') mutate("document.getElementById('inviteToggle').onclick=adminToggleInvite;", "");
if (mutation === 'invite-toggle-static-arrow') mutate("document.getElementById('inviteArrow').textContent=open?'▾':'▸';", "");
if (mutation === 'invite-collapsed-keeps-visible') mutate("var open=body.hidden;body.hidden=!open;", "var open=body.hidden;");
if (mutation === 'bind-persist') mutate("box.dataset.code=code;", "box.dataset.code=code;try{localStorage.setItem('bind',code);}catch(_){}");
if (mutation === 'bind-hub-text') mutate("say(res.code==='already_bound'?'你已经绑定过飞书，无需再绑定':res.text);return;", "say(res.data&&res.data.detail?String(res.data.detail):res.text);return;");
if (mutation === 'bind-no-expiry-timer') mutate("if(Number.isFinite(exp))timer=setTimeout(", "if(false)timer=setTimeout(");
if (mutation === 'bind-copy-without-prefix') mutate("await navigator.clipboard.writeText('#register '+code);fb.textContent='已复制';", "await navigator.clipboard.writeText(code);fb.textContent='已复制';");
if (mutation === 'bind-done-keeps-code') mutate("document.getElementById('bindFeishuDone').onclick=function(){clearBox();say('');};", "document.getElementById('bindFeishuDone').onclick=function(){box.hidden=true;say('');};");
if (mutation === 'bind-sends-body') mutate("consolePost('/api/console/account/feishu-bind-code')", "consolePost('/api/console/account/feishu-bind-code',{user_id:'someone'})");
if (mutation === 'bind-no-already-bound-wording') mutate("say(res.code==='already_bound'?'你已经绑定过飞书，无需再绑定':res.text);return;", "say('操作失败');return;");
if (mutation && !MUTATIONS.includes(mutation)) throw new Error(`unknown mutation ${mutation}`);
if (mutation === 'fixed-table-layout') mutate('#adminUsersTable{table-layout:auto}#adminUsersTable th:nth-child(n+4),#adminUsersTable td:nth-child(n+4){white-space:nowrap}', '');
if (mutation === 'keep-code-after-done') mutate("['inviteResultUser','inviteCode','inviteCmd','inviteExpiry'].forEach(function(id){document.getElementById(id).textContent='';});", '');
if (mutation === 'persist-code') mutate("var code=String(res.registration_code||'');", "var code=String(res.registration_code||'');try{localStorage.setItem('last-code',code);}catch(_){}");
if (mutation === 'show-hub-text') mutate("function consoleErrorText(status,data){", "function consoleErrorText(status,data){if(data&&data.detail)return String(data.detail);");
if (mutation === 'fake-capabilities') mutate("var ADMIN_CAPABILITIES=[{id:'invite',title:'新用户注册邀请'}];", "var ADMIN_CAPABILITIES=[{id:'invite',title:'新用户注册邀请'},{id:'files',title:'文件对象浏览'}];");
if (mutation === 'forbidden-wrong-text') mutate("function adminErrText(code,fallback){return code==='forbidden'?'你没有管理用户的权限':fallback;}", 'function adminErrText(code,fallback){return fallback;}');

let users = [];
const rowCodes = {};   // user_id -> {registration_code?, code_expires_at?}:桌面端按 Hub 契约,键不存在=没有有效码
let failUsersList = 1;
let nextCreateError = null;
let codeSeq = 0;
const writes = [];
let logoutStatus = 204;
let bindMode = 'ok'; let bindSeq = 0; let bindTtlMs = 3600e3;
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function err(res, status, code, extra) { json(res, status, { error: code, detail: 'SECRET-HUB-TEXT', ...extra }); }
function platformItems(role) {
  const base = [{ id: 'specs', kind: 'spec', title: 'Specs', href: '#specs', status: 'approved' }];
  if (role !== 'management') return base;
  return [...base,
    { id: 'platform-admin', kind: 'platform-admin', title: '平台管理', href: '#platform-admin', status: 'approved' },
    { id: 'admin', kind: 'admin', title: '旧版后台', href: '/console/admin', target: '_blank', status: 'approved' }];
}
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  const role = /fixture-role=management/.test(req.headers.cookie || '') ? 'management' : 'owner';
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/console/login') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end('<!doctype html><title>LOGIN-PAGE</title>'); }
  if (p === '/console/logout' && req.method === 'POST') { writes.push({ path: p, body: null }); res.writeHead(logoutStatus); return res.end(); }
  if (p === '/api/console/account/feishu-bind-code' && req.method === 'POST') {
    writes.push({ path: p, contentLength: req.headers['content-length'] || '0' }); await read(req);
    if (bindMode === 'already') return err(res, 409, 'already_bound');
    if (bindMode === 'boom') return err(res, 500, 'internal');
    return json(res, 200, { registration_code: `BIND${++bindSeq}XYZ`.slice(0, 8).padEnd(8, 'Q'), code_expires_at: new Date(Date.now() + bindTtlMs).toISOString() });
  }
  if (p === '/api/console/agents') return json(res, 200, { agents: [] });
  if (p === '/api/console/agent-groups') return json(res, 200, { owned_agents: [], groups: [] });
  if (p === '/api/console/platform') return json(res, 200, { role, actions: [], items: platformItems(role) });
  if (p === '/api/console/platform/users' && req.method === 'GET') {
    if (failUsersList > 0) { failUsersList -= 1; res.writeHead(503, { 'content-type': 'text/plain' }); return res.end('users unavailable'); }
    return json(res, 200, { users: users.map((u) => ({ ...u, ...(rowCodes[u.user_id] || {}) })) });
  }
  if (p === '/api/console/platform/users' && req.method === 'POST') {
    const body = await read(req); writes.push({ path: p, body });
    if (nextCreateError) { const [status, code] = nextCreateError; nextCreateError = null; return err(res, status, code); }
    if (users.some((u) => u.user_id.toLowerCase() === String(body.user_id).toLowerCase())) return err(res, 409, 'user_exists');
    users.push({ user_id: body.user_id, display_name: body.display_name, short_name: body.short_name, type: body.type, state: 'pending_bind' });
    return json(res, 201, { user_id: body.user_id, registration_code: `REG-${++codeSeq}-ABCD`, code_expires_at: '2026-10-02T09:00:00Z' });
  }
  const m = p.match(/^\/api\/console\/platform\/users\/([^/]+)\/registration-code$/);
  if (m && req.method === 'POST') {
    writes.push({ path: p, body: null });
    return json(res, 200, { registration_code: `REG-${++codeSeq}-WXYZ`, code_expires_at: '2026-10-02T10:00:00Z' });
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] });
  const pageErrors = []; const consoleErrors = [];
  const watch = (pg) => { pg.on('pageerror', (e) => pageErrors.push(e.message)); pg.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); }); };
  try {
    await test('an ordinary owner gets no 平台管理 entry', async () => {
      const owner = await context.newPage(); watch(owner); owner.setDefaultTimeout(2500);
      await owner.goto(`${base}/console`);
      await owner.locator('#platform-items a[data-item-id="specs"]').waitFor({ state: 'visible' });
      assert.strictEqual(await owner.locator('#platform-items a[data-item-id="platform-admin"]').count(), 0);
      assert.strictEqual(await owner.locator('#platform-items a[data-item-id="admin"]').count(), 0);
      await owner.close();
    });

    await context.addCookies([{ name: 'fixture-role', value: 'management', url: base }]);
    const page = await context.newPage(); watch(page); page.setDefaultTimeout(2500);
    const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(value); };
    await page.goto(`${base}/console`);

    await test('a management user sees the new tab entry, distinct from the old popup back office', async () => {
      const entry = page.locator('#platform-items a[data-item-id="platform-admin"]');
      await entry.waitFor({ state: 'visible' });
      assert.strictEqual(await entry.textContent(), '平台管理');
      assert.strictEqual(await entry.getAttribute('target'), null, 'internal tab: no target (navigating away would drop the live panes)');
      const old = page.locator('#platform-items a[data-item-id="admin"]');
      assert.strictEqual(await old.textContent(), '旧版后台');
      assert.strictEqual(await old.getAttribute('target'), '_blank');
    });

    await test('clicking opens the 平台管理 tab; the capability menu lists only what exists (no fake entries)', async () => {
      await page.locator('#platform-items a[data-item-id="platform-admin"]').click();
      await page.locator('#tabs .tab[title="平台管理"]').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await page.locator('#adminMenu button').allTextContents(), ['新用户注册邀请']);
      assert.match(await page.locator('#adminInvite h3').textContent(), /新用户注册邀请/);
    });

    await test('user list: first load failure shows the reason with a retry; retry shows the empty state', async () => {
      await page.locator('#adminUsersStatus').filter({ hasText: '用户列表加载失败' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#adminUsersStatus').textContent(), /503/);
      assert.strictEqual(await page.locator('#adminUsersEmpty').isHidden(), true, 'a failed load must not claim "no users"');
      await page.locator('#adminUsersRetry').click();
      await page.locator('#adminUsersEmpty').waitFor({ state: 'visible' });
    });

    await test('incomplete form is hinted locally and nothing is sent', async () => {
      const before = writes.length;
      await page.locator('#inviteCreate').click();
      assert.match(await page.locator('#inviteFeedback').textContent(), /请填写用户名、用户 ID 和用户 ID 简称/);
      assert.strictEqual(writes.length, before);
    });

    await test('Hub refusals use our wording (never the Hub text), keep the typed values; forbidden says "no permission"', async () => {
      await type('#inviteName', '张三'); await type('#inviteUserId', 'zhangsan'); await type('#inviteShort', 'zs');
      nextCreateError = [409, 'user_exists'];
      await page.locator('#inviteCreate').click();
      await page.locator('#inviteFeedback').filter({ hasText: '已被使用' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await page.locator('#inviteFeedback').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await page.locator('#inviteUserId').inputValue(), 'zhangsan');
      nextCreateError = [400, 'bad_request'];
      await page.locator('#inviteCreate').click();
      await page.locator('#inviteFeedback').filter({ hasText: '不符合规则' }).waitFor({ state: 'visible' });
      nextCreateError = [403, 'forbidden'];
      await page.locator('#inviteCreate').click();
      await page.locator('#inviteFeedback').filter({ hasText: '你没有管理用户的权限' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#inviteResult').isHidden(), true, 'no code is shown for a refused create');
    });

    await test('creating a user shows the one-time code, expiry and the Feishu command; inputs are cleared; the user appears in the list', async () => {
      await page.locator('#inviteType').selectOption('manager');
      await page.locator('#inviteCreate').click();
      await page.locator('#inviteResult').waitFor({ state: 'visible' });
      assert.deepStrictEqual(writes.at(-1).body, { display_name: '张三', user_id: 'zhangsan', short_name: 'zs', type: 'manager' });
      assert.strictEqual(await page.locator('#inviteResultUser').textContent(), 'zhangsan');
      assert.strictEqual(await page.locator('#inviteCode').textContent(), 'REG-1-ABCD');
      assert.strictEqual(await page.locator('#inviteCmd').textContent(), '#register REG-1-ABCD');
      assert.match(await page.locator('#inviteExpiry').textContent(), /有效至/);
      for (const id of ['inviteName', 'inviteUserId', 'inviteShort']) assert.strictEqual(await page.locator(`#${id}`).inputValue(), '');
      const row = page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"]');
      await row.waitFor({ state: 'visible' });
      assert.deepStrictEqual(await row.locator('td').allTextContents(), ['zhangsan', '张三', 'zs', '管理用户', '待绑定飞书', '重新生成注册码']);
      assert.doesNotMatch(await page.locator('#adminUsersTable').textContent(), /REG-/, 'the code never lands in the table');
    });

    await test('every column of the users table has real width, so the status and the regenerate button are actually visible (real-Hub finding: fixed layout squeezed them to 0)', async () => {
      const widths = await page.locator('#adminUsersTable th').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().width)));
      assert.ok(widths.every((w) => w >= 40), `column widths must all be >= 40px, got ${JSON.stringify(widths)}`);
      const btn = await page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"] .admin-regen').boundingBox();
      const tbl = await page.locator('#adminUsersTable').boundingBox();
      assert.ok(btn && btn.width > 20 && btn.x + btn.width <= tbl.x + tbl.width + 1, `the regenerate button must sit inside the table, got ${JSON.stringify(btn)} vs ${JSON.stringify(tbl)}`);
    });

    await test('copy puts exactly the code on the clipboard and says so', async () => {
      await page.locator('#inviteCopy').click();
      await page.locator('#inviteCopyFeedback').filter({ hasText: '已复制' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.evaluate(() => navigator.clipboard.readText()), 'REG-1-ABCD');
    });

    await test('"完成" wipes the code from the page and nothing is persisted in browser storage', async () => {
      await page.locator('#inviteDone').click();
      await page.locator('#inviteResult').waitFor({ state: 'hidden' });
      assert.doesNotMatch(await page.locator('body').innerHTML(), /REG-1-ABCD/, 'the code must be gone from the DOM');
      const stored = await page.evaluate(() => JSON.stringify({ l: { ...localStorage }, s: { ...sessionStorage } }));
      assert.doesNotMatch(stored, /REG-/);
    });

    await test('regenerating for a pending user shows the new code (old one is not shown again)', async () => {
      await page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"] .admin-regen').click();
      await page.locator('#inviteResult').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#inviteCode').textContent(), 'REG-2-WXYZ');
      assert.strictEqual(await page.locator('#inviteResultUser').textContent(), 'zhangsan');
      assert.strictEqual(writes.at(-1).path, '/api/console/platform/users/zhangsan/registration-code');
      assert.doesNotMatch(await page.locator('body').innerHTML(), /REG-1-ABCD/);
    });

    await test('pending row shows the Hub-provided code, its expiry and a copy button; copy puts exactly the code on the clipboard', async () => {
      const future = new Date(Date.now() + 3600e3).toISOString();
      rowCodes.zhangsan = { registration_code: 'ROWCODE1', code_expires_at: future };
      await page.locator('#adminUsersRetry').evaluate(() => refreshAdminUsers());
      const box = page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"] .admin-row-codebox');
      await box.waitFor({ state: 'visible' });
      assert.strictEqual(await box.locator('.admin-row-code').textContent(), 'ROWCODE1');
      assert.match(await box.locator('.admin-row-expiry').textContent(), /有效至/);
      await box.locator('.admin-row-copy').click();
      await box.locator('.admin-row-copyfb').filter({ hasText: '已复制' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.evaluate(() => navigator.clipboard.readText()), 'ROWCODE1');
      const stored = await page.evaluate(() => JSON.stringify({ l: { ...localStorage }, s: { ...sessionStorage } }));
      assert.doesNotMatch(stored, /ROWCODE1/, 'row codes are never persisted in browser storage');
    });

    await test('the row code disappears when the Hub stops sending the keys (used / regenerated / expired server-side), and never shows for a non-pending user', async () => {
      delete rowCodes.zhangsan;
      await page.evaluate(() => refreshAdminUsers());
      await page.locator('#adminUsersItems .admin-row-codebox').waitFor({ state: 'detached' });
      users.push({ user_id: 'lisi', display_name: '李四', short_name: 'ls', type: 'member', state: 'active' });
      rowCodes.lisi = { registration_code: 'LEAKCODE', code_expires_at: new Date(Date.now() + 3600e3).toISOString() };
      await page.evaluate(() => refreshAdminUsers());
      await page.locator('#adminUsersItems .admin-user-row[data-user-id="lisi"]').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#adminUsersItems .admin-row-codebox').count(), 0, 'an active user never shows a code');
      delete rowCodes.lisi; users.pop();
    });

    await test('a code without an expiry key, or already expired by the browser clock, is never even rendered (watched with a MutationObserver, so a remove-right-after-render cannot hide it)', async () => {
      await page.evaluate(() => { window.__codeboxSeen = 0; new MutationObserver((ms) => { for (const m of ms) for (const n of m.addedNodes) if (n.nodeType === 1 && (n.matches('.admin-row-codebox') || n.querySelector('.admin-row-codebox'))) window.__codeboxSeen++; }).observe(document.getElementById('adminUsersItems'), { childList: true, subtree: true }); });
      rowCodes.zhangsan = { registration_code: 'NOEXPIRY' };
      await page.evaluate(() => refreshAdminUsers());
      rowCodes.zhangsan = { registration_code: 'PASTCODE', code_expires_at: new Date(Date.now() - 1000).toISOString() };
      await page.evaluate(() => refreshAdminUsers());
      await page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"]').waitFor({ state: 'visible' });
      await page.waitForTimeout(150);
      assert.strictEqual(await page.evaluate(() => window.__codeboxSeen), 0, 'no code box was ever rendered for these rows');
      assert.doesNotMatch(await page.locator('#adminUsersTable').textContent(), /NOEXPIRY|PASTCODE/);
    });

    await test('a shown row code vanishes by itself when it expires, without a refresh', async () => {
      rowCodes.zhangsan = { registration_code: 'SHORTLIV', code_expires_at: new Date(Date.now() + 1200).toISOString() };
      await page.evaluate(() => refreshAdminUsers());
      await page.locator('#adminUsersItems .admin-row-codebox').waitFor({ state: 'visible' });
      await page.locator('#adminUsersItems .admin-row-codebox').waitFor({ state: 'detached', timeout: 4000 });
      assert.doesNotMatch(await page.locator('#adminUsersTable').textContent(), /SHORTLIV/);
      assert.strictEqual(await page.locator('#adminUsersItems .admin-user-row[data-user-id="zhangsan"] .admin-regen').count(), 1, 'the regenerate fallback stays');
      delete rowCodes.zhangsan;
    });

    await test('the invite section has an explicit expand/collapse control: arrow + hint, form folds away and comes back with typed values intact', async () => {
      const toggle = page.locator('#inviteToggle');
      assert.strictEqual(await toggle.getAttribute('aria-expanded'), 'true', 'default is expanded');
      assert.strictEqual(await page.locator('#inviteArrow').textContent(), '▾');
      assert.strictEqual(await page.locator('#inviteToggleHint').textContent(), '点击收起');
      await type('#inviteName', '王五');
      await toggle.click();
      await page.locator('#inviteBody').waitFor({ state: 'hidden' });
      assert.strictEqual(await toggle.getAttribute('aria-expanded'), 'false');
      assert.strictEqual(await page.locator('#inviteArrow').textContent(), '▸');
      assert.strictEqual(await page.locator('#inviteToggleHint').textContent(), '点击展开');
      assert.strictEqual(await page.locator('#inviteCreate').isHidden(), true, 'the form is folded');
      assert.strictEqual(await page.locator('#adminUsersTable').isVisible(), true, 'the user list stays visible when the form is folded');
      await toggle.click();
      await page.locator('#inviteBody').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#inviteArrow').textContent(), '▾');
      assert.strictEqual(await page.locator('#inviteName').inputValue(), '王五');
      await page.locator('#inviteName').fill('');
    });

    await test('closing the tab throws the page state away: reopening shows no code', async () => {
      await page.locator('#tabs .tab[title="平台管理"] .x').click();
      await page.locator('#adminInvite').waitFor({ state: 'detached' });
      await page.locator('#platform-items a[data-item-id="platform-admin"]').click();
      await page.locator('#adminInvite').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#inviteResult').isHidden(), true);
      assert.doesNotMatch(await page.locator('body').innerHTML(), /REG-2-WXYZ/);
    });

    await test('绑定飞书: the button asks for a code for the signed-in user only (empty body), shows "#register 码" + expiry, and the copy puts the full command on the clipboard', async () => {
      const pg = await context.newPage(); watch(pg); pg.setDefaultTimeout(2500);
      await pg.goto(`${base}/console`);
      await pg.locator('#bindFeishuBtn').click();
      await pg.locator('#bindFeishuBox').waitFor({ state: 'visible' });
      const w = writes.filter((x) => x.path === '/api/console/account/feishu-bind-code');
      assert.strictEqual(w.length, 1);
      assert.strictEqual(w[0].contentLength, '0', 'no target in the request: the Hub decides from the session');
      const cmd = await pg.locator('#bindFeishuCmd').textContent();
      assert.match(cmd, /^#register BIND\d+XYZ$/);
      assert.match(await pg.locator('#bindFeishuExpiry').textContent(), /有效至/);
      await pg.locator('#bindFeishuCopy').click();
      await pg.locator('#bindFeishuCopyFb').filter({ hasText: '已复制' }).waitFor({ state: 'visible' });
      assert.strictEqual(await pg.evaluate(() => navigator.clipboard.readText()), cmd);
      const stored = await pg.evaluate(() => JSON.stringify({ l: { ...localStorage }, s: { ...sessionStorage } }));
      assert.doesNotMatch(stored, /BIND\d/, 'the bind code is never persisted in browser storage');
      const first = cmd;
      await pg.locator('#bindFeishuBtn').click();
      await pg.locator('#bindFeishuCmd').filter({ hasNotText: first }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await pg.locator('body').innerHTML(), new RegExp(first.split(' ')[1]), 'asking again replaces the old code on the page');
      await pg.locator('#bindFeishuDone').click();
      await pg.locator('#bindFeishuBox').waitFor({ state: 'hidden' });
      assert.doesNotMatch(await pg.locator('body').innerHTML(), /BIND\d+XYZ/, '完成 wipes the code from the DOM');
      await pg.close();
    });

    await test('绑定飞书: already bound (409) says so in our wording and shows no code; a 500 shows our wording, never the Hub text', async () => {
      const pg = await context.newPage(); watch(pg); pg.setDefaultTimeout(2500);
      await pg.goto(`${base}/console`);
      bindMode = 'already';
      await pg.locator('#bindFeishuBtn').click();
      await pg.locator('#bindFeishuStatus').filter({ hasText: '已经绑定过飞书' }).waitFor({ state: 'visible' });
      assert.strictEqual(await pg.locator('#bindFeishuBox').isHidden(), true);
      assert.strictEqual(await pg.locator('#bindFeishuBtn').isDisabled(), false, 'the button works again');
      bindMode = 'boom';
      await pg.locator('#bindFeishuBtn').click();
      await pg.locator('#bindFeishuStatus').filter({ hasText: '服务端出错了' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await pg.locator('#bindFeishuStatus').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await pg.locator('#bindFeishuBox').isHidden(), true);
      bindMode = 'ok';
      await pg.close();
    });

    await test('绑定飞书: an expired code disappears by itself and says so', async () => {
      const pg = await context.newPage(); watch(pg); pg.setDefaultTimeout(2500);
      await pg.goto(`${base}/console`);
      bindTtlMs = 1200;
      await pg.locator('#bindFeishuBtn').click();
      await pg.locator('#bindFeishuBox').waitFor({ state: 'visible' });
      await pg.locator('#bindFeishuBox').waitFor({ state: 'hidden', timeout: 4000 });
      await pg.locator('#bindFeishuStatus').filter({ hasText: '已过期' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await pg.locator('body').innerHTML(), /BIND\d+XYZ/);
      bindTtlMs = 3600e3;
      await pg.close();
    });

    await test('the top of the console has a 退出 button: it POSTs /console/logout once and then goes to /console/login', async () => {
      const pg = await context.newPage(); watch(pg); pg.setDefaultTimeout(2500);
      await pg.goto(`${base}/console`);
      const btn = pg.locator('#logoutBtn');
      await btn.waitFor({ state: 'visible' });
      assert.strictEqual((await btn.textContent()).trim(), '退出');
      const before = writes.filter((w) => w.path === '/console/logout').length;
      await btn.click();
      await pg.waitForURL(/\/console\/login$/);
      assert.strictEqual(writes.filter((w) => w.path === '/console/logout').length, before + 1, 'exactly one logout POST');
      assert.match(await pg.title(), /LOGIN-PAGE/);
      await pg.close();
    });

    await test('a failed logout does not pretend to have logged out: stays on the console, says so, and the button works again', async () => {
      const pg = await context.newPage(); watch(pg); pg.setDefaultTimeout(2500);
      logoutStatus = 500;
      await pg.goto(`${base}/console`);
      await pg.locator('#logoutBtn').click();
      await pg.locator('#platform-status').filter({ hasText: '退出失败' }).waitFor({ state: 'visible' });
      assert.match(pg.url(), /\/console$/, 'must not navigate away on failure');
      assert.strictEqual(await pg.locator('#logoutBtn').isDisabled(), false, 'the button is usable again');
      logoutStatus = 204;
      await pg.locator('#logoutBtn').click();
      await pg.waitForURL(/\/console\/login$/);
      await pg.close();
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|403|409|500|503)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`platform admin browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) { if (fail < 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
