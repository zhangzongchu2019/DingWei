#!/usr/bin/env node
'use strict';
/*
 * P1 真页面（R-V2-SPEC-TASK-P1-PAGE）：Specs 与任务管理，playwright ＋ 真 Hub（新平台，DW_NEWPLAT=1）＋ 真 PG ＋ 真 /ws/agent Agent。
 *
 * 全程不用页面夹具、不用假后端：
 *   - PG：tools/pgtest 起的临时内嵌 PG（HUB_TEST_PG_STRICT 口径：库起不来即整件失败，绝不跳过）；
 *   - Hub：bin/workpulse（本脚本现编）＋ bootstrap-admin 建首个管理用户，显式给全环境变量；
 *   - 页面：系统 Chrome（PW_CHANNEL，缺省 chrome），登录／改密／建 spec／评审／拆解／指派全部用真键盘与真点击
 *     （click()＋keyboard.type()，不用 fill()／type()；页面 console 与 pageerror 两路监听，任何一路出现即红）；
 *   - Agent：真客户端入网代码（tools/sessionhelper 的 enroll）＋裸 /ws/agent 连接（p1_agent.py，不起真 helper）。
 *
 * 流程（每步都是一个 test，首红必须是目标步）：
 *   登录（含首登强制改密）→ console 平台项含 Specs、任务管理 → Specs iframe 建 spec（带验收判据）→ 提交复核 → 补验收字段并存新版
 *   → 复核（复核 Agent＋疑点）→ 审结 → 手工添加任务 → 任务管理 iframe 见 spec 与任务、指派研发／验收（候选＝在线 Agent）
 *   → 结案 → 开工 → 签发 console-tasks 能力令牌 → 新平台 Agent 拉到分给自己的任务；无令牌被拒。
 *
 * 起法（可复跑；需要 go、node＋playwright、python3＋websockets＋pexpect 之一的 venv、系统 Chrome）：
 *   cd <repo>
 *   NODE_PATH=<含 playwright 的 node_modules> P1_PYTHON=<含 websockets 的 python3> PW_CHANNEL=chrome \
 *     node view-service/console-spec-task-p1-real-regression.js [--mutate=<名>]
 *   无系统 Chrome 时用 PW_CHANNEL=chromium（playwright 自带 Chromium）。
 *   环境：P1_PYTHON（缺省 python3）、P1_KEEP=1 保留临时目录与日志、P1_BIN_DIR 复用已编二进制（跳过 go build，仅限无变异）。
 *   同机一次只跑一个本脚本／一个 pgtest 消费方。
 *
 * 变异（--mutate=<名>，在【临时源码副本】里改 Go 源并重编，不动工作树）：
 *   no-platform-items      撤 specs／task-board 平台项           ⇒ 红在「console 平台项」
 *   no-spec-task-routes    关 spec／任务路由注册                  ⇒ 红在「Specs 页」
 *   no-online-agents       任务页在线 Agent 候选恒空              ⇒ 红在「指派候选」
 *   agent-pull-denied      新平台 Agent 能力令牌校验恒拒          ⇒ 红在「Agent 拉任务」
 *   offline-agents-offered 任务页把离线 Agent 也列进候选          ⇒ 红在「指派候选（反）」
 *   no-cap-issue           撤令牌签发口（新平台分支恒 404）          ⇒ 红在「开工并签发令牌」
 *   cap-optional           令牌可缺失（缺／错令牌照放行）          ⇒ 红在「无令牌被拒」
 *   cap-agent-unbound      令牌不绑定 Agent（别的 Agent 可拿去用）  ⇒ 红在「别的 Agent 的凭据」
 *   cap-page-unbound       令牌不绑定页面 id                      ⇒ 红在「错页面 id 的令牌」
 * 退出码：0 通过（变异下＝存活）、1 有红（变异下＝被杀）、2 崩、3 变异无效（锚失配／未知名）。
 *

 * 隔离范围（如实）：令牌绑定 Agent、绑定页面 id、凭据伪造、吊销后失效在本脚本里有格；【跨属主】本场景只有一个用户，不覆盖，
 * 「令牌属主＝Agent 属主」那条比对（newplat_spec_agent.go）在当前不变量下属纵深防御冗余，撤掉它本脚本与 m8 层都无测试变红
 * （复验方 tosonnet55tc2k 实测：owner-check-off 存活）；跨用户隔离由 m8 层 H10 守。
 *
 * 已知边界（如实）：Hub 把 console-tasks 令牌交到 Agent 手里的链路 095 本就没有（方案第六节答复①），本脚本由「人」在页面会话里
 * 签发后经 Agent 进程的命令口交给它，只验 Hub 侧签发与校验；「开工」在页面上没有按钮，用同一浏览器会话的 fetch 调接口。
 */
const assert = require('assert');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');
const readline = require('readline');
const { spawn, spawnSync } = require('child_process');
const { chromium } = require('playwright');

const ROOT = path.join(__dirname, '..');
const MUTATIONS = {
  'no-platform-items': {
    file: 'internal/m8/newplat_console.go',
    from: '\t\tID: "specs", Kind: "spec", Title: "Specs",\n\t\tHref: "#specs", Status: "approved", Active: true,\n\t}, {\n\t\tID: "task-board", Kind: "task-board", Title: "任务管理",\n\t\tHref: "#task-board", Status: "approved", Active: true,\n\t}, {\n',
    to: '',
  },
  'no-spec-task-routes': {
    file: 'cmd/workpulse/console_routes.go',
    from: '\tregisterSpecTaskRoutes(mux, prefixHub)\n\tmux.HandleFunc("POST /api/console/agents/{sessionName}/capability"',
    to: '\t_ = registerSpecTaskRoutes\n\tmux.HandleFunc("POST /api/console/agents/{sessionName}/capability"',
  },
  'no-online-agents': {
    file: 'internal/m8/console_task_board_page.go',
    from: '\t\tif h.consoleTaskAgentOnline(agent) {\n\t\t\tonline = append(online, agent)\n\t\t\tonlineIDs[agent.ID] = true',
    to: '\t\tif false && h.consoleTaskAgentOnline(agent) {\n\t\t\tonline = append(online, agent)\n\t\t\tonlineIDs[agent.ID] = true',
  },
  'agent-pull-denied': {
    file: 'internal/m8/newplat_spec_agent.go',
    from: '\tif capability == nil || capability.PrincipalID != identity.Lower(ag.OwnerLC) ||',
    to: '\tif true || capability == nil || capability.PrincipalID != identity.Lower(ag.OwnerLC) ||',
  },
  'offline-agents-offered': {
    file: 'internal/m8/console_task_board_page.go',
    from: '\t\tif h.consoleTaskAgentOnline(agent) {\n\t\t\tonline = append(online, agent)\n\t\t\tonlineIDs[agent.ID] = true',
    to: '\t\tif true || h.consoleTaskAgentOnline(agent) {\n\t\t\tonline = append(online, agent)\n\t\t\tonlineIDs[agent.ID] = true',
  },
  'no-cap-issue': {
    file: 'internal/m8/newplat_spec_agent.go',
    from: '\tif !ConsoleAgentCapabilityPageAllowed(pageID) {',
    to: '\tif true || !ConsoleAgentCapabilityPageAllowed(pageID) {',
  },
  'cap-optional': {
    edits: [{
      file: 'internal/m8/newplat_spec_agent.go',
      from: '\tif !repoOK || cred == "" || raw == "" || expectedPage == "" {',
      to: '\tif !repoOK || cred == "" || expectedPage == "" {',
    }, {
      file: 'internal/m8/newplat_spec_agent.go',
      from: '\tif capability == nil || capability.PrincipalID != identity.Lower(ag.OwnerLC) ||\n\t\t!h.newPlatformSessionValid(r.Context(), capability.SessionID, capability.PrincipalID) {',
      to: '\tif capability == nil {\n\t\tcapability = &model.ConsolePageCapability{PrincipalID: identity.Lower(ag.OwnerLC), AgentID: ag.ID}\n\t} else if capability.PrincipalID != identity.Lower(ag.OwnerLC) ||\n\t\t!h.newPlatformSessionValid(r.Context(), capability.SessionID, capability.PrincipalID) {',
    }],
  },
  'cap-agent-unbound': {
    file: 'internal/store/console_auth.go',
    from: 'WHERE c.token_hash=? AND c.agent_id=? AND c.page_id=?',
    to: 'WHERE c.token_hash=? AND (c.agent_id=? OR 1=1) AND c.page_id=?',
  },
  'cap-page-unbound': {
    file: 'internal/store/console_auth.go',
    from: 'WHERE c.token_hash=? AND c.agent_id=? AND c.page_id=?',
    to: 'WHERE c.token_hash=? AND c.agent_id=? AND (c.page_id=? OR 1=1)',
  },
};
const mutationArg = (process.argv.find((a) => a.startsWith('--mutate=')) || '').slice(9);
if (process.argv.some((a) => a.startsWith('--mutate') && !a.startsWith('--mutate='))) {
  console.error('MUTATION-SWITCH-WRONG-FORM: use --mutate=<name>');
  process.exit(3);
}
if (mutationArg && !MUTATIONS[mutationArg]) {
  console.error(`MUTATION-UNKNOWN: ${mutationArg} is not a mutation of this script`);
  process.exit(3);
}

const PYTHON = process.env.P1_PYTHON || 'python3';
const TMP = fs.mkdtempSync(path.join(os.tmpdir(), 'dw-p1-'));
const children = [];
let pg = null;
let failed = 0;
let passed = 0;
let skipped = 0;
let firstRed = '';

function log(...a) { console.log(...a); }

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => resolve(p)); });
    s.on('error', reject);
  });
}

function sh(cmd, args, opts = {}) {
  const r = spawnSync(cmd, args, { encoding: 'utf8', ...opts });
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(' ')} failed: ${(r.stderr || r.stdout || '').slice(-600)}`);
  return r.stdout;
}

async function waitFor(pred, ms, what) {
  const end = Date.now() + ms;
  for (;;) {
    const v = await pred();
    if (v) return v;
    if (Date.now() > end) throw new Error(`timeout waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}

function prepareSource() {
  if (!mutationArg) return ROOT;
  const m = MUTATIONS[mutationArg];
  const dst = path.join(TMP, 'src');
  const files = sh('git', ['ls-files', '-z'], { cwd: ROOT, maxBuffer: 64 << 20 }).split('\0').filter(Boolean);
  for (const f of files) {
    const to = path.join(dst, f);
    fs.mkdirSync(path.dirname(to), { recursive: true });
    if (fs.existsSync(path.join(ROOT, f))) fs.copyFileSync(path.join(ROOT, f), to);
  }
  for (const e of (m.edits || [m])) {
    const target = path.join(dst, e.file);
    const text = fs.readFileSync(target, 'utf8');
    const hits = text.split(e.from).length - 1;
    if (hits !== 1) {
      console.error(`MUTATION-ANCHOR-STALE: mutation ${mutationArg} anchor must match exactly once, hits=${hits} in ${e.file}`);
      process.exit(3);
    }
    fs.writeFileSync(target, text.replace(e.from, e.to));
  }
  return dst;
}

function buildBinaries(src) {
  if (process.env.P1_BIN_DIR && !mutationArg) {
    return { hub: path.join(process.env.P1_BIN_DIR, 'workpulse'), pg: path.join(process.env.P1_BIN_DIR, 'pgtest-bin') };
  }
  const hub = path.join(TMP, 'workpulse');
  const pgBin = path.join(TMP, 'pgtest-bin');
  sh('go', ['build', '-o', hub, './cmd/workpulse'], { cwd: src });
  sh('go', ['build', '-o', pgBin, '.'], { cwd: path.join(ROOT, 'tools', 'pgtest') });
  return { hub, pg: pgBin };
}

async function startPG(bin) {
  const proc = spawn(bin, [], { stdio: ['pipe', 'pipe', 'pipe'] }); // stdin 保活：关 stdin ⇒ 停库并自清
  children.push(proc);
  proc.stderr.on('data', () => {});
  const lines = [];
  readline.createInterface({ input: proc.stdout }).on('line', (l) => lines.push(l));
  await waitFor(() => lines.some((l) => l === 'READY=1'), 300000, 'pgtest READY=1');
  const dsn = lines.find((l) => l.startsWith('DSN=')).slice(4);
  pg = { proc, lines };
  return dsn;
}

async function stopPG() {
  if (!pg) return;
  try { pg.proc.stdin.end(); } catch (_) { /* already closed */ }
  await Promise.race([
    new Promise((r) => pg.proc.on('exit', r)),
    new Promise((r) => setTimeout(r, 30000)),
  ]);
  if (pg.proc.exitCode === null) pg.proc.kill('SIGKILL');
  log(`pgtest: ${pg.lines.filter((l) => /^(STOPPED|CLEANED)=/.test(l)).join(' ') || 'no cleanup report'}`);
}

function hubEnv(dsn, port, internal, publicOrigin) {
  return {
    ...process.env,
    DW_NEWPLAT: '1', DW_PUBLIC_ORIGIN: publicOrigin, DW_NEWPLAT_PG_DSN: dsn,
    WP_DATA_DIR: path.join(TMP, 'data'), WP_DB_PATH: path.join(TMP, 'data', 'wp.db'),
    WP_ADDR: `127.0.0.1:${port}`, WP_INTERNAL_ADDR: `127.0.0.1:${internal}`,
    HOME: path.join(TMP, 'home'), FEISHU_TRANSPORT: 'fake', WP_SECRET_KEY: 'k'.repeat(32),
  };
}

function spawnLines(cmd, args, opts, onLine) {
  const proc = spawn(cmd, args, { stdio: ['pipe', 'pipe', 'pipe'], ...opts });
  children.push(proc);
  readline.createInterface({ input: proc.stdout }).on('line', onLine);
  proc.stderr.on('data', (d) => { if (process.env.P1_KEEP) fs.appendFileSync(path.join(TMP, 'stderr.log'), d); });
  return proc;
}

class TestAgent {
  constructor(name, hub, repo) {
    this.name = name; this.hub = hub; this.repo = repo;
    this.state = path.join(TMP, `agent-${name}`);
    this.events = []; this.https = []; this.pairing = ''; this.enrolled = false;
  }
  env() { return { ...process.env, P1_REPO: this.repo }; }
  startEnroll(owner) {
    this.enroll = spawnLines(PYTHON, [path.join(__dirname, 'p1_agent.py'), 'enroll', '--hub', this.hub, '--owner', owner, '--state', this.state],
      { env: this.env() }, (l) => {
        if (l.startsWith('PAIRING_CODE=')) this.pairing = l.slice(13);
        if (l.startsWith('ENROLLED')) this.enrolled = true;
      });
  }
  startServe() {
    this.events = []; this.https = [];
    this.serve = spawnLines(PYTHON, [path.join(__dirname, 'p1_agent.py'), 'serve', '--hub', this.hub, '--state', this.state],
      { env: this.env() }, (l) => {
        try { const o = JSON.parse(l); if (o.http) this.https.push(o.http); else this.events.push(o); } catch (_) { /* ignore */ }
      });
  }
  // 一次性调用（不依赖 /ws/agent 连接还活着；吊销后连接已被 4401 关掉，serve 进程随之退出）
  callOnce(method, p, cap) {
    const r = spawnSync(PYTHON, [path.join(__dirname, 'p1_agent.py'), 'call', '--hub', this.hub, '--state', this.state,
      '--method', method, '--path', p, '--cap', cap || ''], { encoding: 'utf8', env: this.env() });
    const line = (r.stdout || '').split('\n').find((l) => l.startsWith('{"http"'));
    if (!line) throw new Error(`agent call produced no result: ${(r.stderr || '').slice(-200)}`);
    return JSON.parse(line).http;
  }
  online() { return this.events.some((e) => e.online); }
  stopServe() { try { this.serve.kill('SIGTERM'); } catch (_) { /* gone */ } }
  async http(method, p, cap, cred) {
    const before = this.https.length;
    this.serve.stdin.write(JSON.stringify({ op: 'http', method, path: p, cap: cap || '', cred: cred || '' }) + '\n');
    await waitFor(() => this.https.length > before, 20000, `agent ${this.name} http ${p}`);
    return this.https[before];
  }
}

async function cleanup() {
  for (const c of children.reverse()) { try { c.kill('SIGTERM'); } catch (_) { /* gone */ } }
  await new Promise((r) => setTimeout(r, 500));
  await stopPG();
  if (!process.env.P1_KEEP) fs.rmSync(TMP, { recursive: true, force: true });
  else log(`kept: ${TMP}`);
}

// 前后步骤互为前提：某一步红了，其后依赖它的步骤不再空等 30 秒超时，直接记「上游已红」（不计入 failed，首红仍是真正的目标步）。
let chainBroken = false;
async function test(name, fn, { standalone = false } = {}) {
  if (chainBroken && !standalone) { skipped += 1; log(`SKIP ${name}（上游已红）`); return; }
  try { await fn(); passed += 1; log(`PASS ${name}`); } catch (e) {
    failed += 1;
    if (!standalone) chainBroken = true;
    if (!firstRed) firstRed = name;
    log(`FAIL ${name}: ${String(e && e.message || e).split('\n')[0].slice(0, 300)}`);
  }
}

(async () => {
  let browser;
  try {
    const src = prepareSource();
    const bins = buildBinaries(src);
    const dsn = await startPG(bins.pg);
    const port = await freePort();
    const internalPort = await freePort();
    // 🔴 Chrome 只在 localhost 上接受 Secure cookie；Hub 的来源校验按 DW_PUBLIC_ORIGIN 比对，故两处都用 localhost。
    const base = `http://localhost:${port}`;
    fs.mkdirSync(path.join(TMP, 'data'), { recursive: true });
    fs.mkdirSync(path.join(TMP, 'home'), { recursive: true });
    const env = hubEnv(dsn, port, internalPort, base);
    const boot = sh(bins.hub, ['bootstrap-admin', '--user-id', 'adminone', '--short-name', 'ad'], { env, cwd: TMP });
    const initialPassword = (boot.match(/initial_password=(\S+)/) || [])[1];
    assert.ok(initialPassword, 'bootstrap-admin printed no initial password');
    const hub = spawnLines(bins.hub, [], { env, cwd: TMP }, (l) => { if (process.env.P1_KEEP) fs.appendFileSync(path.join(TMP, 'hub.log'), l + '\n'); });
    await waitFor(async () => { try { return (await fetch(`${base}/console/login`)).status === 200; } catch (_) { return false; } }, 60000, 'Hub /console/login');

    browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : { channel: 'chrome' });
    const ctx = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
    const page = await ctx.newPage();
    const consoleErrors = [];
    const pageErrors = [];
    page.on('console', (m) => {
      if (m.type() !== 'error') return;
      if (m.location().url.endsWith('/favicon.ico')) return; // 唯一已知良性：站点没有 favicon
      consoleErrors.push(`${m.text()} @ ${m.location().url}`);
    });
    page.on('pageerror', (e) => pageErrors.push(e.message));
    const jsonOf = async (r) => { try { return await r.json(); } catch (_) { return {}; } };
    const post = (p, data) => ctx.request.post(base + p, { data, headers: { Origin: base } });
    const get = (p) => ctx.request.get(base + p, { headers: { Origin: base } });

    const agents = { opus: new TestAgent('Opus50ts', base, ROOT), kimi: new TestAgent('Kimi', base, ROOT) };
    const state = {};

    await test('登录：真键盘输入口令，首登强制改密后进入 console', async () => {
      await page.goto(`${base}/console/login`);
      await page.click('#pwUser'); await page.keyboard.type('adminone');
      await page.click('#pwPass'); await page.keyboard.type(initialPassword);
      await page.click('#pwGo');
      await page.locator('#chOld').waitFor({ state: 'visible', timeout: 10000 });
      assert.ok((await page.inputValue('#chOld')).length > 0, '旧密码框应由登录页预填（再键入会变成追加）');
      await page.click('#chNew'); await page.keyboard.type('Pw-P1-test-2026!');
      await page.click('#chNew2'); await page.keyboard.type('Pw-P1-test-2026!');
      await page.click('#chGo');
      await page.waitForURL(`${base}/console`, { timeout: 15000 });
    });

    await test('入网：两个新平台 Agent 经真客户端入网代码申请、属主批准、连入 /ws/agent', async () => {
      agents.opus.startEnroll('adminone'); agents.kimi.startEnroll('adminone');
      await waitFor(() => agents.opus.pairing && agents.kimi.pairing, 30000, 'pairing codes');
      const list = await jsonOf(await get('/api/console/enroll/applications'));
      const apps = (list.applications || []).map((a) => a.application_id);
      assert.strictEqual(apps.length, 2, '应有两条待批申请');
      let group = '';
      for (const ag of [agents.opus, agents.kimi]) {
        let ok = false;
        for (const id of apps) {
          const body = { pairing_code: ag.pairing, agent_name: ag.name };
          if (group) body.group_code = group; else body.new_group_name = 'p1grp';
          const r = await post(`/api/console/enroll/applications/${id}/approve`, body);
          if (r.status() === 200) { ok = true; break; }
        }
        assert.ok(ok, `${ag.name} 批准失败`);
        if (!group) {
          const gl = await jsonOf(await get('/api/console/agent-groups'));
          group = ((gl.groups || gl.items || [])[0] || {}).code || '';
          assert.ok(group, '建组后应能取到组码');
        }
      }
      await waitFor(() => agents.opus.enrolled && agents.kimi.enrolled, 40000, 'both enrolled');
      agents.opus.startServe(); agents.kimi.startServe();
      await waitFor(() => agents.opus.online() && agents.kimi.online(), 20000, 'both online');
      const al = await jsonOf(await get('/api/console/agents'));
      const byName = Object.fromEntries((al.agents || []).map((a) => [a.session, a]));
      assert.ok(byName.kimi && byName.opus50ts && byName.kimi.online && byName.opus50ts.online, 'Hub 应见两 Agent 在线');
      state.kimiID = byName.kimi.agent_id; state.opusID = byName.opus50ts.agent_id;
    });

    await test('console 平台项：下发 Specs 与 任务管理 两个入口', async () => {
      await page.goto(`${base}/console`);
      await page.locator('a:has-text("Specs")').first().waitFor({ state: 'visible', timeout: 10000 });
      await page.locator('a:has-text("任务管理")').first().waitFor({ state: 'visible', timeout: 5000 });
    });

    const specs = () => page.frameLocator('iframe[src*="/console/specs"]');
    const tasks = () => page.frameLocator('iframe[src*="/console/tasks"]');
    // 刷新后重新进入任务页：选中 spec 行、点「编辑」，返回研发下拉的候选文本（排序）
    const assigneeCandidates = async () => {
      await page.reload();
      await page.click('a:has-text("任务管理")');
      const f = tasks();
      await f.locator('table:not(#task-table) tbody tr').filter({ hasText: 'P1 真页面 spec' }).first().click();
      await f.locator('#edit-tasks').click();
      return f.locator('.assignee-select').first().evaluate((sel) => [...sel.options].map((o) => o.textContent).filter(Boolean).sort());
    };

    await test('Specs 页：点 Specs 入口，iframe 里空库出现「添加 spec」入口', async () => {
      await page.click('a:has-text("Specs")');
      await specs().locator('#empty-create-spec').waitFor({ state: 'visible', timeout: 15000 });
    });

    await test('建 spec：真键盘填标题、正文（CodeMirror）、验收判据、原文引用与规范锚定', async () => {
      const f = specs();
      await f.locator('#empty-create-spec').click();
      await f.locator('#new-title').click(); await page.keyboard.type('P1 真页面 spec');
      await f.locator('#new-screen .CodeMirror').first().click(); await page.keyboard.type('# 目标\n页面全链路。');
      await f.locator('#new-acceptance').click(); await page.keyboard.type('登录后能在 Specs 里建出本 spec 并审结');
      await f.locator('#new-screen .acceptance-owner-ref').first().click(); await page.keyboard.type('页面全链路');
      await f.locator('#new-anchor').click(); await page.keyboard.type('msg:p1');
      await f.locator('#create-spec').click();
      // 创建成功后页面回到列表屏（#create-status 随新建屏隐藏），以列表里出现该 spec 为准，再用接口核一次
      await f.locator('.spec-title').filter({ hasText: 'P1 真页面 spec' }).first().waitFor({ timeout: 10000 });
      const sl = await jsonOf(await get('/api/console/specs'));
      assert.ok((sl.specs || sl.items || []).some((x) => x.title === 'P1 真页面 spec' && x.state === 'editing'), '接口应见一条 editing 的 spec');
    });

    await test('提交复核：打开 spec，提交复核后状态 editing → review', async () => {
      const f = specs();
      await f.locator('.spec-title').first().click();
      await f.locator('#spec-state').filter({ hasText: 'editing' }).waitFor({ timeout: 10000 });
      await f.locator('#submit-spec-review').click();
      await f.locator('#spec-state').filter({ hasText: 'review' }).waitFor({ timeout: 10000 });
    });

    await test('补验收字段并存新版：来源、核验人、用例，保存为第 2 版', async () => {
      const f = specs();
      await f.locator('#detail-screen .acceptance-generated-by').first().click(); await page.keyboard.type('owner');
      await f.locator('#detail-screen .acceptance-checked-by').first().click(); await page.keyboard.type('ad');
      await f.locator('#detail-screen .acceptance-cases').first().click(); await page.keyboard.type('用例一：建 spec 后列表可见');
      await f.locator('#save-spec').click();
      await f.locator('#edit-status').filter({ hasText: 'saved revision 2' }).waitFor({ timeout: 10000 });
    });

    await test('复核并审结：填复核 Agent 与疑点，保存复核后审结，状态 → final', async () => {
      const f = specs();
      await f.locator('#review-agent-id').click(); await page.keyboard.type(state.kimiID);
      await f.locator('#add-review-issue').click();
      await f.locator('.review-issue-id').first().click(); await page.keyboard.type('I1');
      await f.locator('.review-issue-text').first().click(); await page.keyboard.type('验收用例是否覆盖空库');
      await f.locator('.review-issue-response').first().click(); await page.keyboard.type('已加用例');
      await f.locator('.review-issue-outcome').first().selectOption('true_problem');
      await f.locator('#save-gate-review').click();
      await f.locator('#workflow-status').filter({ hasText: '已完成' }).waitFor({ timeout: 10000 });
      await f.locator('#finalize-spec').click();
      await f.locator('#spec-state').filter({ hasText: 'final' }).waitFor({ timeout: 10000 });
    });

    await test('拆解：手工添加任务（任务名、内容、完成定义、工时），提示「已拆解」', async () => {
      const f = specs();
      await f.locator('#show-task-split').click();
      await f.locator('#task-title').click(); await page.keyboard.type('P1 任务一');
      await f.locator('#task-content').click(); await page.keyboard.type('把 spec 的验收用例落成脚本');
      await f.locator('#task-dod').click(); await page.keyboard.type('脚本在真页面上绿');
      await f.locator('#task-hours').click(); await page.keyboard.type('2');
      await f.locator('#create-task').click();
      // 「已拆解」状态行会随详情重绘被清掉，轮询抓不稳；以任务板接口里真有这条任务为准，并核页面任务列表已重绘
      await waitFor(async () => {
        const sl = await jsonOf(await get('/api/console/specs'));
        const spec = (sl.specs || sl.items || []).find((x) => x.title === 'P1 真页面 spec');
        if (!spec) return false;
        const board = await jsonOf(await get(`/api/console/task-board?spec_id=${spec.id}`));
        return (board.tasks || []).length === 1;
      }, 30000, '任务板出现 1 条任务');
      await f.locator('#task-list').filter({ hasText: 'P1 任务一' }).waitFor({ state: 'attached', timeout: 30000 });
    });

    await test('任务管理页：点任务管理入口，iframe 里见该 spec 与它的任务', async () => {
      await page.click('a:has-text("任务管理")');
      const f = tasks();
      await f.locator('table:not(#task-table) tbody tr').filter({ hasText: 'P1 真页面 spec' }).first().click();
      await f.locator('#task-table tbody tr').filter({ hasText: '把 spec 的验收用例落成脚本' }).waitFor({ timeout: 10000 });
    });

    await test('指派候选：研发与验收下拉里恰是两个在线 Agent', async () => {
      const f = tasks();
      await f.locator('#edit-tasks').click();
      const opts = await f.locator('.assignee-select').first().evaluate((s) => [...s.options].map((o) => o.textContent).filter(Boolean).sort());
      assert.deepStrictEqual(opts, ['kimi', 'opus50ts'], `候选应为两个在线 Agent，实得 ${JSON.stringify(opts)}`);
    });

    await test('指派候选（反）：断开一个 Agent 后候选缩小为 kimi，重连后恢复两个', async () => {
      agents.opus.stopServe();
      await waitFor(async () => {
        const al = await jsonOf(await get('/api/console/agents'));
        return (al.agents || []).some((a) => a.session === 'opus50ts' && a.online === false);
      }, 20000, 'Hub 见 opus50ts 离线');
      assert.deepStrictEqual(await assigneeCandidates(), ['kimi'], '断开 opus50ts 后候选应只剩 kimi');
      agents.opus.startServe();
      await waitFor(() => agents.opus.online(), 20000, 'opus50ts 重新在线');
      await waitFor(async () => {
        const al = await jsonOf(await get('/api/console/agents'));
        return (al.agents || []).some((a) => a.session === 'opus50ts' && a.online === true);
      }, 20000, 'Hub 见 opus50ts 重新在线');
      assert.deepStrictEqual(await assigneeCandidates(), ['kimi', 'opus50ts'], '重连后候选应恢复为两个');
    });

    await test('指派：研发＝kimi、验收＝opus50ts，编辑完成后刷新仍在', async () => {
      const f = tasks();
      const sels = f.locator('.assignee-select');
      await sels.nth(0).selectOption({ label: 'kimi' });
      await sels.nth(1).selectOption({ label: 'opus50ts' });
      await f.locator('#finish-tasks').click();
      await page.reload();
      await page.click('a:has-text("任务管理")');
      await f.locator('table:not(#task-table) tbody tr').filter({ hasText: 'P1 真页面 spec' }).first().click();
      const row = f.locator('#task-table tbody tr').filter({ hasText: '把 spec 的验收用例落成脚本' });
      await row.waitFor({ timeout: 10000 });
      const text = await row.innerText();
      assert.ok(/kimi/.test(text) && /opus50ts/.test(text), `刷新后任务行应带研发 kimi 与验收 opus50ts：${text}`);
    });

    await test('任务结案：回 Specs 点「任务结案」', async () => {
      await page.click('a:has-text("Specs")');
      const f = specs();
      await f.locator('.spec-title').first().click();
      await f.locator('#close-task-split').click();
      // 结案后详情重绘、工作流状态行不保证可见；以接口里 task_state=closed 为准
      await waitFor(async () => {
        const sl = await jsonOf(await get('/api/console/specs'));
        return (sl.specs || sl.items || []).some((x) => x.title === 'P1 真页面 spec' && x.task_state === 'closed');
      }, 10000, 'spec task_state=closed');
    });

    await test('开工并签发令牌：对已指派的 spec 开工；人在会话里给研发 Agent 签 console-tasks 令牌', async () => {
      const sl = await jsonOf(await get('/api/console/specs'));
      const spec = ((sl.specs || sl.items || [])[0]) || {};
      assert.ok(spec.id, '取不到 spec');
      state.specID = spec.id; state.revision = spec.current_revision || 2;
      const start = await post(`/api/console/task-driver/specs/${state.specID}/start`, {
        repository_name: 'dingwei', branch_name: 'p1-page', schedule: '2026-10-03',
        task_endpoint: '/api/agent/console/task-driver', token_instructions: '由控制台签发 capability',
      });
      assert.strictEqual(start.status(), 201, `开工应 201，实得 ${start.status()} ${await start.text()}`);
      const cap = await post('/api/console/agents/Kimi.ad/capability', { page_id: 'console-tasks' });
      assert.strictEqual(cap.status(), 200, `签发应 200，实得 ${cap.status()}`);
      state.cap = (await jsonOf(cap)).capability;
      assert.ok(state.cap && state.cap.length >= 32, '令牌应非空');
      const board = await jsonOf(await get(`/api/console/task-board?spec_id=${state.specID}`));
      state.taskID = (((board.tasks || [])[0]) || {}).proposal_id || '';
      assert.ok(state.taskID, '任务板应有任务（proposal_id）');
    });

    await test('Agent 拉任务：新平台 Agent 凭「专属凭据＋能力令牌」拉到分给自己的任务', async () => {
      const r = await agents.kimi.http('GET', `/api/agent/console/task-driver/specs/${state.specID}/${state.revision}/tasks`, state.cap);
      assert.strictEqual(r.status, 200, `Agent 拉任务应 200，实得 ${r.status} ${r.body.slice(0, 120)}`);
      assert.ok(r.body.includes(state.taskID), `返回里应含分给 kimi 的任务 ${state.taskID}`);
    });

    await test('Agent 拉任务（反）：不带能力令牌被拒', async () => {
      const r = await agents.kimi.http('GET', `/api/agent/console/task-driver/specs/${state.specID}/${state.revision}/tasks`, '');
      assert.ok(r.status === 401 || r.status === 403, `无令牌应 401/403，实得 ${r.status}`);
    });

    const pullPath = () => `/api/agent/console/task-driver/specs/${state.specID}/${state.revision}/tasks`;
    const denied = (r) => r.status === 401 || r.status === 403;
    await test('Agent 拉任务（反）：别的 Agent（opus）的凭据配 kimi 的令牌被拒', async () => {
      const r = await agents.opus.http('GET', pullPath(), state.cap);
      assert.ok(denied(r), `令牌应绑定 Agent：opus 凭据＋kimi 令牌应 401/403，实得 ${r.status}`);
    });
    await test('Agent 拉任务（反）：错页面 id 的令牌被拒（console-specs:new 令牌拉任务）', async () => {
      const cap = await post('/api/console/agents/Kimi.ad/capability', { page_id: 'console-specs:new' });
      assert.strictEqual(cap.status(), 200, `签发 console-specs:new 应 200，实得 ${cap.status()}`);
      const r = await agents.kimi.http('GET', pullPath(), (await jsonOf(cap)).capability);
      assert.ok(denied(r), `令牌应绑定页面 id：错页面令牌应 401/403，实得 ${r.status}`);
    });
    await test('Agent 拉任务（反）：乱写的令牌被拒；对照：正确令牌仍能拉', async () => {
      const bad = await agents.kimi.http('GET', pullPath(), 'x'.repeat(64));
      assert.ok(denied(bad), `乱写令牌应 401/403，实得 ${bad.status}`);
      const good = await agents.kimi.http('GET', pullPath(), state.cap);
      assert.strictEqual(good.status, 200, `对照：正确令牌应仍 200，实得 ${good.status}（否则上面的拒绝可能只是令牌失效）`);
    });

    await test('Agent 拉任务（反）：伪造的 Agent 凭据配有效令牌被拒', async () => {
      const r = await agents.kimi.http('GET', pullPath(), state.cap, 'dwa_' + '0'.repeat(40));
      assert.ok(denied(r), `伪造凭据应 401/403，实得 ${r.status}`);
    });
    await test('Agent 拉任务（反）：console-tasks 令牌打 spec 新建口被拒（令牌页面方向的另一头）', async () => {
      const r = await agents.kimi.http('POST', '/api/agent/console/specs', state.cap);
      assert.ok(denied(r), `任务令牌不得用于 spec 新建口：应 401/403，实得 ${r.status}`);
    });
    await test('吊销（反）：属主吊销研发 Agent 后，它从候选消失，原凭据＋原令牌不再能拉', async () => {
      const rv = await post(`/api/console/agents/${state.kimiID}/revoke`, {});
      assert.strictEqual(rv.status(), 200, `吊销应 200，实得 ${rv.status()} ${await rv.text()}`);
      const r = agents.kimi.callOnce('GET', pullPath(), state.cap);
      assert.ok(denied(r), `吊销后原凭据＋原令牌应 401/403，实得 ${r.status}`);
      await waitFor(async () => {
        const al = await jsonOf(await get('/api/console/agents'));
        const k = (al.agents || []).find((a) => a.session === 'kimi');
        return !k || k.online === false || k.state !== 'active';
      }, 20000, 'kimi 不再在线／不再 active');
    });

    await test('页面无错：console 与 pageerror 两路全程为空', async () => {
      assert.deepStrictEqual(pageErrors, [], `pageerror: ${pageErrors.join(' | ')}`);
      assert.deepStrictEqual(consoleErrors, [], `console error: ${consoleErrors.join(' | ')}`);
    }, { standalone: true });

    void hub;
  } catch (e) {
    console.error(`CRASH: ${e && e.stack || e}`);
    process.exitCode = 2;
  } finally {
    try { if (browser) await browser.close(); } catch (_) { /* gone */ }
    await cleanup();
    log(`P1 spec/task real-hub browser: passed=${passed} failed=${failed} skipped=${skipped} mutation=${mutationArg || 'none'}`);
    if (mutationArg) log(`MUTATION-VERDICT: ${mutationArg} ${process.exitCode === 2 ? 'CRASHED' : failed > 0 ? 'KILLED' : 'SURVIVED'}${firstRed ? ` first-red=${firstRed}` : ''}`);
    if (process.exitCode !== 2) process.exitCode = failed > 0 ? 1 : 0;
    if (mutationArg && process.exitCode === 1) process.exitCode = 1;
  }
})();
