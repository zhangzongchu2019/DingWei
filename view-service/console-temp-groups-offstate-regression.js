// S-R53-TEMPGROUP-OFFSTATE: render the real Hub handler with TempGroups=nil.
// A disabled endpoint must never be probed, including direct detail/action calls.
const assert = require('assert');
const http = require('http');
const path = require('path');
const {execFileSync} = require('child_process');
const {chromium} = require('playwright');

const html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: path.join(__dirname, '..'), encoding: 'utf8',
  env: {...process.env, DW_CONSOLE_HOSTED_FIXTURE: '0', DW_CONSOLE_TEMP_GROUPS_FIXTURE: '0'},
});
assert(html.includes('<section id=tempGroups data-enabled="false" hidden>'), 'Hub must render its disabled capability');
const tempGroupRequests = [];
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') {
    res.writeHead(200, {'content-type': 'text/html; charset=utf-8'});
    res.end(html); return;
  }
  if (url.pathname === '/favicon.ico') {res.writeHead(204); res.end(); return;}
  const values = {
    '/api/console/platform': {role: 'owner', actions: [], items: [], feishu_bound: true},
    '/api/console/agent-groups': {groups: [], owned_agents: []},
    '/api/console/agents': {agents: []},
    '/api/console/enroll/applications': {applications: []},
  };
  let status = values[url.pathname] ? 200 : 404;
  let value = values[url.pathname] || {error: 'not_found'};
  if (url.pathname.startsWith('/api/console/temp-groups')) {
    tempGroupRequests.push(`${req.method} ${url.pathname}`);
    status = 503; value = {error: '临时编组未启用'};
  }
  res.writeHead(status, {'content-type': 'application/json; charset=utf-8'});
  res.end(JSON.stringify(value));
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch(process.env.PW_CHANNEL ? {headless: true, channel: process.env.PW_CHANNEL} : {headless: true});
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => {if (message.type() === 'error') errors.push(message.text());});
    page.on('dialog', async dialog => {errors.push(`unexpected dialog: ${dialog.message()}`); await dialog.dismiss();});
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.waitForFunction(() => document.getElementById('agents-status').textContent === '没有自己的 Agent');
    await page.evaluate(async () => {
      await refreshAgents();
      await refreshAgents();
      openGroupWorkspace();
      await refreshTempGroups();
      await selectTempGroup('AG7');
      await dissolveTempGroup({code: 'AG7', expires_at: '2099-11-09T01:02:03Z'});
    });
    await page.waitForFunction(() => document.getElementById('groupEmptyCreate') && !document.getElementById('groupEmptyCreate').hidden);
    await page.waitForTimeout(200);
    assert.deepStrictEqual(tempGroupRequests, [], '关态必须零 /api/console/temp-groups 请求');
    assert.deepStrictEqual(errors, [], '关态必须零控制台错误');
    for (const id of ['tempGroups', 'tempGroupWorkspace', 'tempGroupDetail']) {
      assert.strictEqual(await page.locator(`#${id}`).isVisible(), false, `关态不得显示临时编组区块 ${id}`);
    }
    assert.strictEqual(await page.locator('#tempGroupFeedback').textContent(), '', '关态不得显示临时编组报错');
    console.log('PASS offstate: zero temp-group requests, zero console errors, no visible temporary-group UI');
  } finally {
    if (browser) await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => {console.error(error); process.exitCode = 1;});
