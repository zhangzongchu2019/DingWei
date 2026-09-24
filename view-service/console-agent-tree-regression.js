// 左栏「编组-Agent 树」回归 —— 依据【左栏树的六条】体验验收标准
// (业主 2026-09-21T03:18:15Z 逐字确认)。
// 🔴 纪律二十:引用判据必须带限定词。队里同时存在两套,两个确认位,不是谁记错了:
//    【左栏树的六条】    业主 2026-09-21T03:18:15Z —— 就是本件依据的这一套
//    【0.2.1.66 的五条】 业主 2026-09-24T02:48:02Z —— 那次发版的北极星指标,不是本件的依据
//    ⇒ 本文件里凡说「六条」,一律指【左栏树的六条】;不得只说「已确认的那几条」。
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
//
// 🔴 以下两条【出身不同】,读格的人必须能一眼分清哪条是业主定的、哪条是我们自己加的:
//   ⑥ 【回归守卫 · 非业主验收判据】offline 成员在树里须表现为不可用
//      变异 tree-ignores-member-online
//      · 它挡的是**本次改动可能引入的倒退**:扁平列表**今天**就有 b.disabled=!a.online,
//        树若忽略 online,就是我们自己把现有形态降了一档。
//      · 🔴 它**不是**业主提出的要求 —— 【左栏树的六条】里没有这一条。
//      · 🔴 **若业主对树里 offline 另有说法,本条让位**,直接删;
//        **不得拿本条去反驳业主**。
//      · 🔴 让位条款同样适用于**实现扩面**:若为满足本条要把实现摊大、
//        或它与【左栏树的六条】打架 ⇒ **以业主判据为先,本条让位**。
//        删它的是本件的作者(opus50tc2k),**不由实现方绕过它** —— 绕过去就没人知道它让过位。
//   ⑦ 【占位守卫 · 待业主裁定】别人的 Agent 出现在我的左栏时,**点它不得打开会话**
//      变异 foreign-agent-can-open
//      · 由来:【左栏树的六条】② 要求"别人用组码加入 ⇒ 我的左栏出现那个 Agent",
//        而左栏既有语义是"点一下就打开它的会话"。**点别人的 Agent 会发生什么,那六条里没写。**
//        这是权限问题,不是体验问题,组长已交业主裁定。
//      · 🔴 在业主答复前:节点**照常渲染**(【左栏树的六条】② 要求它出现),但**不绑打开动作**。
//      · 🔴 业主口径到位后,本条**按业主说法重写或删除**,不得拿本条去反驳业主。
//   ⑧ 【回归守卫 · 客户端侧】树**只渲染响应里给了的组** —— 不自己拼、不猜、**不缓存上一次**
//      变异 tree-keeps-stale-groups
//      · 🔴 它**不是** visibility 的格。visibility 管"服务端给不给",归 Go 测试(coder56solzzc);
//        本条管"客户端会不会自己多画一个"。两件事,两条都要。
//      · 🔴 为什么不能在这一层测 visibility:这一层的响应**是本件自己拼的** ——
//        我不喂那个组、再断言它没出现,**不论服务端对不对都会绿**(组长 2026-09-24 纪律十五)。
//        **能伪造出我要排除的那种情况**,这一层才有判别力;伪造不了,格就得往下落一层。
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
  if (MUTATION === 'tree-back-to-flat') mutateHTML("agentNavMode='tree'", "agentNavMode='flat'");
  if (MUTATION === 'member-short-name') mutateHTML('b.textContent=agentTreeMemberLabel(member,ownedIDs);', 'b.textContent=dwShortName(member.session_name);');
  if (MUTATION === 'stale-tree-after-remove') mutateHTML('groupSnapshot=data;renderAgentNav();if(groupMode)', "groupSnapshot=data;if(source!=='action')renderAgentNav();if(groupMode)");
  // 🔴 锚点必须绑在【本次要实现的东西】上。第一版我用了 document.getElementById('a') ——
  //    那是页面里早就有的字符串,产品还没落地它就能命中,这个变异因此"看起来有效"。
  //    改绑到切换形态的函数名上:它只会随本次实现出现。
  if (MUTATION === 'mode-switch-rebuilds-panes') mutateHTML('function agentNavSetMode(mode){agentNavMode=mode;', "function agentNavSetMode(mode){document.getElementById('panes').replaceChildren();agentNavMode=mode;");
  if (MUTATION === 'tree-dedupes-agent') mutateHTML('agentTreeAllowDuplicates=true', 'agentTreeAllowDuplicates=false');
  // 不倒退:扁平列表今天就按 online 置灰(b.disabled=!a.online);树里若忽略 members[].online 即为倒退
  if (MUTATION === 'tree-ignores-member-online') mutateHTML('b.disabled=agentTreeMemberDisabled(member);', 'b.disabled=false;');
  // ⑦ 占位守卫:锚点绑在 agentTreeCanOpen( 上 —— 这个名字只有本次实现落地后才存在,落地前必然
  //    报「锚点未命中」。合同名在此钉死:实现方请用 agentTreeCanOpen(node) 决定要不要绑点击。
  if (MUTATION === 'foreign-agent-can-open') mutateHTML('if(agentTreeCanOpen(member,ownedIDs))', 'if(true)');
  // ⑧ 客户端不得自己留着上一轮的组。变异把 renderAgentNav 包一层,让它把历次见过的组累积起来 ——
  //    这正是"缓存上一次"最常见的写法。锚点 'function renderAgentNav(' 只有实现落地后才存在。
  if (MUTATION === 'tree-keeps-stale-groups') mutateHTML('groupSnapshot=data;renderAgentNav();if(groupMode)',
    'var __previousGroups=groupSnapshot.groups||[];groupSnapshot=data;'
    + '__previousGroups.forEach(function(old){if(!groupSnapshot.groups.some(function(now){return now.code===old.code;}))'
    + 'groupSnapshot.groups.push(Object.assign({},old,{members:[]}));});renderAgentNav();if(groupMode)');
  if (MUTATION === 'tree-shows-ungrouped') mutateHTML('section.append(title,members);nav.append(section);});\n}',
    "section.append(title,members);nav.append(section);});var b=document.createElement('button');b.dataset.session='zzc-beta-2642';nav.append(b);\n}");
  if (MUTATION && !['tree-back-to-flat', 'member-short-name', 'stale-tree-after-remove',
    'mode-switch-rebuilds-panes', 'tree-dedupes-agent', 'tree-ignores-member-online',
    'foreign-agent-can-open', 'tree-keeps-stale-groups', 'tree-shows-ungrouped'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
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
  let droppedTeam01 = false;   // ⑧ 用:让响应里不再给 team01
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
      ].filter((group) => !droppedTeam01 || group.short_name !== 'team01'),
    };
  };
  const server = http.createServer((req, res) => {
    const json = (body) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)); };
    if (req.url === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
    if (req.url === '/fixture/remove-foreign' && req.method === 'POST') { removed = true; res.writeHead(204); res.end(); return; }
    if (req.url === '/fixture/drop-team01' && req.method === 'POST') { droppedTeam01 = true; res.writeHead(204); res.end(); return; }
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
    assert.strictEqual(await page.locator('#a [data-session="zzc-beta-2642"]').count(), 0,
      '业主已明确裁定:未分组 Agent 在树形态不显示,只能切到纯 Agent 列表查看');
    // 🔴 上面那条钉的是【这个夹具里那一个】未分组 Agent(zzc-beta-2642)。
    //    它今天够用,但判据与夹具内容绑死:夹具里将来多一个未分组 Agent,它就覆盖不到。
    //    ⇒ 下面这条把规则本身写出来:**从载荷算出"未入任何显式组的自有 session"**,
    //      逐个断言它们在树形态下不出现。夹具怎么长都跟得上。
    const ungroupedSessions = await page.evaluate(() => {
      const snap = window.groupSnapshot || {};
      const grouped = new Set();
      (snap.groups || []).forEach((g) => (g.members || []).forEach((m) => grouped.add(m.agent_id)));
      return (snap.owned_agents || []).filter((a) => !grouped.has(a.id)).map((a) => a.session_name);
    });
    assert.ok(ungroupedSessions.length > 0,
      '🔴 前置:夹具里必须真的有未分组 Agent,否则这条断言不论产品对错都会绿');
    for (const session of ungroupedSessions) {
      assert.strictEqual(await page.locator(`#a [data-session="${session}"]`).count(), 0,
        `未入任何显式组的自有 Agent「${session}」不得出现在树形态(业主 2026-09-21 裁定)`);
    }
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

    // ⑥ 【回归守卫 · 非业主验收判据】(组长 2026-09-24 裁定:保留,但标签必须写清出身)
    //   挡的是:扁平列表今天就按 online 置灰(renderAgentList 里 b.disabled=!a.online),
    //   树形态若忽略 members[].online,就是**本次改动造成的倒退**。防自己造成的倒退属本任务范围。
    //   🔴 它不是【左栏树的六条】之一。**若业主对树里 offline 另有说法,本条让位**,不得拿它反驳业主。
    //   字段名以 2026-09-24 冻结稿为准:members[].online,所有成员都带,包括跨业主成员。
    const onlineShape = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-agent')]
      .map((el) => ({ session: el.dataset.session || '', disabled: el.disabled === true || el.getAttribute('aria-disabled') === 'true' })));
    const foreignOnline = onlineShape.find((m) => m.session === 'other-gamma-2642');
    const ownOnline = onlineShape.find((m) => m.session === 'zzc-alpha-2642');
    assert.strictEqual(foreignOnline.disabled, true,
      'members[].online 为 false 的成员必须在树里表现为不可用(扁平列表今天就是这么做的,树不得倒退)');
    assert.strictEqual(ownOnline.disabled, false, 'online 为 true 的成员不得被置灰');
    passed += 2;

    // ⑦ 【占位守卫 · 待业主裁定】别人的 Agent 点了不得打开会话。
    //   🔴 这条**不是**在规定业主该怎么裁,而是在业主裁定前**把门关上**:
    //   【左栏树的六条】② 只说"它要出现在我的左栏",没说"点它能进去"。在没答案之前默认**不给动作**,
    //   因为这是权限,开错了是单向的(会话一旦打开,内容已经看过了)。
    const panesBefore = await page.evaluate(() => document.getElementById('panes').children.length);
    const errorsBefore = pageErrors.length;
    await page.evaluate(() => {
      const el = [...document.querySelectorAll('#a .agent-tree-agent')].find((x) => x.dataset.session === 'other-gamma-2642');
      if (el) { el.disabled=false; el.removeAttribute('aria-disabled'); el.click(); }
    });
    await page.waitForTimeout(200);
    const panesAfter = await page.evaluate(() => document.getElementById('panes').children.length);
    assert.strictEqual(panesAfter, panesBefore,
      `点别人的 Agent 不得打开会话(业主未裁定前一律不开),实测 panes ${panesBefore} -> ${panesAfter}`);
    assert.strictEqual(pageErrors.length, errorsBefore,
      '点别人的 Agent 不得报错 —— 不绑动作 != 让它抛异常');
    passed += 2;
    // ④ 形态切换:两形态可切换,且已打开的 tab/pane 不被重建
    // 夹具修正:原稿点的是未出现在 groups[].members 的 beta,因此不可能命中树成员。
    // 改点载荷中真实存在的 alpha;本行只修夹具前置,不改变“切换不得重建 pane”的断言。
    await page.evaluate(() => { const el = [...document.querySelectorAll('#a .agent-tree-agent')].find((x) => x.dataset.session === 'zzc-alpha-2642'); if (el) el.click(); });
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

    // ⑧ 【回归守卫 · 客户端侧】树只渲染响应里给了的组:响应里不再给 team01 ⇒ 树上必须立刻没有它。
    //   🔴 这条的判别力来自"我能伪造出要排除的那种情况"——夹具真的把 team01 从响应里拿掉了。
    //   反过来,"服务端该不该给"这一层伪造不了(响应是本件自己拼的),那条归 Go 测试。
    const labelsBefore = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-group-label')].map((el) => el.textContent).sort());
    assert.deepStrictEqual(labelsBefore, ['team01', 'team02'], `前置:此刻树上应有两个组,实测 ${JSON.stringify(labelsBefore)}`);
    await page.request.post(`http://127.0.0.1:${port}/fixture/drop-team01`);
    await page.evaluate(() => window.refreshGroups('action'));
    // 夹具修正:drop-team01 只移除 team01,不得把 team02 一并清空。
    await page.waitForFunction(() => document.querySelectorAll('#a .agent-tree-group').length === 1);
    const labelsAfter = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-group-label')].map((el) => el.textContent).sort());
    assert.deepStrictEqual(labelsAfter, ['team02'],
      `响应里不再给的组必须从树上消失(不得缓存上一轮),实测 ${JSON.stringify(labelsAfter)}`);
    const orphanMembers = await page.evaluate(() => [...document.querySelectorAll('#a .agent-tree-agent')].filter((el) => !el.closest('.agent-tree-group')).length);
    assert.strictEqual(orphanMembers, 0, '组没了,它底下的成员节点也不得留在树上');
    passed += 3;

    assert.deepStrictEqual(pageErrors, [], `console 外壳报了顶层错误: ${JSON.stringify(pageErrors)}`);
    passed += 1;
    console.log(`console agent tree: passed=${passed} failed=0`);
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}
main().catch((error) => { console.error(error.stack || error.message || error); process.exit(1); });
