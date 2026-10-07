// R-V2-0105 件一 端到端：真 EventSource＋真断线重连＋按 Hub 序号语义的工装（renderer-replay-seq-fixture.py）。
// 钉：慢重连（首条重放 >1 s）不叠加；连续重连正文不变；重连带 after；中间层丢 after 仍不叠加；
// 换纪元保留内容、不提示；溢出提示；实时跳号补缺且不重复；跳号风暴补缺次数受限并提示；回合组合并不改正文。
const assert = require('assert');
const net = require('net');
const path = require('path');
const { spawn } = require('child_process');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const FIXTURE = path.join(HERE, 'renderer-replay-seq-fixture.py');
const RENDERER = path.join(HERE, 'renderer-canary-sse.html');
const N = 30;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const tag = s => `[${String(s).padStart(3, '0')}]`;
const range = (a, b) => Array.from({ length: b - a + 1 }, (_, i) => a + i);
const expected = seqs => seqs.map(tag).join('');

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => { const port = server.address().port; server.close(() => resolve(port)); });
  });
}

async function startFixture(mode, extra = {}) {
  const port = await freePort();
  const env = { ...process.env, RENDERER, MODE: mode, N: String(N), ...extra };
  const child = spawn('python3', [FIXTURE, String(port)], { cwd: HERE, env, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', c => { output += c; });
  child.stderr.on('data', c => { output += c; });
  const stop = async () => {
    if (child.exitCode === null) child.kill('SIGTERM');
    await new Promise(resolve => {
      if (child.exitCode !== null) return resolve();
      const timer = setTimeout(() => { if (child.exitCode === null) child.kill('SIGKILL'); resolve(); }, 1500);
      child.once('exit', () => { clearTimeout(timer); resolve(); });
    });
  };
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) { await stop(); throw new Error(`fixture exited: ${output}`); }
    try { if ((await fetch(`http://127.0.0.1:${port}/probe`)).ok) return { port, stop }; } catch (_) {}
    await sleep(100);
  }
  await stop();
  throw new Error(`fixture did not start: ${output}`);
}

const probe = page => page.evaluate(async () => (await fetch('/probe', { cache: 'no-store' })).json());
const turnText = (page, turn = 't-1') => page.evaluate(t => {
  const g = turnGroups.get(t);
  if (!g) return '';
  const evs = g.events.filter(e => e.kind === 'assistant_text').slice().sort(compareCursor);
  return evs.map(e => e.text || '').join('');
}, turn);
const alertState = page => page.evaluate(() => ({ hidden: streamAlert.hidden, mode: streamAlertMode, text: streamAlertText.textContent }));

async function withPage(browser, mode, extra, fn, init) {
  const fixture = await startFixture(mode, extra);
  const page = await browser.newPage({ viewport: { width: 1000, height: 760 } });
  try {
    if (init) await page.addInitScript(init);
    await page.goto(`http://127.0.0.1:${fixture.port}/renderer`, { waitUntil: 'domcontentloaded' });
    return await fn(page);
  } finally {
    await page.close();
    await fixture.stop();
  }
}

// 🔴 不用 page.waitForFunction(async …)：它把返回的 Promise 当真值，立即结束、根本没等（首版 S1 即因此只等到 2 次）。
async function waitConnections(page, count, timeout = 30000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if ((await probe(page)).event_connections >= count) return;
    await sleep(200);
  }
  throw new Error(`connections did not reach ${count}: ${(await probe(page)).event_connections}`);
}

(async () => {
  let browser;
  try {
    browser = await launchChromium();
    const checks = regressionCollector();
    const base = expected(range(2, N));

    const r1 = await withPage(browser, 'reconnect', { RECONNECTS: '6', FIRST_DELAY: '1.5' }, async page => {
      await waitConnections(page, 7);
      await sleep(2500);
      return { text: await turnText(page), probe: await probe(page), alert: await alertState(page) };
    });
    await checks.check('S1 慢重连（首条重放 1.5 s）连续 6 次后正文不变、不叠加', async () => {
      assert.ok(r1.probe.event_connections >= 7, `reconnects did not happen: ${r1.probe.event_connections}`);
      assert.strictEqual(r1.text, base, `text grew or changed: len ${r1.text.length} vs ${base.length}`);
    });
    await checks.check('S2 重连请求带 after＝<纪元>-<已见末序号>', async () => {
      assert.strictEqual(r1.probe.afters[0], '', 'first connect must not carry after');
      for (const a of r1.probe.afters.slice(1)) assert.strictEqual(a, `a1b2c3d4-${N}`, `after=${a}`);
    });
    await checks.check('S3 有序号时不再走 1 秒清空窗口（不弹「重放超时」）', async () => {
      assert.ok(r1.alert.hidden || r1.alert.mode !== 'replay', `replay alert shown: ${JSON.stringify(r1.alert)}`);
    });

    const r2 = await withPage(browser, 'no_after', { RECONNECTS: '4', FIRST_DELAY: '1.5' }, async page => {
      await waitConnections(page, 5);
      await sleep(2500);
      return { text: await turnText(page) };
    });
    await checks.check('S4 发布错位：中间层丢 after、Hub 整份重放 ⇒ 页面按 id 跳过已见、不叠加', async () => {
      assert.strictEqual(r2.text, base, `len ${r2.text.length} vs ${base.length}`);
    });

    const r3 = await withPage(browser, 'epoch', {}, async page => {
      await waitConnections(page, 2);
      await page.waitForFunction(() => { const g = turnGroups.get('t-1'); return g && g.events.some(e => e.text === '[NEW]'); }, null, { timeout: 15000 });
      await sleep(800);
      return { text: await turnText(page), alert: await alertState(page) };
    });
    await checks.check('S5 换纪元（Hub 重启／helper 重连重建）保留原内容、新事件只一份、不提示', async () => {
      assert.strictEqual(r3.text, base + '[NEW]', r3.text);
      assert.ok(r3.alert.hidden, `unexpected alert: ${JSON.stringify(r3.alert)}`);
    });

    const r4 = await withPage(browser, 'overflow', {}, async page => {
      await waitConnections(page, 2);
      await page.waitForFunction(n => { const g = turnGroups.get('t-1'); return g && g.events.some(e => e.text === `[${String(n + 5).padStart(3, '0')}]`); }, N, { timeout: 15000 });
      await sleep(800);
      return { text: await turnText(page), alert: await alertState(page) };
    });
    await checks.check('S6 溢出：先提示「显示可能不全」，ring 内新事件照收、不重复', async () => {
      assert.strictEqual(r4.text, base + expected(range(N + 3, N + 5)), r4.text);
      assert.ok(!r4.alert.hidden && r4.alert.mode === 'gap', `gap alert missing: ${JSON.stringify(r4.alert)}`);
    });

    const r5 = await withPage(browser, 'live_gap', {}, async page => {
      await waitConnections(page, 2, 20000);
      await page.waitForFunction(n => { const g = turnGroups.get('t-1'); return g && g.events.some(e => e.text === `[${String(n + 1).padStart(3, '0')}]`); }, N, { timeout: 20000 });
      await sleep(800);
      return { text: await turnText(page), probe: await probe(page) };
    });
    await checks.check('S7 实时跳号 ⇒ 合并等待后带 after 补缺，缺口补上且已显示的不重复', async () => {
      assert.strictEqual(r5.text, expected(range(2, N + 2)), r5.text);
      assert.strictEqual(r5.probe.afters[1], `a1b2c3d4-${N}`, `refill after=${r5.probe.afters[1]}`);
    });

    const r6 = await withPage(browser, 'gap_storm', {}, async page => {
      await sleep(16000);
      return { probe: await probe(page), alert: await alertState(page) };
    });
    await checks.check('S8 跳号风暴：补缺重连 5 分钟内至多 3 次，之后停止并提示', async () => {
      assert.ok(r6.probe.event_connections <= 4, `refill reconnects not capped: ${r6.probe.event_connections}`);
      assert.ok(r6.probe.event_connections >= 2, 'refill never attempted');
      assert.ok(!r6.alert.hidden && r6.alert.mode === 'gap', `gap alert missing after cap: ${JSON.stringify(r6.alert)}`);
    });

    const r7 = await withPage(browser, 'reconnect', { RECONNECTS: '0' }, async page => {
      await page.waitForFunction(() => turnGroups.get('t-1'), null, { timeout: 10000 });
      return page.evaluate(() => {
        const out = {};
        const feed = (turn, n, mk) => { for (let i = 0; i < n; i++) handleIncomingEvent(mk(i)); };
        // claude／opencode 形态：每片 cursor 不同
        feed('m-1', 350, i => ({ kind: 'assistant_text', text: `<${i}>`, cursor: String(i).padStart(6, '0'), delivery_id: 'dm', turn_id: 'm-1', ts: 0 }));
        const g1 = turnGroups.get('m-1');
        out.len1 = g1.events.length;
        out.text1 = g1.events.filter(e => e.kind === 'assistant_text').slice().sort(compareCursor).map(e => e.text).join('');
        // Codex 形态：同 cursor、相同文本的多片
        feed('m-2', 250, () => ({ kind: 'assistant_text', text: '的', cursor: 'turn:m-2:item:0003', delivery_id: 'dn', turn_id: 'm-2', ts: 0 }));
        const g2 = turnGroups.get('m-2');
        out.len2 = g2.events.length;
        out.text2 = g2.events.map(e => e.text).join('');
        return out;
      });
    }, () => { window.TURN_EVENTS_SOFT_CAP = 100; });
    await checks.check('S9 回合组到软上限合并（不同 cursor）后正文逐字相同、条数下降', async () => {
      assert.ok(r7.len1 < 350, `not compacted: ${r7.len1}`);
      assert.strictEqual(r7.text1, Array.from({ length: 350 }, (_, i) => `<${i}>`).join(''));
    });
    await checks.check('S10 同 cursor 相同文本的多片合并后不丢一片', async () => {
      assert.ok(r7.len2 < 250, `not compacted: ${r7.len2}`);
      assert.strictEqual(r7.text2, '的'.repeat(250));
    });

    checks.finish();
    console.log('R-V2-0105 replay-seq regression: ALL PASS (10/10)');
  } finally {
    if (browser) await browser.close();
  }
})().catch(finishRegression);
