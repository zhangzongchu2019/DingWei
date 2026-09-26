const assert = require('assert');
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

if (mutation === 'k1-hide-task-panel') board = board.replace('<section id=task-panel class=panel>', '<section id=task-panel class=panel hidden>');
if (mutation === 'k1-drop-initial-load') board = board.replace('\nloadBoard();\n</script>', '\n</script>');
if (mutation === 'k2-drop-status-column') board = board.replace('<th>状态</th><th>验收标准</th>', '<th>验收标准</th>');
if (mutation === 'k2-drop-task-table') board = board.replace('<table id=task-table>', '<div id=task-table>');
if (mutation === 'k3-break-next-page') board = board.replace("specPage++;loadBoard()", "specPage+=0;loadBoard()");
if (mutation === 'k3-drop-next-load') board = board.replace("specPage++;loadBoard()", "specPage++");
if (mutation === 'k4-break-acceptance') board = board.replace("if(id)ids.push(id)", "if(false)ids.push(id)");
if (mutation === 'k4-drop-acceptance-call') board = board.replace("acceptance=acceptanceDisplay(task.acceptance_json)", "acceptance={label:'',title:''}");
if (mutation === 'k5-invent-placeholder') board = board.replace("td.textContent=value||''", "td.textContent=value||'未知人员'");
if (mutation === 'k5-drop-empty-normalizer') board = board.replace("td.textContent=value||''", "td.textContent=String(value)");
if (mutation === 'k6-break-toggle') board = board.replace("selectedSpec=selectedSpec===spec.id?'':spec.id", "selectedSpec=spec.id");
if (mutation === 'k6-drop-toggle-handler') board = board.replace("tr.onclick=function(){selectedSpec=selectedSpec===spec.id?'':spec.id;taskPage=1;loadBoard()}", '');

const specs = Array.from({ length: 9 }, (_, i) => ({ id: `spec-${i + 1}`, title: `Spec ${i + 1}`, state: 'final' }));
const tasks = Array.from({ length: 21 }, (_, i) => ({
  proposal_id: `task-${i + 1}`, title: `Task ${i + 1}`, content: `Task text ${i + 1}`,
  acceptance_json: JSON.stringify([{ id: `A${i + 1}`, text: `Acceptance ${i + 1}` }]), status: 'planned',
}));

function sendJSON(res, value) {
  res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(value));
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://fixture');
  if (url.pathname === '/console') { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end(shell); return; }
  if (url.pathname === '/console/tasks') { res.writeHead(200, { 'Content-Type': 'text/html' }); res.end(board); return; }
  if (url.pathname === '/api/console/platform') {
    sendJSON(res, { role: 'owner', actions: ['read'], items: [{ id: 'task-board', kind: 'task-board', title: '任务管理', href: '#task-board', status: 'approved' }] }); return;
  }
  if (url.pathname === '/api/console/agents') { sendJSON(res, { agents: [] }); return; }
  if (url.pathname === '/api/console/agent-groups') { sendJSON(res, { owned_agents: [], groups: [] }); return; }
  if (url.pathname === '/api/console/task-board') {
    const specPage = Number(url.searchParams.get('spec_page') || 1);
    const taskPage = Number(url.searchParams.get('task_page') || 1);
    const selected = url.searchParams.get('spec_id') || '';
    sendJSON(res, {
      specs: specs.slice((specPage - 1) * 8, specPage * 8), spec_page: specPage, spec_page_size: 8, spec_total_pages: 2,
      tasks: selected ? tasks.slice((taskPage - 1) * 20, taskPage * 20) : [], task_page: taskPage, task_page_size: 20, task_total_pages: selected ? 2 : 1,
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
  await frame.locator('#spec-list tr').first().waitFor();
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
  assert.strictEqual((await frame.locator('body').innerText()).includes('未知'), false, 'K5 no invented unknown placeholder');
  await frame.locator('#task-next').click();
  await frame.locator('#task-page').filter({ hasText: '2 / 2' }).waitFor();
  assert.strictEqual(await frame.locator('#task-list tr').count(), 1, 'K3 task page two');
  await frame.locator('#spec-next').click();
  await frame.locator('#spec-page').filter({ hasText: '2 / 2' }).waitFor();
  assert.strictEqual(await frame.locator('#spec-list tr').count(), 1, 'K3 spec page two');
  await frame.locator('#spec-prev').click();
  await frame.locator('#task-list tr').first().waitFor();
  await frame.locator('#spec-list tr').first().click();
  await page.waitForTimeout(50);
  assert.strictEqual(await frame.locator('#task-list tr').count(), 0, 'K6 second click collapses task rows');
  console.log('console task board: K1-K6 passed=16 failed=0');
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}

main().catch(async (error) => { console.error(error); server.close(); process.exit(1); });
