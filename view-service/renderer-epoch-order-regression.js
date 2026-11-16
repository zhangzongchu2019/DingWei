// Hub-sequenced replay regression for late input admission inside an active turn.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

let source = fs.readFileSync(process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
const mutationAppend = process.argv.includes('--mutate-append');
const mutationNoSplit = process.argv.includes('--mutate-no-split');
const mutationDropEpoch = process.argv.includes('--mutate-drop-epoch');
const roundsArg = process.argv.find(arg => arg.startsWith('--rounds='));
const rounds = Math.max(1, Math.min(20, Number(roundsArg?.split('=')[1]) || 1));
const BASE = 1793820000000;
let activeMode = 'claude'; let activeN = 1;

function row(seq, event, mode, n) {
  return { cursor: { generation: `timeline-${mode}`, seq }, event_id: `timeline-${mode}-${n}-${seq}`,
    received_at: new Date(BASE + seq).toISOString(), event: JSON.stringify(event) };
}
function events(mode, n) {
  const turn = `timeline-turn-${mode}-${n}`;
  const delivery = `timeline-delivery-${mode}-${n}`;
  const label = mode.toUpperCase();
  const user = { kind: 'input_posted', schema_version: 'delivery.v2', message_id: `timeline-msg-${n}`,
    delivery_id: delivery, payload: `TIMELINE_USER_${mode}_${n}`, stage: 'helper_queued', updated_at: new Date(BASE + 2000).toISOString(),
    source: { kind: 'agent', trust_decision: 'trusted' }, delivery: { role: 'primary' } };
  const accepted = { kind: 'delivery_update', schema_version: 'delivery.v2', message_id: `timeline-msg-${n}`,
    observed_delivery_id: delivery, delivery: { role: 'primary' }, delivery_stage: 'cli_injected', turn_id: turn,
    ack_protocol: 2, processing_mode: 'driver', ack_generation: 1,
    queued_at: new Date(BASE + 1500).toISOString(), occurred_at: new Date(BASE + 3500).toISOString(),
    updated_at: new Date(BASE + 4000).toISOString(), reason: '', cancellable: true, interruptible: false };
  const completed = { kind: 'state_change', data: { change: 'turn_completed' }, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 6000 };
  let list;
  if (mode === 'claude' || mode === 'live') {
    list = [
      { kind: 'assistant_text', text: `TIMELINE_EARLY_${label}_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 1000 },
      user,
      { kind: 'assistant_text', text: `TIMELINE_INTERLEAVED_${label}_${n}`, delivery_id: `other-${n}`, turn_id: `other-turn-${n}`, ts: BASE + 2500 },
      { kind: 'assistant_text', text: `TIMELINE_REPLY_${label}_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 5000 },
      accepted,
      completed,
    ];
  } else if (mode === 'codex') {
    const item = `item-codex-${n}`;
    list = [
      { kind: 'assistant_text', text: `TIMELINE_EARLY_`, data: { item_id: item }, cursor: `turn:${turn}:item:0001`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 1000 },
      { kind: 'assistant_text', text: `CODEX_${n}`, data: { item_id: item }, cursor: `turn:${turn}:item:0001`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 1100 },
      user,
      { kind: 'assistant_text', text: `TIMELINE_INTERLEAVED_CODEX_${n}`, delivery_id: `other-${n}`, turn_id: `other-turn-${n}`, ts: BASE + 2500 },
      { kind: 'assistant_text', text: `TIMELINE_REPLY_`, data: { item_id: item }, cursor: `turn:${turn}:item:0001`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 5000 },
      { kind: 'assistant_text', text: `CODEX_${n}`, data: { item_id: item }, cursor: `turn:${turn}:item:0001`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 5100 },
      accepted,
      completed,
    ];
  } else if (mode === 'after-end') {
    list = [
      { kind: 'assistant_text', text: `TIMELINE_EARLY_AFTER_END_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 5000 },
      completed, user, accepted,
    ];
  } else if (mode === 'history-page') {
    list = [
      { kind: 'assistant_text', text: `TIMELINE_EARLY_HISTORY_PAGE_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 1000 },
      user,
      { kind: 'assistant_text', text: `TIMELINE_INTERLEAVED_HISTORY_PAGE_${n}`, delivery_id: `other-${n}`, turn_id: `other-turn-${n}`, ts: BASE + 2500 },
      { kind: 'assistant_text', text: `TIMELINE_REPLY_HISTORY_PAGE_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 5000 },
      accepted, completed,
    ];
  } else { // idle control: no active turn to split
    list = [
      { kind: 'assistant_text', text: `TIMELINE_IDLE_REPLY_${n}`, delivery_id: `reply-${n}`, turn_id: turn, ts: BASE + 1000 },
      completed, user, { ...accepted, turn_id: '' },
    ];
  }
  return list.map((event, i) => {
    if (event.kind === 'assistant_text') {
      event.session_id = `fixture-session-${mode}`;
      if (mode === 'codex') {
        const itemId = event.data?.item_id;
        if (itemId) event.data = { raw_method: 'item/agentMessage/delta', params: {
          turnId: event.turn_id, itemId, delta: event.text }, ...event.data };
        else event.data ||= {};
      } else event.data ||= {};
      if (!Object.prototype.hasOwnProperty.call(event, 'cursor')) event.cursor = null;
    }
    return row(i + 1, event, mode, n);
  });
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  const mode = url.searchParams.get('mode') || 'claude';
  const n = Number(url.searchParams.get('n')) || 1;
  if (url.pathname === '/view/timeline') {
    activeMode = mode; activeN = n;
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); res.end(source); return;
  }
  if (url.pathname === '/view/timeline/history') {
    const allRows = events(activeMode, activeN);
    const before = Number(url.searchParams.get('before')) || 0;
    if (activeMode === 'history-epoch') {
      const generation = before ? 'gen-after-restart' : 'gen-before-restart';
      const seq = before ? 1 : 9;
      const marker = before ? 'HISTORY_AFTER_RESTART' : 'HISTORY_BEFORE_RESTART';
      const chronologicalTs = BASE + (before ? 9 : 1);
      const event = { kind: 'assistant_text', text: marker, delivery_id: marker, turn_id: `history-generation-${generation}-turn`,
        session_id: 'history-epoch-session', ts: chronologicalTs, data: {}, cursor: null };
      const rows = [{ cursor: { generation, seq }, event_id: `${generation}-${seq}`,
        received_at: new Date(chronologicalTs).toISOString(), event: JSON.stringify(event) }];
      res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
      res.end(JSON.stringify({ generation, high_water: seq, events: rows, has_older: !before, before_seq: seq })); return;
    }
    const rows = activeMode === 'live' ? [] : activeMode === 'history-page'
      ? (before ? allRows.filter(item => item.cursor.seq < before) : allRows.filter(item => item.cursor.seq >= 4 && item.cursor.seq <= 6))
      : allRows;
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify({ generation: `timeline-${activeMode}`, high_water: allRows.length, events: rows,
      has_older: activeMode === 'history-page' && !before })); return;
  }
  if (url.pathname === '/view/timeline/status') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ session: 'timeline', state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'owner', model: 'm', cost_total: 0, cost_last: 0, turns: 1 })); return;
  }
  if (url.pathname === '/view/timeline/tasks' || url.pathname === '/view/timeline/page') {
    res.writeHead(200, { 'content-type': 'application/json' }); res.end('{}'); return;
  }
  if (url.pathname === '/view/timeline/events') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
    res.write(': connected\n\n');
    if (activeMode === 'live') {
      const rows = events('live', activeN);
      let index = 0;
      const timer = setInterval(() => {
        if (res.destroyed) { clearInterval(timer); return; }
        if (index >= rows.length) { clearInterval(timer);
          const ev = { kind: 'assistant_text', text: `AFTER_HUB_RESTART_${activeN}`, delivery_id: `restart-${activeN}`, turn_id: `timeline-turn-live-${activeN}`, ts: BASE + 9000 };
          setTimeout(() => res.write(`id: b52-1\ndata: ${JSON.stringify(ev)}\n\n`), 300); return; }
        const item = rows[index++];
        res.write(`id: a51-${item.cursor.seq}\ndata: ${item.event}\n\n`);
      }, 8);
      req.on('close', () => clearInterval(timer));
    } else req.on('close', () => {});
    return;
  }
  res.writeHead(404); res.end('not found');
});

const assertion = 'timeline order invalid: late accepted user card must be between earlier and later same-turn output';
async function check(page, mode, n, reload = false) {
  if (reload) await page.reload({ waitUntil: 'domcontentloaded' });
  else await page.goto(`http://127.0.0.1:${server.address().port}/view/timeline?mode=${mode}&n=${n}`, { waitUntil: 'domcontentloaded' });
  const userText = `TIMELINE_USER_${mode}_${n}`;
  const modeLabel = mode.toUpperCase().replaceAll('-', '_');
  const earlyText = mode === 'codex' ? `TIMELINE_EARLY_CODEX_${n}` : mode === 'after-end'
    ? `TIMELINE_EARLY_AFTER_END_${n}` : `TIMELINE_EARLY_${modeLabel}_${n}`;
  const replyText = mode === 'codex' ? `TIMELINE_REPLY_CODEX_${n}` : mode === 'after-end'
    ? `TIMELINE_EARLY_AFTER_END_${n}` : `TIMELINE_REPLY_${modeLabel}_${n}`;
  if (mode === 'history-page') {
    await page.waitForFunction(() => !document.getElementById('historyOlder').disabled, null, { timeout: 10000 });
    await page.locator('#historyOlder').click();
    await page.getByText(userText, { exact: false }).waitFor({ timeout: 10000 });
  } else {
    try { await page.getByText(userText, { exact: false }).waitFor({ timeout: 10000 }); }
    catch (error) { console.error(`fixture state mode=${mode} body=${(await page.locator('#timeline').innerText()).slice(0, 1000)} waiting=${await page.locator('#waitingList').innerText().catch(() => '')}`); throw error; }
  }
  if (mode !== 'idle') {
    try { await page.getByText(replyText, { exact: false }).waitFor({ timeout: 10000 }); }
    catch (error) { console.error(`reply missing mode=${mode} timeline=${(await page.locator('#timeline').innerText()).slice(0, 1000)} waiting=${await page.locator('#waitingList').innerText().catch(() => '')}`); throw error; }
  }
  else await page.getByText(`TIMELINE_IDLE_REPLY_${n}`, { exact: false }).waitFor({ timeout: 10000 });
  await page.waitForFunction(() => pendingRenders.size === 0 && c1MergeBuf.size === 0, null, { timeout: 10000 });
  const order = await page.locator('#timeline').evaluate((timeline, texts) => {
    const children = [...timeline.children];
    const index = text => children.findIndex(node => (node.innerText || '').includes(text));
    return { user: index(texts.user), early: index(texts.early), interleaved: index(texts.interleaved), reply: index(texts.reply),
      children: children.map(node => (node.innerText || '').slice(0, 100)) };
  }, { user: userText, early: earlyText, interleaved: `TIMELINE_INTERLEAVED_${modeLabel}_${n}`,
    reply: mode === 'idle' ? `TIMELINE_IDLE_REPLY_${n}` : replyText });
  if (process.env.TIMELINE_DEBUG) console.error(JSON.stringify({ mode, order }));
  assert.ok(order.user >= 0, `${assertion}; missing user card mode=${mode} children=${JSON.stringify(order.children)}`);
  if (mode === 'idle' || mode === 'after-end') assert.ok(order.reply < order.user,
    `${assertion}; late-after-end control mode=${mode} reply_index=${order.reply} user_index=${order.user} children=${JSON.stringify(order.children)}`);
  else assert.ok(order.early >= 0 && order.early < order.user && order.user < order.interleaved && order.interleaved < order.reply,
    `${assertion}; mode=${mode} early_index=${order.early} user_index=${order.user} reply_index=${order.reply} children=${JSON.stringify(order.children)}`);
  return order;
}

async function benchmarkHistoryInsert(page) {
  await page.goto(`http://127.0.0.1:${server.address().port}/view/timeline?mode=idle&n=bench`, { waitUntil: 'domcontentloaded' });
  return page.evaluate(() => {
    const host = document.getElementById('timeline');
    host.replaceChildren();
    for (let i = 1; i <= 2000; i++) {
      const node = document.createElement('article');
      node.dataset.timelineSeq = String(i * 2);
      host.appendChild(node);
    }
    const start = performance.now();
    for (let i = 1; i <= 100; i++) {
      const node = document.createElement('article');
      node.dataset.timelineSeq = String(i * 2 + 1);
      placeTimelineNode(node);
    }
    return { elapsedMs: performance.now() - start, children: host.children.length };
  });
}

async function main() {
  if (mutationDropEpoch) {
    const anchor = '            setTimelineEventEpoch(payload, `hub:${eventCursor.epoch}`);\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'epoch mutation anchor must be unique');
    source = source.replace(anchor, '            // mutant: omit epoch from the timeline key\n');
  }
  if (mutationAppend || mutationNoSplit) {
    const anchor = mutationAppend
      ? '    function placeTimelineNode(node, fallbackBefore = null) {\n'
      : '            splitTurnGroupAtHubSequence(norm.turnId, Number(wEntry.event?._hubSeq));\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'mutation anchor must be unique');
    source = mutationAppend
      ? source.replace(anchor, `${anchor}      timeline.appendChild(node); // mutant: append instead of sequence positioning\n      return;\n`)
      : source.replace(anchor, `            // mutant: no turn-group split at the injection fence\n`);
  }
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {});
  try {
    for (let i = 1; i <= rounds; i++) {
      const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      for (const mode of ['claude', 'codex']) {
        await check(page, mode, i);
        await check(page, mode, i, true);
      }
      await check(page, 'live', i);
      await page.getByText(`AFTER_HUB_RESTART_${i}`, { exact: false }).waitFor({ timeout: 10000 });
      const ord = await page.locator('#timeline').evaluate(t => { const c=[...t.children]; return { restart: c.findIndex(n => (n.innerText||'').includes('AFTER_HUB_RESTART')), last: c.length-1, kids: c.map(n => (n.innerText||'').replace(/\s+/g,' ').slice(0,50)) }; });
      assert.ok(ord.restart === ord.last, `epoch order invalid: post-restart event must be last; ${JSON.stringify(ord)}`);
      console.log('EPOCH_CHECK_PASS');
      await page.goto(`http://127.0.0.1:${server.address().port}/view/timeline?mode=history-epoch&n=${i}`, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => document.getElementById('timeline').innerText.includes('HISTORY_BEFORE_RESTART'), null, { timeout: 10000 });
      await page.waitForFunction(() => !document.getElementById('historyOlder').disabled, null, { timeout: 10000 });
      await page.locator('#historyOlder').click();
      await page.waitForFunction(() => document.getElementById('timeline').innerText.includes('HISTORY_AFTER_RESTART'), null, { timeout: 10000 });
      const historyOrder = await page.locator('#timeline').evaluate(t => {
        const children = [...t.children];
        return { older: children.findIndex(n => (n.innerText || '').includes('HISTORY_BEFORE_RESTART')),
          newer: children.findIndex(n => (n.innerText || '').includes('HISTORY_AFTER_RESTART')) };
      });
      assert.ok(historyOrder.older >= 0 && historyOrder.older < historyOrder.newer,
        `history generation order invalid: ${JSON.stringify(historyOrder)}`);
      console.log('HISTORY_GENERATION_CHECK_PASS');
      return;
      await check(page, 'history-page', i);
      await check(page, 'after-end', i);
      await check(page, 'idle', i);
      await page.close();
    }
    const benchPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    const bench = await benchmarkHistoryInsert(benchPage);
    await benchPage.close();
    console.log(`PASS rounds=${rounds} realtime=${rounds} claude_active=${rounds * 2} codex_active=${rounds * 2} reload_replay=${rounds * 2} history_older_page=${rounds} late_after_end=${rounds} idle_order=${rounds} benchmark_2000_plus_100_ms=${bench.elapsedMs.toFixed(2)} benchmark_children=${bench.children} renderer_sha=${require('crypto').createHash('sha256').update(source).digest('hex')}`);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
}

main().catch(error => { console.error(error && error.stack || error); process.exitCode = 1; try { server.close(); } catch (_) {} });
