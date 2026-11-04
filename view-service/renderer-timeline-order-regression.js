// Deterministic replay test for late delivery admission vs. turn output ordering.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

let source = fs.readFileSync(process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
const mutation = process.argv.includes('--mutate-append');
const roundsArg = process.argv.find(arg => arg.startsWith('--rounds='));
const rounds = Math.max(1, Math.min(20, Number(roundsArg?.split('=')[1]) || 1));
const BASE = 1793820000000;
let activeMode = 'active'; let activeN = 1;

function events(mode, n) {
  const turn = `timeline-turn-${mode}-${n}`;
  const delivery = `timeline-delivery-${mode}-${n}`;
  const user = { kind: 'input_posted', schema_version: 'delivery.v2', message_id: `timeline-msg-${n}`,
    delivery_id: delivery, payload: `TIMELINE_USER_${mode}_${n}`, stage: 'helper_queued', ts: BASE + 2000,
    source: { kind: 'agent', trust_decision: 'trusted' }, delivery: { role: 'primary' } };
  const reply = { kind: 'assistant_text', text: `TIMELINE_REPLY_${mode}_${n}`, delivery_id: `reply-${n}`,
    turn_id: turn, ts: BASE + 3000 };
  const accepted = { kind: 'delivery_update', schema_version: 'delivery.v2', message_id: `timeline-msg-${n}`,
    observed_delivery_id: delivery, delivery_stage: 'cli_injected', turn_id: turn, ts: BASE + 4000 };
  const list = mode === 'active' ? [user, reply, accepted] : [reply, user, accepted];
  return list.map((event, i) => ({ cursor: { generation: `timeline-${mode}`, seq: i + 1 }, event_id: `timeline-${mode}-${n}-${i + 1}`,
    received_at: new Date(BASE + i).toISOString(), event: JSON.stringify(event) }));
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1');
  const mode = url.searchParams.get('mode') || 'active';
  const n = Number(url.searchParams.get('n')) || 1;
  if (url.pathname === '/view/timeline') {
    activeMode = mode; activeN = n;
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); res.end(source); return;
  }
  if (url.pathname === '/view/timeline/history') {
    const rows = events(activeMode, activeN);
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify({ generation: `timeline-${activeMode}`, high_water: rows.length, events: rows, has_older: false })); return;
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
    res.write(': connected\n\n'); req.on('close', () => {}); return;
  }
  res.writeHead(404); res.end('not found');
});

const assertion = 'timeline order invalid: late accepted user card must be before same-turn reply';
async function check(page, mode, n, reload = false) {
  if (reload) await page.reload({ waitUntil: 'domcontentloaded' });
  else await page.goto(`http://127.0.0.1:${server.address().port}/view/timeline?mode=${mode}&n=${n}`, { waitUntil: 'domcontentloaded' });
  const userText = `TIMELINE_USER_${mode}_${n}`;
  const replyText = `TIMELINE_REPLY_${mode}_${n}`;
  try { await page.getByText(userText, { exact: false }).waitFor({ timeout: 10000 }); }
  catch (error) { console.error(`fixture state mode=${mode} body=${(await page.locator('#timeline').innerText()).slice(0, 1000)} waiting=${await page.locator('#waitingList').innerText().catch(() => '')}`); throw error; }
  await page.getByText(replyText, { exact: false }).waitFor({ timeout: 10000 });
  await page.waitForFunction(() => pendingRenders.size === 0 && c1MergeBuf.size === 0, null, { timeout: 10000 });
  const order = await page.locator('#timeline').evaluate((timeline, texts) => {
    const children = [...timeline.children];
    const index = text => children.findIndex(node => (node.innerText || '').includes(text));
    return { user: index(texts.user), reply: index(texts.reply), children: children.map(node => (node.innerText || '').slice(0, 80)) };
  }, { user: userText, reply: replyText });
  assert.ok(order.user >= 0 && order.reply >= 0 && order.user < order.reply,
    `${assertion}; mode=${mode} user_index=${order.user} reply_index=${order.reply} children=${JSON.stringify(order.children)}`);
  return order;
}

async function main() {
  let html = source;
  if (mutation) {
    const anchor = '    function placeTimelineNode(node, fallbackBefore = null) {\n';
    assert.strictEqual(html.split(anchor).length - 1, 1, 'mutation anchor must be unique');
    source = html.replace(anchor, `${anchor}      timeline.appendChild(node); // mutant: append instead of timestamp positioning\n      return;\n`);
  }
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {});
  try {
    for (let i = 1; i <= rounds; i++) {
      const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      await check(page, 'active', i);
      // The browser page is reloaded; the same event history is replayed from the HTTP history endpoint.
      await check(page, 'active', i, true);
      await check(page, 'idle', i);
      await page.close();
    }
    console.log(`PASS rounds=${rounds} late_accept_before_reply=${rounds * 2} reload_replay=${rounds} idle_order=${rounds} renderer_sha=${require('crypto').createHash('sha256').update(source).digest('hex')}`);
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
}

main().catch(error => { console.error(error && error.stack || error); process.exitCode = 1; try { server.close(); } catch (_) {} });
