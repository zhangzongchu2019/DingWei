// 包 C3 · 建席表单「模型」下拉（接 B2）与席位列表显示生效模型的浏览器闸。派工 R-V2-SEAT-ITEMS-DISPATCH-20261005000613 · B2 批 R-V2-SEAT-B2-DESIGN-APPROVE-20261005000917。
// 线上形状（待 kimi30kqtuxerh 确认，请求 efbda2642291）：options.models＝字符串数组（kimi 已确认，efbda2642291）；建席请求可选 model（选默认＝不带键）；
//   422 model_not_available / model_not_supported；GET /api/console/agents 托管行 hosted.model（空串＝节点默认）。文案为占位。
// MUTATION=<名>：同 console-quota-regression.js 约定（退出码 1 红·0 绿·2 崩·3 变异无效）。
const assert = require('assert');
const { execFileSync } = require('child_process');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

let html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: path.join(__dirname, '..'), encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'],
  env: { ...process.env, DW_CONSOLE_HOSTED_FIXTURE: '1' },
});
const mutation = process.env.MUTATION || '';
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  if (hits !== 1) { console.error(`MUTATION-ANCHOR-STALE: mutation ${mutation} anchor must match exactly once, hits=${hits}: ${from.slice(0, 160)}`); process.exit(3); }
  html = html.replace(from, to);
}
const M = {
  'mutation-anchor-selftest': () => mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x'),
  'model-always-sent': () => mutate('var model=v(\'hostedModel\');if(model)body.model=model;', "var model=v('hostedModel');body.model=model;"),
  'model-never-sent': () => mutate('var model=v(\'hostedModel\');if(model)body.model=model;', "var model=v('hostedModel');"),
  'models-not-deduped': () => mutate("if(typeof m==='string'&&m&&out.indexOf(m)<0)out.push(m);", "if(typeof m==='string'&&m)out.push(m);"),
  'empty-models-no-note': () => mutate("models.length?'默认':'默认（当前没有可选模型）'", "'默认'"),
  'err-not-available-missing': () => mutate("model_not_available:'所选模型当前没有在线节点支持，请换一个模型或稍后再试',", ''),
  'err-not-supported-missing': () => mutate("model_not_supported:'这个 CLI 不支持指定模型，请选「默认」',", ''),
  'roster-no-model': () => mutate("if(a.origin==='hosted'&&a.hosted&&typeof a.hosted.model==='string'){", 'if(false){'),
  'roster-empty-not-default': () => mutate("(a.hosted.model||'默认')", 'a.hosted.model'),
  'roster-shows-on-selfhost': () => mutate("if(a.origin==='hosted'&&a.hosted&&typeof a.hosted.model==='string'){", "if(a.hosted&&typeof a.hosted.model==='string'||a.origin!=='hosted'){ a.hosted=a.hosted||{model:'x'};"),
};
if (mutation && !M[mutation]) { console.error(`MUTATION-UNKNOWN: ${mutation}`); process.exit(3); }
if (mutation) M[mutation]();

const G = 'A'.repeat(32);
let optModels = ['gpt-a', 'gpt-b', 'gpt-a', 'gpt-c'];
const writes = [];
function options() {
  return {
    nodes: [{ id: 'n1', name: 'node-a', pool: 'p', online: true, free_mem_gb: 12, free_vcpu: 6, free_disk_gb: 80 }],
    combos: [{ cli: 'codex', llm: 'openai', cred_kind: 'api_key', verified: true }],
    accounts: [{ id: 'acc1', name: '团队主账号', llm: 'openai', kind: 'api_key', last4: '7f3a' }],
    plans: [{ id: '2c4g', name: '2C/4G', vcpu: 2, mem_gb: 4, fits: true }],
    quota: { remaining: { agents: 3, vcpu: 6, mem_gb: 12, disk_gb: 100 } },
    ...(optModels === undefined ? {} : { models: optModels }),
  };
}
const seats = [
  { id: 'ag-m1', name: 'withm', hosted: { state: 'running', upgradable: false, node_name: 'node-a', model: 'gpt-b' }, origin: 'hosted' },
  { id: 'ag-m2', name: 'defm', hosted: { state: 'running', upgradable: false, node_name: 'node-a', model: '' }, origin: 'hosted' },
  { id: 'ag-m3', name: 'oldh', hosted: { state: 'running', upgradable: false, node_name: 'node-a' }, origin: 'hosted' },
  { id: 'ag-s1', name: 'selfh', origin: 'selfhost' },
];
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function read(req) { return new Promise((r) => { let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => r(b ? JSON.parse(b) : {})); }); }
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/api/console/agents') return json(res, 200, { agents: seats.map((s) => ({ state: 'active', online: true, agent_id: s.id, name: s.name, full_name: `${s.name}.zzc`, origin: s.origin, group_code: G, ...(s.hosted ? { hosted: s.hosted } : {}) })) });
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups') return json(res, 200, { principal: 'owner', owned_agents: [], groups: [] });
  if (p === '/api/console/enroll/applications') return json(res, 200, { applications: [] });
  if (p === '/api/console/hosted/options') return json(res, 200, options());
  if (p === '/api/console/hosted/agents' && req.method === 'POST') {
    const body = await read(req); writes.push(body);
    if (body.name === 'Mna') return json(res, 422, { error: 'model_not_available', detail: 'SECRET-HUB-TEXT' });
    if (body.name === 'Mns') return json(res, 422, { error: 'model_not_supported', detail: 'SECRET-HUB-TEXT' });
    return json(res, 409, { error: 'name_taken' });   // 只为抓请求体、不开遮罩
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext();
  const page = await context.newPage(); page.setDefaultTimeout(4000);
  const pageErrors = []; const consoleErrors = [];
  page.on('pageerror', (e) => pageErrors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
  const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(value); };
  const modelOpts = () => page.locator('#hostedModel option').evaluateAll((els) => els.map((o) => [o.value, o.textContent]));
  const fill = async (name) => {
    await page.locator('#hostedCli').selectOption('codex'); await page.locator('#hostedLlm').selectOption('openai');
    await page.locator('#hostedAccount').selectOption('acc1'); await page.locator('#hostedPlan').selectOption('2c4g'); await type('#hostedName', name);
  };
  const create = async (name) => { const n = writes.length; await fill(name); await page.locator('#hostedCreateBtn').click(); await page.locator('#hostedFeedback').filter({ hasText: /./ }).waitFor(); return writes.slice(n); };
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await page.locator('#hostedForm').waitFor({ state: 'visible' });

    await test('the model dropdown lists 默认 first then the Hub-aggregated models, de-duplicated strings', async () => {
      assert.deepStrictEqual(await modelOpts(), [['', '默认'], ['gpt-a', 'gpt-a'], ['gpt-b', 'gpt-b'], ['gpt-c', 'gpt-c']]);
    });

    await test('choosing a model sends it in the create body (real keystrokes for the name)', async () => {
      await page.locator('#hostedModel').selectOption('gpt-b');
      const w = await create('Pick');
      assert.strictEqual(w.length, 1); assert.strictEqual(w[0].model, 'gpt-b'); assert.strictEqual(w[0].name, 'Pick');
    });

    await test('默认 sends no model key at all (old request shape unchanged)', async () => {
      await page.locator('#hostedModel').selectOption('');
      const w = await create('Dflt');
      assert.strictEqual(w.length, 1); assert.ok(!('model' in w[0]), `body: ${JSON.stringify(w[0])}`);
    });

    await test('model_not_available (422): our wording, no Hub text, the form input is kept', async () => {
      await page.locator('#hostedModel').selectOption('gpt-c');
      await fill('Mna'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '所选模型当前没有在线节点支持' }).waitFor();
      assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /SECRET-HUB-TEXT|model_not_available/);
      assert.strictEqual(await page.locator('#hostedName').inputValue(), 'Mna');
      assert.strictEqual(await page.locator('#hostedModel').inputValue(), 'gpt-c');
    });

    await test('model_not_supported (422): our wording', async () => {
      await fill('Mns'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '这个 CLI 不支持指定模型' }).waitFor();
      assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /SECRET-HUB-TEXT|model_not_supported/);
    });

    await test('no models offered (missing or empty): the dropdown only says 默认 with the reason, creation is still possible and carries no model', async () => {
      for (const v of [undefined, []]) {
        optModels = v; await page.locator('#hostedRefresh').click();
        await page.waitForFunction(() => document.getElementById('hostedModel').options.length === 1);
        assert.deepStrictEqual(await modelOpts(), [['', '默认（当前没有可选模型）']]);
        const w = await create(v === undefined ? 'Nomod' : 'Emptymod');
        assert.ok(!('model' in w[0]));
      }
    });

    await test('the roster shows each hosted seat\'s effective model: value, 默认 for empty, nothing when the field is absent, nothing on self-hosted', async () => {
      const txt = (id) => page.locator(`#rosterItems .roster-row[data-agent-id="${id}"] td`).first().textContent();
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-m1"]').waitFor();
      assert.match(await txt('ag-m1'), /withm\s*模型：gpt-b/);
      assert.match(await txt('ag-m2'), /defm\s*模型：默认/);
      assert.strictEqual(await txt('ag-m3'), 'oldh');
      assert.strictEqual(await txt('ag-s1'), 'selfh');
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (409|422)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((r) => server.close(r)); }
  console.log(`hosted model browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
