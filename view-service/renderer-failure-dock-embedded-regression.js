// 🔴 错误看板「计数有、点开空白」—— 在【console 右栏 iframe 嵌入态】量几何,并补独立态同一维。
//
// 为什么必须单独立这一件(业主 2026-09-29 现场读数:角标 2、点开面板空白、里面只有一条很细的竖直红线):
//   既有 renderer-failure-dock-regression.js 断言的是 **计数与开关**
//   (querySelectorAll('#activityList > .activity-card').length / workspaceDock.hidden / unread),
//   🔴 **一个几何量都不量** ⇒ 面板被挤到 clientWidth=0 时它照样三格全绿。
//   这不是"独立态 vs 嵌入态"的差别:实测两条路上塌的形状**逐格相同**
//   (dock 365.7 / 把手 365.7 / 面板 2px,2px 正是那两条 1px 红边 ⇒ 业主看到的"竖直红线")。
//   ⇒ 真正的盲区是【只断言存在、不断言可见】。本件把判据换成几何。
//
// 装置沿用 renderer-in-console-iframe-regression.js 的手法(不复制 console 页字节):
//   console 页**从生产源码里取**(internal/m8/console_auth.go 的 consolePageHTML),
//   `/view/<session>` 返回**仓内真实的** renderer-canary-sse.html。
//   🔴 iframe 内 renderer 的 status/events 是相对路径 ⇒ 必须挂 /view/<session>/status,只挂 /status 会 404。
//
// 夹具**不在这里写**,读 view-service/fixtures-failure-dock/hub-failure-dock-event.json
//   (Hub 侧 publishFailureDockEvent 的逐字节产出,由 Go 侧 TestFailureDockEventMatchesRendererFixture 钉住)。
//   第二条只改 message_id 以便复现业主看到的角标 2 —— **形状仍是那一份**,不另造。
//
// ── 四腿 ────────────────────────────────────────────────────────────────────
//   腿一 装置自证   事件到了、卡进了 DOM、角标=2 —— 🔴 这一格【绿不代表看得见】,它就是本件存在的理由
//   腿二 主判据     嵌入态四档窗口宽度,每档量七维:面板 clientWidth 有可读宽度、
//                   卡片横向落在面板 client 盒内、卡片填满面板、卡片高度不病态、
//                   把手仍是竖条、dock 不溢出 iframe 视口、首卡中心那一点真的命中它自己
//   腿三 同判据     独立态再验一次(补既有那件的盲区;判据与腿二**同一个函数**,不抄第二份)
//   腿四 阳性对照   同进程把 `width: 100%` 塞回那条共用规则 ⇒ 腿二的判据**必须红**;
//                   🔴 塞不回去(锚串被正当改动挪走)就直接失败并报"对照素材被掏空",不静默跳过
//
// ── 已知边界:本闸只判【几何与命中】,不判【绘制层】(🔴 四腿全绿 ≠ 业主看得见)─────────────
//   裁定 ruling-2026-09-30T032159Z-lead-errordock-gate-boundary 发现①,处置选【甲】:只登记边界、不扩围。
//   在判的:面板 clientWidth、卡片是否落在面板 client 盒内、卡宽/卡高/文本长度、dock 是否溢出 iframe 视口、
//           首卡中心那一点是否命中它自己。**一个像素级(绘制层)判据都没有。**
//   ⇒ 下列形状本闸一律【放行】,绿了也不代表看得见:
//        opacity: 0 · color: transparent 或与背景同色 · font-size: 0 · 被同层元素盖住但中心点仍命中自己
//        (以及任何"几何量都正常、像素上不可见"的形状)
//   登记理由(照裁定原文):本缺陷是布局类(面板被挤到 clientWidth=0);计算样式判据是新维度,低风险只记录、不扩围。
//   🔴 引用纪律:谁要把本闸的绿当"渲染层可见"的证据,先补绘制层判据(计算样式或截图像素),别引这一格。
const assert = require('assert');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const RENDERER = path.join(__dirname, 'renderer-canary-sse.html');
const FIXTURE = path.join(__dirname, 'fixtures-failure-dock', 'hub-failure-dock-event.json');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const SESSION = 'owner-alpha-2642';
// 四档浏览器窗口 ⇒ iframe(=console 右栏 #panes)宽度逐档变窄。
// 🔵 console 的 nav 是固定 240px 列、无响应式规则,所以窗口 480 时右栏只剩 240px —— 那一档量的是
//    渲染器【自己】在窄承载面里收不收得住,不是替 console 找补。
const WINDOWS = [{ width: 1280, height: 900 }, { width: 900, height: 900 }, { width: 600, height: 900 }, { width: 480, height: 900 }];
const SHARED_RULE_ANCHOR = '.workspace-dock-toggle, .dock-section-title {';

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML 起始标记不见了 —— 生产源码结构变了,先修装置');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML 结束标记不见了 —— 生产源码结构变了,先修装置');
  return source.slice(contentStart, end);
}

// ── 几何判据(腿二/腿三/腿四 共用这一份,不抄第二份)───────────────────────────────
// 返回违规项数组;空数组＝看得见(🔴 "看得见"只到几何与命中这一层,绘制层不在判据内 —— 见文件头注「已知边界」)。
// 🔴 阈值来源【如实写】(裁定 ruling-2026-09-30T032159Z-lead-errordock-gate-boundary 发现④):
//    原注那句"所有阈值来自**设计意图**、不是从某一版修复后的输出倒推出来的",对【窄档】不成立 ⇒ 按裁定改掉。
//   · 宽档 want = 240px      设计意图:面板设计宽 320px,240px 是"还剩可读宽度"的下限,与视口无关
//   · 窄档 want = 视口−70    🔴 这一支是【贴着修复输出定的】,不是独立的设计意图:
//       修复公式 renderer-canary-sse.html:498   width: min(320px, calc(100vw - 64px))
//       面板左右各 1px 边框 ⇒ clientWidth = 视口−66;本闸要 ≥ 视口−70 ⇒ 只留 **4px** 余量
//       实测(修复字节 cd660768… / 锚 c944fc0e,本闸 rc=0 那一跑):
//         iframe 视口 240 ⇒ 面板 176 · clientWidth 174 · want 170 ⇒ 余 4px(WINDOWS 里唯一落在窄支的一档)
//         iframe 视口 360 ⇒ 面板 296 · clientWidth 294 · want 240 ⇒ 余 54px(走宽支,被 240 封顶)
//         iframe 视口 660 与 1040 ⇒ 面板 320 · clientWidth 318 · want 240 ⇒ 余 78px(走宽支)
//       窄支只在 iframe 视口 < 310px 时起作用(240 < 310 ≤ 360)
//   · 把手 ≤120px · 卡高 ≤240px · 卡宽 ≥ clientWidth−24 · 文本 ≥20 字   设计意图(把手是竖条、卡不该逐字换行)
//   ⇒ 🔴 这 4px 是【与修复公式的耦合】,不是【灵敏度不足】:公式里那个 64 只要被正当改动收紧到 ≥69,窄档就假红
//        (64→68 仍绿:判据是 clientWidth < want 才算违规)。
//        要不要把窄支改成与公式解耦(例如按视口比例取),属判据改动 ⇒ 另开一格并附自己的变异证明;
//        本单只改注释,判据一字未动(下面那行里的 240 与 −70 原样保留)。
function dockViolations(m, tag) {
  const v = [];
  const want = Math.min(240, Math.max(0, m.viewport[0] - 70));
  if (m.children !== 2) v.push(`${tag}: 面板里应恰好 2 张卡,实为 ${m.children}`);
  if (m.bodyClient[0] < want) {
    v.push(`${tag}: 🔴 面板【点开空白】—— workspaceDockBody.clientWidth=${m.bodyClient[0]}px(应 ≥${want}px)`
      + ` ⇒ 卡片在 DOM 里(${m.children} 张)但被挤到没有可读宽度;`
      + `把手宽 ${m.toggle.w}px、面板宽 ${m.body.w}px(那 2px 就是它的两条 1px 红边)`);
  }
  if (m.toggle.w > 120) {
    v.push(`${tag}: 🔴 竖直把手吃满了整个 dock —— toggle.width=${m.toggle.w}px(应 ≤120px,它是一条 writing-mode:vertical-rl 的竖条)`
      + ` ⇒ 共用规则里那条 width:100% 落到了把手上`);
  }
  m.cards.forEach((c, i) => {
    if (c.rect.w < m.bodyClient[0] - 24) v.push(`${tag}: 卡[${i}] 宽 ${c.rect.w}px 没填满面板(clientWidth=${m.bodyClient[0]}px)`);
    if (c.rect.h > 240) v.push(`${tag}: 卡[${i}] 高 ${c.rect.h}px 病态(宽被挤掉后逐字换行)⇒ 文本 ${c.textLen} 字`);
    if (c.textLen < 20) v.push(`${tag}: 卡[${i}] 文本只有 ${c.textLen} 字 —— DEFECT 件记的「只有数字、内容为空」就是这个形状`);
    // 🔴 横向必须落在面板的 client 盒里。塌成 0 宽时卡片被 overflow 裁在盒外 ⇒ 这一条会红。
    //    纵向只要求"顶边不低于 client 顶边":第二张卡滚到折叠线以下是正常滚动,不是缺陷。
    if (!c.insideClientBox) {
      v.push(`${tag}: 🔴 卡[${i}] 不在面板的 client 盒内 —— 卡 x=${c.rect.x} 宽 ${c.rect.w},`
        + `而 client 盒 x=${m.clientBox.x} 宽 ${m.clientBox.w} ⇒ 它被裁掉了,DOM 里有也看不见`);
    }
  });
  if (m.dock.x < -1 || m.dock.x + m.dock.w > m.viewport[0] + 1) {
    v.push(`${tag}: 🔴 dock 溢出 iframe 视口 —— dock.x=${m.dock.x} 宽 ${m.dock.w},而 iframe 视口只有 ${m.viewport[0]}px`
      + ` ⇒ 面板在承载面里放不下,业主看到的会是"点开在屏幕外"`);
  }
  // 🔵 首卡中心那一点上必须真的画着这张卡(命中它自己或它的子孙)。
  //    这条与上面那些几何量互补:几何对、却被别的层盖住时,只有它会红。
  if (!m.firstCardCenterHitsItself) {
    v.push(`${tag}: 首卡中心那一点命中的不是它自己(${JSON.stringify(m.firstCardCenterHit)}) ⇒ 那个位置上看不到这张卡`);
  }
  return v;
}

const MEASURE = () => {
  const b = (el) => { const r = el.getBoundingClientRect();
    return { x: +r.x.toFixed(1), y: +r.y.toFixed(1), w: +r.width.toFixed(1), h: +r.height.toFixed(1) }; };
  const dock = document.getElementById('workspaceDock');
  const toggle = document.getElementById('workspaceDockToggle');
  const body = document.getElementById('workspaceDockBody');
  const list = document.getElementById('activityList');
  const unread = document.getElementById('workspaceDockUnread');
  const br = body.getBoundingClientRect();
  const csBody = getComputedStyle(body);
  const clientBox = {
    x: +(br.x + (parseFloat(csBody.borderLeftWidth) || 0)).toFixed(1),
    y: +(br.y + (parseFloat(csBody.borderTopWidth) || 0)).toFixed(1),
    w: body.clientWidth,
    h: body.clientHeight,
  };
  const inside = (r) => r.x >= clientBox.x - 0.5 && r.x + r.w <= clientBox.x + clientBox.w + 0.5 && r.y >= clientBox.y - 0.5;
  const cards = Array.from(list.children).map((c) => { const r = b(c);
    return { rect: r, textLen: (c.textContent || '').trim().length, insideClientBox: inside(r) }; });
  const first = list.children[0];
  let firstCardCenterHit = null;
  let firstCardCenterHitsItself = false;
  if (first) {
    const r = first.getBoundingClientRect();
    const el = document.elementFromPoint(r.x + Math.max(1, r.width / 2), r.y + Math.max(1, Math.min(8, r.height / 2)));
    firstCardCenterHit = el ? (el.id || el.className || el.tagName) : null;
    firstCardCenterHitsItself = !!(el && (el === first || first.contains(el)));
  }
  return {
    viewport: [window.innerWidth, window.innerHeight],
    dockHidden: dock.hidden,
    dock: b(dock),
    toggle: b(toggle),
    ariaExpanded: toggle.getAttribute('aria-expanded'),
    bodyHidden: body.hidden,
    body: b(body),
    bodyClient: [body.clientWidth, body.clientHeight, body.scrollWidth, body.scrollHeight],
    clientBox,
    list: b(list),
    children: list.children.length,
    cards,
    firstCardCenterHit,
    firstCardCenterHitsItself,
    unread: unread.textContent,
    unreadHidden: unread.hidden,
  };
};

// 灌两条 Hub 夹具(第二条只改 message_id)、走业主的真实路径点把手展开,然后量。
async function feedAndExpand(scope, fixtureRaw, fixture) {
  await scope.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
  await scope.evaluate((raw) => handleIncomingEvent(JSON.parse(raw)), fixtureRaw);
  await scope.evaluate((ev) => handleIncomingEvent(ev), Object.assign({}, fixture, { message_id: 'failure-dock-golden-2' }));
  await scope.waitForTimeout(200);
  const collapsed = await scope.evaluate(MEASURE);
  await scope.click('#workspaceDockToggle');
  await scope.waitForTimeout(200);
  const expanded = await scope.evaluate(MEASURE);
  return { collapsed, expanded };
}

function startServer(rendererBytesProvider, consoleHTML) {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    if (u === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(consoleHTML); }
    if (u === '/renderer-canary-sse.html') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(rendererBytesProvider()); }
    if (u === '/api/console/agents') return json({ agents: [{ session: SESSION, online: true }] });
    if (u === '/api/console/platform') return json({ role: 'owner', actions: ['read', 'submit'], items: [] });
    if (u === `/view/${encodeURIComponent(SESSION)}`) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(rendererBytesProvider()); }
    // 🔴 相对路径:iframe 内的 renderer 请求的是 /view/<session>/status 与 /events
    if (u.endsWith('/status')) return json({ session: SESSION, state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' });
    if (u.endsWith('/events')) { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', 'Connection': 'keep-alive' }); res.write(': open\n\n'); return; }
    res.writeHead(404, { 'Content-Type': 'text/plain' }); res.end('not found');
  });
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve({ base: `http://127.0.0.1:${server.address().port}`, close: () => new Promise((r) => server.close(r)) }));
  });
}

(async () => {
  const consoleHTML = consoleHTMLFromProductionSource();
  const repoBytes = fs.readFileSync(RENDERER);
  const RENDERER_SHA = crypto.createHash('sha256').update(repoBytes).digest('hex');
  // 🔴 被检字节的身份必须在【所有格之前】打印:红跑也要能自证测的是哪一份字节。
  //    缺口由组长 2026-09-30 复跑时指出 —— 原先这一行写在 checks.finish() 之后,只要有格红就不打印
  //    ⇒ "哪一份红的"只能靠人记得刚换过文件。📌 与本板「状态列必须写取自哪个 tip」同一条。
  console.log(`被检字节 ${path.basename(RENDERER)} sha256=${RENDERER_SHA} ${repoBytes.length} 字节`);
  console.log('  🔴 换文件跑变体时以这一行为准核对;它与末行 sha 必须相同(不同即装置中途被换过)');
  const fixtureRaw = fs.readFileSync(FIXTURE, 'utf8').trim();
  const fixture = JSON.parse(fixtureRaw);
  let served = repoBytes;
  const server = await startServer(() => served, consoleHTML);
  const checks = regressionCollector();
  let browser;
  const seen = [];
  try {
    browser = await launchChromium();

    const openEmbedded = async (vp) => {
      const context = await browser.newContext({ viewport: vp });
      const page = await context.newPage();
      page.on('pageerror', (e) => console.log(`    [pageerror ${vp.width}]`, String(e.message).slice(0, 160)));
      await page.goto(`${server.base}/console`, { waitUntil: 'domcontentloaded' });
      await page.locator(`nav button[title="${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
      await page.click(`nav button[title="${SESSION}"]`);
      await page.locator(`#panes iframe[title="Agent view: ${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
      const frame = page.frames().find((f) => f.url().includes(`/view/${encodeURIComponent(SESSION)}`));
      assert.ok(frame, '没找到 /view/<session> 那个 iframe 的 frame 对象 —— 装置坏了,先修装置');
      await frame.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
      const panes = await page.evaluate(() => { const r = document.getElementById('panes').getBoundingClientRect(); return { w: +r.width.toFixed(1), h: +r.height.toFixed(1) }; });
      return { context, page, frame, panes };
    };

    // ── 腿一 · 装置自证(🔴 绿不代表看得见)───────────────────────────────────
    let first = null;
    await checks.check('腿一 装置自证:嵌入态下事件到了、卡进了 DOM、角标=2(这一格【不】判可见性)', async () => {
      const { context, frame, panes } = await openEmbedded(WINDOWS[0]);
      try {
        const r = await feedAndExpand(frame, fixtureRaw, fixture);
        first = r;
        const c = r.collapsed;
        assert.strictEqual(c.dockHidden, false, '出现错误后看板入口必须出现(T-054)');
        assert.strictEqual(c.unread, '2', `收起时角标应为 2(业主现场读数),实为 ${JSON.stringify(c.unread)}`);
        assert.strictEqual(c.children, 2, `卡片应已进 DOM,实为 ${c.children} 张`);
        assert.strictEqual(r.expanded.ariaExpanded, 'true', '点把手后 aria-expanded 应为 true');
        assert.strictEqual(r.expanded.bodyHidden, false, '点把手后面板不应再 hidden');
        assert.strictEqual(r.expanded.unread, '0', `展开应清零角标(setDockExpanded),实为 ${JSON.stringify(r.expanded.unread)}`);
        console.log(`    · 腿一读数:panes ${panes.w}x${panes.h} · 卡片 ${c.children} 张 · 角标 ${c.unread}→${r.expanded.unread}`);
        console.log('    · 🔴 以上全部是"存在性"判据:既有那件闸断言的就是这一层,而它对本缺陷【完全无感】');
      } finally { await context.close(); }
    });

    // ── 腿二 · 🔴 主判据:嵌入态几何,四档窗口宽度各验一次 ──────────────────────
    await checks.check('腿二 🔴 嵌入态:点开之后【真的看得见】—— 四档窗口宽度各验一次', async () => {
      const all = [];
      for (const vp of WINDOWS) {
        const { context, frame, panes } = await openEmbedded(vp);
        try {
          const r = await feedAndExpand(frame, fixtureRaw, fixture);
          const m = r.expanded;
          const tag = `窗口${vp.width}(右栏 ${panes.w}px / iframe 视口 ${m.viewport[0]}px)`;
          seen.push({ tag, toggle: m.toggle.w, body: m.body.w, bodyClientWidth: m.bodyClient[0], cards: m.cards.map((c) => `${c.rect.w}x${c.rect.h}`) });
          all.push(...dockViolations(m, tag));
        } finally { await context.close(); }
      }
      console.log('    · 腿二实测:', JSON.stringify(seen, null, 1));
      assert.deepStrictEqual(all, [], `🔴 错误看板在【嵌入态】下点开看不见(共 ${all.length} 项):\n       - ${all.join('\n       - ')}`);
    });

    // ── 腿三 · 同一把判据、另一个承载面:独立态 ───────────────────────────────
    await checks.check('腿三 独立态同一把几何判据(补 renderer-failure-dock-regression.js 的盲区)', async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      const page = await context.newPage();
      try {
        await page.route('**/status', (r) => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: SESSION, state: 'idle', readonly: false, can_write: true, owner: 'fixture', model: 'm', cost_total: 0, cost_last: 0, turns: 0 }) }));
        await page.route('**/events', (r) => r.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
        await page.goto(`${server.base}/renderer-canary-sse.html?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
        const r = await feedAndExpand(page, fixtureRaw, fixture);
        const m = r.expanded;
        console.log(`    · 腿三实测:独立态 把手 ${m.toggle.w}px · 面板 ${m.body.w}px(clientWidth=${m.bodyClient[0]}) · 卡 ${m.cards.map((c) => `${c.rect.w}x${c.rect.h}`).join(',')}`);
        assert.deepStrictEqual(dockViolations(m, '独立态1280'), [],
          `🔴 独立态下错误看板同样点开看不见:\n       - ${dockViolations(m, '独立态1280').join('\n       - ')}`
          + '\n       📌 这说明本缺陷【不是嵌入态特有】:既有那件闸放行它,是因为它一个几何量都不量');
      } finally { await context.close(); }
    });

    // ── 腿四 · 🔴 阳性对照:把 width:100% 塞回共用规则 ⇒ 腿二的判据必须红 ─────────
    await checks.check('腿四 🔴 阳性对照:变异体(共用规则里塞回 width:100%)必须被腿二的判据判红', async () => {
      const text = repoBytes.toString('utf8');
      const at = text.indexOf(SHARED_RULE_ANCHOR);
      assert.notStrictEqual(at, -1,
        `🔴 对照素材被掏空:找不到锚串 ${JSON.stringify(SHARED_RULE_ANCHOR)} ⇒ 本闸的阳性对照已失效,`
        + '而"绿"会读成"面板可见"。请改锚串,不要删掉这一腿');
      assert.strictEqual(text.indexOf(SHARED_RULE_ANCHOR, at + 1), -1, '锚串出现多次 ⇒ 变异点不唯一,先修对照');
      const mutated = `${text.slice(0, at + SHARED_RULE_ANCHOR.length)}\n      width: 100%;${text.slice(at + SHARED_RULE_ANCHOR.length)}`;
      assert.notStrictEqual(mutated, text, '变异没有改变字节 ⇒ 对照无意义');
      const mutatedSha = crypto.createHash('sha256').update(mutated).digest('hex');
      console.log(`     (变异副本 sha256=${mutatedSha} ${Buffer.byteLength(mutated)} 字节,`
        + `由被检字节 ${RENDERER_SHA.slice(0, 16)}… 在本进程内合成、不落盘)`);
      served = Buffer.from(mutated, 'utf8');
      try {
        const { context, frame, panes } = await openEmbedded(WINDOWS[0]);
        try {
          const r = await feedAndExpand(frame, fixtureRaw, fixture);
          const m = r.expanded;
          const v = dockViolations(m, '变异体');
          console.log(`    · 腿四实测:变异体 把手 ${m.toggle.w}px · 面板 ${m.body.w}px(clientWidth=${m.bodyClient[0]}) · 首卡命中自己=${m.firstCardCenterHitsItself}`);
          assert.ok(v.length > 0,
            '🔴 阳性对照不成立:把 width:100% 塞回共用规则之后,腿二那把判据【仍然判它合格】'
            + ` ⇒ 判据看不见本缺陷,腿二/腿三的绿一律不采信(实测 把手 ${m.toggle.w}px 面板 clientWidth=${m.bodyClient[0]})`);
          assert.ok(v.some((s) => s.includes('clientWidth')), `变异体应触发"面板 clientWidth 塌掉"那一条,实触发的违规是:\n       - ${v.join('\n       - ')}`);
          assert.ok(m.children === 2, `🔴 变异体里卡片应【仍在 DOM】(children=${m.children}) —— 否则量到的不是本缺陷,而是"卡没进去"`);
        } finally { await context.close(); }
      } finally { served = repoBytes; }
      console.log('    · 阳性对照成立:同一份字节只塞回一条 width:100% 就被判红 ⇒ 判据咬得住本缺陷');
    });

    checks.finish();
    console.log('failure-dock-embedded regression: ALL PASS (4/4)');
    console.log(`  · renderer sha256=${RENDERER_SHA.slice(0, 16)}… ${repoBytes.length} 字节(与首行同一份)`);
  } finally {
    if (browser) await browser.close();
    await server.close();
  }
})().catch(finishRegression);
