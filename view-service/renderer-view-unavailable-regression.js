// Reasoned upstream SSE refusal must stay visible while retrying with the
// renderer's bounded backoff. This uses a real EventSource in Chromium; a
// synthetic callback would not exercise the close/retry ordering.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const renderer = process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html');

async function startFixture() {
  const html = fs.readFileSync(renderer);
  let eventRequests = 0;
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://fixture.invalid').pathname;
    if (pathname === '/view/oaagent.lbl') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      return res.end(html);
    }
    if (pathname === '/view/oaagent.lbl/events') {
      eventRequests += 1;
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' });
      return res.end('event: stream_unavailable\ndata: {"reason":"event_stream_unavailable"}\n\n');
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
    close: () => new Promise(resolve => server.close(resolve)),
  };
}

(async () => {
  const checks = regressionCollector();
  const fixture = await startFixture();
  const browser = await launchChromium();
  const page = await browser.newPage();
  try {
    await page.goto(fixture.url, { waitUntil: 'domcontentloaded' });
    await checks.check('reasoned unavailable state is visible', async () => {
      await page.waitForFunction(() => document.getElementById('localTime')?.textContent.includes('事件流尚未注册'));
      const text = await page.locator('#localTime').textContent();
      assert.match(text || '', /^该 Agent 会话当前不可用：事件流尚未注册$/);
      assert.ok(!/断线重连中/.test(text || ''), `generic reconnect text leaked: ${text}`);
    });
    await checks.check('unavailable stream retries through backoff', async () => {
      const deadline = Date.now() + 4000;
      while (fixture.requests() < 2 && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 50));
      assert.ok(fixture.requests() >= 2, `event requests=${fixture.requests()}, want retry`);
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
