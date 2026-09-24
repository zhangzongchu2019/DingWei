// Vega-260923-D: 从生产 consolePageHTML 抽取真实页面，在系统 Chrome 中验证编组面板。
// MUTATION=retry-feedback / can-manage 分别删除相应守卫；该模式必须只使对应断言失败。
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
if (mutation === 'retry-feedback') mutate("groupFeedback('groupRetryFeedback','已刷新 · '+data.groups.length+' 个编组');", '');
if (mutation === 'leak-delete') mutate('if(g.can_manage){var del=', 'if(true){var del=');
if (mutation === 'leak-remove') mutate('if(selected.can_manage&&!leader){var rm=', 'if(!leader){var rm=');
if (mutation === 'foreign-candidate') mutate("fill('groupJoinAgent',owned.filter(function(a){return !ids.has(a.id);}),", "fill('groupJoinAgent',owned.concat([{id:'foreign',session_name:'foreign-agent-2642',online:true}]).filter(function(a){return !ids.has(a.id);}),");
if (mutation === 'detach-remove-row') mutate('row.append(rm);}members.append(row);', 'members.append(rm);}members.append(row);');
if (mutation === 'ux-table-align') mutate('.group-table th,.group-table td{padding:7px;border-bottom:1px solid #ddd;text-align:left}', '.group-table th{padding:7px;border-bottom:1px solid #ddd;text-align:left}.group-table td{padding:7px 7px 7px 31px;border-bottom:1px solid #ddd;text-align:left}');
if (mutation === 'ux-member-gap') mutate('gap:18px;max-width:520px', 'justify-content:space-between;max-width:none');
if (mutation === 'ux-placeholder') mutate('placeholder="请输入完整的 32 位编组码"', 'placeholder=""');
if (mutation === 'ux-block-colors') {
  mutate('.group-block-list{background:#f4f7fb}.group-block-detail{background:#f8f5ff}.group-block-add{background:#eef8f1}.group-block-join{background:#fff7eb}', '.group-block-list,.group-block-detail,.group-block-add,.group-block-join{background:#fff}');
  mutate('html[data-theme="dark"] .group-block-list{background:#1b2530}html[data-theme="dark"] .group-block-detail{background:#252033}html[data-theme="dark"] .group-block-add{background:#182a22}html[data-theme="dark"] .group-block-join{background:#30251a}', 'html[data-theme="dark"] .group-block-list,html[data-theme="dark"] .group-block-detail,html[data-theme="dark"] .group-block-add,html[data-theme="dark"] .group-block-join{background:#111316}');
}
if (mutation === 'ux-add-focus') mutate('document.getElementById(\'groupAddOpen\').onclick=function(){setGroupAddMode(!groupAddMode);};', "document.getElementById('groupAddOpen').onclick=function(){var p=document.getElementById('groupAddPanel');p.hidden=!p.hidden;};");
if (mutation && !['retry-feedback', 'leak-delete', 'leak-remove', 'foreign-candidate', 'detach-remove-row', 'ux-table-align', 'ux-member-gap', 'ux-placeholder', 'ux-block-colors', 'ux-add-focus'].includes(mutation)) throw new Error(`unknown mutation ${mutation}`);

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

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
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
      assert.strictEqual(await page.locator('#groupJoinCode').inputValue(), '');
      await page.locator('#groupAddOpen').click();
    });
    await test('retry success is visible', async () => {
      await page.evaluate(() => refreshGroups('retry')); await page.locator('#groupRetryFeedback').getByText('已刷新 · 1 个编组').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupRetryFeedback').textContent(), '已刷新 · 1 个编组');
    });
    await test('keeps the 32-character code credential visible and submits the code the operator enters', async () => {
      const code = 'B'.repeat(32);
      assert.strictEqual(await page.locator('#groupJoinCode').inputValue(), '');
      await page.locator('#groupJoinCode').click(); await page.locator('#groupJoinCode').press('ControlOrMeta+A'); await page.locator('#groupJoinCode').pressSequentially(code);
      await page.locator('#groupCodeJoinAgent').selectOption('member'); await page.locator('#joinByCode').click();
      await page.waitForTimeout(20);
      assert.deepStrictEqual(membershipWrites.at(-1), { code, agent_id: 'member' });
      nextJoinFailure = '该 Agent 已在该编组中';
      await page.locator('#joinByCode').click();
      await page.locator('#groupCodeJoinFeedback').getByText('该 Agent 已在该编组中').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupCodeJoinFeedback').textContent(), '该 Agent 已在该编组中');
      groups[0].members = groups[0].members.filter((member) => member.agent_id !== 'member');
      await page.evaluate(() => refreshGroups('credential-reset'));
    });
    await test('renders the owner table and keeps cross-owner management controls absent', async () => {
      groups.push({ code: 'C'.repeat(32), short_name: '他组', creator_principal: 'owner-b', owner_display_name: '业主乙', can_manage: false, leader_key_id: 'foreign-key', leader_session_name: 'foreign-leader-2642', members: [
        { agent_id: 'foreign-leader', key_id: 'foreign-key', session_name: 'foreign-leader-2642', online: false },
        { agent_id: 'member', key_id: 'key-member', session_name: 'owner-member-2642', online: true },
      ] });
      await page.evaluate(() => refreshGroups('cross-owner'));
      assert.deepStrictEqual(await page.locator('#groupListTable th').allTextContents(), ['组名', '组长', '业主', '操作']);
      assert.strictEqual(await page.locator('#groupListItems .group-delete').count(), 1, 'delete must exist only for an owned group');
      assert.deepStrictEqual(await page.locator('#groupListItems tr').allTextContents(), ['团队1leader业主甲删除', '他组leader业主乙']);
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
    await test('labels the 32-character group code input with meaningful placeholder text', async () => {
      const placeholder = await page.locator('#groupJoinCode').getAttribute('placeholder');
      assert.ok(placeholder && placeholder.includes('32'), `group code placeholder=${JSON.stringify(placeholder)}`);
    });
    await test('distinguishes all four functional blocks in light and dark themes', async () => {
      const selectors = ['.group-block-list', '.group-block-detail', '#groupMemberAdd', '.group-block-join'];
      const colors = async () => page.evaluate((items) => items.map((selector) => getComputedStyle(document.querySelector(selector)).backgroundColor), selectors);
      const light = await colors();
      assert.ok(new Set(light).size > 1, `light block colors must differ: ${JSON.stringify(light)}`);
      await page.locator('html').evaluate((html) => html.dataset.theme = 'dark');
      try {
        const dark = await colors();
        assert.ok(new Set(dark).size > 1, `dark block colors must differ: ${JSON.stringify(dark)}`);
        assert.notDeepStrictEqual(dark, light, 'dark theme must not reuse light block colors');
      } finally {
        await page.locator('html').evaluate((html) => html.dataset.theme = 'light');
      }
    });
    await test('shows only list and add blocks while adding, then restores prior visibility', async () => {
      const ids = ['groupDetail', 'groupCodeJoin', 'groupRetry'];
      const visible = async () => page.evaluate((names) => Object.fromEntries(names.map((id) => [id, !document.getElementById(id).hidden])), ids);
      if (await page.locator('#groupAddPanel').isVisible()) await page.locator('#groupAddOpen').click();
      const before = await visible();
      await page.locator('#groupAddOpen').click();
      try {
        assert.deepStrictEqual(await visible(), { groupDetail: false, groupCodeJoin: false, groupRetry: false });
        assert.strictEqual(await page.locator('#groupAddPanel').isVisible(), true);
      } finally {
        await page.locator('#groupAddOpen').click();
      }
      assert.deepStrictEqual(await visible(), before, 'closing add mode must restore each block to its prior visibility');
    });
    await test('close rejects, open joins, offline error is visible, and removal confirms the real consequence', async () => {
      await page.locator('#groupAdditionsToggle').click(); await page.getByRole('button', { name: '开放添加' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#groupAdditionsToggle').textContent(), '开放添加');
      await page.locator('#groupJoinCode').click(); await page.locator('#groupJoinCode').press('ControlOrMeta+A'); await page.locator('#groupJoinCode').pressSequentially('A'.repeat(32));
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
  if (mutation) { if (fail !== 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((err) => { console.error(err); process.exit(2); });
