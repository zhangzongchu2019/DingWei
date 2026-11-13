const fs = require('fs');
const http = require('http');
const cp = require('child_process');
const { chromium } = require('playwright');
const root = '/data/ai-dev-data/work/r562-cr-review-20261109';
const cap = `${root}/view-service/testdata/view-input-capture-20261113`;
const history = JSON.parse(fs.readFileSync(`${cap}/history.json`, 'utf8'));
const frames = fs.readFileSync(`${cap}/sse-frames.jsonl`, 'utf8').trimEnd().split('\n').map(x => JSON.parse(x));
const versions = { baseline: cp.execFileSync('git',['show','2e1e4c9d:view-service/renderer-canary-sse.html'],{cwd:root,encoding:'utf8'}), after: fs.readFileSync(`${root}/view-service/renderer-canary-sse.html`, 'utf8') };
let current = 'before'; let sentFrames = 0; let responseStreams = new Set();
const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://127.0.0.1');
  const m = u.pathname.match(/^\/view\/([^/]+)(?:\/(.*))?$/);
  if (!m) { res.writeHead(404); res.end(); return; }
  if (!m[2]) {
    current = u.searchParams.get('version') || 'before';
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' });
    res.end(versions[current]); return;
  }
  if (m[2] === 'history') {
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    res.end(JSON.stringify(history)); return;
  }
  if (m[2] === 'events') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache', connection: 'keep-alive' });
    responseStreams.add(res); let i = 0;
    const timer = setInterval(() => {
      if (res.destroyed || i >= frames.length) { clearInterval(timer); return; }
      const f = frames[i++];
      if (f.id) res.write(`id: ${f.id}\n`);
      if (f.event) res.write(`event: ${f.event}\n`);
      res.write(`data: ${JSON.stringify(f.data)}\n\n`);
      sentFrames++;
    }, 2);
    req.on('close', () => { clearInterval(timer); responseStreams.delete(res); }); return;
  }
  if (['status', 'tasks', 'page'].includes(m[2])) {
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(m[2] === 'status' ? JSON.stringify({ session: decodeURIComponent(m[1]), state: 'running', readonly: true, can_write: false, auth_mode: 'legacy', owner: 'capture', model: 'fixture', turns: 20, cost_total: 0, cost_last: 0 }) : '{}'); return;
  }
  res.writeHead(404); res.end();
});
function groups(page) {
  return page.evaluate(() => [...document.querySelectorAll('#timeline > .turn-group')].map((g, index) => {
    const sections = [...g.children].filter(n => [...n.classList].some(c => /^turn-(user|assistant|tool|error)$/.test(c)));
    return { index, key: g.dataset.turnKey, segments: sections.map(n => ({
      kind: [...n.classList].find(c => /^turn-(user|assistant|tool|error)$/.test(c)).replace('turn-', ''),
      textChars: n.querySelector('.event-content')?.textContent?.length || 0,
      ts: Number(n.querySelector('.event-time')?.dataset.ts) || null,
      title: n.querySelector('.event-time')?.title || '',
    })) };
  }));
}
async function runVersion(browser, version) {
  current = version; sentFrames = 0;
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  const browserErrors = [];
  page.on('pageerror', e => browserErrors.push(String(e)));
  await page.goto(`http://127.0.0.1:${server.address().port}/view/opus50tc2k.zzc?version=${version}`, { waitUntil: 'domcontentloaded' });
  await page.waitForFunction(() => historyBootstrapPending === false && pendingRenders.size === 0, null, { timeout: 20000 });
  await new Promise(resolve => { const poll = () => sentFrames >= frames.length ? resolve() : setTimeout(poll, 50); poll(); });
  await page.waitForFunction(() => historyBootstrapPending === false && pendingRenders.size === 0 && c1MergeBuf.size === 0, null, { timeout: 20000 });
  await page.waitForTimeout(300);
  const baselineGroups = await groups(page);
  const cards = await page.evaluate(() => [...document.querySelectorAll('#timeline > .message-card.user')].map((n, index) => ({ index, ts: Number(n.querySelector('.event-time')?.dataset.ts) || null, seq: Number(n.dataset.timelineSeq) || null, title: n.querySelector('.event-time')?.title || '' })));
  const outputGroups = baselineGroups.filter(g => g.segments.some(s => s.kind === 'assistant'));
  const postedIds = new Set(frames.filter(f => f.data?.kind === 'input_posted').map(f => f.data.delivery_id).filter(Boolean));
  const outputTurns = new Set(outputGroups.map(g => g.key));
  const matchedIds = new Set();
  for (const row of history.events) { const e = row.event; const raw = typeof e === 'string' ? JSON.parse(e) : e; if (raw?.kind === 'user_input' && raw.turn_id && outputTurns.has(raw.turn_id) && raw.delivery_id) matchedIds.add(raw.delivery_id); }
  for (const f of frames) { const raw = f.data; if (raw?.kind === 'user_input' && raw.turn_id && outputTurns.has(raw.turn_id) && raw.delivery_id) matchedIds.add(raw.delivery_id); }
  const expectedStandalone = [...postedIds].filter(id => !matchedIds.has(id)).length;
  const failures = [];
  for (const g of outputGroups) {
    const kinds = g.segments.map(s => s.kind);
    const user = g.segments.find(s => s.kind === 'user'); const assistant = g.segments.find(s => s.kind === 'assistant');
    if (kinds.filter(k => k === 'user').length !== 1 || kinds.indexOf('user') > kinds.indexOf('assistant') || !user?.ts || !assistant?.ts || user.ts > assistant.ts) failures.push({ key:g.key, kinds, inputTs:user?.ts, outputTs:assistant?.ts });
  }
  const cardsOrdered = cards.every((c,i) => i === 0 || (cards[i-1].seq && c.seq && cards[i-1].seq <= c.seq));
  const timelineTimes = await page.evaluate(() => [...document.querySelectorAll('#timeline > *')].map(n => Number(n.dataset.timelineTs) || Math.min(...[...n.querySelectorAll('.event-time[data-ts]')].map(t => Number(t.dataset.ts)).filter(Number.isFinite))).filter(Number.isFinite));
  const timelineOrdered = timelineTimes.every((ts,i) => i === 0 || timelineTimes[i-1] <= ts);
  const result={version, framesInjected:frames.length, historyRowsInjected:history.events.length, groups:baselineGroups, summary:{groupCount:baselineGroups.length,
    messageCardUserCount:cards.length, expectedStandalone, cardsOrdered, messageCardTimestampsPresent:cards.every(c=>c.ts && c.title.includes('UTC')), timelineOrdered, outputGroupCount:outputGroups.length, failedOutputGroups:failures,
    userSegments:baselineGroups.reduce((n,g)=>n+g.segments.filter(s=>s.kind==='user').length,0), assistantSegments:outputGroups.length,browserErrors}};
  if (version === 'after' && (cards.length !== expectedStandalone || !cardsOrdered || !timelineOrdered || failures.length || browserErrors.length)) throw new Error(JSON.stringify(result.summary));
  await page.close(); return result;
}
(async()=>{await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve)); const browser=await chromium.launch({}); try { const baseline=await runVersion(browser,'baseline'); const after=await runVersion(browser,'after'); console.log(JSON.stringify({capture:{historyRows:history.events.length,sseFrames:frames.length,firstId:frames.find(f=>f.id)?.id,lastId:frames.filter(f=>f.id).at(-1)?.id},baseline:{summary:baseline.summary,groups:baseline.groups},after:{summary:after.summary,groups:after.groups}},null,2)); } finally { await browser.close(); for(const r of responseStreams)r.destroy(); await new Promise(resolve=>server.close(resolve)); }})().catch(e=>{console.error(e.stack||e);process.exitCode=1;try{server.close()}catch{}});
