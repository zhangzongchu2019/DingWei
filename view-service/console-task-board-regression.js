const assertModule = require('assert');
let assertionCount = 0;
const assert = new Proxy(assertModule, {
  apply(target, thisArg, args) { assertionCount += 1; return Reflect.apply(target, thisArg, args); },
  get(target, prop, receiver) {
    const value = Reflect.get(target, prop, receiver);
    return typeof value === 'function' ? (...args) => { assertionCount += 1; return value.apply(target, args); } : value;
  },
});
const MIN_ASSERTIONS = 42;
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const mutation = (process.argv.find((arg) => arg.startsWith('--mutate=')) || '').slice(9);
const allowedMutations = new Set([
  '', 'k1-hide-task-panel', 'k1-drop-initial-load',
  'k2-drop-status-column', 'k2-drop-task-table',
  'k3-break-next-page', 'k3-drop-next-load',
  'k4-break-acceptance', 'k4-drop-acceptance-call',
  'k5-invent-placeholder', 'k5-drop-empty-normalizer',
  'k6-break-toggle', 'k6-drop-toggle-handler',
  'k9-no-persist', 'k9-swap-assignees',
  'k10-include-offline', 'k10-drop-online-options',
  'k11-clear-offline', 'k11-drop-offline-dot',
  'k-status-invent-value', 'k-status-drop-empty-call',
  'theme-drop-receiver',
]);
if (!allowedMutations.has(mutation)) throw new Error(`unknown mutation: ${mutation}`);

function rawGoString(file, name) {
  const source = fs.readFileSync(file, 'utf8');
  const start = `const ${name} = \``;
  const from = source.indexOf(start);
  if (from < 0) throw new Error(`${name} not found`);
  const bodyStart = from + start.length;
  const end = source.indexOf('`\n', bodyStart);
  if (end < 0) throw new Error(`${name} end not found`);
  return source.slice(bodyStart, end);
}

const root = path.join(__dirname, '..');
let shell = rawGoString(path.join(root, 'internal/m8/console_auth.go'), 'consolePageHTML')
  .replace('{{CONSOLE_VERSION}}', 'test').replace('{{CONSOLE_COMMIT}}', 'test').replace('{{CONSOLE_VERSION_TEXT}}', 'test');
let board = rawGoString(path.join(root, 'internal/m8/console_task_board_page.go'), 'consoleTaskBoardPageHTML');

const boardMutations = new Map([
  ['k1-hide-task-panel', ['<section id=task-panel class=panel>', '<section id=task-panel class=panel hidden>']],
  ['k1-drop-initial-load', ['\nloadBoard();\n</script>', '\n</script>']],
  ['k2-drop-status-column', ['<th>状态</th><th>验收标准</th>', '<th>验收标准</th>']],
  ['k2-drop-task-table', ['<table id=task-table>', '<div id=task-table>']],
  ['k3-break-next-page', ['specPage++;loadBoard()', 'specPage+=0;loadBoard()']],
  ['k3-drop-next-load', ['specPage++;loadBoard()', 'specPage++']],
  ['k4-break-acceptance', ['if(id)ids.push(id)', 'if(false)ids.push(id)']],
  ['k4-drop-acceptance-call', ['acceptance=acceptanceDisplay(task.acceptance_json)', "acceptance={label:'',title:''}"]],
  ['k5-invent-placeholder', ["td.textContent=task[nameKey]||''", "td.textContent=task[nameKey]||'未知人员'"]],
  ['k5-drop-empty-normalizer', ["td.textContent=task[nameKey]||''", "td.textContent=task[nameKey]||'undefined'"]],
  ['k6-break-toggle', ["selectedSpec=selectedSpec===spec.id?'':spec.id", 'selectedSpec=spec.id']],
  ['k6-drop-toggle-handler', ["tr.onclick=function(){selectedSpec=selectedSpec===spec.id?'':spec.id;taskPage=1;loadBoard()}", '']],
  ['k11-drop-offline-dot', ['td.append(dot)', '']],
  ['k-status-invent-value', ["taskCell(String(task.passed_cases||0)+'/'+String(task.total_cases||1))", "taskCell('0/0')"]],
  ['k-status-drop-empty-call', ["taskCell(String(task.passed_cases||0)+'/'+String(task.total_cases||1))", "taskCell('')"]],
  ['theme-drop-receiver', ['dwTaskInitTheme();', '']],
]);
for (const [name, [needle]] of boardMutations) assert.ok(board.includes(needle), `stale mutation probe ${name}`);
if (boardMutations.has(mutation)) {
  const [needle, replacement] = boardMutations.get(mutation);
  board = board.replace(needle, replacement);
}

const specs = Array.from({ length: 9 }, (_, i) => ({ id: `spec-${i + 1}`, title: `Spec ${i + 1}`, state: 'final' }));
const tasks = Array.from({ length: 21 }, (_, i) => ({
  proposal_id: `task-${i + 1}`, title: `Task ${i + 1}`, content: `Task text ${i + 1}`,
  acceptance_json: JSON.stringify([{ id: `A${i + 1}`, text: `Acceptance ${i + 1}` }]), status: 'planned', review_id: `manual:task-${i + 1}`,
}));
const agents = [
  { id: 'agent-dev', session_name: 'developer-session' },
  { id: 'agent-review', session_name: 'reviewer-session' },
  { id: 'agent-offline', session_name: 'offline-session' },
];
let developerOnline = true;

async function requestBody(req) {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  return chunks.length ? JSON.parse(Buffer.concat(chunks)) : {};
}

function sendJSON(res, value) {
  res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(value));
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end(shell); return; }
  if (url.pathname === '/console/tasks') { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end(board); return; }
  if (url.pathname === '/api/console/platform') {
    sendJSON(res, { role: 'owner', actions: ['read'], items: [{ id: 'task-board', kind: 'task-board', title: '任务管理', href: '#task-board', status: 'approved' }] }); return;
  }
  if (url.pathname === '/api/console/agents') { sendJSON(res, { agents: [] }); return; }
  if (url.pathname === '/api/console/agent-groups') { sendJSON(res, { owned_agents: [], groups: [] }); return; }
  if (url.pathname === '/fixture/developer-offline' && req.method === 'POST') { developerOnline = false; res.writeHead(204); res.end(); return; }
  const assignment = url.pathname.match(/^\/api\/console\/task-board\/tasks\/(task-\d+)$/);
  if (assignment && req.method === 'PATCH') {
    const body = await requestBody(req), task = tasks.find((item) => item.proposal_id === assignment[1]);
    if (!task) { res.writeHead(404); res.end('not found'); return; }
    if (mutation !== 'k9-no-persist') {
      if (mutation === 'k9-swap-assignees') {
        task.developer_agent_id = body.reviewer_agent_id || '';
        task.reviewer_agent_id = body.developer_agent_id || '';
      } else {
        task.developer_agent_id = body.developer_agent_id || '';
        task.reviewer_agent_id = body.reviewer_agent_id || '';
      }
    }
    res.writeHead(204); res.end(); return;
  }
  if (url.pathname === '/api/console/task-board') {
    const specPage = Number(url.searchParams.get('spec_page') || 1);
    const taskPage = Number(url.searchParams.get('task_page') || 1);
    const selected = url.searchParams.get('spec_id') || '';
    if (selected === 'spec-review') {
      res.writeHead(409, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end('该 spec 尚未审结(当前状态:review),审结后才能进入任务管理'); return;
    }
    const onlineAgents = mutation === 'k10-drop-online-options' ? [] : agents.filter((agent) => agent.id !== 'agent-offline' && (agent.id !== 'agent-dev' || developerOnline));
    if (mutation === 'k10-include-offline') onlineAgents.push(agents[2]);
    const visibleTasks = selected ? tasks.slice((taskPage - 1) * 20, taskPage * 20).map((task) => {
      const item = { ...task };
      item.developer_agent = item.developer_agent_id === 'agent-dev' ? 'developer-session' : '';
      item.reviewer_agent = item.reviewer_agent_id === 'agent-review' ? 'reviewer-session' : '';
      item.developer_online = item.developer_agent_id === 'agent-dev' && developerOnline;
      item.reviewer_online = item.reviewer_agent_id === 'agent-review';
      if (mutation === 'k11-clear-offline' && !item.developer_online) { item.developer_agent_id = ''; item.developer_agent = ''; }
      return item;
    }) : [];
    sendJSON(res, {
      specs: specs.slice((specPage - 1) * 8, specPage * 8), spec_page: specPage, spec_page_size: 8, spec_total_pages: 2,
      tasks: visibleTasks, agents: onlineAgents, task_page: taskPage, task_page_size: 20, task_total_pages: selected ? 2 : 1,
    }); return;
  }
  res.writeHead(404); res.end('not found');
});

async function main() {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({ channel: process.env.PW_CHANNEL || 'chrome', headless: true });
  const page = await browser.newPage();
  page.setDefaultTimeout(3000);
  await page.goto(base + '/console');
  await page.locator('#platform-items a[data-item-id="task-board"]').click();
  const frame = page.frameLocator('iframe[title="任务管理"]');
  const reloadAndSelect = async () => { const boardFrame = page.frames().find((candidate) => candidate.url().includes('/console/tasks')); await boardFrame.goto(boardFrame.url()); await frame.locator('#spec-list tr').first().waitFor(); await frame.locator('#spec-list tr').first().click(); await frame.locator('#task-list tr').first().waitFor(); };
  await frame.locator('#spec-list tr').first().waitFor();
  await page.evaluate(()=>{const c=new BroadcastChannel('dw-theme');c.postMessage({type:'theme',theme:'light'});c.close();});
  await frame.locator('html[data-theme="light"]').waitFor();
  const lightBackground=await frame.locator('body').evaluate((body)=>getComputedStyle(body).backgroundColor);
  await page.evaluate(()=>{const c=new BroadcastChannel('dw-theme');c.postMessage({type:'theme',theme:'dark'});c.close();});
  await frame.locator('html[data-theme="dark"]').waitFor();
  assert.strictEqual(await frame.locator('html').getAttribute('data-theme'),'dark','任务管理页必须接收公共暗色主题');
  assert.notStrictEqual(await frame.locator('body').evaluate((body)=>getComputedStyle(body).backgroundColor),lightBackground,'任务管理页暗色 computed background 必须真实变化');
  assert.strictEqual(await frame.locator('#spec-list tr').first().evaluate((row) => typeof row.onclick === 'function'), true, 'K6 spec row toggle handler installed');
  assert.strictEqual(await frame.locator('#spec-panel').isVisible(), true, 'K1 spec block visible');
  assert.strictEqual(await frame.locator('#task-panel').isVisible(), true, 'K1 task block visible');
  assert.deepStrictEqual(await frame.locator('#task-table th').allTextContents(), ['任务文本', '研发', '验收', '状态', '验收标准'], 'K2 five exact columns');
  assert.strictEqual(await frame.locator('#spec-list tr').count(), 8, 'K3 spec page size');
  assert.strictEqual(await frame.locator('#spec-page').textContent(), '1 / 2', 'K3 spec second page exists');
  await frame.locator('#spec-list tr').first().click();
  await frame.locator('#task-list tr').first().waitFor();
  assert.strictEqual(await frame.locator('#task-list tr').count(), 20, 'K3 task page size');
  assert.strictEqual(await frame.locator('#task-page').textContent(), '1 / 2', 'K3 task second page exists');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(1).textContent(), '', 'K5 missing developer empty');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(2).textContent(), '', 'K5 missing reviewer empty');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(4).textContent(), 'A1', 'K4 fifth column shows stable acceptance id');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(4).getAttribute('title'), 'A1 Acceptance 1', 'K4 acceptance hover exposes full text');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(3).textContent(), '0/1', 'K24 status uses produced-or-not fallback and never 0/0');
  assert.strictEqual((await frame.locator('body').innerText()).includes('未知'), false, 'K5 no invented unknown placeholder');
  await frame.locator('#edit-tasks').click();
  const firstDeveloper = frame.locator('#task-list tr').first().locator('select.developer-agent');
  assert.strictEqual(await firstDeveloper.locator('option[value="agent-dev"]').count(), 1, 'K10 online Agent appears in new assignment options');
  assert.strictEqual(await firstDeveloper.locator('option[value="agent-offline"]').count(), 0, 'K10 offline Agent absent from new assignment options');
  await firstDeveloper.selectOption('agent-dev');
  await frame.locator('#task-list tr').first().locator('select.reviewer-agent').selectOption('agent-review');
  await frame.locator('#finish-tasks').click();
  await frame.locator('#task-status').filter({ hasText: '已保存' }).waitFor();
  await reloadAndSelect();
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(1).textContent(), 'developer-session', 'K9 developer survives refresh');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(2).textContent(), 'reviewer-session', 'K9 reviewer survives refresh');
  await page.request.post(base + '/fixture/developer-offline');
  await reloadAndSelect();
  assert.match(await frame.locator('#task-list tr').first().locator('td').nth(1).textContent(), /developer-session/, 'K11 offline assignment retained');
  assert.strictEqual(await frame.locator('#task-list tr').first().locator('td').nth(1).locator('.offline-dot').count(), 1, 'K11 offline assignment gets grey dot');
  await frame.locator('#task-next').click();
  await frame.locator('#task-page').filter({ hasText: '2 / 2' }).waitFor();
  assert.strictEqual(await frame.locator('#task-list tr').count(), 1, 'K3 task page two');
  await frame.locator('#spec-next').click();
  await page.waitForTimeout(100);
  assert.strictEqual(await frame.locator('#spec-page').textContent(), '2 / 2', 'K3 spec next advances to page two');
  assert.strictEqual(await frame.locator('#spec-list tr').count(), 1, 'K3 spec page two');
  const blocked = await browser.newPage();
  await blocked.goto(base + '/console/tasks?spec_id=spec-review', { waitUntil: 'domcontentloaded' });
  await blocked.locator('#task-status').filter({ hasText: '该 spec 尚未审结(当前状态:review),审结后才能进入任务管理' }).waitFor();
  assert.strictEqual(await blocked.locator('#task-list tr').count(), 0, 'unfinalized direct access must explain inapplicability, not render an empty task set as valid');
  await blocked.close();
  await frame.locator('#spec-prev').click();
  await frame.locator('#task-list tr').first().waitFor();
  await frame.locator('#spec-list tr').first().click();
  await page.waitForTimeout(50);
  assert.strictEqual(await frame.locator('#task-list tr').count(), 0, 'K6 second click collapses task rows');
  assertModule.ok(assertionCount >= MIN_ASSERTIONS, `assertion floor ${MIN_ASSERTIONS} not met: ${assertionCount}`);
  console.log(`console task board: assertions=${assertionCount} floor=${MIN_ASSERTIONS} failed=0 (K13 deferred)`);
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}

main().catch(async (error) => { console.error(error); server.close(); process.exit(1); });
