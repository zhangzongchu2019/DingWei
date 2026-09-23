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
if (mutation === 'retry-feedback') html = html.replace("groupFeedback('groupRetryFeedback','已刷新 · '+data.groups.length+' 个编组');", '');
if (mutation === 'retry-reachability') html = html.replace('detail.before(retryArea);', 'detail.append(retryArea);');
if (mutation === 'name-feedback') {
  const feedback = "groupFeedback('groupNameFeedback','团队名称不能超过 8 个字符')";
  const at = html.lastIndexOf(feedback);
  assert(at >= 0, 'cannot find final overlong-name feedback');
  html = html.slice(0, at) + '' + html.slice(at + feedback.length);
}
if (mutation === 'can-manage') {
  // The production page has an earlier compatibility definition and the final
  // definition that is actually used. Mutate only the final guard.
  const guard = 'if(!leader&&selected.can_manage)';
  const at = html.lastIndexOf(guard);
  assert(at >= 0, 'cannot find final can_manage guard');
  html = html.slice(0, at) + 'if(false)' + html.slice(at + guard.length);
}
if (mutation && !['retry-feedback', 'retry-reachability', 'name-feedback', 'can-manage'].includes(mutation)) throw new Error(`unknown mutation ${mutation}`);

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
  return { principal: 'owner', owned_agents: agents, groups: groups.map((g) => ({ ...g, additions_closed: additionsClosed, members: g.members.map((m) => ({ ...m })) })) };
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
    groups.push({ code: 'A'.repeat(32), short_name: short, creator_principal: 'owner', can_manage: true, leader_key_id: 'key-leader', leader_session_name: 'owner-leader-2642', members: [{ agent_id: 'leader', key_id: 'key-leader', session_name: 'owner-leader-2642' }] });
    return json(res, 201, groups[0]);
  }
  const memberMatch = pathName.match(/^\/api\/console\/agent-groups\/([^/]+)\/members(?:\/([^/]+))?$/);
  if (memberMatch && req.method === 'POST') {
    if (nextJoinFailure) { const text = nextJoinFailure; nextJoinFailure = ''; res.writeHead(409, { 'content-type': 'text/plain' }); return res.end(text); }
    if (additionsClosed) { res.writeHead(409, { 'content-type': 'text/plain' }); return res.end('该编组已关闭添加'); }
    const body = await read(req); membershipWrites.push({ code: decodeURIComponent(memberMatch[1]), agent_id: body.agent_id }); const a = agents.find((x) => x.id === body.agent_id);
    if (!a) { res.writeHead(404); return res.end(); }
    groups[0].members.push({ agent_id: a.id, key_id: a.key_id, session_name: a.session_name }); res.writeHead(204); return res.end();
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
    await page.locator('#groupLeader').waitFor({ state: 'visible' });
    await page.waitForFunction(() => !document.getElementById('groupLeader').disabled);
    await test('name feedback is near the name control and the obsolete pattern is absent', async () => {
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('pattern'), null);
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('maxlength'), null);
      await page.locator('#createGroup').click();
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '请输入团队名称');
    });
    await test('pasting more than eight characters remains visible and gets nearby feedback', async () => {
      await page.locator('#groupShortName').click();
      await page.locator('#groupShortName').press('ControlOrMeta+A');
      await page.locator('#groupShortName').press('Backspace');
      await page.evaluate(async () => navigator.clipboard.writeText('超过八个字符的团队名称'));
      await page.locator('#groupShortName').press('ControlOrMeta+V');
      assert.ok((await page.locator('#groupShortName').inputValue()).length > 8);
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').getByText('团队名称不能超过 8 个字符').waitFor({ state: 'visible' });
    });
    await test('create trims, lists leader first, and reports exact duplicate with suggestion', async () => {
      await page.locator('#groupShortName').click(); await page.locator('#groupShortName').press('ControlOrMeta+A'); await page.locator('#groupShortName').press('Backspace'); await page.locator('#groupShortName').pressSequentially('  团队1  '); await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').getByText('操作成功').waitFor({ state: 'visible' });
      await page.locator('#groupListItems').getByText('团队1').waitFor({ state: 'visible' });
      assert.match(await page.locator('#groupMembers').textContent(), /^leader（组长）$/);
      await page.locator('#groupShortName').click(); await page.locator('#groupShortName').press('ControlOrMeta+A'); await page.locator('#groupShortName').press('Backspace'); await page.locator('#groupShortName').pressSequentially('团队1'); await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').filter({ hasText: '团队名称不可用' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#groupNameFeedback').textContent(), /团队名称不可用；建议名称：团队11、团队12/);
      assert.strictEqual(await page.locator('#groupJoinCode').inputValue(), '');
      assert.strictEqual(await page.locator('#groupCodeValue').textContent(), 'A'.repeat(32));
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
      await page.locator('#groupMembers').getByRole('button', { name: 'member' }).click(); await page.locator('#groupRemoveAction button').click();
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
