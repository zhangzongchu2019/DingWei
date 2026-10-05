// 包 C2 · 托管生命周期按钮接 A1（job 化）的浏览器闸。派工 R-V2-SEAT-ITEMS-DISPATCH-20261005000613（0.2.1.100）。
// 页面据实时抠自生产 HandleConsolePage（console-hosted-page-fixture，托管开），Hub 用本地桩：POST 操作 ⇒ 202+{job_id}，GET jobs/{id} 按剧本给阶段。
// 守的是：点后显示「<操作>中…」并锁按钮 · 阶段随轮询变 · 完成才刷新 · 失败显示中文原因＋原因码 · 同步 422 invalid_state 当场显示并重取列表 ·
//   Hub 把升级/重启中的席位报成 online 时不提前算完成 · 进行中状态在行重绘后不丢 · 轮询抖动不算失败 · 轮询有上限 · 旧的同步 200 路径仍可用。
// MUTATION=<名>（环境变量；argv --mutate 形态退出 3），锚点必须唯一命中否则 exit 3；退出码 1＝红（变异被杀）· 0＝绿 · 2＝崩 · 3＝变异无效。
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
  'job-ignored': () => mutate('if(jobId){hostedOpTrack(a,d[0],jobId);return;}', ''),
  'no-inflight-lock': () => mutate("busy=hs.state==='busy'||!!hostedOpInflight[a.agent_id],list=[]", "busy=hs.state==='busy',list=[]"),
  'no-inflight-text': () => mutate('st.textContent=hostedOpInflight[a.agent_id]?hostedOpInflightText(hostedOpInflight[a.agent_id]):(hs?', 'st.textContent=(hs?'),
  'no-stage-text': () => mutate("var st=inf.stage&&inf.stage!=='queued'?", "var st=false?"),
  'online-while-busy-is-done': () => mutate("if(op!=='destroy'&&hs&&hs.state==='busy'){", 'if(false){'),
  'fail-drops-code': () => mutate("'（原因码 '+(reason||'未知')+'）'", "''"),
  'fail-drops-reason-text': () => mutate("(HOSTED_FAIL_REASONS[reason]||'操作未成功')", "'操作未成功'"),
  'poll-error-is-failure': () => mutate('}catch(err){inf.timer=setTimeout(tick,hostedPollMs);return;}', "}catch(err){stage='failed';reason='x';}"),
  'sync-422-no-refetch': () => mutate("if(res.code==='invalid_state')refreshRoster();return;}", 'return;}'),
  'poll-no-cap': () => mutate('if(Date.now()-inf.started>HOSTED_OP_MAX_MS){', 'if(false){'),
  'destroy-gone-ignored': () => mutate("if(op==='destroy'&&!rowOf()&&!HOSTED_FAIL_REASONS[reason]){", 'if(false){'),
  'destroy-failed-as-success': () => mutate("if(op==='destroy'&&!rowOf()&&!HOSTED_FAIL_REASONS[reason]){", "if(op==='destroy'){"),
  'reasons-missing-backup': () => mutate("backup_failed:'销毁前备份数据卷失败，容器没有被删除',", ''),
  'reasons-missing-rollback': () => mutate("容器没有被删除',upgrade_failed_rolled_back:'升级失败，已自动回滚到原版本',invalid_state", "容器没有被删除',invalid_state"),
  'legacy-sync-broken': () => mutate("      refreshRoster();refreshGroups('hosted-op');refreshAgents();\n    };", '    };'),
};
if (mutation && !M[mutation]) { console.error(`MUTATION-UNKNOWN: ${mutation}`); process.exit(3); }
if (mutation) M[mutation]();

const G = 'A'.repeat(32);
let rows;
function reset() {
  rows = {};
  for (const n of ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf']) rows[n] = { id: `ag-${n}`, state: 'running', upgradable: true, online: true };
}
reset();
const jobs = {}; let jobSeq = 0; const writes = []; const getCounts = { agents: 0 }; const polls = {}; let pollFail = 0;
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function read(req) { return new Promise((r) => { let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => r(b ? JSON.parse(b) : {})); }); }
// 每个 job 一份剧本：逐次轮询取下一步，最后一步停住；步里的 fn 在被取到时改桩状态
function newJob(steps) { const id = `j${++jobSeq}`; jobs[id] = steps.slice(); polls[id] = 0; return id; }
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/api/console/agents') {
    getCounts.agents++;
    return json(res, 200, { agents: Object.entries(rows).map(([n, r]) => ({ state: 'active', online: r.online, agent_id: r.id, name: n, full_name: `${n}.zzc`, origin: 'hosted', group_code: G, hosted: { state: r.state, upgradable: r.upgradable, node_name: 'node-a' } })) });
  }
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups') return json(res, 200, { principal: 'owner', owned_agents: [], groups: [] });
  if (p === '/api/console/enroll/applications') return json(res, 200, { applications: [] });
  if (p === '/api/console/hosted/options') return json(res, 200, { nodes: [], combos: [], accounts: [], plans: [], quota: { remaining: { agents: 1, vcpu: 1, mem_gb: 1, disk_gb: 1 } } });
  const j = p.match(/^\/api\/console\/hosted\/jobs\/([^/]+)$/);
  if (j) {
    const steps = jobs[j[1]]; if (!steps) return json(res, 404, { error: 'not_found' });
    polls[j[1]]++;
    if (pollFail > 0) { pollFail--; res.writeHead(500); return res.end('boom'); }
    const s = steps.length > 1 ? steps.shift() : steps[0];
    if (s.fn) s.fn();
    return json(res, 200, { stage: s.stage, reason: s.reason || '' });
  }
  const o = p.match(/^\/api\/console\/hosted\/agents\/([^/]+)\/(upgrade|restart|disable|enable|destroy)$/);
  if (o && req.method === 'POST') {
    const id = o[1]; const op = o[2]; const body = await read(req); writes.push({ path: p, body });
    const name = id.replace('ag-', ''); const r = rows[name];
    if (name === 'bravo') return json(res, 422, { error: 'invalid_state', detail: 'SECRET-HUB-TEXT' });
    if (name === 'golf') { r.state = 'running'; return json(res, 200, { state: 'running' }); }   // 旧的同步成功
    const finish = (stage, reason, fn) => ({ stage, reason, fn });
    let steps;
    if (name === 'alpha') {   // 重启：升级/重启中 Hub 的 job 视图把 busy 报成 online
      r.state = 'busy';
      steps = [finish('queued'), finish('starting'), finish('online'), finish('online'), finish('online', '', () => { r.state = 'running'; })];
    } else if (name === 'charlie') {   // 升级失败回滚
      r.state = 'busy'; steps = [finish('queued'), finish('failed', 'upgrade_failed_rolled_back', () => { r.state = 'running'; })];
    } else if (name === 'delta') {   // 销毁：备份失败，行还在
      steps = [finish('queued'), finish('failed', 'backup_failed')];
    } else if (name === 'echo') {   // 销毁成功：行没了；Hub 把销毁完的座位报成 failed 且无已知原因
      steps = [finish('queued'), finish('failed', '', () => { delete rows.echo; })];
    } else if (name === 'foxtrot') {   // 永远处理中
      steps = [finish('pulling')];
    } else steps = [finish('failed', 'start_failed')];
    return json(res, 202, { job_id: newJob(steps) });
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  if (p.startsWith('/view/')) { res.writeHead(200, { 'content-type': 'text/html' }); return res.end('<!doctype html>'); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext();
  const page = await context.newPage(); page.setDefaultTimeout(4000);
  const pageErrors = []; const consoleErrors = []; const dialogs = [];
  page.on('pageerror', (e) => pageErrors.push(e.message));
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
  page.on('dialog', (d) => { dialogs.push(d.message()); d.accept().catch(() => {}); });
  const row = (n) => page.locator(`#rosterItems .roster-row[data-agent-id="ag-${n}"]`);
  const stText = (n) => row(n).locator('td').nth(1);
  const opsDisabled = (n) => row(n).locator('.hosted-op').evaluateAll((els) => els.map((e) => e.disabled));
  const feedback = () => page.locator('#rosterFeedback');
  const waitStatus = (n, re) => page.waitForFunction(([id, src]) => new RegExp(src).test(document.querySelector(`#rosterItems .roster-row[data-agent-id="${id}"] td:nth-child(2)`)?.textContent || ''), [`ag-${n}`, re.source]);
  const waitFeedback = (re) => page.waitForFunction((src) => new RegExp(src).test(document.getElementById('rosterFeedback')?.textContent || ''), re.source);
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await row('alpha').waitFor({ state: 'visible' });
    await page.evaluate(() => { hostedPollMs = 250; });

    await test('restart returns 202+job: the row shows 重启中… with the stage, every button is locked; Hub reporting online while the row is still busy is NOT done; done only when the row is no longer busy, then 已完成', async () => {
      await row('alpha').locator('[data-op="restart"]').click();
      await waitStatus('alpha', /^重启中…/);
      assert.deepStrictEqual(await opsDisabled('alpha'), [true, true, true, true], 'buttons locked while in flight');
      await waitStatus('alpha', /^重启中…（启动）/);   // 阶段随轮询变
      await page.waitForTimeout(600);   // 此时 job 视图已报 online，但席位行还是 busy
      assert.match(await stText('alpha').textContent(), /^重启中…/, 'online from the job view while the row is busy is not completion');
      assert.doesNotMatch(await feedback().textContent(), /已完成/);
      await waitFeedback(/重启 alpha 已完成/);
      assert.match(await stText('alpha').textContent(), /^运行中/);
      assert.deepStrictEqual(await opsDisabled('alpha'), [false, false, false, false], 'buttons back after completion');
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-alpha/restart', body: {} });
    });

    await test('failed job: 中文原因 + 原因码, never the Hub text; rollback reason is its own wording; buttons come back', async () => {
      await row('charlie').locator('[data-op="upgrade"]').click();
      await waitFeedback(/升级 charlie 失败：升级失败，已自动回滚到原版本（原因码 upgrade_failed_rolled_back）/);
      assert.doesNotMatch(await feedback().textContent(), /SECRET-HUB-TEXT|HTTP/);
      assert.doesNotMatch(await stText('charlie').textContent(), /中…/, 'in-flight text cleared');
      assert.deepStrictEqual(await opsDisabled('charlie'), [false, false, false, false]);
    });

    await test('destroy whose backup fails: the row is still there and the failure says the container was not deleted', async () => {
      await row('delta').locator('[data-op="destroy"]').click();
      await waitFeedback(/销毁 delta 失败：销毁前备份数据卷失败，容器没有被删除（原因码 backup_failed）/);
      assert.strictEqual(await row('delta').count(), 1);
      assert.deepStrictEqual(writes.at(-1), { path: '/api/console/hosted/agents/ag-delta/destroy', body: { confirm: true } });
    });

    await test('destroy that succeeded (row gone) is not shown as a failure even if the Hub job view says failed without a known reason', async () => {
      await row('echo').locator('[data-op="destroy"]').click();
      await waitFeedback(/销毁 echo 已完成/);
      assert.strictEqual(await row('echo').count(), 0);
      assert.doesNotMatch(await feedback().textContent(), /失败/);
    });

    await test('synchronous 422 invalid_state: shown at once in our wording, no job polled, the roster is refetched', async () => {
      const before = getCounts.agents; const jobsBefore = jobSeq;
      await row('bravo').locator('[data-op="restart"]').click();
      await waitFeedback(/该 Agent 当前状态不允许这个操作/);
      assert.doesNotMatch(await feedback().textContent(), /SECRET-HUB-TEXT/);
      await page.waitForTimeout(150);
      assert.ok(getCounts.agents > before, 'roster refetched after invalid_state');
      assert.strictEqual(jobSeq, jobsBefore, 'no job created');
      assert.deepStrictEqual(await opsDisabled('bravo'), [false, false, false, false]);
    });

    await test('in-flight state survives a roster re-render (list refetch) and keeps the buttons locked', async () => {
      await page.evaluate(() => { HOSTED_OP_MAX_MS = 900; });
      await row('foxtrot').locator('[data-op="restart"]').click();
      await waitStatus('foxtrot', /^重启中…（拉镜像）/);
      await page.evaluate(() => refreshRoster()); await page.waitForTimeout(100);
      assert.match(await stText('foxtrot').textContent(), /^重启中…（拉镜像）/);
      assert.deepStrictEqual(await opsDisabled('foxtrot'), [true, true, true, true]);
    });

    await test('a job that never finishes stops at the cap with 仍在处理 (not success, not failure) and unlocks the row', async () => {
      await waitFeedback(/重启 foxtrot：仍在处理，请稍后刷新查看/);
      assert.deepStrictEqual(await opsDisabled('foxtrot'), [false, false, false, false]);
      assert.doesNotMatch(await feedback().textContent(), /失败|已完成/);
    });

    await test('poll glitches (HTTP 500 on the job read) are not a failure: the job keeps being polled and completes', async () => {
      await page.evaluate(() => { HOSTED_OP_MAX_MS = 15 * 60 * 1000; });
      rows.alpha.state = 'running'; pollFail = 2;
      await page.evaluate(() => refreshRoster());
      await row('alpha').locator('[data-op="restart"]').click();
      await waitFeedback(/重启 alpha 已完成/);
      assert.doesNotMatch(await feedback().textContent(), /失败/);
      assert.strictEqual(pollFail, 0);
    });

    await test('legacy synchronous 200 (no job_id) still works: no polling, row refreshed, buttons usable', async () => {
      const jobsBefore = jobSeq;
      await row('golf').locator('[data-op="restart"]').click();
      await page.waitForTimeout(400);
      assert.strictEqual(jobSeq, jobsBefore);
      assert.match(await stText('golf').textContent(), /^运行中/);
      assert.deepStrictEqual(await opsDisabled('golf'), [false, false, false, false]);
      assert.strictEqual(await feedback().textContent(), '');
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (422|500)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((r) => server.close(r)); }
  console.log(`hosted lifecycle jobs browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
