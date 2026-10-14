// Reasoned upstream SSE refusal must stay visible while retrying with the
// renderer's bounded backoff. This uses a real EventSource in Chromium; a
// synthetic callback would not exercise the close/retry ordering.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const renderer = process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html');

async function startFixture({ reason = 'event_stream_unavailable', previousContent = false } = {}) {
  const html = fs.readFileSync(renderer);
  let eventRequests = 0;
  let retryHeadersFlushedAt = 0;
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://fixture.invalid').pathname;
    if (pathname === '/view/oaagent.lbl') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      return res.end(html);
    }
    if (pathname === '/view/oaagent.lbl/events') {
      eventRequests += 1;
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' });
      if (eventRequests === 1) {
        // The preservation promise applies to the production sequenced stream.
        // Old no-id Hub streams intentionally retain their existing replay-reset
        // behavior and are not made stronger by this View404 change.
        const previous = previousContent ? 'id: 0000000000000001-1\ndata: {"kind":"assistant_text","text":"PREVIOUS-CONTENT","data":{}}\n\n' : '';
        return res.end(`${previous}event: stream_unavailable\ndata: {"reason":"${reason}"}\n\n`);
      }
      // A transport can open before its upstream refusal is known. Keep this
      // connection open briefly to make sure the prior reason remains visible
      // during that interval, then refuse it too.
      if (eventRequests === 2) {
        // Actually commit the response headers so the browser EventSource opens
        // before the refusal frame exists (writeHead alone does not send them).
        res.flushHeaders();
        retryHeadersFlushedAt = Date.now();
        return setTimeout(() => res.end(`event: stream_unavailable\ndata: {"reason":"${reason}"}\n\n`), 500);
      }
      res.write('data: {"kind":"assistant_text","text":"恢复后输出","data":{}}\n\n');
      return;
    }
    if (pathname.endsWith('/status')) {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      return res.end(JSON.stringify({ session: 'oaagent.lbl', can_write: false }));
    }
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end('{}');
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve); });
  return {
    url: `http://127.0.0.1:${server.address().port}/view/oaagent.lbl`,
    requests: () => eventRequests,
    retryHeadersFlushedAt: () => retryHeadersFlushedAt,
    close: () => new Promise(resolve => server.close(resolve)),
  };
}

(async () => {
  const checks = regressionCollector();
  const fixture = await startFixture({ previousContent: true });
  const browser = await launchChromium();
  const page = await browser.newPage();
  try {
    await page.goto(fixture.url, { waitUntil: 'domcontentloaded' });
    await checks.check('reasoned unavailable state is visible', async () => {
      await page.waitForFunction(() => document.getElementById('localTime')?.textContent.includes('等待会话输出'));
      const text = await page.locator('#localTime').textContent();
      assert.match(text || '', /^等待会话输出$/);
      assert.ok(!/断线重连中/.test(text || ''), `generic reconnect text leaked: ${text}`);
    });
    await checks.check('unavailable retry preserves already-rendered sequenced content', async () => {
      await page.waitForFunction(() => document.body.textContent.includes('PREVIOUS-CONTENT'));
      assert.ok((await page.locator('body').textContent()).includes('PREVIOUS-CONTENT'));
    });
    await checks.check('unavailable stream retries through backoff', async () => {
      const deadline = Date.now() + 4000;
      while (fixture.requests() < 2 && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 50));
      assert.ok(fixture.requests() >= 2, `event requests=${fixture.requests()}, want retry`);
    });
    await checks.check('reason remains visible while a retry is open but unresolved', async () => {
      const deadline = Date.now() + 4000;
      while (!fixture.retryHeadersFlushedAt() && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20));
      assert.ok(fixture.retryHeadersFlushedAt(), 'retry response headers were never flushed');
      // Sample repeatedly inside the 500ms window between the flushed headers
      // (browser open) and the delayed refusal frame.
      for (let i = 0; i < 8; i++) {
        await new Promise(resolve => setTimeout(resolve, 40));
        if (Date.now() - fixture.retryHeadersFlushedAt() > 450) break;
        const text = await page.locator('#localTime').textContent();
        assert.match(text || '', /^等待会话输出$/, `sample ${i}: ${text}`);
        assert.ok(!/已连接.*等待事件|断线重连中/.test(text || ''), `retry hid unavailable reason: ${text}`);
      }
    });
    await checks.check('first event after unavailable restores normal state', async () => {
      await page.waitForFunction(() => document.body.textContent.includes('恢复后输出'));
      const body = await page.locator('body').textContent();
      const text = await page.locator('#localTime').textContent();
      assert.ok(body.includes('PREVIOUS-CONTENT'), 'reasoned refusal erased existing sequenced content');
      assert.ok(!/等待会话输出|断线重连中|无法识别该 Agent 会话/.test(text || ''), `unavailable state remained: ${text}`);
    });
    await checks.check('unknown refusal reason uses the generic View-service text', async () => {
      const unknownFixture = await startFixture({ reason: 'some_future_reason' });
      const unknownPage = await browser.newPage();
      try {
        await unknownPage.goto(unknownFixture.url, { waitUntil: 'domcontentloaded' });
        await unknownPage.waitForFunction(() => document.getElementById('localTime')?.textContent.includes('View 服务暂时无法获取该会话事件'));
        const text = await unknownPage.locator('#localTime').textContent();
        assert.ok(!/无法识别该 Agent|删除或重命名|等待会话输出/.test(text || ''), `unknown reason mislabelled: ${text}`);
      } finally {
        await unknownPage.close();
        await unknownFixture.close();
      }
    });
    await checks.check('generic upstream refusal is not presented as identity loss', async () => {
      const genericFixture = await startFixture({ reason: 'upstream_events_unavailable' });
      const genericPage = await browser.newPage();
      try {
        await genericPage.goto(genericFixture.url, { waitUntil: 'domcontentloaded' });
        await genericPage.waitForFunction(() => document.getElementById('localTime')?.textContent.includes('View 服务暂时无法获取该会话事件'));
        const text = await genericPage.locator('#localTime').textContent();
        assert.ok(!/无法识别该 Agent|删除或重命名/.test(text || ''), `generic failure was mislabelled as identity loss: ${text}`);
      } finally {
        await genericPage.close();
        await genericFixture.close();
      }
    });
    await checks.check('unavailable event handler remains present', async () => {
      const source = fs.readFileSync(renderer, 'utf8');
      assert.match(source, /addEventListener\('stream_unavailable'/);
      assert.match(source, /streamUnavailableReason = reason/);
    });
    checks.finish();
  } finally {
    await page.close();
    await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
