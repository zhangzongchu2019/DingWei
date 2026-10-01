// G3 · Agent 管理 Tab 新增两块(自备 Agent 审核 / Agent 列表)的浏览器闸。
// 派工 R-NEWPLAT-G3-tosonnet50ts-20261001084334 · 依据 runbooks/REVIEW-OWNER-UI-SKETCH-1-20261001.md 第五节业主答复
//
// 装置与 console-agent-groups-vega-regression.js 同源:console 页从生产源码实时抠,Hub 用本地桩。
// 🔴 Hub 接口路径为【暂定】(ENROLL_API),G3 设计件落板后只改页面里的 ENROLL_API 与本件桩路由两处。
// 判据口径:真键盘(click + keyboard.type,不用 fill);pageerror 与 console error 两路监听,末尾断言为空
//    (只放行桩有意返回的 4xx/5xx 资源加载行)。
//
// MUTATION=<name> 删除对应守卫;对应那一条断言必须红(见各变异的首红);后续用例共享桩状态,可能级联变红 ——
//    解读时按"首红是不是该条"判,不按红的总数判。
//    no-stale-guard · keep-inputs · no-confirm-revoke · roster-not-following · leak-nonmanage-group
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome。NODE_PATH 指向含 playwright 的 node_modules。
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
const MUTATIONS = ['no-stale-guard', 'keep-inputs', 'no-confirm-revoke', 'roster-not-following', 'leak-nonmanage-group'];
if (mutation && !MUTATIONS.includes(mutation)) throw new Error(`unknown mutation ${mutation}`);
if (mutation === 'no-stale-guard') mutate('if(generation!==enrollGeneration)return null;', '');
if (mutation === 'keep-inputs') mutate("if(!res.ok){enrollFeedback('enrollFeedback',res.text);return;}   // 申请保留", "if(!res.ok){enrollFeedback('enrollFeedback',res.text);enrollCloseEditor();return;}   // 申请保留");
if (mutation === 'no-confirm-revoke') mutate("if(!confirm('吊销 '+groupShortName(a.session_name)+'：该 Agent 将在 5 秒内断开,凭据作废;重装后需重新申请。确认吊销？'))return;", '');
if (mutation === 'roster-not-following') mutate('renderGroups();renderRoster();', 'renderGroups();');
if (mutation === 'leak-nonmanage-group') mutate('.filter(function(g){return g.can_manage;});', '.filter(function(){return true;});');

const GROUP_MINE = 'A'.repeat(32);
const GROUP_OTHER = 'B'.repeat(32);
const extraMembers = []; // 批准时选了编组 ⇒ Hub 把新 Agent 加进该组(桩复现这一语义)
let agents = [
  { id: 'ag-leader', key_id: 'key-leader', session_name: 'zzc-leader-2642', online: true },
  { id: 'ag-locked', key_id: 'key-locked', session_name: 'zzc-locked-2642', online: false },
];
const member = (a) => ({ agent_id: a.id, key_id: a.key_id, session_name: a.session_name, online: a.online });
function groupsPayload() {
  return {
    principal: 'owner', owned_agents: agents,
    groups: [
      { code: GROUP_MINE, short_name: '团队甲', creator_principal: 'owner', can_manage: true, owner_display_name: '业主甲', leader_key_id: 'key-leader', leader_session_name: 'zzc-leader-2642', additions_closed: false, members: [member(agents[0]), ...extraMembers.map(member)] },
      { code: GROUP_OTHER, short_name: '别人组', creator_principal: 'someone', can_manage: false, owner_display_name: '业主乙', leader_key_id: 'key-x', leader_session_name: 'zzc-x-2642', additions_closed: false, members: [] },
    ],
  };
}
let apps = [];
const PAIRING = { a1: '123456', a2: '654321', a3: '111111' };
function resetApps() {
  apps = [
    { id: 'a1', ip: '10.0.0.8', device_fingerprint: 'dev-AAAAAAAAAAAAAAAAAAAA', client_version: '0.2.1.95', requested_at: '2026-10-01T08:00:00Z' },
    { id: 'a2', ip: '10.0.0.9', device_fingerprint: 'dev-AAAAAAAAAAAAAAAAAAAA', client_version: '0.2.1.95', requested_at: '2026-10-01T08:05:00Z' },
    { id: 'a3', ip: '10.0.0.77', device_fingerprint: 'dev-CCCCCCCCCCCCCCCCCCCC', client_version: '0.2.1.90', requested_at: '2026-10-01T08:10:00Z' },
  ];
}
resetApps();
let failNextList = 0;
let nextListDelayMs = 0;
const writes = [];
function read(req) { return new Promise((resolve) => { let body = ''; req.on('data', (c) => { body += c; }); req.on('end', () => resolve(body ? JSON.parse(body) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function text(res, status, body) { res.writeHead(status, { 'content-type': 'text/plain' }); res.end(body); }
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/api/console/agents') return json(res, 200, { agents: agents.map((a) => ({ session: a.session_name, online: a.online })) });
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups' && req.method === 'GET') return json(res, 200, groupsPayload());
  if (p === '/api/console/enroll/applications' && req.method === 'GET') {
    if (failNextList > 0) { failNextList -= 1; return text(res, 503, 'enroll list unavailable'); }
    const snapshot = apps.map((a) => ({ ...a })); // 请求到达时刻的快照:慢应答会带着旧数据迟到
    const delay = nextListDelayMs; nextListDelayMs = 0;
    return setTimeout(() => json(res, 200, { applications: snapshot }), delay);
  }
  const m = p.match(/^\/api\/console\/enroll\/applications\/([^/]+)\/(approve|reject)$/);
  if (m && req.method === 'POST') {
    const id = decodeURIComponent(m[1]); const app = apps.find((a) => a.id === id);
    const body = await read(req); writes.push({ path: p, body });
    if (!app) return text(res, 404, '申请不存在');
    if (m[2] === 'reject') { apps = apps.filter((a) => a.id !== id); res.writeHead(204); return res.end(); }
    if (body.pairing_code !== PAIRING[id]) return text(res, 409, '配对码不符');
    if (agents.some((a) => a.session_name.split('-')[1].toLowerCase() === String(body.agent_name).toLowerCase())) return text(res, 409, `名称 ${body.agent_name} 与已有 Agent 重名`);
    const created = { id: `ag-${String(body.agent_name).toLowerCase()}`, key_id: `key-${body.agent_name}`, session_name: `zzc-${String(body.agent_name).toLowerCase()}-2642`, online: true };
    agents.push(created); if (body.group_code === GROUP_MINE) extraMembers.push(created);
    apps = apps.filter((a) => a.device_fingerprint !== app.device_fingerprint); // 同设备其余申请自动消失
    res.writeHead(204); return res.end();
  }
  const r = p.match(/^\/api\/console\/agents\/([^/]+)\/revoke$/);
  if (r && req.method === 'POST') {
    const id = decodeURIComponent(r[1]); writes.push({ path: p, body: null });
    if (id === 'ag-locked') return text(res, 409, '该 Agent 已吊销');
    agents = agents.filter((a) => a.id !== id); const at = extraMembers.findIndex((a) => a.id === id); if (at >= 0) extraMembers.splice(at, 1); res.writeHead(204); return res.end();
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
  const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.type(value); };
  const rows = () => page.locator('#enrollItems .enroll-row');
  const ids = () => page.locator('#enrollItems .enroll-row').evaluateAll((els) => els.map((e) => e.dataset.applicationId));
  try {
    failNextList = 1; // 首次加载故意失败:三态之「失败」
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();

    await test('failed first load shows the reason and a reachable retry; retry loads the rows', async () => {
      await page.locator('#enrollStatus').filter({ hasText: '自备 Agent 申请加载失败' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#enrollStatus').textContent(), /503/);
      assert.strictEqual(await page.locator('#enrollRetry').isVisible(), true);
      assert.strictEqual(await page.locator('#enrollEmpty').isHidden(), true, 'a failed load must not claim "no applications"');
      await page.locator('#enrollRetry').click();
      await rows().first().waitFor({ state: 'visible' });
      assert.deepStrictEqual(await ids(), ['a1', 'a2', 'a3']);
      assert.strictEqual(await page.locator('#enrollStatus').textContent(), '');
    });

    await test('each row shows IP, fingerprint (shortened), client version, request time — no CLI choice, no remaining time', async () => {
      const first = rows().first();
      const cells = await first.locator('td').allTextContents();
      assert.strictEqual(cells[0], '10.0.0.8');
      assert.strictEqual(cells[1], 'dev-AAAAAAAA');
      assert.strictEqual(cells[2], '0.2.1.95');
      assert.ok(cells[3].length > 6, 'request time rendered');
      const headers = (await page.locator('#enrollTable thead th').allTextContents()).join('|');
      assert.strictEqual(headers, 'IP 来源|设备指纹|客户端版本|申请时间|操作');
      assert.strictEqual(await page.locator('#enrollReview select').count(), 1, 'only the group select exists; no CLI select');
      assert.doesNotMatch(await page.locator('#enrollReview').textContent(), /剩余|CLI/);
    });

    await test('an older, slower list response never overwrites a newer one', async () => {
      nextListDelayMs = 700;
      await page.locator('#enrollRefresh').click();            // req1: 慢,带 3 条旧数据
      apps = apps.filter((a) => a.id !== 'a3');
      await page.locator('#enrollRefresh').click();            // req2: 快,2 条
      await page.waitForFunction(() => document.querySelectorAll('#enrollItems .enroll-row').length === 2);
      await page.waitForTimeout(1000);                         // 让 req1 迟到
      assert.deepStrictEqual(await ids(), ['a1', 'a2']);
      resetApps();
      await page.locator('#enrollRefresh').click();
      await page.waitForFunction(() => document.querySelectorAll('#enrollItems .enroll-row').length === 3);
    });

    await test('group dropdown offers only manageable groups', async () => {
      await rows().first().locator('.enroll-open').click();
      assert.deepStrictEqual(await page.locator('#enrollGroup option').allTextContents(), ['（不加入编组）', '团队甲']);
    });

    await test('empty required fields are hinted locally and nothing is sent', async () => {
      const before = writes.length;
      await page.locator('#enrollApprove').click();
      assert.match(await page.locator('#enrollFeedback').textContent(), /请填写/);
      assert.strictEqual(writes.length, before);
    });

    await test('wrong pairing code: Hub message shown, application kept, typed inputs kept, editor stays open', async () => {
      await type('#enrollCode', '000000'); await type('#enrollName', 'Alpha');
      await page.locator('#enrollApprove').click();
      await page.locator('#enrollFeedback').filter({ hasText: '配对码不符' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#enrollEditor').isVisible(), true);
      assert.strictEqual(await page.locator('#enrollCode').inputValue(), '000000');
      assert.strictEqual(await page.locator('#enrollName').inputValue(), 'Alpha');
      assert.deepStrictEqual(await ids(), ['a1', 'a2', 'a3']);
    });

    await test('duplicate name (case-insensitive, Hub-judged) is refused with the Hub text; no auto-rename', async () => {
      await page.locator('#enrollCode').click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type('123456');
      await page.locator('#enrollName').click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type('LEADER');
      await page.locator('#enrollApprove').click();
      await page.locator('#enrollFeedback').filter({ hasText: '重名' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#enrollName').inputValue(), 'LEADER', 'name left for the owner to change');
    });

    await test('valid approve: editor closes, row and same-device sibling vanish, new agent appears in roster and nav', async () => {
      await page.locator('#enrollName').click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type('Beta');
      await page.locator('#enrollGroup').selectOption(GROUP_MINE);
      await page.locator('#enrollApprove').click();
      await page.locator('#enrollEditor').waitFor({ state: 'hidden' });
      await page.waitForFunction(() => document.querySelectorAll('#enrollItems .enroll-row').length === 1);
      assert.deepStrictEqual(await ids(), ['a3'], 'a1 approved and a2 (same device) gone');
      const last = writes[writes.length - 1];
      assert.deepStrictEqual(last.body, { pairing_code: '123456', agent_name: 'Beta', group_code: GROUP_MINE });
      await page.locator('#rosterItems .roster-row').filter({ hasText: 'beta' }).waitFor({ state: 'visible' });
      await page.locator('#a button').filter({ hasText: 'beta' }).first().waitFor({ state: 'attached' });
    });

    await test('reject asks for confirmation; dismissing sends nothing, accepting removes the application', async () => {
      await rows().first().locator('.enroll-open').click();
      const before = writes.length; dialogAccept = false; dialogs.length = 0;
      await page.locator('#enrollReject').click();
      await page.waitForTimeout(250);
      assert.strictEqual(writes.length, before, 'dismissed confirm must not POST');
      assert.match(dialogs[0], /拒绝即踢走/);
      dialogAccept = true;
      await page.locator('#enrollReject').click();
      await page.locator('#enrollEmpty').waitFor({ state: 'visible' });
      assert.strictEqual(await rows().count(), 0);
    });

    await test('roster lists own agents; revoke is confirmed, dismiss sends nothing, Hub refusal is shown, success removes the row', async () => {
      assert.deepStrictEqual(await page.locator('#rosterItems .roster-row').evaluateAll((els) => els.map((e) => e.dataset.agentId)), ['ag-leader', 'ag-locked', 'ag-beta']);
      const before = writes.length; dialogAccept = false; dialogs.length = 0;
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-beta"] .roster-revoke').click();
      await page.waitForTimeout(250);
      assert.strictEqual(writes.length, before);
      assert.match(dialogs[0], /5 秒内断开/);
      dialogAccept = true;
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-locked"] .roster-revoke').click();
      await page.locator('#rosterFeedback').filter({ hasText: '已吊销' }).waitFor({ state: 'visible' });
      await page.locator('#rosterItems .roster-row[data-agent-id="ag-beta"] .roster-revoke').click();
      await page.waitForFunction(() => !document.querySelector('#rosterItems .roster-row[data-agent-id="ag-beta"]'));
      assert.strictEqual(writes[writes.length - 1].path, '/api/console/agents/ag-beta/revoke');
    });

    await test('the existing group panel is still there (list, create toggle, code-join)', async () => {
      assert.strictEqual(await page.locator('#groupListTable').count(), 1);
      assert.strictEqual(await page.locator('#groupAddOpen').count(), 1);
      assert.strictEqual(await page.locator('#groupCodeJoinOpen').count(), 1);
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (409|503)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`enroll review browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  if (mutation) { if (fail < 1) process.exit(1); } else if (fail) process.exit(1);
})().catch((err) => { console.error(err); process.exit(2); });
