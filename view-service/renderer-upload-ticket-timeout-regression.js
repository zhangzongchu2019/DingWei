// R-V2-0105-P1-LIST-UPLOAD b：申请上传凭证限时（业主截图「申请上传凭证一直转」）。
// 工装 renderer-replay-seq-fixture.py MODE=ticket：第一次申请凭证按 TICKET 出毛病，之后正常发票、/upload 收字节。
// 钉：凭证请求挂起 ⇒ 默认 20 s 内行内失败、文案为「申请上传凭证超时（20 秒）」、有重试，重试走通；
// 响应头已回、正文不结束 ⇒ 同样超时失败；HTTP 错误文案与超时区分，也可重试；正常票不受影响。
// RENDERER_OVERRIDE 可由环境改指（反向：去掉超时的副本必须红）。
const assert = require('assert');
const net = require('net');
const path = require('path');
const { spawn } = require('child_process');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const FIXTURE = path.join(HERE, 'renderer-replay-seq-fixture.py');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => { const port = server.address().port; server.close(() => resolve(port)); });
  });
}

async function startFixture(ticket) {
  const port = await freePort();
  const env = { ...process.env, RENDERER, MODE: 'ticket', N: '5', READONLY: '0', TICKET: ticket };
  const child = spawn('python3', [FIXTURE, String(port)], { cwd: HERE, env, stdio: ['ignore', 'pipe', 'pipe'] });
  await new Promise((resolve, reject) => {
    child.stdout.once('data', resolve);
    child.once('exit', code => reject(new Error(`fixture exited ${code}`)));
  });
  return { port, stop: () => new Promise(resolve => { child.once('exit', resolve); child.kill('SIGTERM'); }) };
}

const probe = page => page.evaluate(async () => (await fetch('/probe', { cache: 'no-store' })).json());

// node 侧轮询（不用 page.waitForFunction(async …)：它把 Promise 当真值、根本不等）
async function waitFor(page, fn, arg, timeout, what) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const v = await page.evaluate(fn, arg);
    if (v) return v;
    await sleep(100);
  }
  throw new Error(`timed out after ${timeout} ms: ${what}`);
}

const rowState = page => page.evaluate(() => Array.from(document.querySelectorAll('#uploadQueue .upload-row')).map(r => ({
  cls: r.className, meta: (r.querySelector('.u-meta') || {}).textContent || '', retry: !!r.querySelector('.u-retry'),
})));

async function withUpload(browser, ticket, timeoutMs, fn) {
  const fixture = await startFixture(ticket);
  const page = await browser.newPage({ viewport: { width: 1000, height: 760 } });
  try {
    if (timeoutMs) await page.addInitScript(ms => { globalThis.DW_UPLOAD_TICKET_TIMEOUT_MS = ms; }, timeoutMs);
    await page.goto(`http://127.0.0.1:${fixture.port}/renderer`, { waitUntil: 'domcontentloaded' });
    await waitFor(page, () => stats.sessionName === 'smoke-2642' && !stats.readonly, null, 10000, 'status loaded');
    await page.evaluate(() => { window.__up = uploadFiles([new File(['hello upload'], 'a.txt', { type: 'text/plain' })]); });
    return await fn(page);
  } finally {
    await page.close();
    await fixture.stop();
  }
}

// 失败行出现 ⇒ 读文案与重试；点重试 ⇒ 第二张票正常 ⇒ 输入框回填文件引用、失败行让位（草稿块只认同源 /f/，工装是 http，不看它）
async function failThenRetry(page, budgetMs) {
  const t0 = Date.now();
  await waitFor(page, () => !!document.querySelector('#uploadQueue .upload-row.failed'), null, budgetMs, 'failed row');
  const elapsed = Date.now() - t0;
  const failedRows = (await rowState(page)).filter(r => r.cls.includes('failed'));
  await page.click('#uploadQueue .upload-row.failed .u-retry');
  await waitFor(page, () => humanInput.value.includes('https://files.example.invalid/f/smoke/a.txt'), null, 10000, 'file reference in the input after retry');
  const after = await rowState(page);
  const p = await probe(page);
  return { elapsed, failedRows, after, tickets: p.tickets, uploads: p.uploads };
}

(async () => {
  let browser;
  try {
    browser = await launchChromium();
    const checks = regressionCollector();

    const def = await withUpload(browser, 'hang', 0, async page => ({
      ms: await page.evaluate(() => uploadTicketTimeoutMs()),
      ...(await failThenRetry(page, 23000)),
    }));
    await checks.check('U1 凭证请求挂起 ⇒ 默认 20 s 内行内失败、文案「申请上传凭证超时（20 秒）」、可重试且重试走通', async () => {
      assert.strictEqual(def.ms, 20000);
      assert.ok(def.elapsed <= 21500, `failed too late: ${def.elapsed} ms`);
      assert.ok(def.elapsed >= 18000, `failed too early for a 20 s budget: ${def.elapsed} ms`);
      assert.strictEqual(def.failedRows.length, 1);
      assert.strictEqual(def.failedRows[0].meta, '失败 · a.txt：申请上传凭证超时（20 秒）');
      assert.strictEqual(def.failedRows[0].retry, true);
      assert.strictEqual(def.after.filter(r => r.cls.includes('failed')).length, 0, 'failed row must give way to the retry');
      assert.strictEqual(def.tickets, 2);
      assert.strictEqual(def.uploads, 1);
    });

    const body = await withUpload(browser, 'body_hang', 1500, page => failThenRetry(page, 6000));
    await checks.check('U2 响应头已回、正文不结束 ⇒ 计时照样罩住读正文，超时失败、可重试', async () => {
      assert.ok(body.elapsed <= 3500, `failed too late: ${body.elapsed} ms`);
      assert.strictEqual(body.failedRows[0].meta, '失败 · a.txt：申请上传凭证超时（1.5 秒）');
      assert.strictEqual(body.failedRows[0].retry, true);
      assert.strictEqual(body.tickets, 2);
      assert.strictEqual(body.uploads, 1);
    });

    const http = await withUpload(browser, 'http503', 1500, page => failThenRetry(page, 6000));
    await checks.check('U3 HTTP 错误文案与超时区分，同样可重试', async () => {
      assert.strictEqual(http.failedRows[0].meta, '失败 · a.txt：申请上传票据失败 HTTP 503');
      assert.strictEqual(http.failedRows[0].retry, true);
      assert.strictEqual(http.uploads, 1);
    });

    const ok = await withUpload(browser, '', 1500, async page => {
      await waitFor(page, () => humanInput.value.includes('https://files.example.invalid/f/smoke/a.txt'), null, 10000, 'file reference in the input');
      await sleep(2000);   // 超过限时后仍无失败行：计时器在解析完成后已清
      return { rows: await rowState(page), p: await probe(page) };
    });
    await checks.check('U4 正常票不受影响（超过限时后也不冒出失败）', async () => {
      assert.strictEqual(ok.rows.filter(r => r.cls.includes('failed')).length, 0);
      assert.strictEqual(ok.p.tickets, 1);
      assert.strictEqual(ok.p.uploads, 1);
    });

    checks.finish();
    console.log('R-V2-0105 upload-ticket timeout regression: ALL PASS (4/4)');
  } finally {
    if (browser) await browser.close();
  }
})().catch(finishRegression);
