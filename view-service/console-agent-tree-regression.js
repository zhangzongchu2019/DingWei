// 左栏「编组-Agent 树」回归 —— 依据业主 2026-09-21 03:18:15Z 逐字确认的六条体验验收标准。
//
// 🔴 本件**先于产品落地**写成,按组长要求:产品未到之前**它应当全红**。
//    所有断言都只依赖**今天已经存在**的 /api/console/agent-groups 载荷字段:
//      groups[].code / short_name / can_manage / members[].{agent_id,key_id,session_name,owner_key}
//      owned_agents[].{id,session_name,online}
//    🔴 owner 的人类可读显示名与成员 online 两个**新字段由 coder56solzzc 定稿**,本件**不预设其名**;
//    定稿后再补断言,现在不猜。
//
// 判据(组长 2026-09-24 拟,每条配一个精确变异):
//   ① 有组时左栏出现该组节点,组名按短名显示            变异 tree-back-to-flat
//   ② 组成员挂在组节点下;**不属于我自有 Agent 的成员显示全名**  变异 member-short-name
//      🔵 今天的载荷里没有"跨业主"这个位,但"是不是我自有的"可由 owned_agents 判定 ——
//      判据落在**能从现有数据推出来的那个事实**上,不落在还没定名的字段上。
//   ③ 剔除后该 Agent 从树上消失                        变异 stale-tree-after-remove
//   ④ 两形态可切换,且**切换后已打开的 tab/pane 不被重建** 变异 mode-switch-rebuilds-panes
//      🔴 这条是 0.2.1.64 那条 P0 的教训:左栏**不拥有** tabs/panes。
//   ⑤ 同一 Agent 属多组 ⇒ 树中**重复出现**(业主已定)   变异 tree-dedupes-agent
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice('--mutate='.length) || '';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML end marker missing');
  let html = source.slice(contentStart, end);
  // 🔴 变异必须真的改到字节;锚点失配就当场报错,不静默空跑。
  //    产品未落地时,下面每一个变异都会**明确报错「锚点未命中」** —— 那是设计好的响声,不是故障。
  const mutateHTML = (needle, replacement) => {
    const before = html;
    html = html.split(needle).join(replacement);
    assert.notStrictEqual(html, before,
      `变异 ${MUTATION} 未改动任何字节 —— 锚点未命中(产品尚未落地,或实现方用了别的写法)`);
  };
  if (MUTATION === 'tree-back-to-flat') mutateHTML('agentNavMode', 'agentNavModeDisabled');
  if (MUTATION === 'member-short-name') mutateHTML('agentTreeMemberLabel', 'dwShortName');
  if (MUTATION === 'stale-tree-after-remove') mutateHTML('renderAgentNav()', 'void 0');
  // 🔴 锚点必须绑在【本次要实现的东西】上。第一版我用了 document.getElementById('a') ——
  //    那是页面里早就有的字符串,产品还没落地它就能命中,这个变异因此"看起来有效"。
  //    改绑到切换形态的函数名上:它只会随本次实现出现。
  if (MUTATION === 'mode-switch-rebuilds-panes') mutateHTML('agentNavSetMode(', 'tabs.clear()||agentNavSetMode(');
  if (MUTATION === 'tree-dedupes-agent') mutateHTML('agentTreeAllowDuplicates', 'false&&agentTreeAllowDuplicates');
  // 不倒退:扁平列表今天就按 online 置灰(b.disabled=!a.online);树里若忽略 members[].online 即为倒退
  if (MUTATION === 'tree-ignores-member-online') mutateHTML('agentTreeMemberDisabled', 'false&&agentTreeMemberDisabled');
  if (MUTATION && !['tree-back-to-flat', 'member-short-name', 'stale-tree-after-remove',
    'mode-switch-rebuilds-panes', 'tree-dedupes-agent', 'tree-ignores-member-online'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
}

const listen = (server) => new Promise((resolve, reject) => {
  server.once('error', reject);
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});

async function main() {
  const html = productionHTML();
  // 自有 Agent 两个;外来成员一个(owner_key 不同,且不在 owned_agents 里)
  const mine = [
    { id: 'agent-a', session_name: 'zzc-alpha-2642', online: true },
    { id: 'agent-b', session_name: 'zzc-beta-2642', online: true },
  ];
  // 字段名以 2026-09-24 冻结稿为准:groups[].owner_display_name(永不为空)、members[].online(所有成员都带)
  const foreign = { agent_id: 'agent-x', key_id: 'k-x', session_name: 'other-gamma-2642', owner_key: 'other-owner', online: false };
  let removed = false;
  const groupsPayload = () => {
    const teamMembers = [
      { agent_id: 'agent-a', key_id: 'k-a', session_name: 'zzc-alpha-2642', owner_key: 'zzc', online: true },
      ...(removed ? [] : [foreign]),
    ];
    return {
      principal: 'owner-a',
      owned_agents: mine,
      groups: [
        { code: 'g'.repeat(32), short_name: 'team01', owner_display_name: '业主甲', can_manage: true, additions_closed: false,
          leader_key_id: 'k-a', leader_session_name: 'zzc-alpha-2642', members: teamMembers },
        // ⑤ 同一 Agent(agent-a)同时属于第二个组 ⇒ 树中应当出现两次
        { code: 'h'.repeat(32), short_name: 'team02', owner_display_name: '未知业主', can_manage: true, additions_closed: false,
          leader_key_id: 'k-a', leader_session_name: 'zzc-alpha-2642',
          members: [{ agent_id: 'agent-a', key_id: 'k-a', session_name: 'zzc-alpha-2642', owner_key: 'zzc', online: true }] },
      ],
    };
  };
  const server = http.createServer((req, res) => {
    const json = (body) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)); };
    if (req.url === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
    if (req.url === '/fixture/remove-foreign' && req.method === 'POST') { removed = true; res.writeHead(204); res.end(); return; }
    if (req.url === '/api/console/agents') return json({ agents: [...mine.map((a) => ({ session: a.session_name, online: a.online })), { session: foreign.session_name, online: true }] });
    if (req.url === '/api/console/platform') return json({ role: 'management', actions: ['read', 'submit'], items: [] });
    if (String(req.url).startsWith('/api/console/agent-groups')) return json(groupsPayload());
    if (String(req.url).startsWith('/view/')) { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end('<!doctype html><title>v</title>'); return; }
    res.writeHead(404); res.end('not found');
  });
  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  let passed = 0;
  try {
    const page = await (await browser.newContext()).newPage();
    page.setDefaultTimeout(4000);
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'load' });

    // ① 组节点出现,组名按短名显示
    await page.waitForSelector('#a .agent-tree-group');
    const groupNodes = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-group')]
      .map((el) => ({ code: el.dataset.groupCode || '', label: (el.querySelector('.agent-tree-group-label') || {}).textContent || '' })));
    assert.strictEqual(groupNodes.length, 2, `左栏必须出现两个组节点,实测 ${groupNodes.length}`);
    assert.deepStrictEqual(groupNodes.map((g) => g.label).sort(), ['team01', 'team02'],
      `组节点必须按短名显示,实测 ${JSON.stringify(groupNodes.map((g) => g.label))}`);
    passed += 2;

    // ② 成员挂在组节点下;非自有成员显示全名,自有成员显示短名
    const memberShape = await page.evaluate(() => {
      const team01 = [...document.querySelectorAll('#a .agent-tree-group')].find((el) => (el.querySelector('.agent-tree-group-label') || {}).textContent === 'team01');
      const members = [...team01.querySelectorAll('.agent-tree-agent')];
      return members.map((el) => ({ session: el.dataset.session || '', label: el.textContent.trim(), nested: el.closest('.agent-tree-group') === team01 }));
    });
    assert.ok(memberShape.every((m) => m.nested), '成员节点必须嵌在组节点之内');
    const foreignNode = memberShape.find((m) => m.session === 'other-gamma-2642');
    const ownNode = memberShape.find((m) => m.session === 'zzc-alpha-2642');
    assert.ok(foreignNode, 'team01 下必须能看到外来成员');
    assert.strictEqual(foreignNode.label, 'other-gamma-2642',
      `不属于我自有 Agent 的成员必须显示全名,实测 ${JSON.stringify(foreignNode.label)}`);
    assert.strictEqual(ownNode.label, 'alpha',
      `我自己的 Agent 仍按短名显示,实测 ${JSON.stringify(ownNode.label)}`);
    passed += 4;

    // ⑤ 同一 Agent 属两组 ⇒ 树中重复出现(业主已定,不许去重)
    const dupCount = await page.evaluate(() => document.querySelectorAll('#a .agent-tree-agent[data-session="zzc-alpha-2642"]').length);
    assert.strictEqual(dupCount, 2, `同一 Agent 属两个组时必须在树中出现两次,实测 ${dupCount}`);
    passed += 1;

    // ⑥ 不倒退(parity,非业主六条之一 —— 🟡 组长若认为越界可删):
    //   扁平列表今天就按 online 置灰(renderAgentList 里 b.disabled=!a.online)。
    //   树形态若忽略 members[].online,等于比现有形态倒退一档 —— 故在此钉一条。
    //   字段名以 2026-09-24 冻结稿为准:members[].online,所有成员都带,包括跨业主成员。
    const onlineShape = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-agent')]
      .map((el) => ({ session: el.dataset.session || '', disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true' })));
    const foreignOnline = onlineShape.find((m) => m.session === 'other-gamma-2642');
    const ownOnline = onlineShape.find((m) => m.session === 'zzc-alpha-2642');
    assert.strictEqual(foreignOnline.disabled, true,
      'members[].online 为 false 的成员必须在树里表现为不可用(扁平列表今天就是这么做的,树不得倒退)');
    assert.strictEqual(ownOnline.disabled, false, 'online 为 true 的成员不得被置灰');
    passed += 2;
    // ④ 形态切换:两形态可切换,且已打开的 tab/pane 不被重建
    await page.evaluate(() => { const el = [...document.querySelectorAll('#a .agent-tree-agent')].find((x) => x.dataset.session === 'zzc-beta-2642'); if (el) el.click(); });
    await page.waitForSelector('#panes iframe');
    await page.evaluate(() => { document.querySelector('#panes iframe').dataset.identity = 'keep-me'; });
    await page.click('#agentNavMode');
    await page.waitForSelector('#a button');
    const flatShape = await page.evaluate(() => ({
      treeNodes: document.querySelectorAll('#a .agent-tree-group').length,
      flatButtons: document.querySelectorAll('#a button').length,
    }));
    assert.strictEqual(flatShape.treeNodes, 0, '切到纯 Agent 列表后不应再有组节点');
    assert.ok(flatShape.flatButtons > 0, '纯 Agent 列表形态必须列出 Agent');
    await page.click('#agentNavMode');
    await page.waitForSelector('#a .agent-tree-group');
    const paneKept = await page.evaluate(() => {
      const f = document.querySelector('#panes iframe');
      return { identity: f ? f.dataset.identity : null, src: f ? f.src : null, count: document.querySelectorAll('#panes iframe').length };
    });
    assert.strictEqual(paneKept.identity, 'keep-me',
      '切换左栏形态不得重建右侧 pane —— 左栏不拥有 tabs/panes(0.2.1.64 那条 P0 的教训)');
    assert.strictEqual(paneKept.count, 1, '切换形态不得多出或少掉 pane');
    passed += 4;

    // ③ 剔除后该 Agent 从树上消失
    await page.request.post(`http://127.0.0.1:${port}/fixture/remove-foreign`);
    await page.evaluate(() => window.refreshGroups('action'));
    await page.waitForFunction(() => document.querySelectorAll('#a .agent-tree-agent[data-session="other-gamma-2642"]').length === 0);
    passed += 1;

    assert.deepStrictEqual(pageErrors, [], `console 外壳报了顶层错误: ${JSON.stringify(pageErrors)}`);
    passed += 1;
    console.log(`console agent tree: passed=${passed} failed=0`);
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}
main().catch((error) => { console.error(error.message || error); process.exit(1); });
