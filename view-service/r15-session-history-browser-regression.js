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
let forcedHistoryFailure = 0;
const eventRow = (seq) => ({
  cursor: { generation: 'generation-r15', seq },
  event_id: `r15-history-${seq}`,
  received_at: new Date(1700000000000 + seq).toISOString(),
  event: JSON.stringify({
    kind: 'user_input', text: `R15_HISTORY_SEQ_${String(seq).padStart(3, '0')}`, delivery_id: `r15-${seq}`,
    turn_id: `r15-${seq}`, stage: 'cli_injected', ts: 1700000000000 + seq,
  }),
});

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  if (url.pathname === '/view/r15') {
    forcedHistoryFailure = Number(url.searchParams.get('failure')) || 0;
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
    if (failure) console.log(`MOCK history failure=${failure} request=${req.url}`);
    if (failure) { res.writeHead(failure, { 'content-type': 'application/json' }); res.end(JSON.stringify({ code: `mock_${failure}` })); return; }
    const before = url.searchParams.get('before');
    seenCursors.push(before || 'latest');
    const events = before === '101'
      ? Array.from({ length: 100 }, (_, i) => eventRow(1 + i))
      : Array.from({ length: 100 }, (_, i) => eventRow(101 + i));
    const page = {
      generation: 'generation-r15', high_water: 200, events, gaps: [],
      has_older: before !== '101', before_seq: before === '101' ? null : 101, gap_before: false,
    };
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify(page)); return;
  }
  if (url.pathname === '/view/r15/events') {
    if (forcedHistoryFailure) console.log(`MOCK events failure=${forcedHistoryFailure}`);
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
    const live = { kind: 'assistant_text', text: 'R15_LIVE_SEQ_201', delivery_id: 'r15-live', turn_id: 'r15-live' };
    if (forcedHistoryFailure !== 503) live._history = { generation: 'generation-r15', seq: 201, event_id: 'r15-live-201' };
    res.write(`data: ${JSON.stringify(live)}\n\n`);
    req.on('close', () => { try { res.end(); } catch (_) {} }); return;
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
    await page.getByText('R15_HISTORY_SEQ_101').waitFor({ timeout: 7000 });
    await page.getByText('R15_HISTORY_SEQ_200').waitFor({ timeout: 7000 });
    await page.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: 7000 });
    const timeline = page.locator('#timeline');
    await timeline.evaluate((n) => { n.style.scrollBehavior = 'auto'; n.scrollTop = 300; });
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
    assert.ok(Math.abs(afterTop - beforeTop) <= 2, `scroll anchor shifted ${afterTop - beforeTop}px before=${JSON.stringify(beforeScroll)} after=${JSON.stringify(afterScroll)} anchor=${JSON.stringify(anchorState)} first=${JSON.stringify(firstState)}`);
    assert.strictEqual(await page.getByRole('button', { name: '没有更早的记录' }).isDisabled(), true);
    for (const status of [503, 401, 404, 500]) {
      const failedPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      await failedPage.goto(`http://127.0.0.1:${address.port}/view/r15?failure=${status}`, { waitUntil: 'domcontentloaded' });
      await failedPage.getByText('R15_LIVE_SEQ_201').waitFor({ timeout: 7000 });
      if (status === 503) {
        console.log(`button disabled ${JSON.stringify(await failedPage.locator('#historyOlder').evaluate((n) => ({hidden:n.hidden,display:getComputedStyle(n).display,text:n.textContent})))}`);
        assert.strictEqual(await failedPage.locator('#historyOlder').isHidden(), true, 'disabled feature showed history button');
      }
      await failedPage.close();
    }
    console.log(`PASS channel=${process.env.PW_CHANNEL || 'bundled-chromium'} latest=100 older=100 live=1 cursor_exclusive=true anchor_delta_px=${(afterTop - beforeTop).toFixed(2)} errors=503,401,404,500 live_unblocked=true`);
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
