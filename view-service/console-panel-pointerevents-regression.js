// 平台功能面板(Specs / Agent 编组)不得被 Agent 在线轮询判成"离线"而变成不可点。
//
// 🔴 缺陷形状(业主 2026-09-23 报"下拉无响应/组码框无法输入/Specs 按键无响应"):
//    console 外壳每 15 秒跑一次 refreshAgents → applyAgentSnapshot(agents),其中
//      tabs.forEach((e,session) => { const online = agentSnapshot.get(session)===true;
//                                    e.pane.style.pointerEvents = online ? '' : 'none'; })
//    而平台面板也登记在同一张 tabs 表里(`__specs` / `__agent-groups`),
//    它们不是真实会话 ⇒ agentSnapshot.get(...) 是 undefined ⇒ online=false
//    ⇒ **整块 pane 被设成 pointer-events:none** ⇒ 点不进、聚不了焦、于是"按键也没反应"。
//    🔵 pointer-events 这个机制本身对【Agent 掉线】是有意设计,不动它;
//       错的是把平台面板也当成 Agent 去判在线。
//
// 🔴 为什么此前所有装置全绿:它们都在 15 秒内跑完,**永远等不到第一次 tick**。
//    ⇒ 本件专门设一条【真的等满一个 tick】的用例,不靠手工调用糊过去。
//
// 真实 console 上的实测(锚 8a3a33a7 = 0.2.1.63 回流,空库,两个真 WebSocket 在线 Agent):
//    T+1s  Specs 面板内点「新建 spec」成功
//    T+18s iframe(specs) pointerEvents="none" · group-workspace pointerEvents="none"
//          在线 Agent 的 iframe 仍为 ""(不受影响)
//    T+25s Specs 内点击与输入全部失败;T+29s 编组「团队名称」框点不进
const assert = require('assert');
const { agentRecord } = require('./fixture-agents');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const MUTATION = process.argv.find((arg) => arg.startsWith('--mutate='))?.slice('--mutate='.length) || '';

function productionHTML() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML start marker missing');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML end marker missing');
  let html = source.slice(contentStart, end);
  // 变异:把"跳过平台面板"的保护去掉 ⇒ 本件必须转红。锚失配即报错,不静默空跑。
  if (MUTATION === 'panel-treated-as-agent') {
    assert.ok(html.includes('isPlatformPaneKey'),
      'panel-treated-as-agent 锚点未命中:修复尚未落地,或保护写法与本件预期不同。\n'
      + '修复方请把"跳过平台面板"的判断命名为 isPlatformPaneKey(session),或同步改本件的锚点 —— 变异不生效等于这条证明不存在。');
    html = html.replace(/if\s*\(isPlatformPaneKey\(session\)\)\s*return;/g, '');
  }
  if (MUTATION && MUTATION !== 'panel-treated-as-agent') throw new Error(`unknown mutation: ${MUTATION}`);
  return html;
}

const listen = (server) => new Promise((resolve, reject) => {
  server.once('error', reject);
  server.listen(0, '127.0.0.1', () => resolve(server.address().port));
});

async function main() {
  const html = productionHTML();
  const agents = [agentRecord('owner-alpha-2642', true), agentRecord('owner-offline-2642', false)];
  const server = http.createServer((req, res) => {
    const json = (body) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)); };
    if (req.url === '/console') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end(html); return; }
    if (req.url === '/api/console/agents') return json({ agents });
    if (req.url === '/api/console/platform') return json({ role: 'management', actions: ['read', 'submit'], items: [
      { id: 'agents', kind: 'agents', title: 'Agents', href: '#agents', status: 'approved' },
      { id: 'specs', kind: 'spec', title: 'Specs', href: '#specs', status: 'approved' },
      { id: 'agent-groups', kind: 'agent-groups', title: 'Agent 编组', href: '#agent-groups', status: 'approved' }] });
    if (String(req.url).startsWith('/api/console/agent-groups')) return json({ principal: 'owner-a', owned_agents: [{ id: 'agent-a', session_name: 'owner-alpha-2642', online: true }], groups: [] });
    if (String(req.url).startsWith('/console/specs')) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end('<!doctype html><title>specs stub</title><button id=stub>stub</button>'); return; }
    if (String(req.url).startsWith('/view/')) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); res.end('<!doctype html><title>view stub</title>'); return; }
    res.writeHead(404); res.end('not found');
  });
  const port = await listen(server);
  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);
  let passed = 0;
  try {
    const page = await (await browser.newContext()).newPage();
    const pageErrors = [];
    page.on('pageerror', (error) => pageErrors.push(error.message));
    await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'load' });
    await page.waitForSelector('#platform-items a[data-item-id=specs]');
    await page.click('#platform-items a[data-item-id=specs]');
    await page.click('#platform-items a[data-item-id=agent-groups]');
    await page.click('#a button');
    await page.evaluate(() => window.openTab('owner-offline-2642'));   // 离线项在折叠区且按钮 disabled,直接走产品函数
    await page.waitForTimeout(600);

    const paneState = () => page.evaluate(() => {
      const pick = (pred) => { const el = [...document.querySelectorAll('#panes > *')].find(pred); return el ? (el.style.pointerEvents || '') : 'MISSING'; };
      const tab = (title) => { const t = [...document.querySelectorAll('#tabs .tab')].find((x) => x.title === title); return t ? { cls: t.className, aria: t.getAttribute('aria-disabled') } : 'MISSING'; };
      return {
        specs: pick((el) => el.tagName === 'IFRAME' && el.src.includes('/console/specs')),
        groups: pick((el) => el.classList && el.classList.contains('group-workspace')),
        agent: pick((el) => el.tagName === 'IFRAME' && el.src.includes('/view/owner-alpha-2642')),
        offlineAgent: pick((el) => el.tagName === 'IFRAME' && el.src.includes('/view/owner-offline-2642')),
        specsTab: tab('Specs'), groupsTab: tab('Agent 管理'),
      };
    });

    // ① 手工触发一次在线轮询(快、确定):平台面板不得被判成离线
    await page.evaluate(() => window.refreshAgents && window.refreshAgents());
    await page.waitForTimeout(400);
    const manual = await paneState();
    assert.strictEqual(manual.specs, '', `刷新一次 Agent 在线状态后,Specs 面板被设成 pointer-events:${manual.specs} ⇒ 整块点不进去`);
    assert.strictEqual(manual.groups, '', `刷新一次 Agent 在线状态后,编组面板被设成 pointer-events:${manual.groups} ⇒ 整块点不进去`);
    assert.notStrictEqual(manual.specsTab.aria, 'true', 'Specs 页签被标成 aria-disabled=true,而它完全可用');
    assert.notStrictEqual(manual.groupsTab.aria, 'true', '编组页签被标成 aria-disabled=true,而它完全可用');
    passed += 4;

    // ② 🔴 真的等满一个 15 秒 tick —— 此前所有装置都在 15 秒内跑完,才一直全绿
    await page.waitForTimeout(16500);
    const ticked = await paneState();
    assert.strictEqual(ticked.specs, '', `等满一个 15 秒 tick 后,Specs 面板 pointer-events=${ticked.specs} ⇒ 业主看到的"按键无响应"`);
    assert.strictEqual(ticked.groups, '', `等满一个 15 秒 tick 后,编组面板 pointer-events=${ticked.groups} ⇒ 业主看到的"下拉无响应/组码框打不进字"`);
    passed += 2;

    // ③ 对照(两侧都要):在线 Agent 的面板保持可点;🔴 **离线 Agent 的面板必须仍然是禁用的**
    //    —— 后者是防"为了修这条把机制整行删掉"。`pointer-events:none` 对真实掉线是有意设计。
    assert.strictEqual(ticked.agent, '', `在线 Agent 的面板不该被禁用,实测 pointer-events=${ticked.agent}`);
    assert.strictEqual(ticked.offlineAgent, 'none',
      `离线 Agent 的面板必须保持禁用(这是有意设计),实测 pointer-events=${JSON.stringify(ticked.offlineAgent)}`);
    passed += 2;

    // ④ 真实点击:面板内的控件必须还能点(像人一样,先点回那个页签再操作)
    await page.locator('#tabs .tab').filter({ hasText: 'Specs' }).click();
    await page.waitForTimeout(200);
    const frame = page.frames().find((f) => f.url().includes('/console/specs'));
    await frame.locator('#stub').click({ timeout: 4000 });
    await page.locator('#tabs .tab').filter({ hasText: 'Agent 管理' }).click();
    await page.waitForTimeout(200);
    await page.locator('#groupAddOpen').click({ timeout: 4000 });
    await page.locator('#groupShortName').click({ timeout: 4000 });
    await page.keyboard.type('abc');
    assert.strictEqual(await page.locator('#groupShortName').inputValue(), 'abc', '编组面板的团队名称框在一个 tick 之后必须仍能输入');
    passed += 2;

    assert.deepStrictEqual(pageErrors, [], `console 外壳报了顶层错误: ${JSON.stringify(pageErrors)}`);
    passed += 1;
    console.log(`console panel pointer-events: passed=${passed} failed=0`);
  } finally {
    await browser.close(); await new Promise((resolve) => server.close(resolve));
  }
}
main().catch((error) => { console.error(error); process.exit(1); });
