// Vega-260923-D: 从生产 consolePageHTML 抽取真实页面，在系统 Chrome 中验证编组面板。
// MUTATION=<名> 删除相应守卫；判据：首个失败的用例必须是该变异的目标格（见 MUTATION_TARGET），级联出来的次红允许。
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
if (mutation === 'mutation-anchor-selftest') mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x'); // 自检:必须以退出码 3 报 MUTATION-ANCHOR-STALE
if (mutation === 'retry-reachability') { // 重试区被放进默认隐藏的详情块里 ⇒ 零编组加载失败时点不到「重试加载」（HTML 半截在 2026-09-24 重建页面时丢了，这里按现行结构恢复）
  mutate('<div id=groupRetry><button id=retryGroups type=button>重试加载</button><div id=groupRetryFeedback class=group-feedback></div></div></section><section id=groupAddPanel', '</section><section id=groupAddPanel');
  mutate('id=groupDetail class="group-block group-block-detail" hidden>', 'id=groupDetail class="group-block group-block-detail" hidden><div id=groupRetry><button id=retryGroups type=button>重试加载</button><div id=groupRetryFeedback class=group-feedback></div></div>');
}
if (mutation === 'retry-feedback') mutate("groupFeedback('groupRetryFeedback','已刷新 · '+data.groups.length+' 个编组');", '');
if (mutation === 'leak-delete') mutate('if(g.can_manage){var del=', 'if(true){var del=');
if (mutation === 'leak-remove') mutate('if(selected.can_manage&&!leader){var rm=', 'if(!leader){var rm=');
if (mutation === 'foreign-candidate') mutate("fill('groupJoinAgent',owned.filter(function(a){return !ids.has(a.id);}),", "fill('groupJoinAgent',owned.concat([{id:'foreign',session_name:'foreign-agent-2642',online:true}]).filter(function(a){return !ids.has(a.id);}),");
if (mutation === 'detach-remove-row') mutate('row.append(rm);}members.append(row);', 'members.append(rm);}members.append(row);');
if (mutation === 'ux-table-align') mutate('.group-table th,.group-table td{padding:7px;border-bottom:1px solid #ddd;text-align:left}', '.group-table th{padding:7px;border-bottom:1px solid #ddd;text-align:left}.group-table td{padding:7px 7px 7px 31px;border-bottom:1px solid #ddd;text-align:left}');
if (mutation === 'drop-group-row-stripe') mutate('.group-table tbody tr:nth-child(even){background:var(--row-alt)}', '');
if (mutation === 'ux-member-gap') mutate('gap:18px;max-width:520px', 'justify-content:space-between;max-width:none');
if (mutation === 'ux-block-colors') {
  mutate('.group-block-list{background:#f4f7fb}.group-block-detail{background:#f8f5ff}.group-block-add{background:#eef8f1}', '.group-block-list,.group-block-detail,.group-block-add{background:#f4f7fb}');
  mutate('html[data-theme="dark"] .group-block-list{background:#1b2530}html[data-theme="dark"] .group-block-detail{background:#252033}html[data-theme="dark"] .group-block-add{background:#182a22}', 'html[data-theme="dark"] .group-block-list,html[data-theme="dark"] .group-block-detail,html[data-theme="dark"] .group-block-add{background:#1b2530}');
}
if (mutation === 'ux-add-focus') mutate("document.getElementById('groupAddOpen').onclick=function(){setGroupPanelMode(groupPanelMode==='add'?'':'add');};", "document.getElementById('groupAddOpen').onclick=function(){var p=document.getElementById('groupAddPanel');p.hidden=!p.hidden;};");
if (mutation === 'ux-detail-exclusive') mutate("row.onclick=function(e){if(e.target.closest('button'))return;window.selectedGroupCode=g.code;renderGroups();setGroupPanelMode('detail');}", "row.onclick=function(e){if(e.target.closest('button'))return;window.selectedGroupCode=g.code;renderGroups();if(globalThis.__testExclusive)document.getElementById('groupDetail').hidden=false;else setGroupPanelMode('detail');}");
if (mutation && !['retry-feedback', 'leak-delete', 'leak-remove', 'foreign-candidate', 'detach-remove-row', 'ux-table-align', 'drop-group-row-stripe', 'ux-member-gap', 'ux-block-colors', 'ux-add-focus', 'ux-detail-exclusive', 'retry-reachability'].includes(mutation)) { console.error(`MUTATION-UNKNOWN: ${mutation} is not a mutation of this script`); process.exit(3); }

let groups = [];
let additionsClosed = false;
let failInitialGroupsLoad = true;
const agents = [
  { id: 'leader', key_id: 'key-leader', session_name: 'owner-leader-2642', online: true },
  { id: 'member', key_id: 'key-member', session_name: 'owner-member-2642', online: true },
];
let nextJoinFailure = '';
const membershipWrites = [];
function payload() {
  return { principal: 'owner', owned_agents: agents, groups: groups.map((g) => ({ ...g, owner_display_name: g.owner_display_name || '业主甲', additions_closed: additionsClosed, members: g.members.map((m) => ({ ...m })) })) };
}
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
const server = http.createServer(async (req, res) => {
  const pathName = req.url.split('?')[0];
  if (pathName === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (pathName === '/api/console/agents') return json(res, 200, { agents: [] });
  if (pathName === '/api/console/hosted/options') return json(res, 200, { nodes: [], combos: [], accounts: [], plans: [], quota: { remaining: {} } }); // G3 托管区的挂载请求;本件不测它,只让桩认得路由
  if (pathName === '/api/console/enroll/applications') return json(res, 200, { applications: [] }); // G3 新增面板的挂载请求;本件不测它,只让桩认得这条路由
  if (pathName === '/api/console/platform') return json(res, 200, { role: 'management', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'approved' }] });
  if (pathName === '/api/console/agent-groups' && req.method === 'GET') {
    if (failInitialGroupsLoad) { failInitialGroupsLoad = false; res.writeHead(503, { 'content-type': 'text/plain' }); return res.end('initial load failed'); }
    return json(res, 200, payload());
  }
  if (pathName === '/api/console/agent-groups' && req.method === 'POST') {
    const body = await read(req); const short = body.short_name;
    const collides = groups.find((g) => g.short_name === short);
    if (collides) { res.writeHead(409, { 'content-type': 'text/plain' }); return res.end(`团队名称不可用；建议名称：${short.slice(0, 7)}1、${short.slice(0, 7)}2`); }
    groups.push({ code: 'A'.repeat(32), short_name: short, creator_principal: 'owner', can_manage: true, owner_display_name: '业主甲', leader_key_id: 'key-leader', leader_session_name: 'owner-leader-2642', members: [{ agent_id: 'leader', key_id: 'key-leader', session_name: 'owner-leader-2642', online: true }] });
    return json(res, 201, groups[0]);
  }
  const memberMatch = pathName.match(/^\/api\/console\/agent-groups\/([^/]+)\/members(?:\/([^/]+))?$/);
  if (memberMatch && req.method === 'POST') {
    if (nextJoinFailure) { const text = nextJoinFailure; nextJoinFailure = ''; res.writeHead(409, { 'content-type': 'text/plain' }); return res.end(text); }
    if (additionsClosed) { res.writeHead(409, { 'content-type': 'text/plain' }); return res.end('该编组已关闭添加'); }
    const body = await read(req); membershipWrites.push({ code: decodeURIComponent(memberMatch[1]), agent_id: body.agent_id }); const a = agents.find((x) => x.id === body.agent_id);
    if (!a) { res.writeHead(404); return res.end(); }
    groups[0].members.push({ agent_id: a.id, key_id: a.key_id, session_name: a.session_name, online: a.online }); res.writeHead(204); return res.end();
  }
  if (memberMatch && req.method === 'DELETE') { groups[0].members = groups[0].members.filter((m) => m.agent_id !== decodeURIComponent(memberMatch[2])); res.writeHead(204); return res.end(); }
  if (pathName.endsWith('/additions') && req.method === 'POST') { additionsClosed = (await read(req)).closed; res.writeHead(204); return res.end(); }
  if (pathName.startsWith('/api/console/agent-groups/') && req.method === 'DELETE') { groups = []; res.writeHead(204); return res.end(); }
  if (pathName === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0; const failedNames = [];
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; failedNames.push(name); console.log(`FAIL ${name}: ${e.message}`); } }
// 每个变异的【目标格】（组长 2026-10-02 裁定：判据是「首红必须是目标那格」，之后级联出来的次红允许，不整理夹具）。
// 值是目标用例名的一段子串；首个失败的用例名必须含它，否则变异没有打在它该打的地方 ⇒ 退出码 5（MUTATION-NOT-ISOLATED，不算红）。
const MUTATION_TARGET = {
  'retry-feedback': 'retry success is visible',
  'retry-reachability': 'reachable retry control',
  'leak-delete': 'keeps cross-owner management controls absent',
  'leak-remove': 'keeps cross-owner management controls absent',
  'foreign-candidate': 'only own online short names are candidates',
  'detach-remove-row': 'keeps member action close to its Agent name',
  'ux-table-align': 'aligns table headers with row cells',
  'drop-group-row-stripe': 'keeps cross-owner management controls absent',
  'ux-member-gap': 'keeps member action close to its Agent name',
  'ux-block-colors': 'distinguishes all three functional blocks',
  'ux-add-focus': 'makes detail and add mutually exclusive',
  'ux-detail-exclusive': 'makes detail and add mutually exclusive',
};
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext({ permissions: ['clipboard-read', 'clipboard-write'] });
  const page = await context.newPage(); page.setDefaultTimeout(1800);
  const pageErrors = []; const consoleErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await test('zero-group initial load failure has a reachable retry control', async () => {
      await page.locator('#groupRetry').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupDetail').isHidden(), true);
      assert.strictEqual(await page.locator('#retryGroups').isVisible(), true);
      assert.strictEqual(await page.locator('#retryGroups').evaluate((button) => button.closest('#groupRetry') !== null), true);
      await page.locator('#retryGroups').click();
    });
    if (mutation === 'retry-reachability') {
      // Continue the rest of the independent cases after recording the one
      // intended failure; this bypass is test-fixture setup only.
      await page.locator('#groupDetail').evaluate((detail) => { detail.hidden = false; });
      await page.locator('#retryGroups').click();
    }
    await page.locator('#groupAddOpen').click();
    await page.locator('#groupLeader').waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.getElementById('groupLeader').disabled);
    await test('name feedback is near the name control and the obsolete pattern is absent', async () => {
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('pattern'), null);
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('maxlength'), '8');
      await page.locator('#createGroup').click();
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '请输入团队名称');
    });
    await test('the owner-confirmed eight-character limit is enforced by the native control', async () => {
      await page.locator('#groupShortName').click();
      await page.locator('#groupShortName').press('ControlOrMeta+A');
      await page.locator('#groupShortName').press('Backspace');
      await page.evaluate(async () => navigator.clipboard.writeText('超过八个字符的团队名称'));
      await page.locator('#groupShortName').press('ControlOrMeta+V');
      assert.strictEqual((await page.locator('#groupShortName').inputValue()).length, 8);
    });
    await test('create trims, lists leader first, and reports exact duplicate with suggestion', async () => {
      await page.locator('#groupShortName').click(); await page.locator('#groupShortName').press('ControlOrMeta+A'); await page.locator('#groupShortName').press('Backspace'); await page.locator('#groupShortName').pressSequentially('  团队1  '); await page.locator('#createGroup').click();
      await page.locator('#groupAddPanel').waitFor({ state: 'hidden' });
      await page.locator('#groupListItems').getByText('团队1').waitFor({ state: 'visible' });
      assert.match(await page.locator('#groupMembers').textContent(), /^leader（组长）$/);
      await page.locator('#groupAddOpen').click();
      await page.locator('#groupShortName').click(); await page.locator('#groupShortName').press('ControlOrMeta+A'); await page.locator('#groupShortName').press('Backspace'); await page.locator('#groupShortName').pressSequentially('团队1'); await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').filter({ hasText: '团队名称不可用' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#groupNameFeedback').textContent(), /团队名称不可用；建议名称：团队11、团队12/);
      await page.locator('#groupAddOpen').click();
    });
    await test('retry success is visible', async () => {
      await page.evaluate(() => refreshGroups('retry')); await page.locator('#groupRetryFeedback').getByText('已刷新 · 1 个编组').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupRetryFeedback').textContent(), '已刷新 · 1 个编组');
    });
    // 夹具复位(原「32 位编组码」用例末尾的两行;后续用例依赖它,故保留):
    groups[0].members = groups[0].members.filter((member) => member.agent_id !== 'member');
    await page.evaluate(() => refreshGroups('credential-reset'));
    await test('renders the owner table and keeps cross-owner management controls absent', async () => {
      groups.push({ code: 'C'.repeat(32), short_name: '他组', creator_principal: 'owner-b', owner_display_name: '业主乙', can_manage: false, leader_key_id: 'foreign-key', leader_session_name: 'foreign-leader-2642', members: [
        { agent_id: 'foreign-leader', key_id: 'foreign-key', session_name: 'foreign-leader-2642', online: false },
        { agent_id: 'member', key_id: 'key-member', session_name: 'owner-member-2642', online: true },
      ] });
      await page.evaluate(() => refreshGroups('cross-owner'));
      assert.deepStrictEqual(await page.locator('#groupListTable th').allTextContents(), ['组名', '组长', '业主', '操作']);
      assert.strictEqual(await page.locator('#groupListItems .group-delete').count(), 1, 'delete must exist only for an owned group');
      assert.deepStrictEqual(await page.locator('#groupListItems tr').allTextContents(), ['团队1leader业主甲删除', '他组leader业主乙']);
      for (const theme of ['light', 'dark']) {
        const colors = await page.evaluate((value) => { document.documentElement.dataset.theme=value; return Array.from(document.querySelectorAll('#groupListItems tr')).slice(0,2).map((row)=>getComputedStyle(row).backgroundColor); }, theme);
        assert.strictEqual(colors.length, 2, `Agent 编组 ${theme} 必须有相邻两行`);
        assert.notStrictEqual(colors[0], colors[1], `Agent 编组 ${theme} 相邻行必须有不同 computed background`);
      }
      await page.evaluate(() => { document.documentElement.dataset.theme='light'; });
      await page.locator('#groupListItems tr').filter({ hasText: '他组' }).click();
      const crossText = await page.locator('#groupMembers').textContent();
      const crossRemoveCount = await page.locator('#groupMembers .group-member-remove').count();
      const crossAddHidden = await page.locator('#groupMemberAdd').isHidden();
      await page.locator('#groupListItems tr').filter({ hasText: '团队1' }).click();
      assert.strictEqual(crossRemoveCount, 0, 'cross-owner members must not expose removal');
      assert.match(crossText, /^leader（组长）（offline）member$/);
      assert.strictEqual(crossAddHidden, true, 'cross-owner group must not expose member candidates');
    });
    await test('aligns table headers with row cells at runtime', async () => {
      await page.setViewportSize({ width: 1800, height: 900 });
      const edges = await page.evaluate(() => {
        const textLeft = (node) => { const range=document.createRange();range.selectNodeContents(node);return range.getBoundingClientRect().left; };
        return { header:textLeft(document.querySelector('#groupListTable th')), cell:textLeft(document.querySelector('#groupListItems tr td')) };
      });
      assert.ok(Math.abs(edges.header - edges.cell) <= 0.5, `table header/cell left edges differ: ${JSON.stringify(edges)}`);
    });
    await test('keeps member action close to its Agent name on a wide screen', async () => {
      groups[0].members.push({ agent_id: 'member', key_id: 'key-member', session_name: 'owner-member-2642', online: true });
      try {
        await page.evaluate(() => refreshGroups('ux-member-gap'));
        const row = page.locator('#groupMembers .group-member-row').filter({ hasText: 'member' });
        await row.hover();
        const gap = await row.evaluate((node) => {
          const name = node.querySelector('span').getBoundingClientRect(), remove = node.querySelector('.group-member-remove').getBoundingClientRect();
          return remove.left - name.right;
        });
        assert.ok(gap <= 80, `wide-screen member/remove gap=${gap}px exceeds 80px`);
      } finally {
        groups[0].members = groups[0].members.filter((member) => member.agent_id !== 'member');
        await page.evaluate(() => refreshGroups('ux-member-gap-reset'));
      }
    });
    await test('distinguishes all three functional blocks in light and dark themes', async () => {
      const selectors = ['.group-block-list', '.group-block-detail', '#groupMemberAdd'];
      const colors = async () => page.evaluate((items) => items.map((selector) => getComputedStyle(document.querySelector(selector)).backgroundColor), selectors);
      const light = await colors();
      assert.strictEqual(new Set(light).size, 3, `all three light-theme functional blocks must have distinct colors: ${JSON.stringify(light)}`);
      await page.locator('html').evaluate((html) => html.dataset.theme = 'dark');
      try {
        const dark = await colors();
        assert.strictEqual(new Set(dark).size, 3, `all three dark-theme functional blocks must have distinct colors: ${JSON.stringify(dark)}`);
        assert.notDeepStrictEqual(dark, light, 'dark theme must not reuse light block colors');
      } finally {
        await page.locator('html').evaluate((html) => html.dataset.theme = 'light');
      }
    });
    await test('keeps the list resident and makes detail and add mutually exclusive', async () => {
      const visible = async () => page.evaluate(() => ({
        list: !document.getElementById('groupListTable').closest('.group-block-list').hidden,
        detail: !document.getElementById('groupDetail').hidden,
        add: !document.getElementById('groupAddPanel').hidden,
      }));
      await page.evaluate(() => { globalThis.__testExclusive = true; });
      try {
        await page.locator('#groupListItems tr').first().click();
        assert.deepStrictEqual(await visible(), { list: true, detail: true, add: false }, '点组名后只能展开组详情');
        await page.locator('#groupAddOpen').click();
        assert.deepStrictEqual(await visible(), { list: true, detail: false, add: true }, '点添加后必须收起详情且只展开添加编组');
        await page.locator('#groupListItems tr').first().click();
        assert.deepStrictEqual(await visible(), { list: true, detail: true, add: false }, '从添加返回组详情时前一个展开项必须收起');
      } finally {
        await page.evaluate(() => { globalThis.__testExclusive = false; setGroupPanelMode('detail'); });
      }
    });
    await test('close rejects, open joins, offline error is visible, and removal confirms the real consequence', async () => {
      await page.locator('#groupListItems tr').first().click();
      await page.locator('#groupAdditionsToggle').click(); await page.getByRole('button', { name: '开放添加' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupAdditionsToggle').textContent(), '开放添加');
      await page.locator('#groupJoinAgent').selectOption('member'); await page.locator('#joinGroup').click(); await page.locator('#groupJoinFeedback').getByText('该编组已关闭添加').waitFor({ state: 'visible' });
      await page.locator('#groupAdditionsToggle').click(); await page.getByRole('button', { name: '关闭添加' }).waitFor({ state: 'visible' });
      await page.locator('#groupJoinAgent').selectOption('member');
      nextJoinFailure = '该 Agent 已下线'; await page.locator('#joinGroup').click(); await page.locator('#groupJoinFeedback').getByText('该 Agent 已下线').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupJoinFeedback').textContent(), '该 Agent 已下线');
      await page.locator('#joinGroup').click(); await page.locator('#groupMembers').getByText('member').waitFor({ state: 'visible' });
      page.once('dialog', async (dialog) => { assert.match(dialog.message(), /消息可达性会变/); await dialog.accept(); });
      const memberRow = page.locator('#groupMembers .group-member-row').filter({ hasText: 'member' });
      await memberRow.hover();
      assert.strictEqual(await memberRow.locator(':scope > .group-member-remove').count(), 1, 'removal must be a direct child of the hovered member row');
      const remove = memberRow.locator('.group-member-remove');
      assert.strictEqual(await remove.evaluate((button) => button.closest('.group-member-row') === button.parentElement), true);
      await remove.click();
      await page.waitForFunction(() => !document.getElementById('groupMembers').textContent.includes('member'));
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((text) => !text.includes('status of 409') && !text.includes('status of 503')), []);
    });
    await test('only own online short names are candidates', async () => {
      // Keep this assertion independent of the removal-flow test: a mutation
      // that correctly blocks removal must not make this later candidate check
      // fail merely because its fixture cleanup did not run.
      groups[0].members = groups[0].members.filter((member) => member.agent_id !== 'member');
      await page.evaluate(() => refreshGroups('candidate-reset'));
      const names = await page.locator('#groupJoinAgent option').allTextContents();
      assert.deepStrictEqual(names, ['member']);
      assert.strictEqual(await page.locator('#groupJoinAgent option').first().getAttribute('value'), 'member');
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`vega group browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  // 退出码约定同其它脚本：1＝变异被杀（有 FAIL 行）· 0＝存活 · 3＝MUTATION-ANCHOR-STALE（变异无效）· 2＝脚本崩。
  // 🔴 本脚本的变异判据是「【首个失败的用例】必须是该变异的目标格」（MUTATION_TARGET）；之后级联出来的次红允许（共享桩状态所致）。
  //    首红不是目标格（或该变异没登记目标格）⇒ 变异没打在它该打的地方，退出码 5（MUTATION-NOT-ISOLATED），不算红。
  if (mutation && fail >= 1) {
    const want = MUTATION_TARGET[mutation];
    if (!want || !failedNames[0].includes(want)) {
      console.error(`MUTATION-NOT-ISOLATED: mutation ${mutation}: first failing case is "${failedNames[0]}", expected one containing "${want}"`);
      process.exit(5);
    }
  }
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((err) => { console.error(err); process.exit(2); });
