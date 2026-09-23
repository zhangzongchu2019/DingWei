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
  if (MUTATION === 'silent-group-load') html = html.replace("groupStatus('编组加载失败: '+String(err&&err.name==='AbortError'?'请求超时':err&&err.message||err));", "groupStatus('');");
  if (MUTATION === 'unhandled-group-action') html = html.replace("status.textContent='编组操作失败: '+String(err&&err.message||err);return false;", "throw err;");
  if (MUTATION && !['stale-groups','expose-group-code','manual-agent-fields','legacy-group-entry','legacy-specs-entry','silent-group-load','unhandled-group-action'].includes(MUTATION)) throw new Error(`unknown mutation: ${MUTATION}`);
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
      res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
      res.end(JSON.stringify({ agents }));
      return;
    }
    if (req.url === '/api/console/platform') {
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
        let raw=''; req.on('data',(c)=>{raw+=c}); req.on('end',()=>{groupWrites.push({url:req.url,body:JSON.parse(raw)});res.writeHead(201,{'Content-Type':'application/json'});res.end(JSON.stringify(groupPayload(false).groups[0]));}); return;
      }
      if (failGroupLoad === '500') { res.writeHead(500, {'Content-Type':'text/plain'}); res.end('fixture groups unavailable'); return; }
      if (failGroupLoad === 'shape') { res.writeHead(200, {'Content-Type':'application/json'}); res.end('{"owned_agents":"wrong","groups":[]}'); return; }
      if (failGroupLoad === 'timeout') { setTimeout(() => { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify(groupPayload(false))); }, 200); return; }
      if (failGroupLoad === 'empty') { res.writeHead(200, {'Content-Type':'application/json'}); res.end(JSON.stringify({principal:'owner-a',owned_agents:[],groups:[]})); return; }
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
        groupJoinWrites.push({ url: req.url, body: JSON.parse(raw) });
        res.writeHead(204);
        res.end();
      });
      return;
    }
    if (req.url === '/console/specs?embedded=1') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end('<!doctype html><title>DingWei Specs</title><h1>我的 spec</h1>'); return; }
    if (req.url && req.url.startsWith('/view/')) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end('<!doctype html><title>fixture agent view</title>');
      return;
    }
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

  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    page.on('pageerror', (error) => pageErrors.push(error.message));
    page.setDefaultTimeout(1500);
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: `http://127.0.0.1:${port}` });
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded' });

    await test('loads the real console document', async () => {
      assert.strictEqual(await page.title(), 'DingWei Agent Console');
      await page.locator('nav #a').waitFor({ state: 'attached' });
    });

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

    await test('opens Agent groups inside console with the owner-only online Agent selector', async () => {
      const entry = page.locator('#platform-items a[data-item-id="agent-groups"]');
      await entry.waitFor({ state: 'visible' });
      assert.strictEqual(await entry.getAttribute('href'), '#agent-groups');
      assert.strictEqual(await entry.getAttribute('target'), null);
      await entry.click();
      await page.locator('#tabs .tab[title="Agent 编组"]').waitFor({ state: 'visible' });
      await page.waitForFunction(() => document.getElementById('groupLeader').disabled === false);
      await page.locator('#groupListItems').getByText('team-001').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await page.locator('#groupLeader option').allTextContents(), ['alpha', 'beta']);
      assert.deepStrictEqual(await page.locator('#groupJoinAgent option').allTextContents(), ['beta']);
      assert.strictEqual(await page.locator('#groupJoinCode').count(), 0);
      assert.strictEqual(await page.locator('#groupShortName').getAttribute('pattern'), null);
      assert.strictEqual(await page.locator('.group-workspace input[name="key_id"]').count(), 0);
    });

    await test('shows a visible successful retry result', async () => {
      await page.locator('#retryGroups').click();
      await page.locator('#groupRetryFeedback').getByText('已刷新 · 1 个编组').waitFor({ state: 'visible' });
    });

    await test('keeps rejected and accepted create results visible beside the name control', async () => {
      await page.locator('#groupShortName').fill('');
      await page.locator('#createGroup').click();
      assert.strictEqual(await page.locator('#groupNameFeedback').textContent(), '请输入团队名称');
      assert.strictEqual(groupWrites.length, 0);
      await page.locator('#groupShortName').fill('团队2');
      await page.locator('#createGroup').click();
      await page.locator('#groupNameFeedback').getByText('操作成功').waitFor({ state: 'visible' });
      assert.deepStrictEqual(groupWrites[0].body, { agent_id: 'agent-a', short_name: '团队2' });
      assert.strictEqual('key_id' in groupWrites[0].body, false);
      assert.strictEqual('session_name' in groupWrites[0].body, false);
    });

    await test('joins through visible controls and leaves no uncaught page error', async () => {
      await page.locator('#groupJoinAgent').selectOption('agent-b');
      const joined = page.waitForResponse((response) => response.url().endsWith('/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members') && response.status() === 204);
      await page.locator('#joinGroup').click();
      await joined;
      assert.deepStrictEqual(groupJoinWrites, [{
        url: '/api/console/agent-groups/team-001AAAAAAAAAAAAAAAAAAAAAAAA/members',
        body: { agent_id: 'agent-b' },
      }]);
      assert.deepStrictEqual(pageErrors, []);
    });

    await test('shows group load and network action failures beside their controls', async () => {
      failGroupLoad = '500';
      await page.locator('#retryGroups').click();
      await page.locator('#groupRetryFeedback').getByText('编组加载失败: HTTP 500').waitFor({state:'visible'});
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
