// R15 跟随释放覆盖补格（S-BROWSER-FOLLOWUP 第 1 项）：
// 初始历史重放【自然收尾】（默认 QUIET 250ms／HARD 1000ms 旋钮，不人为撑开回放窗）
// 之后，真实用户上翻必须释放跟随（timelineAtBottom=false），且随后的 live 事件
// 不得把滚动位置抢回底部。冻结的 browser-flake-windows 格覆盖的是「重放窗内上翻」
// 逃生口（finishTimelineReplay(false)）；本格覆盖 finishTimelineReplay(true)＋
// snapTimelineToBottomSettled 三拍补滚全部落地之后的释放路径（scroll 监听的
// else-if 分支）。刀 --mutate=release-only-in-replay 把该分支 gated 在
// replayInProgress 上（自然收尾后永不释放），本格必须红；锚缺失即报错。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';
// 上界理由：产品自身回放硬截止 1000ms＋收尾三拍（双 rAF／250ms／fonts.ready），
// 30000ms ≫ 产品各时限＋4x 负载调度余量（与 console harness CONDITION_WAIT_MS 同值）；
// 释放判定是手势后首个 scroll 事件内的同步判定，3000ms 沿用冻结格的原值与理由。
const CONDITION_WAIT_MS = 30000;
const RELEASE_WAIT_MS = 3000;
const FOLLOW_RELEASE_GAP_PX = 160;
const BOTTOM_THRESHOLD_PX = 50;

let html = fs.readFileSync(process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
function mutate(from, to) {
  const next = html.replace(from, to);
  if (next === html) throw new Error(`mutation anchor missing: ${from}`);
  html = next;
}
if (MUTATION === 'release-only-in-replay') {
  mutate('else if (byUser && gap > FOLLOW_RELEASE_GAP_PX) timelineAtBottom = false;',
    'else if (replayInProgress && byUser && gap > FOLLOW_RELEASE_GAP_PX) timelineAtBottom = false;');
} else if (MUTATION) {
  throw new Error(`unknown mutation: ${MUTATION}`);
}

const eventRow = (seq) => {
  const event = {
    kind: 'user_input',
    text: `R15_HISTORY_SEQ_${String(seq).padStart(3, '0')}`,
    delivery_id: `r15-${seq}`,
    turn_id: `r15-${seq}`, stage: 'cli_injected', ts: 1700000000000 + seq,
  };
  return {
    cursor: { generation: 'generation-r15', seq },
    event_id: `r15-history-${seq}`,
    received_at: new Date(1700000000000 + seq).toISOString(),
    event: JSON.stringify(event),
  };
};

const eventStreams = new Set();
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  if (url.pathname === '/view/r15') {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); res.end(html); return;
  }
  if (url.pathname === '/view/r15/status') {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ session: 'r15', state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'owner', model: 'm', cost_total: 0, cost_last: 0, turns: 1 })); return;
  }
  if (url.pathname === '/view/r15/tasks' || url.pathname === '/view/r15/page') {
    res.writeHead(200, { 'content-type': 'application/json' }); res.end('{}'); return;
  }
  if (url.pathname === '/view/r15/history') {
    const before = url.searchParams.get('before');
    const events = before === '101'
      ? Array.from({ length: 100 }, (_, i) => eventRow(1 + i))
      : Array.from({ length: 100 }, (_, i) => eventRow(101 + i));
    const page = {
      generation: 'generation-r15', high_water: 200, events,
      gaps: [],
      has_older: before !== '101', before_seq: before === '101' ? null : 101, gap_before: false,
    };
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify(page)); return;
  }
  if (url.pathname === '/view/r15/events') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
    eventStreams.add(res);
    const live = { kind: 'assistant_text', text: 'R15_LIVE_SEQ_201', delivery_id: 'r15-live', turn_id: 'r15-live', _history: { generation: 'generation-r15', seq: 201, event_id: 'r15-live-201' } };
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
    await page.goto(`http://127.0.0.1:${address.port}/view/r15`, { waitUntil: 'domcontentloaded' });
    await page.getByText('R15_HISTORY_SEQ_101').waitFor({ timeout: CONDITION_WAIT_MS });
    await page.getByText('R15_HISTORY_SEQ_200').waitFor({ timeout: CONDITION_WAIT_MS });
    await page.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: CONDITION_WAIT_MS });
    const timeline = page.locator('#timeline');
    // 自然收尾的可观察条件：回放标志已清、渲染缓冲排空、无待执行补滚帧、字体就绪、
    // 贴底（gap≤BOTTOM_THRESHOLD_PX）。不人为设置 REPLAY_QUIET_MS／REPLAY_HARD_LIMIT_MS，
    // 走产品默认旋钮的真实收尾路径。
    await page.waitForFunction(() => replayInProgress === false && pendingRenders.size === 0 && c1MergeBuf.size === 0 && followScrollFrame === null && document.fonts.status === 'loaded' && timelineAtBottom === true && (timeline.scrollHeight - timeline.scrollTop - timeline.clientHeight) <= 50, null, { timeout: CONDITION_WAIT_MS });
    // 排空收尾三拍（snapTimelineToBottomSettled：双 rAF＋250ms 补拍＋fonts.ready 补拍）。
    // 三拍都在 finishTimelineReplay(true) 时刻登记，早于本观察点；此处再登记一轮
    // 双 rAF＋300ms（>250ms）定时器：rAF 按登记序执行、定时器按截止序触发 ⇒ 本轮
    // 走完时产品三拍必已全部执行完（fonts 已 loaded，其微任务早已排空）。
    // 这是「等可观察条件＋有上界」：条件＝三拍可证排空，上界＝CONDITION_WAIT_MS。
    await Promise.race([
      page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(resolve, 300))))),
      new Promise((_, reject) => setTimeout(() => reject(new Error(`settled-snap drain exceeded ${CONDITION_WAIT_MS}ms`)), CONDITION_WAIT_MS)),
    ]);
    const settled = await page.evaluate(() => ({ replay: replayInProgress, atBottom: timelineAtBottom, gap: timeline.scrollHeight - timeline.clientHeight - timeline.scrollTop, top: timeline.scrollTop }));
    assert.strictEqual(settled.replay, false, `replay must have finished naturally before the gesture: ${JSON.stringify(settled)}`);
    assert.strictEqual(settled.atBottom, true, `settled snap must leave the timeline at bottom: ${JSON.stringify(settled)}`);
    assert.ok(settled.gap <= BOTTOM_THRESHOLD_PX, `settled gap ${settled.gap}px exceeds bottom threshold`);
    await timeline.evaluate((n) => { n.style.scrollBehavior = 'auto'; });
    const box = await timeline.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    // 大幅上翻真手势：-600 产生 gap>160 的离底位移；释放判定在首个 scroll 事件内
    // 同步完成（scroll 步骤先于 rAF，补滚帧抢不到判定之前）。
    await page.mouse.wheel(0, -600);
    await page.waitForFunction(() => timelineAtBottom === false, null, { timeout: RELEASE_WAIT_MS });
    const released = await page.evaluate(() => ({ gap: timeline.scrollHeight - timeline.clientHeight - timeline.scrollTop, top: timeline.scrollTop, atBottom: timelineAtBottom, replay: replayInProgress }));
    assert.ok(released.gap > FOLLOW_RELEASE_GAP_PX, `release gap ${released.gap}px must exceed FOLLOW_RELEASE_GAP_PX: ${JSON.stringify(released)}`);
    assert.strictEqual(released.replay, false, 'user scroll-up after natural finish must not re-arm replay');
    // 释放后的 live 事件：ResizeObserver／补滚判据 (replayInProgress || timelineAtBottom)
    // 均为假 ⇒ 不得抢回滚动位置。
    await page.evaluate(() => fetch('/view/r15/test-live', { method: 'POST' }));
    await page.getByText('R15_LIVE_AFTER_PAGE').waitFor({ timeout: CONDITION_WAIT_MS });
    await page.waitForFunction(() => pendingRenders.size === 0 && c1MergeBuf.size === 0 && followScrollFrame === null, null, { timeout: CONDITION_WAIT_MS });
    const afterLive = await page.evaluate(() => ({ gap: timeline.scrollHeight - timeline.clientHeight - timeline.scrollTop, top: timeline.scrollTop, atBottom: timelineAtBottom }));
    assert.strictEqual(afterLive.atBottom, false, `live append after release must not re-arm follow: ${JSON.stringify(afterLive)}`);
    assert.ok(Math.abs(afterLive.top - released.top) <= 2, `follow stole scroll back after live append: released=${JSON.stringify(released)} afterLive=${JSON.stringify(afterLive)}`);
    assert.ok(afterLive.gap > FOLLOW_RELEASE_GAP_PX, `post-live gap ${afterLive.gap}px collapsed below release threshold`);
    console.log(`PASS channel=${process.env.PW_CHANNEL || 'bundled-chromium'} mutation=${MUTATION || 'none'} natural_finish=true settled_gap_px=${settled.gap} released_gap_px=${released.gap} released_top_px=${released.top} post_live_gap_px=${afterLive.gap} post_live_top_px=${afterLive.top} follow_released=true live_no_refollow=true`);
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
