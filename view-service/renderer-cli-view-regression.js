// S-R62-CLI-VIEW-B：终端风格第二投影（Codex CLI 版式）回归套件。
// 设计页：docs/reviews/r62-cli-view-b/DESIGN.md（§八 G1～G12＋K1～K12；V1 追加窄屏关态往返格）。
// 加性双投影：关态与基锚 7d3035c6 的 #timeline innerHTML＋body class 逐字节同一（G2）；
// 开态映射/来源/失败/长文/窄屏/重连逐出/覆盖位/洪流各有判据与删保护刀。
// 刀机制与现套件同轨：MUTATION 环境变量选刀，锚缺失即 throw（不假绿）。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { execFileSync } = require('child_process');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

const HERE = __dirname;
const BASE_ANCHOR = '7d3035c6';   // 手机单冻结 tip（原型基锚）；G2 字节同一对照源
const MUTATION = process.env.MUTATION || '';
const SHOT_DIR = process.env.CLI_VIEW_SHOT_DIR || '';
const RATE = 6000;
const SOFT_CAP = process.env.MUTATION === 'cli-view-threshold' ? 1000000000 : 20000;   // K10 刀：放宽⇒自检红
const MARGIN = 100;
const DEADLINE_S = Number(process.env.STRESS_DEADLINE_S || 180);
const sleep = ms => new Promise(r => setTimeout(r, ms));

// K10 判据漂移自检：阈值/场景必须与 flood 套件本体逐字同（flood 本体不改）。
function thresholdSelfCheck() {
  const src = fs.readFileSync(path.join(HERE, 'renderer-flood-stress-regression.js'), 'utf8');
  const grab = re => { const m = src.match(re); assert.ok(m, `flood 套件阈值锚缺失: ${re}`); return Number(m[1]); };
  assert.strictEqual(grab(/const RATE = (\d+);/), RATE, 'K10 自检：RATE 与 flood 套件漂移');
  assert.strictEqual(grab(/const SOFT_CAP = (\d+);/), SOFT_CAP, 'K10 自检：SOFT_CAP 与 flood 套件漂移（阈值放宽刀须红在这里）');
  assert.strictEqual(grab(/const MARGIN = (\d+);/), MARGIN, 'K10 自检：MARGIN 与 flood 套件漂移');
  assert.ok(src.includes("{ name: 'flood', total: 100000, lateCursor: 0, lateAfter: 0 }"), 'K10 自检：flood 场景锚缺失');
  assert.ok(src.includes("{ name: 'late-order', total: 30000, lateCursor: 12000, lateAfter: 21000 }"), 'K10 自检：late-order 场景锚缺失');
  const rsrc = fs.readFileSync(path.join(HERE, 'renderer-canary-sse.html'), 'utf8');
  assert.ok(rsrc.includes("const LONGTEXT_THRESHOLD = (typeof window !== 'undefined' && window.DW_LONGTEXT_THRESHOLD) || 4000;"), 'K10 自检：LONGTEXT_THRESHOLD 锚缺失/漂移');
  assert.ok(rsrc.includes("const LONGTEXT_EXCERPT = (typeof window !== 'undefined' && window.DW_LONGTEXT_EXCERPT) || 1200;"), 'K10 自检：LONGTEXT_EXCERPT 锚缺失/漂移');
}

function baseRenderer() {
  try {
    return execFileSync('git', ['show', `${BASE_ANCHOR}:view-service/renderer-canary-sse.html`], { cwd: HERE, maxBuffer: 64 * 1024 * 1024 });
  } catch (err) {
    throw new Error(`G2 基锚缺失（${BASE_ANCHOR} 不可达）——不假绿: ${err.message}`);
  }
}

// 删保护刀：每把锚定唯一上下文，替换未生效即 throw（mutant-revive 教训：锚漂移假绿）。
const KNIVES = {
  'cli-view-offstate-add': {
    find: 'if (cliViewOn()) rebuildTurnCLI(group);   // S-R62-CLI-VIEW-B：关态零副作用',
    replace: 'rebuildTurnCLI(group); document.body.classList.add("cli-view-on");   // K1：关态加性破坏',
  },
  'cli-view-toggle-wire': {
    find: "cliViewBtn?.addEventListener('click', () => {\n      if (cliViewLocked()) return;",
    replace: "cliViewBtn?.addEventListener('click', () => {\n      return;   // K2：切换接线断\n      if (cliViewLocked()) return;",
  },
  'cli-view-persist': {
    find: '      writeStoredCliView(on);',
    replace: '      void 0;   // K3：持久化断',
  },
  'cli-view-trust-bypass': {
    find: "      if (provFlagOn() && norm.prov && norm.prov.trusted) {\n        const label = norm.prov.displayName || { feishu: '飞书', agent: 'Agent', human: '人工', system: '系统' }[norm.prov.kind] || '';",
    replace: "      if (norm.prov) {   // K4：CLI 层自判 trust（绕过 fail-closed）\n        const label = norm.prov.displayName || norm.sourceLabel || { feishu: '飞书', agent: 'Agent', human: '人工', system: '系统' }[norm.prov.kind] || '';",
  },
  'cli-view-fail-newline': {
    find: 'if (cliViewOn() && entry.card) renderUserCLI(entry);   // S-R62-CLI-VIEW-B：阶段更新同步 CLI 行',
    replace: 'if (cliViewOn() && entry.card) { if (norm.stage === "failed") { const kn = document.createElement("div"); kn.className = "cli-line cli-error"; kn.textContent = "✗ 失败另起行"; cliTimeline.appendChild(kn); } else { renderUserCLI(entry); } }   // K5：失败另起行（G7 所经阶段更新钩子）',
  },
  'cli-view-longtext-full': {
    find: 'for (const ev of tools) html += cliToolLine(ev, Boolean(completed));\n      if (assistants.length) {\n        const total = eventsTextLength(assistants);\n        if (total > LONGTEXT_THRESHOLD) {',
    replace: 'for (const ev of tools) html += cliToolLine(ev, Boolean(completed));\n      if (assistants.length) {\n        const total = eventsTextLength(assistants);\n        if (total > Infinity) {   // K6：长文全量渲染',
  },
  'cli-view-nowrap': {
    find: '#timeline-cli .cli-line { white-space: pre-wrap; overflow-wrap: anywhere;',
    replace: '#timeline-cli .cli-line { white-space: nowrap;   /* K7：窄屏换行删 */',
  },
  'cli-view-dom-depend': {
    find: 'html += `<div class="cli-line cli-assistant">• ${cliMarkdownLite(eventsTextFull(assistants))}</div>`;',
    replace: 'html += `<div class="cli-line cli-assistant">• ${cliMarkdownLite(Array.from(group.node.querySelectorAll(".turn-assistant .event-content")).map(n => n.textContent).join(""))}</div>`;   // K8：重建读 DOM 而非 events',
  },
  'cli-view-override-flip': {
    find: 'if (override !== null) return override;   // 优先级：会话覆盖 ＞ 用户偏好 ＞ 默认关',
    replace: 'if (override !== null) return !override;   // K9：override 优先级反',
  },
  'cli-view-tool-drop': {
    find: 'for (const ev of tools) html += cliToolLine(ev, Boolean(completed));',
    replace: 'for (const ev of tools) void ev;   // K11：工具行删（G5/G5b 映射删保护）',
  },
  'cli-view-toggle-residue': {
    find: '      if (on) rebuildAllCLI();',
    replace: '      if (on) { rebuildAllCLI(); timeline.appendChild(document.createComment("cli-residue")); }   // K12：开态往主时间线留残留（V1 往返格杀）',
  },
};

function rendererBuffer() {
  let html = fs.readFileSync(path.join(HERE, 'renderer-canary-sse.html'), 'utf8');
  if (MUTATION && MUTATION !== 'cli-view-threshold') {
    const knife = KNIVES[MUTATION];
    assert.ok(knife, `未知刀名: ${MUTATION}`);
    assert.ok(html.includes(knife.find), `刀锚缺失（${MUTATION}）——不假绿，先核锚`);
    html = html.replace(knife.find, knife.replace);
  }
  return Buffer.from(html);
}

const TS0 = Date.UTC(2026, 10, 18, 12, 0, 0);
function fixtureA() {
  return [
    { schema_version: 'delivery.v2', kind: 'input_posted', message_id: 'm1', delivery_id: 'dw-dlv_A',
      stage: 'hub_received', payload: '请帮我跑一下测试', ts: TS0,
      source: { kind: 'agent', display_name: 'zzc-prodops', trust_decision: 'trusted' },
      provenance: { kind: 'agent', display_name: 'zzc-prodops', attested_by: 'hub', trust_decision: 'trusted' },
      delivery: { role: 'primary' }, cancellable: false, updated_at: new Date(TS0).toISOString() },
    // 真实路由：input_posted 先入待提交区，canonical primary 的 cli_injected 阶段更新才把卡提入时间线；
    // 带 turn 的 admitted echo 若此时到达会合卡入回合（G6b 单独覆盖），fixture 主干不带 echo。
    { schema_version: 'delivery.v2', kind: 'delivery_update', message_id: 'm1', observed_delivery_id: 'dw-dlv_A',
      delivery_stage: 'cli_injected', delivery: { role: 'primary' }, ts: TS0 + 500,
      updated_at: new Date(TS0 + 500).toISOString() },
    { kind: 'assistant_text', turn_id: 't1', delivery_id: 'd-out', text: '收到，开始跑测试。', cursor: '0000002', ts: TS0 + 2000 },
    { kind: 'tool_call', turn_id: 't1', cursor: '0000003', ts: TS0 + 3000,
      data: { name: 'commandExecution', item: { type: 'commandExecution', command: `go test ./... -run ${'X'.repeat(300)}`, status: 'completed', exitCode: 0, aggregatedOutput: 'ok  \tmain\t1.2s' } } },
    { kind: 'tool_call', turn_id: 't1', cursor: '0000004', ts: TS0 + 4000,
      data: { name: 'fileChange', item: { type: 'fileChange', status: 'completed', changes: { '/tmp/a.go': { type: 'update', content: 'package main\n' } } } } },
    { kind: 'tool_call', turn_id: 't1', cursor: '0000005', ts: TS0 + 5000,
      data: { name: 'dynamicToolCall', item: { type: 'dynamicToolCall', name: 'dw_send', status: 'completed' }, input: { to: 'x' } } },
    { kind: 'assistant_text', turn_id: 't1', delivery_id: 'd-out', text: '测试全绿。', cursor: '0000006', ts: TS0 + 6000 },
    { kind: 'error', turn_id: 't1', text: '示例错误', cursor: '0000007', ts: TS0 + 7000 },
    { kind: 'state_change', turn_id: 't1', cursor: '0000008', ts: TS0 + 9000,
      data: { change: 'turn_completed', model: 'qwen4-max', usage: { total_tokens: 1234 }, turn_cost_usd: 0.01 } },
  ];
}

async function boot(browser, baseUrl, { provFlag = true, override = null, storedOn = false, viewport = { width: 1280, height: 900 }, context = null } = {}) {
  const page = context ? await context.newPage() : await browser.newPage({ viewport });
  await page.route('**/status', r => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
    session: 'fixture-cli-view', state: 'idle', readonly: true, can_write: false,
    owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0 }) }));
  await page.route('**/events', r => r.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.addInitScript(arg => {
    window.PROVENANCE_ICONS_V1 = arg.provFlag;
    if (arg.override !== null) window.CLI_VIEW_SESSION_OVERRIDE = arg.override;
    if (arg.storedOn) { try { localStorage.setItem('dw-cli-view', 'on'); } catch (_) {} }
    window.__openedUrls = [];
    window.open = url => { window.__openedUrls.push(String(url)); return {}; };
  }, { provFlag, override, storedOn });
  await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
  await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
  return page;
}

async function inject(page, events) {
  for (const ev of events) await page.evaluate(e => handleIncomingEvent(e), ev);
  await page.evaluate(() => { if (typeof flushPendingRenders === 'function') flushPendingRenders(); });
}

const cliSerialize = page => page.evaluate(() => ({
  lines: Array.from(document.querySelectorAll('#timeline-cli .cli-line')).map(n => n.textContent),
  turns: document.querySelectorAll('#timeline-cli .cli-turn').length,
  inputs: document.querySelectorAll('#timeline-cli .cli-input-card').length,
  bodyClass: document.body.className,
  hidden: document.getElementById('timeline-cli').hidden,
}));

async function shot(page, name) {
  if (!SHOT_DIR) return;
  fs.mkdirSync(SHOT_DIR, { recursive: true });
  await page.screenshot({ path: path.join(SHOT_DIR, `${name}.png`), fullPage: false });
}

(async () => {
  thresholdSelfCheck();
  const fixture = await startRendererServer(rendererBuffer());
  const baseUrl = fixture.url;
  let browser;
  try {
    browser = await launchChromium();
    const checks = regressionCollector();

    // ── G1 默认关 ─────────────────────────────────────────────
    const pOff = await boot(browser, baseUrl);
    await inject(pOff, fixtureA());
    await checks.check('G1 默认关：无 CLI DOM、body 无 cli-view-on、按钮 aria-pressed=false', async () => {
      const s = await cliSerialize(pOff);
      assert.strictEqual(s.lines.length, 0, 'G1 关态不得有 CLI 行');
      assert.ok(!s.bodyClass.includes('cli-view-on'), 'G1 关态 body 不得带 cli-view-on');
      assert.strictEqual(await pOff.locator('#cliViewBtn').getAttribute('aria-pressed'), 'false', 'G1 按钮 aria-pressed 须 false');
      assert.strictEqual(await pOff.evaluate(() => document.getElementById('timeline-cli').hidden), true, 'G1 容器须 hidden');
    });

    // ── G2 关态 DOM 字节同一（基锚 7d3035c6 双页面序列化比对） ──
    const baseFixture = await startRendererServer(baseRenderer());
    const pBase = await boot(browser, baseFixture.url);
    await inject(pBase, fixtureA());
    await checks.check('G2 关态 #timeline innerHTML＋body class 与基锚逐字节同一', async () => {
      const [cur, base] = await Promise.all([
        pOff.evaluate(() => ({ html: document.getElementById('timeline').innerHTML, cls: document.body.className })),
        pBase.evaluate(() => ({ html: document.getElementById('timeline').innerHTML, cls: document.body.className })),
      ]);
      assert.strictEqual(cur.html, base.html, 'G2 关态 #timeline innerHTML 与基锚字节不同（加性层破坏了现 DOM）');
      assert.strictEqual(cur.cls, base.cls, 'G2 关态 body class 与基锚不同（K1 关态加性破坏须红在这里）');
    });
    await shot(pOff, 'pc-1440-off');
    await pBase.close();
    await baseFixture.close();

    // ── G3 开合接线 ───────────────────────────────────────────
    await checks.check('G3 点按钮开合：CLI DOM 出现/还原、aria-pressed、localStorage 随动', async () => {
      await pOff.click('#cliViewBtn');
      let s = await cliSerialize(pOff);
      assert.ok(s.bodyClass.includes('cli-view-on'), 'G3 开后 body 须带 cli-view-on（K2 接线断须红在这里）');
      assert.ok(s.lines.length > 0, 'G3 开后须有 CLI 行');
      assert.strictEqual(s.hidden, false, 'G3 开后容器须可见');
      assert.strictEqual(await pOff.locator('#cliViewBtn').getAttribute('aria-pressed'), 'true', 'G3 aria-pressed 须 true');
      assert.strictEqual(await pOff.evaluate(() => localStorage.getItem('dw-cli-view')), 'on', 'G3 localStorage 须 on');
      assert.strictEqual(await pOff.evaluate(() => getComputedStyle(document.getElementById('timeline')).display), 'none', 'G3 开后现 timeline 须隐藏');
      await shot(pOff, 'pc-1440-on');
      await pOff.click('#cliViewBtn');
      s = await cliSerialize(pOff);
      assert.ok(!s.bodyClass.includes('cli-view-on'), 'G3 再点须还原关态');
      assert.strictEqual(s.lines.length, 0, 'G3 关后 CLI DOM 须移除');
      assert.strictEqual(await pOff.evaluate(() => localStorage.getItem('dw-cli-view')), 'off', 'G3 localStorage 须 off');
    });

    // ── G4 持久化跨 tab（BroadcastChannel＋storage＋新页读回） ──
    await checks.check('G4 跨 tab 同步：BroadcastChannel 跟随、写入方自 apply、新页读 localStorage', async () => {
      // browser.newPage 各自隔离 context（localStorage/BroadcastChannel 不共享）；跨 tab 须同一 context。
      const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      const pA = await boot(browser, baseUrl, { context: ctx });
      const pB = await boot(browser, baseUrl, { context: ctx });
      await inject(pA, fixtureA());
      await pA.click('#cliViewBtn');   // 写入方自 apply
      assert.ok((await cliSerialize(pA)).bodyClass.includes('cli-view-on'), 'G4 写入方须自己立刻 apply');
      await pB.waitForFunction(() => document.body.classList.contains('cli-view-on'), null, { timeout: 5000 });
      assert.strictEqual(await pB.evaluate(() => localStorage.getItem('dw-cli-view')), 'on', 'G4 持久化值须 on（K3 持久化断须红在这里）');
      const pC = await boot(browser, baseUrl, { context: ctx });   // 新页：错过广播，只靠读回 localStorage
      await inject(pC, fixtureA());
      await pC.waitForFunction(() => document.body.classList.contains('cli-view-on'), null, { timeout: 5000 });
      assert.ok((await cliSerialize(pC)).lines.length > 0, 'G4 新页读回偏好后须有 CLI 投影');
      await pA.click('#cliViewBtn');
      await pB.waitForFunction(() => !document.body.classList.contains('cli-view-on'), null, { timeout: 5000 });
      await pA.close(); await pB.close(); await pC.close();
      await ctx.close();
    });

    // ── G5 映射正确性（user/assistant/三类工具/error/turn_completed） ──
    const pMap = await boot(browser, baseUrl);
    await inject(pMap, fixtureA());
    await pMap.click('#cliViewBtn');
    await checks.check('G5 映射：›/•/✗/Worked for 行首与关键字段、工具完成后保留行', async () => {
      const s = await cliSerialize(pMap);
      const joined = s.lines.join('\n');
      assert.ok(s.lines.some(l => l.startsWith('› ') && l.includes('请帮我跑一下测试')), 'G5 用户行须 › 开头含正文');
      assert.ok(s.lines.some(l => l.startsWith('• ') && l.includes('收到，开始跑测试。')), 'G5 assistant 行须 • 开头');
      assert.ok(s.lines.some(l => l.startsWith('• Ran go test ./...')), 'G5 commandExecution 须 • Ran <command>');
      assert.ok(joined.includes('└ exit 0'), 'G5 工具结果须 └ exit <code> 摘要');
      assert.ok(s.lines.some(l => l.startsWith('• Edited /tmp/a.go')), 'G5 fileChange 须 • Edited <changes[0] 路径>');
      assert.ok(joined.includes('└ update /tmp/a.go'), 'G5 fileChange changes 类型须入摘要');
      assert.ok(s.lines.some(l => l.includes('dw_send')), 'G5 dynamicToolCall 须降级 name 行');
      assert.ok(s.lines.some(l => l.startsWith('✗ 示例错误')), 'G5 error 须 ✗ 行');
      assert.ok(s.lines.some(l => l.includes('Worked for') && l.includes('•')), 'G5 回合尾须 Worked for <时长> • <时钟>');
      assert.strictEqual(await pMap.locator('#timeline-cli .cli-tool').count(), 3, 'G5 工具完成后须保留 3 行（与现视图有意差异）');
      const status = await pMap.locator('#timeline-cli .cli-statusbar').textContent();
      assert.ok(status.includes('qwen4-max default'), 'G5 状态行须含 model');
      assert.ok(status.includes('tokens 1234'), 'G5 状态行须含 usage');
    });

    // ── G5b 工具行版式校准（0.170 live 通知原文＋rollout 序列化容错对照） ──
    const calFixture = JSON.parse(fs.readFileSync(path.join(HERE, 'fixtures/cli-view-tool-items-0170.json'), 'utf8'));
    const pCal = await boot(browser, baseUrl);
    await inject(pCal, [
      ...calFixture.arms.map((arm, index) => ({ kind: 'tool_call', turn_id: 'tCal', cursor: String(index + 1).padStart(7, '0'), ts: TS0 + index * 1000, data: { name: arm.item.type, item: arm.item } })),
      { kind: 'state_change', turn_id: 'tCal', cursor: '0000099', ts: TS0 + 9000, data: { change: 'turn_completed' } },
    ]);
    await pCal.click('#cliViewBtn');
    await checks.check('G5b 工具行校准：live commandExecution 绿样本＋rollout FileChange changes 映射', async () => {
      const s = await cliSerialize(pCal);
      const joined = s.lines.join('\n');
      assert.ok(s.lines.some(l => l.startsWith('• Ran /bin/bash -lc')), 'G5b live command 串须原样渲染');
      assert.ok(joined.includes('└ exit 0 · ok'), 'G5b live aggregatedOutput/exitCode 须入摘要');
      assert.ok(s.lines.some(l => l.startsWith('• Edited /home/is3/outbox/')), 'G5b rollout FileChange 须取 changes[0] 路径');
      assert.ok(joined.includes('└ add /home/is3/outbox/'), 'G5b changes 类型须入摘要');
      assert.strictEqual(await pCal.locator('#timeline-cli .cli-tool').count(), calFixture.arms.length, 'G5b 每臂一行工具行');
    });
    await shot(pCal, 'pc-1440-tool-calibration');
    await pCal.close();

    // ── G6 来源不绕过（fail-closed＋canonical attested＋合卡） ──
    await checks.check('G6 来源前缀：attested 挂、不可信不挂、echo 合卡只一行', async () => {
      const s = await cliSerialize(pMap);
      const inputLines = s.lines.filter(l => l.includes('请帮我跑一下测试'));
      assert.strictEqual(inputLines.length, 1, 'G6 同一投递（input_posted＋admitted echo）CLI 只一行（T-074 合卡语义）');
      assert.ok(inputLines[0].includes('[zzc-prodops]'), 'G6 canonical attested 来源须挂前缀（K4 自判 trust 须红在这里）');
      // 无 turn 的 admitted echo（delivery_id 只在 data 里）：G-MERGE-SAME 走 footnote「已回显」，不另起行。
      await inject(pMap, [{ kind: 'user_input', text: '请帮我跑一下测试',
        data: { delivery_id: 'dlv_A', lifecycle: 'admitted' }, cursor: '0000009', ts: TS0 + 10000 }]);
      const sEcho = await cliSerialize(pMap);
      assert.strictEqual(sEcho.lines.filter(l => l.includes('请帮我跑一下测试')).length, 1, 'G6 无 turn echo 只合 footnote，不新增用户行');
      assert.strictEqual(sEcho.inputs, 1, 'G6 footnote 合并后独立卡仍在');
      await inject(pMap, [{ schema_version: 'delivery.v2', kind: 'input_posted', message_id: 'm2', delivery_id: 'dw-dlv_B',
        stage: 'hub_received', payload: '不可信来源正文', ts: TS0 + 20000,
        source: { kind: 'agent', display_name: 'spoofed' }, delivery: { role: 'primary' },
        cancellable: false, updated_at: new Date(TS0 + 20000).toISOString() },
        { schema_version: 'delivery.v2', kind: 'delivery_update', message_id: 'm2', observed_delivery_id: 'dw-dlv_B',
          delivery_stage: 'cli_injected', delivery: { role: 'primary' }, ts: TS0 + 20500,
          updated_at: new Date(TS0 + 20500).toISOString() }]);
      const s2 = await cliSerialize(pMap);
      const untrusted = s2.lines.find(l => l.includes('不可信来源正文'));
      assert.ok(untrusted, 'G6 不可信输入须有行');
      assert.ok(untrusted.startsWith('› 不可信来源正文'), 'G6 不可信来源不得挂前缀（fail-closed；K4 须红在这里）');
    });

    // ── G7 失败不另起行 ───────────────────────────────────────
    await checks.check('G7 delivery failed 只更新用户行阶段缀，不另起错误行', async () => {
      const before = await cliSerialize(pMap);
      await inject(pMap, [{ schema_version: 'delivery.v2', kind: 'delivery_update', message_id: 'm1',
        observed_delivery_id: 'dw-dlv_A', delivery_stage: 'helper_received', delivery_status: 'failed',
        reason: 'cli_answer_timeout', ts: TS0 + 30000 }]);
      const after = await cliSerialize(pMap);
      assert.strictEqual(after.inputs, before.inputs, 'G7 失败不得新增用户行');
      const errBefore = before.lines.filter(l => l.startsWith('✗')).length;
      const errAfter = after.lines.filter(l => l.startsWith('✗')).length;
      assert.strictEqual(errAfter, errBefore, 'G7 失败不得另起错误行（K5 须红在这里）');
      const line = after.lines.find(l => l.includes('请帮我跑一下测试'));
      assert.ok(line.includes('失败'), 'G7 用户行阶段缀须含失败文案');
    });

    // ── G6b 带 turn 的 admitted echo：合卡入回合，canonical 前缀同源保留 ──
    await checks.check('G6b admitted echo 合卡入回合：仍只一行、canonical 前缀保留', async () => {
      await inject(pMap, [{ kind: 'user_input', delivery_id: 'dlv_A', turn_id: 't1', text: '请帮我跑一下测试',
        data: { lifecycle: 'admitted' }, cursor: '0000010', ts: TS0 + 31000 }]);
      // G-MERGE-SAME 带 turn 的 echo 经 C2_ECHO_HOLD_MS（800ms）乱序短窗后才 admit 入回合；上界 5s 足够覆盖。
      await pMap.waitForFunction(() => document.querySelector('#timeline-cli .cli-turn .cli-user'), null, { timeout: 5000 });
      const s = await cliSerialize(pMap);
      const inputLines = s.lines.filter(l => l.includes('请帮我跑一下测试'));
      assert.strictEqual(inputLines.length, 1, 'G6b 合卡入回合后 CLI 仍只一行用户行');
      assert.ok(inputLines[0].includes('[zzc-prodops]'), 'G6b 合卡后 canonical attested 前缀须保留（retainCanonicalTrustedProvenance 同源）');
      assert.strictEqual(await pMap.locator('#timeline-cli .cli-turn .cli-user').count(), 1, 'G6b 用户行须已入回合投影');
      assert.strictEqual(s.inputs, 1, 'G6b 合卡后独立卡须移除（仅剩 m2 不可信卡）');
    });
    await pMap.close();

    // ── G8 长文 pager＋全文现拼 ───────────────────────────────
    const pLong = await boot(browser, baseUrl);
    const longText = '长'.repeat(5000);
    await inject(pLong, [
      { kind: 'assistant_text', turn_id: 'tL', delivery_id: 'd-l', text: longText, cursor: '0000001', ts: TS0 },
      { kind: 'state_change', turn_id: 'tL', cursor: '0000002', ts: TS0 + 1000, data: { change: 'turn_completed' } },
    ]);
    await pLong.click('#cliViewBtn');
    await checks.check('G8 超长正文：摘要≤1200＋pager 行，打开全文现拼逐字正确', async () => {
      const pager = pLong.locator('#timeline-cli .cli-pager');
      assert.strictEqual(await pager.count(), 1, 'G8 >4000 字须出 pager 行（K6 全量渲染须红在这里）');
      const bullet = await pLong.locator('#timeline-cli .cli-assistant').first().textContent();
      assert.ok(bullet.length <= 1200 + 10, `G8 摘要须封顶 1200（实 ${bullet.length}）`);
      assert.ok((await pager.textContent()).includes('5,000'), 'G8 pager 须标全文字数');
      await pLong.locator('#timeline-cli [data-cli-open]').click();
      const urls = await pLong.evaluate(() => window.__openedUrls);
      assert.strictEqual(urls.length, 1, 'G8 打开全文须触发一次');
      const full = await pLong.evaluate(async url => await (await fetch(url)).text(), urls[0]);
      assert.strictEqual(full, longText, 'G8 全文现拼须与事件态逐字同');
    });
    await pLong.close();

    // ── G9 窄屏（390×844／360×780）＋PC 关态像素同基锚 ─────────
    // V1：窄屏关态字节同一覆盖初始关＋开→关往返后两态（防切换残留；K12 杀点在往返格）。
    const baseFixture2 = await startRendererServer(baseRenderer());
    for (const vp of [{ width: 390, height: 844 }, { width: 360, height: 780 }]) {
      const pN = await boot(browser, baseUrl, { viewport: vp });
      const pBaseN = await boot(browser, baseFixture2.url, { viewport: vp });
      await inject(pN, fixtureA());
      await inject(pBaseN, fixtureA());
      const narrowOffState = () => Promise.all([
        pN.evaluate(() => ({ html: document.getElementById('timeline').innerHTML, cls: document.body.className })),
        pBaseN.evaluate(() => ({ html: document.getElementById('timeline').innerHTML, cls: document.body.className })),
      ]);
      await checks.check(`G9 窄屏 ${vp.width}×${vp.height} 初始关态 #timeline innerHTML＋body class 与基锚逐字节同一`, async () => {
        const [cur, base] = await narrowOffState();
        assert.strictEqual(cur.html, base.html, `G9 窄屏 ${vp.width} 初始关态 innerHTML 与基锚字节不同`);
        assert.strictEqual(cur.cls, base.cls, `G9 窄屏 ${vp.width} 初始关态 body class 与基锚不同`);
      });
      await pN.click('#cliViewBtn');
      await checks.check(`G9 窄屏 ${vp.width}×${vp.height}：CLI 开态横向溢出 0、按钮≥40、长命令不横滚`, async () => {
        const overflow = await pN.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert.strictEqual(overflow, 0, `G9 窄屏 document 横向溢出须 0（K7 nowrap 须红在这里，实 ${overflow}）`);
        const box = await pN.locator('#cliViewBtn').boundingBox();
        assert.ok(box && box.width >= 40 && box.height >= 40, 'G9 切换按钮窄屏须 ≥40×40');
        const cliOverflow = await pN.evaluate(() => { const el = document.getElementById('timeline-cli'); return el.scrollWidth - el.clientWidth; });
        assert.strictEqual(cliOverflow, 0, 'G9 CLI 容器自身横向溢出须 0');
      });
      await shot(pN, `mobile-${vp.width}-on`);
      await pN.click('#cliViewBtn');
      await shot(pN, `mobile-${vp.width}-off`);
      await checks.check(`G9 窄屏 ${vp.width}×${vp.height} 开→关往返后关态仍与基锚逐字节同一（防切换残留）`, async () => {
        const [cur, base] = await narrowOffState();
        assert.strictEqual(cur.html, base.html, `G9 窄屏 ${vp.width} 往返后关态 innerHTML 与基锚字节不同（K12 切换残留须红在这里）`);
        assert.strictEqual(cur.cls, base.cls, `G9 窄屏 ${vp.width} 往返后关态 body class 与基锚不同（K12 须红在这里）`);
      });
      await pN.close(); await pBaseN.close();
    }
    const pBase2 = await boot(browser, baseFixture2.url, { viewport: { width: 1440, height: 900 } });
    const pCur2 = await boot(browser, baseUrl, { viewport: { width: 1440, height: 900 } });
    await inject(pBase2, fixtureA());
    await inject(pCur2, fixtureA());
    await checks.check('G9 PC 1440 关态 #timeline 像素同基锚（元素截图逐字节）', async () => {
      const a = await pBase2.locator('#timeline').screenshot();
      const b = await pCur2.locator('#timeline').screenshot();
      assert.ok(a.equals(b), 'G9 PC 关态 timeline 像素与基锚不同');
    });
    await pBase2.close(); await pCur2.close(); await baseFixture2.close();

    // ── G10 重连/分页/逐出稳定性（纯投影一致性） ──────────────
    const pSt = await boot(browser, baseUrl);
    await inject(pSt, fixtureA());
    await pSt.click('#cliViewBtn');
    await checks.check('G10 重连回放＋回合分段＋逐出后开态投影与事件态一致', async () => {
      const before = await cliSerialize(pSt);
      await pSt.evaluate(() => { resetConversation(); finishTimelineReplay(false); });
      await inject(pSt, fixtureA());   // 重连补放同一流
      const replayed = await cliSerialize(pSt);
      assert.deepStrictEqual(replayed.lines, before.lines, 'G10 重连回放后投影须逐行同（K8 读 DOM 须红在这里）');
      await pSt.evaluate(() => splitTurnGroupAtHubSequence('t1', 5));   // 历史分页的回合分段机制
      await pSt.evaluate(() => { setCliView(false); setCliView(true); });
      const split = await cliSerialize(pSt);
      assert.strictEqual(split.lines.length, before.lines.length, 'G10 分段重建后行数须守恒');
      assert.deepStrictEqual(split.lines, before.lines, 'G10 分段重建后行序/内容须同');
      const evicted = await pSt.evaluate(() => {
        const node = document.getElementById('timeline').firstElementChild;
        const hadCli = Boolean(node && node._cliNode);
        if (node) releaseTimelineNode(node);
        return { hadCli, remaining: document.getElementById('timeline').children.length };
      });
      assert.ok(evicted.hadCli, 'G10 被逐出节点须有 CLI 对应件');
      const afterEvict = await cliSerialize(pSt);
      assert.ok(afterEvict.lines.length < split.lines.length, 'G10 逐出后 CLI 行须同步减少');
      await pSt.evaluate(() => { setCliView(false); setCliView(true); });
      const rebuilt = await cliSerialize(pSt);
      assert.deepStrictEqual(rebuilt.lines, afterEvict.lines, 'G10 逐出后开关重建须与事件态一致');
      // 纯投影判别力：清空主 DOM 回合内容（事件态保留），重建仍须从 events 出全文（K8 读 DOM 确定性红在这里）。
      // 分段后 turnGroups 可能指向空尾段，须清全部段节点，否则清错节点假绿。
      await pSt.evaluate(() => { for (const segment of (turnGroupSegments.get('t1') || [])) segment.node.innerHTML = ''; });
      await pSt.evaluate(() => { setCliView(false); setCliView(true); });
      const domCleared = await cliSerialize(pSt);
      assert.deepStrictEqual(domCleared.lines, afterEvict.lines, 'G10 投影不得依赖主 DOM（主 DOM 清空后仍须与事件态同）');
    });
    await pSt.close();

    // ── G11 会话覆盖位优先级 ──────────────────────────────────
    const pOvT = await boot(browser, baseUrl, { override: true, storedOn: false });
    const pOvF = await boot(browser, baseUrl, { override: false, storedOn: true });
    await inject(pOvT, fixtureA());
    await inject(pOvF, fixtureA());
    await checks.check('G11 override 优先：true 强制开、false 强制关、按钮置灰', async () => {
      assert.ok((await cliSerialize(pOvT)).bodyClass.includes('cli-view-on'), 'G11 override=true 须开（K9 优先级反须红在这里）');
      assert.ok((await cliSerialize(pOvT)).lines.length > 0, 'G11 override=true 须有 CLI 投影（钩子与开关同向；K9 须红在这里）');
      assert.ok(!(await cliSerialize(pOvF)).bodyClass.includes('cli-view-on'), 'G11 override=false 须关（压过 localStorage on）');
      assert.strictEqual((await cliSerialize(pOvF)).lines.length, 0, 'G11 override=false 钩子须同步关（不得暗建 CLI DOM；K9 须红在这里）');
      assert.strictEqual(await pOvT.locator('#cliViewBtn').isDisabled(), true, 'G11 锁定态按钮须置灰');
      assert.strictEqual(await pOvF.locator('#cliViewBtn').isDisabled(), true, 'G11 锁定态按钮须置灰');
      await pOvF.click('#cliViewBtn', { force: true }).catch(() => {});
      assert.ok(!(await cliSerialize(pOvF)).bodyClass.includes('cli-view-on'), 'G11 锁定态点击不得改变');
    });
    await pOvT.close(); await pOvF.close();
    await pOff.close();

    // ── G12 flood CLI-on（同阈值同场景）＋toggle-storm ─────────
    await runFlood(browser, checks);

    checks.finish();
    console.log(`renderer-cli-view-regression: G1～G12 通过${MUTATION ? `（MUTATION=${MUTATION} 应红未红=假绿）` : ''}`);
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);

// ── G12：独立 SSE 洪流（renderer 直服，CLI-on 预置），阈值/场景与 flood 套件逐字同 ──
function frame(epoch, seq, ev) { return `id: ${epoch}-${seq}\ndata: ${JSON.stringify(ev)}\n\n`; }
function floodOrder(sc) {
  const order = [];
  for (let c = 1; c <= sc.total; c++) {
    if (c === sc.lateCursor) continue;
    order.push(c);
    if (c === sc.lateAfter) order.push(sc.lateCursor);
  }
  return order;
}
async function runFlood(browser, checks) {
  const scenarios = [
    { name: 'flood', total: 100000, lateCursor: 0, lateAfter: 0 },
    { name: 'late-order', total: 30000, lateCursor: 12000, lateAfter: 21000 },
  ];
  for (const sc of scenarios) {
    const order = floodOrder(sc);
    const renderer = rendererBuffer();
    const st = { go: null, done: null, pushed: 0, aborted: undefined, start: 0, end: 0 };
    let release; st.go = new Promise(r => { release = r; });
    st.done = new Promise(r => { st.finish = r; });
    const server = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://x');
      if (u.pathname === '/status') { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ session: 'flood-cli', state: 'running', readonly: true, can_write: false, owner: 'o', model: 'm', cost_total: 0, cost_last: 0, turns: 1 })); return; }
      if (u.pathname === '/events') {
        res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
        res.flushHeaders();
        if (u.searchParams.get('after')) return;
        (async () => {
          await st.go;
          res.write(frame('f1', 1, { kind: 'user_input', text: '灌入', cursor: '0000000', delivery_id: 'd-f', turn_id: 't-f', stage: 'cli_injected', ts: 0 }));
          st.start = Date.now();
          let i = 0;
          while (i < order.length) {
            if (Date.now() - st.start > DEADLINE_S * 1000) { st.aborted = i; break; }
            const due = Math.min(order.length, Math.floor(((Date.now() - st.start) / 1000) * RATE) + 1);
            let chunk = '';
            for (; i < due; i++) {
              const c = order[i];
              chunk += frame('f1', i + 2, { kind: 'assistant_text', text: `d${c - 1};`, cursor: String(c).padStart(7, '0'), delivery_id: 'd-f', turn_id: 't-f', ts: 0 });
            }
            if (chunk && !res.write(chunk)) await Promise.race([new Promise(r => res.once('drain', r)), sleep(DEADLINE_S * 1000)]);
            await sleep(10);
          }
          st.end = Date.now(); st.pushed = i;
          if (st.aborted === undefined) res.write(frame('f1', order.length + 2, { kind: 'state_change', text: '', cursor: '9999999', delivery_id: 'd-f', turn_id: 't-f', ts: 0, data: { change: 'turn_completed' } }));
          st.finish();
        })();
        return;
      }
      if (u.pathname === '/sample') { let body = ''; req.on('data', c => { body += c; }); req.on('end', () => { try { st.sample = JSON.parse(body); } catch (_) {} res.writeHead(204); res.end(); }); return; }
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end(renderer);
    });
    await new Promise(r => server.listen(0, '127.0.0.1', r));
    const port = server.address().port;
    const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
    try {
      await page.addInitScript(() => { try { localStorage.setItem('dw-cli-view', 'on'); } catch (_) {} });
      await page.goto(`http://127.0.0.1:${port}/?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => typeof turnGroups !== 'undefined' && document.body.classList.contains('cli-view-on'));
      await page.evaluate(() => {
        window.__peakEvents = 0; window.__peakNodes = 0; window.__peakCliNodes = 0;
        const countNodes = node => { let n = 0; const w = document.createTreeWalker(node, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT); while (w.nextNode()) n++; return n; };
        const watch = arr => {
          if (arr.length > window.__peakEvents) window.__peakEvents = arr.length;
          arr.push = function (...a) { const r = Array.prototype.push.apply(this, a); if (this.length > window.__peakEvents) window.__peakEvents = this.length; return r; };
          return arr;
        };
        const origSet = turnGroups.set.bind(turnGroups);
        turnGroups.set = (k, g) => {
          if (k === 't-f') {
            let events = watch(g.events);
            Object.defineProperty(g, 'events', { configurable: true, get: () => events, set: v => { events = watch(v); } });
            new MutationObserver(() => { const n = countNodes(g.node); if (n > window.__peakNodes) window.__peakNodes = n; }).observe(g.node, { childList: true, subtree: true, characterData: true });
          }
          return origSet(k, g);
        };
        new MutationObserver(() => { const n = countNodes(document.getElementById('timeline-cli')); if (n > window.__peakCliNodes) window.__peakCliNodes = n; }).observe(document.getElementById('timeline-cli'), { childList: true, subtree: true, characterData: true });
        setInterval(() => { const g = turnGroups.get('t-f'); navigator.sendBeacon('/sample', JSON.stringify({ peakEvents: window.__peakEvents, events: g ? g.events.length : 0, peakNodes: window.__peakNodes, peakCliNodes: window.__peakCliNodes })); }, 500);
      });
      await page.evaluate(() => fetch('/status'));
      release();
      await st.done;
      // 服务端写完 ≠ 页面收齐：等最后一帧 seq（order.length+2）连续落地再 flush 读取（与 flood 本体同口径）。
      const wantSeq = order.length + 2;
      let lastSeq = 0;
      const recvDeadline = Date.now() + (st.aborted !== undefined ? 5000 : 120000);
      while (Date.now() < recvDeadline) {
        lastSeq = await Promise.race([page.evaluate(() => lastContiguousSeq), sleep(5000).then(() => -1)]);
        if (lastSeq < 0 || lastSeq >= wantSeq) break;
        await sleep(200);
      }
      await sleep(500);
      const read = await Promise.race([page.evaluate(() => {
        flushPendingRenders();
        const g = turnGroups.get('t-f');
        const countNodes = node => { let n = 0; const w = document.createTreeWalker(node, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT); while (w.nextNode()) n++; return n; };
        const cliText = Array.from(document.querySelectorAll('#timeline-cli .cli-assistant')).map(n => n.textContent.replace(/^• /, '')).join('');
        const dataFull = g ? g.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join('') : '';
        const pagerCount = document.querySelectorAll('#timeline-cli .cli-pager').length;
        return { peakEvents: window.__peakEvents, finalEvents: g ? g.events.length : 0, peakNodes: window.__peakNodes, finalNodes: g ? countNodes(g.node) : 0, peakCliNodes: window.__peakCliNodes, finalCliNodes: countNodes(document.getElementById('timeline-cli')), cliText, dataFull, pagerCount, pushed: 0 };
      }), sleep(30000).then(() => st.sample || {})]).catch(() => st.sample || {});
      const expected = Array.from({ length: sc.total }, (_, i) => `d${i};`).join('');
      const bound = SOFT_CAP + MARGIN;
      await checks.check(`G12 ${sc.name} CLI-on：事件/节点峰值终值 ≤ ${SOFT_CAP}+${MARGIN}，全文逐字收敛`, async () => {
        assert.strictEqual(st.aborted, undefined, `G12 ${sc.name} 推入在 DEADLINE 内未完成（背压超时）`);
        assert.ok(lastSeq >= wantSeq, `G12 ${sc.name} 页面未收齐（lastSeq ${lastSeq} < ${wantSeq}）`);
        assert.ok(read.peakEvents <= bound, `G12 ${sc.name} peakEvents ${read.peakEvents} > ${bound}`);
        assert.ok(read.finalEvents <= bound, `G12 ${sc.name} finalEvents ${read.finalEvents} > ${bound}`);
        assert.ok(read.peakNodes <= bound, `G12 ${sc.name} peakNodes ${read.peakNodes} > ${bound}`);
        assert.ok(read.finalNodes <= bound, `G12 ${sc.name} finalNodes ${read.finalNodes} > ${bound}`);
        assert.ok(read.peakCliNodes <= bound, `G12 ${sc.name} peakCliNodes ${read.peakCliNodes} > ${bound}`);
        // 全文收敛判在数据层（DOM 按长文策略只渲摘要＋pager，全文点开现拼，G8 已覆盖逐字）。
        assert.strictEqual(read.dataFull, expected, `G12 ${sc.name} 数据层全文与逐条拼接期望不同（乱序/合并收敛失效）`);
        assert.strictEqual(read.pagerCount, 1, `G12 ${sc.name} 超长文须出 pager 行（K6 全量渲染刀须红在这里）`);
        assert.strictEqual(read.cliText, expected.slice(0, 1200), `G12 ${sc.name} DOM 摘要须与事件态前 1200 字逐字同`);
      });
      console.log(`G12_READINGS ${JSON.stringify({ scenario: sc.name, pushed: st.pushed, lastSeq, seconds: ((st.end - st.start) / 1000).toFixed(1), ...read, cliText: undefined, dataFull: undefined, bound })}`);
    } finally {
      await page.close().catch(() => {});
      await new Promise(r => server.close(r));
    }
  }
  // toggle-storm：流式推入中反复开合 ≥50 次（页内驱动，避免洪流下 evaluate 饿死）
  const st = { go: null, done: null };
  let release; st.go = new Promise(r => { release = r; });
  st.done = new Promise(r => { st.finish = r; });
  const renderer = rendererBuffer();
  const server = http.createServer((req, res) => {
    const u = new URL(req.url, 'http://x');
    if (u.pathname === '/status') { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ session: 'storm-cli', state: 'running', readonly: true, can_write: false, owner: 'o', model: 'm', cost_total: 0, cost_last: 0, turns: 1 })); return; }
    if (u.pathname === '/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.flushHeaders();
      if (u.searchParams.get('after')) return;
      (async () => {
        await st.go;
        res.write(frame('s1', 1, { kind: 'user_input', text: '风暴', cursor: '0000000', delivery_id: 'd-s', turn_id: 't-s', stage: 'cli_injected', ts: 0 }));
        for (let i = 0; i < 6000; i++) {
          res.write(frame('s1', i + 2, { kind: 'assistant_text', text: `s${i};`, cursor: String(i + 1).padStart(7, '0'), delivery_id: 'd-s', turn_id: 't-s', ts: 0 }));
          if (i % 200 === 0) await sleep(30);
        }
        res.write(frame('s1', 6002, { kind: 'state_change', cursor: '9999999', delivery_id: 'd-s', turn_id: 't-s', ts: 0, data: { change: 'turn_completed' } }));
        st.finish();
      })();
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(renderer);
  });
  await new Promise(r => server.listen(0, '127.0.0.1', r));
  const page = await browser.newPage({ viewport: { width: 1280, height: 860 } });
  try {
    await page.addInitScript(() => { try { localStorage.setItem('dw-cli-view', 'on'); } catch (_) {} });
    await page.goto(`http://127.0.0.1:${server.address().port}/?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof turnGroups !== 'undefined' && typeof setCliView === 'function');
    await page.evaluate(() => {
      window.__storm = { toggles: 0, mismatches: 0, maxNodeDelta: 0 };
      const baseNodes = document.getElementsByTagName('*').length;
      window.__stormTimer = setInterval(() => {
        const on = document.body.classList.contains('cli-view-on');
        setCliView(!on);
        window.__storm.toggles++;
        if (!on) {
          const g = turnGroups.get('t-s');
          const expected = g ? g.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join('') : '';
          const cliText = Array.from(document.querySelectorAll('#timeline-cli .cli-assistant')).map(n => n.textContent.replace(/^• /, '')).join('');
          const expectedDom = expected.length > LONGTEXT_THRESHOLD ? expected.slice(0, LONGTEXT_EXCERPT) : expected;
          if (cliText !== expectedDom) window.__storm.mismatches++;
        } else if (document.getElementById('timeline-cli').children.length !== 0) {
          window.__storm.mismatches++;
        }
        const delta = Math.abs(document.getElementsByTagName('*').length - baseNodes);
        if (delta > window.__storm.maxNodeDelta) window.__storm.maxNodeDelta = delta;
        if (window.__storm.toggles >= 60) clearInterval(window.__stormTimer);
      }, 100);
    });
    release();
    await st.done;
    await page.waitForFunction(() => window.__storm && window.__storm.toggles >= 50, null, { timeout: 30000 });
    await sleep(500);
    const storm = await page.evaluate(() => {
      clearInterval(window.__stormTimer);
      setCliView(true);
      const g = turnGroups.get('t-s');
      const expected = g.events.filter(e => e.kind === 'assistant_text').map(e => e.text || '').join('');
      const cliText = Array.from(document.querySelectorAll('#timeline-cli .cli-assistant')).map(n => n.textContent.replace(/^• /, '')).join('');
      const expectedDom = expected.length > LONGTEXT_THRESHOLD ? expected.slice(0, LONGTEXT_EXCERPT) : expected;
      return { ...window.__storm, finalMatch: cliText === expectedDom, finalEvents: g.events.length };
    });
    await checks.check('G12 toggle-storm ≥50 次：每次开态投影一致、关态容器空、节点数有界', async () => {
      assert.ok(storm.toggles >= 50, `G12 storm 开合次数不足（${storm.toggles}）`);
      assert.strictEqual(storm.mismatches, 0, `G12 storm 投影不一致 ${storm.mismatches} 次（K8 读 DOM 须红在这里）`);
      assert.ok(storm.finalMatch, 'G12 storm 终态投影须与事件态逐字同');
      assert.ok(storm.maxNodeDelta <= SOFT_CAP + MARGIN, `G12 storm 节点增量 ${storm.maxNodeDelta} 超界（detached 泄漏）`);
      assert.ok(storm.finalEvents <= SOFT_CAP + MARGIN, 'G12 storm 事件数须有界');
    });
    console.log(`G12_READINGS ${JSON.stringify({ scenario: 'toggle-storm', ...storm })}`);
  } finally {
    await page.close().catch(() => {});
    await new Promise(r => server.close(r));
  }
}
