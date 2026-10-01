// G3 · Agent 管理 Tab 的「托管 Agent 生成区 / 进度蒙版 / 托管生命周期操作」浏览器闸。
// 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 台账 31738b3b(业主改序:托管先行) · 依据 V6/V8 草稿与示意图评审件第五节
//
// 装置与 console-agent-mgmt-enroll-regression.js 同源:console 页从生产源码实时抠,Hub 用本地桩。
// 🔴 Hub 接口为【提案】(HOSTED_API,已发 G4 kimi30kqtuxerh 对齐);落板后只改页面里的 HOSTED_API 与本件桩路由两处。
// 口径:真键盘(click + keyboard.type);pageerror 与 console error 两路监听,末尾断言为空(只放行桩有意返回的 4xx)。
//
// MUTATION=<name> 删除对应守卫;对应那条断言必须红(判"首红是不是该条",后续用例共享桩状态会级联):
//    unverified-selectable · no-linkage · hardcoded-mask · destroy-no-confirm · drop-confirm-flag · show-secret · hosted-shows-revoke
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome;NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const source = fs.readFileSync(path.join(__dirname, '..', 'internal/m8/console_auth.go'), 'utf8');
const start = source.indexOf('const consolePageHTML = `');
const end = source.indexOf('`\n\nfunc (h *Hub) HandleConsoleAgents', start);
assert(start >= 0 && end > start, 'cannot extract consolePageHTML from production source');
let html = source.slice(start + 'const consolePageHTML = `'.length, end);
const mutation = process.env.MUTATION || '';
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  assert.strictEqual(hits, 1, `mutation anchor must match exactly once: ${from} hits=${hits}`);
  html = html.replace(from, to);
}
const MUTATIONS = ['unverified-selectable', 'no-linkage', 'hardcoded-mask', 'destroy-no-confirm', 'drop-confirm-flag', 'show-secret', 'hosted-shows-revoke'];
if (mutation && !MUTATIONS.includes(mutation)) throw new Error(`unknown mutation ${mutation}`);
if (mutation === 'unverified-selectable') mutate("o.disabled=!!it.disabled;", 'o.disabled=false;');
if (mutation === 'no-linkage') mutate("var rows=(opts.accounts||[]).filter(function(a){return a.llm===llm;});", 'var rows=(opts.accounts||[]);');
if (mutation === 'hardcoded-mask') mutate("hostedMaskRender(name,stage,'',lastStage);\n    if(stage==='online')", "hostedMaskRender(name,'online','',lastStage);\n    if(stage==='online')");
if (mutation === 'destroy-no-confirm') mutate("if(!confirm('销毁 '+name+'：容器和数据卷将被删除,凭据立即吊销,不可恢复。确认销毁？'))return;", '');
if (mutation === 'drop-confirm-flag') mutate("if(d[0]==='destroy'){if(!confirm('销毁 '+name+'：容器和数据卷将被删除,凭据立即吊销,不可恢复。确认销毁？'))return;body.confirm=true;}", "if(d[0]==='destroy'){if(!confirm('销毁 '+name+'：容器和数据卷将被删除,凭据立即吊销,不可恢复。确认销毁？'))return;}");
if (mutation === 'show-secret') mutate("text:a.name+'（…'+a.last4+'）'", "text:a.name+'（'+a.secret+'）'");
if (mutation === 'hosted-shows-revoke') mutate("revoke.hidden=a.origin==='hosted'||!!hs;", 'revoke.hidden=false;');

const GROUP_MINE = 'A'.repeat(32);
let agents = [{ id: 'ag-leader', key_id: 'key-leader', session_name: 'zzc-leader-2642', online: true }];
let hostedAgents = [{ agent_id: 'ag-offline', state: 'abnormal', upgradable: false, node_name: 'node-b' }];
agents.push({ id: 'ag-offline', key_id: 'key-off', session_name: 'zzc-offline-2642', online: false, origin: 'hosted' });
let planBigFits = false;
let failOptions = 1;
const writes = [];
const jobs = {};
let jobSeq = 0;
function options() {
  return {
    nodes: [
      { id: 'n1', name: 'node-a', pool: 'p', online: true, free_mem_gb: 12, free_vcpu: 6 },
      { id: 'n2', name: 'node-b', pool: 'p', online: false, free_mem_gb: 30, free_vcpu: 8 },
    ],
    combos: [
      { cli: 'codex', llm: 'openai', verified: true }, { cli: 'codex', llm: 'deepseek', verified: false },
      { cli: 'opencode', llm: 'deepseek', verified: false }, { cli: 'claude', llm: 'anthropic', verified: true },
    ],
    accounts: [
      { id: 'acc1', name: '团队主账号', llm: 'openai', last4: '7f3a', secret: 'SK-LEAK-1' },
      { id: 'acc2', name: '备用账号', llm: 'openai', last4: '91bc', secret: 'SK-LEAK-2' },
      { id: 'acc3', name: '团队 anthropic', llm: 'anthropic', last4: '0d2e', secret: 'SK-LEAK-3' },
    ],
    plans: [
      { id: '2c4g', name: '2C/4G', vcpu: 2, mem_gb: 4, fits: true },
      { id: '4c8g', name: '4C/8G', vcpu: 4, mem_gb: 8, fits: planBigFits },
    ],
    quota: { remaining: { agents: 3, vcpu: 6, mem_gb: 12, disk_gb: 100 } },
  };
}
function groupsPayload() {
  return { principal: 'owner', owned_agents: agents, groups: [
    { code: GROUP_MINE, short_name: '团队甲', creator_principal: 'owner', can_manage: true, owner_display_name: '业主甲', leader_key_id: 'key-leader', leader_session_name: 'zzc-leader-2642', additions_closed: false,
      members: agents.map((a) => ({ agent_id: a.id, key_id: a.key_id, session_name: a.session_name, online: a.online })) },
  ] };
}
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function err(res, status, code, extra) { json(res, status, { error: code, detail: 'SECRET-HUB-TEXT', ...extra }); }
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/api/console/agents') return json(res, 200, { agents: agents.map((a) => ({ session: a.session_name, online: a.online })) });
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups' && req.method === 'GET') return json(res, 200, groupsPayload());
  if (p === '/api/console/enroll/applications') return json(res, 200, { applications: [] });
  if (p === '/api/console/hosted/options') {
    if (failOptions > 0) { failOptions -= 1; res.writeHead(503, { 'content-type': 'text/plain' }); return res.end('options unavailable'); }
    return json(res, 200, options());
  }
  if (p === '/api/console/hosted/agents' && req.method === 'GET') return json(res, 200, { agents: hostedAgents });
  if (p === '/api/console/hosted/agents' && req.method === 'POST') {
    const body = await read(req); writes.push({ path: p, body });
    if (!/^[A-Za-z0-9]+$/.test(String(body.name))) return err(res, 422, 'name_invalid');
    if (agents.some((a) => a.session_name.split('-')[1].toLowerCase() === String(body.name).toLowerCase())) return err(res, 409, 'name_taken');
    if (!options().combos.some((c) => c.cli === body.cli && c.llm === body.llm && c.verified)) return err(res, 422, 'combo_not_verified');
    const id = `j${++jobSeq}`; jobs[id] = { stage: 'queued', reason: '', name: body.name, made: false };
    return json(res, 202, { job_id: id });
  }
  const j = p.match(/^\/api\/console\/hosted\/jobs\/([^/]+)$/);
  if (j) {
    const job = jobs[j[1]]; if (!job) return err(res, 404, 'not_found');
    if (job.stage === 'online' && !job.made) {
      job.made = true; const id = `ag-${job.name.toLowerCase()}`;
      agents.push({ id, key_id: `key-${job.name}`, session_name: `zzc-${job.name.toLowerCase()}-2642`, online: true, origin: 'hosted' });
      hostedAgents.push({ agent_id: id, state: 'running', upgradable: true, node_name: 'node-a' });
    }
    return json(res, 200, { stage: job.stage, reason: job.reason });
  }
  const o = p.match(/^\/api\/console\/hosted\/agents\/([^/]+)\/(upgrade|restart|disable|enable|destroy)$/);
  if (o && req.method === 'POST') {
    const id = decodeURIComponent(o[1]); const op = o[2]; const body = await read(req); writes.push({ path: p, body });
    const h = hostedAgents.find((x) => x.agent_id === id);
    if (!h) return err(res, 404, 'not_found');
    if (id === 'ag-offline') return err(res, 409, 'node_offline');
    if ((op === 'disable' || op === 'destroy') && body.confirm !== true) return err(res, 400, 'confirm_required');
    if (op === 'disable') h.state = 'disabled';
    if (op === 'enable') h.state = 'running';
    if (op === 'restart') h.state = 'running';
    if (op === 'upgrade') h.upgradable = false;
    if (op === 'destroy') { hostedAgents = hostedAgents.filter((x) => x.agent_id !== id); agents = agents.filter((a) => a.id !== id); }
    return json(res, 200, { state: h.state });
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext();
  const page = await context.newPage(); page.setDefaultTimeout(2500);
  const pageErrors = []; const consoleErrors = []; const dialogs = []; let dialogAccept = true;
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });
  page.on('dialog', (d) => { dialogs.push(d.message()); (dialogAccept ? d.accept() : d.dismiss()).catch(() => {}); });
  const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(value); };
  const opts = (sel) => page.locator(`${sel} option`).evaluateAll((els) => els.map((o) => ({ text: o.textContent, value: o.value, disabled: o.disabled })));
  const stageStates = () => page.locator('#hostedMaskBox .hosted-stage').evaluateAll((els) => els.map((e) => `${e.dataset.stage}:${e.dataset.state}`));
  const fillValid = async (name) => {
    await page.locator('#hostedCli').selectOption('codex'); await page.locator('#hostedLlm').selectOption('openai');
    await page.locator('#hostedAccount').selectOption('acc1'); await page.locator('#hostedPlan').selectOption('2c4g');
    await page.locator('#hostedGroup').selectOption(GROUP_MINE); await type('#hostedName', name);
  };
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await page.evaluate(() => { hostedPollMs = 120; });

    await test('options load failure shows the reason and a reachable retry; the form stays hidden; retry shows the form with the remaining quota', async () => {
      await page.locator('#hostedStatus').filter({ hasText: '托管选项加载失败' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#hostedStatus').textContent(), /503/);
      assert.strictEqual(await page.locator('#hostedForm').isHidden(), true);
      await page.locator('#hostedRetry').click();
      await page.locator('#hostedForm').waitFor({ state: 'visible' });
      assert.match(await page.locator('#hostedQuota').textContent(), /剩余额度:Agent 3 个 · vCPU 6 · 内存 12G · 磁盘 100G/);
    });

    await test('CLI list greys out unverified CLIs; nodes show free resources and offline ones are disabled', async () => {
      const cli = await opts('#hostedCli');
      assert.deepStrictEqual(cli.map((o) => [o.value, o.disabled]), [['', false], ['codex', false], ['opencode', true], ['claude', false]]);
      assert.match(cli.find((o) => o.value === 'opencode').text, /未验通/);
      const nodes = await opts('#hostedNode');
      assert.deepStrictEqual(nodes.map((o) => [o.value, o.disabled]), [['', false], ['n1', false], ['n2', true]]);
      assert.match(nodes[1].text, /剩余 12G \/ 6C/);
      assert.strictEqual(nodes[0].text, '自动');
    });

    await test('CLI → LLM → account linkage: incompatible combos are absent, unverified are greyed, accounts follow the LLM, change resets downstream', async () => {
      assert.deepStrictEqual((await opts('#hostedLlm')).map((o) => o.text), ['请先选择 CLI']);
      await page.locator('#hostedCli').selectOption('codex');
      const llm = await opts('#hostedLlm');
      assert.deepStrictEqual(llm.map((o) => [o.value, o.disabled]), [['', false], ['openai', false], ['deepseek', true]], 'only codex combos; anthropic is not even listed');
      await page.locator('#hostedLlm').selectOption('openai');
      const acc = await opts('#hostedAccount');
      assert.deepStrictEqual(acc.map((o) => o.value), ['', 'acc1', 'acc2'], 'accounts of the chosen LLM only');
      assert.strictEqual(acc[1].text, '团队主账号（…7f3a）');
      await page.locator('#hostedAccount').selectOption('acc1');
      await page.locator('#hostedCli').selectOption('claude');
      assert.strictEqual(await page.locator('#hostedLlm').inputValue(), '', 'LLM reset after CLI change');
      assert.strictEqual(await page.locator('#hostedAccount').inputValue(), '', 'account reset after CLI change');
    });

    await test('plans over the remaining quota are greyed and cannot be chosen', async () => {
      const plans = await opts('#hostedPlan');
      assert.deepStrictEqual(plans.map((o) => [o.value, o.disabled]), [['', false], ['2c4g', false], ['4c8g', true]]);
      assert.match(plans[2].text, /超出剩余额度/);
    });

    await test('no secret is ever rendered anywhere on the page', async () => {
      assert.doesNotMatch(await page.locator('body').innerText(), /SK-LEAK/);
      assert.doesNotMatch(await page.locator('#hostedAccount').evaluate((s) => s.outerHTML), /SK-LEAK/);
    });

    await test('incomplete form is hinted locally and nothing is sent', async () => {
      const before = writes.length;
      await page.locator('#hostedCreateBtn').click();
      assert.match(await page.locator('#hostedFeedback').textContent(), /请填写名称并选择/);
      assert.strictEqual(writes.length, before);
    });

    await test('Hub refusals are shown in our wording, never the Hub text, and the typed values are kept', async () => {
      await fillValid('Leader');                                    // 与已有 leader 同名(按小写)
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '已被占用' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await page.locator('#hostedName').inputValue(), 'Leader');
      assert.strictEqual(await page.locator('#hostedMask').isHidden(), true, 'no mask for a refused create');
      await type('#hostedName', 'bad-name'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '只能使用大小写字母和数字' }).waitFor({ state: 'visible' });
    });

    await test('successful create opens a mask that reports the REAL stage the Hub reports, step by step', async () => {
      await type('#hostedName', 'Gamma');
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedMask').waitFor({ state: 'visible' });
      assert.deepStrictEqual(writes.at(-1).body, { name: 'Gamma', group_id: GROUP_MINE, node_id: 'auto', cli: 'codex', llm: 'openai', account_id: 'acc1', plan_id: '2c4g' });
      await page.waitForFunction(() => document.querySelector('#hostedMaskBox .hosted-stage[data-stage="queued"]')?.dataset.state === 'current');
      assert.deepStrictEqual(await stageStates(), ['queued:current', 'pulling:todo', 'starting:todo', 'online:todo']);
      jobs.j1.stage = 'pulling';
      await page.waitForFunction(() => document.querySelector('#hostedMaskBox .hosted-stage[data-stage="pulling"]')?.dataset.state === 'current');
      assert.deepStrictEqual(await stageStates(), ['queued:done', 'pulling:current', 'starting:todo', 'online:todo']);
      jobs.j1.stage = 'starting';
      await page.waitForFunction(() => document.querySelector('#hostedMaskBox .hosted-stage[data-stage="starting"]')?.dataset.state === 'current');
      assert.deepStrictEqual(await stageStates(), ['queued:done', 'pulling:done', 'starting:current', 'online:todo']);
      assert.strictEqual(await page.locator('#hostedMaskResult').textContent(), '');
    });

    await test('clicking anywhere dismisses the mask; the job keeps going and finishes in the background (form line + roster)', async () => {
      await page.mouse.click(5, 5);
      await page.locator('#hostedMask').waitFor({ state: 'hidden' });
      jobs.j1.stage = 'online';
      await page.locator('#hostedFeedback').filter({ hasText: 'Gamma 已上线' }).waitFor({ state: 'visible' });
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#hostedMask').isHidden(), true, 'a dismissed mask does not pop back');
    });

    await test('failed job: failing stage is marked, earlier ones done, later ones untouched, reason in our wording; unknown code is shown generically', async () => {
      await fillValid('Delta'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedMask').waitFor({ state: 'visible' });
      jobs.j2.stage = 'pulling';
      await page.waitForFunction(() => document.querySelector('#hostedMaskBox .hosted-stage[data-stage="pulling"]')?.dataset.state === 'current');
      jobs.j2.stage = 'failed'; jobs.j2.reason = 'image_pull_failed';
      await page.locator('#hostedMaskResult').filter({ hasText: '创建失败' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#hostedMaskResult').textContent(), '创建失败：拉取镜像失败');
      assert.deepStrictEqual(await stageStates(), ['queued:done', 'pulling:failed', 'starting:todo', 'online:todo']);
      await page.mouse.click(5, 5);
      await fillValid('Echo'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedMask').waitFor({ state: 'visible' });
      jobs.j3.stage = 'failed'; jobs.j3.reason = 'something_new';
      await page.locator('#hostedMaskResult').filter({ hasText: '原因码 something_new' }).waitFor({ state: 'visible' });
      await page.mouse.click(5, 5);
    });

    await test('hosted rows show state and lifecycle buttons, never revoke; self-hosted/legacy rows are unchanged', async () => {
      const gamma = page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]');
      assert.strictEqual(await gamma.locator('td').nth(1).textContent(), '运行中（可升级）');
      assert.deepStrictEqual(await gamma.locator('.hosted-op').allTextContents(), ['升级', '重启', '停用', '销毁']);
      assert.strictEqual(await gamma.locator('.roster-revoke').isHidden(), true, 'a hosted agent is not revoked here');
      const leader = page.locator('#rosterItems .roster-row[data-agent-id="ag-leader"]');
      assert.strictEqual(await leader.locator('.hosted-op').count(), 0);
      assert.strictEqual(await leader.locator('.roster-revoke').isVisible(), true);
      assert.strictEqual(await page.locator('#rosterItems .roster-row[data-agent-id="ag-offline"] td').nth(1).textContent(), '异常');
    });

    await test('upgrade and restart send no confirmation; upgrade clears the "可升级" badge', async () => {
      const gamma = page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]');
      dialogs.length = 0;
      await gamma.locator('[data-op="upgrade"]').click();
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"] td:nth-child(2)')?.textContent === '运行中');
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/upgrade', body: {} });
      await gamma.locator('[data-op="restart"]').click();
      await page.waitForTimeout(250);
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/restart', body: {} });
      assert.strictEqual(dialogs.length, 0, 'no confirm for upgrade/restart');
    });

    await test('disable needs a second confirmation: dismiss sends nothing, accept sends confirm:true, then the row offers enable', async () => {
      const gamma = page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]');
      const before = writes.length; dialogAccept = false; dialogs.length = 0;
      await gamma.locator('[data-op="disable"]').click(); await page.waitForTimeout(250);
      assert.strictEqual(writes.length, before); assert.match(dialogs[0], /数据卷保留/);
      dialogAccept = true;
      await gamma.locator('[data-op="disable"]').click();
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"] td:nth-child(2)')?.textContent === '已停用');
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/disable', body: { confirm: true } });
      assert.deepStrictEqual(await gamma.locator('.hosted-op').allTextContents(), ['升级', '重启', '启用', '销毁']);
      await gamma.locator('[data-op="enable"]').click();
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"] td:nth-child(2)')?.textContent === '运行中');
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/enable', body: {} });
    });

    await test('node offline: the operation fails with our wording and the row is left as it was', async () => {
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-offline"] [data-op="restart"]').click();
      await page.locator('#rosterFeedback').filter({ hasText: '节点离线' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await page.locator('#rosterFeedback').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await page.locator('#rosterItems .roster-row[data-agent-id="ag-offline"] [data-op="restart"]').isEnabled(), true, 'buttons come back after a failure');
    });

    await test('destroy needs a second confirmation naming the consequence; dismiss sends nothing, accept sends confirm:true and the row goes', async () => {
      const gamma = page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]');
      const before = writes.length; dialogAccept = false; dialogs.length = 0;
      await gamma.locator('[data-op="destroy"]').click(); await page.waitForTimeout(250);
      assert.strictEqual(writes.length, before); assert.match(dialogs[0], /数据卷将被删除.*不可恢复/);
      dialogAccept = true;
      await gamma.locator('[data-op="destroy"]').click();
      await page.waitForFunction(() => !document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"]'));
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/destroy', body: { confirm: true } });
    });

    await test('busy hosted agent: every operation button is greyed', async () => {
      hostedAgents.push({ agent_id: 'ag-offline', state: 'busy', upgradable: true, node_name: 'node-b' });
      hostedAgents = hostedAgents.filter((x, i, arr) => arr.findLastIndex((y) => y.agent_id === x.agent_id) === i);
      await page.evaluate(() => refreshHostedRoster());
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-offline"] td:nth-child(2)')?.textContent === '处理中…');
      const states = await page.locator('#rosterItems .roster-row[data-agent-id="ag-offline"] .hosted-op').evaluateAll((els) => els.map((e) => e.disabled));
      assert.deepStrictEqual(states, [true, true, true, true]);
    });

    await test('the group panel is still there: list, create toggle; join-by-code stays removed', async () => {
      assert.strictEqual(await page.locator('#groupListTable').count(), 1);
      assert.strictEqual(await page.locator('#groupAddOpen').count(), 1);
      assert.strictEqual(await page.locator('#groupCodeJoinOpen').count(), 0);
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|404|409|422|503)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`hosted console browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) { if (fail < 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
