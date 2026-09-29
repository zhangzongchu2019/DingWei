// 🔴 Agent 编组入口的闸(0.2.1.7)—— **浏览器语义**那一半
//
// 另一半(发给谁 / 带什么 target)在 internal/m8/console_platform_agentgroups_test.go。
// 🔴 两腿**机器耦合**,不是各测各的:本件 `/api/console/platform` 的应答**不是夹具手写的**,
//    是 `go test -run TestConsolePlatformPayloadDump` 现场从真 handler 产出后读进来的
//    ⇒ 「后端把 target 去掉」这个变异会**同时**打到本件的点击腿。
//    (若载荷手写在这里,那个变异就只红 Go 腿,本件测的是夹具 —— 判据③ 要求每条红都能解释。)
//
// 为什么入口必须 target="_blank"(实测到的活缺陷):
//    /admin/agent-groups 是**独立文档**;console 右栏是一堆活着的 iframe,每个一条 SSE。
//    顶层导航过去 ⇒ pane 全丢、SSE 全断、后退回不来(实测 pane 数 2→0)。
// builtin "Agents" 已按业主裁定从服务端移除。G3/G4 现在守它不得重新长回。
//
// 🔴 口径变更(S-GROUP-ENTRY,2026-09-29):「Agent 编组」入口由"只对 management 下发"改为
//    **对所有 principal 下发** ⇒ G4 从"owner 不得看到它"翻成"owner **必须**看到它"。
//    由来:业主逐字「界面上没有 Agent 编组(只有 spec 和任务)」—— 而「加入到其他业主编组」
//    那个面板只在这个入口**里面**(console_auth.go 的 openGroupWorkspace 仅一个调用点),
//    它的目标用户恰恰是默认 role=owner 的另一位业主。
//    派工 runbooks/DISPATCH-20260929-S-GROUP-ENTRY.md ·
//    背景 runbooks/DEFECT-group-entry-is-management-only-20260929.md
//    🔵 G4 仍守两件旧事:builtin Agents 不得长回;**admin(平台管理)不得下发给 owner**。
//
// 装置与 console-theme-global-regression.js 同源同手法:
//    console 页**从生产源码实时抠**(consolePageHTML),/view/* 返回仓内真实 renderer 字节。
//
// 环境变量:
//    POSITIVE_CONTROL=1  阳性对照跑:把产品发出的那一项**改回旧 admin popup 形状**(带 target=_blank)
//                        ⇒ 期望**红的正好是 G1/G2/G4**,G3 仍绿(它守 builtin Agents,与本变异无关)。
//                        🔴 少红 = 那一条断言没有判别力(假绿);多红 = 装置异常或另有回归(假故障)。
//                           两种都不算"对照成立" ⇒ 末尾**逐格核红格集合**,不是只看"有没有红"。
//    PW_CHANNEL=chrome   用系统 Chrome
//    GO_BIN=<path>       go 可执行文件(默认 PATH 里的 go)
// 退出码:0 全过 · 1 断言失败 · 3 环境缺失(没有 go toolchain ⇒ 本件**不验**,不是产品坏了)
const assert = require('assert');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');

const REPO = path.join(__dirname, '..');
const GO_SOURCE = path.join(REPO, 'internal', 'm8', 'console_auth.go');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const A = 'owner-alpha-2642';
const B = 'owner-beta-2642';
const ENTRY_ID = 'agent-groups';
const ENTRY_HREF = '#agent-groups';
const CONTROL = process.env.POSITIVE_CONTROL === '1';
const GO_BIN = process.env.GO_BIN || 'go';
// 🔴 阳性对照的形状:把产品发出的那一项改回**旧 admin popup**(独立文档 + target=_blank)。
// 🔵 它只用来证明 G1/G2/G4 的断言有牙,**不是**产品证据;产品级证据由变异 M1(真去掉 target)给。
// 🔴 旧版这里还有一条"产品没发该项就**合成**一项"的退路(SYNTHESIZED_ENTRY),已删:
//    合成出来的项形状是**对的** ⇒ 对照跑会在那条腿上绿,而那个绿会被读成"对照成立"(假绿)。
//    现在改成:前提不满足就**硬中止**(见下面 CONTROL 前提自检),一条结论都不产。
const CONTROL_HREF = '/admin/agent-groups';

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

// ── 从真 Go handler 取平台载荷(机器耦合腿)──────────────────────────────
function dumpPayloadsFromGo() {
  const out = path.join('/tmp', `dw-console-platform-${process.pid}.jsonl`);
  try {
    execFileSync(GO_BIN, ['test', './internal/m8/', '-run', 'TestConsolePlatformPayloadDump', '-count=1'], {
      cwd: REPO, env: { ...process.env, DW_CONSOLE_PLATFORM_PAYLOAD_OUT: out }, stdio: ['ignore', 'pipe', 'pipe'],
    });
  } catch (e) {
    const stderr = String((e && (e.stderr || e.message)) || '');
    if (/command not found|ENOENT|no such file/i.test(stderr) || (e && e.code === 'ENOENT')) {
      console.log('  ⚠️  环境缺失:找不到 go 可执行文件 ⇒ 取不到真载荷,本件**不验**(不是产品坏了)');
      process.exit(3);
    }
    // go test 本身失败(例如闸正在红)也算取不到载荷 —— 但要把原因原样打出来,不吞。
    console.log('  🔴 go test 取载荷失败,原样输出如下:');
    console.log(stderr.split('\n').slice(0, 25).map((l) => `      ${l}`).join('\n'));
    throw e;
  }
  const lines = fs.readFileSync(out, 'utf8').split('\n').filter((l) => l.trim());
  fs.unlinkSync(out);
  const byPrincipal = {};
  for (const line of lines) {
    const rec = JSON.parse(line);
    byPrincipal[rec.principal] = rec.payload;
  }
  assert.ok(byPrincipal.manager && byPrincipal.owner, `载荷缺角色:实到 ${Object.keys(byPrincipal).join(',')}`);
  return byPrincipal;
}

// 阳性对照:把产品发出的那一项改回**旧 admin popup 形状**。
// 🔴 前提(该项存在)已在启动时自检过并对**两个角色**一视同仁 ⇒ 这里不再需要"缺项就合成"的退路。
//    旧版那条 `if (principal !== 'manager') return …owner-untouched` 一并删除:
//    新口径下 G4 也断言 owner 那一项的形状,把 owner 腿排除在对照之外只会造出假绿或假故障。
function makeControlPayload(payload) {
  const items = Array.isArray(payload.items) ? payload.items.map((x) => ({ ...x })) : [];
  const i = items.findIndex((x) => x && x.id === ENTRY_ID);
  if (i < 0) throw new Error(`装置前提坏了:${payload.role} 的载荷里没有 ${ENTRY_ID},对照无从施加`);
  items[i].href = CONTROL_HREF;
  items[i].target = '_blank';
  return { ...payload, items, __control: 'restored-legacy-admin-popup' };
}

let pass = 0;
let fail = 0;
// 🔴 记下**哪几格**红了,不只记条数:阳性对照的判据是"红的正好是该红的那几条"。
const failedIDs = [];
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; } catch (e) { console.log('  🔴', name, '->', e && e.message); fail += 1; failedIDs.push(name.slice(0, 2)); }
}

(async () => {
  let consoleHTML = consoleHTMLFromProductionSource();
  if (CONTROL) consoleHTML = consoleHTML.replace("if(item.id==='agent-groups') a.onclick=function(ev){ev.preventDefault();openGroupWorkspace();};", "");
  const rendererBytes = fs.readFileSync(RENDERER);
  const payloads = dumpPayloadsFromGo();
  if (CONTROL) {
    // 🔴 阳性对照的前提是"产品确实发出了该项",对照才有东西可改。
    //    前提不满足时**硬中止、一条结论都不产** —— 与"阴格不过则后面的绿不算绿"同形:
    //    否则缺项的那条腿会以"绿"收场,而那个绿会被读成"对照成立"。
    for (const who of ['manager', 'owner']) {
      const ids = (((payloads[who] || {}).items) || []).map((x) => (x && x.id));
      if (!ids.includes(ENTRY_ID)) {
        console.log(`  🔴 阳性对照前提塌了:${who} 的载荷里没有 ${ENTRY_ID},items=${JSON.stringify(ids)}`);
        console.log('     对照要改的是**产品发出的那一项**;它不存在 ⇒ 本跑不产任何结论(不是产品坏了,是对照无从施加)');
        process.exit(3);
      }
    }
  }
  console.log(`  · console 页取自生产源码 internal/m8/console_auth.go(${consoleHTML.length} 字节)`);
  console.log(`  · 平台载荷取自真 Go handler(TestConsolePlatformPayloadDump):manager/owner 两份`);
  console.log(`    manager.items=${JSON.stringify((payloads.manager.items || []).map((x) => [x.id, x.target || '']))}`);
  console.log(`    owner.items  =${JSON.stringify((payloads.owner.items || []).map((x) => [x.id, x.target || '']))}`);
  console.log(`  · renderer  ${RENDERER}`);
  console.log(`    sha256=${sha256(rendererBytes).slice(0, 16)}… ${rendererBytes.length} 字节`);
  if (CONTROL) console.log('  🔵 POSITIVE_CONTROL=1:把入口改回旧 admin popup,期望红的正好是 G1/G2/G4、G3 仍绿');

  // 当前该给哪个角色的载荷 —— 每个角色用**一个新页面**取,靠页面自己启动时那次 refreshPlatform()
  let currentPrincipal = 'manager';
  const json = (res, o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(consoleHTML); }
    if (u === '/api/console/agents') return json(res, { agents: [{ session: A, online: true }, { session: B, online: true }] });
    if (u === '/api/console/platform') {
      const p = payloads[currentPrincipal] || { role: 'owner', actions: [], items: [] };
      return json(res, CONTROL ? makeControlPayload(p) : p);
    }
    if (u === '/api/console/agent-groups') return json(res, { principal: 'manager', owned_agents: [{ id: 'a', key_id: 'ka', session_name: A }], groups: [] });
    if (u === `/view/${encodeURIComponent(A)}` || u === `/view/${encodeURIComponent(B)}`) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(rendererBytes);
    }
    if (u.endsWith('/status')) return json(res, { session: 'x', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' });
    if (u.endsWith('/events')) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', 'Connection': 'keep-alive' });
      res.write(': open\n\n');
      return;   // 🔴 不 end:保持长连接,否则 renderer 进断线态,量到的是断线不是入口
    }
    res.writeHead(404, { 'Content-Type': 'text/plain' }); res.end('not found');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;

  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);

  // 取 frame 用 elementHandle().contentFrame() + 轮询(与主题闸同一手法:
  // page.frames().find(url…) 在"元素已 attached 而 frame 模型还没建好"的窗口里会抖)
  // 🔴 elementHandle() **必须显式给短超时**:它默认自动等待 30s,而本件在失败路径上
  //    (顶层被导航走 ⇒ iframe 根本不存在)会 50 次重试 × 30s = **25 分钟挂死**。
  //    改前第一版阳性对照跑就挂在这里(2 分钟无输出)才查出来。
  //    🔵 主题闸 console-theme-global-regression.js 里的同名助手有同一潜在问题(那边元素总在,所以没暴露)。
  async function frameOf(page, session) {
    const title = `Agent view: ${session}`;
    for (let i = 0; i < 25; i += 1) {
      const handle = await page.locator(`#panes iframe[title="${title}"]`).elementHandle({ timeout: 200 }).catch(() => null);
      const f = handle ? await handle.contentFrame().catch(() => null) : null;
      if (f) return f;
      await page.waitForTimeout(100);
    }
    throw new Error(`拿不到 ${session} 那个 iframe 的 frame 对象`);
  }

  async function openTwoPanes(context) {
    const page = await context.newPage();
    page.on('pageerror', (e) => console.log('    [pageerror top]', String(e.message).slice(0, 160)));
    await page.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
    const frames = {};
    for (const s of [A, B]) {
      await page.locator(`nav button[title="${s}"]`).waitFor({ state: 'attached', timeout: 20000 });
      await page.click(`nav button[title="${s}"]`);
      await page.locator(`#panes iframe[title="Agent view: ${s}"]`).waitFor({ state: 'attached', timeout: 20000 });
      frames[s] = await frameOf(page, s);
      await frames[s].waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
      // 身份标记:iframe 对象一旦被重建/文档一旦重载,这个标记就没了 ⇒ 用它证明"还是原来那个对象"
      await frames[s].evaluate((tag) => { window.__marker = tag; }, `${s}-alive`);
    }
    await page.click(`#tabs .tab[title="${B}"]`);
    await page.click(`#tabs .tab[title="${A}"]`);
    await page.waitForTimeout(200);
    return { page, frames };
  }

  // 点击后的现场快照 —— 断言消息里带上它,红了才看得出"到底发生了什么"
  const snapshot = (page) => page.evaluate(() => ({
    url: location.href,
    hash: location.hash,
    consoleStillThere: !!document.getElementById('platform-items'),
    panes: document.querySelectorAll('#panes iframe').length,
    paneTitles: Array.from(document.querySelectorAll('#panes iframe')).map((f) => f.title),
    tabs: (typeof tabs !== 'undefined' && tabs && typeof tabs.size === 'number') ? tabs.size : -1,
    agentButtons: document.querySelectorAll('nav button').length,
  })).catch((e) => ({ snapshotError: String(e).slice(0, 120) }));

  const readMarkers = async (page) => {
    const out = {};
    for (const s of [A, B]) {
      try {
        const f = await frameOf(page, s);
        out[s] = await f.evaluate(() => (typeof window.__marker === 'undefined' ? 'gone' : window.__marker));
        out[`${s}_sse`] = await f.evaluate(() => (typeof eventSource !== 'undefined' && eventSource ? eventSource.readyState : -1));
      } catch (e) { out[s] = `frame-gone(${String(e).slice(0, 40)})`; }
    }
    return out;
  };

  const platformEntry = (page, id) => page.evaluate((entryId) => {
    const all = document.querySelectorAll(`#platform-items a[data-item-id="${entryId}"]`);
    if (!all.length) return null;
    const a = all[0];
    return { count: all.length, href: a.getAttribute('href'), target: a.getAttribute('target'), text: a.textContent };
  }, id);

  try {
    // ── G1 入口存在、形状是 console 内部 tab ────────────────────────────────
    currentPrincipal = 'manager';
    await t(`G1 🔴 management 必须拿到「${ENTRY_ID}」内部 tab 平台项`, async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page } = await openTwoPanes(context);
        const e = await platformEntry(page, ENTRY_ID);
        assert.ok(e, `#platform-items 里没有 data-item-id="${ENTRY_ID}" 的项(它必须是**平台项形状**,不是往 nav 里硬塞 HTML)`);
        assert.strictEqual(e.count, 1, `入口必须唯一可寻址,实到 ${e.count} 个同 id 项`);
        assert.strictEqual(e.href, ENTRY_HREF, `href=${e.href},应为 ${ENTRY_HREF}`);
        assert.ok(!e.target, `内部 tab 不得带 target,实为 ${e.target}`);
      } finally { await context.close(); }
    });

    // ── G2 点击后:console 内 tab 打开、pane 对象与 SSE 都还活着 ─────────────
    currentPrincipal = 'manager';
    await t('G2 🔴 点击入口后 pane 数不变、iframe 对象不变、SSE 不断、console 页还在', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page } = await openTwoPanes(context);
        const before = await snapshot(page);
        assert.strictEqual(before.panes, 2, `前置:必须真开着 2 个 pane,实为 ${before.panes}(否则本格测的是空集)`);
        assert.strictEqual(before.tabs, 2, `前置:tabs 应为 2,实为 ${before.tabs}`);
        const markersBefore = await readMarkers(page);
        assert.strictEqual(markersBefore[A], `${A}-alive`, '前置:A pane 的标记没种上');
        assert.strictEqual(markersBefore[B], `${B}-alive`, '前置:B pane 的标记没种上');

        // 🔴 先断言入口在,再点击:否则元素缺失会以"click 超时"的形式红,
        //    那是**超时红不是断言红**,读日志的人分不清"入口没发"还是"点击没开新页"。
        const pre = await platformEntry(page, ENTRY_ID);
        assert.ok(pre, `前置坏了:#platform-items 里没有 data-item-id="${ENTRY_ID}" 的项,本格无从点击`);
        // 🔴 waitForEvent 必须**立刻挂 catch**:浮动的 promise 在 click 还没返回时就超时的话,
        //    会变成 unhandled rejection 把整个进程带走(改前第一跑就崩在这里)。
        await page.click(`#platform-items a[data-item-id="${ENTRY_ID}"]`, { timeout: 8000 });
        await page.waitForTimeout(400);
        // 🔴 先把现场**全部**量完,再一次性断言:逐条 assert 会在第一条就抛出,
        //    红了只看得见"没开新页",看不见"pane 全丢、SSE 全断"——而那才是业主要赔的东西。
        const after = await snapshot(page);
        const markersAfter = await readMarkers(page);
        const groupTab = await page.locator('#tabs .tab[title="Agent 编组"]').count();
        const workspace = await page.locator('#panes .group-workspace').count();
        const scene = `\n      点击前=${JSON.stringify(before)}\n      点击后=${JSON.stringify(after)}\n      标记/SSE=${JSON.stringify(markersAfter)}\n      编组tab=${groupTab} workspace=${workspace}`;
        const v = [];
        if (groupTab !== 1 || workspace !== 1) v.push('点击后没有在 console 内打开唯一 Agent 编组 tab');
        if (!after.consoleStillThere) v.push('console 顶层文档已不是控制台(#platform-items 没了)⇒ 整页被导航走,后退回不来');
        if (!after.url.startsWith(`${base}/console`)) v.push(`console 顶层 URL 变了:${after.url}`);
        if (after.panes !== 2) v.push(`pane 数 2→${after.panes}(已打开的 Agent 页全丢)`);
        if (after.tabs !== 3) v.push(`tab 数应 2→3,实为 ${after.tabs}`);
        if (JSON.stringify(after.paneTitles) !== JSON.stringify(before.paneTitles)) v.push(`pane 标题集变了:${JSON.stringify(after.paneTitles)}`);
        if (markersAfter[A] !== `${A}-alive`) v.push(`A pane 的 iframe 对象被重建/重载(标记=${markersAfter[A]})`);
        if (markersAfter[B] !== `${B}-alive`) v.push(`B(隐藏)pane 的对象没了(标记=${markersAfter[B]})`);
        if (markersAfter[`${A}_sse`] !== 1) v.push(`A pane 的 SSE 断了(readyState=${markersAfter[`${A}_sse`]},1=OPEN)`);
        if (markersAfter[`${B}_sse`] !== 1) v.push(`B pane 的 SSE 断了(readyState=${markersAfter[`${B}_sse`]})`);
        assert.strictEqual(v.length, 0,
          `点击入口后现场被破坏(${v.length} 条):\n      - ${v.join('\n      - ')}${scene}`);
      } finally { await context.close(); }
    });

    // ── G3 builtin "Agents" 已删除:服务端不得再下发 ───────────────────────
    currentPrincipal = 'manager';
    await t('G3 🔴 builtin「Agents」入口必须不存在', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page } = await openTwoPanes(context);
        const e = await platformEntry(page, 'agents');
        assert.strictEqual(e, null, `builtin Agents 不得重新下发:${JSON.stringify(e)}`);
      } finally { await context.close(); }
    });

    // ── G4 owner 侧:🔴 S-GROUP-ENTRY(2026-09-29)口径已翻 ──────────────────
    //    旧口径"owner 不得看到 agent-groups"(0.2.1.7 把它关在 management 里);
    //    新口径"owner **必须**看到"——「加入到其他业主编组」只在这个入口里面,
    //    而它的目标用户恰恰是默认 role=owner 的另一位业主。
    //    🔵 本格仍守两件旧事:builtin Agents 不得长回;admin 不得下发给 owner
    //       (ids 用 deepStrictEqual 钉**全集** ⇒ 多一项就红,这是派工判据② 的浏览器侧同形判据)。
    await t('G4 🔴 owner 左栏必须有 agent-groups 内部 tab,且仍不得出现 builtin Agents / admin', async () => {
      currentPrincipal = 'owner';
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      try {
        const { page } = await openTwoPanes(context);
        const role = await page.evaluate(() => document.documentElement.dataset.platformRole || '');
        assert.strictEqual(role, 'owner', `前置:这一腿必须以 owner 身份跑,实为 ${JSON.stringify(role)}`);
        const e = await platformEntry(page, ENTRY_ID);
        assert.ok(e, `🔴 owner 的 #platform-items 里没有 data-item-id="${ENTRY_ID}" 的项 ⇒ 入口又只对 management 发了。` +
          `「加入到其他业主编组」只能由这一项点击打开(openGroupWorkspace 仅一个调用点),而目标用户正是默认 role=owner 的另一位业主`);
        assert.strictEqual(e.count, 1, `入口必须唯一可寻址,实到 ${e.count} 个同 id 项`);
        assert.strictEqual(e.href, ENTRY_HREF, `href=${e.href},应为 ${ENTRY_HREF}(指向 /admin/agent-groups 是**方向相反**:旧 admin 独立鉴权域)`);
        assert.ok(!e.target, `内部 tab 不得带 target,实为 ${e.target}`);
        const builtin = await platformEntry(page, 'agents');
        assert.strictEqual(builtin, null, `owner 侧 builtin Agents 不得重新出现:${JSON.stringify(builtin)}`);
        const ids = await page.evaluate(() => Array.from(document.querySelectorAll('#platform-items a')).map((a) => a.dataset.itemId).sort());
        assert.deepStrictEqual(ids, ['agent-groups', 'specs', 'task-board'],
          `owner 的平台项**全集**应为 agent-groups/specs/task-board(🔴 多出 admin = 把管理面一刀切放出去了),实为 ${JSON.stringify(ids)}`);
      } finally { await context.close(); }
    });
  } finally {
    await browser.close();
    server.close();
  }

  console.log(`\n  合计 ${pass + fail} 条:✅ ${pass} · 🔴 ${fail}`);
  if (CONTROL) {
    // 🔴 阳性对照的判据不是"有红",是"**红的正好是该红的那几条**":
    //    少红 ⇒ 那一条断言对"恢复旧 admin popup"这个变异不敏感(假绿);
    //    多红 ⇒ 装置异常或产品另有回归,而它会**混在**"对照成功"里被读过去(假故障)。
    const want = ['G1', 'G2', 'G4'];
    const got = failedIDs.slice().sort();
    if (JSON.stringify(got) !== JSON.stringify(want)) {
      console.log(`  🔴 阳性对照的红格集合不对:期望 ${JSON.stringify(want)},实到 ${JSON.stringify(got)}`);
      console.log('     少红 = 缺的那条断言没有判别力;多红 = 先查装置/产品,不要读成"对照成立"');
      process.exit(1);
    }
    console.log(`  🔵 阳性对照成立:恢复旧 admin popup 后红的正好是 ${JSON.stringify(want)},G3 仍绿`);
    console.log('     (本跑按约定 rc=1 —— 对照跑本来就该红,判据是**红格集合**,不是 rc)');
  }
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.log('  🔴 装置异常:', e && e.stack ? e.stack.split('\n').slice(0, 4).join('\n') : e); process.exit(1); });
