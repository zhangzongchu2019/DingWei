// 第一期登录页（N3）的浏览器闸：用户 ID＋密码（含强制改密）、用户 ID＋飞书确认；以及 console 页遇到
// 401 unauthenticated／403 must_change_password 时回登录页。
// 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 组长令 2026-10-01（最高优先级）· 接口按 opus50tc2k 的 N3 分支 httpapi/n3.go。
//
// 装置：登录页从 internal/m8/console_login_newplat.go 实时抠，console 页从 console_auth.go 实时抠，Hub 用本地桩。
// 口径：真键盘（click + keyboard.type）；pageerror 与 console error 两路监听，末尾断言为空（只放行桩有意回的 4xx/429 资源行）。
//
// MUTATION=<name> 删除对应守卫，对应那条断言必须红（判"首红是不是该条"）：
//    password-visible · keep-password-after-fail · skip-force-change · no-expiry-check · show-hub-text · allow-mismatch · guard-off
// 环境变量：PW_CHANNEL=chrome 用系统 Chrome；NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

function extract(file, constName, end) {
  const src = fs.readFileSync(path.join(__dirname, '..', 'internal/m8', file), 'utf8');
  const start = src.indexOf(`const ${constName} = \``);
  const stop = src.indexOf(end, start);
  assert(start >= 0 && stop > start, `cannot extract ${constName} from ${file}`);
  return src.slice(start + `const ${constName} = \``.length, stop);
}
let loginHtml = extract('console_login_newplat.go', 'newPlatformLoginPageHTML', '`\n');
let consoleHtml = extract('console_auth.go', 'consolePageHTML', '`\n\nfunc (h *Hub) HandleConsoleAgents');
const mutation = process.env.MUTATION || '';
function mutate(which, from, to) {
  const hay = which === 'login' ? loginHtml : consoleHtml;
  const hits = hay.split(from).length - 1;
  assert.strictEqual(hits, 1, `mutation anchor must match exactly once: ${from} hits=${hits}`);
  if (which === 'login') loginHtml = loginHtml.replace(from, to); else consoleHtml = consoleHtml.replace(from, to);
}
const MUTATIONS = ['password-visible', 'keep-password-after-fail', 'skip-force-change', 'no-expiry-check', 'show-hub-text', 'allow-mismatch', 'guard-off'];
if (mutation && !MUTATIONS.includes(mutation)) throw new Error(`unknown mutation ${mutation}`);
if (mutation === 'password-visible') mutate('login', 'id=pwPass type=password', 'id=pwPass type=text');
if (mutation === 'keep-password-after-fail') mutate('login', "if(!res.ok){say('pwErr',res.text);$('pwPass').value='';return;}", "if(!res.ok){say('pwErr',res.text);return;}");
if (mutation === 'skip-force-change') mutate('login', "if(res.data.must_change_password){", "if(false){");
if (mutation === 'no-expiry-check') mutate('login', 'if(Number.isFinite(expires)&&Date.now()>=expires){', 'if(false){');
if (mutation === 'show-hub-text') mutate('login', "function errText(status,data){", "function errText(status,data){if(data&&data.detail)return String(data.detail);");
if (mutation === 'allow-mismatch') mutate('login', "if(n1!==n2){say('chErr','两次输入的新密码不一致');return;}", '');
if (mutation === 'guard-off') mutate('console', "else if(d&&d.error==='must_change_password')location.assign('/console/login#change');", '');

const OLD_PW = 'Init-Password-1!';
let currentPw = OLD_PW;
let mustChange = true;
let wrongAttempts = 0;
let codeSeq = 0;
let ttlMs = 300000;
const confirmed = new Set();
const posts = [];
const statusGets = [];
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body, headers = {}) { res.writeHead(status, { 'content-type': 'application/json', ...headers }); res.end(JSON.stringify(body)); }
function err(res, status, code, extra) { json(res, status, { error: code, detail: 'SECRET-HUB-TEXT', ...extra }); }
let consoleMode = 'unauth'; // /api/console/platform 的行为:unauth | mustchange
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console/login') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(loginHtml); }
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html' }); return res.end('<!doctype html><title>console-ok</title>'); }
  if (p === '/console-real') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(consoleHtml); }
  if (p === '/api/console/platform') return consoleMode === 'mustchange' ? err(res, 403, 'must_change_password') : err(res, 401, 'unauthenticated');
  if (p === '/api/console/agents' || p === '/api/console/agent-groups') return err(res, consoleMode === 'mustchange' ? 403 : 401, consoleMode === 'mustchange' ? 'must_change_password' : 'unauthenticated');
  if (p === '/console/login/password' && req.method === 'POST') {
    const b = await read(req); posts.push({ p, b });
    if (wrongAttempts >= 3) return err(res, 429, 'rate_limited', { retry_after_s: 30 });
    if (b.user_id !== 'systemdefault' || b.password !== currentPw) { wrongAttempts += 1; return err(res, 401, 'bad_credentials'); }
    return json(res, 200, { user_id: 'systemdefault', must_change_password: mustChange }, { 'set-cookie': 'np=s1; Path=/; HttpOnly' });
  }
  if (p === '/api/console/account/password' && req.method === 'POST') {
    const b = await read(req); posts.push({ p, b });
    if (!/np=s1/.test(req.headers.cookie || '')) return err(res, 401, 'unauthenticated');
    if (b.old_password !== currentPw) return err(res, 401, 'bad_credentials');
    if (String(b.new_password).length < 12 || b.new_password === b.old_password) return err(res, 422, 'weak_password', { min_length: 12 });
    currentPw = b.new_password; mustChange = false; return json(res, 200, {});
  }
  if (p === '/console/login/code' && req.method === 'POST') {
    const b = await read(req); posts.push({ p, b });
    if (!/^[A-Za-z0-9]{2,32}$/.test(String(b.user_id))) return err(res, 400, 'bad_request');
    const code = `AB12-CD${String(++codeSeq).padStart(2, '0')}`;
    return json(res, 200, { code, expires_at: new Date(Date.now() + ttlMs).toISOString() });
  }
  if (p === '/console/login/status' && req.method === 'GET') {
    const code = new URL(req.url, 'http://x').searchParams.get('code'); statusGets.push(code);
    return confirmed.has(code) ? json(res, 200, { state: 'confirmed' }, { 'set-cookie': 'np=s2; Path=/; HttpOnly' }) : json(res, 200, { state: 'pending' });
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
  const context = await browser.newContext();
  const pageErrors = []; const consoleErrors = [];
  const watch = (pg) => { pg.on('pageerror', (e) => pageErrors.push(e.message)); pg.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); }); };
  const page = await context.newPage(); watch(page); page.setDefaultTimeout(2500);
  const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(value); };
  try {
    await page.goto(`${base}/console/login`);

    await test('both sign-in routes are offered; the change-password form is hidden; password inputs are masked', async () => {
      assert.strictEqual(await page.locator('#pwSection').isVisible(), true);
      assert.strictEqual(await page.locator('#fsSection').isVisible(), true);
      assert.strictEqual(await page.locator('#changeSection').isHidden(), true);
      assert.strictEqual(await page.locator('#pwPass').getAttribute('type'), 'password');
    });

    await test('empty fields are hinted locally; nothing is sent', async () => {
      const before = posts.length;
      await page.locator('#pwGo').click();
      assert.match(await page.locator('#pwErr').textContent(), /请输入用户 ID 和密码/);
      assert.strictEqual(posts.length, before);
    });

    await test('wrong password: our wording (never the Hub text), the password is cleared, the user ID is kept', async () => {
      await type('#pwUser', 'systemdefault'); await type('#pwPass', 'wrong-one');
      await page.locator('#pwGo').click();
      await page.locator('#pwErr').filter({ hasText: '用户 ID 或密码不对' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await page.locator('#pwErr').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await page.locator('#pwPass').inputValue(), '', 'a failed attempt must not leave the typed password in the field');
      assert.strictEqual(await page.locator('#pwUser').inputValue(), 'systemdefault');
      assert.match(page.url(), /\/console\/login$/);
    });

    await test('too many failures: the wait time from the Hub is shown', async () => {
      for (let i = 0; i < 2; i += 1) { await type('#pwPass', 'wrong-again'); await page.locator('#pwGo').click(); await page.waitForTimeout(150); }
      await type('#pwPass', 'wrong-again'); await page.locator('#pwGo').click();
      await page.locator('#pwErr').filter({ hasText: '请 30 秒后再试' }).waitFor({ state: 'visible' });
      wrongAttempts = 0;
    });

    await test('first login with the initial password: only the change form is offered, the user is NOT let into the console', async () => {
      await type('#pwPass', OLD_PW); await page.locator('#pwGo').click();
      await page.locator('#changeSection').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#pwSection').isHidden(), true);
      assert.strictEqual(await page.locator('#fsSection').isHidden(), true);
      assert.match(page.url(), /\/console\/login$/, 'must not navigate to /console before the password is changed');
      assert.strictEqual(await page.locator('#chOld').getAttribute('type'), 'password');
    });

    await test('change form: mismatch is hinted locally and sends nothing; a weak password is refused with the Hub minimum; a wrong old password is refused and cleared', async () => {
      const before = posts.length;
      await type('#chNew', 'A-brand-new-password-1'); await type('#chNew2', 'something-else-entirely');
      await page.locator('#chGo').click();
      assert.match(await page.locator('#chErr').textContent(), /两次输入的新密码不一致/);
      assert.strictEqual(posts.length, before, 'a mismatch must not reach the Hub');
      await type('#chNew', 'short'); await type('#chNew2', 'short'); await page.locator('#chGo').click();
      await page.locator('#chErr').filter({ hasText: '至少 12 位' }).waitFor({ state: 'visible' });
      await type('#chOld', 'not-the-old-password'); await type('#chNew', 'A-brand-new-password-1'); await type('#chNew2', 'A-brand-new-password-1');
      await page.locator('#chGo').click();
      await page.locator('#chErr').filter({ hasText: '用户 ID 或密码不对' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#chOld').inputValue(), '', 'a wrong old password is cleared');
    });

    await test('a valid change lets the user into the console', async () => {
      await type('#chOld', OLD_PW); await type('#chNew', 'A-brand-new-password-1'); await type('#chNew2', 'A-brand-new-password-1');
      await page.locator('#chGo').click();
      await page.waitForURL(/\/console$/);
      assert.strictEqual(await page.title(), 'console-ok');
      assert.deepStrictEqual(posts.at(-1).b, { old_password: OLD_PW, new_password: 'A-brand-new-password-1' });
    });

    await page.goto(`${base}/console/login`);
    await page.evaluate(() => { LOGIN_POLL_MS = 120; });

    await test('Feishu: a malformed user ID is refused in our wording; a valid one shows the "#console <code>" command', async () => {
      await type('#fsUser', 'a'); await page.locator('#fsGo').click();
      await page.locator('#fsErr').filter({ hasText: '用户 ID 格式不对' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#fsBox').isHidden(), true);
      await type('#fsUser', 'zhangsan'); await page.locator('#fsGo').click();
      await page.locator('#fsBox').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#fsCmd').textContent(), '#console AB12-CD01');
      assert.match(await page.locator('#fsStatus').textContent(), /等待你在飞书里确认/);
      assert.deepStrictEqual(posts.at(-1).b, { user_id: 'zhangsan' });
    });

    await test('Feishu: once the Hub reports it confirmed, the page enters the console', async () => {
      await page.waitForFunction(() => document.getElementById('fsStatus').textContent.includes('等待'));
      confirmed.add('AB12-CD01');
      await page.waitForURL(/\/console$/);
      assert.strictEqual(await page.title(), 'console-ok');
    });

    await page.goto(`${base}/console/login`);
    await page.evaluate(() => { LOGIN_POLL_MS = 100; });

    await test('Feishu: an expired code is announced, the old code disappears, polling stops, and "重新获取" gives a new code', async () => {
      ttlMs = 700;
      await type('#fsUser', 'zhangsan'); await page.locator('#fsGo').click();
      await page.locator('#fsBox').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#fsCmd').textContent(), '#console AB12-CD02');
      await page.locator('#fsStatus').filter({ hasText: '登录码已过期' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#fsCmd').textContent(), '', 'an expired code must not stay on screen');
      assert.strictEqual(await page.locator('#fsRetry').isVisible(), true);
      const seen = statusGets.length; await page.waitForTimeout(500);
      assert.strictEqual(statusGets.length, seen, 'no polling after expiry');
      ttlMs = 300000;
      await page.locator('#fsRetry').click();
      await page.locator('#fsCmd').filter({ hasText: 'AB12-CD03' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#fsRetry').isHidden(), true);
    });

    await test('no password or session material is kept in browser storage', async () => {
      const stored = await page.evaluate(() => JSON.stringify({ l: { ...localStorage }, s: { ...sessionStorage } }));
      assert.strictEqual(stored, '{"l":{},"s":{}}');
    });

    await test('the console page itself sends an unauthenticated visitor to the login page', async () => {
      consoleMode = 'unauth';
      const p2 = await context.newPage(); watch(p2); p2.setDefaultTimeout(2500);
      await p2.goto(`${base}/console-real`);
      await p2.waitForURL(/\/console\/login$/);
      await p2.close();
    });

    await test('the console page sends a must-change-password session to the change form, and only that form', async () => {
      consoleMode = 'mustchange';
      const p3 = await context.newPage(); watch(p3); p3.setDefaultTimeout(2500);
      await p3.goto(`${base}/console-real`);
      await p3.waitForURL(/\/console\/login#change$/);
      await p3.locator('#changeSection').waitFor({ state: 'visible' });
      assert.strictEqual(await p3.locator('#pwSection').isHidden(), true);
      assert.strictEqual(await p3.locator('#fsSection').isHidden(), true);
      assert.strictEqual(await p3.locator('#chOld').inputValue(), '', 'arriving from the console, the old password is asked again');
      await p3.close();
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|401|403|422|429)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`login browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) { if (fail < 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
