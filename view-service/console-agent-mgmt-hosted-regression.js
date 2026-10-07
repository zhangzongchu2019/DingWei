// G3 · Agent 管理 Tab 的「托管 Agent 生成区 / 进度蒙版 / 托管生命周期操作」浏览器闸。
// 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 台账 31738b3b(业主改序:托管先行) · 依据 V6/V8 草稿与示意图评审件第五节
//
// 装置与 console-agent-mgmt-enroll-regression.js 同源:console 页从生产源码实时抠,Hub 用本地桩。
// 接口按 G4 契约 v2(runbooks/CONTRACT-G4-CONSOLE-API-DRAFT-20261001.md,草稿待冻结):combos 带 cred_kind、托管状态在行里的 hosted 子对象。
//    契约改动只需改页面里的 HOSTED_API 与本件桩路由。
// 口径:真键盘(click + keyboard.type);pageerror 与 console error 两路监听,末尾断言为空(只放行桩有意返回的 4xx)。
//
// MUTATION=<name> 删除对应守卫;对应那条断言必须红(判"首红是不是该条",后续用例共享桩状态会级联):
//    unverified-selectable · no-linkage · hardcoded-mask · destroy-no-confirm · drop-confirm-flag · show-secret · hosted-shows-revoke
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome;NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const { execFileSync } = require('child_process');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const hostedRegistered = process.env.HOSTED_REGISTERED !== '0';
// 走生产 HandleConsolePage 的服务端注入路径取得页面；装置不得再把 false 改成 true。
// 本夹具只选择要验证的服务端渲染值，不证明路由联动；Go 真 PG 四子格另守
//「wireHosted 实际注册成功 ⇒ true；未注册/失败/旧平台 ⇒ false」。
let html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: path.join(__dirname, '..'), encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'],
  env: { ...process.env, DW_CONSOLE_HOSTED_FIXTURE: hostedRegistered ? '1' : '0' },
});
assert(html.includes(`var FEATURE_HOSTED='${hostedRegistered}'==='true';`), 'real server injection returned the wrong hosted feature value');
assert(!html.includes('{{FEATURE_HOSTED}}'), 'server left FEATURE_HOSTED placeholder unresolved');
const mutation = process.env.MUTATION || '';
// 变异开关形态：本脚本读【环境变量】MUTATION=<名>。给了 --mutate=<名>（argv 形态，别的脚本用它）会被静默忽略、跑成「未变异」而全绿——fail-closed。
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>, not --mutate=<name> (the argv form would be silently ignored)'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  // 🔴 锚点失效（页面改了，变异的目标文本没了或不唯一）必须【大声】失败：stderr 打 MUTATION-ANCHOR-STALE、退出码 3，不得静默——
  // 打不上去的变异什么也没测，却会被当成「已经验过」。退出码约定见文件末尾（1＝变异被杀·0＝存活·3＝变异无效）。
  if (hits !== 1) {
    console.error(`MUTATION-ANCHOR-STALE: mutation ${mutation} anchor must match exactly once, hits=${hits}: ${from.slice(0, 160)}`);
    process.exit(3);
  }
  html = html.replace(from, to);
}
const MUTATIONS = ['mutation-anchor-selftest', 'hosted-online-no-group-refresh', 'ignore-hosted-disabled', 'always-hide-hosted', 'hosted-group-still-required', 'hosted-new-group-no-check', 'disabled-offers-upgrade', 'disabled-offers-restart', 'suspended-loses-to-hosted', 'selfhost-suspended-plain', 'quota-only-zero', 'quota-old-fallback', 'quota-generic', 'unverified-selectable', 'no-linkage', 'hardcoded-mask', 'destroy-no-confirm', 'drop-confirm-flag', 'show-secret', 'hosted-shows-revoke', 'nav-opens-by-name', 'ignore-cred-kind', 'ignore-suspended', 'backup-failed-unmapped', 'drop-in-use'];
if (mutation === 'mutation-anchor-selftest') mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x'); // 自检:必须以退出码 3 报 MUTATION-ANCHOR-STALE
if (mutation && !MUTATIONS.includes(mutation)) { console.error(`MUTATION-UNKNOWN: ${mutation} is not a mutation of this script`); process.exit(3); }
if (mutation === 'ignore-hosted-disabled') mutate('if(!FEATURE_HOSTED){var hc=', 'if(false){var hc=');
if (mutation === 'always-hide-hosted') mutate('if(!FEATURE_HOSTED){var hc=', 'if(true){var hc=');
if (mutation === 'hosted-group-still-required') mutate("if(!v)return {fields:{}};", "if(!v)return {error:'请选择所属编组'};");
if (mutation === 'hosted-online-no-group-refresh') mutate("enrollFeedback('hostedFeedback',name+' 已上线');refreshGroups('hosted');", "enrollFeedback('hostedFeedback',name+' 已上线');");
if (mutation === 'hosted-new-group-no-check') mutate("if(!NEW_GROUP_RE.test(n))return {error:'编组名只能用 1～8 位字母或数字'};", '');
if (mutation === 'backup-failed-unmapped') mutate("backup_failed:'销毁前备份数据卷失败,容器没有被删除,请稍后再试或联系管理员',", '');
if (mutation === 'quota-only-zero') mutate('&&data.remaining[d[0]]<=0)used.push', '&&data.remaining[d[0]]===0)used.push');
if (mutation === 'quota-old-fallback') mutate("quota_exceeded:'配额不足，请联系管理员调整配额',", "quota_exceeded:'Agent 数量已达上限',");
if (mutation === 'quota-generic') mutate("if(code==='quota_exceeded'&&data&&data.remaining&&typeof data.remaining==='object'){", "if(false){");
if (mutation === 'nav-opens-by-name') mutate('b.onclick=function(){ openTab(a.full_name) };', 'b.onclick=function(){ openTab(a.name) };');
if (mutation === 'ignore-cred-kind') mutate('&&(!a.kind||c.cred_kind===a.kind);});', '&&true;});');
if (mutation === 'suspended-loses-to-hosted') mutate("Object.assign({online:a.online},a.hosted||{},a.state==='suspended'?{state:'disabled'}:{})", "Object.assign({online:a.online},a.state==='suspended'?{state:'disabled'}:{},a.hosted||{})");
if (mutation === 'selfhost-suspended-plain') mutate("(a.state==='suspended'?'已停用':(a.online?'在线':'离线'))", "(a.online?'在线':'离线')");
if (mutation === 'disabled-offers-upgrade') mutate("?[['enable','启用',true],['destroy','销毁',true]]", "?[['upgrade','升级',false],['enable','启用',true],['destroy','销毁',true]]");
if (mutation === 'disabled-offers-restart') mutate("?[['enable','启用',true],['destroy','销毁',true]]", "?[['restart','重启',false],['enable','启用',true],['destroy','销毁',true]]");
if (mutation === 'ignore-suspended') mutate("a.hosted||{},a.state==='suspended'?{state:'disabled'}:{})", "a.hosted||{})");
if (mutation === 'unverified-selectable') mutate("o.disabled=!!it.disabled;", 'o.disabled=false;');
if (mutation === 'no-linkage') mutate("var rows=(opts.accounts||[]).filter(function(a){return a.llm===llm;});", 'var rows=(opts.accounts||[]);');
if (mutation === 'hardcoded-mask') mutate("hostedMaskRender(name,stage,'',lastStage);\n    if(stage==='online')", "hostedMaskRender(name,'online','',lastStage);\n    if(stage==='online')");
if (mutation === 'destroy-no-confirm') mutate("if(!confirm('销毁 '+name+'：容器将被删除,凭据立即吊销;数据卷会先备份一次并保留 14 天,到期删除;备份不占你的配额。确认销毁？'))return;", '');
if (mutation === 'drop-confirm-flag') mutate("if(d[0]==='destroy'){if(!confirm('销毁 '+name+'：容器将被删除,凭据立即吊销;数据卷会先备份一次并保留 14 天,到期删除;备份不占你的配额。确认销毁？'))return;body.confirm=true;}", "if(d[0]==='destroy'){if(!confirm('销毁 '+name+'：容器将被删除,凭据立即吊销;数据卷会先备份一次并保留 14 天,到期删除;备份不占你的配额。确认销毁？'))return;}");
if (mutation === 'drop-in-use') mutate("+(a.in_use>0?'（已被 '+a.in_use+' 个 Agent 使用）':'')", '');
if (mutation === 'show-secret') mutate("text:a.name+'（…'+a.last4+'）'", "text:a.name+'（'+a.secret+'）'");
if (mutation === 'hosted-shows-revoke') mutate("revoke.hidden=a.origin==='hosted';", 'revoke.hidden=false;');

const GROUP_MINE = 'A'.repeat(32);
let agents = [{ id: 'ag-leader', key_id: 'key-leader', session_name: 'zzc-leader-2642', online: true }];
let hostedAgents = [{ agent_id: 'ag-offline', state: 'abnormal', upgradable: false, node_name: 'node-b' }];
agents.push({ id: 'ag-offline', key_id: 'key-off', session_name: 'zzc-offline-2642', online: false, origin: 'hosted' });
let planBigFits = false;
let failOptions = 1;
const writes = [];
const jobs = {};
const extraGroups = [];   // R-V2-0102-GROUP-FIXES A：桩照 Hub 行为——带 new_group_name 的建席上线后，该组出现、新席为组长
let jobSeq = 0;
function options() {
  return {
    nodes: [
      { id: 'n1', name: 'node-a', pool: 'p', online: true, free_mem_gb: 12, free_vcpu: 6, free_disk_gb: 80 },
      { id: 'n2', name: 'node-b', pool: 'p', online: false, free_mem_gb: 30, free_vcpu: 8, free_disk_gb: 200 },
    ],
    combos: [
      { cli: 'codex', llm: 'openai', cred_kind: 'api_key', verified: true }, { cli: 'codex', llm: 'openai', cred_kind: 'token_file', verified: false },
      { cli: 'codex', llm: 'deepseek', cred_kind: 'api_key', verified: false },
      { cli: 'opencode', llm: 'deepseek', cred_kind: 'api_key', verified: false }, { cli: 'claude', llm: 'anthropic', cred_kind: 'api_key', verified: true },
    ],
    accounts: [
      { id: 'acc1', name: '团队主账号', llm: 'openai', kind: 'api_key', last4: '7f3a', secret: 'SK-LEAK-1' },
      { id: 'acc2', name: '备用账号', llm: 'openai', kind: 'api_key', last4: '91bc', secret: 'SK-LEAK-2', in_use: 2 },
      { id: 'acc4', name: '订阅令牌账号', llm: 'openai', kind: 'token_file', last4: 'aa11', secret: 'SK-LEAK-4' },
      { id: 'acc3', name: '团队 anthropic', llm: 'anthropic', kind: 'api_key', last4: '0d2e', secret: 'SK-LEAK-3' },
    ],
    plans: [
      { id: '2c4g', name: '2C/4G', vcpu: 2, mem_gb: 4, fits: true },
      { id: '4c8g', name: '4C/8G', vcpu: 4, mem_gb: 8, fits: planBigFits },
    ],
    quota: { remaining: { agents: 3, vcpu: 6, mem_gb: 12, disk_gb: 100 } },
  };
}
function groupsPayload() {
  return { principal: 'owner', owned_agents: agents, groups: extraGroups.concat([
    { code: GROUP_MINE, short_name: '团队甲', creator_principal: 'owner', can_manage: true, owner_display_name: '业主甲', leader_key_id: 'key-leader', leader_session_name: 'zzc-leader-2642', additions_closed: false,
      members: agents.map((a) => ({ agent_id: a.id, key_id: a.key_id, session_name: a.session_name, online: a.online })) },
  ]) };
}
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function err(res, status, code, extra) { json(res, status, { error: code, detail: 'SECRET-HUB-TEXT', ...extra }); }
const getPaths = [];
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (req.method === 'GET') getPaths.push(p);
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  // 设计件 3.2 新形状(agent_id/name/full_name/origin/group_code/online);session 一并带上只为让左栏(尚未迁移的旧调用方)继续工作
  // GET /api/console/agents:设计件 3.2 新形状 + G4 契约 v2 的 hosted 子对象 {node_name,state,upgradable}(仅托管行);
  if (p === '/api/console/agents') {
    return json(res, 200, { agents: agents.map((a) => {
      const name = a.session_name.split('-')[1]; const h = hostedAgents.find((x) => x.agent_id === a.id);
      return { ...(a.suspended ? { state: 'suspended' } : { state: 'active' }), online: a.online, agent_id: a.id, name, full_name: `${name}.zzc`, origin: a.origin === 'hosted' ? 'hosted' : 'selfhost', group_code: GROUP_MINE, ...(h ? { hosted: { state: h.state, upgradable: h.upgradable, node_name: h.node_name } } : {}) };
    }) });
  }
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups' && req.method === 'GET') return json(res, 200, groupsPayload());
  if (p === '/api/console/enroll/applications') return json(res, 200, { applications: [] });
  if (p === '/api/console/llm-accounts' && req.method === 'GET') return json(res, 200, []); // 0.2.1.101「我的 LLM 账号」区首载
  if (p === '/api/console/hosted/options') {
    if (failOptions > 0) { failOptions -= 1; res.writeHead(503, { 'content-type': 'text/plain' }); return res.end('options unavailable'); }
    return json(res, 200, options());
  }
  if (p === '/api/console/hosted/agents' && req.method === 'POST') {
    const body = await read(req); writes.push({ path: p, body });
    if (!/^[A-Za-z0-9]+$/.test(String(body.name))) return err(res, 422, 'name_invalid');
    if (agents.some((a) => a.session_name.split('-')[1].toLowerCase() === String(body.name).toLowerCase())) return err(res, 409, 'name_taken');
    if (body.new_group_name === 'Newteam') return err(res, 409, 'group_name_taken');   // 只为抓请求体、不开遮罩
    if (String(body.name) === 'Gnew') return err(res, 409, 'name_taken');   // 同上:抓已有编组的请求体,不开遮罩
    if (String(body.name) === 'Quotaodd') return err(res, 403, 'quota_exceeded', { remaining: { agents: 2, vcpu: 6, mem_gb: '0', disk_gb: null } });
    if (String(body.name) === 'Quotaneg') return err(res, 403, 'quota_exceeded', { remaining: { agents: 2, vcpu: -1, mem_gb: 3, disk_gb: 3 } });
    if (String(body.name) === 'Quotabare') return err(res, 403, 'quota_exceeded');
    if (String(body.name) === 'Quotamem') return err(res, 403, 'quota_exceeded', { remaining: { agents: 2, vcpu: 6, mem_gb: 0, disk_gb: 0 } });
    const o = options(); const acct = o.accounts.find((a) => a.id === body.account_id);
    if (!o.combos.some((c) => c.cli === body.cli && c.llm === body.llm)) return err(res, 422, 'combo_incompatible');
    if (!o.combos.some((c) => c.cli === body.cli && c.llm === body.llm && c.verified && (!acct || c.cred_kind === acct.kind))) return err(res, 422, 'combo_not_verified');
    const id = `j${++jobSeq}`; jobs[id] = { stage: 'queued', reason: '', name: body.name, made: false, newGroup: body.new_group_name || '' };
    return json(res, 202, { job_id: id });
  }
  const j = p.match(/^\/api\/console\/hosted\/jobs\/([^/]+)$/);
  if (j) {
    const job = jobs[j[1]]; if (!job) return err(res, 404, 'not_found');
    if (job.stage === 'online' && !job.made) {
      job.made = true; const id = `ag-${job.name.toLowerCase()}`;
      agents.push({ id, key_id: `key-${job.name}`, session_name: `zzc-${job.name.toLowerCase()}-2642`, online: true, origin: 'hosted' });
      hostedAgents.push({ agent_id: id, state: 'running', upgradable: true, node_name: 'node-a' });
      if (job.newGroup) {
        const sess = `zzc-${job.name.toLowerCase()}-2642`;
        extraGroups.push({ code: 'N'.repeat(31) + String(jobSeq), short_name: job.newGroup, creator_principal: 'owner', can_manage: true, owner_display_name: '业主甲',
          leader_key_id: `key-${job.name}`, leader_session_name: sess, additions_closed: false,
          members: [{ agent_id: id, key_id: `key-${job.name}`, session_name: sess, online: true }] });
      }
    }
    return json(res, 200, { stage: job.stage, reason: job.reason });
  }
  const o = p.match(/^\/api\/console\/hosted\/agents\/([^/]+)\/(upgrade|restart|disable|enable|destroy)$/);
  if (o && req.method === 'POST') {
    const id = decodeURIComponent(o[1]); const op = o[2]; const body = await read(req); writes.push({ path: p, body });
    const h = hostedAgents.find((x) => x.agent_id === id);
    if (!h) return err(res, 404, 'not_found');
    if (id === 'ag-offline') return err(res, 409, 'node_offline');
    if (id === 'ag-nope') return err(res, 403, 'forbidden_role');
    if (id === 'ag-nobackup' && op === 'destroy' && body.confirm === true) return err(res, 503, 'backup_failed', { detail: 'backup_pipeline_not_ready' });
    if (id === 'ag-boom') return err(res, 500, 'internal_error');
    if (id === 'ag-params') return err(res, 400, 'params_invalid');
    if ((op === 'disable' || op === 'destroy') && body.confirm !== true) return err(res, 400, 'confirm_required');
    if (op === 'disable') h.state = 'disabled';
    if (op === 'enable') h.state = 'running';
    if (op === 'restart') h.state = 'running';
    if (op === 'upgrade') h.upgradable = false;
    if (op === 'destroy') { hostedAgents = hostedAgents.filter((x) => x.agent_id !== id); agents = agents.filter((a) => a.id !== id); }
    return json(res, 200, { state: h.state });
  }
  if (p.startsWith('/view/')) { res.writeHead(200, { 'content-type': 'text/html' }); return res.end('<!doctype html><title>v</title>'); }
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

    if (!hostedRegistered) {
      await test('unregistered hosted routes remove the hosted panel and make zero hosted API requests', async () => {
        assert.strictEqual(await page.locator('#hostedCreate').count(), 0);
        await page.waitForTimeout(200);
        assert.deepStrictEqual(getPaths.filter((p) => p.startsWith('/api/console/hosted/')), []);
      });
    } else {
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
      assert.match(nodes[1].text, /剩余 12G \/ 6C \/ 盘 80G/);
      assert.strictEqual(nodes[0].text, '自动');
    });

    await test('CLI → LLM → account linkage: incompatible combos are absent, unverified are greyed, accounts follow the LLM, change resets downstream', async () => {
      assert.deepStrictEqual((await opts('#hostedLlm')).map((o) => o.text), ['请先选择 CLI']);
      await page.locator('#hostedCli').selectOption('codex');
      const llm = await opts('#hostedLlm');
      assert.deepStrictEqual(llm.map((o) => [o.value, o.disabled]), [['', false], ['openai', false], ['deepseek', true]], 'only codex combos (openai once although it has two credential kinds); anthropic is not even listed');
      await page.locator('#hostedLlm').selectOption('openai');
      const acc = await opts('#hostedAccount');
      assert.deepStrictEqual(acc.map((o) => [o.value, o.disabled]), [['', false], ['acc1', false], ['acc2', false], ['acc4', true]], 'accounts of the chosen LLM; the token_file one needs a verified token_file combo, which does not exist yet');
      assert.strictEqual(acc[1].text, '团队主账号（…7f3a）');
      assert.strictEqual(acc[2].text, '备用账号（…91bc）（已被 2 个 Agent 使用）', 'shared account shows how many Agents already use it (hint only; still selectable)');
      assert.strictEqual(acc[2].disabled, false, 'in_use never disables the option');
      assert.match(acc[3].text, /未验通/);
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

    await test('the group is optional on the create form: no-group is first and default, and a create without a group sends neither group_id nor new_group_name', async () => {
      await fillValid('Gnew');
      assert.deepStrictEqual(await page.locator('#hostedGroup option').allTextContents(), ['不加入编组', '团队甲', '新建编组…']);
      await page.locator('#hostedGroup').selectOption('');
      const before = writes.length;
      await page.evaluate(() => { document.getElementById('hostedFeedback').textContent = ''; });
      await page.locator('#hostedCreateBtn').click();
      for (let i = 0; i < 100 && writes.length === before; i++) await page.waitForTimeout(20);
      assert.strictEqual(writes.length, before + 1, 'not choosing a group must not block the create');
      const body = writes[writes.length - 1].body;
      assert.ok(!('group_id' in body) && !('new_group_name' in body), `no group keys at all, got ${JSON.stringify(body)}`);
      await page.locator('#hostedName').fill('');
    });

    await test('new group on the create form: an empty or malformed name is blocked locally; a valid name sends new_group_name instead of group_id; an existing group still sends group_id', async () => {
      await fillValid('Gnew');
      assert.ok((await page.locator('#hostedGroup option').allTextContents()).includes('新建编组…'), 'the new-group choice is offered');
      assert.strictEqual(await page.locator('#hostedNewGroupBox').isHidden(), true);
      await page.locator('#hostedGroup').selectOption('__new__');
      await page.locator('#hostedNewGroupBox').waitFor({ state: 'visible' });
      let before = writes.length;
      await page.evaluate(() => { document.getElementById('hostedFeedback').textContent = ''; });
      await page.locator('#hostedCreateBtn').click();
      assert.match(await page.locator('#hostedFeedback').textContent(), /请填写新编组的名字/);
      assert.strictEqual(writes.length, before, 'no name ⇒ nothing sent');
      await page.locator('#hostedNewGroup').fill('a b'); await page.locator('#hostedCreateBtn').click();
      assert.match(await page.locator('#hostedFeedback').textContent(), /1～8 位字母或数字/);
      assert.strictEqual(writes.length, before, 'malformed name ⇒ nothing sent');
      await page.locator('#hostedNewGroup').fill(''); await page.locator('#hostedNewGroup').click(); await page.keyboard.type('Newteam');
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '编组名已被使用' }).waitFor({ state: 'visible' });
      const last = writes[writes.length - 1];
      assert.strictEqual(last.body.new_group_name, 'Newteam');
      assert.ok(!('group_id' in last.body), 'a new group is not sent together with a group_id');
      assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /SECRET-HUB-TEXT/);
      // 再选已有编组:收起名字框,发 group_id、不带 new_group_name
      await page.locator('#hostedGroup').selectOption(GROUP_MINE);
      assert.strictEqual(await page.locator('#hostedNewGroupBox').isHidden(), true, 'choosing an existing group hides the name box');
      await page.locator('#hostedCreateBtn').click();
      for (let i = 0; i < 100 && writes[writes.length - 1] === last; i++) await page.waitForTimeout(20);
      assert.strictEqual(writes[writes.length - 1].body.group_id, GROUP_MINE);
      assert.ok(!('new_group_name' in writes[writes.length - 1].body));
      await page.locator('#hostedName').fill(''); await page.evaluate(() => { document.getElementById('hostedNewGroup').value = ''; });
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
      await page.locator('#hostedFeedback').filter({ hasText: '已被你名下的 Agent 使用' }).waitFor({ state: 'visible' });
      assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /SECRET-HUB-TEXT/);
      assert.strictEqual(await page.locator('#hostedName').inputValue(), 'Leader');
      assert.strictEqual(await page.locator('#hostedMask').isHidden(), true, 'no mask for a refused create');
      await type('#hostedName', 'bad-name'); await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '只能用大小写字母和数字' }).waitFor({ state: 'visible' });
    });

    await test('quota_exceeded names the exhausted dimensions from remaining, in our wording', async () => {
      await fillValid('Quotamem');
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: '配额不足' }).waitFor({ state: 'visible' });
      const t = await page.locator('#hostedFeedback').textContent();
      assert.match(t, /内存、磁盘已用完/);
      assert.doesNotMatch(t, /CPU|SECRET-HUB-TEXT/);
    });

    await test('a non-positive remaining counts as exhausted (<=0), not just exactly 0', async () => {
      await fillValid('Quotaneg');
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedFeedback').filter({ hasText: 'CPU已用完' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#hostedFeedback').textContent(), /CPU已用完/);
    });

    await test('quota_exceeded without an exhausted dimension gets the neutral wording, never a misleading Agent-count one', async () => {
      for (const name of ['Quotaodd', 'Quotabare']) {
        await fillValid(name);
        await page.evaluate(() => { document.getElementById('hostedFeedback').textContent = ''; });   // 清掉上一例的提示,免得陈旧文案冒充本例结果
        await page.locator('#hostedCreateBtn').click();
        await page.locator('#hostedFeedback').filter({ hasText: '配额不足，请联系管理员' }).waitFor({ state: 'visible' });
        assert.doesNotMatch(await page.locator('#hostedFeedback').textContent(), /Agent 数量|SECRET-HUB-TEXT/);
      }
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
      await fillValid('Foxtrot'); await page.locator('#hostedCreateBtn').click();      // 契约 v2 的排队超时原因码(不可取消,10 分钟超时算失败)
      await page.locator('#hostedMask').waitFor({ state: 'visible' });
      jobs.j4.stage = 'failed'; jobs.j4.reason = 'queue_timeout';
      await page.locator('#hostedMaskResult').filter({ hasText: '排队超时' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#hostedMaskResult').textContent(), '创建失败：排队超时（超过 10 分钟）');
      assert.strictEqual(await page.locator('#hostedMaskBox button').count(), 0, 'no cancel button: queued jobs are not cancellable');
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
      assert.strictEqual(writes.length, before); assert.match(dialogs[0], /数据卷保留/); assert.match(dialogs[0], /凭据暂停、无法再接入/, 'the confirmation says what suspension means for the credential');
      dialogAccept = true;
      await gamma.locator('[data-op="disable"]').click();
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"] td:nth-child(2)')?.textContent === '已停用');
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/disable', body: { confirm: true } });
      assert.deepStrictEqual(await gamma.locator('.hosted-op').allTextContents(), ['启用', '销毁']);
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

    await test('a hosted row the Hub reports only as state=suspended (no hosted sub-object yet) is shown as stopped, with enable offered', async () => {
      agents.push({ id: 'ag-susp', key_id: 'key-susp', session_name: 'zzc-susp-2642', online: false, origin: 'hosted', suspended: true });
      await page.evaluate(() => refreshRoster());
      const row = page.locator('#rosterItems .roster-row[data-agent-id="ag-susp"]');
      await row.waitFor({ state: 'visible' });
      assert.strictEqual(await row.locator('td').nth(1).textContent(), '已停用');
      assert.deepStrictEqual(await row.locator('.hosted-op').allTextContents(), ['启用', '销毁']);
      agents = agents.filter((a) => a.id !== 'ag-susp'); await page.evaluate(() => refreshRoster());
    });

    await test('top-level state=suspended wins over hosted.state=running; a suspended self-hosted row is also shown as stopped', async () => {
      hostedAgents.push({ agent_id: 'ag-sw', state: 'running', upgradable: false, node_name: 'node-a' });
      agents.push({ id: 'ag-sw', key_id: 'key-sw', session_name: 'zzc-sw-2642', online: true, origin: 'hosted', suspended: true });
      agents.push({ id: 'ag-sh', key_id: 'key-sh', session_name: 'zzc-sh-2642', online: false, origin: 'selfhost', suspended: true });
      await page.evaluate(() => refreshRoster());
      const hosted = page.locator('#rosterItems .roster-row[data-agent-id="ag-sw"]');
      await hosted.waitFor({ state: 'visible' });
      assert.strictEqual(await hosted.locator('td').nth(1).textContent(), '已停用', 'suspended on top must beat hosted.state=running');
      assert.deepStrictEqual(await hosted.locator('.hosted-op').allTextContents(), ['启用', '销毁']);
      assert.strictEqual(await page.locator('#rosterItems .roster-row[data-agent-id="ag-sh"] td').nth(1).textContent(), '已停用');
      agents = agents.filter((a) => a.id !== 'ag-sw' && a.id !== 'ag-sh'); hostedAgents = hostedAgents.filter((h) => h.agent_id !== 'ag-sw'); await page.evaluate(() => refreshRoster());
    });

    await test('backup_failed / internal_error / params_invalid are shown in our wording; the Hub detail never reaches the page', async () => {
      for (const id of ['ag-nobackup', 'ag-boom', 'ag-params']) {
        hostedAgents.push({ agent_id: id, state: 'running', upgradable: false, node_name: 'node-a' });
        agents.push({ id, key_id: `key-${id}`, session_name: `zzc-${id.slice(3)}-2642`, online: true, origin: 'hosted' });
      }
      await page.evaluate(() => refreshRoster());
      dialogAccept = true;
      const cases = [['ag-nobackup', 'destroy', /销毁前备份数据卷失败/], ['ag-boom', 'restart', /服务端出错了/], ['ag-params', 'restart', /内容不完整或格式不对/]];
      for (const [id, op, re] of cases) {
        await page.evaluate(() => { document.getElementById('rosterFeedback').textContent = ''; });
        await page.locator(`#rosterItems .roster-row[data-agent-id="${id}"] [data-op="${op}"]`).click();
        await page.locator('#rosterFeedback').filter({ hasText: re }).waitFor({ state: 'visible' });
        assert.doesNotMatch(await page.locator('#rosterFeedback').textContent(), /backup_pipeline_not_ready|SECRET-HUB-TEXT|HTTP/, `${id}: only our wording`);
      }
      hostedAgents = hostedAgents.filter((x) => !['ag-nobackup', 'ag-boom', 'ag-params'].includes(x.agent_id));
      agents = agents.filter((a) => !['ag-nobackup', 'ag-boom', 'ag-params'].includes(a.id));
      await page.evaluate(() => refreshRoster());
    });

    await test('a role-refused operation says so in our wording', async () => {
      hostedAgents.push({ agent_id: 'ag-nope', state: 'running', upgradable: false, node_name: 'node-a' });
      agents.push({ id: 'ag-nope', key_id: 'key-nope', session_name: 'zzc-nope-2642', online: true, origin: 'hosted' });
      await page.evaluate(() => refreshRoster());
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-nope"] [data-op="restart"]').click();
      await page.locator('#rosterFeedback').filter({ hasText: '你的角色不足' }).waitFor({ state: 'visible' });
      hostedAgents = hostedAgents.filter((x) => x.agent_id !== 'ag-nope'); agents = agents.filter((a) => a.id !== 'ag-nope');
      await page.evaluate(() => refreshRoster());
    });

    await test('destroy needs a second confirmation naming the consequence; dismiss sends nothing, accept sends confirm:true and the row goes', async () => {
      const gamma = page.locator('#rosterItems .roster-row[data-agent-id="ag-gamma"]');
      const before = writes.length; dialogAccept = false; dialogs.length = 0;
      await gamma.locator('[data-op="destroy"]').click(); await page.waitForTimeout(250);
      assert.strictEqual(writes.length, before); assert.match(dialogs[0], /先备份一次并保留 14 天,到期删除;备份不占你的配额/); assert.match(dialogs[0], /凭据立即吊销/); assert.doesNotMatch(dialogs[0], /不可恢复/, 'the old "unrecoverable" wording is wrong now: the volume is backed up for 14 days'); assert.doesNotMatch(dialogs[0], /恢复/, 'the page must not promise a restore: there is no restore entry in this release');
      dialogAccept = true;
      await gamma.locator('[data-op="destroy"]').click();
      await page.waitForFunction(() => !document.querySelector('#rosterItems .roster-row[data-agent-id="ag-gamma"]'));
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-gamma/destroy', body: { confirm: true } });
    });

    await test('busy hosted agent: every operation button is greyed', async () => {
      hostedAgents.push({ agent_id: 'ag-offline', state: 'busy', upgradable: true, node_name: 'node-b' });
      hostedAgents = hostedAgents.filter((x, i, arr) => arr.findLastIndex((y) => y.agent_id === x.agent_id) === i);
      await page.evaluate(() => refreshRoster());
      await page.waitForFunction(() => document.querySelector('#rosterItems .roster-row[data-agent-id="ag-offline"] td:nth-child(2)')?.textContent === '处理中…');
      const states = await page.locator('#rosterItems .roster-row[data-agent-id="ag-offline"] .hosted-op').evaluateAll((els) => els.map((e) => e.disabled));
      assert.deepStrictEqual(states, [true, true, true, true]);
    });

    await test('the self-hosted review area is offered by default alongside the hosted area (enabled by the real server injection path)', async () => {
      assert.strictEqual(await page.locator('#enrollReview').count(), 1);
      assert.ok(getPaths.includes('/api/console/enroll/applications'), 'the review list is requested');
      const heads = (await page.locator('.group-workspace h3').allTextContents()).map((t) => t.trim()).filter((t) => ['编组列表', '自备 Agent 审核', '托管 Agent 生成', 'Agent 列表'].includes(t));
      assert.ok(heads.includes('自备 Agent 审核') && heads.includes('托管 Agent 生成'), `headings: ${JSON.stringify(heads)}`);
    });

    await test('lifecycle state rides on /api/console/agents: no separate hosted-agents read; a hosted row without state shows only online/offline', async () => {
      assert.ok(getPaths.includes('/api/console/agents'));
      assert.ok(!getPaths.includes('/api/console/hosted/agents'), 'the page must not read a separate hosted-agents list');
      agents.push({ id: 'ag-nostate', key_id: 'key-ns', session_name: 'zzc-nostate-2642', online: true, origin: 'hosted' });
      await page.evaluate(() => refreshRoster());
      const row = page.locator('#rosterItems .roster-row[data-agent-id="ag-nostate"]');
      await row.waitFor({ state: 'visible' });
      assert.strictEqual(await row.locator('td').nth(1).textContent(), '在线', 'no lifecycle state invented');
      assert.strictEqual(await row.locator('.roster-revoke').isHidden(), true);
      agents = agents.filter((a) => a.id !== 'ag-nostate');
      await page.evaluate(() => refreshRoster());
    });

    await test('left list uses full_name: button shows the short name, title/tab/iframe use full_name', async () => {
      await page.locator('#agentNavMode').click();            // 树 -> 纯 Agent 列表
      const btn = page.locator('#a button').filter({ hasText: /^leader$/ });
      await btn.waitFor({ state: 'visible' });
      assert.strictEqual(await btn.getAttribute('title'), 'leader.zzc');
      await btn.click();
      await page.locator('#tabs .tab[title="leader.zzc"]').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#panes iframe').first().getAttribute('src'), '/view/leader.zzc');
      await page.locator('#tabs .tab[title="leader.zzc"] .x').click();
      await page.locator('#agentNavMode').click();
    });

    await test('the group panel is still there: list, create toggle; join-by-code stays removed', async () => {
      assert.strictEqual(await page.locator('#groupListTable').count(), 1);
      assert.strictEqual(await page.locator('#groupAddOpen').count(), 1);
      assert.strictEqual(await page.locator('#groupCodeJoinOpen').count(), 0);
    });
    }

    // 断言面＝页面：建席上线后编组列表按 Hub 回的数据刷新，新组出现、组长是新席；实现承诺（Hub 真建组、新席为组长）
    // 在 cmd/workpulse/hosted_new_group_test.go 用真 identity＋真存储钉，本格只钉页面这一侧不丢。
    await test('a create with a new group shows that group in the group list once online, led by the new agent', async () => {
      await fillValid('Kilo');
      await page.locator('#hostedGroup').selectOption('__new__');
      await page.locator('#hostedNewGroupBox').waitFor({ state: 'visible' });
      await page.locator('#hostedNewGroup').fill('Kilogrp');
      await page.locator('#hostedCreateBtn').click();
      await page.locator('#hostedMask').waitFor({ state: 'visible' });
      const sent = writes.at(-1).body;
      assert.strictEqual(sent.new_group_name, 'Kilogrp'); assert.ok(!('group_id' in sent));
      const jid = Object.keys(jobs).find((k) => jobs[k].name === 'Kilo');
      await page.mouse.click(5, 5); await page.locator('#hostedMask').waitFor({ state: 'hidden' });
      jobs[jid].stage = 'online';
      await page.locator('#hostedFeedback').filter({ hasText: 'Kilo 已上线' }).waitFor({ state: 'visible' });
      await page.waitForFunction(() => Array.from(document.querySelectorAll('.group-row')).some((r) => r.cells[0]?.textContent === 'Kilogrp'));
      const row = await page.locator('.group-row').evaluateAll((els) => els.map((r) => Array.from(r.cells).slice(0, 2).map((c) => c.textContent)).find((c) => c[0] === 'Kilogrp'));
      assert.deepStrictEqual(row, ['Kilogrp', 'kilo'], 'new group listed with the new seat as leader');
      await page.locator('#hostedGroup').selectOption(GROUP_MINE);
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|403|404|409|422|500|503)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`hosted console browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  // 退出码约定（变异模式与正常模式一致：用例失败即 1）：
  //   1 ＝ 有用例失败（变异被杀＝「红」，且输出里有 FAIL 行）· 0 ＝ 全绿（变异模式下＝变异【存活】）
  //   3 ＝ MUTATION-ANCHOR-STALE（变异的锚点没命中，变异无效——不得算红，也不得算存活）· 2 ＝ 脚本自己崩了
  // 判「变异红」只认【退出码 1 且有 FAIL 行】；其余一律不是红。
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
