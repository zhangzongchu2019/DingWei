// View 长文本卡死 hotfix 回归(修复口径 A/B/C/D,验收 E)
//
// 用法:
//   node renderer-longtext-regression.js
//   RENDERER_FILE=<被测件> BASELINE_FILE=<修复前件> node renderer-longtext-regression.js
//
// BASELINE_FILE 给出时会对同一负载跑一遍修复前的件,作为 A 的对照:
// 证明 renderMarkdown 调用数/最长单事件阻塞这两个度量确实由本次改动移动,
// 而不是"换了个负载碰巧变绿"。
const { chromium } = require('playwright');
const fs = require('fs');
const assert = require('assert');

const RENDERER_FILE = process.env.RENDERER_FILE || 'renderer-canary-sse.html';
const BASELINE_FILE = process.env.BASELINE_FILE || '';
const SESSION = 'longtext-test-2642';

const LONG_REPLY_CHUNKS = 800;      // 800 chunk × 10 字 = 8000 字,对齐现网 7964 字契约文本
const CHUNK_TEXT = '契约文本长回复压测甲';   // 10 个中文字

async function boot(browser, body) {
  const context = await browser.newContext();
  await context.route('http://renderer.test/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/renderer') return route.fulfill({ contentType: 'text/html', body });
    if (path.endsWith('/status')) {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        session: SESSION, auth_mode: 'legacy', readonly: true, state: 'idle',
      }) });
    }
    if (path.endsWith('/events')) return route.abort();
    return route.fulfill({ contentType: 'application/json', body: '{}' });
  });
  const page = await context.newPage();
  await page.goto('http://renderer.test/renderer', { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => {
    clearInterval(statusTimer);
    // 回放态会影响滚动跟随;测试里显式收敛到"已结束回放",各用例自行 begin/finish
    finishTimelineReplay(false);
    resetConversation();
  });
  return { context, page };
}

// 在页内装 renderMarkdown 计数器(classic script 顶层函数声明=全局属性,可覆写)
async function instrument(page) {
  await page.evaluate(() => {
    window.__mdCalls = 0;
    if (!window.__mdOrig) window.__mdOrig = window.renderMarkdown;
    window.renderMarkdown = function (...a) { window.__mdCalls += 1; return window.__mdOrig.apply(this, a); };
  });
}

// 单回合长回复负载:1 条 user_input + N 条 assistant_text chunk + turn_completed
// 返回 renderMarkdown 调用数、最长单事件同步阻塞、总耗时
async function driveLongReply(page, chunks, chunkText) {
  await instrument(page);
  return page.evaluate(({ chunks, chunkText }) => {
    resetConversation();
    window.__mdCalls = 0;
    const t0 = performance.now();
    let maxBlock = 0;
    handleIncomingEvent({ kind: 'user_input', text: '请输出契约全文', delivery_id: 'd-long', turn_id: 't-long', cursor: '0001' });
    for (let i = 0; i < chunks; i += 1) {
      const s = performance.now();
      handleIncomingEvent({ kind: 'assistant_text', text: chunkText, delivery_id: 'd-long', turn_id: 't-long',
        cursor: `1${String(i).padStart(5, '0')}` });
      const d = performance.now() - s;
      if (d > maxBlock) maxBlock = d;
    }
    const streamMs = performance.now() - t0;
    handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.5 },
      delivery_id: 'd-long', turn_id: 't-long', cursor: '900000' });
    if (window.__dwRender) window.__dwRender.flush();
    return {
      mdCalls: window.__mdCalls,
      maxBlockMs: Math.round(maxBlock * 100) / 100,
      streamMs: Math.round(streamMs),
      totalMs: Math.round(performance.now() - t0),
      turnGroups: document.querySelectorAll('#timeline > .turn-group').length,
      timelineChildren: document.getElementById('timeline').children.length,
      // G 之后「完整性」必须对**可取回的全文**断言,不能对 DOM 断言 ——
      // 超阈值时 DOM 里按设计只有摘要,正文完整地留在 group.events 里、可由折叠条取回。
      fullChars: (() => {
        const grp = turnGroups.get('t-long');
        return grp ? grp.events.filter(e => e.kind === 'assistant_text')
          .reduce((n, e) => n + (e.text || '').length, 0) : -1;
      })(),
      folded: !!document.querySelector('#timeline > [data-turn-key="t-long"] .longtext-bar'),
      assistantChars: (() => {
        // mergeMetaIntoMarkdown 会把时间/类型 meta 挪进首个 markdown 块,
        // 直接取 innerText 会把 meta 文字算进正文 ⇒ 先剥离再计数
        const el = document.querySelector('#timeline .turn-assistant .event-content');
        if (!el) return -1;
        const clone = el.cloneNode(true);
        clone.querySelectorAll('.event-meta').forEach(n => n.remove());
        return clone.innerText.length;
      })(),
    };
  }, { chunks, chunkText });
}

(async () => {
  const rendererBody = fs.readFileSync(RENDERER_FILE, 'utf-8');
  const browser = await chromium.launch({ headless: true });
  const fail = [];
  const ok = (name) => console.log(`  PASS  ${name}`);
  try {
    const { context, page } = await boot(browser, rendererBody);

    // ── T0 策略台账 ⇄ 实现 双向对撞 ──────────────────────────
    // 上限这个数**只在本格出现一次**;其余各格一律从实现读 CAP 派生,
    // 于是「改上限」只需要改这里一行,不会散落成一堆手写常数(手写分母是本线最贵的坑)。
    const EXPECTED_CAP = 200;   // ← 业主硬约束「始终最新 200 条」。这不是可优化项,改它要先改约束。
    const caps = await page.evaluate(() => ({
      max: window.__dwRender.maxCards(), throttle: window.__dwRender.throttleMs(),
    }));
    assert.strictEqual(caps.max, EXPECTED_CAP, `B: 上限必须是 ${EXPECTED_CAP},实为 ${caps.max}`);
    assert.ok(caps.throttle >= 50 && caps.throttle <= 100, `A: 节流窗口须在 50~100ms,实为 ${caps.throttle}`);
    const CAP = caps.max;
    const OVER = 50;            // 超出上限多灌的条数,用于验证"只留最新 CAP 条"
    ok(`T0 上限=${CAP}(台账 ⇄ 实现对撞)· 节流窗口=${caps.throttle}ms`);

    // ── T1 (E①) 初始回放 CAP+OVER 条 → 只留最新 CAP ─────────────
    const t1 = await page.evaluate(({ CAP, OVER }) => {
      resetConversation();
      beginTimelineReplay();
      for (let i = 0; i < CAP + OVER; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `回放 ${i}`, delivery_id: `d-${i}`,
          turn_id: `t-${i}`, cursor: `${String(i).padStart(4, '0')}` });
      }
      finishTimelineReplay(false);
      const tl = document.getElementById('timeline');
      return {
        count: tl.children.length,
        firstKey: tl.firstElementChild?.dataset.turnKey,
        lastKey: tl.lastElementChild?.dataset.turnKey,
        turnGroupsSize: turnGroups.size,
      };
    }, { CAP, OVER });
    assert.strictEqual(t1.count, CAP, `E①: 初始 ${CAP + OVER} 条后应只剩 ${CAP},实为 ${t1.count}`);
    assert.strictEqual(t1.firstKey, `t-${OVER}`, `E①: 应只保留最新 ${CAP} 条,最旧实为 ${t1.firstKey}`);
    assert.strictEqual(t1.lastKey, `t-${CAP + OVER - 1}`);
    assert.strictEqual(t1.turnGroupsSize, CAP, `D: turnGroups 应随逐出收敛到 ${CAP},实为 ${t1.turnGroupsSize}`);
    ok(`T1 (E①) 初始 ${CAP + OVER} → 仅最新 ${CAP},turnGroups 同步收敛`);

    // ── T2 (E②) 实时追加第 201 条 → 最旧立即消失(不等 flush)────
    const t2 = await page.evaluate((CAP) => {
      resetConversation();
      for (let i = 0; i < CAP; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `实时 ${i}`, delivery_id: `r-${i}`,
          turn_id: `rt-${i}`, cursor: `${String(i).padStart(4, '0')}` });
      }
      const tl = document.getElementById('timeline');
      const before = { count: tl.children.length, firstKey: tl.firstElementChild?.dataset.turnKey };
      handleIncomingEvent({ kind: 'assistant_text', text: `第 ${CAP + 1} 条`, delivery_id: `r-${CAP}`,
        turn_id: `rt-${CAP}`, cursor: `${String(CAP).padStart(4, '0')}` });
      // 不调用 flush:逐出必须在 handleIncomingEvent 返回时已完成
      return { before, after: { count: tl.children.length, firstKey: tl.firstElementChild?.dataset.turnKey,
        lastKey: tl.lastElementChild?.dataset.turnKey, hasEvicted: !!turnGroups.get('rt-0') } };
    }, CAP);
    assert.strictEqual(t2.before.count, CAP);
    assert.strictEqual(t2.before.firstKey, 'rt-0');
    assert.strictEqual(t2.after.count, CAP, `E②: 追加第 ${CAP + 1} 条后仍须为 ${CAP},实为 ${t2.after.count}`);
    assert.strictEqual(t2.after.firstKey, 'rt-1', 'E②: 最旧一条须立即消失');
    assert.strictEqual(t2.after.lastKey, `rt-${CAP}`);
    assert.strictEqual(t2.after.hasEvicted, false, 'D: 被逐出的 turnGroups 记录须同步清除');
    ok(`T2 (E②) 第 ${CAP + 1} 条进入 → 最旧同步逐出,无需等 flush`);

    // ── T3 (B) 曾绕过上限的分支:delivery_update / 待提交区转入 ──
    // 旧实现只在 dispatchIncomingEvent 的最后一个分支裁剪,该路径提前 return ⇒ 上限失效。
    // 本格先自证机制在场(待提交区确实会把卡片推进时间线),再断上限,避免空转。
    const t3 = await page.evaluate((CAP) => {
      const tl = document.getElementById('timeline');
      const wcount = () => document.querySelectorAll('#waitingList > .waiting-item').length;
      const inject = (id) => {
        handleIncomingEvent({ kind: 'user_input', text: `排队的外部任务 ${id}`, data: { lifecycle: 'queued' },
          delivery_id: id, turn_id: '', cursor: `${id}.1` });
        const parked = { tl: tl.children.length, waiting: wcount() };
        // stage 必须写在顶层:normalizeEvent 取的是 ev.delivery_stage || ev.stage,不看 data.stage
        handleIncomingEvent({ kind: 'delivery_update', delivery_id: id, stage: 'cli_injected',
          data: { lifecycle: 'cli_injected' }, cursor: `${id}.2` });
        return { parked, admitted: { tl: tl.children.length, waiting: wcount() } };
      };
      resetConversation();
      for (let i = 0; i < CAP - 1; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `底 ${i}`, delivery_id: `w-${i}`,
          turn_id: `wt-${i}`, cursor: `${String(i).padStart(4, '0')}` });
      }
      const under = inject('d-wait-a');          // 未满:转入必须让时间线 +1(机制在场证明)
      const oldestBefore = tl.firstElementChild?.dataset.turnKey;
      const full = inject('d-wait-b');           // 已满:转入必须仍受 CAP 限额
      return { under, oldestBefore, full, oldestAfter: tl.firstElementChild?.dataset.turnKey };
    }, CAP);
    assert.strictEqual(t3.under.parked.tl, CAP - 1, '前置:排队投递不得直接进时间线');
    assert.strictEqual(t3.under.parked.waiting, 1, '前置:排队投递须先进待提交区');
    assert.strictEqual(t3.under.admitted.waiting, 0, '前置:注入后须移出待提交区');
    assert.strictEqual(t3.under.admitted.tl, CAP,
      `前置:注入后须把卡片推进时间线(${CAP - 1}→${CAP});不成立则本格空转,B 的结论不可用`);
    assert.strictEqual(t3.full.admitted.waiting, 0);
    assert.strictEqual(t3.full.admitted.tl, CAP,
      `B: 待提交区转入时间线的分支也须受 ${CAP} 限额,实为 ${t3.full.admitted.tl}`);
    assert.strictEqual(t3.oldestBefore, 'wt-0');
    assert.notStrictEqual(t3.oldestAfter, 'wt-0', `B: 该分支触发的第 ${CAP + 1} 张卡须同样逐出最旧一条`);
    ok('T3 (B) delivery_update / 待提交区转入分支同样受限(含机制在场前置)');

    // ── T4 (E③) 重连 + 回放补放 → 仍为 CAP ─────────────────────
    const t4 = await page.evaluate(({ CAP, OVER }) => {
      resetConversation();
      for (let i = 0; i < 120; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `旧 ${i}`, delivery_id: `o-${i}`,
          turn_id: `ot-${i}`, cursor: `${String(i).padStart(4, '0')}` });
      }
      // 模拟断线重连:重新进入回放并补放 250 条
      beginTimelineReplay();
      for (let i = 0; i < CAP + OVER; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `补放 ${i}`, delivery_id: `n-${i}`,
          turn_id: `nt-${i}`, cursor: `1${String(i).padStart(4, '0')}` });
      }
      finishTimelineReplay(false);
      const tl = document.getElementById('timeline');
      return { count: tl.children.length, firstKey: tl.firstElementChild?.dataset.turnKey,
        lastKey: tl.lastElementChild?.dataset.turnKey, turnGroupsSize: turnGroups.size };
    }, { CAP, OVER });
    assert.strictEqual(t4.count, CAP, `E③: 重连补放后仍须 ${CAP},实为 ${t4.count}`);
    assert.strictEqual(t4.firstKey, `nt-${OVER}`);
    assert.strictEqual(t4.lastKey, `nt-${CAP + OVER - 1}`);
    assert.strictEqual(t4.turnGroupsSize, CAP);
    ok(`T4 (E③) 重连/回放补放后仍为 ${CAP}`);

    // ── T5 (E④ + A + C) 8K 字长回复 ────────────────────────────
    const t5 = await driveLongReply(page, LONG_REPLY_CHUNKS, CHUNK_TEXT);
    assert.strictEqual(t5.turnGroups, 1, `C: 8K 字长回复须只占 1 个回合卡,实为 ${t5.turnGroups}`);
    assert.strictEqual(t5.timelineChildren, 1, `C: 时间线不得按 chunk 拆卡,实为 ${t5.timelineChildren}`);
    const LONG_TOTAL = LONG_REPLY_CHUNKS * CHUNK_TEXT.length;
    assert.strictEqual(t5.fullChars, LONG_TOTAL,
      `C: 可取回的全文须完整保留 ${LONG_TOTAL} 字,实为 ${t5.fullChars}`);
    if (t5.folded) {
      // G 生效:内联只剩摘要,完整性由上面对 fullChars 的断言保证
      assert.ok(t5.assistantChars > 0 && t5.assistantChars < LONG_TOTAL,
        `G: 折叠后内联正文须短于全文(实为 ${t5.assistantChars}/${LONG_TOTAL})`);
    } else {
      assert.strictEqual(t5.assistantChars, LONG_TOTAL,
        `C: 未折叠时正文须完整内联 ${LONG_TOTAL} 字,实为 ${t5.assistantChars}`);
    }
    assert.ok(t5.mdCalls <= 20,
      `A: ${LONG_REPLY_CHUNKS} 个 chunk 的 markdown 重渲须被合并到极少数次,实为 ${t5.mdCalls}`);
    assert.ok(t5.maxBlockMs < 50,
      `E④: 单事件同步阻塞须 <50ms(页面持续可操作),实为 ${t5.maxBlockMs}ms`);
    assert.ok(t5.streamMs < 3000, `E④: 8K 字流式注入总耗时须 <3000ms,实为 ${t5.streamMs}ms`);
    ok(`T5 (E④/A/C) 8K 字=1 回合${t5.folded ? '(已折叠,内联 ' + t5.assistantChars + ' 字/全文 ' + t5.fullChars + ' 字)' : ''}` +
      ` · markdown 重渲 ${t5.mdCalls} 次 · 最长阻塞 ${t5.maxBlockMs}ms · 注入 ${t5.streamMs}ms`);

    // ── T5c (A) turn_completed 必须强制终渲,不得被节流窗口推迟 ──
    // 若无强制终渲,窗口内的尾部 chunk 与「✓ 回合完成」会压在队列里,用户看到半截答案。
    const t5c = await page.evaluate(() => {
      resetConversation();
      handleIncomingEvent({ kind: 'user_input', text: '问题', delivery_id: 'd-fin', turn_id: 't-fin', cursor: '0001' });
      // 紧接着在同一节流窗口内追加 chunk(此时应被挂起,不落 DOM)
      for (let i = 0; i < 5; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `尾段${i}`, delivery_id: 'd-fin',
          turn_id: 't-fin', cursor: `1000${i}` });
      }
      const beforeDone = {
        pending: window.__dwRender.pending(),
        text: document.querySelector('#timeline .turn-assistant .event-content')?.innerText || '',
      };
      handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.25 },
        delivery_id: 'd-fin', turn_id: 't-fin', cursor: '900000' });
      // 不 flush:终渲必须已同步完成
      const el = document.querySelector('#timeline .turn-assistant .event-content');
      const clone = el ? el.cloneNode(true) : null;
      if (clone) clone.querySelectorAll('.event-meta').forEach(n => n.remove());
      return { beforeDone, pendingAfter: window.__dwRender.pending(),
        text: clone ? clone.innerText : '',
        footer: document.querySelector('#timeline .turn-completed')?.innerText || '' };
    });
    assert.strictEqual(t5c.beforeDone.pending, 1, '前置:窗口内的后续 chunk 必须先被挂起(否则本格没测到节流)');
    assert.strictEqual(t5c.pendingAfter, 0, 'A: turn_completed 后待渲队列须已排空');
    assert.strictEqual(t5c.text, '尾段0尾段1尾段2尾段3尾段4', `A: turn_completed 须终渲全部尾段,实为「${t5c.text}」`);
    // 🔴 2026-09-11 业主:「这里只要给个完成标志即可,不用单写一行文字」⇒ 文案去掉,只留 ✓。
    // 本格的**测试意图不变**:终态必须同步落 DOM(证明终渲跑完了),只是可观测物从文案换成标志。
    // 这不是为让测试通过而放宽 —— 若连标志也没有,本断言照样红。
    assert.match(t5c.footer, /✓/, 'A: turn_completed 的终态标志须同步落 DOM');
    ok('T5c (A) turn_completed 强制终渲:尾段与终态文案同步落 DOM');

    // 长回复后页面仍可交互:真实控件可见可读
    const t0i = Date.now();
    assert.strictEqual(await page.locator('#humanInput').count(), 1);
    await page.locator('#timeline').evaluate(el => { el.scrollTop = 0; });
    const interactMs = Date.now() - t0i;
    assert.ok(interactMs < 1500, `E④: 长回复后 UI 往返须 <1500ms,实为 ${interactMs}ms`);
    ok(`T5b (E④) 长回复后 UI 仍可操作(往返 ${interactMs}ms)`);

    // ── T6 (E⑤ + D) 逐出后关联索引缓存无残留 ───────────────────
    // 6a 非 turn 分组的流式助手卡:messageNodes / messageBuffers / assistantEntries
    const t6a = await page.evaluate(({ CAP, OVER }) => {
      resetConversation();
      for (let i = 0; i < CAP + OVER; i += 1) {
        handleIncomingEvent({ kind: 'assistant_text', text: `游离 ${i}`, cursor: `${String(i).padStart(4, '0')}` });
      }
      if (window.__dwRender) window.__dwRender.flush();
      const evicted = [];
      for (let i = 0; i < OVER; i += 1) {
        const k = `cursor-${String(i).padStart(4, '0')}`;
        if (messageNodes.has(k) || messageBuffers.has(k) || assistantEntries.has(k)) evicted.push(k);
      }
      return { count: document.getElementById('timeline').children.length,
        nodes: messageNodes.size, buffers: messageBuffers.size, entries: assistantEntries.size,
        leaked: evicted, pending: window.__dwRender.pending() };
    }, { CAP, OVER });
    assert.strictEqual(t6a.count, CAP);
    assert.deepStrictEqual(t6a.leaked, [], `D: 被逐出的 ${OVER} 条仍留在索引里: ${t6a.leaked.join(',')}`);
    assert.strictEqual(t6a.nodes, CAP, `D: messageNodes 应为 ${CAP},实为 ${t6a.nodes}`);
    assert.strictEqual(t6a.buffers, CAP, `D: messageBuffers 应为 ${CAP},实为 ${t6a.buffers}`);
    assert.strictEqual(t6a.entries, CAP, `D: assistantEntries 应为 ${CAP},实为 ${t6a.entries}`);
    assert.strictEqual(t6a.pending, 0, 'D: 逐出后不得留下待渲队列');
    ok(`T6a (E⑤/D) messageNodes/messageBuffers/assistantEntries 随逐出收敛到 ${CAP}`);

    // 6b 输入卡:c1InputCards / c1PendingUpdates / c1DeliveryCards
    const t6b = await page.evaluate(({ CAP, OVER }) => {
      resetConversation();
      for (let i = 0; i < CAP + OVER; i += 1) {
        // stage 须已过注入点,否则 input_posted 落在待提交区而不进时间线(normalizeEvent 默认 generated)
        handleIncomingEvent({ kind: 'input_posted', text: `输入 ${i}`, message_id: `m-${i}`,
          delivery_id: `id-${i}`, stage: 'cli_injected', cursor: `${String(i).padStart(4, '0')}` });
      }
      const tl = document.getElementById('timeline');
      // c1DeliveryCards(deliveryId -> messageKey 反向索引)由 af20f28 引入;
      // 早于该锚的件没有这张表,本格对它的断言按在场与否自适应,不在缺表的锚上假红。
      const hasDlvIndex = typeof c1DeliveryCards !== 'undefined';
      const leaked = [];
      if (hasDlvIndex) {
        for (const [, mapped] of c1DeliveryCards) {
          if (!c1InputCards.has(mapped)) leaked.push(mapped);
        }
      }
      return { count: tl.children.length, cards: c1InputCards.size, pending: c1PendingUpdates.size,
        hasDlvIndex, dlv: hasDlvIndex ? c1DeliveryCards.size : null, danglingDeliveryRefs: leaked,
        waiting: document.querySelectorAll('#waitingList > .waiting-item').length,
        firstId: tl.firstElementChild?.dataset.messageId,
        evictedStillIndexed: c1InputCards.has('m-0') };
    }, { CAP, OVER });
    assert.strictEqual(t6b.waiting, 0, '前置:这批 input_posted 必须进时间线而非待提交区,否则本格测错对象');
    assert.strictEqual(t6b.count, CAP);
    assert.strictEqual(t6b.firstId, `m-${OVER}`, `B: 应只保留最新 ${CAP} 张输入卡`);
    assert.strictEqual(t6b.evictedStillIndexed, false, 'D: 被逐出的输入卡不得留在 c1InputCards');
    assert.strictEqual(t6b.cards, CAP, `D: c1InputCards 应为 ${CAP},实为 ${t6b.cards}`);
    if (t6b.hasDlvIndex) {
      assert.strictEqual(t6b.dlv, CAP, `D: c1DeliveryCards 应为 ${CAP},实为 ${t6b.dlv}`);
      assert.deepStrictEqual(t6b.danglingDeliveryRefs, [], 'D: c1DeliveryCards 不得留下指向已逐出卡的悬挂键');
    }
    ok(`T6b (E⑤/D) c1InputCards/c1PendingUpdates${t6b.hasDlvIndex ? '/c1DeliveryCards' : '(本锚无 c1DeliveryCards,该子断言跳过)'} 随逐出收敛到 ${CAP}`);

    // T6c:逐出路径清干净了,不代表重置路径也清干净 —— 两条路径各清各的索引,必须各测一次。
    // 本格由 T-043 误报的诊断过程顺带带出:当时为查别的问题读 resetConversation,
    // 发现它清了 c1InputCards/c1PendingUpdates 却漏了反向索引 c1DeliveryCards(实测残留 5 条)。
    const t6c = await page.evaluate(() => {
      resetConversation();
      for (let i = 0; i < 5; i += 1) {
        handleIncomingEvent({ kind: 'input_posted', text: `t${i}`, message_id: `rm-${i}`,
          delivery_id: `rid-${i}`, stage: 'cli_injected', cursor: `${i}` });
      }
      const hasDlvIndex = typeof c1DeliveryCards !== 'undefined';
      const before = { cards: c1InputCards.size, dlv: hasDlvIndex ? c1DeliveryCards.size : null };
      resetConversation();
      return { hasDlvIndex, before,
        after: { cards: c1InputCards.size, dlv: hasDlvIndex ? c1DeliveryCards.size : null } };
    });
    assert.strictEqual(t6c.before.cards, 5, '前置:这 5 张必须真的建了卡,否则本格测的是空集');
    assert.strictEqual(t6c.after.cards, 0, 'D: resetConversation 后 c1InputCards 应清空');
    if (t6c.hasDlvIndex) {
      assert.strictEqual(t6c.before.dlv, 5, '前置:反向索引必须真的建了 5 条');
      assert.strictEqual(t6c.after.dlv, 0, 'D: resetConversation 后 c1DeliveryCards 不得留下悬挂键');
    }
    ok(`T6c (D) resetConversation 同样清空输入卡与反向索引${t6c.hasDlvIndex ? '' : '(本锚无反向索引)'}`);

    // ── T8 (E′) 流式 delta 必须全量拼接;重连不得重复 ──────────
    // 🔴 2026-08-29 P0:我原先按 cursor 去重,而 Codex 的流式 delta **整个 item 共用一个 cursor**
    //(375 片全是 turn:<id>:item:0003),于是首片之后的 374 片被当成重放丢弃 ——
    // 页面只剩首片正文 + 一个假的「✓ 回合完成」。本格就是钉死这条:
    // 同 cursor 的大量不同 delta,一片都不许少、顺序不许乱。
    const t8 = await page.evaluate(() => {
      const N = 375;
      const SHARED = 'turn:t-delta:item:0003';   // 全部 delta 共用同一 cursor(真实形态)
      const deltas = [];
      for (let i = 0; i < N; i += 1) deltas.push(`第${String(i).padStart(3, '0')}片`);
      const expected = deltas.join('');
      const feed = () => {
        handleIncomingEvent({ kind: 'user_input', text: '请输出全文', delivery_id: 'd-delta',
          turn_id: 't-delta', stage: 'cli_injected', cursor: 'turn:t-delta:item:0001' });
        for (const d of deltas) {
          handleIncomingEvent({ kind: 'assistant_text', text: d, delivery_id: 'd-delta',
            turn_id: 't-delta', cursor: SHARED });
        }
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed' },
          delivery_id: 'd-delta', turn_id: 't-delta', cursor: 'turn:t-delta:item:0004' });
      };
      const readFull = () => {
        const grp = turnGroups.get('t-delta');
        return grp ? grp.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join('') : '';
      };
      const readDom = () => {
        const g = document.querySelector('#timeline > [data-turn-key="t-delta"]');
        const el = g && g.querySelector('.turn-assistant .event-content');
        if (!el) return '';
        const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n => n.remove());
        return c.innerText.replace(/\s+/g, '');
      };
      resetConversation();
      feed();
      window.__dwRender.flush();
      const first = { full: readFull(), dom: readDom(),
        completed: !!document.querySelector('#timeline > [data-turn-key="t-delta"] .turn-completed') };
      // 重连:hub 重放同一批 —— 走 markStreamReconnect(清空后由重放重建),不得叠加
      markStreamReconnect();
      feed();
      finishTimelineReplay(false);
      window.__dwRender.flush();
      const afterReconnect = { full: readFull(), dom: readDom() };
      return { N, expected, expectedLen: expected.length, first, afterReconnect };
    });
    assert.strictEqual(t8.first.full.length, t8.expectedLen,
      `E′: ${t8.N} 片同 cursor 的 delta 必须全部保留(应 ${t8.expectedLen} 字,实为 ${t8.first.full.length}) —— ` +
      '少一片就是去重把正文吞了');
    assert.strictEqual(t8.first.full, t8.expected, 'E′: delta 必须按序拼接,顺序不许乱');
    assert.strictEqual(t8.first.dom, t8.expected.replace(/\s+/g, ''),
      'E′: 拼接结果必须真的渲染进 DOM,不能只留在 group.events 里');
    assert.strictEqual(t8.first.completed, true, 'E′: turn_completed 须落地');
    assert.strictEqual(t8.afterReconnect.full.length, t8.expectedLen,
      `E′: 重连重放后正文不得翻倍(应仍 ${t8.expectedLen} 字,实为 ${t8.afterReconnect.full.length})`);
    assert.strictEqual(t8.afterReconnect.full, t8.expected, 'E′: 重连重建后正文须逐字等于原文');
    ok(`T8 (E′) ${t8.N} 片同 cursor delta 全量按序拼接(${t8.expectedLen} 字)· 重连重建后逐字相等、不翻倍`);

    // ── T9 (F) 单条渲染抛错不得连累同批其余条目 ────────────────
    // 旧写法下同批其余条目已被移出 pendingRenders,异常一冒泡它们就再没人渲染,
    // 表现为「页面从此不再更新、刷新也刷不出来」。
    const t9 = await page.evaluate(() => {
      resetConversation();
      const orig = window.renderMarkdown;
      window.renderMarkdown = function (src) {
        if (String(src).includes('§BOOM§')) throw new Error('人造渲染失败');
        return orig.apply(this, arguments);
      };
      try {
        // 两个回合落在同一个节流窗口内:先各建组(前沿渲染),再各追一条进 pending
        handleIncomingEvent({ kind: 'assistant_text', text: '开头A', delivery_id: 'd-a', turn_id: 't-a', cursor: 'a1' });
        handleIncomingEvent({ kind: 'assistant_text', text: '开头B', delivery_id: 'd-b', turn_id: 't-b', cursor: 'b1' });
        handleIncomingEvent({ kind: 'assistant_text', text: '§BOOM§', delivery_id: 'd-a', turn_id: 't-a', cursor: 'a2' });
        handleIncomingEvent({ kind: 'assistant_text', text: '后续B内容', delivery_id: 'd-b', turn_id: 't-b', cursor: 'b2' });
        const pendingBefore = window.__dwRender.pending();
        // flush 自身若把异常放跑,这里兜住并记下 —— 这样变异体也会走到下面那条具名断言,
        // 而不是以「异常逃逸」的形式红掉(红因必须就是格名承诺的那条)。
        let escaped = '';
        try { window.__dwRender.flush(); } catch (e) { escaped = String(e && e.message || e); }
        const gb = document.querySelector('#timeline > [data-turn-key="t-b"]');
        const el = gb && gb.querySelector('.turn-assistant .event-content');
        let txt = '';
        if (el) { const c = el.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n => n.remove()); txt = c.innerText; }
        return { pendingBefore, bText: txt, escaped, pendingAfter: window.__dwRender.pending() };
      } finally {
        window.renderMarkdown = orig;
      }
    });
    assert.strictEqual(t9.pendingBefore, 2, '前置:两个回合都须落在同一节流窗口的待渲队列里,否则本格空转');
    assert.match(t9.bText, /后续B内容/,
      `F: 同批另一回合必须照常渲染完(实为「${t9.bText}」,逃逸异常「${t9.escaped}」)` +
      ' —— 一条抛错不得让其余条目永久不渲染');
    assert.strictEqual(t9.escaped, '', `F: 渲染异常不得冲出 flush(逃逸了「${t9.escaped}」)`);
    ok('T9 (F) 单条渲染抛错被逐条兜住,同批其余条目照常渲染');

    // ── T10/T11/T12 (G) 超长正文文件化 ─────────────────────────
    // 不变量:内联 markdown 的输入被摘要长度封顶,与正文总长无关 ⇒ 渲染成本与长度解耦。
    const g = await page.evaluate(() => {
      const TH = window.__dwRender.longtextThreshold();
      const EX = window.__dwRender.longtextExcerpt();
      // 计数器:记录 renderMarkdown 每次拿到的输入长度
      const orig = window.renderMarkdown;
      window.__mdMax = 0; window.__mdCalls2 = 0;
      window.renderMarkdown = function (src) {
        window.__mdMax = Math.max(window.__mdMax, String(src == null ? '' : src).length);
        window.__mdCalls2 += 1;
        return orig.apply(this, arguments);
      };
      const readTurn = (key) => {
        const node = document.querySelector(`#timeline > [data-turn-key="${key}"]`);
        const content = node && node.querySelector('.turn-assistant .event-content');
        let shown = 0;
        if (content) { const c = content.cloneNode(true); c.querySelectorAll('.event-meta').forEach(n => n.remove()); shown = c.innerText.length; }
        return { hasBar: !!(node && node.querySelector('.longtext-bar')),
                 note: (node && node.querySelector('.longtext-note')?.innerText) || '',
                 shown, buttons: node ? node.querySelectorAll('.longtext-bar button[data-longtext]').length : 0 };
      };
      try {
        // ① 短正文:阈值以下,行为不变(全量内联、无入口条)
        resetConversation();
        const shortText = '短'.repeat(Math.floor(TH / 2));
        handleIncomingEvent({ kind: 'assistant_text', text: shortText, delivery_id: 'd-s', turn_id: 't-s', cursor: 's1' });
        window.__dwRender.flush();
        const shortR = readTurn('t-s');

        // ② 超长正文:3 倍阈值,分 60 个 chunk 流式送达
        resetConversation();
        window.__mdMax = 0;
        const unit = '契约正文压测甲乙丙丁';           // 10 字
        const totalChars = TH * 3;
        const per = Math.ceil(totalChars / 60 / unit.length) * unit.length;
        let sent = 0, i = 0;
        while (sent < totalChars) {
          const piece = unit.repeat(per / unit.length);
          handleIncomingEvent({ kind: 'assistant_text', text: piece, delivery_id: 'd-l', turn_id: 't-l', cursor: `l${String(i).padStart(4, '0')}` });
          sent += piece.length; i += 1;
        }
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed' }, delivery_id: 'd-l', turn_id: 't-l', cursor: 'l900000' });
        window.__dwRender.flush();
        const longR = readTurn('t-l');
        const mdMax = window.__mdMax;

        // ③ 全文可取回:桩掉剪贴板,点[复制全文]拿到的必须是完整正文
        let copied = null;
        const realClip = navigator.clipboard;
        Object.defineProperty(navigator, 'clipboard', {
          configurable: true, value: { writeText: async (t) => { copied = t; } },
        });
        // 折叠条不在场时不要去 click(null) —— 否则本格会以 TypeError 崩掉,
        // 红因就不是 T10 承诺的那条断言了(格名即承诺)。
        const copyBtn = document.querySelector('#timeline > [data-turn-key="t-l"] button[data-longtext="copy"]');
        if (copyBtn) copyBtn.click();
        return new Promise(res => setTimeout(() => {
          Object.defineProperty(navigator, 'clipboard', { configurable: true, value: realClip });
          res({ TH, EX, shortR, longR, mdMax, sentChars: sent, chunkLen: per,
                copiedLen: copied === null ? -1 : copied.length,
                copiedTail: copied === null ? '' : copied.slice(-10) });
        }, 50));
      } finally {
        window.renderMarkdown = orig;
      }
    });
    assert.strictEqual(g.shortR.hasBar, false, 'G: 阈值以下不得出现折叠条(短消息行为必须不变)');
    assert.ok(g.shortR.shown >= g.TH / 2 - 5,
      `G: 阈值以下须全量内联(应约 ${Math.floor(g.TH / 2)} 字,实为 ${g.shortR.shown})`);
    ok(`T12 (G) 阈值 ${g.TH} 以下行为不变:全量内联、无折叠条`);

    assert.strictEqual(g.longR.hasBar, true, 'G: 超长正文须出现折叠条');
    assert.strictEqual(g.longR.buttons, 3, 'G: 折叠条须有[打开全文]/[下载 .md]/[复制全文]三个入口');
    assert.match(g.longR.note, new RegExp(`全文 ${g.sentChars.toLocaleString()} 字`),
      `G: 折叠条须如实显示全文字数(应 ${g.sentChars.toLocaleString()},实为「${g.longR.note}」)`);
    assert.ok(g.longR.shown < g.TH,
      `G: 内联展示的正文须被摘要封顶(应 < ${g.TH},实为 ${g.longR.shown})`);
    ok(`T10 (G) ${g.sentChars} 字 → 内联仅 ${g.longR.shown} 字 + 三入口折叠条`);

    // 余量只给"一个 chunk":eventsTextPrefix 是累到超过阈值才停,故最多带出一个 chunk 的尾巴。
    // 早先写 +200 是拍脑袋的数,恰好把"摘要没截断"这类变异吃掉了 —— 余量必须由机制推出来。
    const MD_BOUND = g.EX + g.chunkLen;
    assert.ok(g.mdMax <= MD_BOUND,
      `G 不变量: 内联 markdown 的输入上界须≈摘要长度(应 ≤ ${MD_BOUND} = 摘要 ${g.EX} + 单 chunk ${g.chunkLen},` +
      `实为 ${g.mdMax}) —— 一旦超出就说明全文又被整段送进渲染,长度与成本没有解耦`);
    ok(`T11 (G 不变量) 正文 ${g.sentChars} 字,但 renderMarkdown 单次输入最大仅 ${g.mdMax} 字`);

    assert.strictEqual(g.copiedLen, g.sentChars,
      `G: [复制全文]必须取回完整正文(应 ${g.sentChars} 字,实为 ${g.copiedLen}) —— 折叠不得丢内容`);
    ok(`T10b (G) [复制全文]取回完整 ${g.copiedLen} 字,折叠不丢内容`);

    // ── T13 (H) markdown 逐前缀渲染:任何前缀都必须终止 ──────────
    // 长文本「卡死」的直接原因:renderMarkdownText 的外层 for 不自增,靠各分支推进 i;
    // 「### 」这种只有标记没有标题的行两头落空(标题分支要求标题非空;段落守卫又把它挡住),
    // 于是 i 原地不动 → 无限循环 → html 每轮追加空 <p></p> → 撑爆 V8 字符串上限。
    // 流式下 chunk 边界落在「### 」之后就会踩到,所以这一格按**逐前缀**来测,
    // 而不是只测完整文档 —— 只测完整文档恰好测不到这个缺陷。
    const DOC = [
      '# 契约标题', '', '前言段落,含 `行内代码` 与 **加粗**。', '',
      '## 一、范围', '', '| 项 | 值 |', '|---|---|', '| A | 1 |', '| B | 2 |', '',
      '### ① 前置', '', '- 条目一', '- 条目二', '1. 有序一', '2. 有序二', '',
      '> 引用行', '', '---', '', '### ② 交付物', '', '结尾段落。',
    ].join('\n');
    const prevTimeout = page.getDefaultTimeout ? undefined : undefined;
    page.setDefaultTimeout(20000);
    let t13;
    try {
      t13 = await page.evaluate((doc) => {
        const bad = [];
        // 逐字符前缀太多,按 1 字步进覆盖所有 chunk 边界(文档短,足够快)
        for (let n = 1; n <= doc.length; n += 1) {
          const s = doc.slice(0, n);
          const out = renderMarkdown(s);
          // 膨胀比失控 = 空 <p> 被反复追加的特征
          if (out.length > Math.max(400, s.length * 12)) bad.push({ n, outLen: out.length, tail: JSON.stringify(s.slice(-6)) });
          if (bad.length > 3) break;
        }
        // 显式点名那三种「有标记无标题」的行
        const named = {};
        for (const c of ['# ', '## ', '### ']) named[c] = renderMarkdown(c).length;
        return { bad, named, docLen: doc.length };
      }, DOC);
    } catch (err) {
      assert.fail('H: markdown 逐前缀渲染卡住了(evaluate 超时)—— ' +
        '几乎可以肯定是 renderMarkdownText 的外层循环没有前进:' + String(err).slice(0, 120));
    } finally {
      page.setDefaultTimeout(30000);
    }
    assert.deepStrictEqual(t13.bad, [],
      `H: 存在前缀使输出异常膨胀(空 <p> 被反复追加的特征): ${JSON.stringify(t13.bad)}`);
    for (const c of ['# ', '## ', '### ']) {
      assert.ok(t13.named[c] < 200,
        `H: 「${c}」(有标记无标题)必须立即渲染完且输出短小,实为 ${t13.named[c]} 字`);
    }
    ok(`T13 (H) 文档全部 ${t13.docLen} 个前缀逐一渲染无卡住;「# / ## / ### 」空标题行已终止`);

    // ── T7 (A 对照) 同一负载跑修复前的件 ───────────────────────
    if (BASELINE_FILE) {
      const { context: bctx, page: bpage } = await boot(browser, fs.readFileSync(BASELINE_FILE, 'utf-8'));
      const base = await driveLongReply(bpage, LONG_REPLY_CHUNKS, CHUNK_TEXT);
      await bctx.close();
      console.log(`  对照(修复前 ${BASELINE_FILE}): markdown 重渲 ${base.mdCalls} 次 · ` +
        `最长阻塞 ${base.maxBlockMs}ms · 注入 ${base.streamMs}ms · 回合卡 ${base.turnGroups}`);
      assert.ok(base.mdCalls >= LONG_REPLY_CHUNKS,
        `对照失真:修复前应每 chunk 重渲一次(≥${LONG_REPLY_CHUNKS}),实为 ${base.mdCalls} —— ` +
        '说明该度量没有测到目标行为,结论不可用');
      assert.ok(base.mdCalls / Math.max(t5.mdCalls, 1) >= 20,
        `A: 修复前后 markdown 重渲次数差距不足(前 ${base.mdCalls} / 后 ${t5.mdCalls})`);
      ok(`T7 (A 对照) 修复前 ${base.mdCalls} 次 → 修复后 ${t5.mdCalls} 次 · ` +
        `最长阻塞 ${base.maxBlockMs}ms → ${t5.maxBlockMs}ms`);
    } else {
      console.log('  SKIP  T7 (A 对照):未提供 BASELINE_FILE');
    }

    await context.close();
  } catch (err) {
    fail.push(err);
  } finally {
    await browser.close();
  }
  if (fail.length) { console.error('\nFAIL:', fail[0].message); process.exit(1); }
  console.log('\nrenderer-longtext-regression: ALL PASS');
})();
