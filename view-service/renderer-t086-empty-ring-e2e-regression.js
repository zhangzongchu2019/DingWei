// T-086 end-to-end guard: real EventSource + real disconnect + fixture-side
// empty ring. This deliberately does not extract/evaluate renderer functions.
const assert = require('assert');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const FIXTURE = path.join(HERE, 'renderer-sse-fixture.py');
const RENDERER = path.join(HERE, 'renderer-canary-sse.html');
const TEXT = '空 ring 重连后必须保留的正文';

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
  });
}

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

async function startFixture(mode) {
  const port = await freePort();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'dw-t086-empty-ring-'));
  const textFile = path.join(dir, 'body.txt');
  fs.writeFileSync(textFile, TEXT, 'utf8');
  const env = {
    ...process.env,
    RENDERER,
    TEXT_FILE: textFile,
    CHUNK: '200',
    DELAY: '0.01',
    RECONNECT: mode === 'replay' ? '1' : '0',
    EMPTY_RING_RECONNECT: mode === 'empty' ? '1' : '0',
  };
  const child = spawn('python3', [FIXTURE, String(port)], { cwd: HERE, env, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', chunk => { output += chunk; });
  child.stderr.on('data', chunk => { output += chunk; });
  const stop = async () => {
    if (child.exitCode === null) child.kill('SIGTERM');
    await new Promise(resolve => {
      if (child.exitCode !== null) return resolve();
      const timer = setTimeout(() => { if (child.exitCode === null) child.kill('SIGKILL'); resolve(); }, 1500);
      child.once('exit', () => { clearTimeout(timer); resolve(); });
    });
    fs.rmSync(dir, { recursive: true, force: true });
  };
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      await stop();
      throw new Error(`fixture exited rc=${child.exitCode}: ${output}`);
    }
    try {
      const response = await fetch(`http://127.0.0.1:${port}/probe`);
      if (response.ok) return { port, stop };
    } catch (_) {}
    await sleep(100);
  }
  await stop();
  throw new Error(`fixture did not start: ${output}`);
}

async function connectionCount(page) {
  return page.evaluate(async () => (await (await fetch('/probe', { cache: 'no-store' })).json()).event_connections);
}

async function waitForConnections(page, count) {
  await page.waitForFunction(async target => {
    const probe = await (await fetch('/probe', { cache: 'no-store' })).json();
    return probe.event_connections >= target;
  }, count, { timeout: 12000 });
}

async function fullText(page) {
  return page.evaluate(() => {
    const group = turnGroups.get('t-1');
    return group ? group.events.filter(event => event.kind === 'assistant_text').map(event => event.text || '').join('') : '';
  });
}

async function runLeg(browser, mode) {
  const fixture = await startFixture(mode);
  const page = await browser.newPage({ viewport: { width: 1000, height: 760 } });
  try {
    await page.goto(`http://127.0.0.1:${fixture.port}/renderer`, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(expected => {
      const group = turnGroups.get('t-1');
      return group && group.events.filter(event => event.kind === 'assistant_text').map(event => event.text || '').join('') === expected;
    }, TEXT, { timeout: 10000 });
    await waitForConnections(page, 2);
    // The delayed-clear hard limit is 1000ms. Empty-ring content must survive
    // beyond it; replay mode must have rebuilt exactly one copy by then.
    await sleep(1300);
    return { text: await fullText(page), connections: await connectionCount(page) };
  } finally {
    await page.close();
    await fixture.stop();
  }
}

(async () => {
  let browser;
  try {
    browser = await launchChromium();
    const checks = regressionCollector();
    const empty = await runLeg(browser, 'empty');
    await checks.check('E1 empty ring reconnect preserves existing body', async () => {
      assert.ok(empty.connections >= 2, `disconnect/reconnect did not occur: ${empty.connections}`);
      assert.strictEqual(empty.text, TEXT, 'empty ring reconnect blanked or changed existing body');
    });
    const replay = await runLeg(browser, 'replay');
    await checks.check('E2 non-empty ring still rebuilds one copy', async () => {
      assert.ok(replay.connections >= 2, `replay reconnect did not occur: ${replay.connections}`);
      assert.strictEqual(replay.text, TEXT, 'deferred clear missing: replay accumulated duplicate body');
    });
    checks.finish();
    console.log('T-086 empty-ring E2E regression: ALL PASS (2/2)');
  } finally {
    if (browser) await browser.close();
  }
})().catch(finishRegression);
