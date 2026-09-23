// Console browser harness: serves the real consolePageHTML embedded in
// internal/m8/console_auth.go and replaces only its HTTP dependencies.
const assert = require('assert');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice(9) || '';

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML end marker missing');
  let html = source.slice(contentStart, end);
  if (MUTATION === 'stale-groups') html = html.replace('if(generation!==groupRequestGeneration)return null;', '');
  if (MUTATION === 'expose-group-code') html = html.replace("box.append(copy);", "box.append(copy);var leaked=document.createElement('code');leaked.className='group-code';leaked.textContent=group.code;box.append(leaked);");
  if (MUTATION === 'manual-agent-fields') html = html.replace("option.textContent=agent.session_name;option.dataset.keyId=agent.key_id||'';select.append(option)", "option.textContent=agent.id;option.dataset.keyId=agent.key_id||'';select.append(option)");
  if (MUTATION === 'legacy-specs-entry') html = html.replace("if(item.id==='specs') a.onclick=function(ev){ev.preventDefault();openSpecsWorkspace();};", "");
  if (MUTATION === 'silent-group-load') html = html.replace("groupStatus(message);if(source==='retry')groupFeedback('groupRetryFeedback',message);", "groupStatus('');if(source==='retry')groupFeedback('groupRetryFeedback','');");
  if (MUTATION === 'silent-platform-load') html = html.replace("status.textContent='工作台加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);", "status.textContent='';");
  if (MUTATION === 'silent-agent-load') html = html.replace("status.textContent='Agent 列表加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err);", "status.textContent='';");
  if (MUTATION === 'silent-offline-tab') html = html.replace("state.textContent=online?'':'离线，暂不可交互';", "state.textContent='';");
  if (MUTATION === 'disable-platform-tabs-on-agent-refresh') html = html.replace("if(String(session).startsWith('__')) return;", '');
  if (MUTATION === 'unhandled-group-action') html = html.replace("status.textContent='编组操作失败: '+String(err&&err.message||err);return false;", "throw err;");
  if (MUTATION === 'legacy-group-deadlock') {
    html = html.replace(/<section id=groupCodeJoin>.*?<\/section>/, '');
    html = html.replace('<section id=groupDetail hidden>', '<section id=groupDetail hidden><label>完整 32 字符组码 <input id=groupJoinCode maxlength=32></label>');
    html = html.replace('detail.before(retryArea);', 'detail.append(retryArea);');
  }
  if (MUTATION && !['stale-groups','expose-group-code','manual-agent-fields','legacy-group-entry','legacy-specs-entry','silent-group-load','silent-platform-load','silent-agent-load','silent-offline-tab','disable-platform-tabs-on-agent-refresh','unhandled-group-action','legacy-group-deadlock'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => resolve(server.address().port));
  });
}

async function main() {
  const html = consoleHTMLFromProductionSource();
  const agents = [
    { session: 'owner-alpha-2642', online: true },
    { session: 'owner-offline-2642', online: false },
  ];
  let raceGroupResponses = false;
  let raceGroupRequest = 0;
  let failGroupLoad = '';
  let failGroupAction = '';
  let slowGroupLoad = false;
  let emptyGroupInventory = false;
  let auditGroupMode = false;
  let auditGroup = null;
  let groupGETs = 0;
  let failPlatformLoad = '';
  let failAgentsLoad = '';
  let agentGETs = 0;
  const groupWrites = [];
  const groupJoinWrites = [];
  function groupPayload(closed) {
    return { principal: 'owner-a', owned_agents: [{ id: 'agent-a', key_id: 'fixture-key-a', session_name: 'owner-alpha-2642', online: true }, { id: 'agent-b', key_id: 'fixture-key-b', session_name: 'owner-beta-2642', online: true }], groups: [{
      code: 'team-001AAAAAAAAAAAAAAAAAAAAAAAA', short_name: 'team-001', creator_principal: 'owner-a', can_manage: true, additions_closed: closed,
      leader_key_id: 'fixture-key-a', leader_session_name: 'owner-alpha-2642',
      members: [{ agent_id: 'agent-a', key_id: 'fixture-key-a', session_name: 'owner-alpha-2642' }],
    }] };
  }
  const server = http.createServer((req, res) => {
    if (req.url === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      res.end(html);
      return;
    }
    if (req.url === '/api/console/agents') {
      agentGETs += 1;
      if (failAgentsLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture agents unavailable'); return; }
      if (failAgentsLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({ agents })); }, 200); return; }
      if (failAgentsLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"agents":[]}'); return; }
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ agents }));
      return;
    }
    if (req.url === '/api/console/platform') {
      if (failPlatformLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture platform unavailable'); return; }
      if (failPlatformLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({role:'management',actions:[],items:[]})); }, 200); return; }
      if (failPlatformLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"role":"management","actions":[],"items":[]}'); return; }
      const management = String(req.headers.cookie || '').includes('fixture-role=management');
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({
        role: management ? 'management' : 'owner',
        actions: management ? ['read', 'submit', 'add', 'edit', 'delete', 'approve'] : ['read', 'submit'],
        items: [
          { id: 'agents', kind: 'agents', title: 'Agents', href: '#agents', status: 'approved' },
          { id: 'specs', kind: 'spec', title: 'Specs', href: MUTATION === 'legacy-specs-entry' ? '/console/specs' : '#specs', ...(MUTATION === 'legacy-specs-entry' ? {target:'_blank'} : {}), status: 'approved' },
          ...(management ? [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: MUTATION === 'legacy-group-entry' ? '/admin/agent-groups' : '#agent-groups', ...(MUTATION === 'legacy-group-entry' ? {target:'_blank'} : {}), status: 'approved' }] : []),
        ],
      }));
      return;
    }
    if (req.url === '/api/console/agent-groups') {
      if (req.method === 'POST') {
        if (failGroupAction === 'network') { req.socket.destroy(); return; }
        if (failGroupAction === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture create denied'); return; }
        let raw=''; req.on('data',(c)=>{raw+=c}); req.on('end',()=>{const body=JSON.parse(raw);groupWrites.push({url:req.url,body});if(auditGroupMode&&auditGroup&&body.short_name===auditGroup.short_name){res.writeHead(409,{'Content-Type':'text/plain; charset=utf-8'});res.end(`group name ${body.short_name} is unavailable; try ${body.short_name}1`);return;}if(auditGroupMode){auditGroup={...groupPayload(false).groups[0],short_name:body.short_name,members:[groupPayload(false).groups[0].members[0]]};}res.writeHead(201,{'Content-Type':'application/json'});res.end(JSON.stringify(auditGroupMode?auditGroup:groupPayload(false).groups[0]));}); return;
      }
      groupGETs += 1;
      if (failGroupLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture groups unavailable'); return; }
      if (failGroupLoad === 'shape') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"owned_agents":"wrong","groups":[]}'); return; }
      if (failGroupLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify(groupPayload(false))); }, 200); return; }
      if (failGroupLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:[],groups:[]})); return; }
      if (auditGroupMode) { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:groupPayload(false).owned_agents,groups:auditGroup?[auditGroup]:[]})); return; }
      if (emptyGroupInventory) { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:[],groups:[]})); return; }
      const send = (closed) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(groupPayload(closed))); };
      if (slowGroupLoad) { slowGroupLoad=false; setTimeout(() => send(false), 120); return; }
      if (raceGroupResponses) {
        raceGroupRequest += 1;
        if (raceGroupRequest === 1) { setTimeout(() => send(false), 150); return; }
        send(true); return;
      }
      send(false);
      return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members' && req.method === 'POST') {
      let raw = '';
      req.on('data', (chunk) => { raw += chunk; });
      req.on('end', () => {
        const body = JSON.parse(raw); groupJoinWrites.push({ url: req.url, body });
        if (auditGroupMode && auditGroup && auditGroup.members.some((member) => member.agent_id === body.agent_id)) {
          res.writeHead(409, {'Content-Type':'text/plain; charset=utf-8'}); res.end('该 Agent 已在该编组中'); return;
        }
        if (auditGroupMode && auditGroup && auditGroup.additions_closed) {
          res.writeHead(409, {'Content-Type':'text/plain; charset=utf-8'}); res.end('该编组已关闭添加'); return;
        }
        if (auditGroupMode && auditGroup && !auditGroup.members.some((member) => member.agent_id === body.agent_id)) {
          auditGroup.members.push({agent_id:'agent-b',key_id:'fixture-key-b',session_name:'owner-beta-2642'});
        }
        res.writeHead(204);
        res.end();
      });
      return;
    }
    if (req.url === '/api/console/agent-groups/missing-1AAAAAAAAAAAAAAAAAAAAAAA/members' && req.method === 'POST' && auditGroupMode) {
      res.writeHead(404, {'Content-Type':'text/plain; charset=utf-8'}); res.end('404 page not found'); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members/agent-b' && req.method === 'DELETE' && auditGroupMode) {
      auditGroup.members = auditGroup.members.filter((member) => member.agent_id !== 'agent-b'); res.writeHead(204); res.end(); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/additions' && req.method === 'POST' && auditGroupMode) {
      let raw=''; req.on('data',(chunk)=>{raw+=chunk}); req.on('end',()=>{auditGroup.additions_closed=JSON.parse(raw).closed;res.writeHead(204);res.end();}); return;
    }
    if (req.url === '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA' && req.method === 'DELETE' && auditGroupMode) {
      auditGroup = null; res.writeHead(204); res.end(); return;
    }
    if (req.url === '/console/specs?embedded=1') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end('<!doctype html><title>DingWei Specs</title><h1>我的 spec</h1>'); return; }
    if (req.url && req.url.startsWith('/view/')) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end('<!doctype html><title>fixture agent view</title>');
      return;
    }
    if (req.url === '/favicon.ico') { res.writeHead(204); res.end(); return; }
    res.writeHead(404);
    res.end('not found');
  });

  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  let passed = 0;
  const failures = [];
    const pageErrors = [];
    const consoleErrors = [];
  async function test(name, fn) {
    try {
      await fn();
      passed += 1;
      console.log(`  PASS ${name}`);
    } catch (error) {
      failures.push(`${name}: ${error.message}`);
      console.log(`  FAIL ${name}: ${error.message}`);
    }
  }

  // Console refreshes the Agent snapshot every 15 seconds. Interactive-flow
  // checks must cross a real timer tick so platform tabs cannot accidentally
  // inherit the offline-Agent pointer lock after a fast test has already ended.
  async function waitPastAgentRefreshCycle(page) {
    await page.waitForTimeout(16000);
  }


  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    page.on('pageerror', (error) => pageErrors.push(error.message));
    page.on('console', (message) => { if (message.type() === 'error') consoleErrors.push(message.text()); });
    page.setDefaultTimeout(1500);
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: `http://127.0.0.1:${port}` });
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });

    await test('loads the real console document', async () => {
      assert.strictEqual(await page.title(), 'DingWei Agent Console');
      await page.locator('nav #a').waitFor({ state: 'attached' });
    });

    await test('keeps platform workspaces interactive after a full Agent refresh interval', async () => {
      const timerPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      timerPage.setDefaultTimeout(1500);
      await timerPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await timerPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      await timerPage.locator('#platform-items a[data-item-id="agent-groups"]').click();
      await timerPage.locator('.group-workspace').waitFor({state:'visible'});
      await timerPage.locator('#platform-items a[data-item-id="specs"]').click();
      const specsPane = timerPage.locator('#panes iframe[title="Specs workspace"]');
      await specsPane.waitFor({state:'visible'});
      await timerPage.locator('#tabs .tab[title="Agent 编组"]').click();

      const agentGETsBeforeTimer = agentGETs;
      await waitPastAgentRefreshCycle(timerPage);
      assert.ok(agentGETs > agentGETsBeforeTimer, 'the test must observe at least one real 15-second Agent refresh');

      const groupPane = timerPage.locator('#panes .group-workspace');
      assert.notStrictEqual(await groupPane.evaluate((node) => node.style.pointerEvents), 'none', 'Agent timer must not disable the group platform pane');
      assert.notStrictEqual(await specsPane.evaluate((node) => node.style.pointerEvents), 'none', 'Agent timer must not disable the Specs platform pane');
      const code = timerPage.locator('#groupJoinCode');
      await code.scrollIntoViewIfNeeded();
      const box = await code.boundingBox();
      assert.ok(box, 'group code input must have a visible box after timer tick');
      assert.strictEqual(await timerPage.evaluate(({x,y}) => document.elementFromPoint(x,y)?.id, {x:box.x+box.width/2,y:box.y+box.height/2}), 'groupJoinCode');
      await code.click();
      await timerPage.keyboard.type('team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      assert.strictEqual(await code.inputValue(), 'team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      await timerPage.close();
    });

    await test('distinguishes platform and Agent load failures from true empty states', async () => {
      failPlatformLoad = '500';
      failAgentsLoad = '500';
      const failedPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      failedPage.setDefaultTimeout(1500);
      const failedPageErrors = [], failedConsoleErrors = [], expectedFailureDiagnostics = [];
      failedPage.on('pageerror', (error) => failedPageErrors.push(error.message));
      failedPage.on('console', (message) => {
        if (message.type() !== 'error') return;
        if (message.text().startsWith('Failed to load resource: the server responded with a status of')) expectedFailureDiagnostics.push(message.text());
        else failedConsoleErrors.push(message.text());
      });
      await failedPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await failedPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      await failedPage.locator('#platform-status').filter({ hasText: '工作台加载失败: HTTP 500: fixture platform unavailable' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: 'Agent 列表加载失败: HTTP 500: fixture agents unavailable' }).waitFor({ state: 'visible' });
      assert.strictEqual(await failedPage.locator('#retryPlatform').isVisible(), true);
      assert.strictEqual(await failedPage.locator('#retryAgents').isVisible(), true);
      assert.notStrictEqual(await failedPage.locator('#agents-status').textContent(), '没有自己的 Agent');

      failPlatformLoad = 'timeout';
      failAgentsLoad = 'timeout';
      await failedPage.evaluate(() => { consoleFetchTimeoutMs = 100; });
      await failedPage.locator('#retryPlatform').click();
      await failedPage.locator('#retryAgents').click();
      assert.strictEqual(await failedPage.locator('#platform-status').textContent(), '工作台加载中…');
      assert.strictEqual(await failedPage.locator('#agents-status').textContent(), 'Agent 列表加载中…');
      await failedPage.locator('#platform-status').filter({ hasText: '工作台加载失败: 请求超时' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: 'Agent 列表加载失败: 请求超时' }).waitFor({ state: 'visible' });

      failPlatformLoad = 'empty';
      failAgentsLoad = 'empty';
      await failedPage.locator('#retryPlatform').click();
      await failedPage.locator('#retryAgents').click();
      await failedPage.locator('#platform-status').filter({ hasText: '没有可用工作台入口' }).waitFor({ state: 'visible' });
      await failedPage.locator('#agents-status').filter({ hasText: '没有自己的 Agent' }).waitFor({ state: 'visible' });
      assert.notStrictEqual(await failedPage.locator('#agents-status').textContent(), 'Agent 列表加载失败: 请求超时');

      failPlatformLoad = '';
      failAgentsLoad = '';
      await failedPage.locator('#retryPlatform').click();
      await failedPage.locator('#retryAgents').click();
      await failedPage.locator('#platform-items a[data-item-id="agent-groups"]').waitFor({ state: 'visible' });
      await failedPage.locator('#a button[title="owner-alpha-2642"]').waitFor({ state: 'visible' });
      assert.strictEqual(await failedPage.locator('#platform-status').textContent(), '');
      assert.strictEqual(await failedPage.locator('#agents-status').textContent(), '');
      assert.strictEqual(expectedFailureDiagnostics.length, 2);
      assert.deepStrictEqual(failedPageErrors, []);
      assert.deepStrictEqual(failedConsoleErrors, []);
      await failedPage.close();
    });
    if (MUTATION === 'silent-platform-load' || MUTATION === 'silent-agent-load') {
      console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
      if (failures.length) console.error(failures.join('\n'));
      process.exitCode = failures.length ? 1 : 0;
      return;
    }

    await test('renders online and offline agents from the console API', async () => {
      const online = page.locator('#a button[title="owner-alpha-2642"]');
      const offline = page.locator('#a button[title="owner-offline-2642"]');
      await online.waitFor({ state: 'visible' });
      await offline.waitFor({ state: 'attached' });
      assert.strictEqual(await online.isEnabled(), true);
      assert.strictEqual(await offline.isDisabled(), true);
    });

    await test('mounts platform items while retiring all five dead action buttons', async () => {
      const agentsItem = page.locator('#platform-items a[data-item-id="agents"]');
      await agentsItem.waitFor({ state: 'visible' });
      assert.strictEqual(await agentsItem.getAttribute('href'), '#agents');
      assert.strictEqual(await page.locator('#platform-actions button').count(), 0);
      assert.strictEqual(await page.locator('html').getAttribute('data-platform-role'), 'owner');
    });

    await test('keeps dead action buttons absent after the server role changes', async () => {
      await page.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await page.evaluate(() => refreshPlatform());
      assert.strictEqual(await page.locator('#platform-actions button').count(), 0);
      assert.strictEqual(await page.locator('html').getAttribute('data-platform-role'), 'management');
    });

    await test('lets a human start from zero groups without a hidden-control deadlock', async () => {
      emptyGroupInventory = true;
      const emptyPage = await browser.newPage({ viewport: { width: 1280, height: 800 } });
      emptyPage.setDefaultTimeout(1500);
      const emptyErrors = [];
      emptyPage.on('pageerror', (error) => emptyErrors.push(error.message));
      emptyPage.on('console', (message) => { if (message.type() === 'error') emptyErrors.push(message.text()); });
      await emptyPage.context().addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      await emptyPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      const entry = emptyPage.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      await entry.click();
      await emptyPage.locator('#groupCodeJoin').waitFor({ state: 'visible' });
      await emptyPage.locator('#group-status').filter({ hasText: '没有自己的 Agent，无法建组或加入' }).waitFor({ state: 'visible' });
      assert.strictEqual(await emptyPage.locator('#groupDetail').isHidden(), true);
      assert.strictEqual(await emptyPage.locator('#groupLeader').isDisabled(), true);
      assert.strictEqual(await emptyPage.locator('#groupJoinAgent').isDisabled(), true);
      assert.strictEqual(await emptyPage.locator('#groupCodeJoinAgent').isDisabled(), true);
      assert.strictEqual(await emptyPage.locator('#groupJoinCode').isEnabled(), true);
      async function humanHit(selector) {
        const target = emptyPage.locator(selector);
        const box = await target.boundingBox();
        assert.ok(box && box.width > 0 && box.height > 0, `${selector} has no visible hit box`);
        return emptyPage.evaluate(({ selector, x, y }) => {
          const wanted = document.querySelector(selector);
          const hit = document.elementFromPoint(x, y);
          return Boolean(wanted && hit && (hit === wanted || wanted.contains(hit)));
        }, { selector, x: box.x + box.width / 2, y: box.y + box.height / 2 });
      }
      assert.strictEqual(await humanHit('#groupJoinCode'), true);
      assert.strictEqual(await humanHit('#retryGroups'), true);
      await emptyPage.locator('#groupJoinCode').click();
      await emptyPage.keyboard.type('team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      assert.strictEqual(await emptyPage.locator('#groupJoinCode').inputValue(), 'team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      const getsBeforeRetry = groupGETs;
      const refreshed = emptyPage.waitForResponse((response) => response.url().endsWith('/api/console/agent-groups') && response.status() === 200);
      await emptyPage.locator('#retryGroups').click();
      await refreshed;
      assert.strictEqual(groupGETs, getsBeforeRetry + 1, 'visible retry must issue one groups GET');
      assert.strictEqual(await emptyPage.locator('#group-status').textContent(), '没有自己的 Agent，无法建组或加入');
      assert.deepStrictEqual(emptyErrors, []);
      await emptyPage.close();
      emptyGroupInventory = false;
    });
    if (MUTATION === 'legacy-group-deadlock') {
      console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
      if (failures.length) console.error(failures.join('\n'));
      process.exitCode = failures.length ? 1 : 0;
      return;
    }

    await test('runs the zero-group human workflow without hidden actions or long tasks', async () => {
      auditGroupMode = true;
      auditGroup = null;
      const auditContext = await browser.newContext({ viewport: { width: 1280, height: 800 } });
      await auditContext.addInitScript(() => {
        globalThis.__dwLongTasks = [];
        new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) if (entry.duration > 200) globalThis.__dwLongTasks.push(entry.duration);
        }).observe({ type: 'longtask', buffered: true });
      });
      await auditContext.addCookies([{ name: 'fixture-role', value: 'management', url: `http://127.0.0.1:${port}` }]);
      const auditPage = await auditContext.newPage();
      auditPage.setDefaultTimeout(1500);
      const auditErrors = [];
      const auditExpectedHTTPDiagnostics = [];
      auditPage.on('pageerror', (error) => auditErrors.push(error.message));
      auditPage.on('console', (message) => {
        if (message.type() !== 'error') return;
        if (message.text().startsWith('Failed to load resource: the server responded with a status of')) auditExpectedHTTPDiagnostics.push(message.text());
        else auditErrors.push(message.text());
      });
      await auditPage.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });
      async function humanHit(selector) {
        const target = auditPage.locator(selector);
        await target.scrollIntoViewIfNeeded();
        const box = await target.boundingBox();
        assert.ok(box && box.width > 0 && box.height > 0, `${selector} has no visible hit box`);
        return auditPage.evaluate(({ selector, x, y }) => {
          const wanted = document.querySelector(selector), hit = document.elementFromPoint(x, y);
          return Boolean(wanted && hit && (hit === wanted || wanted.contains(hit)));
        }, { selector, x: box.x + box.width / 2, y: box.y + box.height / 2 });
      }
      const entry = auditPage.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      await entry.click();
      await auditPage.waitForFunction(() => document.getElementById('groupLeader').disabled === false);

      await auditPage.locator('#groupShortName').click();
      await auditPage.keyboard.type('dingwei');
      await auditPage.locator('#groupLeader').selectOption('agent-a');
      assert.strictEqual(await humanHit('#createGroup'), true);
      await auditPage.locator('#createGroup').click();
      await auditPage.locator('#groupListItems').getByText('dingwei').waitFor({ state: 'visible' });

      // Failure paths are product outcomes and must remain visible without JS errors.
      assert.strictEqual(await humanHit('#createGroup'), true);
      await auditPage.locator('#createGroup').click();
      await auditPage.locator('#groupNameFeedback').filter({ hasText: 'group name dingwei is unavailable; try dingwei1' }).waitFor({ state: 'visible' });

      await auditPage.locator('#groupJoinCode').click();
      await auditPage.keyboard.type('short');
      assert.strictEqual(await humanHit('#joinByCode'), true);
      await auditPage.locator('#joinByCode').click();
      await auditPage.locator('#groupCodeJoinFeedback').filter({ hasText: '请输入完整 32 字符组码' }).waitFor({ state: 'visible' });

      async function keyboardReplace(selector, value) {
        await auditPage.locator(selector).click();
        await auditPage.keyboard.press(process.platform === 'darwin' ? 'Meta+A' : 'Control+A');
        await auditPage.keyboard.type(value);
      }
      await keyboardReplace('#groupJoinCode', 'missing-1AAAAAAAAAAAAAAAAAAAAAAA');
      await auditPage.locator('#groupCodeJoinAgent').selectOption('agent-b');
      assert.strictEqual(await humanHit('#joinByCode'), true);
      await auditPage.locator('#joinByCode').click();
      await auditPage.locator('#groupCodeJoinFeedback').filter({ hasText: '404 page not found' }).waitFor({ state: 'visible' });

      await keyboardReplace('#groupJoinCode', 'team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      await auditPage.locator('#groupCodeJoinAgent').selectOption('agent-a');
      await auditPage.locator('#joinByCode').click();
      await auditPage.locator('#groupCodeJoinFeedback').filter({ hasText: '该 Agent 已在该编组中' }).waitFor({ state: 'visible' });

      assert.strictEqual(await humanHit('#groupAdditionsToggle'), true);
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '开放添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupCodeJoinAgent').selectOption('agent-b');
      await auditPage.locator('#joinByCode').click();
      await auditPage.locator('#groupCodeJoinFeedback').filter({ hasText: '该编组已关闭添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '关闭添加' }).waitFor({ state: 'visible' });

      await auditPage.locator('#groupCodeJoinAgent').selectOption('agent-b');
      await auditPage.locator('#joinByCode').click();
      const member = auditPage.locator('#groupMembers button').filter({ hasText: 'beta' });
      await member.waitFor({ state: 'visible' });
      assert.strictEqual(await humanHit('#groupMembers div:nth-child(2) button'), true);
      await member.click();
      await auditPage.locator('#groupRemoveAction button').waitFor({ state: 'visible' });
      assert.strictEqual(await humanHit('#groupRemoveAction button'), true);
      auditPage.once('dialog', (dialog) => dialog.accept());
      await auditPage.locator('#groupRemoveAction button').click();
      await member.waitFor({ state: 'detached' });

      assert.strictEqual(await humanHit('#groupAdditionsToggle'), true);
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '开放添加' }).waitFor({ state: 'visible' });
      await auditPage.locator('#groupAdditionsToggle').click();
      await auditPage.locator('#groupAdditionsToggle').filter({ hasText: '关闭添加' }).waitFor({ state: 'visible' });

      const deleteGroup = auditPage.locator('#groupListItems button').filter({ hasText: '删组' });
      assert.strictEqual(await deleteGroup.count(), 1);
      auditPage.once('dialog', (dialog) => dialog.accept());
      await deleteGroup.click();
      await auditPage.locator('#groupEmptyCreate').waitFor({ state: 'visible' });
      assert.strictEqual(await auditPage.locator('#groupCodeJoin').isVisible(), true);
      assert.deepStrictEqual(await auditPage.evaluate(() => globalThis.__dwLongTasks), []);
      assert.strictEqual(auditExpectedHTTPDiagnostics.length, 4, 'four deliberate HTTP failures must stay narrowly classified');
      assert.deepStrictEqual(auditErrors, []);
      await auditContext.close();
      auditGroupMode = false;
      auditGroup = null;
      groupWrites.length = 0;
      groupJoinWrites.length = 0;
    });

    await test('opens Agent groups inside console with the owner-only online Agent selector', async () => {
      const entry = page.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      assert.strictEqual(await entry.getAttribute('href'), '#agent-groups');
      assert.strictEqual(await entry.getAttribute('target'), null);
      slowGroupLoad = true;
      await entry.click();
      await page.locator('#tabs .tab[title="Agent 编组"]').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('.group-workspace').count(), 1);
      assert.strictEqual(await page.locator('.group-workspace:visible').count(), 1);
      for (const id of ['groupLeader', 'groupJoinCode', 'groupCodeJoinAgent', 'joinByCode', 'groupListItems', 'groupDetail', 'retryGroups']) {
        assert.strictEqual(await page.locator(`#${id}`).count(), 1, `${id} must have one live DOM node`);
      }
      assert.strictEqual(await page.locator('#groupLeader').isDisabled(), true);
      assert.strictEqual(await page.locator('#groupLeader option').textContent(), '加载中…');
      assert.strictEqual(await page.locator('#group-status').textContent(), 'Agent 列表加载中…');
      await page.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
      await page.locator('#groupListItems').getByText('team-001').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await page.locator('#groupLeader option').allTextContents(), ['alpha', 'beta']);
      assert.deepStrictEqual(await page.locator('#groupJoinAgent option').allTextContents(), ['beta']);
      assert.strictEqual(await page.locator('#groupJoinCode').count(), 1);
      assert.strictEqual(await page.locator('#groupJoinCode').inputValue(), '');
      assert.strictEqual(await page.locator('#groupCodeValue').textContent(), 'team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      assert.deepStrictEqual(await page.locator('#groupCodeJoinAgent option').allTextContents(), ['alpha', 'beta']);
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('pattern'), null);
      assert.strictEqual(await page.locator('.group-workspace input[name="key_id"]').count(), 0);
      await page.locator('#copyGroupCode').click();
      assert.strictEqual(await page.evaluate(() => navigator.clipboard.readText()), 'team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      assert.strictEqual(await page.locator('#groupManageFeedback').textContent(), '组码已复制');
    });

    await test('shows a visible successful retry result', async () => {
      await page.locator('#retryGroups').click();
      await page.locator('#groupRetryFeedback').getByText('已刷新 · 1 个编组').waitFor({ state: 'visible' });
    });

    await test('ignores an older group snapshot that arrives after the latest response', async () => {
      raceGroupResponses = true;
      raceGroupRequest = 0;
      await Promise.all([
        page.evaluate(() => refreshGroups('race-first')),
        page.evaluate(() => refreshGroups('race-latest')),
      ]);
      await page.waitForTimeout(180);
      assert.strictEqual(await page.locator('#groupAdditionsToggle').textContent(), '开放添加');
      raceGroupResponses = false;
    });

    await test('keeps rejected and accepted create results visible beside the name control', async () => {
      await page.locator('#groupShortName').fill('');
      await page.locator('#createGroup').click();
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '请输入团队名称');
      assert.strictEqual(groupWrites.length, 0);
      await page.locator('#groupShortName').click();
      await page.locator('#groupShortName').pressSequentially('团队2');
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').getByText('操作成功').waitFor({ state: 'visible' });
      assert.deepStrictEqual(groupWrites[0].body, { agent_id: 'agent-a', short_name: '团队2' });
      assert.strictEqual('key_id' in groupWrites[0].body, false);
      assert.strictEqual('session_name' in groupWrites[0].body, false);
    });

    await test('joins through visible controls and leaves no uncaught page error', async () => {
      await page.locator('#groupJoinCode').pressSequentially('team-001AAAAAAAAAAAAAAAAAAAAAAAA');
      await page.locator('#groupCodeJoinAgent').selectOption('agent-b');
      const joined = page.waitForResponse((response) => response.url().endsWith('/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members') && response.status() === 204);
      await page.locator('#joinByCode').click();
      await joined;
      assert.deepStrictEqual(groupJoinWrites, [{
        url: '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members',
        body: { agent_id: 'agent-b' },
      }]);
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors, []);
    });

    await test('shows group load and network action failures beside their controls', async () => {
      failGroupLoad = '500';
      await page.locator('#retryGroups').click();
      await page.locator('#groupRetryFeedback').getByText('编组加载失败: HTTP 500').waitFor({state:'visible'});
      assert.strictEqual(await page.locator('#groupLeader').isDisabled(), true);
      assert.strictEqual(await page.locator('#groupLeader option').textContent(), '加载失败');
      failGroupLoad = 'shape'; await page.locator('#retryGroups').click(); await page.locator('#groupRetryFeedback').getByText('响应格式无效').waitFor({state:'visible'});
      failGroupLoad = 'timeout'; await page.evaluate(() => { groupFetchTimeoutMs=20; }); await page.locator('#retryGroups').click(); await page.locator('#groupRetryFeedback').getByText('请求超时').waitFor({state:'visible'});
      failGroupLoad = 'empty'; await page.locator('#retryGroups').click(); await page.locator('#group-status').getByText('没有自己的 Agent').waitFor({state:'visible'});
      assert.strictEqual(await page.locator('#groupLeader option').textContent(), '没有自己的 Agent');
      failGroupLoad = ''; await page.locator('#retryGroups').click(); await page.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
      assert.deepStrictEqual(await page.locator('#groupLeader option').allTextContents(), ['alpha', 'beta']);
      failGroupLoad = '';
      failGroupAction = '500';
      await page.locator('#groupShortName').fill('团队3');
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').filter({hasText:'fixture create denied'}).waitFor({state:'visible'});
      failGroupAction = 'network';
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').filter({hasText:'请求失败:'}).waitFor({state:'visible'});
      failGroupAction = '';
    });

    await test('opens a real console tab and agent iframe', async () => {
      await page.locator('#a button[title="owner-alpha-2642"]').click();
      const tab = page.locator('#tabs .tab[title="owner-alpha-2642"]');
      const pane = page.locator('#panes iframe[title="Agent view: owner-alpha-2642"]');
      await tab.waitFor({ state: 'visible' });
      assert.strictEqual(await tab.evaluate((node) => node.classList.contains('on')), true);
      assert.strictEqual(new URL(await pane.getAttribute('src'), page.url()).pathname, '/view/owner-alpha-2642');
    });

    await test('explains why an open Agent tab becomes non-interactive when offline', async () => {
      agents[0].online = false;
      await page.evaluate(() => refreshAgents());
      const tab = page.locator('#tabs .tab[title^="owner-alpha-2642"]');
      await tab.waitFor({ state: 'visible' });
      assert.strictEqual(await tab.locator('.tab-state').textContent(), '离线，暂不可交互');
      assert.strictEqual(await tab.getAttribute('aria-disabled'), 'true');
      assert.match(await tab.getAttribute('title'), /Agent 离线，暂不可交互/);
      assert.strictEqual(await page.locator('#panes iframe[title="Agent view: owner-alpha-2642"]').evaluate((node) => node.style.pointerEvents), 'none');
      assert.notStrictEqual(await page.locator('#panes .group-workspace').evaluate((node) => node.style.pointerEvents), 'none', 'Agent refresh must not disable the platform group workspace');
      agents[0].online = true;
      await page.evaluate(() => refreshAgents());
      assert.strictEqual(await page.locator('#tabs .tab[title="owner-alpha-2642"] .tab-state').isHidden(), true);
    });

    await test('opens Specs inside console without destroying live Agent panes', async () => {
      const paneCount = await page.locator('#panes iframe').count();
      const paneSrc = await page.locator('#panes iframe').first().getAttribute('src');
      await page.locator('#platform-items a[data-item-id="specs"]').click();
      const specsPane = page.locator('#panes iframe[title="Specs workspace"]');
      await specsPane.waitFor({state:'visible'});
      assert.strictEqual(new URL(await specsPane.getAttribute('src'), page.url()).pathname, '/console/specs');
      assert.strictEqual(await page.locator('#panes iframe').count(), paneCount+1);
      assert.strictEqual(await page.locator('#panes iframe').first().getAttribute('src'), paneSrc);
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`console browser harness: passed=${passed} failed=${failures.length}`);
  if (failures.length) {
    console.error(failures.join('\n'));
    process.exit(1);
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(2);
});
