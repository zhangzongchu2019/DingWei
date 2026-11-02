// R14-S018/S023: production Console HTML, local fake APIs, no Feishu calls.
const assert = require('assert');
const http = require('http');
const path = require('path');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');
const root = path.join(__dirname, '..');
let html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: root, encoding: 'utf8', env: { ...process.env, DW_CONSOLE_NAME_CHECK_FIXTURE: '1', DW_CONSOLE_HOSTED_FIXTURE: '1' },
});
const mutation = process.argv.find(a => a.startsWith('--mutate='))?.split('=')[1] || '';
function mutate(from, to) {
  assert.strictEqual(html.split(from).length - 1, 1, 'MUTATION_ANCHOR_INVALID');
  html = html.replace(from, to);
}
if (mutation === 'enroll-no-check') mutate("if(!await agentNameAvailable(name,'enrollFeedback')){enrollBusy(false);return;}", '');
else if (mutation === 'hosted-no-check') mutate("if(!await agentNameAvailable(body.name,'hostedFeedback')){btn.disabled=false;return;}", '');
else if (mutation === 'candidate-strip-hash') {} // applied to the local text transport below
else if (mutation) throw new Error('UNKNOWN_MUTATION');
const posts = [], checks = [];
let failCheck = false, malformed = false;
const apps = [{ application_id: 'app-r14', source_ip: '127.0.0.1', device_fingerprint: 'local-fixture', client_version: 'fixture', requested_at: new Date().toISOString(), same_device_count: 1 }];
const opts = {
 nodes: [{ id: 'local-node', name: 'local-node', online: true, free_mem_gb: 8, free_vcpu: 4, free_disk_gb: 40 }],
 combos: [{ cli: 'codex', llm: 'openai', cred_kind: 'api_key', verified: true }],
 accounts: [{ id: 'local-account', name: 'fixture', llm: 'openai', kind: 'api_key', last4: '0000' }],
 plans: [{ id: 'local-plan', name: 'fixture', vcpu: 1, mem_gb: 1, fits: true }],
 quota: { remaining: { agents: 3, vcpu: 4, mem_gb: 8, disk_gb: 40 } },
};
function json(res, status, value) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(value)); }
const server = http.createServer((req, res) => {
 const url = new URL(req.url, 'http://localhost'), p = url.pathname;
 if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
 if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 管理', href: '#agent-groups', status: 'ready' }] });
 if (p === '/api/console/agents') return json(res, 200, { agents: [] });
 if (p === '/api/console/agent-groups') return json(res, 200, { principal: 'owner', owned_agents: [], groups: [] });
 if (p === '/api/console/enroll/applications') return json(res, 200, { applications: apps });
 if (p === '/api/console/hosted/options') return json(res, 200, opts);
 if (p === '/api/console/llm-accounts') return json(res, 200, []);
 if (p === '/api/console/agent-name-used') {
  checks.push(Object.fromEntries(url.searchParams));
  if (failCheck) return json(res, 503, { error: 'fixture outage' });
  if (malformed) return json(res, 200, { used: false });
  return json(res, 200, ['taken', 'sleeping'].includes(url.searchParams.get('name_lc')));
 }
 if (req.method === 'POST') { posts.push(p); return json(res, 409, { error: 'name_taken' }); }
 if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
 return json(res, 200, {});
});
(async () => {
 await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
 const base = `http://127.0.0.1:${server.address().port}`;
 let browser;
 try {
  browser = await chromium.launch({ headless: true, ...(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {}), args: ['--no-sandbox'] });
  const page = await browser.newPage(), errors = [], foreign = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/*', async route => {
   if (!route.request().url().startsWith(base + '/')) { foreign.push(route.request().url()); return route.abort(); }
   return route.continue();
  });
  await page.goto(base + '/console');
  await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
  await page.locator('#enrollItems button').first().click();
  await page.locator('#enrollCode').fill('123456');
  await page.locator('#hostedForm').waitFor({ state: 'visible' });
  await page.locator('#hostedCli').selectOption('codex');
  await page.locator('#hostedLlm').selectOption('openai');
  await page.locator('#hostedAccount').selectOption('local-account');
  await page.locator('#hostedPlan').selectOption('local-plan');
  for (const name of ['TaKeN', 'Sleeping']) {
   for (const entry of ['enroll', 'hosted']) {
    const nameId = entry === 'enroll' ? '#enrollName' : '#hostedName';
    const button = entry === 'enroll' ? '#enrollApprove' : '#hostedCreateBtn';
    await page.locator(nameId).fill(name);
    const before = posts.length;
    await page.locator(button).click();
    await page.waitForFunction(id => !document.querySelector(id).disabled, button);
    assert.strictEqual(posts.length, before, 'S023_' + entry.toUpperCase() + '_DUPLICATE_POST');
    assert.match(await page.locator('#' + entry + 'Feedback').innerText(), /短名已被占用/, 'S023_DUPLICATE_NOT_VISIBLE');
    assert.strictEqual(await page.locator(nameId).inputValue(), name, 'S023_INPUT_LOST');
   }
  }
  for (const mode of ['failure', 'malformed']) {
   failCheck = mode === 'failure'; malformed = mode === 'malformed';
   for (const entry of ['enroll', 'hosted']) {
    const button = entry === 'enroll' ? '#enrollApprove' : '#hostedCreateBtn';
    await page.locator(entry === 'enroll' ? '#enrollName' : '#hostedName').fill('FreeName');
    const before = posts.length;
    await page.locator(button).click();
    await page.waitForFunction(id => !document.querySelector(id).disabled, button);
    assert.strictEqual(posts.length, before, 'S023_PREFLIGHT_ERROR_SENT_POST');
    assert.match(await page.locator('#' + entry + 'Feedback').innerText(), /查重失败/, 'S023_CHECK_ERROR_HIDDEN');
   }
  }
  failCheck = false; malformed = false;
  for (const entry of ['enroll', 'hosted']) {
   const button = entry === 'enroll' ? '#enrollApprove' : '#hostedCreateBtn';
   await page.locator(entry === 'enroll' ? '#enrollName' : '#hostedName').fill('FreeName');
   const before = posts.length;
   await page.locator(button).click();
   await page.waitForFunction(id => !document.querySelector(id).disabled, button);
   assert.strictEqual(posts.length, before + 1, 'S023_FREE_NAME_NOT_SUBMITTED');
  }
  assert(checks.length >= 10 && checks.every(q => Object.keys(q).join() === 'name_lc' && q.name_lc === q.name_lc.toLowerCase()), 'S023_OWNER_INPUT_OR_UNNORMALIZED');
  // Mock text transport only: desktop/mobile Feishu is separately authorized T-08.
  const candidate = '请选择一个 Agent：\n#Beta.owner\n#Alpha.owner';
  let transported = JSON.parse(JSON.stringify({ text: candidate })).text;
  if (mutation === 'candidate-strip-hash') transported = transported.replaceAll('#', '');
  const rendered = await page.evaluate(text => { const box = document.createElement('pre'); box.textContent = text; document.body.append(box); return box.textContent; }, transported);
  assert.strictEqual(rendered, candidate, 'S018_HASH_OR_NEWLINE_LOST');
  assert.strictEqual(foreign.length, 0, 'REAL_NETWORK_ATTEMPT');
  assert.deepStrictEqual(errors, [], 'BROWSER_JS_ERROR');
  console.log('R14_BROWSER_OK S018=local-mock S023=both-entry-preflight authorization=separate-PG-test');
 } finally { if (browser) await browser.close(); await new Promise(resolve => server.close(resolve)); }
})().catch(e => { console.error(e); process.exitCode = 1; });
