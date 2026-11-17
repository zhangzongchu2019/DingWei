// S-R61-N2 端到端：Claude 席回合外输出折叠条（设计 docs/reviews/r61-n2-offturn-fold/DESIGN.md）。
// G1 W2 折叠/默认折叠/展开全量；G2 W1 折叠＋标题逐字；G3 边界（回合/输入/别席/错误/缺口断开，隐藏卡与原位更新不断）；
// G4 双闸（闸 A 事件形状、闸 B 会话级 Codex 标记）；G5 有序号重连不叠加；G6 旧 Hub 重连清空重建字节一致；
// G7 历史分页合并/缺口拆分/bootstrap 不重复；G8 逐出无泄漏；G9 折叠条无重发按钮无来源图标；G10 不吞正文。
// 每格附删保护刀：对被测保护做字符串变异，期望该格转红；变异后仍绿＝保护未被覆盖，套件失败。
const assert = require('assert');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const { finishRegression, launchChromium, regressionCollector } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const FIXTURE = path.join(HERE, 'renderer-offturn-fold-fixture.py');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const N = 12;
const TITLE = '回合外输出（Claude）';
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const tag = seq => `[${String(seq).padStart(3, '0')}]`;
const tags = (from, to) => { let out = ''; for (let seq = from; seq <= to; seq += 1) out += tag(seq); return out; };

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => { const port = server.address().port; server.close(() => resolve(port)); });
  });
}

async function startFixture(mode, extra, renderer) {
  const port = await freePort();
  const env = { ...process.env, RENDERER: renderer, MODE: mode, N: String(N), ...extra };
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

const foldRead = page => page.evaluate(() => ({
  groups: [...offturnGroups.values()].map(group => ({
    key: group.key,
    count: group.events.length,
    buffer: group.buffer,
    open: group.node.open,
    connected: group.node.isConnected,
    sealed: group.sealed,
    title: group.node.querySelector('.offturn-fold-title')?.textContent || '',
    meta: group.node.querySelector('.offturn-fold-meta')?.textContent || '',
    hasRetry: !!group.node.querySelector('.waiting-retry-btn'),
    hasProvIcon: !!group.node.querySelector('.prov-ic'),
    bodyText: group.body.textContent,
  })),
  domFolds: [...timeline.children].filter(node => node.dataset && node.dataset.offturnKey).map(node => node.dataset.offturnKey),
  codexSeen: offturnCodexSeen,
  assistantCards: messageNodes.size,
  detachedPending: [...pendingRenders].filter(entry => entry.detached).length,
  timelineNodes: timeline.children.length,
}));

async function expandFolds(page) {
  await page.evaluate(() => { for (const group of offturnGroups.values()) group.node.open = true; });
  await sleep(300);
}

async function waitConnections(page, count, timeout = 30000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if ((await probe(page)).event_connections >= count) return;
    await sleep(200);
  }
  throw new Error(`connections did not reach ${count}: ${(await probe(page)).event_connections}`);
}

async function withPage(browser, mode, extra, fn, opts = {}) {
  const fixture = await startFixture(mode, extra, opts.renderer || RENDERER);
  const page = await browser.newPage({ viewport: opts.viewport || { width: 1000, height: 760 } });
  try {
    await page.goto(`http://127.0.0.1:${fixture.port}/renderer`, { waitUntil: 'domcontentloaded' });
    return await fn(page);
  } finally {
    await page.close();
    await fixture.stop();
  }
}

const totalOffturn = () => [...offturnGroups.values()].reduce((sum, group) => sum + group.events.length, 0);

async function scW2(browser, renderer) {
  return withPage(browser, 'w2', {}, async page => {
    await page.waitForFunction(() => offturnGroups.size === 1 && [...offturnGroups.values()][0].events.length === 12, null, { timeout: 15000 });
    await sleep(350);
    const before = await foldRead(page);
    await expandFolds(page);
    const after = await foldRead(page);
    return { before, after, expected: tags(5, 16) };
  }, { renderer });
}
function assertW2(readings) {
  const { before, after, expected } = readings;
  assert.strictEqual(before.groups.length, 1, `expected one fold, got ${before.groups.length}`);
  assert.strictEqual(before.groups[0].count, 12, `count ${before.groups[0].count}`);
  assert.strictEqual(before.groups[0].open, false, 'fold must default collapsed');
  assert.strictEqual(before.groups[0].buffer, expected, 'collapsed buffer mismatch');
  assert.strictEqual(after.groups[0].buffer, expected, 'expanded buffer mismatch');
  for (let seq = 5; seq <= 16; seq += 1) {
    assert.ok(after.groups[0].bodyText.includes(tag(seq)), `expanded body missing ${tag(seq)}`);
  }
}

async function scW1(browser, renderer) {
  return withPage(browser, 'w1', {}, async page => {
    await page.waitForFunction(() => offturnGroups.size === 1 && [...offturnGroups.values()][0].events.length === 12, null, { timeout: 15000 });
    await sleep(350);
    return foldRead(page);
  }, { renderer });
}
function assertW1(readings) {
  assert.strictEqual(readings.groups.length, 1, `expected one fold, got ${readings.groups.length}`);
  assert.strictEqual(readings.groups[0].count, 12);
  assert.strictEqual(readings.groups[0].title, TITLE, `title not verbatim: ${readings.groups[0].title}`);
  assert.strictEqual(readings.groups[0].buffer, tags(1, 12));
  assert.ok(readings.groups[0].meta.startsWith('12 条'), `meta: ${readings.groups[0].meta}`);
}

async function scBoundary(browser, variant, renderer) {
  return withPage(browser, 'boundary', { BOUNDARY: variant }, async page => {
    await page.waitForFunction(() => [...offturnGroups.values()].reduce((sum, group) => sum + group.events.length, 0) === 8, null, { timeout: 15000 });
    await sleep(400);
    return foldRead(page);
  }, { renderer });
}
function assertBoundarySplit(readings) {
  assert.strictEqual(readings.groups.length, 2, `boundary must split runs: got ${readings.groups.length} fold(s)`);
  assert.deepStrictEqual(readings.groups.map(group => group.count).sort(), [4, 4]);
}
function assertBoundaryJoin(readings) {
  assert.strictEqual(readings.groups.length, 1, `non-boundary must keep one run: got ${readings.groups.length}`);
  assert.strictEqual(readings.groups[0].count, 8);
}

async function scGapBoundary(browser, renderer) {
  return withPage(browser, 'boundary_delay', {}, async page => {
    await page.waitForFunction(() => [...offturnGroups.values()].reduce((sum, group) => sum + group.events.length, 0) === 4, null, { timeout: 15000 });
    await page.evaluate(() => handleIncomingEvent({ kind: 'state_change', _history_gap: true, data: {} }));
    await page.waitForFunction(() => [...offturnGroups.values()].reduce((sum, group) => sum + group.events.length, 0) === 8, null, { timeout: 15000 });
    await sleep(400);
    return foldRead(page);
  }, { renderer });
}

async function scGateA(browser, renderer) {
  return withPage(browser, 'gatea', {}, async page => {
    await page.waitForFunction(() => messageNodes.size === 6, null, { timeout: 15000 });
    await sleep(350);
    return foldRead(page);
  }, { renderer });
}
function assertGateA(readings) {
  assert.strictEqual(readings.groups.length, 0, 'gate A: Codex-marked shape must not fold');
  assert.strictEqual(readings.assistantCards, 6);
}

async function scGateB(browser, renderer) {
  return withPage(browser, 'gateb', {}, async page => {
    await page.waitForFunction(() => offturnCodexSeen && messageNodes.size === 12, null, { timeout: 15000 });
    await sleep(350);
    return foldRead(page);
  }, { renderer });
}
function assertGateB(readings) {
  assert.strictEqual(readings.codexSeen, true, 'gate B must latch on Codex markers');
  assert.strictEqual(readings.groups.length, 0, 'gate B: whole session must unfold');
  assert.strictEqual(readings.assistantCards, 12);
}

async function scReconnect(browser, renderer) {
  return withPage(browser, 'reconnect', {}, async page => {
    await waitConnections(page, 2);
    await page.waitForFunction(() => offturnGroups.size === 1 && [...offturnGroups.values()][0].events.length === 12, null, { timeout: 15000 });
    await sleep(800);
    return { readings: await foldRead(page), probe: await probe(page) };
  }, { renderer });
}
function assertReconnect(result) {
  const { readings, probe } = result;
  assert.strictEqual(readings.groups.length, 1, `reconnect doubled folds: ${readings.groups.length}`);
  assert.strictEqual(readings.groups[0].count, 12, `reconnect doubled count: ${readings.groups[0].count}`);
  assert.strictEqual(readings.groups[0].buffer, tags(1, 12));
  assert.strictEqual(probe.afters[0], '', 'first connect must not carry after');
  assert.strictEqual(probe.afters[1], `a1b2c3d4-${N}`, `after=${probe.afters[1]}`);
}

async function scLegacy(browser, ring, renderer) {
  return withPage(browser, 'legacy', { LEGACY_RING: ring }, async page => {
    await waitConnections(page, 2);
    await sleep(1200);
    return foldRead(page);
  }, { renderer });
}
function assertLegacySame(readings) {
  assert.strictEqual(readings.groups.length, 1, `legacy rebuild folds: ${readings.groups.length}`);
  assert.strictEqual(readings.groups[0].count, 12);
  assert.strictEqual(readings.groups[0].buffer, tags(1, 12), 'legacy rebuild must be byte-identical');
  assert.strictEqual(readings.detachedPending, 0);
}
function assertLegacyNew(readings) {
  assert.strictEqual(readings.groups.length, 1, `stale fold state after reset: ${readings.groups.length}`);
  assert.strictEqual(readings.groups[0].count, 1);
  assert.strictEqual(readings.groups[0].buffer, '[NEW]');
}

async function scSplit(browser, renderer) {
  return withPage(browser, 'split', {}, async page => {
    await waitConnections(page, 2);
    await page.waitForFunction(() => turnGroups.has('t-x'), null, { timeout: 15000 });
    await sleep(600);
    const readings = await foldRead(page);
    const order = await page.evaluate(() => [...timeline.children].map(node => node.dataset.offturnKey ? 'fold' : (node.dataset.turnKey === 't-x' ? 'intruder' : 'other')));
    return { readings, order };
  }, { renderer });
}
function assertSplit(result) {
  const { readings, order } = result;
  assert.strictEqual(readings.groups.length, 2, `gap-fill intruder must split fold: ${readings.groups.length}`);
  const byKey = new Map(readings.groups.map(group => [group.key, group]));
  const ordered = readings.domFolds.map(key => byKey.get(key));
  assert.deepStrictEqual(ordered.map(group => group.buffer), [tags(3, 4), tags(6, 12)]);
  const intruderIndex = order.indexOf('intruder');
  assert.ok(intruderIndex > 0 && order[intruderIndex - 1] === 'fold' && order[intruderIndex + 1] === 'fold', `intruder not between folds: ${order.join(',')}`);
}

async function scHistory(browser, renderer) {
  return withPage(browser, 'history', {}, async page => {
    await page.waitForFunction(() => offturnGroups.size === 1 && [...offturnGroups.values()][0].events.length === 22, null, { timeout: 20000 });
    const initial = await foldRead(page);
    await page.click('#historyOlder');
    await page.waitForFunction(() => offturnGroups.size === 2, null, { timeout: 20000 });
    await sleep(500);
    const paged = await foldRead(page);
    return { initial, paged, probe: await probe(page) };
  }, { renderer });
}
function assertHistory(result) {
  const { initial, paged, probe } = result;
  assert.strictEqual(initial.groups.length, 1, `initial merge: ${initial.groups.length}`);
  assert.strictEqual(initial.groups[0].count, 22, `bootstrap double-append or lost events: ${initial.groups[0].count}`);
  assert.strictEqual(initial.groups[0].buffer, tags(20, 41));
  assert.strictEqual(paged.groups.length, 2, `paged runs: ${paged.groups.length}`);
  const byKey = new Map(paged.groups.map(group => [group.key, group]));
  const ordered = paged.domFolds.map(key => byKey.get(key));
  assert.deepStrictEqual(ordered.map(group => group.count), [5, 26]);
  assert.deepStrictEqual(ordered.map(group => group.buffer), [tags(10, 14), tags(16, 41)]);
  assert.deepStrictEqual(probe.history_requests, ['', '20']);
}

async function scEvict(browser, renderer) {
  return withPage(browser, 'evict', {}, async page => {
    await sleep(2500);
    return foldRead(page);
  }, { renderer, viewport: { width: 390, height: 700 } });
}
function assertEvict(readings) {
  assert.ok(readings.timelineNodes <= 20, `cap not enforced: ${readings.timelineNodes}`);
  assert.ok(readings.groups.length > 0, 'all folds evicted unexpectedly');
  assert.ok(readings.groups.length < 30, 'eviction did not happen');
  assert.strictEqual(readings.groups.length, readings.domFolds.length, `map leak: ${readings.groups.length} groups vs ${readings.domFolds.length} nodes`);
  assert.ok(readings.groups.every(group => group.connected), 'evicted group still indexed');
  assert.strictEqual(readings.detachedPending, 0, 'detached entry left in render queue');
}

function assertNoRetryNoIcon(readings) {
  assert.ok(readings.groups.length >= 1, 'fold must exist');
  for (const group of readings.groups) {
    assert.strictEqual(group.hasRetry, false, 'fold must not carry retry button');
    assert.strictEqual(group.hasProvIcon, false, 'fold must not carry provenance icon');
  }
}
function assertNoSwallow(readings) {
  const { after, expected } = readings;
  assert.strictEqual(after.groups[0].buffer.length, expected.length, `swallowed text: ${after.groups[0].buffer.length} vs ${expected.length}`);
  assert.strictEqual(after.groups[0].buffer, expected);
}

function mutate(source, from, to) {
  const parts = source.split(from);
  if (parts.length !== 2) throw new Error(`knife anchor matched ${parts.length - 1} times: ${from.slice(0, 70)}`);
  return parts.join(to);
}

(async () => {
  let browser;
  const knifeDir = fs.mkdtempSync(path.join(os.tmpdir(), 'offturn-knife-'));
  try {
    browser = await launchChromium();
    const checks = regressionCollector();
    const baseSource = fs.readFileSync(RENDERER, 'utf8');
    let knifeSerial = 0;
    const knifeFile = (from, to) => {
      const file = path.join(knifeDir, `knife-${knifeSerial++}.html`);
      fs.writeFileSync(file, mutate(baseSource, from, to));
      return file;
    };
    async function expectRed(name, file, scenario, assertion) {
      await checks.check(name, async () => {
        let readings;
        try { readings = await scenario(file); } catch (err) { return; }
        try { assertion(readings); } catch (err) { return; }
        throw new Error('mutation stayed green — delete-protection not covered by this grid');
      });
    }

    const w2 = await scW2(browser);
    await checks.check('G1 W2 回合结束后连续回合外输出合并为一条、默认折叠、展开即全量拼接', async () => assertW2(w2));
    await checks.check('G9 折叠条不带重发按钮、不带来源图标', async () => assertNoRetryNoIcon(w2.before));
    await checks.check('G10 折叠条不吞正文（展开长度＝逐条之和）', async () => assertNoSwallow(w2));

    const w1 = await scW1(browser);
    await checks.check('G2 W1 首段投递前窗口折叠，标题逐字「回合外输出（Claude）」', async () => assertW1(w1));

    for (const variant of ['turn', 'input', 'mention', 'error']) {
      const readings = await scBoundary(browser, variant);
      await checks.check(`G3 边界断开：${variant}`, async () => assertBoundarySplit(readings));
    }
    for (const variant of ['hidden', 'update']) {
      const readings = await scBoundary(browser, variant);
      await checks.check(`G3 不断开：${variant}`, async () => assertBoundaryJoin(readings));
    }
    const gapBoundary = await scGapBoundary(browser);
    await checks.check('G3 边界断开：历史缺口提示', async () => assertBoundarySplit(gapBoundary));

    const gateA = await scGateA(browser);
    await checks.check('G4 闸 A：带 Codex 数据标记的无归属输出不折叠', async () => assertGateA(gateA));
    const gateB = await scGateB(browser);
    await checks.check('G4 闸 B：会话出现 Codex 标记即整体回落现状', async () => assertGateB(gateB));

    const reconnect = await scReconnect(browser);
    await checks.check('G5 有序号重连（整份重放）不叠加、after 正确', async () => assertReconnect(reconnect));

    const legacySame = await scLegacy(browser, 'same');
    await checks.check('G6 旧 Hub 重连清空重建后字节一致', async () => assertLegacySame(legacySame));
    const legacyNew = await scLegacy(browser, 'new');
    await checks.check('G6 旧 Hub 重连换 ring 后无残留折叠状态', async () => assertLegacyNew(legacyNew));

    const split = await scSplit(browser);
    await checks.check('G7 补缺回合卡落在折叠段内则拆分该段', async () => assertSplit(split));
    const history = await scHistory(browser);
    await checks.check('G7 历史分页确定性合并＋bootstrap 不重复', async () => assertHistory(history));

    const evict = await scEvict(browser);
    await checks.check('G8 逐出整节点移除、索引与渲染队列无泄漏', async () => assertEvict(evict));

    // ── 删保护刀：每格至少一刀，变异后该格必须转红 ──
    // 闸 A 的标记 clause 与闸 B 共用同一检测器（单源）；只删 clause 会被闸 B 兜住，故刀切检测器本体。
    await expectRed('M1 删 Codex 标记检测器（闸 A 形状闸核心）⇒ G4 闸 A 红',
      knifeFile('    function offturnCodexMarkers(data) {\n      if (!data || typeof data !== \'object\') return false;',
        '    function offturnCodexMarkers(data) {\n      if (true) return false;'),
      file => scGateA(browser, file), assertGateA);
    await expectRed('M2 删闸 B 会话扫描 ⇒ G4 闸 B 红',
      knifeFile('      if (offturnCodexSeen || !offturnCodexMarkers(ev?.data)) return;', '      if (true) return;'),
      file => scGateB(browser, file), assertGateB);
    await expectRed('M3 删边界判定 ⇒ G3 错误边界红',
      knifeFile('      if (HIDDEN_TIMELINE_CARD_KINDS.has(kind) && !turnKeyFor(ev)) return false;\n      return true;\n    }',
        '      if (HIDDEN_TIMELINE_CARD_KINDS.has(kind) && !turnKeyFor(ev)) return false;\n      return false;\n    }'),
      file => scBoundary(browser, 'error', file), assertBoundarySplit);
    await expectRed('M4 删合并封口检查 ⇒ G3 错误边界红',
      knifeFile('        if (!left || !right || left.sealed) { node = next; continue; }', '        if (!left || !right) { node = next; continue; }'),
      file => scBoundary(browser, 'error', file), assertBoundarySplit);
    await expectRed('M5 删合并 pass ⇒ G7 分页合并红',
      knifeFile('      offturnMergePass();', '      void 0;'),
      file => scHistory(browser, file), assertHistory);
    await expectRed('M6 删拆分 pass ⇒ G7 缺口拆分红',
      knifeFile('      while (offturnSplitPass() && guard < 32) guard += 1;', '      void 0;'),
      file => scSplit(browser, file), assertSplit);
    await expectRed('M7 删逐出索引回收 ⇒ G8 泄漏红',
      knifeFile(`      if (ds.offturnKey) {
        const group = offturnGroups.get(ds.offturnKey);
        if (group) {
          group.detached = true;
          pendingRenders.delete(group);
          if (openOffturnGroup === group) openOffturnGroup = null;
        }
        offturnGroups.delete(ds.offturnKey);
      }
`, ''),
      file => scEvict(browser, file), assertEvict);
    await expectRed('M8 删重置时折叠状态清空 ⇒ G6 换 ring 残留红',
      knifeFile(`      offturnGroups.clear();
      openOffturnGroup = null;
      offturnCodexSeen = false;
      offturnSessionCli = null;
      if (offturnRecomputeTimer !== null) { clearTimeout(offturnRecomputeTimer); offturnRecomputeTimer = null; }
`, ''),
      file => scLegacy(browser, 'new', file), assertLegacyNew);
    await expectRed('M9 默认展开 ⇒ G1 默认折叠红',
      knifeFile('      node.open = open;', '      node.open = true;'),
      file => scW2(browser, file), assertW2);
    await expectRed('M10 吞正文 ⇒ G10 长度红',
      knifeFile("      group.buffer += ev.text || '';", "      group.buffer += '';"),
      file => scW2(browser, file), assertNoSwallow);
    await expectRed('M11 改标题 ⇒ G2 逐字红',
      knifeFile(`    const OFFTURN_TITLE = '${TITLE}';`, "    const OFFTURN_TITLE = '回合外输出';"),
      file => scW1(browser, file), assertW1);
    await expectRed('M12 删折叠路由 ⇒ G9 折叠存在性红',
      knifeFile('        if (!offturnCodexSeen && isOffturnCandidate(ev)) return renderOffturn(ev);\n', ''),
      file => scW2(browser, file), readings => assertNoRetryNoIcon(readings.before));

    checks.finish();
    console.log('offturn fold regression: all grids green, all knives red');
  } catch (err) {
    finishRegression(err);
  } finally {
    fs.rmSync(knifeDir, { recursive: true, force: true });
    if (browser) await browser.close();
  }
})();
