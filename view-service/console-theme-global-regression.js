// 🔴 日夜主题【全局化 + 持久化】的闸(0.2.1.7 · 先写闸、跑到红、再动手)
//
// 业主原话:「◐ 应该要**提出来,放在整个 Agent 列表**,而不是某一个 tab」
// ⇒ 开关的归属在 **console 页**(internal/m8/console_auth.go 的 consolePageHTML),
//   renderer 侧只负责**接收并应用**。本件就是把这句话变成断言。
//
// 装置与 renderer-in-console-iframe-regression.js 同源同手法:
//   · console 页**从生产源码实时抠**(consolePageHTML),不复制一份 ⇒ 改了产品,本件测的就是改后的真件
//   · /view/* 返回**仓内真实的 renderer-canary-sse.html**,不是占位页
//   · 两个 Agent pane ⇒ "已打开的 pane"这个场景是真的(切 tab 不重载,已由探针实测:__marker 仍在)
//
// 🔴 三条推论(后备件第三节,实施方 opus50tc2k 查出)本件**不当注释采信,而是各自变成判据**:
//   ① 同源 localStorage 天然共享      → T4 的"新开 page 仍暗"腿(同 context 共享 localStorage、不共享 sessionStorage)
//   ② 只做"加载时读"救不了已开 pane  → T2 的**隐藏 pane**腿(它是 display:none 的活文档,不会重载)
//   ③ storage 事件不在写入方触发      → T3 用**同一个任务内同步变暗**作判据
//      🔵 不断言"顶层没收到 storage 事件"(那是浏览器语义,将来变了会误伤产品);
//         改为:点下去的**同一个 JS 任务里**就必须已经暗 ⇒ 异步事件不可能是原因。
//
// 环境变量:
//   RENDERER_OVERRIDE=<path>  换一份 renderer 字节(阳性对照用)
//   PW_CHANNEL=chrome         用系统 Chrome
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const A = 'owner-alpha-2642';
const B = 'owner-beta-2642';
const KEY = 'dw-theme';   // 🔴 判据钉住键名:换了名字就等于换了一套偏好,必须显式改这里

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML 起始标记不见了 —— 生产源码结构变了,先修装置');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML 结束标记不见了 —— 生产源码结构变了,先修装置');
  return source.slice(contentStart, end);
}
const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

// 相对亮度(WCAG 公式):用来判"真的变暗了",而不是"某个值变了"
function luminance(rgb) {
  const m = String(rgb).match(/[\d.]+/g);
  if (!m) return null;
  const [r, g, b] = m.slice(0, 3).map((n) => {
    const c = Number(n) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

let pass = 0;
let fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; } catch (e) { console.log('  🔴', name, '->', e && e.message); fail += 1; }
}

// An absent iframe is a retry condition, not permission to inherit
// Playwright's 30s locator timeout on every attempt (the old 50 * 30s = 25 min).
async function frameOf(page, session) {
  const title = `Agent view: ${session}`;
  for (let i = 0; i < 25; i += 1) {
    const handle = await page.locator(`#panes iframe[title="${title}"]`).elementHandle({ timeout: 200 }).catch(() => null);
    const frame = handle ? await handle.contentFrame().catch(() => null) : null;
    if (frame) return frame;
    await page.waitForTimeout(100);
  }
  const viaUrl = page.frames().find((frame) => frame.url().includes(`/view/${encodeURIComponent(session)}`));
  if (viaUrl) return viaUrl;
  throw new Error(`拿不到 ${session} 那个 iframe 的 frame 对象(elementHandle/contentFrame 与 url 两条路都失败)`);
}

async function selftestFrameOfTimeout() {
  let calls = 0;
  const optionsSeen = [];
  const page = {
    locator() {
      return { elementHandle: async (options) => {
        calls += 1;
        optionsSeen.push(options);
        return null;
      } };
    },
    waitForTimeout: async () => {},
    frames: () => [],
  };
  await assert.rejects(() => frameOf(page, 'missing-agent-2642'), /拿不到/);
  assert.strictEqual(calls, 25, '重试数必须有界');
  assert.strictEqual(optionsSeen.every(options => options && options.timeout === 200), true,
    `每次 elementHandle 必须显式限时, 实测 ${JSON.stringify(optionsSeen)}`);
  console.log('frameOf timeout selftest: PASS (25 attempts * explicit 200ms cap)');
}

(async () => {
  if (process.argv.includes('--selftest-frame-timeout')) {
    await selftestFrameOfTimeout();
    return;
  }
  const consoleHTML = consoleHTMLFromProductionSource();
  const rendererBytes = fs.readFileSync(RENDERER);
  console.log(`  · console 页取自生产源码 internal/m8/console_auth.go(${consoleHTML.length} 字节)`);
  console.log(`  · renderer  ${RENDERER}`);
  console.log(`    sha256=${sha256(rendererBytes).slice(0, 16)}… ${rendererBytes.length} 字节`
    + (process.env.RENDERER_OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这是【阳性对照】跑' : ''));

  const json = (res, o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(consoleHTML); }
    if (u === '/api/console/agents') return json(res, { agents: [agentRecord(A), agentRecord(B)] });
    if (u === '/api/console/platform') return json(res, { role: 'owner', actions: ['read', 'submit'], items: [] });
    if (u === `/view/${encodeURIComponent(A)}` || u === `/view/${encodeURIComponent(B)}`) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(rendererBytes);
    }
    if (u.endsWith('/status')) return json(res, { session: 'x', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' });
    if (u.endsWith('/events')) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', 'Connection': 'keep-alive' });
      res.write(': open\n\n');
      return;   // 🔴 不 end:保持长连接,否则 renderer 进断线态、盖一层告警干扰量测
    }
    res.writeHead(404, { 'Content-Type': 'text/plain' }); res.end('not found');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;

  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);

  // ── 装置:开 console、开两个 pane(A 可见 / B 隐藏),返回量测助手 ──────────
  async function openTwoPanes(context) {
    const page = await context.newPage();
    page.on('pageerror', (e) => console.log('    [pageerror top]', String(e.message).slice(0, 200)));
    await page.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
    const frames = {};
    for (const s of [A, B]) {
      await page.locator(`nav button[title="${s}"]`).waitFor({ state: 'attached', timeout: 20000 });
      await page.click(`nav button[title="${s}"]`);
      await page.locator(`#panes iframe[title="Agent view: ${s}"]`).waitFor({ state: 'attached', timeout: 20000 });
      const f = await frameOf(page, s);
      await f.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
      frames[s] = f;
    }
    // 🔵 点 B 再点回 A ⇒ A 可见、B 隐藏(display:none 的活文档)
    await page.click(`#tabs .tab[title="${B}"]`);
    await page.click(`#tabs .tab[title="${A}"]`);
    await page.waitForTimeout(200);
    return { page, frames };
  }

  // 外壳的两个表面:页面有效底色(透明就往上找)+ tab 条底色。
  // 🔵 只量一个会漏掉"body 暗了、tab 条还是白的"这种花脸。
  const shellProbe = () => {
    const eff = (el) => {
      let e = el;
      while (e && e.nodeType === 1) {
        const c = getComputedStyle(e).backgroundColor;
        if (c && !/rgba\(0, 0, 0, 0\)|transparent/.test(c)) return c;
        e = e.parentElement;
      }
      return 'rgb(255, 255, 255)';   // 全透明 ⇒ 浏览器画布默认白
    };
    const nav = document.querySelector('nav');
    const tab = document.querySelector('#tabs .tab');
    return {
      htmlTheme: document.documentElement.dataset.theme || null,
      bodyTheme: document.body.dataset.theme || null,
      pageBg: eff(document.body),
      navBorder: nav ? getComputedStyle(nav).borderRightColor : null,
      tabBg: tab ? getComputedStyle(tab).backgroundColor : null,
    };
  };
  const paneProbe = () => ({
    theme: document.body.dataset.theme || null,
    bg: getComputedStyle(document.body).backgroundColor,
    hasOwnToggle: !!document.getElementById('themeBtn'),
  });

  // 🔴 取 frame 不能靠 page.frames().find(url…) —— 元素已 attached 而 Playwright 的 frame 模型
  //    还没建好的窗口是存在的(实测抖动过一次:变异 MT1 那轮 T4 报"没找到 frame 对象",
  //    那**不是**主题信号,是装置抖动 ⇒ 按判据③,解释不了的红条就是真问题,所以必须修掉)。
  //    改用 elementHandle().contentFrame() + 轮询:拿到的一定是这个元素自己的 frame。
  const shellOf = (page) => page.evaluate(shellProbe);
  const paneOf = (f) => f.evaluate(paneProbe);
  const storeOf = (page) => page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }));

  try {
    // ── T1 开关的【归属】:顶层 console 的 Agent 列表区,不在某个 pane 里 ──────
    await t('T1 🔴 主题开关必须在【顶层 console 的 Agent 列表区】(业主:提出来,不要放在某一个 tab)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page, frames } = await openTwoPanes(context);
        const btn = await page.evaluate(() => {
          const el = document.getElementById('themeBtn');
          if (!el) return null;
          const r = el.getBoundingClientRect();
          const h3 = document.getElementById('agents');
          return { tag: el.tagName, text: (el.textContent || '').trim(), title: el.title || el.getAttribute('aria-label') || '',
                   inNav: !!el.closest('nav'), width: +r.width.toFixed(1), height: +r.height.toFixed(1),
                   agentsHeading: h3 ? (h3.textContent || '').trim() : null };
        });
        assert.ok(btn, 'console 顶层【没有】#themeBtn ⇒ 开关还只在各 pane 里,业主那句"提出来"没落地');
        console.log('    · T1 实测:', JSON.stringify(btn));
        assert.ok(btn.inNav, `开关必须在左栏(nav)里,实测 closest('nav')=false`);
        assert.ok(btn.width > 0 && btn.height > 0, `开关必须可见可点,实测 ${btn.width}x${btn.height}`);
        assert.ok(/◐|主题|夜间|白天|深色|浅色|dark|light|theme/i.test(`${btn.text} ${btn.title}`),
          `开关必须有可访问名(业主得认得出它是什么),实测 text=${JSON.stringify(btn.text)} title=${JSON.stringify(btn.title)}`);
        // 🔵 pane 里那个 ◐ 允许保留(不删业主看惯的控件),但**不能只有它**
        assert.strictEqual(await frames[A].evaluate(() => !!document.getElementById('themeBtn')), true,
          '装置对账:renderer 自己那个 ◐ 应该还在(本件不要求删它)');
      } finally { await context.close(); }
    });

    // ── T2 点顶层 ◐ ⇒ 外壳 + 可见 pane + 【隐藏 pane】三者都真的变暗 ──────────
    await t('T2 🔴 点顶层 ◐ ⇒ console 外壳、可见 pane、以及【隐藏的】pane 都必须真的变暗', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page, frames } = await openTwoPanes(context);
        const before = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]) };
        console.log('    · T2 点前:', JSON.stringify(before));
        // 守卫:点前必须全浅色,且浅色确实"亮" ⇒ 否则"变暗"无从证明,本条自动作废
        for (const [k, v] of [['A', before.A], ['B', before.B]]) {
          assert.ok(v.theme === 'light' || v.theme === null, `守卫:pane ${k} 点前应是浅色,实测 ${v.theme}`);
        }
        const lumBeforePage = luminance(before.shell.pageBg);
        const lumBeforeA = luminance(before.A.bg);
        assert.ok(lumBeforePage > 0.5, `守卫:浅色下外壳有效底色亮度应 >0.5,实测 ${lumBeforePage}(${before.shell.pageBg})⇒ 本条无法证明"变暗",判据失效`);
        assert.ok(lumBeforeA > 0.5, `守卫:浅色下 pane 底色亮度应 >0.5,实测 ${lumBeforeA}(${before.A.bg})⇒ 判据失效`);
        // 守卫:隐藏 pane 必须真是 display:none 的活文档(否则"打到隐藏 pane"这条腿是空的)
        const hidden = await page.evaluate(() => Array.from(document.querySelectorAll('#panes iframe'))
          .map((f) => ({ title: f.title, display: getComputedStyle(f).display })));
        const bDisplay = (hidden.find((h) => h.title === `Agent view: ${B}`) || {}).display;
        assert.strictEqual(bDisplay, 'none', `守卫:pane B 应是隐藏(display:none),实测 ${bDisplay} ⇒ "隐藏 pane 也跟随"这条腿没有意义`);

        await page.click('#themeBtn');
        await page.waitForTimeout(400);
        const after = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]) };
        console.log('    · T2 点后:', JSON.stringify(after));

        const violations = [];
        const dark = (after.shell.htmlTheme === 'dark') || (after.shell.bodyTheme === 'dark');
        if (!dark) violations.push(`外壳没有 data-theme=dark(html=${after.shell.htmlTheme} body=${after.shell.bodyTheme})`);
        const lumAfterPage = luminance(after.shell.pageBg);
        if (!(lumAfterPage < lumBeforePage * 0.5)) {
          violations.push(`外壳有效底色没有真的变暗:亮度 ${lumBeforePage.toFixed(3)} → ${lumAfterPage.toFixed(3)}(${before.shell.pageBg} → ${after.shell.pageBg})`);
        }
        const lumAfterTab = luminance(after.shell.tabBg);
        const lumBeforeTab = luminance(before.shell.tabBg);
        if (!(lumAfterTab < lumBeforeTab * 0.7)) {
          violations.push(`tab 条没有跟着变暗(花脸):亮度 ${lumBeforeTab.toFixed(3)} → ${lumAfterTab.toFixed(3)}(${before.shell.tabBg} → ${after.shell.tabBg})`);
        }
        // 🔵 键是字面量 'A'/'B'(量测对象的形状),不是会话名 —— 首版这里写成了 [A, B](会话名常量)
        //    ⇒ 取到 undefined 抛 TypeError。修的是**索引**,判据一字未削弱。
        for (const [k, tag] of [['A', '可见 pane A'], ['B', '🔴 隐藏 pane B']]) {
          if (after[k].theme !== 'dark') violations.push(`${tag} 的 body[data-theme] 仍是 ${after[k].theme},不是 dark`);
          const l0 = luminance(before[k].bg); const l1 = luminance(after[k].bg);
          if (!(l1 < l0 * 0.5)) violations.push(`${tag} 没有真的变暗:亮度 ${l0.toFixed(3)} → ${l1.toFixed(3)}(${before[k].bg} → ${after[k].bg})`);
        }
        assert.deepStrictEqual(violations, [], `主题没有全局生效(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });

    // ── T3 写入方自己【同步】变暗 ⇒ 不可能是靠 storage 事件(推论③) ──────────
    await t('T3 🔴 点下去的【同一个 JS 任务里】外壳就必须已变暗(证明写入方自己 apply,不是等 storage 事件)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page } = await openTwoPanes(context);
        const r = await page.evaluate((key) => {
          const seen = [];
          window.addEventListener('storage', (e) => seen.push(e.key));
          document.getElementById('themeBtn').click();
          // 🔴 同一个任务内立刻读:异步任务(storage 事件、postMessage、微任务后的宏任务)都还没机会跑
          return { themeNow: document.documentElement.dataset.theme || document.body.dataset.theme || null,
                   pageBgNow: getComputedStyle(document.body).backgroundColor,
                   storedNow: (() => { try { return localStorage.getItem(key); } catch (_) { return 'THREW'; } })(),
                   storageEventsSeen: seen.length };
        }, KEY);
        console.log('    · T3 实测(同一任务内):', JSON.stringify(r));
        assert.strictEqual(r.themeNow, 'dark',
          `🔴 点完的同一个任务里外壳还是 ${r.themeNow} ⇒ 它是在等异步事件(推论③:写入方等不到自己的 storage 事件 ⇒ 永远不变)`);
        assert.ok(luminance(r.pageBgNow) < 0.5, `同一任务内外壳底色应已变暗,实测 ${r.pageBgNow}(亮度 ${luminance(r.pageBgNow)})`);
        assert.strictEqual(r.storageEventsSeen, 0,
          '本条的前提是"同一个任务内不可能收到 storage 事件";若你看到 >0,说明前提不成立、本条判别力失效,请重新设计');
        assert.strictEqual(r.storedNow, 'dark', `同一任务内就应已落盘 ${KEY}=dark,实测 ${r.storedNow}`);
      } finally { await context.close(); }
    });

    // ── T4 持久化:落 localStorage(不许 sessionStorage 顶替)· 重载仍暗 · 新开 tab 仍暗 ──
    await t('T4 🔴 持久化:localStorage 有、sessionStorage 没有;重载 console 仍暗;【新开一个 tab】仍暗', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page, frames } = await openTwoPanes(context);
        await page.click('#themeBtn');
        await page.waitForTimeout(300);
        const store = await storeOf(page);
        console.log('    · T4 存储实测:', JSON.stringify(store));
        const violations = [];
        if (store.local[KEY] !== 'dark') violations.push(`localStorage['${KEY}'] 应是 dark,实测 ${JSON.stringify(store.local[KEY])}(全部键:${JSON.stringify(Object.keys(store.local))})`);
        if (KEY in store.session) violations.push(`🔴 不得用 sessionStorage 顶替:它不跨 tab ⇒ 不满足业主"含持久化"。实测 sessionStorage['${KEY}']=${JSON.stringify(store.session[KEY])}`);
        // pane 侧不该各存一份互相打架(单一真相源)
        const paneStore = await frames[A].evaluate((k) => ({ local: localStorage.getItem(k), session: sessionStorage.getItem(k) }), KEY);
        console.log('    · T4 pane 内存储:', JSON.stringify(paneStore));

        // 腿 2:重载 console ⇒ 外壳与【重载后新开的】两个 pane 都仍暗
        await page.reload({ waitUntil: 'domcontentloaded' });
        await page.waitForTimeout(300);
        const afterReload = await shellOf(page);
        if (!(((afterReload.htmlTheme === 'dark') || (afterReload.bodyTheme === 'dark')) && luminance(afterReload.pageBg) < 0.5)) {
          violations.push(`重载 console 后外壳没有回到暗色:${JSON.stringify(afterReload)} ⇒ "加载时读"这条腿缺了`);
        }
        const frames2 = {};
        for (const s of [A, B]) {
          await page.click(`nav button[title="${s}"]`);
          await page.locator(`#panes iframe[title="Agent view: ${s}"]`).waitFor({ state: 'attached', timeout: 20000 });
          const f = await frameOf(page, s);
          await f.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
          frames2[s] = f;
        }
        await page.waitForTimeout(300);
        for (const s of [A, B]) {
          const p = await paneOf(frames2[s]);
          if (p.theme !== 'dark' || luminance(p.bg) >= 0.5) {
            violations.push(`重载后【新开】的 pane ${s} 不是暗色:${JSON.stringify(p)} ⇒ 新 pane 起来时没读偏好`);
          }
        }

        // 腿 3:🔴 判别 localStorage / sessionStorage / 纯内存 的那一条
        //      同一 context 的新 page 共享 localStorage、**不共享** sessionStorage
        const page2 = await context.newPage();
        await page2.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        await page2.waitForTimeout(300);
        const fresh = await shellOf(page2);
        console.log('    · T4 新开 tab 实测:', JSON.stringify(fresh));
        if (!(((fresh.htmlTheme === 'dark') || (fresh.bodyTheme === 'dark')) && luminance(fresh.pageBg) < 0.5)) {
          violations.push(`新开一个 tab 打开 console 是浅色:${JSON.stringify(fresh)} ⇒ 说明偏好没落到跨 tab 的存储里(sessionStorage/内存都不算持久化)`);
        }
        await page2.close();
        assert.deepStrictEqual(violations, [], `持久化不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });

    // ── T5 两个入口必须一致:在 pane 里点 ◐ ⇒ 外壳与另一个 pane 都跟随 ────────
    await t('T5 🔴 在 pane A 里点它自己的 ◐ ⇒ console 外壳与 pane B 也必须跟随(不是各管各的)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page, frames } = await openTwoPanes(context);
        const before = { shell: await shellOf(page), B: await paneOf(frames[B]) };
        assert.ok(luminance(before.shell.pageBg) > 0.5, `守卫:点前外壳应是浅色,实测 ${before.shell.pageBg}`);
        assert.ok(frames[A] && await frames[A].evaluate(() => !!document.getElementById('themeBtn')), '守卫:pane A 里应有它自己的 ◐');
        await frames[A].click('#themeBtn');
        await page.waitForTimeout(500);
        const after = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]), store: await storeOf(page) };
        console.log('    · T5 实测:', JSON.stringify(after));
        const violations = [];
        if (!(luminance(after.shell.pageBg) < luminance(before.shell.pageBg) * 0.5)) {
          violations.push(`pane 内点 ◐,console 外壳没跟随:${before.shell.pageBg} → ${after.shell.pageBg}`);
        }
        if (after.B.theme !== 'dark' || luminance(after.B.bg) >= 0.5) {
          violations.push(`pane 内点 ◐,【另一个】pane B 没跟随(它自己是暗的、别人还是白的 ⇒ 正是业主说的"某一个 tab"):${JSON.stringify(after.B)}`);
        }
        if (after.store.local[KEY] !== 'dark') {
          violations.push(`pane 内点 ◐ 没有落盘到 localStorage['${KEY}'],实测 ${JSON.stringify(after.store.local[KEY])}`);
        }
        assert.deepStrictEqual(violations, [], `两个入口不一致(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });

    // ── T6 一个 pane 都不开:钉住"是 console 自己落的盘",不是 renderer 替它落的 ──────
    // 🔵 为什么要这一条:T2~T5 里都开着 pane,而 renderer 收到广播后**也会**写一次存储
    //    ⇒ "console 侧不落盘"这个变异会被 renderer 掩盖成【零转红】(我自己先想到的漏洞)。
    //    一个 pane 都不开 ⇒ 这一条里唯一可能落盘的只有 console 自己。
    await t('T6 🔴 一个 pane 都不开:console 自己点 ◐ 也必须落盘,且重载后保持暗', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const page = await context.newPage();
        page.on('pageerror', (e) => console.log('    [pageerror top]', String(e.message).slice(0, 200)));
        await page.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        const panes = await page.evaluate(() => document.querySelectorAll('#panes iframe').length);
        assert.strictEqual(panes, 0, `守卫:本条要求一个 pane 都不开,实测 ${panes} 个 ⇒ 判别力失效(renderer 会替 console 落盘)`);
        const before = await shellOf(page);
        assert.ok(luminance(before.pageBg) > 0.5, `守卫:点前外壳应是浅色,实测 ${before.pageBg} ⇒ 判据失效`);
        await page.click('#themeBtn');
        await page.waitForTimeout(250);
        const store = await storeOf(page);
        console.log('    · T6 实测(无 pane):', JSON.stringify({ store, afterClick: await shellOf(page) }));
        const violations = [];
        if (store.local[KEY] !== 'dark') {
          violations.push(`没有任何 pane 时,console 自己没把偏好落进 localStorage['${KEY}'],实测 ${JSON.stringify(store.local[KEY])}`
            + ' ⇒ 那说明 T2~T5 里的"落盘"全是 renderer 替它做的,console 侧那条腿是空的');
        }
        if (KEY in store.session) violations.push(`🔴 不得用 sessionStorage 顶替:实测 sessionStorage['${KEY}']=${JSON.stringify(store.session[KEY])}`);
        await page.reload({ waitUntil: 'domcontentloaded' });
        await page.waitForTimeout(250);
        const after = await shellOf(page);
        if (!(luminance(after.pageBg) < 0.5)) violations.push(`重载后外壳没有回到暗色:${JSON.stringify(after)} ⇒ console 侧"加载时读"这条腿缺了`);
        assert.deepStrictEqual(violations, [], `console 侧独立持久化不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });
    // ── T7 存储被禁(隐私模式)⇒ 只剩广播这一条路 ────────────────────────────────
    // 🔵 为什么必须有这一条:变异 MT2(把 renderer 的频道接收去掉)在 T1~T6 下【零转红】——
    //    因为 console 落盘会触发 renderer 的 storage 监听,两条路**互为冗余、互相掩盖**。
    //    🔴 冗余不是错(隐私模式下 localStorage 取用即抛,那时只有频道能用),
    //    但**冗余必须各自被钉住**,否则删掉任何一条都没人发现。这条钉的是频道那条路。
    // 🔵 只禁 localStorage、**不禁 sessionStorage**:renderer:1742 裸用 sessionStorage(无 try/catch),
    //    禁它会让整页脚本在加载时就抛 ⇒ 红得与主题无关、解释不了。(那条裸用是既有缺陷,另行登记,本格不修。)
    await t('T7 🔴 localStorage 被禁时,广播仍必须把主题送到【隐藏的】pane(钉住 BroadcastChannel 这条路)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      await context.addInitScript(() => {
        Object.defineProperty(window, 'localStorage', { configurable: true,
          get() { throw new Error('dw-probe: localStorage disabled(模拟隐私模式)'); } });
      });
      try {
        const { page, frames } = await openTwoPanes(context);
        const p1 = await page.evaluate(() => { try { return String(window.localStorage); } catch (e) { return 'THREW:' + e.message; } });
        const p2 = await frames[B].evaluate(() => { try { return String(window.localStorage); } catch (e) { return 'THREW:' + e.message; } });
        assert.ok(/^THREW/.test(p1), `守卫:顶层 localStorage 应取用即抛,实测 ${p1} ⇒ 本条退化成 T2,判别力失效`);
        assert.ok(/^THREW/.test(p2), `守卫:pane 内 localStorage 也应取用即抛,实测 ${p2}`);
        const before = { shell: await shellOf(page), B: await paneOf(frames[B]) };
        assert.ok(luminance(before.shell.pageBg) > 0.5, `守卫:点前外壳应是浅色,实测 ${before.shell.pageBg}`);
        await page.click('#themeBtn');
        await page.waitForTimeout(400);
        const after = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]) };
        console.log('    · T7 实测(存储被禁):', JSON.stringify(after));
        const violations = [];
        if (!(luminance(after.shell.pageBg) < luminance(before.shell.pageBg) * 0.5)) {
          violations.push(`存储被禁时外壳自己都没变暗:${JSON.stringify(after.shell)}`);
        }
        for (const [k, tag] of [['A', '可见 pane A'], ['B', '🔴 隐藏 pane B']]) {
          if (after[k].theme !== 'dark' || luminance(after[k].bg) >= 0.5) {
            violations.push(`存储被禁时 ${tag} 没跟随 ⇒ 广播那条路是断的(平时被 storage 事件掩盖着):${JSON.stringify(after[k])}`);
          }
        }
        assert.deepStrictEqual(violations, [], `广播路径不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });

    // ── T8 没有 BroadcastChannel ⇒ storage 事件那条退路必须顶上 ──────────────────
    // 🔵 与 T7 成对:两条冗余路径**各钉一条**。这条钉的是 storage 事件那条,
    //    对应变异 MT7(去掉 renderer 的 storage 监听)—— 没有这条,MT7 会零转红。
    await t('T8 🔴 浏览器没有 BroadcastChannel 时,主题仍必须全局生效(钉住 storage 事件这条退路)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      await context.addInitScript(() => { delete window.BroadcastChannel; });
      try {
        const { page, frames } = await openTwoPanes(context);
        const has = await page.evaluate(() => ('BroadcastChannel' in window));
        const hasPane = await frames[B].evaluate(() => ('BroadcastChannel' in window));
        assert.strictEqual(has, false, '守卫:顶层应已没有 BroadcastChannel,否则本条只是在重跑 T2');
        assert.strictEqual(hasPane, false, `守卫:pane 内也应没有 BroadcastChannel,实测 ${hasPane}`);
        const before = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]) };
        assert.ok(luminance(before.shell.pageBg) > 0.5, `守卫:点前外壳应是浅色,实测 ${before.shell.pageBg}`);
        await page.click('#themeBtn');
        await page.waitForTimeout(500);
        const after = { shell: await shellOf(page), A: await paneOf(frames[A]), B: await paneOf(frames[B]) };
        console.log('    · T8 实测(无 BroadcastChannel):', JSON.stringify(after));
        const violations = [];
        if (!(luminance(after.shell.pageBg) < luminance(before.shell.pageBg) * 0.5)) {
          violations.push(`无频道时外壳没变暗:${JSON.stringify(after.shell)}`);
        }
        for (const [k, tag] of [['A', '可见 pane A'], ['B', '🔴 隐藏 pane B']]) {
          if (after[k].theme !== 'dark' || luminance(after[k].bg) >= 0.5) {
            violations.push(`无频道时 ${tag} 没跟随 ⇒ storage 事件那条退路是断的:${JSON.stringify(after[k])}`);
          }
        }
        assert.deepStrictEqual(violations, [], `storage 退路不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
      } finally { await context.close(); }
    });
    // ── T9 两个 tab **同时开着**:A 点 ◐ ⇒ B 必须【实时】跟随(不是等 B 重载读档) ──────
    // 🔵 这条补的是一个我自己列出来的缺口:T4 的"新开 tab 仍暗"验的是**加载时读档**,
    //    而"两个 tab 都开着、一边点另一边立刻跟"走的是**跨文档广播**,是另一条腿,不能互相代替。
    //    🔴 不重载 B —— 一重载就退化成 T4 那条腿了。
    await t('T9 🔴 两个 tab 同时开着,在 A 点 ◐ ⇒ B 必须实时跟随(不重载 B)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const pageA = await context.newPage();
        await pageA.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        const pageB = await context.newPage();
        await pageB.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        const a0 = await shellOf(pageA);
        const b0 = await shellOf(pageB);
        assert.ok(luminance(a0.pageBg) > 0.5 && luminance(b0.pageBg) > 0.5,
          `守卫:两个 tab 点前都应是浅色,实测 ${a0.pageBg} / ${b0.pageBg} ⇒ 否则"跟随"无从证明`);
        const shared = await pageB.evaluate((k) => { try { localStorage.setItem(k, 'probe'); const v = localStorage.getItem(k); localStorage.removeItem(k); return v === 'probe'; } catch (e) { return false; } }, KEY);
        assert.strictEqual(shared, true, '守卫:同 context 的两个 page 应共享同一份 localStorage,否则本条测不到跨 tab');
        await pageA.click('#themeBtn');
        await pageB.waitForTimeout(600);
        const a1 = await shellOf(pageA);
        const b1 = await shellOf(pageB);
        console.log('    · T9 实测:', JSON.stringify({ tabA: a1, tabB: b1 }));
        const violations = [];
        if (!(luminance(a1.pageBg) < 0.5)) violations.push(`点的那个 tab 自己没变暗:${JSON.stringify(a1)}`);
        if (!(luminance(b1.pageBg) < 0.5)) violations.push(`🔴 另一个 tab 没有实时跟随(它全程没被重载):${JSON.stringify(b1)} ⇒ 跨 tab 那条路是断的`);
        assert.deepStrictEqual(violations, [], `跨 tab 实时同步不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
        await pageB.close();
        await pageA.close();
      } finally { await context.close(); }
    });
    // ── T10 两个 tab + **没有 BroadcastChannel** ⇒ console 侧的 storage 退路必须顶上 ──────
    // 🔵 为什么要有这一条:变异 MT8(把 console 的 storage 监听去掉)在 T1~T9 下【零转红】——
    //    跨 tab 那条腿被频道掩盖了。T9 是"**有**频道"的跨 tab,T10 是"**没有**频道"的跨 tab,
    //    两条腿各钉一条路 ⇒ 删掉任何一条都会有人发现。
    // 🔴 这与 T7/T8 是同一个纪律:冗余是故意的,但**冗余必须各自被钉住**。
    await t('T10 🔴 没有 BroadcastChannel 时,两个 tab 之间仍必须实时同步(钉住 console 侧的 storage 退路)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      await context.addInitScript(() => { delete window.BroadcastChannel; });
      try {
        const pageA = await context.newPage();
        await pageA.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        const pageB = await context.newPage();
        await pageB.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
        assert.strictEqual(await pageA.evaluate(() => ('BroadcastChannel' in window)), false, '守卫:tab A 应已没有 BroadcastChannel');
        assert.strictEqual(await pageB.evaluate(() => ('BroadcastChannel' in window)), false, '守卫:tab B 应已没有 BroadcastChannel,否则本条只是在重跑 T9');
        const a0 = await shellOf(pageA);
        const b0 = await shellOf(pageB);
        assert.ok(luminance(a0.pageBg) > 0.5 && luminance(b0.pageBg) > 0.5,
          `守卫:两个 tab 点前都应是浅色,实测 ${a0.pageBg} / ${b0.pageBg}`);
        await pageA.click('#themeBtn');
        await pageB.waitForTimeout(700);   // 🔴 全程不重载 B
        const a1 = await shellOf(pageA);
        const b1 = await shellOf(pageB);
        console.log('    · T10 实测(无频道):', JSON.stringify({ tabA: a1, tabB: b1 }));
        const violations = [];
        if (!(luminance(a1.pageBg) < 0.5)) violations.push(`点的那个 tab 自己没变暗:${JSON.stringify(a1)}`);
        if (!(luminance(b1.pageBg) < 0.5)) violations.push(`🔴 没有频道时,另一个 tab 没有实时跟随(全程未重载):${JSON.stringify(b1)} ⇒ console 侧的 storage 退路是断的(平时被频道掩盖着)`);
        assert.deepStrictEqual(violations, [], `无频道时的跨 tab 同步不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
        await pageB.close();
        await pageA.close();
      } finally { await context.close(); }
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}   (日夜主题全局化 + 持久化)`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
