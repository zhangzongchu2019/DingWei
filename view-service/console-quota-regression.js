// 托管配额页（「平台管理」Tab 的「托管配额」；T-G4-41，契约 ae6b6443；R-V2-SEAT-ITEMS 包 C1）的浏览器闸。
//
// 装置同 console-agent-mgmt-hosted-regression.js：页面走生产 HandleConsolePage 的服务端注入路径（go run ./view-service/console-hosted-page-fixture）
// 取得，不在装置里改 FEATURE_HOSTED；Hub 用本地桩（只按契约形状回：GET 数组、PUT 四键）。
// 口径：真键盘（click + keyboard.type，不用 fill）；pageerror 与 console error 两路监听，末尾断言为空（只放行桩有意返回的 4xx/5xx 资源行）。
//
// MUTATION=<name> 删除对应守卫，对应那条断言必须红（首红必须是该条；后续用例共享桩状态会级联）：
//   quota-ignores-feature-flag   托管面没开也出「托管配额」入口
//   quota-put-wrong-key          第三键写成 mem_mb_limit（契约是 mem_gb_limit、单位 GB）
//   quota-put-missing-key        PUT 少一个键（契约：四键齐全才收）
//   quota-no-int-check           不校验整数（1.5／1e2／负数照发）
//   quota-low-no-note            上限低于已用量时不提醒
//   quota-blocks-below-used      上限低于已用量时拒绝保存（契约：允许保存，只限新建）
//   quota-leaks-hub-text         错误提示显示 Hub 原文
//   quota-403-wrong-text         403 不说「没有权限」
//   quota-no-refetch-after-save  保存成功后不重新取（已用量／上限不以服务端为准）
//   quota-no-saved-mark          保存成功不显示「已保存」
//   quota-double-submit          保存中不禁用按钮（连点发两次）
//   quota-enter-no-save          输入框里按回车不保存
//   quota-empty-on-failure       加载失败却显示「暂无用户」
//   quota-no-retry-wire          加载失败后「重试加载」点了没反应
//   quota-user-id-not-encoded    路径里的用户 ID 不转义
//   quota-used-hidden            不并排显示「已用」
// 退出码：1＝有用例红（变异被杀）· 0＝全绿（变异下＝存活）· 3＝MUTATION-ANCHOR-STALE／UNKNOWN／WRONG-FORM（变异无效）· 2＝脚本崩。
// 环境变量: PW_CHANNEL=chrome 用系统 Chrome；NODE_PATH 指向含 playwright 的 node_modules。
const assert = require('assert');
const { execFileSync } = require('child_process');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

function renderPage(hosted) {
  return execFileSync('go', ['run', './view-service/console-hosted-page-fixture'], {
    cwd: path.join(__dirname, '..'), encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'],
    env: { ...process.env, DW_CONSOLE_HOSTED_FIXTURE: hosted ? '1' : '0' },
  });
}
let html = renderPage(true);
let htmlOff = renderPage(false);
assert(html.includes("var FEATURE_HOSTED='true'==='true';"), 'real server injection returned the wrong hosted feature value (on)');
assert(htmlOff.includes("var FEATURE_HOSTED='false'==='true';"), 'real server injection returned the wrong hosted feature value (off)');
const mutation = process.env.MUTATION || '';
if (process.argv.some((a) => a.startsWith('--mutate'))) { console.error('MUTATION-SWITCH-WRONG-FORM: this script reads env MUTATION=<name>, not --mutate=<name> (the argv form would be silently ignored)'); process.exit(3); }
function mutate(from, to) {
  const hits = html.split(from).length - 1;
  if (hits !== 1) {
    console.error(`MUTATION-ANCHOR-STALE: mutation ${mutation} anchor must match exactly once, hits=${hits}: ${from.slice(0, 160)}`);
    process.exit(3);
  }
  html = html.replace(from, to);
  htmlOff = htmlOff.replace(from, to);   // 托管面关着的那份页面同样变异（「没开就没有入口」那条才测得到开关被改）
}
const MUTATIONS = ['quota-ignores-feature-flag', 'quota-put-wrong-key', 'quota-put-missing-key', 'quota-no-int-check', 'quota-low-no-note', 'quota-blocks-below-used', 'quota-leaks-hub-text', 'quota-403-wrong-text',
  'quota-no-refetch-after-save', 'quota-no-saved-mark', 'quota-double-submit', 'quota-enter-no-save', 'quota-empty-on-failure', 'quota-no-retry-wire', 'quota-user-id-not-encoded', 'quota-used-hidden', 'mutation-anchor-selftest'];
if (mutation === 'quota-ignores-feature-flag') mutate("concat(FEATURE_HOSTED?[{id:'quota',title:'托管配额'}]:[])", "concat([{id:'quota',title:'托管配额'}])");
if (mutation === 'quota-put-wrong-key') mutate("else if(!bad)body[k[1]]=Number(raw);", "else if(!bad)body[k[1]==='mem_gb_limit'?'mem_mb_limit':k[1]]=Number(raw);");
if (mutation === 'quota-put-missing-key') mutate("else if(!bad)body[k[1]]=Number(raw);", "else if(!bad&&k[1]!=='disk_gb_limit')body[k[1]]=Number(raw);");
if (mutation === 'quota-no-int-check') mutate("if(!bad&&!/^\\d{1,7}$/.test(raw))bad=k[2]+'上限须为 0 或正整数';else if(!bad)", "if(false)bad='';else if(!bad)");
if (mutation === 'quota-low-no-note') mutate("tr.querySelector('.quota-note').textContent=low.length?", "tr.querySelector('.quota-note').textContent=false?");
if (mutation === 'quota-blocks-below-used') mutate("  if(bad){fb.textContent=bad;return;}\n  btn.disabled=true;fb.textContent='保存中…';", "  QUOTA_KEYS.forEach(function(k){var i=tr.querySelector('input[data-key='+k[1]+']');if(!bad&&Number(i.value)<Number(i.dataset.used))bad='上限不能低于已用量';});\n  if(bad){fb.textContent=bad;return;}\n  btn.disabled=true;fb.textContent='保存中…';");
if (mutation === 'quota-leaks-hub-text') mutate("if(!res.ok){fb.textContent=res.status===403?'你没有管理用户的权限':res.text;return;}", "if(!res.ok){fb.textContent=res.status===403?'你没有管理用户的权限':(res.data&&res.data.detail?String(res.data.detail):res.text);return;}");
if (mutation === 'quota-403-wrong-text') mutate("if(!res.ok){fb.textContent=res.status===403?'你没有管理用户的权限':res.text;return;}", "if(!res.ok){fb.textContent=res.text;return;}");
if (mutation === 'quota-no-refetch-after-save') mutate("},5000);\n  refreshAdminQuotas();\n}", "},5000);\n}");
if (mutation === 'quota-no-saved-mark') mutate("if(quotaSavedId&&quotaSavedId===uid)fb.textContent='已保存';", "");
if (mutation === 'quota-double-submit') mutate("btn.disabled=true;fb.textContent='保存中…';", "fb.textContent='保存中…';");
if (mutation === 'quota-enter-no-save') mutate("inp.onkeydown=function(e){if(e.key==='Enter'){e.preventDefault();save.click();}};", "");
if (mutation === 'quota-empty-on-failure') mutate("quotaRows=[];body.replaceChildren();empty.hidden=true;status.textContent='配额加载失败: '", "quotaRows=[];body.replaceChildren();empty.hidden=false;status.textContent='配额加载失败: '");
if (mutation === 'quota-no-retry-wire') mutate("var quotaRetryBtn=document.getElementById('quotaRetry');if(quotaRetryBtn)quotaRetryBtn.onclick=refreshAdminQuotas;", "");
if (mutation === 'quota-user-id-not-encoded') mutate("quota:function(id){return '/api/console/admin/quotas/'+encodeURIComponent(id);}", "quota:function(id){return '/api/console/admin/quotas/'+id;}");
if (mutation === 'quota-used-hidden') mutate("used.textContent='已用 '+quotaNum(cell.used)+k[3]+' / 上限 ';", "used.textContent='上限 ';");
if (mutation === 'mutation-anchor-selftest') mutate('THIS-TEXT-IS-NOT-IN-THE-PAGE', 'x'); // 自检：必须以退出码 3 报 MUTATION-ANCHOR-STALE
if (mutation && !MUTATIONS.includes(mutation)) { console.error(`MUTATION-UNKNOWN: ${mutation} is not a mutation of this script`); process.exit(3); }

const writes = [];
let getCount = 0;
let getMode = ['503', 'ok'];   // 每次 GET 取队首模式：503＝先失败一次再成功
let putMode = 'ok';
let putDelayMs = 0;
let role = 'management';
// 桩里的服务端状态（以服务端为准：保存后页面必须重新取到它）
let rows = [
  { user: { id: 'adminone', short_name: 'ad' }, agents: { used: 1, limit: 3 }, vcpu: { used: 4, limit: 15 }, mem_gb: { used: 8, limit: 63 }, disk_gb: { used: 20, limit: 60 } },
  { user: { id: 'Bob/七', short_name: 'bo' }, agents: { used: 0, limit: 0 }, vcpu: { used: 0, limit: 0 }, mem_gb: { used: 0, limit: 0 }, disk_gb: { used: 0, limit: 0 } },
];
function read(req) { return new Promise((resolve) => { let b = ''; req.on('data', (c) => { b += c; }); req.on('end', () => resolve(b ? JSON.parse(b) : {})); }); }
function json(res, status, body) { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(body)); }
function err(res, status, code) { json(res, status, { error: code, detail: 'SECRET-HUB-TEXT' }); }
const server = http.createServer(async (req, res) => {
  const p = req.url.split('?')[0];
  if (p === '/console') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(html); }
  if (p === '/console-off') { res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' }); return res.end(htmlOff); }
  if (p === '/api/console/agents') return json(res, 200, { agents: [] });
  if (p === '/api/console/agent-groups') return json(res, 200, { owned_agents: [], groups: [] });
  if (p === '/api/console/platform') {
    // S-R62-CONSOLE-SLIM：菜单只留 Agent 管理＋平台管理（与产品新形状同形）。
    const items = [{ id: 'agent-groups', kind: 'agent-groups', title: 'Agent 管理', href: '#agent-groups', status: 'approved' }];
    if (role === 'management') items.push({ id: 'platform-admin', kind: 'platform-admin', title: '平台管理', href: '#platform-admin', status: 'approved' });
    return json(res, 200, { role, actions: [], items });
  }
  if (p === '/api/console/platform/users' && req.method === 'GET') return json(res, 200, { users: [] });
  if (p === '/api/console/admin/quotas' && req.method === 'GET') {
    getCount += 1; const mode = getMode.length ? getMode.shift() : 'ok';
    if (mode === '503') { res.writeHead(503, { 'content-type': 'text/plain' }); return res.end('quota unavailable'); }
    if (mode === '403') return err(res, 403, 'forbidden_role');
    return json(res, 200, rows);
  }
  const m = p.match(/^\/api\/console\/admin\/quotas\/(.+)$/);
  if (m && req.method === 'PUT') {
    const body = await read(req); writes.push({ path: p, body, contentType: req.headers['content-type'] || '' });
    if (putDelayMs) await new Promise((r) => setTimeout(r, putDelayMs));
    if (putMode === '403') return err(res, 403, 'forbidden_role');
    if (putMode === '500') return err(res, 500, 'internal');
    const uid = decodeURIComponent(m[1]); const row = rows.find((r) => r.user.id.toLowerCase() === uid.toLowerCase());
    if (!row) return err(res, 404, 'not_found');
    row.agents.limit = body.agents_limit; row.vcpu.limit = body.vcpu_limit; row.mem_gb.limit = body.mem_gb_limit; row.disk_gb.limit = body.disk_gb_limit;
    return json(res, 200, { state: 'ok' });
  }
  if (p === '/favicon.ico') { res.writeHead(204); return res.end(); }
  res.writeHead(404); res.end();
});

let pass = 0; let fail = 0;
async function test(name, fn) { try { await fn(); pass++; console.log(`PASS ${name}`); } catch (e) { fail++; console.log(`FAIL ${name}: ${e.message}`); } }
(async () => {
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.PW_CHANNEL ? { headless: true, channel: process.env.PW_CHANNEL } : { headless: true });
  const context = await browser.newContext();
  const pageErrors = []; const consoleErrors = [];
  const watch = (pg) => { pg.on('pageerror', (e) => pageErrors.push(e.message)); pg.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); }); };
  try {
    const page = await context.newPage(); watch(page); page.setDefaultTimeout(2500);
    const type = async (selector, value) => { await page.locator(selector).click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.type(value); };
    const row = (id) => page.locator(`.quota-row[data-user-id="${id}"]`);
    const limit = (id, key) => row(id).locator(`input[data-key="${key}"]`);
    await page.goto(`${base}/console`);

    await test('托管面开着时，平台管理 Tab 的能力菜单含「托管配额」，且默认仍显示「新用户注册邀请」', async () => {
      await page.locator('#platform-items a[data-item-id="platform-admin"]').click();
      await page.locator('#tabs .tab[title="平台管理"]').waitFor({ state: 'visible' });
      assert.deepStrictEqual(await page.locator('#adminMenu button').allTextContents(), ['新用户注册邀请', '托管配额']);
      assert.strictEqual(await page.locator('#adminInvite').isVisible(), true);
      assert.strictEqual(await page.locator('#adminQuota').isHidden(), true, 'quota stays hidden until its menu entry is clicked');
      assert.strictEqual(getCount, 0, 'no quota request before the entry is opened');
    });

    await test('托管面没开时没有「托管配额」入口（页面不探测 404 猜开关）', async () => {
      const off = await context.newPage(); watch(off); off.setDefaultTimeout(2500);
      await off.goto(`${base}/console-off`);
      await off.locator('#platform-items a[data-item-id="platform-admin"]').click();
      await off.locator('#adminMenu button').first().waitFor({ state: 'visible' });
      assert.deepStrictEqual(await off.locator('#adminMenu button').allTextContents(), ['新用户注册邀请']);
      assert.strictEqual(await off.locator('#adminQuota').count(), 0);
      await off.close();
    });

    await test('点「托管配额」：邀请块收起、配额块出现；首次加载失败时说明原因并给重试，且不冒称「暂无用户」', async () => {
      await page.locator('#adminMenu button[data-capability="quota"]').click();
      await page.locator('#quotaStatus').filter({ hasText: '配额加载失败' }).waitFor({ state: 'visible' });
      assert.match(await page.locator('#quotaStatus').textContent(), /503/);
      assert.strictEqual(await page.locator('#quotaEmpty').isHidden(), true, 'a failed load must not claim "no users"');
      assert.strictEqual(await page.locator('#adminInvite').isHidden(), true);
      assert.strictEqual(await page.locator('#quotaRetry').isVisible(), true);
      assert.strictEqual(await page.locator('#adminMenu button[data-capability="quota"]').getAttribute('aria-current'), 'true');
      assert.strictEqual(await page.locator('#adminMenu button[data-capability="invite"]').getAttribute('aria-current'), null);
    });

    await test('重试加载：一行一个用户，四项都是「已用 / 上限」并排（内存、磁盘按 GB）', async () => {
      await page.locator('#quotaRetry').click();
      await row('adminone').waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#quotaStatus').textContent(), '');
      assert.strictEqual(await page.locator('.quota-row').count(), 2);
      const cells = await row('adminone').locator('td').allTextContents();
      assert.match(cells[0], /ad（adminone）/);
      assert.match(cells[1], /已用 1 \/ 上限/); assert.match(cells[2], /已用 4 \/ 上限/);
      assert.match(cells[3], /已用 8G \/ 上限.*GB/); assert.match(cells[4], /已用 20G \/ 上限.*GB/);
      assert.deepStrictEqual(await Promise.all(['agents_limit', 'vcpu_limit', 'mem_gb_limit', 'disk_gb_limit'].map((k) => limit('adminone', k).inputValue())), ['3', '15', '63', '60']);
    });

    await test('改上限用真键盘；保存发恰好四个键、整数、GB 单位，路径里的用户 ID 做转义；成功后显示「已保存」并以服务端为准重新取', async () => {
      const gets = getCount;
      await type('.quota-row[data-user-id="adminone"] input[data-key="agents_limit"]', '5');
      await type('.quota-row[data-user-id="adminone"] input[data-key="mem_gb_limit"]', '96');
      await row('adminone').locator('.quota-save').click();
      await row('adminone').locator('.quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible' });
      const w = writes[writes.length - 1];
      assert.strictEqual(w.path, '/api/console/admin/quotas/adminone');
      assert.deepStrictEqual(w.body, { agents_limit: 5, vcpu_limit: 15, mem_gb_limit: 96, disk_gb_limit: 60 });
      assert.match(w.contentType, /application\/json/);
      assert.ok(getCount > gets, 'the list must be fetched again after a successful save');
      assert.strictEqual(await limit('adminone', 'agents_limit').inputValue(), '5');
    });

    await test('用户 ID 含斜杠和中文时路径必须转义（否则落到别的路由）', async () => {
      await type('.quota-row[data-user-id="Bob/七"] input[data-key="vcpu_limit"]', '2');
      await type('.quota-row[data-user-id="Bob/七"] input[data-key="agents_limit"]', '1');
      await type('.quota-row[data-user-id="Bob/七"] input[data-key="mem_gb_limit"]', '4');
      await type('.quota-row[data-user-id="Bob/七"] input[data-key="disk_gb_limit"]', '8');
      await page.locator('.quota-row[data-user-id="Bob/七"] .quota-save').click();
      await page.locator('.quota-row[data-user-id="Bob/七"] .quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible' });
      assert.strictEqual(writes[writes.length - 1].path, '/api/console/admin/quotas/Bob%2F%E4%B8%83');
    });

    await test('不合规的值本地拦住、什么也不发：空、负数、小数、科学计数', async () => {
      const before = writes.length;
      for (const bad of ['', '-1', '1.5', '1e2']) {
        await type('.quota-row[data-user-id="adminone"] input[data-key="vcpu_limit"]', bad || 'x');
        if (!bad) { await page.locator('.quota-row[data-user-id="adminone"] input[data-key="vcpu_limit"]').click(); await page.keyboard.press('ControlOrMeta+A'); await page.keyboard.press('Backspace'); }
        await row('adminone').locator('.quota-save').click();
        await row('adminone').locator('.quota-fb').filter({ hasText: /须为 0 或正整数/ }).waitFor({ state: 'visible' });
        assert.strictEqual(writes.length, before, `"${bad}" must not be sent`);
      }
      await type('.quota-row[data-user-id="adminone"] input[data-key="vcpu_limit"]', '15');
    });

    await test('新上限低于已用量：就地提醒「只限制新建、已有 Agent 不受影响」，但允许保存', async () => {
      await type('.quota-row[data-user-id="adminone"] input[data-key="vcpu_limit"]', '2');
      await row('adminone').locator('.quota-note').filter({ hasText: /低于已用量（vCPU）.*只限制新建/ }).waitFor({ state: 'visible' });
      const before = writes.length;
      await row('adminone').locator('.quota-save').click();
      await row('adminone').locator('.quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible' });
      assert.strictEqual(writes.length, before + 1, 'saving below the used amount is allowed (it only limits new creation)');
      assert.strictEqual(writes[writes.length - 1].body.vcpu_limit, 2);
      await type('.quota-row[data-user-id="adminone"] input[data-key="vcpu_limit"]', '15');
      await row('adminone').locator('.quota-note').filter({ hasText: /^$/ }).waitFor({ state: 'attached' });
      await row('adminone').locator('.quota-save').click();
      await row('adminone').locator('.quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible' });
    });

    await test('在输入框里按回车等于点保存', async () => {
      const before = writes.length;
      await type('.quota-row[data-user-id="adminone"] input[data-key="disk_gb_limit"]', '61');
      await page.keyboard.press('Enter');
      await row('adminone').locator('.quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible' });
      assert.strictEqual(writes.length, before + 1);
      assert.strictEqual(writes[writes.length - 1].body.disk_gb_limit, 61);
    });

    await test('保存期间按钮禁用，连点只发一次', async () => {
      putDelayMs = 400; const before = writes.length;
      await type('.quota-row[data-user-id="adminone"] input[data-key="agents_limit"]', '6');
      await row('adminone').locator('.quota-save').dblclick();
      await row('adminone').locator('.quota-fb').filter({ hasText: '已保存' }).waitFor({ state: 'visible', timeout: 4000 });
      putDelayMs = 0;
      assert.strictEqual(writes.length, before + 1, 'double click must send exactly one PUT');
    });

    await test('保存失败只说我们自己的话：403 说没有权限，500 说服务端出错，Hub 原文不出现在页面上', async () => {
      putMode = '403';
      await row('adminone').locator('.quota-save').click();
      await row('adminone').locator('.quota-fb').filter({ hasText: '你没有管理用户的权限' }).waitFor({ state: 'visible' });
      putMode = '500';
      await row('adminone').locator('.quota-save').click();
      await row('adminone').locator('.quota-fb').filter({ hasText: '服务端出错了' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('body').innerText().then((t) => t.includes('SECRET-HUB-TEXT')), false, 'Hub text must never reach the page');
      assert.strictEqual(await row('adminone').locator('.quota-save').isDisabled(), false, 'the button is usable again after a failure');
      putMode = 'ok';
    });

    await test('GET 返回 403（不是管理用户）：说没有权限、给重试、不显示 Hub 原文', async () => {
      getMode = ['403'];
      await page.locator('#adminMenu button[data-capability="invite"]').click();
      await page.locator('#adminMenu button[data-capability="quota"]').click();
      await page.locator('#quotaStatus').filter({ hasText: '你没有管理用户的权限' }).waitFor({ state: 'visible' });
      assert.strictEqual(await page.locator('#quotaStatus').innerText().then((t) => t.includes('SECRET-HUB-TEXT')), false);
      assert.strictEqual(await page.locator('.quota-row').count(), 0, 'a refused list must not keep stale rows on screen');
      assert.strictEqual(await page.locator('#quotaEmpty').isHidden(), true);
      await page.locator('#quotaRetry').click();
      await row('adminone').waitFor({ state: 'visible' });
    });

    await test('切回「新用户注册邀请」：配额块收起、邀请块回来', async () => {
      await page.locator('#adminMenu button[data-capability="invite"]').click();
      assert.strictEqual(await page.locator('#adminQuota').isHidden(), true);
      assert.strictEqual(await page.locator('#adminInvite').isVisible(), true);
    });

    await test('普通用户拿不到平台管理入口，自然也没有配额页', async () => {
      role = 'owner';
      const owner = await context.newPage(); watch(owner); owner.setDefaultTimeout(2500);
      await owner.goto(`${base}/console`);
      await owner.locator('#platform-items a[data-item-id="agent-groups"]').waitFor({ state: 'visible' });
      assert.strictEqual(await owner.locator('#platform-items a[data-item-id="specs"]').count(), 0);
      assert.strictEqual(await owner.locator('#platform-items a[data-item-id="task-board"]').count(), 0);
      assert.strictEqual(await owner.locator('#platform-items a[data-item-id="platform-admin"]').count(), 0);
      assert.strictEqual(await owner.locator('#adminQuota').count(), 0);
      await owner.close(); role = 'management';
    });

    await test('no pageerror and no unexpected console error across the whole run', async () => {
      assert.deepStrictEqual(pageErrors, []);
      assert.deepStrictEqual(consoleErrors.filter((t) => !/status of (400|401|403|404|409|429|500|503)/.test(t)), []);
    });
  } finally { await context.close(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
  console.log(`quota page browser: passed=${pass} failed=${fail} mutation=${mutation || 'none'}`);
  // 退出码：1＝有用例失败（变异被杀）· 0＝全绿（变异下＝存活）· 3＝MUTATION-ANCHOR-STALE／UNKNOWN／WRONG-FORM · 2＝脚本崩。判「变异红」只认退出码 1 且有 FAIL 行。
  if (mutation) console.log(`MUTATION-VERDICT: ${mutation} ${fail >= 1 ? 'KILLED' : 'SURVIVED'}`);
  if (fail) process.exit(1);
})().catch((e) => { console.error(e); process.exit(2); });
