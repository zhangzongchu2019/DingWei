// Item backfill replaces live fragments by (thread, turn, item) key in the real renderer.
const assert = require('assert/strict');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

const html = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
(async () => {
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/*', async route => {
      const url = new URL(route.request().url());
      if (url.pathname.startsWith('/view/')) return route.fulfill({ contentType: 'text/html', body: html });
      if (url.pathname.endsWith('/status')) return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        session: 'fixture-thread', state: 'idle', readonly: true, can_write: false, auth_mode: 'legacy',
        owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0,
      }) });
      if (url.pathname.endsWith('/events')) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' });
      if (url.pathname.endsWith('/alerts') || url.pathname.endsWith('/tasks')) return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      if (url.pathname === '/favicon.ico') return route.fulfill({ status: 204, body: '' });
      return route.fulfill({ status: 404, contentType: 'application/json', body: '{}' });
    });
    await page.goto('http://renderer.test/view/fixture-thread', { waitUntil: 'domcontentloaded' });
    await page.locator('#humanInput').waitFor({ state: 'visible' });
    await page.waitForTimeout(100);
    await page.evaluate(() => {
      resetConversation();
      const delta = (item_id, text, cursor, thread_id = 'fixture-thread') => ({
        kind: 'assistant_text', text, session_id: thread_id, delivery_id: 'delivery-1', turn_id: 'turn-1', cursor,
        data: { thread_id, item_id, params: { threadId: thread_id, turnId: 'turn-1', itemId: item_id } },
      });
      handleIncomingEvent(delta('item-a', 'Hello ', 'turn:turn-1:item:0002'));
      handleIncomingEvent(delta('item-a', 'world?', 'turn:turn-1:item:0002'));
      handleIncomingEvent(delta('item-b', ' other item', 'turn:turn-1:item:0003'));
      handleIncomingEvent({
        kind: 'assistant_text', text: 'Hello world!', session_id: 'fixture-thread', delivery_id: 'delivery-1',
        turn_id: 'turn-1', cursor: 'turn:turn-1:item:0002',
        data: { thread_id: 'fixture-thread', item_id: 'item-a', backfill: true, replay: true },
      });
    });
    const text = () => page.locator('#timeline .turn-group .turn-assistant .event-content').innerText();
    await page.waitForFunction(() => document.querySelector('#timeline .turn-group .turn-assistant .event-content')?.textContent.includes('Hello world!'));
    const first = (await text()).replace(/\s+/g, ' ');
    assert.ok(first.includes('Hello world!') && first.includes('other item'), 'BACKFILL_DID_NOT_REPLACE_ITEM');
    assert.ok(!first.includes('world?') && !first.includes('Hello Hello'), 'BACKFILL_LEFT_OLD_ITEM_FRAGMENTS');
    await page.evaluate(() => {
      handleIncomingEvent({ kind: 'assistant_text', text: 'late delta', session_id: 'fixture-thread', delivery_id: 'delivery-1',
        turn_id: 'turn-1', cursor: 'turn:turn-1:item:0002', data: { thread_id: 'fixture-thread', item_id: 'item-a' } });
      handleIncomingEvent({ kind: 'assistant_text', text: 'Hello world!', session_id: 'fixture-thread', delivery_id: 'delivery-1',
        turn_id: 'turn-1', cursor: 'turn:turn-1:item:0002', data: { thread_id: 'fixture-thread', item_id: 'item-a', backfill: true, replay: true } });
    });
    await page.waitForTimeout(100);
    const second = (await text()).replace(/\s+/g, ' ');
    assert.ok(second.includes('Hello world!') && second.includes('other item'), 'BACKFILL_NOT_IDEMPOTENT');
    assert.ok(!second.includes('late delta') && !second.includes('world?'), 'LATE_DELTA_NOT_SUPPRESSED');
    assert.deepEqual(errors, [], `RENDERER_ERRORS: ${errors.join(';')}`);
    console.log(JSON.stringify({ suite: 'stale-thread-backfill-browser', passed: 3, failed: 0, renderer: 'renderer-canary-sse.html' }));
  } finally {
    await browser.close();
  }
})().catch(err => { console.error(err.stack || err); process.exitCode = 1; });
