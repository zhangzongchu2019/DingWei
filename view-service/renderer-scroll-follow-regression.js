// 🔴 滚动跟随权的**端到端守闸**(板件 ㉒;组长 ts-No.24 ④「建闸钉住 A1」)
//
// 钉的是这条不变量:
//   **用户用真手势上翻(交出跟随权)之后,一次"出窗重放"不得把他拽回底部。**
//
// ── 为什么必须有这一件(㉒ 的实测结论,八臂实验,锚 `fd8c75fc`)─────────────────────
//   板件原先把 ㉒/D1 记成"真缺陷、纯伤害",依据是 X7 那格:「首条重放 1.3s ⇒ 清空被取消、
//   `resetConversation` **0 次**,而用户仍被从 6008 拽到 7034」。
//   我把它拆成**只差一个变量**的两臂复跑,结论是 🔴 **X7 那条腿证伪**:
//
//     A1 真 wheel 手势 ×3   reset 0 次 · st 0 → **0** · atBottom=true **0/78** · 拽底 **0 次**
//     A2 仅程序化 scrollTop reset 0 次 · st 96 → **6001** · atBottom=true **78/78** · 拽底 **122 次**
//
//   两臂 `firstDelay`(1302/1300)、conn#2 事件数(158/158)、`rendered 74→230` 全都一样,
//   **只差"上翻有没有真手势"这一个变量**。产品里的机制是
//   `renderer-canary-sse.html:1692  else if (byUser && gap > FOLLOW_RELEASE_GAP_PX) timelineAtBottom = false;`
//   ⇒ 🔴 **无手势代理永远交还不了跟随权**,于是"被拽"是**探针的产物**,不是产品的缺陷。
//   而"无手势代理"正是 ㉒ 那条长期红的成因(`fd8c75fc` 已治),**同族第三次**。
//
//   ⇒ 所以本件的形状是:**A1 硬断言(现在好的,但全仓没人守)＋ A2 阳性对照(必须仍被拽)**。
//      🔴 没有 A2,A1 的绿没有意义 —— 装置瞎了也会绿。
//
// ── A3:唯一在真手势下可复现的拽底,组长裁定**不修**(ts-No.24 ③)────────────────────
//   真手势上翻在先 → 真断链 → 重连(ring 非空、首条即时)⇒ `resetConversation` 1 次,
//   内容被清空重建、位置本来就不可恢复,重建后停在底部。首个拽底调用栈逐字:
//     `at resetConversation (:1745) ← eventSource.onmessage (:3892)`
//   ⇒ 本件把 A3 做成**显影腿**:不断言"该不该",只断言**形态还是不是那个形态**;
//      形态变了(有人真修了、或换了机制)⇒ 红,并按 ts-No.24 ③ 报组长。
//      🔵 「重连重建后应停在哪」是**产品问题,已交业主**,不是本闸能裁的。
//
// ── 四条不变量腿 C0/C1/C2/C3 ──────────────────────────────────────────────────────
//   任何将来的修法都必须让它们保持绿(实测:v2/v3 形状下四条都未回归):
//     C0 初次加载不上翻 ⇒ 停在底部(🔴 这是**业主 09-11 定的行为**,不是我们的偏好)
//     C1 初次加载的重放窗**内**真手势上翻 ⇒ 逃生口生效,留在上面
//     C2 重连时用户本来就在底部(未上翻)⇒ 仍跟随到底
//     C3 上翻后又用真手势滚回底部 ⇒ 重连仍跟随到底(防"接管标志粘滞"把人漏掉)
//
// 装置:本件**自带** HTTP+SSE 夹具(自启自收、自挑空闲端口、路径一律走 __dirname,cwd 无关)。
//   按 `internal/m8/terminal.go:645` HandleInternalViewEvents「先回放 eventRing,再挂实时流」的
//   语义模拟 hub;全仓无 `Last-Event-ID` ⇒ "清空 + 由重放重建"是唯一的幂等机制。
//   🔴 iframe 腿的 console 外壳**从生产源码里实时抠**(`internal/m8/console_auth.go` 的
//      `consolePageHTML`),不复制一份 —— 否则验的不是业主看到的那一页。
//      业主走的就是 console 那条(`console_auth.go:306` 建 iframe,src='/view/'+session),
//      独立 renderer 页他从不走 ⇒ **在用户不走的那条路上验收**是本组反复撞的形状。
//
// 🔴 可被 tools/run-browser-regressions.py 直接加进 SUITES:
//   · 调用方可控的 env 面只有已登记的两个:`PW_CHANNEL` / `RENDERER_OVERRIDE`(不开新面)
//   · 其余参数一律显式写在腿定义里
//   · rc 与发布约定一致:**0 全过 / 2 断言失败 / 3 环境不可用**
//
// 变异(证明本件有牙;每发都用 RENDERER_OVERRIDE 喂副本,**产品件不动**):
//   M-D1 拆掉 `:1692` 的交还(byUser 不再清 timelineAtBottom)⇒ 🔴 A1/iframe-A1 必须红
//   M-D2 施加"(i) 只改 `:2309`"(组长 ts-No.21 ① 裁的那个修法)⇒ 🔵 实测**全绿**,
//        而"绿"正是它**是空操作**的证据:若它红了,(i) 就不是空操作。
//        🔴 这与组长 ts-No.24 ④「M-D2 必须红」的判据冲突 ⇒ 已请裁,见交付件。
//   M-D3 施加 v2(合取真修:`:1745` 守卫 ＋ 四点铺开)⇒ 🔴 A3 显影腿必须红(形态变了),
//        而 A1/A2 仍绿 ⇒ 证明显影腿看得见东西、且不是"任何改动都红"。
//   🔵 **L9「(i) 空操作对照」**(组长裁定:撤销「M-D2 必须红」,但**采纳**这条对照腿)——
//        M-D2 全绿只是"没观测到差别",🔴 而"没观测到差别 ⇒ 空操作"是**把结论当判据用**。
//        L9 把这句话变成一条**会自己报警的断言**:同进程内跑基线字节与 (i) 字节,
//        **逐项比对已登记观测量**;全同 ⇒ "(i) 是空操作"成立;
//        🔴 任一项不同 ⇒ (i) **非**空操作 ⇒ 「M-D2 必须红」**自动恢复生效**(阴格)。
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const HERE = __dirname;
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const OVERRIDE = !!process.env.RENDERER_OVERRIDE;
const GO_SOURCE = path.join(HERE, '..', 'internal', 'm8', 'console_auth.go');
const CONSOLE_START = 'const consolePageHTML = `';
const CONSOLE_END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const SESSION = 'owner-alpha-2642';
const VP = { width: 1280, height: 900 };

const RENDERER_BYTES = fs.readFileSync(RENDERER);
const RENDERER_SHA = crypto.createHash('sha256').update(RENDERER_BYTES).digest('hex');

// ── 夹具内容:长正文(>4000 折叠阈值)+ ring 若干回合,让"上翻"有地方可翻 ──────────
const UNIT = '契约正文压测段落';
const LONG = UNIT.repeat(120);
const CHUNK = 160;
const RING_TURNS = 8;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const now = () => Date.now();
const chunks = (t, n) => Array.from({ length: Math.ceil(t.length / n) }, (_, i) => t.slice(i * n, i * n + n));

function ringEvents() {
  const out = [{ kind: 'state_change', data: { change: 'session_init', session_id: 's1', model: 'm' }, delivery_id: '', turn_id: '', ts: now() }];
  for (let h = 0; h < RING_TURNS; h += 1) {
    out.push({ kind: 'user_input', text: `历史问题 ${h}`, delivery_id: `h-${h}`, turn_id: `ht-${h}`, stage: 'cli_injected', ts: now() });
    for (const c of chunks(LONG, CHUNK)) out.push({ kind: 'assistant_text', text: c, delivery_id: `h-${h}`, turn_id: `ht-${h}`, ts: now() });
    out.push({ kind: 'state_change', data: { change: 'turn_completed' }, delivery_id: `h-${h}`, turn_id: `ht-${h}`, ts: now() });
  }
  return out;
}

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(CONSOLE_START);
  assert.notStrictEqual(start, -1, 'consolePageHTML 起始标记不见了 —— 生产源码结构变了,先修装置');
  const contentStart = start + CONSOLE_START.length;
  const end = source.indexOf(CONSOLE_END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML 结束标记不见了 —— 生产源码结构变了,先修装置');
  return source.slice(contentStart, end);
}

// ── 自带夹具:一套路由同时喂"独立 renderer 页"与"真 console 的真 iframe" ───────────
async function startServer(opt) {
  const st = { conns: 0, evConns: [] };
  const consoleHTML = opt.console ? consoleHTMLFromProductionSource() : null;
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    // 🔵 L9 用 opt.bytes 换一份字节来跑(基线 vs (i));其余各腿一律用仓内那份
    const html = () => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(opt.bytes || RENDERER_BYTES); };
    if (u === '/__hangup') {
      // 🔴 真断链:把所有还开着的 SSE 连接 shutdown+close(不是发个事件假装断)
      const snap = st.evConns.filter((c) => !c.res.writableEnded);
      snap.forEach((c) => { try { c.res.end(); } catch {} });
      return json({ closed: snap.map((c) => c.rec.idx) });
    }
    if (u === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(consoleHTML);
    }
    if (u === '/api/console/agents') return json({ agents: [agentRecord(SESSION)] });
    if (u === '/api/console/platform') return json({ role: 'owner', actions: ['read', 'submit'], items: [] });
    if (u === '/tasks' || u === '/alerts') return json(u === '/alerts' ? [] : {});
    if (u.endsWith('/status')) return json({ session: SESSION, state: 'running', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
    if (u === `/view/${encodeURIComponent(SESSION)}`) return html();
    if (!u.endsWith('/events')) return html();
    st.conns += 1;
    const idx = st.conns;
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
    res.flushHeaders();
    const rec = { idx, t0: Date.now(), emitted: 0, firstAt: null, closedAt: null };
    st.evConns.push({ res, rec });
    let cur = 0;
    const emit = (ev) => {
      cur += 1;
      ev = Object.assign({ cursor: String(cur).padStart(6, '0') }, ev);
      try {
        res.write('data: ' + JSON.stringify(ev) + '\n\n');
        rec.emitted += 1;
        if (rec.firstAt === null) rec.firstAt = Date.now() - rec.t0;
      } catch {}
    };
    // 🔴 alive() 只看**本条连接**:用全局 hangup 标志会让 conn#2 一条都发不出(实测踩过,emitted=0)
    const alive = () => !res.writableEnded && !res.destroyed;
    (async () => {
      try {
        if (idx === 1) {
          for (const e of ringEvents()) { if (!alive()) return; emit(e); }
          emit({ kind: 'user_input', text: '请输出契约全文', delivery_id: 'd-1', turn_id: 't-1', stage: 'cli_injected', ts: now() });
          if (opt.longLive1) {
            // C1 需要:初次加载的**重放窗内**真有 live 事件在灌,否则"留在上面"是空的(没刺激)
            emit({ kind: 'user_input', text: '持续输出(活事件)', delivery_id: 'd-l1', turn_id: 't-l1', stage: 'cli_injected', ts: now() });
            for (let i = 0; i < 200; i += 1) {
              if (!alive()) return;
              emit({ kind: 'assistant_text', text: `[live1 ${String(i).padStart(3, '0')}] ${UNIT.repeat(6)}\n`, delivery_id: 'd-l1', turn_id: 't-l1', ts: now() });
              await sleep(30);
            }
          }
          for (const c of chunks(LONG, CHUNK)) {
            if (!alive()) return;
            emit({ kind: 'assistant_text', text: c, delivery_id: 'd-1', turn_id: 't-1', ts: now() });
            await sleep(25);
          }
          emit({ kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.5 }, delivery_id: 'd-1', turn_id: 't-1', ts: now() });
          emit({ kind: 'state_change', data: { change: '__done__' }, delivery_id: '', turn_id: '' });
          while (alive()) await sleep(50);
          return;
        }
        // conn#2:重连。firstReplayDelayMs = open 之后、首条重放事件之前的静默(ms)
        //   🔴 这是本件唯一的"时间旋钮":>REPLAY_HARD_LIMIT_MS(1000) ⇒ 清空窗口过期 ⇒ 清空被取消
        if (opt.firstReplayDelayMs) await sleep(opt.firstReplayDelayMs);
        if (!alive()) return;
        if (!opt.emptyRing2) for (const e of ringEvents()) { if (!alive()) return; emit(e); }
        emit({ kind: 'state_change', data: { change: 'session_init', session_id: 's1', model: 'm' }, delivery_id: '', turn_id: '', ts: now() });
        emit({ kind: 'user_input', text: '后续追问(活事件)', delivery_id: 'd-live', turn_id: 't-live', stage: 'cli_injected', ts: now() });
        for (let i = 0; i < 200; i += 1) {
          if (!alive()) return;
          emit({ kind: 'assistant_text', text: `[live ${String(i).padStart(3, '0')}] ${UNIT.repeat(6)}\n`, delivery_id: 'd-live', turn_id: 't-live', ts: now() });
          await sleep(30);
        }
        emit({ kind: 'state_change', data: { change: 'turn_completed' }, delivery_id: 'd-live', turn_id: 't-live', ts: now() });
        while (alive()) await sleep(50);
      } catch {}
    })();
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const port = server.address().port;
  return {
    url: `http://127.0.0.1:${port}/`,
    port,
    st,
    close: async () => {
      st.evConns.forEach((c) => { try { c.res.end(); } catch {} });
      await new Promise((r) => server.close(r));
    },
  };
}

// ── 仪表:包住 resetConversation / scrollTimelineToBottom,并 16ms 采样 ──────────────
// 🔴 只数"有没有被拽"不够:必须能说出**是谁拽的**(调用栈),否则 A3 的显影腿没有内容。
const INSTRUMENT = () => {
  if (window.__rec) return true;
  if (typeof window.resetConversation !== 'function') return false;
  const tl = document.getElementById('timeline');
  if (!tl) return false;
  const rec = { t0: Date.now(), samples: [], resets: [], snaps: [], alerts: [] };
  window.__rec = rec;
  const T = () => Date.now() - rec.t0;
  const oReset = window.resetConversation;
  window.resetConversation = function (...a) { rec.resets.push(T()); return oReset.apply(this, a); };
  const oSnap = window.scrollTimelineToBottom;
  window.scrollTimelineToBottom = function (...a) {
    const stack = (new Error().stack || '').split('\n').slice(2, 5)
      .map((s) => s.trim().replace(/http:\/\/127\.0\.0\.1:\d+\//, ':')).join(' <- ');
    rec.snaps.push({ t: T(), st: tl.scrollTop, sh: tl.scrollHeight, atBottom: timelineAtBottom, replay: replayInProgress, stack });
    return oSnap.apply(this, a);
  };
  if (typeof window.showStreamAlert === 'function') {
    const oAlert = window.showStreamAlert;
    window.showStreamAlert = function (...a) { rec.alerts.push({ t: T(), arg: a[0] }); return oAlert.apply(this, a); };
  }
  let n = 0;
  rec.timer = setInterval(() => {
    n += 1;
    const s = { t: T(), st: tl.scrollTop, sh: tl.scrollHeight, ch: tl.clientHeight, atBottom: timelineAtBottom, replay: replayInProgress, rendered: rendered };
    if (n % 4 === 1) rec.samples.push(s);   // 降采样存证,足够看清轨迹
  }, 16);
  return true;
};

const GEO = () => {
  const tl = document.getElementById('timeline');
  return { st: tl.scrollTop, sh: tl.scrollHeight, ch: tl.clientHeight,
    gap: tl.scrollHeight - tl.clientHeight - tl.scrollTop,
    atBottom: timelineAtBottom, replay: replayInProgress,
    kids: tl.children.length, rendered: rendered };
};

// ctx = frame 或 page(两者都有 evaluate/waitForFunction/locator);鼠标一律走 page
async function instrument(ctx) {
  for (let i = 0; i < 400; i += 1) {
    const ok = await ctx.evaluate(INSTRUMENT).catch(() => false);
    if (ok) return;
    await sleep(20);
  }
  throw Object.assign(new Error('instrument 失败(页面里没有 resetConversation/#timeline)'), { env: true });
}
const geo = (ctx) => ctx.evaluate(GEO);
const mark = (ctx) => ctx.evaluate(() => Date.now() - window.__rec.t0);
async function waitUntil(ctx, fn, ms, label) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) {
    if (await ctx.evaluate(fn).catch(() => false)) return Date.now() - t0;
    await sleep(25);
  }
  throw Object.assign(new Error(`waitFor 超时: ${label}`), { env: true });
}
// 🔴 真手势:CDP 的 mouse.wheel,不是程序化置 scrollTop。
//    这一条是本件存在的理由 —— 差别见文件头 A1/A2 那张表。
async function realWheelUp(page, ctx, dy, times) {
  const box = await ctx.locator('#timeline').boundingBox();
  assert.ok(box, '拿不到 #timeline 的 boundingBox ⇒ 无法施加真手势');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let i = 0; i < times; i += 1) { await page.mouse.wheel(0, dy); await sleep(40); }
  await sleep(150);   // mouse.wheel 是异步的(实测晚约 20ms),不等会抢在手势生效前
}

async function runLeg(browser, leg) {
  const srv = await startServer(leg);
  const out = { name: leg.name, opt: leg, marks: {} };
  const context = await browser.newContext({ viewport: leg.viewport || VP });
  let page = null;
  try {
    page = await context.newPage();
    const pageErrors = [];
    page.on('pageerror', (e) => pageErrors.push(String(e.message).slice(0, 160)));
    let ctx = page;
    if (leg.console) {
      // 业主那条路:真 console → 点在线 Agent → 真 iframe 内跑仓内那一份 renderer
      await page.goto(`${srv.url}console`, { waitUntil: 'domcontentloaded' });
      await page.locator(`nav button[title="${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
      await page.click(`nav button[title="${SESSION}"]`);
      await page.locator(`#panes iframe[title="Agent view: ${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
      const frame = page.frames().find((f) => f.url().includes(`/view/${encodeURIComponent(SESSION)}`));
      assert.ok(frame, '没找到 /view/<session> 那个 iframe 的 frame 对象');
      await frame.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
      ctx = frame;
      out.iframe = { url: frame.url(), viewport: await frame.evaluate(() => [window.innerWidth, window.innerHeight]) };
    } else {
      await page.goto(srv.url, { waitUntil: 'domcontentloaded' });
    }
    await instrument(ctx);
    out.servedSha = RENDERER_SHA.slice(0, 16);

    if (leg.scrollDuringReplay) {
      // C1:初次加载的**重放窗内**就真手势上翻(验 :1698 那条逃生口)
      await waitUntil(ctx, () => window.__rec && document.getElementById('timeline').children.length > 0, 25000, 'first kids');
      out.duringReplay = await geo(ctx);
      await realWheelUp(page, ctx, -1400, 2);
      out.ref = await geo(ctx);
      out.marks.scroll = await mark(ctx);
      await waitUntil(ctx, () => replayInProgress === false, 25000, 'replay closed');
      await sleep(200);
      out.marks.ref = await mark(ctx);
    } else {
      await waitUntil(ctx, () => window.__rec && replayInProgress === false && document.getElementById('timeline').children.length > 0, 25000, 'conn1 built');
      await sleep(400);
      out.built = await geo(ctx);
      if (leg.scrollMode === 'wheel') await realWheelUp(page, ctx, -1400, leg.wheelTimes || 3);
      else if (leg.scrollMode === 'wheelUpThenBottom') {
        await realWheelUp(page, ctx, -1400, 3);
        out.afterUp = await geo(ctx);
        for (let i = 0; i < 12; i += 1) {
          const g = await geo(ctx);
          if (g.gap <= 1) break;
          const box = await ctx.locator('#timeline').boundingBox();
          await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
          await page.mouse.wheel(0, Math.max(600, g.gap));
          await sleep(80);
        }
        await sleep(200);
      } else if (leg.scrollMode === 'programmatic') {
        // 🔴 A2 阳性对照臂:**故意**不用真手势 —— 它就是"过期代理"的形状
        await ctx.evaluate(() => {
          const tl = document.getElementById('timeline');
          const p = tl.style.scrollBehavior;
          tl.style.scrollBehavior = 'auto';
          tl.scrollTop = Math.max(0, tl.scrollHeight - tl.clientHeight - 3000);
          tl.style.scrollBehavior = p;
        });
        await sleep(150);
      }
      out.ref = await geo(ctx);
      out.marks.scroll = await mark(ctx);
      if (leg.noHangup) out.marks.hangup = null;
      else {
        await page.evaluate((u) => fetch(u, { method: 'POST' }).catch(() => {}), `${srv.url}__hangup`);
        out.marks.hangup = await mark(ctx);
      }
      out.marks.ref = out.marks.scroll;
    }

    await sleep(leg.observeMs || 3000);
    out.end = await geo(ctx);
    out.marks.end = await mark(ctx);
    out.pageErrors = pageErrors;
    const rec = await ctx.evaluate(() => ({ resets: window.__rec.resets, alerts: window.__rec.alerts, snaps: window.__rec.snaps, samples: window.__rec.samples }));
    out.resets = rec.resets;
    out.alerts = rec.alerts;
    out.snapsAfter = rec.snaps.filter((s) => s.t >= out.marks.ref);
    const win = rec.samples.filter((s) => s.t >= out.marks.ref);
    out.window = {
      n: win.length,
      renderedStart: win.length ? win[0].rendered : null,
      renderedEnd: win.length ? win[win.length - 1].rendered : null,
      stMin: win.length ? Math.min(...win.map((s) => s.st)) : null,
      stMax: win.length ? Math.max(...win.map((s) => s.st)) : null,
      atBottomTrueSamples: win.filter((s) => s.atBottom).length,
      replayTrueSamples: win.filter((s) => s.replay).length,
    };
    out.conns = srv.st.evConns.map((c) => ({ idx: c.rec.idx, emitted: c.rec.emitted, firstAt: c.rec.firstAt }));
    out.drag = out.end.st - out.ref.st;
  } catch (err) {
    out.error = String((err && err.message) || err);
    out.env = !!(err && err.env);
    out.conns = srv.st.evConns.map((c) => ({ idx: c.rec.idx, emitted: c.rec.emitted, firstAt: c.rec.firstAt }));
  } finally {
    if (page) await page.evaluate(() => clearInterval(window.__rec.timer)).catch(() => {});
    await context.close();
    await srv.close();
  }
  return out;
}

// 把一条腿的量测打成一行行证据(🔴 绿也要打印:判据必须看得见,不能只有 ✅)
function evidence(o) {
  if (o.error) return [`🔴 腿内异常: ${o.error}`, `   conns=${JSON.stringify(o.conns)}`];
  const w = o.window;
  return [
    `   参照 st=${o.ref.st} gap=${o.ref.gap} atBottom=${o.ref.atBottom} replay=${o.ref.replay} rendered=${o.ref.rendered}`,
    `   结束 st=${o.end.st} gap=${o.end.gap} atBottom=${o.end.atBottom} replay=${o.end.replay} rendered=${o.end.rendered}`,
    `   位移=${o.drag}px · 窗内 st∈[${w.stMin},${w.stMax}] · atBottom=true 采样 ${w.atBottomTrueSamples}/${w.n} · replay=true ${w.replayTrueSamples}/${w.n}`,
    `   刺激自证 rendered ${w.renderedStart} → ${w.renderedEnd} · conn2 emitted=${o.conns[1] ? o.conns[1].emitted : '-'} firstAt=${o.conns[1] ? o.conns[1].firstAt : '-'}`,
    `   resetConversation=${JSON.stringify(o.resets)} · 参照点后拽底 ${o.snapsAfter.length} 次`
      + (o.snapsAfter.length ? ` · 首栈 ${o.snapsAfter[0].stack}` : ''),
    o.alerts.length ? `   alerts=${JSON.stringify(o.alerts.slice(0, 3))}` : null,
    o.pageErrors && o.pageErrors.length ? `   🔴 pageErrors=${JSON.stringify(o.pageErrors.slice(0, 2))}` : null,
  ].filter(Boolean);
}

// ── (i) 变体:在本进程内**合成**,不依赖任何人的 /tmp ─────────────────────────────
// (i) = 组长 ts-No.21 ① 裁的那个修法:引入 `userTookOverScroll` ＋ **只**在 `:2309 shouldFollow` 加守卫。
// 🔴 三处锚点都必须**恰好命中一次**才写盘;命中数不对就报错,**不静默跳过** ——
//    跳过就等于把"(i) 是空操作"退回成一句没人验的论断,那正是 L9 要治的东西。
const VARIANT_I_STEPS = [
  ["    let timelineAtBottom = true;\n",
   "    let timelineAtBottom = true;\n    let userTookOverScroll = false;\n"],
  ["else if (byUser && gap > FOLLOW_RELEASE_GAP_PX) timelineAtBottom = false;",
   "else if (byUser && gap > FOLLOW_RELEASE_GAP_PX) { timelineAtBottom = false; if (streamEverOpened) userTookOverScroll = true; }"],
  ["      const shouldFollow = replayInProgress || timelineAtBottom;",
   "      const shouldFollow = (replayInProgress && !userTookOverScroll) || timelineAtBottom;"],
];
function buildVariantI() {
  let s = RENDERER_BYTES.toString('utf8');
  VARIANT_I_STEPS.forEach(([oldStr, newStr], i) => {
    const n = s.split(oldStr).length - 1;
    assert.strictEqual(n, 1,
      `🔴 (i) 对照腿的锚点过期:第 ${i + 1} 处命中 ${n} 次(应恰好 1 次)。`
      + `产品件动过了 ⇒ 请先更新 VARIANT_I_STEPS 的锚点,别把这条腿静默跳过。锚点前 60 字:${JSON.stringify(oldStr.slice(0, 60))}`);
    s = s.replace(oldStr, newStr);
  });
  const bytes = Buffer.from(s, 'utf8');
  return { bytes, sha256: crypto.createHash('sha256').update(bytes).digest('hex') };
}

// ── 已登记观测量(L9 逐项比对的就是这张表)────────────────────────────────────────
// 🔴 哪些量**能**逐项比、哪些**不能**,不是我拍的:是**三次基线实跑对账**量出来的
//    (同一份字节 `9b5656f4…` 连跑三轮,逐字段比):
//   ✅ 三轮逐字相同 ⇒ 进对照集:
//      位移 px(A1 0/0/0 · A2 5905/5905/5905 · A3 3284×3 · C2 185×3 · C3 188×3)
//      reset **次数** · 结束 atBottom 标志 · gap<=1 · 拽底"有没有"(零/非零)·
//      **首个拽底调用栈的函数名链** · atBottom 采样类(0/部分/全部)· 告警 arg 序列 ·
//      conn#2 有没有发事件 · 时间旋钮是否被尊重 · 刺激是否增长 · pageerror 数
//   🔴 实测有抖动 ⇒ **不进**对照集(否则对照腿会变成"狼来了"):
//      拽底**次数**(127/123/128、132/139/137,±7)· rendered(±2)· conn2 emitted(±1)·
//      firstAt(1302/1301、1/0)· 全部时间戳 · 窗内 st 极值(6994/6971)· 采样**分母**(78/79)·
//      🔴 **参照点/终点的绝对 st 与 gap**:C1 三轮实测 **117 / 117 / 1610**(差 1493px!)——
//         因为 C1 的手势施加在**重放进行中**,落点取决于那一刻已到多少内容 ⇒ 结构性抖动。
//         🔵 但 C1 的**位移**三轮都是 0 ⇒ 位移是"两端一起动"的量,比绝对位置稳。
//   🔴 **调用栈里的行号不是观测量,是源码坐标**:(i) 在 `:1429` 插入一行声明 ⇒ 其后所有行号 +1。
//      实测机械核对:基线 `:1605` ≡ (i) `:1606`、基线 `:1745` ≡ (i) `:1746`、基线 `:3892` ≡ (i) `:3893`,
//      **三处逐字相同**(列号也未变)⇒ 行号差是**位移假象**,不是行为差。
//      ⇒ 登记的是**函数名链**(与组长那条纪律同源:行号随树变,跨席给行号必须同时给锚点)。
//   🔵 L9 另有一道**运行时抖动对照**:同进程内把基线字节再跑一遍(**A1/A2/C1** 三腿 ——
//      C1 是实测抖过的那条,所以它在守卫里),若它与自己都不同 ⇒ 先报这个,**不许读 (i) 的结论**。
//      🔴 已知限制:这道守卫只覆盖 3 腿,不是 8 腿 ⇒ 它挡不住"只在其它五腿上抖"的量;
//         那五条靠上面那张**三轮对账表**兜底,不是靠运行时守卫。
// 🔴 只取**函数名链**,把 `:行:列` 全部丢掉 —— 行号是源码坐标,插一行就整体位移(见上)
const normStack = (st) => (st || '').split('<-').map((f) => {
  const m = f.trim().match(/^at\s+([^\s(]+)\s*\(/);
  return m ? m[1] : f.trim().replace(/:\d+:\d+/g, '').replace(/\s+/g, ' ');
}).join(' <- ').slice(0, 160);
function observables(o) {
  const cls = (n, total) => (n === 0 ? 'none' : (total && n === total ? 'all' : 'some'));
  return {
    dragPx: o.drag,
    endAtBottomFlag: o.end.atBottom,
    endAtBottomGap: o.end.gap <= 1,
    resetsCount: o.resets.length,
    snapsClass: o.snapsAfter.length === 0 ? 'zero' : 'some',
    firstSnapStack: o.snapsAfter.length ? normStack(o.snapsAfter[0].stack) : null,
    atBottomSamplesClass: cls(o.window.atBottomTrueSamples, o.window.n),
    alertArgs: (o.alerts || []).map((a) => a.arg),
    conn2HasEvents: o.conns[1] ? o.conns[1].emitted > 0 : null,
    firstDelayRespected: o.conns[1] && o.opt.firstReplayDelayMs ? o.conns[1].firstAt >= o.opt.firstReplayDelayMs - 200 : null,
    stimulusGrew: o.window.renderedEnd > o.window.renderedStart,
    pageErrorsCount: (o.pageErrors || []).length,
  };
}
const diffObservables = (a, b) => Object.keys(a)
  .filter((k) => JSON.stringify(a[k]) !== JSON.stringify(b[k]))
  .map((k) => `${k}: 基线=${JSON.stringify(a[k])} ↔ 对照=${JSON.stringify(b[k])}`);

let pass = 0;
let fail = 0;
const VECTORS = {};   // L1-L8 的基线观测量,供 L9 逐项比对
let envMissing = 0;
const failures = [];
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; }
  catch (e) {
    if (e && e.env) { console.log('  ⚠️  环境缺失:', name, '->', e.message); envMissing += 1; }
    else { console.log('  🔴', name, '->', e && e.message); fail += 1; failures.push(name); }
  }
}

// ── 腿定义 ────────────────────────────────────────────────────────────────────
// firstReplayDelayMs = 重连后首条重放事件之前的静默(ms)。1300 > REPLAY_HARD_LIMIT_MS(1000) ⇒ 出窗。
const LEG_A1 = { name: 'A1', scrollMode: 'wheel', wheelTimes: 3, firstReplayDelayMs: 1300, observeMs: 5000 };
const LEGS = [
  ['L1 🔴 A1 硬断言:真手势上翻 ＋ 出窗重放 ⇒ **不得被拽底**(全仓此前无人守这一条)', LEG_A1, 'a1'],
  ['L2 🔴 A2 阳性对照:同一条腿换成【无手势代理】⇒ **必须仍被拽**(否则 L1 的绿没有意义)',
    { name: 'A2', scrollMode: 'programmatic', firstReplayDelayMs: 1300, observeMs: 5000 }, 'a2'],
  ['L3 A3 显影腿:真手势在先 ＋ 首条即时 ＋ ring 非空 ⇒ 形态必须仍是"resetConversation 1 次后停在底部"',
    { name: 'A3', scrollMode: 'wheel', firstReplayDelayMs: 0, observeMs: 4000 }, 'a3'],
  ['L4 C0 业主不变量(09-11):初次加载、不上翻 ⇒ 必须停在底部',
    { name: 'C0', scrollMode: 'none', noHangup: true, observeMs: 2500 }, 'bottom'],
  ['L5 C1 初次加载的重放窗**内**真手势上翻 ⇒ 逃生口生效,留在上面',
    { name: 'C1', scrollMode: 'none', noHangup: true, scrollDuringReplay: true, longLive1: true, observeMs: 2500 }, 'up'],
  ['L6 C2 重连时用户本来就在底部(未上翻)⇒ 仍必须跟随到底',
    { name: 'C2', scrollMode: 'none', firstReplayDelayMs: 0, observeMs: 4000 }, 'bottom'],
  ['L7 C3 上翻后又用真手势滚回底部 ⇒ 重连仍必须跟随到底(防接管标志粘滞把人漏掉)',
    { name: 'C3', scrollMode: 'wheelUpThenBottom', firstReplayDelayMs: 0, observeMs: 4000 }, 'bottom'],
  ['L8 🔴 iframe-A1:同 L1,但在【真 console 的真 iframe】内跑 —— 业主走的就是这条路',
    Object.assign({}, LEG_A1, { name: 'A1-in-console-iframe', console: true, observeMs: 5000 }), 'a1'],
];

(async () => {
  console.log('renderer-scroll-follow-regression(㉒ · 组长 ts-No.24 ④)');
  console.log(`  renderer  ${RENDERER}`);
  console.log(`  sha256    ${RENDERER_SHA.slice(0, 16)}…${OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这是变异/对照跑,不是产品那份字节' : ''}`);
  if (!fs.existsSync(RENDERER)) { console.log('  ⚠️  环境缺失:被测 renderer 不存在'); process.exit(3); }

  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  let browser;
  try { browser = await chromium.launch(launchOptions); }
  catch (e) { console.log('  ⚠️  环境缺失:Playwright 起不来 ->', String(e.message).slice(0, 160)); process.exit(3); }

  try {
    for (const [title, leg, kind] of LEGS) {
      await t(title, async () => {
        const o = await runLeg(browser, leg);
        if (o.error) throw Object.assign(new Error(o.error), { env: o.env });
        evidence(o).forEach((l) => console.log(l));
        VECTORS[leg.name] = observables(o);
        // ── 共用守卫(每条腿都要):装置自证,不然是"我跑了"不是"我验了" ──
        assert.ok(o.conns.length >= 1, '守卫:夹具一条 SSE 连接都没收到 ⇒ 这一腿根本没跑起来');
        assert.ok(!o.pageErrors || o.pageErrors.length === 0,
          `守卫:页面有未捕获异常(整段脚本可能已停摆)⇒ 先修这个,别读下面的数:${JSON.stringify(o.pageErrors.slice(0, 2))}`);
        assert.ok(o.window.n > 10, `守卫:观测窗内只有 ${o.window.n} 个采样 ⇒ 采样器没跑,量不到东西`);
        if (kind !== 'bottom') {
          // 🔴 刺激自证:窗内 rendered 必须增长。否则"没被拽"可能只是**没有事件在灌**。
          assert.ok(o.window.renderedEnd > o.window.renderedStart,
            `守卫(刺激自证):观测窗内 rendered ${o.window.renderedStart} → ${o.window.renderedEnd} **没增长**`
            + ` ⇒ "没被拽"是空的,这一腿不算验过`);
        }
        if (leg.firstReplayDelayMs) {
          const c2 = o.conns[1];
          assert.ok(c2 && c2.emitted > 0, `守卫:重连那条连接没发出任何事件 ⇒ 出窗/重放没发生,实测 ${JSON.stringify(o.conns)}`);
          assert.ok(c2.firstAt >= leg.firstReplayDelayMs - 200,
            `守卫(时间旋钮自证):conn#2 首条 firstAt=${c2.firstAt}ms,应 >= ${leg.firstReplayDelayMs - 200}ms`
            + ` ⇒ 夹具的 firstDelay 没生效,这一腿会静默退化成"窗内"`);
        }

        if (kind === 'a1') {
          // 🔴 组长 ts-No.22 ② 点名的那条守卫:出窗 ⇒ 清空被取消 ⇒ resetConversation **必须 0 次**。
          //    若它 >0,说明这一腿走的不是 X7/A1 那个分支(而是 A3 那条重建路径),判据就不成立了。
          assert.strictEqual(o.resets.length, 0,
            `守卫(reset 必须 0 次):出窗重放下清空应被取消,实测 resetConversation 调用 ${JSON.stringify(o.resets)}`
            + ` ⇒ 这一腿走的不是 A1 分支,先查装置再判产品`);
          assert.strictEqual(o.window.atBottomTrueSamples, 0,
            `真手势上翻后 timelineAtBottom 必须**一直**为 false(跟随权已交还),实测 ${o.window.atBottomTrueSamples}/${o.window.n} 个采样为 true`);
          assert.strictEqual(o.snapsAfter.length, 0,
            `🔴 参照点后 scrollTimelineToBottom 被调用 ${o.snapsAfter.length} 次(首栈 ${o.snapsAfter[0] && o.snapsAfter[0].stack})`
            + ` ⇒ 用户已被拽。这一条就是 ㉒ 要守的东西`);
          assert.ok(Math.abs(o.drag) <= 4, `🔴 位移 ${o.drag}px(>4px)⇒ 用户被从阅读位置拖走了`);
          assert.ok(o.end.gap > 160, `🔴 结束时 gap=${o.end.gap}(<=160)⇒ 用户已被带到底部,不在原阅读位置`);
        } else if (kind === 'a2') {
          // 阳性对照:**必须**被拽。它不红 ⇒ 装置瞎了 ⇒ L1 的绿一文不值。
          assert.strictEqual(o.resets.length, 0,
            `守卫:A2 也必须是出窗分支(reset 0 次),实测 ${JSON.stringify(o.resets)}`);
          assert.ok(o.window.atBottomTrueSamples > 0,
            `阳性对照失效:无手势代理下 timelineAtBottom 竟一直没为 true(${o.window.atBottomTrueSamples}/${o.window.n})`
            + ` ⇒ 这一臂不再复现"被拽",装置可能瞎了`);
          assert.ok(o.snapsAfter.length > 0,
            '🔴 阳性对照失效:无手势代理这一臂**没有被拽底**(参照点后 0 次拽底调用)'
            + ' ⇒ 说明装置量不到拽底,那 L1 的"0 次"就不可信。先修装置,别读 L1 的绿');
          assert.ok(o.end.gap <= 1,
            `🔴 阳性对照失效:无手势代理这一臂结束时 gap=${o.end.gap}(应为 <=1,即已被拽到底)`);
        } else if (kind === 'a3') {
          // 显影腿:不裁"该不该",只钉**形态**。形态变了 ⇒ 红 ⇒ 按 ts-No.24 ③ 报组长。
          const WHY = '(显影腿:形态变了不一定是产品坏 —— 有人可能真修了 A3,也可能换了机制;'
            + '按组长 ts-No.24 ③,这一格是"当前已知行为",形态变了要报)';
          assert.strictEqual(o.resets.length, 1,
            `A3 形态变了:resetConversation 应恰好 1 次,实测 ${JSON.stringify(o.resets)} ${WHY}`);
          assert.ok(o.snapsAfter.length > 0, `A3 形态变了:参照点后应仍有拽底调用,实测 0 次 ${WHY}`);
          assert.ok(/resetConversation/.test(o.snapsAfter[0].stack),
            `A3 形态变了:首个拽底调用栈应出自 resetConversation,实测「${o.snapsAfter[0].stack}」 ${WHY}`);
          assert.ok(o.end.gap <= 1, `A3 形态变了:重建后应停在底部(gap<=1),实测 gap=${o.end.gap} ${WHY}`);
          console.log(`   🔵 A3 = 当前已知行为(不修,ts-No.24 ③):被拽 ${o.drag}px、拽底 ${o.snapsAfter.length} 次;`
            + '「重连重建后应停在哪」是产品问题,已交业主');
        } else if (kind === 'bottom') {
          assert.ok(o.end.gap <= 1, `不变量:必须停在底部,实测 gap=${o.end.gap}(st=${o.end.st} sh=${o.end.sh})`);
        } else if (kind === 'up') {
          assert.ok(o.end.gap > 160 && Math.abs(o.drag) <= 4,
            `不变量:必须留在上面,实测 gap=${o.end.gap} 位移=${o.drag}px`);
        }
      });
    }

    // ── L9「(i) 空操作对照」──────────────────────────────────────────────────
    // 组长裁定(撤销「M-D2 必须红」那封):M-D2 全绿**只是"没观测到差别"**,
    // 🔴 而"没观测到差别 ⇒ 空操作"是**把结论当判据用**(`:1745` 绕过 `:2309` 那套是读码论证)。
    // ⇒ 这条腿把那句话变成**会自己报警的断言**:同进程内跑基线字节与 (i) 字节,逐项比对已登记观测量。
    //    全同 ⇒ "(i) 是空操作"成立(**已裁定的非缺口**);任一不同 ⇒ 🔴 阴格命中 ⇒ 「M-D2 必须红」自动恢复生效。
    await t('L9 🔴 (i) 空操作对照:基线 vs 施加 (i) 的字节,【已登记观测量逐项相同】(任一不同 ⇒ (i) 非空操作 ⇒ 「M-D2 必须红」自动恢复生效)', async () => {
      const v = buildVariantI();
      console.log(`   (i) 副本 sha256=${v.sha256.slice(0, 16)}… 由基线 ${RENDERER_SHA.slice(0, 16)}… 在本进程内合成(3 处锚点各命中 1 次)`);
      const missing = LEGS.map(([, lg]) => lg.name).filter((n) => !VECTORS[n]);
      assert.deepStrictEqual(missing, [], `基线那一轮缺腿,无从比对:${missing.join(', ')}`);

      // ① 抖动对照:同一份【基线字节】再跑两腿。它若与自己都不同 ⇒ 登记集不稳 ⇒ 不许读 (i) 的结论
      // 🔴 覆盖 A1/A2/**C1**:C1 是三次基线对账里**实测抖过**的那条(绝对位置 117/117/1610),
      //    把它放进运行时守卫,正是为了下次它再抖时**先报警、而不是把抖动算到 (i) 头上**。
      for (const [, lg] of LEGS.filter(([, x]) => ['A1', 'A2', 'C1'].includes(x.name))) {
        const o = await runLeg(browser, lg);
        if (o.error) throw Object.assign(new Error(`抖动对照腿(${lg.name})异常: ${o.error}`), { env: o.env });
        const d = diffObservables(VECTORS[lg.name], observables(o));
        assert.deepStrictEqual(d, [],
          `🔴 抖动对照失败:【同一份基线字节】两轮量出来的登记观测量不同(${lg.name})⇒ 登记集本身不稳,`
          + `先把抖的那个量降级成类别或移出登记集,**不许读 (i) 的结论**。差异:${d.join(' | ')}`);
      }
      console.log('   ✅ 抖动对照:基线 vs 基线(A1/A2/C1 三腿)逐项相同 ⇒ 这两轮登记集没抖');

      // ② 正式对照:八条腿全部用 (i) 字节重跑,逐腿逐项比对
      const allDiffs = [];
      let fields = 0;
      for (const [, lg] of LEGS) {
        const o = await runLeg(browser, Object.assign({}, lg, { bytes: v.bytes }));
        if (o.error) throw Object.assign(new Error(`(i) 对照腿(${lg.name})异常: ${o.error}`), { env: o.env });
        const obs = observables(o);
        fields = Object.keys(obs).length;
        const d = diffObservables(VECTORS[lg.name], obs);
        console.log(`   · ${lg.name}: ${d.length ? '🔴 有差异 ⇒ ' + d.join(' | ') : `逐项相同(${fields} 项)`}`);
        d.forEach((x) => allDiffs.push(`${lg.name} ${x}`));
      }

      // 🔴 阴格:任一观测量不同 ⇒ (i) 不是空操作
      assert.deepStrictEqual(allDiffs, [],
        `🔴 阴格命中:(i) **不是**空操作 —— ${allDiffs.length} 项观测量与基线不同:${allDiffs.join(' | ')}`
        + ` ⇒ 按组长裁定,ts-No.24 ④「M-D2 必须红」**自动恢复生效**:请给 (i) 补一条真闸(钉住它带来的那个行为差别),`
        + `并把这份差异逐项贴给组长。🔵 在那之前,别再引用"(i) 是空操作"这句话。`);
      console.log(`   🔵 结论:八腿 × ${fields} 项登记观测量逐项相同 ⇒ 「(i) 是空操作」**成立**`
        + '(已裁定的非缺口:撤销依据与作废条件见交付件 §5.1)');
      // 🔴 组长裁定(「三件回齐」那封 ④,非可选):**盲区必须写进【通过】文本**,不只写在失败文本与交付件里。
      //    理由(逐字):「盲区危险的时刻恰恰是绿的时候 —— 红的时候有人在读,绿的时候没人读。
      //    一条只在交付件里声明过盲区的绿腿,在第三个人眼里就是"(i) 绝对是空操作"」⇒ **绿也要说出自己的边界。**
      console.log('   🔵 这条绿【看不见什么】(边界随绿一起打印,不用谁去翻交付件):');
      console.log('      ① 只比**已登记**的 12 项观测量。落在登记集之外的差别(例:拽底次数 127→90)'
        + '**与实测抖动同量级、无法区分** ⇒ 不得把本腿读成"(i) 对任何量都无影响"。(交付件 §六-11 / §5.4)');
      console.log('      ② 运行时抖动对照只覆盖 **3 腿**(A1/A2/C1),不是 8 腿;其余五腿靠"同字节三轮对账表"兜底,'
        + '**不是运行时守卫** ⇒ 三轮不等于三十轮。将来阴格命中时,正确读法是**先怀疑登记集**'
        + '(§5.3 就是这么发现的),不是先怀疑 (i)。(交付件 §六-10 / §5.4)');
      console.log('      ③ 只对照了 **(i) 这一个修法**;v2a/v2b/v3 那几种形状没做同样的逐项对照。(交付件 §六-13)');
      console.log('      ④ 结论的效力只及于**本进程这份基线字节**(sha256 见上)。三处 (i) 锚点是硬编码字符串,'
        + '产品件在锚点附近改动 ⇒ buildVariantI() **报错**(设计如此,不静默跳过)⇒ 本件会红;'
        + '那时请先读失败文本,别当成产品回归。(交付件 §六-12)');
    });
  } finally {
    await browser.close();
  }

  console.log(`通过 ${pass} / 失败 ${fail}${envMissing ? ` / 环境缺失 ${envMissing}` : ''}   (renderer sha256=${RENDERER_SHA.slice(0, 16)}…)`);
  if (envMissing) {
    console.log('  ⚠️  环境缺失 ⇒ rc=3(不是产品失败,也不是通过)');
    process.exit(3);
  }
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 别把 rc=0 读成"㉒ 修好了":A1 本来就是好的(本件钉住它不被改坏);'
    + 'A3 是**当前已知行为**,组长裁定不修,「重连重建后应停在哪」已交业主。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
