const fs = require('fs');
const path = require('path');
const { launchChromium, startRendererServer } = require('/tmp/wt-r62-cli-view-b/view-service/renderer-regression-selfhost');
const TS0 = Date.UTC(2026, 10, 18, 12, 0, 0);
(async () => {
  const html = fs.readFileSync('/tmp/wt-r62-cli-view-b/view-service/renderer-canary-sse.html', 'utf8');
  const fixture = await startRendererServer(Buffer.from(html));
  const baseUrl = fixture.url;
  const browser = await launchChromium();
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  let headHits = 0, getHits = 0;
  await page.route('**/status', r => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ session: 'probe', state: 'idle', readonly: true, can_write: false, owner: 'o', model: 'm', cost_total: 0, cost_last: 0, turns: 0 }) }));
  await page.route('**/events', r => r.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route('**/f/**', r => {
    if (r.request().method() === 'HEAD') { headHits++; return r.fulfill({ status: 200, headers: { 'content-type': 'text/plain; charset=utf-8', 'content-length': '18' }, body: '' }); }
    getHits++; return r.fulfill({ status: 200, contentType: 'text/plain; charset=utf-8', body: 'fixture file body\n' });
  });
  page.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') console.log('CONSOLE', m.type(), m.text().slice(0, 200)); });
  page.on('pageerror', e => console.log('PAGEERROR', String(e).slice(0, 300)));
  await page.addInitScript(() => { window.PROVENANCE_ICONS_V1 = true; });
  await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
  const attUrl = name => new URL(`/f/${name}`, baseUrl).href;
  const uCard = attUrl('r62-card-notes.txt'), uTurnUser = attUrl('r62-turn-input.txt'), uTurnBody = attUrl('r62-result-log.txt'), uTail = attUrl('r62-tail-file.txt');
  const events = [
    { schema_version: 'delivery.v2', kind: 'input_posted', message_id: 'm-att', delivery_id: 'dw-dlv_ATT', stage: 'hub_received', payload: `请查收 ${uCard}`, ts: TS0, delivery: { role: 'primary' }, cancellable: false, updated_at: new Date(TS0).toISOString() },
    { schema_version: 'delivery.v2', kind: 'delivery_update', message_id: 'm-att', observed_delivery_id: 'dw-dlv_ATT', delivery_stage: 'cli_injected', delivery: { role: 'primary' }, ts: TS0 + 400, updated_at: new Date(TS0 + 400).toISOString() },
    { kind: 'user_input', turn_id: 't-att', delivery_id: 'd-att-in', text: `跑一下并回传 ${uTurnUser}`, cursor: '0000001', ts: TS0 + 1000 },
    { kind: 'assistant_text', turn_id: 't-att', delivery_id: 'd-att-out', text: `结果见 ${uTurnBody}`, cursor: '0000002', ts: TS0 + 2000 },
    { kind: 'state_change', turn_id: 't-att', cursor: '0000003', ts: TS0 + 3000, data: { change: 'turn_completed' } },
    { kind: 'assistant_text', turn_id: 't-att-long', delivery_id: 'd-att-long', text: `${'长'.repeat(4500)} 尾部附件 ${uTail}`, cursor: '0000001', ts: TS0 + 4000 },
    { kind: 'state_change', turn_id: 't-att-long', cursor: '0000002', ts: TS0 + 5000, data: { change: 'turn_completed' } },
  ];
  for (const ev of events) await page.evaluate(e => handleIncomingEvent(e), ev);
  await page.evaluate(() => { if (typeof flushPendingRenders === 'function') flushPendingRenders(); });
  await new Promise(r => setTimeout(r, 3000));
  const dump = await page.evaluate(() => ({
    attachments: Array.from(document.querySelectorAll('#timeline .attachment')).map(n => ({ url: n.dataset.fileUrl, dispatch: n.dataset.dispatch, rendered: n.dataset.rendered, kind: n.dataset.kind })),
    hosts: Array.from(document.querySelectorAll('#timeline .attachments')).map(h => Array.from(h.children).map(c => c.dataset.fileUrl || c.tagName)),
    cards: Array.from(document.querySelectorAll('#timeline .message-card')).map(c => ({ text: (c.textContent || '').slice(0, 60), hasHost: !!c._fileHost, fileText: (c._fileText || '').slice(0, 60) })),
    cliAttach: document.querySelectorAll('.cli-attach').length,
  }));
  console.log('HEAD_HITS', headHits, 'GET_HITS', getHits);
  console.log(JSON.stringify(dump, null, 1));
  await page.click('#cliViewBtn');
  const cli = await page.evaluate(() => Array.from(document.querySelectorAll('#timeline-cli .cli-attach')).map(n => n.textContent));
  console.log('CLI_ATTACH', JSON.stringify(cli));
  await browser.close(); await fixture.close();
})().catch(e => { console.error('PROBE_ERR', e); process.exit(1); });
