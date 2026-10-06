// 0.2.1.101「我的 LLM 账号」区浏览器闸（R-V2-ACCOUNT-PER-USER＋R-V2-ACCOUNT-LOGIN-FLOW；业主文字即示意图）：
//   新增一行（LLM、方式、邮箱或 Key）；账号登录只显示可复制的链接与一次性码、轮询到结束；API Key 登记后输入清空、页面不留 Key；
//   列表（邮箱或 Key 末四位、LLM、方式、在用、状态、换 Key／重新登录、停用启用）；错误一律本页中文、不露机器码。
// MUTATION=<名>：同 console-quota-regression.js 约定（退出码 1 红·0 绿·2 崩·3 变异无效）。
const assert = require('assert');
const { execFileSync } = require('child_process');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

let html = execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
  cwd: path.join(__dirname, '..'), encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'],
  env: { ...process.env, DW_CONSOLE_HOSTED_FIXTURE: '1' },
});
const mutation = process.env.MUTATION || '';
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  if (hits !== 1) { console.error(`MUTATION-ANCHOR-STALE: mutation ${mutation} anchor must match exactly once, hits=${hits}: ${from.slice(0, 160)}`); process.exit(3); }
  html = html.replace(from, to);
}
const M = {
  'mutation-anchor-selftest': () => mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x'),
  'login-box-never-shown': () => mutate('box.hidden=!active;', 'box.hidden=true;'),
  'terms-note-hidden': () => mutate("document.getElementById('llmTermsNote').hidden=m!=='account_login';", ''),
  'key-not-cleared': () => mutate("ki.value='';key='';", "key='';"),
  'list-shows-name-not-last4': () => mutate("(a.last4?'…'+a.last4:a.name)", 'a.name'),
  'no-poll': () => mutate('llmLoginTimer=setInterval(llmPollLogin,3000);', ''),
  'login-err-raw': () => mutate("login_in_progress:'你已有一个进行中的账号登录，请先完成或取消',", ''),
  'apikey-offered-for-login-only': () => mutate("if(c.cred_kind==='api_key'&&", "if(false&&c.cred_kind==='api_key'&&"),
  'rekey-no-inuse-note': () => mutate("(n>0?'；在跑的 '+n+' 个 Agent 须逐个「刷新凭据」才生效':'')", "''"),
};
if (mutation && !M[mutation]) { console.error(`MUTATION-UNKNOWN: ${mutation}`); process.exit(3); }
if (mutation) M[mutation]();

const G = 'A'.repeat(32);
const KEY = 'sk-REALKEY-LEAKMARK-9z8y';
let accounts = [
  { id: 'a1', name: 'old@example.com', kind: 'token_file', method: 'account_login', email: 'old@example.com', cli: 'codex', llm: 'openai', state: 'available', in_use: 2, created_at: '2026-10-06T00:00:00Z' },
  { id: 'a2', name: 'openai …abcd', kind: 'api_key', method: 'api_key', cli: 'codex', llm: 'openai', last4: 'abcd', state: 'disabled', in_use: 0, created_at: '2026-10-06T00:00:00Z' },
];
const writes = [];
let loginPolls = 0; let loginFinal = 'done'; let loginStartErr = '';
function options() {
  return {
    nodes: [{ id: 'n1', name: 'node-a', pool: 'p', online: true, free_mem_gb: 12, free_vcpu: 6, free_disk_gb: 80 }],
    combos: [
      { cli: 'codex', llm: 'openai', cred_kind: 'token_file', verified: true },
      { cli: 'codex', llm: 'openai', cred_kind: 'api_key', verified: true },
      { cli: 'claude', llm: 'anthropic', cred_kind: 'api_key', verified: true },
    ],
    accounts: [], plans: [{ id: '2c4g', name: '2C/4G', vcpu: 2, mem_gb: 4, fits: true }],
    quota: { remaining: { agents: 3, vcpu: 6, mem_gb: 12, disk_gb: 100 } }, models: [],
  };
}
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function read(req) { return new Promise((r) => { let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => r(b ? JSON.parse(b) : {})); }); }
const LOGIN = { id: 'l'.repeat(32), url: 'https://auth.example.test/codex/device', user_code: 'QWER-12345' };
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/api/console/agents') return json(res, 200, { agents: [] });
  if (p === '/api/console/platform') return json(res, 200, { role: 'owner', actions: [], items: [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'ready' }] });
  if (p === '/api/console/agent-groups') return json(res, 200, { principal: 'owner', owned_agents: [], groups: [] });
  if (p === '/api/console/enroll/applications') return json(res, 200, { applications: [] });
  if (p === '/api/console/hosted/options') return json(res, 200, options());
  if (p === '/api/console/llm-accounts' && req.method === 'GET') return json(res, 200, accounts);
  if (p === '/api/console/llm-accounts' && req.method === 'POST') {
    const b = await read(req); writes.push({ p, b });
    accounts.push({ id: 'a3', name: b.name, kind: 'api_key', method: 'api_key', cli: b.cli, llm: b.llm, last4: b.secret.slice(-4), state: 'available', in_use: 0, created_at: 'x' });
    return json(res, 201, accounts[accounts.length - 1]);
  }
  let m = p.match(/^\/api\/console\/llm-accounts\/([^/]+)\/(secret|disable|enable)$/);
  if (m) {
    const b = await read(req); writes.push({ p, b, method: req.method });
    if (m[2] === 'secret') return json(res, 200, { in_use: 3 });
    accounts = accounts.map((a) => (a.id === m[1] ? { ...a, state: m[2] === 'disable' ? 'disabled' : 'available' } : a));
    return json(res, 200, {});
  }
  if (p === '/api/console/llm-account-logins' && req.method === 'POST') {
    const b = await read(req); writes.push({ p, b });
    if (loginStartErr) return json(res, loginStartErr === 'email_invalid' ? 400 : 409, { error: loginStartErr, detail: 'SECRET-HUB-TEXT' });
    loginPolls = 0;
    return json(res, 201, { ...LOGIN, state: 'waiting_user', llm: b.llm || 'openai', email: b.email || '' });
  }
  m = p.match(/^\/api\/console\/llm-account-logins\/([^/]+)(\/cancel)?$/);
  if (m) {
    if (m[2]) { writes.push({ p }); return json(res, 200, { id: m[1], state: 'cancelled', reason: 'cancelled' }); }
    loginPolls += 1;
    if (loginPolls < 2) return json(res, 200, { ...LOGIN, state: 'waiting_user' });
    if (loginFinal === 'done') {
      if (!accounts.some((a) => a.id === 'a9')) accounts.push({ id: 'a9', name: 'new@example.com', kind: 'token_file', method: 'account_login', email: 'new@example.com', cli: 'codex', llm: 'openai', state: 'available', in_use: 0, created_at: 'x' });
      return json(res, 200, { id: m[1], state: 'done', account_id: 'a9' });
    }
    return json(res, 200, { id: m[1], state: 'failed', reason: 'login_timeout' });
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const page = await (await browser.newContext()).newPage(); page.setDefaultTimeout(8000);
  const pageErrors = []; const consoleErrors = [];
  page.on('pageerror', (e) => pageErrors.push(e.message));
  page.on('console', (msg) => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });
  const type = async (sel, v) => { await page.locator(sel).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(v); };
  const methods = () => page.locator('#llmAddMethod option').evaluateAll((els) => els.map((o) => [o.value, o.textContent]));
  const rows = () => page.locator('#llmItems tr').evaluateAll((trs) => trs.map((tr) => Array.from(tr.children).map((td) => td.textContent)));
  try {
    await page.goto(`http://127.0.0.1:${server.address().port}/console`);
    await page.locator('#platform-items a[data-item-id="agent-groups"]').click();
    await page.locator('#llmAccounts').waitFor({ state: 'visible' });
    await page.waitForFunction(() => document.querySelectorAll('#llmItems tr').length === 2);

    await test('methods follow the combo table: openai offers 账号登录 and API Key, anthropic only API Key; the input and terms note follow the method', async () => {
      await page.locator('#llmAddLlm').selectOption('openai');
      assert.deepStrictEqual(await methods(), [['', '请选择方式'], ['account_login', '账号登录'], ['api_key', 'API Key']]);
      await page.locator('#llmAddMethod').selectOption('account_login');
      assert.strictEqual(await page.locator('#llmAddEmailBox').isVisible(), true);
      assert.strictEqual(await page.locator('#llmAddKeyBox').isVisible(), false);
      assert.strictEqual(await page.locator('#llmTermsNote').isVisible(), true);
      await page.locator('#llmAddMethod').selectOption('api_key');
      assert.strictEqual(await page.locator('#llmAddKeyBox').isVisible(), true);
      assert.strictEqual(await page.locator('#llmTermsNote').isVisible(), false);
      await page.locator('#llmAddLlm').selectOption('anthropic');
      assert.deepStrictEqual(await methods(), [['', '请选择方式'], ['api_key', 'API Key']]);
    });

    await test('the list shows email for account login and …last4 for API keys, with 重新登录／换 Key and 停用／启用 by state', async () => {
      const r = await rows();
      assert.deepStrictEqual(r[0].slice(0, 5), ['old@example.com', 'openai', '账号登录', '2', '可用']);
      assert.match(r[0][5], /重新登录/); assert.match(r[0][5], /停用/);
      assert.deepStrictEqual(r[1].slice(0, 5), ['…abcd', 'openai', 'API Key', '0', '已停用']);
      assert.match(r[1][5], /换 Key/); assert.match(r[1][5], /启用/);
    });

    await test('account login: the request carries only llm＋email; the box shows the copyable link (href) and code; polling ends in 已登录 and the new account is listed', async () => {
      await page.locator('#llmAddLlm').selectOption('openai'); await page.locator('#llmAddMethod').selectOption('account_login');
      await type('#llmAddEmail', 'new@example.com');
      const n = writes.length; await page.locator('#llmAddBtn').click();
      await page.locator('#llmLoginBox').waitFor({ state: 'visible' });
      assert.deepStrictEqual(writes[n].b, { llm: 'openai', email: 'new@example.com' });
      assert.strictEqual(await page.locator('#llmLoginUrl').textContent(), LOGIN.url);
      assert.strictEqual(await page.locator('#llmLoginUrl').getAttribute('href'), LOGIN.url);
      assert.strictEqual(await page.locator('#llmLoginUrl').getAttribute('rel'), 'noopener noreferrer');
      assert.strictEqual(await page.locator('#llmLoginCode').textContent(), LOGIN.user_code);
      await page.locator('#llmFeedback').filter({ hasText: '已登录' }).waitFor({ timeout: 12000 });
      assert.strictEqual(await page.locator('#llmLoginBox').isVisible(), false);
      await page.waitForFunction(() => Array.from(document.querySelectorAll('#llmItems td')).some((t) => t.textContent === 'new@example.com'));
    });

    await test('cancel: posts to the cancel endpoint, hides the box and says 已取消', async () => {
      loginFinal = 'never';
      await page.locator('#llmAddLlm').selectOption('openai'); await page.locator('#llmAddMethod').selectOption('account_login');
      await type('#llmAddEmail', 'c@example.com'); await page.locator('#llmAddBtn').click();
      await page.locator('#llmLoginBox').waitFor({ state: 'visible' });
      const n = writes.length; await page.locator('#llmLoginCancel').click();
      await page.locator('#llmFeedback').filter({ hasText: '已取消' }).waitFor();
      assert.ok(writes.slice(n).some((w) => /\/cancel$/.test(w.p)));
      assert.strictEqual(await page.locator('#llmLoginBox').isVisible(), false);
      loginFinal = 'done';
    });

    await test('login start errors use our wording, never the Hub text or machine code', async () => {
      for (const [code, want] of [['login_in_progress', '你已有一个进行中的账号登录'], ['email_invalid', '账号邮箱格式不对']]) {
        loginStartErr = code;
        await type('#llmAddEmail', 'x@example.com'); await page.locator('#llmAddBtn').click();
        await page.locator('#llmFeedback').filter({ hasText: want }).waitFor();
        assert.doesNotMatch(await page.locator('#llmFeedback').textContent(), /SECRET-HUB-TEXT|login_in_progress|email_invalid/);
      }
      loginStartErr = '';
    });

    await test('API key: body kind=api_key with the combo CLI and a name ending in the last four; the input is cleared and the key is nowhere in the page', async () => {
      await page.locator('#llmAddLlm').selectOption('openai'); await page.locator('#llmAddMethod').selectOption('api_key');
      await type('#llmAddKey', KEY);
      const n = writes.length; await page.locator('#llmAddBtn').click();
      await page.locator('#llmFeedback').filter({ hasText: '已新增' }).waitFor();
      const w = writes[n].b;
      assert.strictEqual(w.kind, 'api_key'); assert.strictEqual(w.cli, 'codex'); assert.strictEqual(w.llm, 'openai'); assert.strictEqual(w.secret, KEY);
      assert.ok(w.name.endsWith('9z8y') && !w.name.includes('LEAKMARK'), w.name);
      assert.strictEqual(await page.locator('#llmAddKey').inputValue(), '');
      assert.ok(!(await page.content()).includes('LEAKMARK'), 'key leaked into the DOM');
    });

    await test('换 Key: PUT secret, then the in-use note says the running Agents must each be refreshed', async () => {
      page.once('dialog', (d) => d.accept('sk-NEWKEY-12345678'));
      const row = page.locator('#llmItems tr', { hasText: '…abcd' });
      const n = writes.length; await row.locator('button', { hasText: '换 Key' }).click();
      await page.locator('#llmFeedback').filter({ hasText: '刷新凭据' }).waitFor();
      assert.strictEqual(writes[n].method, 'PUT'); assert.deepStrictEqual(writes[n].b, { secret: 'sk-NEWKEY-12345678' });
      assert.match(await page.locator('#llmFeedback').textContent(), /在跑的 3 个 Agent/);
    });

    await test('停用／启用 posts to the state endpoint and the row follows', async () => {
      const row = page.locator('#llmItems tr', { hasText: 'old@example.com' });
      await row.locator('button', { hasText: '停用' }).click();
      await page.waitForFunction(() => Array.from(document.querySelectorAll('#llmItems tr')).some((tr) => tr.textContent.includes('old@example.com') && tr.textContent.includes('已停用')));
      assert.ok(writes.some((w) => /\/a1\/disable$/.test(w.p)));
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|409)/.test(t)), []);
    });
  } finally {
    await browser.close(); server.close();
    console.log(`llm accounts browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
    process.exit(fail ? 1 : 0);
  }
})().catch((e) => { console.error(e); process.exit(2); });
