// S-R53-TEMPGROUP-CONSOLE-UI: real production Console HTML + local JSON stubs.
// The first temp-group response is held so the empty refresh must leave the
// existing nav DOM byte-for-byte unchanged. No live Hub or platform Agent data.
const assert = require('assert');
const http = require('http');
const path = require('path');
const {execFileSync} = require('child_process');
const {chromium} = require('playwright');

const ROOT = path.join(__dirname, '..');
const html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: ROOT, encoding: 'utf8',
  env: {...process.env, DW_CONSOLE_HOSTED_FIXTURE: '1', DW_CONSOLE_HOSTED_ACCESS_FIXTURE: '0', DW_CONSOLE_TEMP_GROUPS_FIXTURE: '1'},
});
assert(html.includes('<section id=tempGroups data-enabled="true" hidden>'), 'production page must start with the temp AG block hidden');
assert(html.includes('setInterval(refreshAgents,15000)'), 'existing Agent refresh cadence must be retained');
assert(!html.includes('setInterval(refreshTempGroups'), 'temp AG must not create another timer');

const listItem = {
  code: 'AG7', seq: 7, generation: 1, owner_lc: 'u_owner',
  created_by_display: 'Owner Alpha', external_agent_id: 'ex_1',
  external_full_name: 'alice_AG7.owner', external_short_name: 'alice',
  target_agent_id: 'agent_1', target_full_name: 'Helper.owner', target_online: true,
  created_at: '2026-11-06T00:00:00Z', last_used_at: '2026-11-06T01:02:03Z',
  expires_at: '2099-11-09T01:02:03Z', dissolved_at: '', dissolve_reason: '', can_dissolve: true,
};
const detail = {
  ...listItem,
  members: [
    {agent_id: 'ex_1', role: 'external', full_name: 'alice_AG7.owner', online: true, created_at: '2026-11-06T00:00:00Z'},
    {agent_id: 'agent_1', role: 'internal', full_name: 'Helper.owner', online: true, created_at: '2026-11-06T00:00:00Z'},
  ],
  last_use: {kind: 'accepted_send', at: '2026-11-06T01:02:03Z'},
};
let tempGroups = [];
let listReads = 0;
let dissolveStatus = 409;
let firstListStartedResolve;
let releaseFirstList;
let firstListFinishedResolve;
const firstListStarted = new Promise(resolve => { firstListStartedResolve = resolve; });
const firstListGate = new Promise(resolve => { releaseFirstList = resolve; });
const firstListFinished = new Promise(resolve => { firstListFinishedResolve = resolve; });
const dissolveBodies = [];
const reads = [];

function json(res, status, value) {
  res.writeHead(status, {'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store'});
  res.end(JSON.stringify(value));
}
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://fixture');
  const p = url.pathname;
  reads.push(`${req.method} ${p}`);
  if (p === '/console') {
    res.writeHead(200, {'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store'});
    res.end(html); return;
  }
  if (p === '/api/console/temp-groups' && req.method === 'GET') {
    listReads += 1;
    if (listReads === 1) {
      firstListStartedResolve();
      await firstListGate;
    }
    json(res, 200, {temp_groups: tempGroups});
    if (listReads === 1) firstListFinishedResolve();
    return;
  }
  if (p === '/api/console/temp-groups/AG7' && req.method === 'GET') {
    json(res, 200, detail); return;
  }
  if (p === '/api/console/temp-groups/AG7/dissolve' && req.method === 'POST') {
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    dissolveBodies.push(JSON.parse(Buffer.concat(chunks).toString('utf8')));
    if (dissolveStatus === 409) {
      json(res, 409, {error: 'conflict', message: '临时编组已被其他人解散，请刷新后重试'}); return;
    }
    res.writeHead(204, {'cache-control': 'no-store'}); res.end(); return;
  }
  const values = {
    '/api/console/platform': {role: 'owner', actions: [], items: [], feishu_bound: true},
    '/api/console/agent-groups': {groups: [], owned_agents: []},
    '/api/console/agents': {agents: []},
    '/api/console/enroll/applications': {applications: []},
  };
  json(res, values[p] ? 200 : 404, values[p] || {error: 'not_found'});
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch(process.env.PW_CHANNEL ? {headless: true, channel: process.env.PW_CHANNEL} : {headless: true});
    const page = await browser.newPage();
    const pageErrors = [];
    page.on('pageerror', error => pageErrors.push(error.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await firstListStarted;
    await page.waitForTimeout(100);
    const navBefore = await page.locator('nav').evaluate(node => node.innerHTML);
    assert.strictEqual(await page.locator('#tempGroups').getAttribute('hidden'), '', 'empty block starts hidden');
    releaseFirstList();
    await firstListFinished;
    await page.waitForTimeout(100);
    const navAfter = await page.locator('nav').evaluate(node => node.innerHTML);
    assert.strictEqual(navAfter, navBefore, 'empty temp AG response must leave nav DOM byte-for-byte unchanged');
    assert.strictEqual(await page.locator('#tempGroups').isVisible(), false, 'empty temp AG block remains hidden');

    tempGroups = [listItem];
    const readsBeforeCadence = listReads;
    await page.evaluate(() => refreshAgents());
    await page.waitForFunction(() => !document.getElementById('tempGroups').hidden && document.querySelectorAll('#tempGroupsList .temp-group-item').length === 1);
    assert.strictEqual(listReads, readsBeforeCadence + 1, 'existing refreshAgents cadence must refresh temp AG exactly once');
    const sidebarText = await page.locator('#tempGroups').innerText();
    for (const expected of ['与我相关的临时编组', 'AG7', 'alice_AG7.owner', '剩余', '有效']) {
      assert(sidebarText.includes(expected), `sidebar missing ${expected}: ${sidebarText}`);
    }

    await page.locator('#tempGroupsList .temp-group-item').click();
    await page.waitForFunction(() => document.querySelectorAll('#tempGroupMembers .temp-group-member').length === 2);
    assert.strictEqual(await page.locator('#tempGroupWorkspace').isVisible(), true, 'property section is visible with a related AG');
    assert.strictEqual(await page.locator('#tempGroupItems .temp-group-row').count(), 1, 'property table lists one AG');
    const detailText = await page.locator('#tempGroupDetail').innerText();
    for (const expected of ['alice_AG7.owner', 'Helper.owner', 'Owner Alpha', '创建时刻', '最后使用时刻', '预计解散时刻']) {
      assert(detailText.includes(expected), `detail missing ${expected}: ${detailText}`);
    }
    assert.strictEqual(await page.locator('#tempGroupCreatedAt').getAttribute('datetime'), detail.created_at);
    assert.strictEqual(await page.locator('#tempGroupLastUsedAt').getAttribute('datetime'), detail.last_used_at);
    assert.strictEqual(await page.locator('#tempGroupExpiresAt').getAttribute('datetime'), detail.expires_at);

    await page.evaluate(() => { window.confirm = () => false; });
    await page.locator('#tempGroupDissolve').click();
    await page.waitForTimeout(150);
    assert.strictEqual(dissolveBodies.length, 0, 'cancelled confirmation must not send dissolve');

    await page.evaluate(() => { window.confirm = () => true; });
    await page.locator('#tempGroupDissolve').click();
    await page.waitForFunction(() => document.getElementById('tempGroupFeedback').textContent.includes('其他人解散'));
    assert.strictEqual(dissolveBodies.length, 1, '409 sends one request');
    assert.deepStrictEqual(dissolveBodies[0], {expected_expires_at: detail.expires_at, reason: 'owner'});
    assert((await page.locator('#tempGroupFeedback').innerText()).includes('临时编组已被其他人解散，请刷新后重试'), '409 must show readable server detail');

    dissolveStatus = 204;
    tempGroups = [];
    const listReadsBeforeSuccess = listReads;
    await page.locator('#tempGroupDissolve').click();
    await page.waitForFunction(() => document.getElementById('tempGroups').hidden === true);
    assert.strictEqual(dissolveBodies.length, 2, 'successful confirmation sends one request');
    assert.deepStrictEqual(dissolveBodies[1], {expected_expires_at: detail.expires_at, reason: 'owner'});
    assert.strictEqual(listReads, listReadsBeforeSuccess + 1, 'successful dissolve refreshes the temp AG list once');
    assert.strictEqual(await page.locator('#tempGroupWorkspace').isVisible(), false, 'empty success result hides the property section');
    assert.deepStrictEqual(pageErrors, [], `unexpected browser errors: ${pageErrors.join('; ')}`);
    console.log('TEMP_GROUP_UI_BROWSER_OK empty_dom=byte_equal sidebar=true detail_members=2 cancel_requests=0 conflict_readable=true success_refresh=1 page_errors=0');
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
