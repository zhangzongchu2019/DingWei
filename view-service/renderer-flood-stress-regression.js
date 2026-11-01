// R-V2-0105-P1-STRESS／STRESS-DET：单会话洪流压力格（真控制台页＋两个 /view iframe＋桩 Hub 的 SSE）。
// 灌入：flood 会话同一回合 100000 条 assistant_text delta，经 SSE 以每秒不少于 5000 条推入；calm 会话同时正常流。
// 判据零（确定性，排在毫秒判据之前）：该回合组事件数与正文 DOM 节点数的峰值、终值都不超过软上限 20000＋余量 MARGIN（固定 100）。
//   事件数峰值是精确值：给该组的 events 数组挂 push 钩子、给 group.events 赋值挂 setter，每次长度变化都记；
//   DOM 节点数峰值取 MutationObserver 每批变更后的计数（回合卡 article 之下元素＋文本节点）。
// 判据三：灌完后该回合 assistant_text 全文与逐条拼接的期望值逐字相同；calm 会话正文完整、不受影响。
// 读数（不作判据，组长 STRESS-DET 第 1 条）：agents 定时请求相邻间隔、longtask 最大值／总数／总时长、实际推入速率。
//   agents 定时器在本格把 15 s 缩到 AGENT_POLL_MS＝1 s（只改顶层页的 setInterval(…,15000)），这样洪流期间能看到十几个间隔。
// 反向：RENDERER_OVERRIDE 改指去合并的副本（STRESS_EXPECT_MUTANT=1）⇒ 先断言事件数峰值确实涨到约 10 万（变异生效），再断言判据零红。
// R-V2-0105-F12-TESTS（正式复核件 2.1／2.2／5）：
//   F1（flood 场景）：灌入期间长度大于 5000 的数组 sort 次数＝0（确定性硬断言，保护 70121033 顺序到达免重排的收益）；
//      计数在 flood iframe 里包 Array.prototype.sort，自装钩起算。去掉 70121033 的副本须红在 F1。
//   F2（late-order 场景，3 万条）：cursor 12000 扣下，等 cursor 21000 发出后才补发（迟到约 9000 条＜较新一半约 1 万条）。
//      流内顺序固定 ⇒ 第 20001 条 push 时同步合并较旧一半，迟到那条必在合并之后到达，与机器快慢无关。
//      断言全文与位置逐字正确；把「较旧一半」改为整组合并（R7）的副本须红在位置判据。
const assert = require('assert');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { launchChromium } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const GO_SOURCE = path.join(HERE, '..', 'internal', 'm8', 'console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const RATE = 6000;           // 每秒条数（目标；实际速率另记）
const SOFT_CAP = 20000;
const MARGIN = 100;          // 判据零余量
const AGENT_POLL_MS = 1000;
const EXPECT_MUTANT = process.env.STRESS_EXPECT_MUTANT === '1';
const READINGS = process.env.STRESS_READINGS || '';               // 读数 JSON 落盘路径（可选）
const DEADLINE_S = Number(process.env.STRESS_DEADLINE_S || 180);   // 推入总时限：页面收不动（背压）时到点停推，X0 红并照常出读数
const FLOOD = 'flood-a-2642';
const CALM = 'calm-b-2642';
const sleep = ms => new Promise(r => setTimeout(r, ms));
const deltaText = i => `d${i};`;

function consoleHTML() {
  const src = fs.readFileSync(GO_SOURCE, 'utf8');
  const s = src.indexOf(START);
  const e = src.indexOf(END, s + START.length);
  assert.ok(s >= 0 && e > s, 'consolePageHTML markers missing');
  return src.slice(s + START.length, e).split('{{CONSOLE_VERSION_TEXT}}').join('stress')
    .split('{{CONSOLE_VERSION}}').join('stress').split('{{CONSOLE_COMMIT}}').join('stress');
}

function frame(epoch, seq, ev) {
  return `id: ${epoch}-${seq}\ndata: ${JSON.stringify(ev)}\n\n`;
}

function startServer(sc) {
  const html = consoleHTML();
  const renderer = fs.readFileSync(RENDERER);
  const st = { sc, agentGETs: [], samples: [], go: null, floodDone: null, floodStart: 0, floodEnd: 0, calmSent: 0, calmStop: false };
  let release;
  st.go = new Promise(r => { release = r; });
  st.floodDone = new Promise(r => { st.finishFlood = r; });
  const server = http.createServer((req, res) => {
    const u = new URL(req.url, 'http://x');
    const p = u.pathname;
    const json = (obj) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(obj)); };
    if (p === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
    if (p === '/api/console/agents') {
      st.agentGETs.push(Date.now());
      return json({ agents: [FLOOD, CALM].map(n => ({ agent_id: `id-${n}`, name: n.split('-')[1], full_name: n, origin: 'selfhost', group_code: '', online: true })) });
    }
    if (p === '/api/console/platform') return json({ role: 'owner', actions: ['read'], items: [] });
    if (p === '/api/console/agent-groups') return json({ principal: 'o', owned_agents: [], groups: [] });
    if (p === '/go') { release(); return json({ ok: true }); }
    if (p === '/sample') {   // 页内采样（sendBeacon）：页面冻住后 node 侧读不到页内状态，就用它冻住前最后一次报的
      let body = '';
      req.on('data', c => { body += c; });
      req.on('end', () => { try { const v = JSON.parse(body); v.at = Date.now(); st.samples.push(v); } catch (_) {} res.writeHead(204); res.end(); });
      return;
    }
    const m = p.match(/^\/view\/([^/]+)(\/.*)?$/);
    if (m) {
      const session = decodeURIComponent(m[1]);
      const rest = m[2] || '/';
      if (rest === '/' ) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(renderer); return; }
      if (rest === '/status') return json({ session, state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'o', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
      if (rest === '/events') {
        res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
        res.flushHeaders();
        if (u.searchParams.get('after')) { req.on('close', () => {}); return; }   // 重连：本格不该发生，保持空连接（读数里看连接数）
        (session === FLOOD ? pumpFlood : pumpCalm)(res, st);
        return;
      }
      return json({});
    }
    if (p.startsWith('/api/') || p === '/tasks') return json({});
    res.writeHead(404); res.end();
  });
  return new Promise(resolve => server.listen(0, '127.0.0.1', () => resolve({ server, port: server.address().port, st })));
}

// 发送顺序：cursor 1..total，cursor sc.lateCursor 扣下，在 cursor sc.lateAfter 之后紧接着补发；seq 按发送顺序递增。
function floodOrder(sc) {
  const order = [];
  for (let c = 1; c <= sc.total; c++) {
    if (c === sc.lateCursor) continue;
    order.push(c);
    if (c === sc.lateAfter) order.push(sc.lateCursor);
  }
  return order;
}

async function pumpFlood(res, st) {
  await st.go;
  const sc = st.sc;
  const epoch = 'f1';
  const order = floodOrder(sc);
  res.write(frame(epoch, 1, { kind: 'user_input', text: '灌入', cursor: '0000000', delivery_id: 'd-f', turn_id: 't-f', stage: 'cli_injected', ts: 0 }));
  st.floodStart = Date.now();
  let i = 0;
  const tick = 10;
  let lastLog = st.floodStart;
  while (i < order.length) {
    if (Date.now() - st.floodStart > DEADLINE_S * 1000) { st.aborted = i; break; }
    if (Date.now() - lastLog >= 5000) { lastLog = Date.now(); console.error(`${sc.name}: pushed ${i} in ${((lastLog - st.floodStart) / 1000).toFixed(1)} s`); }
    const due = Math.min(order.length, Math.floor(((Date.now() - st.floodStart) / 1000) * RATE) + 1);
    let chunk = '';
    for (; i < due; i++) {
      const c = order[i];
      chunk += frame(epoch, i + 2, { kind: 'assistant_text', text: deltaText(c - 1), cursor: String(c).padStart(7, '0'), delivery_id: 'd-f', turn_id: 't-f', ts: 0 });
    }
    if (chunk && !res.write(chunk)) await Promise.race([new Promise(r => res.once('drain', r)), sleep(DEADLINE_S * 1000)]);   // 背压：浏览器收不动就等，实际速率另记
    await sleep(tick);
  }
  st.floodEnd = Date.now();
  st.pushed = i;
  if (st.aborted !== undefined) { st.finishFlood(); return; }
  res.write(frame(epoch, order.length + 2, { kind: 'state_change', text: '', cursor: '9999999', delivery_id: 'd-f', turn_id: 't-f', ts: 0, data: { change: 'turn_completed' } }));
  st.finishFlood();
}

async function pumpCalm(res, st) {
  const epoch = 'c1';
  res.write(frame(epoch, 1, { kind: 'user_input', text: '正常', cursor: '0000000', delivery_id: 'd-c', turn_id: 't-c', stage: 'cli_injected', ts: 0 }));
  await st.go;
  let i = 0;
  while (!st.calmStop) {
    res.write(frame(epoch, i + 2, { kind: 'assistant_text', text: `c${i};`, cursor: String(i + 1).padStart(7, '0'), delivery_id: 'd-c', turn_id: 't-c', ts: 0 }));
    st.calmSent = ++i;
    await sleep(100);
  }
}

// 每个 frame 都装：longtask 记录；flood iframe 另挂事件数精确峰值钩子与 DOM 节点计数
function initScript(arg) {
  window.__lt = [];
  try {
    new PerformanceObserver(list => { for (const e of list.getEntries()) window.__lt.push(e.duration); }).observe({ type: 'longtask', buffered: true });
  } catch (_) { window.__ltUnsupported = true; }
  if (window === window.top) {
    const orig = window.setInterval;
    window.setInterval = function (fn, ms, ...rest) { return orig.call(this, fn, ms === 15000 ? arg.pollMs : ms, ...rest); };
  }
}

function instrumentFlood() {
  // 在 SSE 推第一条前装上：turnGroups 由产品脚本的顶层 const 声明，这里只包它的 set
  window.__peakEvents = 0; window.__peakNodes = 0; window.__lastNodes = 0; window.__bigSorts = 0;
  // F1：本 iframe 内长度大于 5000 的数组每 sort 一次记一次（产品重渲／合并若对整组重排就会计入）
  const origSort = Array.prototype.sort;
  Array.prototype.sort = function (...a) { if (this.length > 5000) window.__bigSorts++; return origSort.apply(this, a); };
  const watch = arr => {
    if (arr.length > window.__peakEvents) window.__peakEvents = arr.length;
    arr.push = function (...a) {
      const r = Array.prototype.push.apply(this, a);
      if (this.length > window.__peakEvents) {
        window.__peakEvents = this.length;
        // 页内定时器在洪流下会饿死；从 push 钩子每涨 1000 报一次，页面冻住后 node 侧仍有冻住前的峰值
        if (this.length % 1000 === 0) navigator.sendBeacon('/sample', JSON.stringify({ peakEvents: this.length, events: this.length, peakNodes: window.__peakNodes, lastSeq: lastContiguousSeq, bigSorts: window.__bigSorts }));
      }
      return r;
    };
    return arr;
  };
  const countNodes = node => { let n = 0; const w = document.createTreeWalker(node, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT); while (w.nextNode()) n++; return n; };
  const origSet = turnGroups.set.bind(turnGroups);
  turnGroups.set = (k, g) => {
    if (k === 't-f') {
      let events = watch(g.events);
      Object.defineProperty(g, 'events', { configurable: true, get: () => events, set: v => { events = watch(v); } });
      new MutationObserver(() => { const n = countNodes(g.node); window.__lastNodes = n; if (n > window.__peakNodes) window.__peakNodes = n; })
        .observe(g.node, { childList: true, subtree: true, characterData: true });
    }
    return origSet(k, g);
  };
  setInterval(() => {
    const g = turnGroups.get('t-f');
    navigator.sendBeacon('/sample', JSON.stringify({ peakEvents: window.__peakEvents, events: g ? g.events.length : 0,
      peakNodes: window.__peakNodes, lastSeq: lastContiguousSeq, bigSorts: window.__bigSorts }));
  }, 500);
}

// 页面冻住时 evaluate 永不返回：一律限时，超时记为「页面无响应」
function evalOr(frame, fn, arg, ms) {
  return Promise.race([frame.evaluate(fn, arg).then(v => ({ ok: true, v })), sleep(ms).then(() => ({ ok: false }))]);
}

const SCENARIOS = [
  { name: 'flood', total: 100000, lateCursor: 0, lateAfter: 0 },
  { name: 'late-order', total: 30000, lateCursor: 12000, lateAfter: 21000 },
];

async function runScenario(browser, sc) {
  const TOTAL = sc.total;
  const sent = floodOrder(sc).length;
  const { server, port, st } = await startServer(sc);
  const base = `http://127.0.0.1:${port}`;
  let exitCode = 0;
  const results = [];
  let readings = null;
  const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
  try {
    await page.addInitScript(initScript, { pollMs: AGENT_POLL_MS });
    await page.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => document.querySelectorAll('#a button').length === 2);
    await page.evaluate(([a, b]) => { openTab(b); openTab(a); }, [FLOOD, CALM]);   // flood 后开 ⇒ 它是可见页签
    const frameOf = name => page.frames().find(f => {
      try { const url = new URL(f.url()); return url.origin === base && url.pathname === `/view/${name}`; }
      catch (_) { return false; }
    });
    let floodFrame, calmFrame;
    for (let k = 0; k < 100 && !(floodFrame && calmFrame); k++) { await sleep(100); floodFrame = frameOf(FLOOD); calmFrame = frameOf(CALM); }
    assert.ok(floodFrame && calmFrame, 'iframes not found');
    const frameMetadata = await page.locator('#panes iframe').evaluateAll(es => es.map(e => ({src:e.src,instance:e.dataset.viewRefreshInstance})));
    for (const f of [floodFrame,calmFrame]) { const url = new URL(f.url()); const pane = frameMetadata.find(p => p.src===f.url()); assert.ok(pane && pane.instance && url.searchParams.get('view_refresh_instance')===pane.instance,'iframe refresh metadata must remain bound'); }
    console.log('STRESS_FRAME_METADATA '+JSON.stringify({scenario:sc.name,frames:frameMetadata}));
    await floodFrame.waitForFunction(() => typeof turnGroups !== 'undefined' && typeof stats !== 'undefined');
    await floodFrame.evaluate(instrumentFlood);
    await calmFrame.waitForFunction(() => typeof turnGroups !== 'undefined' && turnGroups.has('t-c'));
    await sleep(3 * AGENT_POLL_MS);   // 洪流前的正常间隔
    const goAt = Date.now();
    await page.evaluate(() => fetch('/go'));
    await st.floodDone;
    // 等页面收齐（最后一帧 seq＝TOTAL+2）；node 侧轮询，不用 waitForFunction(async)
    const deadline = Date.now() + (st.aborted !== undefined ? 5000 : 120000);
    let lastSeq = 0;
    let unresponsive = false;
    while (Date.now() < deadline) {
      const r = await evalOr(floodFrame, () => lastContiguousSeq, null, 5000);
      if (!r.ok) { unresponsive = true; break; }
      lastSeq = r.v;
      if (lastSeq >= sent + 2) break;
      await sleep(200);
    }
    const receivedAt = Date.now();
    await sleep(3 * AGENT_POLL_MS);   // 洪流后的正常间隔
    st.calmStop = true;
    await sleep(300);

    const floodR = unresponsive ? { ok: false } : await evalOr(floodFrame, ({ total }) => {
      flushPendingRenders();
      const g = turnGroups.get('t-f');
      const text = g.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join('');
      let expected = '';
      for (let i = 0; i < total; i++) expected += `d${i};`;
      const w = document.createTreeWalker(g.node, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT); let nodes = 0; while (w.nextNode()) nodes++;
      let firstDiff = 0;
      while (firstDiff < text.length && text[firstDiff] === expected[firstDiff]) firstDiff++;
      return { peakEvents: window.__peakEvents, finalEvents: g.events.length, peakNodes: window.__peakNodes, finalNodes: nodes, bigSorts: window.__bigSorts,
        same: text === expected, firstDiff, textLen: text.length, expectedLen: expected.length, text, lt: window.__lt, ltUnsupported: !!window.__ltUnsupported };
    }, { total: TOTAL }, 30000);
    if (!floodR.ok) unresponsive = true;
    const last = st.samples[st.samples.length - 1] || {};
    const samplePeak = st.samples.reduce((m, v) => Math.max(m, v.peakEvents || 0), 0);
    const sampleSorts = st.samples.reduce((m, v) => Math.max(m, v.bigSorts || 0), 0);
    const flood = floodR.ok ? floodR.v : { peakEvents: samplePeak, finalEvents: last.events || 0, peakNodes: last.peakNodes || 0, finalNodes: last.peakNodes || 0,
      bigSorts: sampleSorts, same: false, firstDiff: -1, textLen: -1, expectedLen: -1, text: '', lt: [], ltUnsupported: false };
    if (!unresponsive) lastSeq = (await evalOr(floodFrame, () => lastContiguousSeq, null, 5000)).v ?? lastSeq;
    else lastSeq = last.lastSeq || lastSeq;
    const calmR = unresponsive ? { ok: false } : await evalOr(calmFrame, () => {
      const g = turnGroups.get('t-c');
      return { text: g.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join(''), lt: window.__lt };
    }, null, 10000);
    const calm = calmR.ok ? calmR.v : { text: '', lt: [] };
    const topR = unresponsive ? { ok: false } : await evalOr(page, () => window.__lt, null, 10000);
    const topLt = topR.ok ? topR.v : [];

    const sha = crypto.createHash('sha256').update(flood.text, 'utf8').digest('hex');
    const pushSecs = (st.floodEnd - st.floodStart) / 1000;
    const recvSecs = (receivedAt - st.floodStart) / 1000;
    const window_ = st.agentGETs.filter(t => t >= goAt - AGENT_POLL_MS && t <= receivedAt + 2 * AGENT_POLL_MS);
    const gaps = window_.slice(1).map((t, k) => t - window_[k]);
    const windowEnd = receivedAt + 2 * AGENT_POLL_MS;
    if (window_.length) gaps.push(Math.min(Date.now(), windowEnd) - window_[window_.length - 1]);   // 末段：最后一次请求到窗口末，冻住时的静默在这一段
    const allLt = [...topLt, ...flood.lt, ...calm.lt];
    readings = {
      scenario: sc.name, late_cursor: sc.lateCursor || null, late_after: sc.lateAfter || null,
      renderer_sha256: crypto.createHash('sha256').update(fs.readFileSync(RENDERER)).digest('hex'),
      total: TOTAL, pushed: st.pushed, aborted_at_deadline: st.aborted !== undefined, target_rate: RATE, margin: MARGIN, bound: SOFT_CAP + MARGIN,
      push_seconds: +pushSecs.toFixed(2), push_rate: Math.round(st.pushed / pushSecs),
      page_unresponsive: unresponsive, last_sample_age_ms: last.at ? Date.now() - last.at : null, beacon_samples: st.samples.length,
      receive_seconds: +recvSecs.toFixed(2), receive_rate: Math.round(TOTAL / recvSecs), last_seq: lastSeq,
      peak_events: flood.peakEvents, final_events: flood.finalEvents, peak_nodes: flood.peakNodes, final_nodes: flood.finalNodes,
      big_sorts: flood.bigSorts, text_same: flood.same, first_diff: flood.firstDiff, text_len: flood.textLen, expected_len: flood.expectedLen, text_sha256: sha,
      calm_sent: st.calmSent, calm_complete: calm.text === Array.from({ length: st.calmSent }, (_, i) => `c${i};`).join(''),
      agents_poll_ms: AGENT_POLL_MS, agents_requests_in_window: window_.length, agents_gap_max_ms: gaps.length ? Math.max(...gaps) : null,
      agents_gaps_ms: gaps,
      longtask_supported: !flood.ltUnsupported, longtask_count: allLt.length,
      longtask_max_ms: allLt.length ? Math.round(Math.max(...allLt)) : 0, longtask_total_ms: Math.round(allLt.reduce((a, b) => a + b, 0)),
    };
    console.log(`STRESS READINGS ${sc.name} ` + JSON.stringify(readings));

    const check = (name, fn) => { try { fn(); results.push(`  PASS [${sc.name}] ${name}`); } catch (e) { exitCode = 1; results.push(`  FAIL [${sc.name}] ${name}: ${e.message}`); } };
    check('X0 灌入真走完：页面收齐最后一帧、推入速率不低于每秒 5000 条', () => {
      assert.strictEqual(lastSeq, sent + 2);
      assert.ok(readings.push_rate >= 5000, `push rate ${readings.push_rate}/s`);
    });
    if (EXPECT_MUTANT && sc.name === 'flood') {
      // 变异生效的证据：事件数越过软上限还在涨（未合并）。涨到约 10 万的前提是页面撑得住；撑不住（冻住）时如实报冻住前的峰值。
      check('R0 反向先证变异生效：事件数峰值越过软上限（目标约 10 万）', () => {
        assert.ok(flood.peakEvents > SOFT_CAP + MARGIN, `peak events only ${flood.peakEvents}: mutation did not take`);
        assert.ok(flood.peakEvents >= 90000 || unresponsive, `peak events ${flood.peakEvents} below 90000 while the page stayed responsive: mutation did not take`);
      });
    }
    check(`S0 判据零：事件数峰值／终值 ≤ ${SOFT_CAP}+${MARGIN}`, () => {
      assert.ok(flood.peakEvents <= SOFT_CAP + MARGIN, `peak events ${flood.peakEvents}`);
      assert.ok(flood.finalEvents <= SOFT_CAP + MARGIN, `final events ${flood.finalEvents}`);
    });
    check(`S0b 判据零：正文 DOM 节点数峰值／终值 ≤ ${SOFT_CAP}+${MARGIN}`, () => {
      assert.ok(flood.peakNodes <= SOFT_CAP + MARGIN, `peak nodes ${flood.peakNodes}`);
      assert.ok(flood.finalNodes <= SOFT_CAP + MARGIN, `final nodes ${flood.finalNodes}`);
    });
    check('S3 判据三：全文与逐条拼接逐字相同', () => assert.ok(flood.same, `text differs: ${flood.textLen} vs ${flood.expectedLen}`));
    check('S4 另一会话正常流完整', () => assert.ok(readings.calm_complete && st.calmSent > 0, 'calm session text incomplete'));
    if (sc.name === 'flood') {
      // F1：顺序到达时不应对大数组重排；去 70121033 的副本每次重渲都整组 sort ⇒ 计数远大于 0
      check('F1 灌入期间长度大于 5000 的数组 sort 次数＝0', () => assert.strictEqual(flood.bigSorts, 0, `big array sorts during flood: ${flood.bigSorts}`));
    }
    if (sc.name === 'late-order') {
      // F2 前提：迟到那条确在合并之后到达（第 20001 条时合并，迟到那条排在第 lateAfter 条之后发出）且迟到量小于较新一半
      check('F2a 前提：越过软上限并已合并，迟到那条在合并之后到达、迟到量小于较新一半', () => {
        assert.ok(sc.lateAfter > SOFT_CAP && sc.lateCursor > Math.floor(SOFT_CAP / 2), 'scenario does not exercise the newer-half path');
        assert.ok(sc.lateAfter - sc.lateCursor < Math.floor(SOFT_CAP / 2), 'lag not smaller than the newer half');
        assert.ok(flood.peakEvents > SOFT_CAP && flood.finalEvents < SOFT_CAP, `no compaction observed: peak ${flood.peakEvents} final ${flood.finalEvents}`);
      });
      // F2：位置逐字——迟到那片必须正好夹在前后两片之间（S3 已比全文；这里单独点名迟到片的位置，便于读红）
      check('F2 迟到片位置正确（夹在前后两片之间）', () => {
        const prev = deltaText(sc.lateCursor - 2), mine = deltaText(sc.lateCursor - 1), next = deltaText(sc.lateCursor);
        const at = flood.text.indexOf(prev + mine + next);
        assert.ok(at >= 0, `late delta ${mine} not between ${prev} and ${next}; first diff at ${flood.firstDiff}`);
      });
    }
    // 读数（不判红绿，STRESS-DET 第 1 条）：agents 间隔对 2 倍正常间隔、longtask 对 500 ms
    results.push(`  READING agents 最大间隔 ${readings.agents_gap_max_ms} ms（2 倍正常间隔 ${2 * AGENT_POLL_MS} ms）${readings.agents_gap_max_ms > 2 * AGENT_POLL_MS ? ' 超' : ''}；窗口内请求 ${readings.agents_requests_in_window} 次`);
    results.push(`  READING longtask 最大 ${readings.longtask_max_ms} ms、${readings.longtask_count} 次、合计 ${readings.longtask_total_ms} ms${unresponsive ? '（页面无响应，页内 longtask 未取到）' : ''}`);
  } finally {
    st.calmStop = true;
    await Promise.race([page.close(), sleep(10000)]);
    server.close();
  }
  return { exitCode, results, readings };
}

(async () => {
  setTimeout(() => { console.error('stress watchdog: overall deadline exceeded'); process.exit(3); }, (SCENARIOS.length * (DEADLINE_S + 240)) * 1000).unref();
  let browser;
  let exitCode = 0;
  const all = {};
  try {
    browser = await launchChromium();
    const lines = [];
    for (const sc of SCENARIOS) {
      const r = await runScenario(browser, sc);
      if (r.exitCode) exitCode = 1;
      all[sc.name] = r.readings;
      lines.push(...r.results);
    }
    if (READINGS) fs.writeFileSync(READINGS, JSON.stringify(all, null, 2) + '\n');
    console.log(lines.join('\n'));
    if (EXPECT_MUTANT) console.log(`stress (mutant run): ${exitCode ? 'RED as required' : 'still GREEN'}`);
    else console.log(exitCode ? 'R-V2-0105 flood stress: FAIL' : 'R-V2-0105 flood stress: ALL PASS');
  } catch (e) {
    console.error(e);
    exitCode = 2;
  } finally {
    if (browser) await Promise.race([browser.close(), sleep(10000)]);
    process.exit(exitCode);
  }
})();
