// Hub-sequenced replay regression for late input admission inside an active turn.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

let source = fs.readFileSync(process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
const mutationAppend = process.argv.includes('--mutate-append');
const mutationNoSplit = process.argv.includes('--mutate-no-split');
const mutationLatestOrder = process.argv.includes('--mutate-latest-order');
const latestReloadOnly = process.argv.includes('--latest-reload-only');
const mutationLatestTimestamp = process.argv.includes('--mutate-latest-timestamp');
const mutationCrossStreamTime = process.argv.includes('--mutate-cross-stream-time');
const mutationNoCrossEpochPair = process.argv.includes('--mutate-no-cross-epoch-pair');
const mutationWrongCrossEpochSegment = process.argv.includes('--mutate-wrong-cross-epoch-segment');
const mutationCrossEpochReposition = process.argv.includes('--mutate-cross-epoch-reposition');
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
  if (mode === 'latest-input') {
    list = [
      { kind: 'assistant_text', text: `TIMELINE_OLD_OUTPUT_${n}`, delivery_id: `old-reply-${n}`,
        turn_id: `old-turn-${n}`, ts: BASE + 1000, data: {} },
    ];
  } else if (mode === 'claude' || mode === 'live') {
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
      completed, { ...user, updated_at: new Date(BASE + 7000).toISOString() }, accepted,
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
    if (activeMode === 'latest-input') {
      res.write('id: abcdef0123456789-1\ndata: {"kind":"state_change","data":{"change":"synthetic_epoch_seed"}}\n\n');
    }
    if (activeMode === 'live') {
      const rows = events('live', activeN);
      let index = 0;
      const timer = setInterval(() => {
        if (index >= rows.length || res.destroyed) { clearInterval(timer); return; }
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
    timelineSequencedNodes.length = 0;
    for (let i = 1; i <= 2000; i++) {
      const node = document.createElement('article');
      node.dataset.timelineSeq = String(i * 2);
      node.dataset.timelineEpochOrder = '0';
      host.appendChild(node);
      timelineSequencedNodes.push(node);
    }
    // Unsequenced notices are free to sit between sequenced cards. The binary
    // search must use the maintained sequenced-node index, not all children.
    const notice = document.createElement('div');
    notice.className = 'event-card';
    notice.textContent = 'UNSEQUENCED_GAP_NOTICE';
    host.insertBefore(notice, timelineSequencedNodes[1001]);
    const start = performance.now();
    for (let i = 1; i <= 100; i++) {
      const node = document.createElement('article');
      node.dataset.timelineSeq = String(i * 2 + 1);
      placeTimelineNode(node);
    }
    const ordered = timelineSequencedNodes.every((node, index, nodes) => index === 0
      || compareTimelineKey(Number(nodes[index - 1].dataset.timelineEpochOrder) || 0,
        timelineSequence(nodes[index - 1]), Number(node.dataset.timelineEpochOrder) || 0,
        timelineSequence(node)) <= 0);
    const noticeAnchored = Number(notice.previousElementSibling?.dataset?.timelineSeq) === 2002
      && Number(notice.nextElementSibling?.dataset?.timelineSeq) === 2004;
    return { elapsedMs: performance.now() - start, children: host.children.length,
      unsequencedNoticeStable: notice.parentNode === host && ordered && noticeAnchored };
  });
}

async function checkCrossStreamTimeline(page) {
  const result = await page.evaluate(() => {
    const host = document.getElementById('timeline');
    host.replaceChildren();
    timelineSequencedNodes.length = 0;
    const expected = [];
    const add = (kind, label, ts) => {
      const node = document.createElement('article');
      node.className = kind === 'turn' ? 'turn-group' : 'message-card user';
      node.dataset.regressionLabel = label;
      const time = document.createElement('span');
      time.className = 'event-time';
      time.dataset.ts = String(ts);
      node.appendChild(time);
      expected.push({ label, ts, kind });
      placeTimelineNode(node);
    };
    // Seven individually named inputs have no Hub seq. Each must land between
    // the neighboring turn groups on the same event-time axis.
    for (let i = 1; i <= 8; i++) add('turn', `S_VIEW_TURN_${String(i).padStart(2, '0')}`, i * 1000);
    for (let i = 1; i <= 7; i++) add('input', `S_VIEW_INPUT_${i}`, i * 1000 + 500);
    const nodes = [...host.children].map(node => ({
      label: node.dataset.regressionLabel,
      ts: Number(node.querySelector('.event-time[data-ts]')?.dataset.ts),
      kind: node.classList.contains('turn-group') ? 'turn' : 'input',
    }));
    const eachInputBetweenTurns = Array.from({ length: 7 }, (_, n) => {
      const i = n + 1;
      const at = nodes.findIndex(node => node.label === `S_VIEW_INPUT_${i}`);
      return at > 0 && at < nodes.length - 1
        && nodes[at - 1].label === `S_VIEW_TURN_${String(i).padStart(2, '0')}`
        && nodes[at + 1].label === `S_VIEW_TURN_${String(i + 1).padStart(2, '0')}`;
    });
    return { nodes, monotonic: nodes.every((node, i) => i === 0 || nodes[i - 1].ts <= node.ts), eachInputBetweenTurns };
  });
  assert(result.monotonic, `whole timeline event-time must be monotonic: ${JSON.stringify(result.nodes)}`);
  for (let i = 0; i < 7; i++) assert.strictEqual(result.eachInputBetweenTurns[i], true,
    `S_VIEW_INPUT_${i + 1} must sit between its adjacent turn groups: ${JSON.stringify(result.nodes)}`);
  const sameEpoch = await page.evaluate(() => {
    const host = document.getElementById('timeline');
    host.replaceChildren();
    timelineSequencedNodes.length = 0;
    const add = (seq, ts) => {
      const node = document.createElement('article');
      node.dataset.timelineEpochOrder = '77';
      node.dataset.timelineSeq = String(seq);
      const time = document.createElement('span');
      time.className = 'event-time';
      time.dataset.ts = String(ts);
      node.appendChild(time);
      placeTimelineNode(node);
    };
    add(2, 1000);
    add(1, 9000);
    return [...host.children].map(node => Number(node.dataset.timelineSeq));
  });
  assert.deepStrictEqual(sameEpoch, [1, 2], `same-epoch sequence must override clock skew: ${JSON.stringify(sameEpoch)}`);
  return result;
}

async function checkLatestInput(page, n, afterReload, verify = true) {
  const url = `http://127.0.0.1:${server.address().port}/view/timeline?mode=latest-input&n=${n}`;
  if (afterReload) await page.reload({ waitUntil: 'domcontentloaded' });
  else await page.goto(url, { waitUntil: 'domcontentloaded' });
  const oldText = `TIMELINE_OLD_OUTPUT_${n}`;
  await page.getByText(oldText, { exact: false }).waitFor({ timeout: 10000 });
  await page.waitForFunction(() => historyBootstrapPending === false, null, { timeout: 10000 });
  const turnId = `latest-turn-${n}-${afterReload ? 'reload' : 'first'}`;
  const inputText = `TIMELINE_LATEST_INPUT_${n}_${afterReload ? 'RELOAD' : 'FIRST'}`;
  const outputText = `TIMELINE_LATEST_OUTPUT_${n}_${afterReload ? 'RELOAD' : 'FIRST'}`;
  await page.evaluate(({ turnId, inputText, outputText, n, afterReload }) => {
    const epoch = 'abcdef0123456789';
    const input = { kind: 'user_input', text: inputText, turn_id: turnId, delivery_id: turnId,
      data: { lifecycle: 'admitted' }, _hubSeq: 10, _hubEpoch: epoch };
    const output = { kind: 'assistant_text', text: outputText, turn_id: turnId, delivery_id: turnId,
      ts: Date.now() + 1000, _hubSeq: 11, _hubEpoch: epoch, data: {} };
    setTimelineEventEpoch(input, `hub:${epoch}`);
    setTimelineEventEpoch(output, `hub:${epoch}`);
    handleIncomingEvent(input);
    handleIncomingEvent(output);
  }, { turnId, inputText, outputText, n, afterReload });
  await page.getByText(outputText, { exact: false }).waitFor({ timeout: 10000 });
  await page.waitForFunction(() => pendingRenders.size === 0, null, { timeout: 10000 });
  const state = await page.evaluate(({ oldText, inputText, outputText, turnId }) => {
    const groups = [...document.querySelectorAll('#timeline > .turn-group')];
    const oldIndex = groups.findIndex(group => group.innerText.includes(oldText));
    const next = groups.find(group => group.dataset.turnKey === turnId);
    const latestIndex = groups.indexOf(next);
    const sections = next ? [...next.querySelectorAll('.turn-user,.turn-assistant')] : [];
    const inputTs = Number(next?.querySelector('.turn-user .event-time')?.dataset.ts);
    const outputTs = Number(next?.querySelector('.turn-assistant .event-time')?.dataset.ts);
    return { oldIndex, latestIndex, groupCount: groups.length,
      segmentKinds: sections.map(section => section.classList.contains('turn-user') ? 'user' : 'assistant'),
      inputFound: !!next?.innerText.includes(inputText), outputFound: !!next?.innerText.includes(outputText), inputTs, outputTs };
  }, { oldText, inputText, outputText, turnId });
  const cell = afterReload ? '③ refresh then new input' : '② history then live new input';
  if (verify) {
    assert(state.oldIndex >= 0 && state.latestIndex === state.groupCount - 1,
      `${cell}: new turn must follow old history output; state=${JSON.stringify(state)}`);
    assert.deepStrictEqual(state.segmentKinds, ['user', 'assistant'],
      `${cell}: same new turn must keep input before output; state=${JSON.stringify(state)}`);
    assert(state.inputFound && state.outputFound, `${cell}: both segments must render; state=${JSON.stringify(state)}`);
    assert(Number.isFinite(state.inputTs) && Number.isFinite(state.outputTs) && state.inputTs <= state.outputTs,
      `${cell}: input fallback time must be present and no later than output; state=${JSON.stringify(state)}`);
  }
  return state;
}

async function checkCrossEpochTurnPairing(page) {
  await page.evaluate(() => {
    const host = document.getElementById('timeline');
    host.replaceChildren();
    timelineSequencedNodes.length = 0;
    turnGroups.clear(); turnGroupSegments.clear();
    const turn = 'cross-epoch-turn';
    const add = (seq, ts, text, epoch) => {
      const event = { kind: 'assistant_text', text, turn_id: turn, delivery_id: turn, ts, _hubSeq: seq, data: {} };
      setTimelineEventEpoch(event, `hub:${epoch}`);
      renderTurnEvent(turn, event);
    };
    // The captured failure has a lower-ranked live epoch than the history
    // segment it pairs into, so its seq key would sort behind the queued card.
    timelineEpochRank('hub:live');
    add(1, 1000, 'CROSS_EPOCH_EARLY', 'history');
    add(20, 9000, 'CROSS_EPOCH_LATE', 'history');
    splitTurnGroupAtHubSequence(turn, 10, timelineEventRank({ _timelineEpoch: 'hub:history' }));
    const queued = document.createElement('article');
    queued.className = 'event-card';
    queued.dataset.timelineTs = '8900';
    queued.dataset.timelineEpochOrder = String(timelineEpochRank('hub:live'));
    queued.dataset.timelineSeq = '9';
    const queuedTime = document.createElement('span');
    queuedTime.className = 'event-time';
    queuedTime.dataset.ts = '8900';
    queued.appendChild(queuedTime);
    placeTimelineNode(queued);
    add(10, 8800, 'CROSS_EPOCH_LIVE', 'live');
  });
  await page.waitForTimeout(100);
  const state = await page.evaluate(() => {
    const host = document.getElementById('timeline');
    const turn = 'cross-epoch-turn';
    const groups = turnGroupSegments.get(turn) || [];
    const queued = host.querySelector('.event-card');
    const nodes = Array.from(host.children);
    const liveGroup = groups.find(group => group.events.some(event => event.text === 'CROSS_EPOCH_LIVE'));
    const timestamps = nodes.map(timelineTimestamp);
    return { groupCount: groups.length,
      texts: groups.map(group => group.events.map(event => event.text).join(' ')),
      liveGroup: groups.indexOf(liveGroup),
      liveBeforeQueued: Boolean(liveGroup && nodes.indexOf(liveGroup.node) < nodes.indexOf(queued)),
      timestamps,
      monotonic: timestamps.every((ts, i) => !Number.isFinite(ts) || i === 0 || !Number.isFinite(timestamps[i - 1]) || timestamps[i - 1] <= ts) };
  });
  assert.strictEqual(state.groupCount, 2, `cross-epoch event must not create a third turn segment: ${JSON.stringify(state)}`);
  assert.strictEqual(state.liveGroup, 1, `new event must join the time-nearest existing segment: ${JSON.stringify(state)}`);
  assert(state.texts[1].includes('CROSS_EPOCH_LATE'), `paired segment must retain existing events: ${JSON.stringify(state)}`);
  assert(state.liveBeforeQueued, `paired group must stay before the later queued card: ${JSON.stringify(state)}`);
  assert(state.monotonic, `whole timeline timestamps must remain monotonic: ${JSON.stringify(state)}`);
  return state;
}

async function main() {
  if (mutationAppend || mutationNoSplit) {
    const anchor = mutationAppend
      ? '    function placeTimelineNode(node, fallbackBefore = null) {\n'
      : '            splitTurnGroupAtHubSequence(norm.turnId, Number(wEntry.event?._hubSeq), timelineEventRank(wEntry.event));\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'mutation anchor must be unique');
    source = mutationAppend
      ? source.replace(anchor, `${anchor}      timeline.appendChild(node); // mutant: append instead of sequence positioning\n      return;\n`)
      : source.replace(anchor, `            // mutant: no turn-group split at the injection fence\n`);
  }
  if (mutationNoCrossEpochPair) {
    const anchor = '        if (!group && segments.length) {\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'cross-epoch pairing mutation anchor must be unique');
    source = source.replace(anchor, '        if (false && !group && segments.length) { // mutant: never pair across epochs\n');
  }
  if (mutationWrongCrossEpochSegment) {
    const anchor = 'Math.abs(segmentTs - eventTs) < Math.abs(timelineTimestamp(nearest.node) - eventTs)';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'cross-epoch segment mutation anchor must be unique');
    source = source.replace(anchor, 'false /* mutant: keep earliest segment, ignoring time */');
  }
  if (mutationCrossEpochReposition) {
    const anchor = '        if (rank === group.timelineEpochOrder\n            && compareTimelineKey(rank, seq, group.timelineEpochOrder, group.timelineSeq) < 0) {';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'cross-epoch reposition mutation anchor must be unique');
    source = source.replace(anchor, '        if (compareTimelineKey(rank, seq, group.timelineEpochOrder, group.timelineSeq) < 0) { // mutant: let foreign epoch rewrite group key');
  }
  if (mutationLatestTimestamp) {
    const anchor = "      if (ev?.kind === 'user_input' && !Number.isFinite(ev.ts)) ev.ts = Date.now();\n";
    assert.strictEqual(source.split(anchor).length - 1, 1, 'input arrival timestamp fallback anchor must be unique');
    source = source.replace(anchor, '      // mutant: omit input arrival timestamp fallback\n');
  }
  if (mutationLatestOrder) {
    const anchor = '      if (Number.isFinite(aTs) && Number.isFinite(bTs) && aTs !== bTs) return aTs - bTs;\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'cross-epoch timestamp-order anchor must be unique');
    source = source.replace(anchor, '      // mutant: use epoch rank before event time\n');
  }
  if (mutationCrossStreamTime) {
    const anchor = '      if (Number.isFinite(aTs) && Number.isFinite(bTs) && aTs !== bTs) return aTs - bTs;\n';
    assert.strictEqual(source.split(anchor).length - 1, 1, 'cross-stream event-time comparator anchor must be unique');
    source = source.replace(anchor, '      // mutant: omit cross-stream event-time ordering\n');
  }
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {});
  try {
    for (let i = 1; i <= rounds; i++) {
      const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      await page.goto(`http://127.0.0.1:${server.address().port}/view/timeline?mode=idle&n=${i}`, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => !!document.getElementById('timeline'), null, { timeout: 10000 });
      const crossStream = await checkCrossStreamTimeline(page);
      console.log(`CROSS_STREAM_TIMELINE ${JSON.stringify(crossStream)}`);
      const crossEpoch = await checkCrossEpochTurnPairing(page);
      console.log(`CROSS_EPOCH_TURN_PAIRING ${JSON.stringify(crossEpoch)}`);
      for (const mode of ['claude', 'codex']) {
        await check(page, mode, i);
        await check(page, mode, i, true);
      }
      await check(page, 'live', i);
      await check(page, 'history-page', i);
      await check(page, 'after-end', i);
      await check(page, 'idle', i);
      const latestFirst = await checkLatestInput(page, i, false, !latestReloadOnly);
      const latestAfterReload = await checkLatestInput(page, i, true);
      console.log(`LATEST_INPUT_ORDER ${JSON.stringify({ first: latestFirst, afterReload: latestAfterReload })}`);
      await page.close();
    }
    const benchPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    const bench = await benchmarkHistoryInsert(benchPage);
    await benchPage.close();
    assert.ok(bench.unsequencedNoticeStable, 'unsequenced notice must not break sequenced node ordering');
    console.log(`PASS rounds=${rounds} cross_stream_inputs=7 whole_timeline_monotonic=true realtime=${rounds} claude_active=${rounds * 2} codex_active=${rounds * 2} reload_replay=${rounds * 2} history_older_page=${rounds} late_after_end=${rounds} idle_order=${rounds} benchmark_2000_plus_100_ms=${bench.elapsedMs.toFixed(2)} benchmark_children=${bench.children} unsequenced_notice=stable renderer_sha=${require('crypto').createHash('sha256').update(source).digest('hex')}`);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
}

main().catch(error => { console.error(error && error.stack || error); process.exitCode = 1; try { server.close(); } catch (_) {} });
