// R15 end-to-end upward paging: real HTTP, EventSource, and Chromium UI.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const html = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'));
const rendererOverride = process.env.RENDERER_OVERRIDE;
const pageHTML = rendererOverride ? fs.readFileSync(rendererOverride) : html;
const seenCursors = [];
const eventStreams = new Set();
let forcedHistoryFailure = 0;
let forcedHistoryGap = false;
const eventRow = (seq) => {
  const paired = seq === 50 || seq === 51;
  const event = {
    kind: seq === 50 ? 'input_posted' : 'user_input',
    text: paired ? `R15_HISTORY_SEQ_${String(seq).padStart(3, '0')}_${seq === 50 ? 'PRIMARY' : 'ECHO'}` : `R15_HISTORY_SEQ_${String(seq).padStart(3, '0')}`,
    delivery_id: paired ? 'r15-paired-50' : `r15-${seq}`,
    message_id: paired ? `r15-paired-message-${seq}` : undefined,
    turn_id: `r15-${seq}`, stage: 'cli_injected', ts: 1700000000000 + seq,
  };
  return {
    cursor: { generation: 'generation-r15', seq },
    event_id: `r15-history-${seq}`,
    received_at: new Date(1700000000000 + seq).toISOString(),
    event: JSON.stringify(event),
  };
};

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  if (url.pathname === '/view/r15') {
    forcedHistoryFailure = Number(url.searchParams.get('failure')) || 0;
    forcedHistoryGap = url.searchParams.get('gap') === '1';
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); res.end(pageHTML); return;
  }
  if (url.pathname === '/view/r15/status') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ session: 'r15', state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'owner', model: 'm', cost_total: 0, cost_last: 0, turns: 1 })); return;
  }
  if (url.pathname === '/view/r15/tasks' || url.pathname === '/view/r15/page') {
    res.writeHead(200, { 'content-type': 'application/json' }); res.end('{}'); return;
  }
  if (url.pathname === '/view/r15/history') {
    const failure = forcedHistoryFailure;
    if (failure) { res.writeHead(failure, { 'content-type': 'application/json' }); res.end(JSON.stringify({ code: `mock_${failure}` })); return; }
    const before = url.searchParams.get('before');
    seenCursors.push(before || 'latest');
    const events = before === '101'
      ? Array.from({ length: 100 }, (_, i) => eventRow(1 + i))
      : Array.from({ length: 100 }, (_, i) => eventRow(101 + i));
    const page = {
      generation: 'generation-r15', high_water: 200, events,
      gaps: forcedHistoryGap ? [{ after_seq: 100, started_at: new Date(1700000000000).toISOString(), reason: 'feature_disabled', lost_events: 1 }] : [],
      has_older: before !== '101', before_seq: before === '101' ? null : 101, gap_before: forcedHistoryGap,
    };
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify(page)); return;
  }
  if (url.pathname === '/view/r15/events') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
    eventStreams.add(res);
    const live = { kind: 'assistant_text', text: 'R15_LIVE_SEQ_201', delivery_id: 'r15-live', turn_id: 'r15-live' };
    if (forcedHistoryFailure !== 503) live._history = { generation: 'generation-r15', seq: 201, event_id: 'r15-live-201' };
    res.write(`data: ${JSON.stringify(live)}\n\n`);
    req.on('close', () => { eventStreams.delete(res); try { res.end(); } catch (_) {} }); return;
  }
  if (url.pathname === '/view/r15/test-live' && req.method === 'POST') {
    const live = { kind: 'assistant_text', text: 'R15_LIVE_AFTER_PAGE', delivery_id: 'r15-live-after', turn_id: 'r15-live-after' };
    for (const stream of eventStreams) stream.write(`data: ${JSON.stringify(live)}\n\n`);
    res.writeHead(200, { 'content-type': 'application/json' }); res.end('{"ok":true}'); return;
  }
  if (req.method === 'POST') { req.resume(); res.writeHead(200, { 'content-type': 'application/json' }); res.end('{"ok":true}'); return; }
  res.writeHead(404); res.end('not found');
});

async function main() {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  const browserOptions = process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {};
  const browser = await chromium.launch(browserOptions);
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    // 时序窗定因见 docs/reviews/browser-flake-windows/REPORT.md:原前置只等渲染缓冲
    // 清空,回放收尾 finishTimelineReplay(true) 的补拍(fonts 微任务/双 rAF/250ms)
    // 会抢先把位置滚回底部,+10 微滚轮随即在底部钳位,timelineAtBottom 永不置假。
    // 回放窗旋钮是渲染器明示的测试钩子(REPLAY_QUIET_MS/REPLAY_HARD_LIMIT_MS),
    // 60000ms 远大于本格时长 ⇒ 自然收尾不会与释放手势竞态,手势确定性地走
    // 「重放窗内用户上翻」逃生口(finishTimelineReplay(false),不武装补拍)。
    await page.addInitScript(() => { window.REPLAY_QUIET_MS = 60000; window.REPLAY_HARD_LIMIT_MS = 60000; });
    await page.goto(`http://127.0.0.1:${address.port}/view/r15`, { waitUntil: 'domcontentloaded' });
    await page.getByText('R15_HISTORY_SEQ_101').waitFor({ timeout: 7000 });
    await page.getByText('R15_HISTORY_SEQ_200').waitFor({ timeout: 7000 });
    await page.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: 7000 });
    const timeline = page.locator('#timeline');
    await page.waitForFunction(() => pendingRenders.size === 0 && c1MergeBuf.size === 0 && followScrollFrame === null && document.fonts.status === 'loaded', null, { timeout: 7000 });
    assert.strictEqual(await page.evaluate(() => replayInProgress), true, 'replay window knob must keep replay open until the release gesture');
    await timeline.evaluate((n) => { n.style.scrollBehavior = 'auto'; n.scrollTop = 300; });
    const box = await timeline.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    // 大幅上翻真手势:即便有补滚先落底,-600 仍产生 gap>160 的离底位移;重放窗内
    // 唯一补滚来源 followScrollFrame 是 rAF,帧序上 scroll 步骤先于 rAF,抢不到
    // 释放判定之前 ⇒ 无竞态。3000ms 上界沿用原值:释放是手势后首个 scroll 事件
    // 内的同步判定,3s 远大于负载下的帧调度余量。
    await page.mouse.wheel(0, -600);
    await page.waitForFunction(() => timelineAtBottom === false, null, { timeout: 3000 });
    const originalFirst = await timeline.evaluate((n) => n.firstElementChild.innerText.match(/R15_HISTORY_SEQ_\d+/)?.[0]);
    const anchor = await timeline.evaluateHandle((n) => [...n.children].find(node => node.getBoundingClientRect().bottom > n.getBoundingClientRect().top));
    const beforeTop = await anchor.evaluate((n) => n.getBoundingClientRect().top);
    const beforeScroll = await timeline.evaluate((n) => ({ top: n.scrollTop, height: n.scrollHeight, client: n.clientHeight, overflowAnchor: getComputedStyle(n).overflowAnchor }));
    await page.getByRole('button', { name: '向上翻页' }).click();
    try {
      await page.getByText('R15_HISTORY_SEQ_001').waitFor({ timeout: 7000 });
      await page.getByText('R15_HISTORY_SEQ_100').waitFor({ timeout: 7000 });
    } catch (error) {
      console.error(`debug cursors=${JSON.stringify(seenCursors)} button=${await page.locator('#historyOlder').textContent()} timeline=${(await timeline.innerText()).slice(0, 600)}`);
      throw error;
    }
    const afterTop = await anchor.evaluate((n) => n.getBoundingClientRect().top);
    const afterScroll = await timeline.evaluate((n) => ({ top: n.scrollTop, height: n.scrollHeight, client: n.clientHeight, overflowAnchor: getComputedStyle(n).overflowAnchor }));
    assert.deepStrictEqual(seenCursors, ['latest', '101'], `history requests=${JSON.stringify(seenCursors)}`);
    const anchorState = await anchor.evaluate((n) => ({ connected: n.isConnected, text: n.innerText.slice(0, 100), top: n.getBoundingClientRect().top }));
    const firstState = await timeline.locator(':scope > *').first().evaluate((n) => ({ text: n.innerText.slice(0, 100), top: n.getBoundingClientRect().top }));
    const order = await timeline.evaluate((n, originalText) => {
      const children = [...n.children];
      const text = (node) => node.innerText || '';
      const first = children[0];
      const seq100 = children.find(node => text(node).includes('R15_HISTORY_SEQ_100'));
      const original = children.find(node => text(node).includes(originalText));
      return { first: text(first).slice(0, 120), seq100NextIsOriginal: !!seq100 && seq100.nextElementSibling === original,
        original: original ? text(original).slice(0, 120) : null, originalText };
    }, originalFirst);
    assert.ok(order.first.includes('R15_HISTORY_SEQ_001') && order.seq100NextIsOriginal,
      `timeline order invalid: ${JSON.stringify(order)} before=${JSON.stringify(beforeScroll)} after=${JSON.stringify(afterScroll)}`);
    assert.strictEqual(await timeline.locator('.turn-user').filter({ hasText: 'R15_HISTORY_SEQ_051_ECHO' }).count(), 1,
      'history input_posted/user_input echo with one delivery must render once in its turn input segment');
    assert.ok(Math.abs(afterTop - beforeTop) <= 2, `scroll anchor shifted ${afterTop - beforeTop}px before=${JSON.stringify(beforeScroll)} after=${JSON.stringify(afterScroll)} anchor=${JSON.stringify(anchorState)} first=${JSON.stringify(firstState)}`);
    await page.evaluate(() => fetch('/view/r15/test-live', { method: 'POST' }));
    await page.getByText('R15_LIVE_AFTER_PAGE').waitFor({ timeout: 7000 });
    const afterLiveTop = await anchor.evaluate((n) => n.getBoundingClientRect().top);
    const afterLiveFirst = await timeline.locator(':scope > *').first().innerText();
    const postLiveAnchorDelta = afterLiveTop - beforeTop;
    assert.ok(afterLiveFirst.includes('R15_HISTORY_SEQ_001'), `post-live history prefix was evicted: first=${afterLiveFirst.slice(0, 120)}`);
    assert.ok(Math.abs(postLiveAnchorDelta) <= 2, `post-live scroll anchor shifted ${postLiveAnchorDelta}px before=${JSON.stringify(beforeScroll)} after=${JSON.stringify(await timeline.evaluate((n) => ({ top: n.scrollTop, height: n.scrollHeight, client: n.clientHeight })))}`);
    await timeline.evaluate((n) => { n.scrollTop = n.scrollHeight; });
    await page.waitForFunction(() => timelineAtBottom && !historyWindowExpanded, null, { timeout: 3000 });
    assert.ok(await timeline.locator(':scope > *').count() <= 200, 'timeline cap did not resume after returning to live edge');
    assert.strictEqual(await page.getByRole('button', { name: '没有更早的记录' }).isDisabled(), true);
    const gapPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    await gapPage.goto(`http://127.0.0.1:${address.port}/view/r15?gap=1`, { waitUntil: 'domcontentloaded' });
    await gapPage.getByText('这段历史未保存').waitFor({ timeout: 7000 });
    await gapPage.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: 7000 });
    await gapPage.close();
    for (const status of [503, 401, 404, 500]) {
      const failedPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      await failedPage.goto(`http://127.0.0.1:${address.port}/view/r15?failure=${status}`, { waitUntil: 'domcontentloaded' });
      await failedPage.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: 7000 });
      if (status === 503) {
        assert.strictEqual(await failedPage.locator('#historyOlder').isHidden(), true, 'disabled feature showed history button');
      }
      await failedPage.close();
    }
    console.log(`PASS channel=${process.env.PW_CHANNEL || 'bundled-chromium'} latest=100 older=100 live=1 live_after_page=true cursor_exclusive=true anchor_delta_px=${(afterTop - beforeTop).toFixed(2)} post_live_anchor_delta_px=${postLiveAnchorDelta.toFixed(2)} history_pair_cards=1 feature_disabled_gap=true errors=503,401,404,500 live_unblocked=true`);
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }
}

main().catch((error) => {
  console.error(error && error.stack || error);
  process.exitCode = 2;
  try { server.close(); } catch (_) {}
});
