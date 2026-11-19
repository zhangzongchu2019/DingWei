const fs = require('fs'); const http = require('http'); const path = require('path');
const { chromium } = require('playwright');
const ROOT = '/tmp/wt-r62-mobile';
const renderer = fs.readFileSync(path.join(ROOT, 'view-service/renderer-canary-sse.html'), 'utf8');
const json = (res, v) => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(v)); };
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/view/s') { res.writeHead(200, { 'Content-Type': 'text/html' }); return res.end(renderer); }
  if (url.pathname === '/view/s/status') return json(res, { session: 's', state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'o', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
  if (url.pathname === '/view/s/tasks' || url.pathname === '/view/s/page') return json(res, {});
  if (url.pathname === '/view/s/history') return json(res, { generation: 'g', high_water: 150, events: Array.from({ length: 150 }, (_, i) => ({ cursor: { generation: 'g', seq: i + 1 }, event_id: `h${i}`, received_at: new Date(1700000000000 + i).toISOString(), event: JSON.stringify({ kind: 'assistant_text', text: `LINE_${i} 正文足够长以产生滚动。`, delivery_id: `d${i}`, turn_id: `t${i}`, stage: 'cli_injected', ts: 1700000000000 + i }) })), gaps: [], has_older: false, before_seq: null, gap_before: false });
  if (url.pathname === '/view/s/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write('data: {"kind":"assistant_text","text":"LIVE","delivery_id":"l","turn_id":"l"}\n\n'); req.on('close', () => { try { res.end(); } catch (_) {} }); return; }
  if (req.method === 'POST') { req.resume(); return json(res, { ok: true }); }
  res.writeHead(404); res.end();
});
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch({ headless: true });
  for (const vp of [{ width: 390, height: 844 }, { width: 360, height: 780 }]) {
    const ctx = await browser.newContext({ viewport: vp, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
    const page = await ctx.newPage();
    await page.goto(`http://127.0.0.1:${server.address().port}/view/s`);
    await page.waitForFunction(() => document.getElementById('timeline') && document.getElementById('timeline').children.length >= 100, null, { timeout: 15000 });
    const rows = await page.evaluate(() => [...document.querySelectorAll('button,input,select,a,[role=button]')].filter((el) => el.offsetParent !== null).map((el) => { const r = el.getBoundingClientRect(); return { id: el.id || el.className.toString().slice(0, 24), tag: el.tagName, w: Math.round(r.width), h: Math.round(r.height) }; }));
    console.log(vp.width, JSON.stringify(rows.filter((r) => r.h < 40 || r.w < 40)));
    console.log(vp.width, 'total', rows.length, 'overflow', await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth));
    await ctx.close();
  }
  await browser.close(); server.close();
})().catch((e) => { console.error(e); process.exitCode = 1; });
