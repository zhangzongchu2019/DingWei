// S-R62-MOBILE 浏览器门：Console 登录页（两形）、工作台主页（菜单＋标签页＋Agent 管理）、
// 会话阅读页在 390×844 与 360×780 可读可操作；1440 宽 PC 外观不变。
// 判据：窄屏 document 横向溢出为 0；菜单抽屉能开合（按钮／遮罩／ESC／选中即关）；
// 标签页横向可滚；两视口能打开一个 Agent 会话并滚动阅读；点击目标≥40px；PC 布局原样。
// 删保护刀：mobile-no-drawer／mobile-tabs-wrap／mobile-input-wide／mobile-no-viewport／
// mobile-session-small-targets。
// M1 追加（S-R62-MOBILE 小活）：抽屉开合改为「语义断言＋有界等待」——开抽屉前先断言
// 按钮可见且 aria-expanded 初值 false，开／合等待上界 3000ms（CSS 过渡 .2s 的 15 倍余量），
// 超时落到判据文案上，mobile-no-drawer 刀不再以 page.waitForFunction 10000ms 超时红。
// 截图：MOBILE_SHOT_DIR=<目录> 时输出两视口＋1440 PC 各页 PNG（交件证据用）。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');
const { agentRecord } = require('./fixture-agents');

const ROOT = path.join(__dirname, '..');
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';
const allowedMutations = new Set(['', 'mobile-no-drawer', 'mobile-tabs-wrap', 'mobile-input-wide', 'mobile-no-viewport', 'mobile-session-small-targets']);
if (!allowedMutations.has(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
const SHOT_DIR = process.env.MOBILE_SHOT_DIR || '';

function template(file, start, end) {
  const source = fs.readFileSync(file, 'utf8');
  const a = source.indexOf(start); assert.notStrictEqual(a, -1, `${start} missing`);
  const b = source.indexOf(end, a + start.length); assert.notStrictEqual(b, -1, `${end} missing`);
  return source.slice(a + start.length, b);
}
let shell = template(path.join(ROOT, 'internal/m8/console_auth.go'), 'const consolePageHTML = `', '`\n\nfunc (h *Hub) HandleConsoleAgents');
shell = shell.split('{{CONSOLE_VERSION}}').join('7.7.7.7-harness').split('{{CONSOLE_COMMIT}}').join('c0ffee1234567890').split('{{CONSOLE_VERSION_TEXT}}').join('7.7.7.7-harness');
function mutate(from, to) { const next = shell.split(from).join(to); assert.notStrictEqual(next, shell, `mutation anchor missing: ${from}`); shell = next; }
if (MUTATION === 'mobile-no-drawer') mutate("toggle.onclick=function(){setNav(!navEl.classList.contains('open'));};", 'toggle.onclick=null;');
if (MUTATION === 'mobile-tabs-wrap') mutate('#tabs{flex-wrap:nowrap;overflow-x:auto;-webkit-overflow-scrolling:touch}', '#tabs{flex-wrap:wrap;overflow-x:visible}');
if (MUTATION === 'mobile-input-wide') mutate('.group-workspace input,.group-workspace select{min-width:0;max-width:100%}', '.group-workspace input,.group-workspace select{min-width:500px}');
if (MUTATION === 'mobile-no-viewport') mutate('<meta name=viewport content="width=device-width,initial-scale=1"><title>DingWei Agent Console</title>', '<title>DingWei Agent Console</title>');

const oldLogin = template(path.join(ROOT, 'internal/m8/console_login.go'), 'fmt.Fprintf(w, `', '`, html.EscapeString').replace('%s', 'abcd1234').replace('%q', '"abcd1234"');
const newplatLogin = template(path.join(ROOT, 'internal/m8/console_login_newplat.go'), 'const newPlatformLoginPageHTML = `', '`\n');
let renderer = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
if (MUTATION === 'mobile-session-small-targets') {
  const next = renderer.split('.text-btn, .unlock-btn, #themeBtn, #clockStatus, #interruptBtn { min-height: 40px; }').join('');
  assert.notStrictEqual(next, renderer, 'mutation anchor missing: session target min-height');
  renderer = next;
}

const SESSIONS = ['owner-mobile-2642', 'owner-second-2642', 'owner-third-2642', 'owner-fourth-2642'];
const json = (res, value) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
const html = (res, body) => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(body); };
const historyPage = () => ({
  generation: 'generation-mobile', high_water: 150,
  events: Array.from({ length: 150 }, (_, i) => ({
    cursor: { generation: 'generation-mobile', seq: i + 1 },
    event_id: `mobile-history-${i + 1}`,
    received_at: new Date(1700000000000 + i).toISOString(),
    event: JSON.stringify({ kind: 'assistant_text', text: `MOBILE_READ_LINE_${String(i + 1).padStart(3, '0')} 这是一段足够长的会话正文，用来在窄屏上产生真实的纵向滚动。`, delivery_id: `mobile-${i + 1}`, turn_id: `mobile-${i + 1}`, stage: 'cli_injected', ts: 1700000000000 + i }),
  })),
  gaps: [], has_older: false, before_seq: null, gap_before: false,
});
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') return html(res, shell);
  if (url.pathname === '/console/login') return html(res, oldLogin);
  if (url.pathname === '/console/login/status') { res.writeHead(202, { 'Content-Type': 'application/json' }); return res.end('{"confirmed":false}'); }
  if (url.pathname === '/np-login') return html(res, newplatLogin);
  const view = url.pathname.match(/^\/view\/([^/]+)(\/.*)?$/);
  if (view) {
    const sub = view[2] || '';
    if (sub === '') return html(res, renderer);
    if (sub === '/status') return json(res, { session: view[1], state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'owner', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
    if (sub === '/tasks' || sub === '/page') return json(res, {});
    if (sub === '/history') return json(res, historyPage());
    if (sub === '/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
      res.write(`data: ${JSON.stringify({ kind: 'assistant_text', text: 'MOBILE_LIVE_151', delivery_id: 'mobile-live', turn_id: 'mobile-live' })}\n\n`);
      req.on('close', () => { try { res.end(); } catch (_) {} });
      return;
    }
  }
  if (url.pathname === '/api/console/platform') return json(res, { role: 'owner', actions: ['read'], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 管理', href: '#agent-groups', status: 'approved' }] });
  if (url.pathname === '/api/console/platform/users') return json(res, { users: [] });
  if (url.pathname === '/api/console/agents') return json(res, { agents: SESSIONS.map((name) => agentRecord(name, true)) });
  if (url.pathname === '/api/console/agent-groups') return json(res, { groups: [], owned_agents: [] });
  if (url.pathname === '/favicon.ico') { res.writeHead(204); return res.end(); }
  if (req.method === 'POST') { req.resume(); return json(res, { ok: true }); }
  res.writeHead(404); res.end('not found');
});

const overflow = (page) => page.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) - document.documentElement.clientWidth);
const rect = (loc) => loc.evaluate((el) => { const r = el.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right }; });
let passed = 0; let failed = 0;
async function grid(name, fn) {
  try { await fn(); passed += 1; console.log(`  PASS ${name}`); }
  catch (error) { failed += 1; console.log(`  FAIL ${name}: ${error && error.message}`); }
}
async function shot(page, name) {
  if (!SHOT_DIR) return;
  fs.mkdirSync(SHOT_DIR, { recursive: true });
  await page.screenshot({ path: path.join(SHOT_DIR, `${name}.png`) });
}
async function openDrawer(page) {
  const toggle = page.locator('#navToggle');
  assert.ok(await toggle.isVisible(), '窄屏必须有可见的抽屉按钮 ☰（抽屉入口缺失或被隐藏）');
  assert.strictEqual(await toggle.getAttribute('aria-expanded'), 'false', '点开前抽屉按钮 aria-expanded 初值必须为 false');
  await toggle.click();
  const opened = await page.waitForFunction(() => { const nav = document.querySelector('nav'); return nav.classList.contains('open') && nav.getBoundingClientRect().x >= 0; }, null, { timeout: 3000 }).then(() => true).catch(() => false);
  assert.ok(opened, '点 ☰ 后抽屉必须展开（nav.open 且移入视口）——抽屉打开接线失效');
  assert.strictEqual(await toggle.getAttribute('aria-expanded'), 'true', '点开后抽屉按钮 aria-expanded 必须为 true');
}
async function waitDrawerClosed(page, why) {
  const closed = await page.waitForFunction(() => !document.querySelector('nav').classList.contains('open'), null, { timeout: 3000 }).then(() => true).catch(() => false);
  assert.ok(closed, why);
  assert.strictEqual(await page.locator('#navToggle').getAttribute('aria-expanded'), 'false', '抽屉收起后按钮 aria-expanded 必须回到 false');
}
async function waitSessionFrame(page, session) {
  await page.locator(`iframe[src*="/view/${session}"]`).waitFor({ state: 'attached' });
  const frame = page.frames().find((f) => f.url().includes(`/view/${session}`));
  await frame.waitForFunction(() => document.getElementById('timeline') && document.getElementById('timeline').children.length >= 100, null, { timeout: 15000 });
  return frame;
}
async function scrollSession(frame) {
  return frame.evaluate(() => {
    let el = document.getElementById('timeline');
    while (el && el !== document.body) {
      const style = getComputedStyle(el);
      if (el.scrollHeight > el.clientHeight + 10 && /(auto|scroll)/.test(style.overflowY)) break;
      el = el.parentElement;
    }
    if (!el || el === document.body) return { found: false };
    el.scrollTop = el.scrollHeight;
    return { found: true, scrollTop: el.scrollTop, scrollHeight: el.scrollHeight, clientHeight: el.clientHeight };
  });
}

(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  try {
    for (const vp of [{ width: 390, height: 844 }, { width: 360, height: 780 }]) {
      const tag = `${vp.width}x${vp.height}`;
      const context = await browser.newContext({ viewport: vp, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
      const page = await context.newPage();
      page.setDefaultTimeout(10000);
      console.log(`viewport ${tag}:`);
      await grid(`old login readable without horizontal overflow (${tag})`, async () => {
        await page.goto(`${base}/console/login`);
        assert.ok(await page.locator('meta[name=viewport]').count() >= 1, '旧登录页必须有 viewport meta');
        assert.strictEqual(await overflow(page), 0, '旧登录页横向溢出必须为 0');
        assert.ok(await page.locator('code').isVisible(), '登录码必须可见');
        await shot(page, `login-old-${tag}`);
      });
      await grid(`new-platform login targets >=40px and no overflow (${tag})`, async () => {
        await page.goto(`${base}/np-login`);
        assert.strictEqual(await overflow(page), 0, '新平台登录页横向溢出必须为 0');
        const small = await page.evaluate(() => [...document.querySelectorAll('input,button')].filter((el) => el.offsetParent !== null).map((el) => ({ id: el.id, h: el.getBoundingClientRect().height })).filter((r) => r.h < 40));
        assert.deepStrictEqual(small, [], `登录页可见 input/button 高度必须≥40px: ${JSON.stringify(small)}`);
        await shot(page, `login-newplat-${tag}`);
      });
      await grid(`workbench drawer opens and closes (${tag})`, async () => {
        await page.goto(`${base}/console`);
        await page.locator('#a button').first().waitFor();
        assert.strictEqual(await overflow(page), 0, '工作台横向溢出必须为 0');
        const scale = await page.evaluate(() => window.visualViewport ? window.visualViewport.scale : 1);
        assert.ok(Math.abs(scale - 1) < 0.01, `viewport meta 必须生效（visualViewport.scale≈1，实测 ${scale}）`);
        assert.ok(await page.locator('#navToggle').isVisible(), '窄屏必须显示抽屉按钮');
        const closed = await rect(page.locator('nav'));
        assert.ok(closed.right <= 0, `抽屉默认必须收起（nav right=${closed.right}）`);
        await openDrawer(page);
        assert.ok(await page.locator('#navMask').isVisible(), '打开时遮罩必须可见');
        await shot(page, `workbench-drawer-${tag}`);
        await page.locator('#navMask').click({ position: { x: vp.width - 20, y: vp.height - 20 } });
        await waitDrawerClosed(page, '点遮罩空白处后抽屉必须收起——遮罩关闭接线失效');
        await openDrawer(page);
        await page.keyboard.press('Escape');
        await waitDrawerClosed(page, '按 ESC 后抽屉必须收起——ESC 关闭接线失效');
      });
      await grid(`open an Agent session from drawer and scroll to read (${tag})`, async () => {
        await openDrawer(page);
        await page.locator('#a button').first().click();
        await waitDrawerClosed(page, '选中 Agent 后抽屉必须自动收起——选中即关接线失效');
        const frame = await waitSessionFrame(page, SESSIONS[0]);
        assert.strictEqual(await overflow(page), 0, '打开会话后工作台横向溢出必须为 0');
        const inner = await frame.evaluate(() => Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) - document.documentElement.clientWidth);
        assert.ok(inner <= 0, `会话阅读页自身横向溢出必须为 0（实测 ${inner}）`);
        const scrolled = await scrollSession(frame);
        assert.ok(scrolled.found && scrolled.scrollTop > 0, `会话必须能纵向滚动阅读: ${JSON.stringify(scrolled)}`);
        const smallTargets = await frame.evaluate(() => [...document.querySelectorAll('button,input,select,a,[role=button]')]
          .filter((el) => el.offsetParent !== null)
          .map((el) => { const r = el.getBoundingClientRect(); return { id: el.id || String(el.className).slice(0, 20), w: Math.round(r.width), h: Math.round(r.height) }; })
          .filter((r) => r.h < 40 || r.w < 40));
        assert.deepStrictEqual(smallTargets, [], `会话阅读页可见点击目标必须≥40px: ${JSON.stringify(smallTargets)}`);
        await shot(page, `session-${tag}`);
      });
      await grid(`tabs scroll horizontally without document overflow (${tag})`, async () => {
        for (const session of SESSIONS.slice(1)) {
          await openDrawer(page);
          await page.locator('#a button', { hasText: session.split('-')[1] }).click();
          await waitDrawerClosed(page, '选中 Agent 后抽屉必须自动收起——选中即关接线失效');
          await page.locator(`iframe[src*="/view/${session}"]`).waitFor({ state: 'attached' });
        }
        await openDrawer(page);
        await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
        await waitDrawerClosed(page, '选中菜单项后抽屉必须自动收起——选中即关接线失效');
        await page.locator('#panes .group-workspace').waitFor();
        const tabs = await page.evaluate(() => { const el = document.getElementById('tabs'); return { scrollWidth: el.scrollWidth, clientWidth: el.clientWidth, overflowX: getComputedStyle(el).overflowX, count: el.children.length }; });
        assert.ok(tabs.count >= 5, `应已打开≥5 个标签页: ${JSON.stringify(tabs)}`);
        assert.strictEqual(tabs.overflowX, 'auto', '窄屏标签条必须横向可滚');
        assert.ok(tabs.scrollWidth > tabs.clientWidth, `标签条必须真的产生横向滚动: ${JSON.stringify(tabs)}`);
        assert.strictEqual(await overflow(page), 0, '多标签＋工作区下 document 横向溢出必须为 0');
        const wideInputs = await page.evaluate(() => [...document.querySelectorAll('.group-workspace input,.group-workspace select')].filter((el) => el.getBoundingClientRect().width > window.innerWidth).length);
        assert.strictEqual(wideInputs, 0, 'Agent 管理工作区输入控件不得宽过视口');
        await shot(page, `tabs-workspace-${tag}`);
      });
      await grid(`click targets >=40px (${tag})`, async () => {
        await openDrawer(page);
        const targets = await page.evaluate(() => {
          const pick = (sel) => [...document.querySelectorAll(sel)].filter((el) => el.offsetParent !== null).map((el) => { const r = el.getBoundingClientRect(); return { sel, w: r.width, h: r.height }; });
          return [...pick('#navToggle'), ...pick('#a button'), ...pick('#platform-items a'), ...pick('#themeBtn'), ...pick('#agentNavMode'), ...pick('#logoutBtn'), ...pick('#tabs .tab'), ...pick('#tabs .tab .x')];
        });
        assert.ok(targets.length >= 10, `取样目标过少: ${targets.length}`);
        const small = targets.filter((r) => r.h < 40 || r.w < 40);
        assert.deepStrictEqual(small, [], `点击目标必须≥40px: ${JSON.stringify(small)}`);
        await page.keyboard.press('Escape');
      });
      await context.close();
    }
    const pc = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await pc.newPage();
    page.setDefaultTimeout(10000);
    console.log('viewport 1440x900 (PC):');
    await grid('PC layout unchanged: sidebar in flow, drawer controls hidden, tabs wrap', async () => {
      await page.goto(`${base}/console`);
      await page.locator('#a button').first().waitFor();
      const layout = await page.evaluate(() => ({
        toggle: getComputedStyle(document.getElementById('navToggle')).display,
        mask: getComputedStyle(document.getElementById('navMask')).display,
        navPosition: getComputedStyle(document.querySelector('nav')).position,
        navWidth: document.querySelector('nav').getBoundingClientRect().width,
        navX: document.querySelector('nav').getBoundingClientRect().x,
        columns: getComputedStyle(document.body).gridTemplateColumns,
        wrap: getComputedStyle(document.getElementById('tabs')).flexWrap,
      }));
      assert.strictEqual(layout.toggle, 'none', 'PC 上抽屉按钮必须隐藏');
      assert.strictEqual(layout.mask, 'none', 'PC 上遮罩必须隐藏');
      assert.strictEqual(layout.navPosition, 'static', 'PC 左栏必须在文档流里');
      assert.strictEqual(layout.navWidth, 240, 'PC 左栏宽度必须仍是 240px');
      assert.strictEqual(layout.navX, 0, 'PC 左栏必须贴左');
      assert.ok(layout.columns.startsWith('240px '), `PC  body 网格必须仍是 240px＋1fr: ${layout.columns}`);
      assert.strictEqual(layout.wrap, 'wrap', 'PC 标签条必须仍是换行布局');
      await shot(page, 'workbench-1440x900');
      await page.locator('#a button').first().click();
      await waitSessionFrame(page, SESSIONS[0]);
      await shot(page, 'session-1440x900');
      await page.goto(`${base}/console/login`);
      await shot(page, 'login-old-1440x900');
      await page.goto(`${base}/np-login`);
      await shot(page, 'login-newplat-1440x900');
    });
    await pc.close();
    console.log(`console mobile: passed=${passed} failed=${failed} mutation=${MUTATION || 'none'}`);
    if (failed) process.exitCode = 1;
  } finally { await browser.close(); server.close(); }
})().catch((error) => { console.error(error); server.close(); process.exitCode = 1; });
