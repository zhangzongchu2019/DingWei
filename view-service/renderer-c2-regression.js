// C2 · 待提交区删除 + 中断确认 回归(renderer-c2-regression.js)
// 配对 C2 契约现状 v0.1 红绿门 sketch:G-HONEST(诚实呈现)/G-ADDR(寻址字段随请求带出)。
// 全部断言=呈现语义(契约无关):transport 经 window.__c2.setTransport 注入 mock。
// 用法: RENDERER_FILE=<path> node renderer-c2-regression.js
const { chromium } = require('playwright');
const fs = require('fs');
const assert = require('assert');

const RENDERER_FILE = process.env.RENDERER_FILE || 'renderer-c2.html';
const SESSION = 'c2-test-2642';

async function bootContext(browser, rendererBody, { interruptRouteHits, initScript, controlRouteHits, controlReply } = {}) {
  const context = await browser.newContext();
  if (initScript) await context.addInitScript(initScript);
  await context.route('http://renderer.test/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/renderer') return route.fulfill({ contentType: 'text/html', body: rendererBody });
    if (path === '/status') {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        session: SESSION, auth_mode: 'view_page_v2', readonly: false, state: 'idle',
      }) });
    }
    if (path === '/events') return route.abort();
    if (path === '/interrupt') {
      interruptRouteHits && interruptRouteHits.push(route.request().method());
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    }
    if (path === '/control' && (controlRouteHits || controlReply)) {
      const body = route.request().postDataJSON() || {};
      controlRouteHits && controlRouteHits.push(body);
      const reply = controlReply || { ok: true };
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify(reply) });
    }
    if (path === '/page') {
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
        page_id: 'p1', page_token: 't1', code: 'c1', state: 'locked',
      }) });
    }
    if (path === '/page/resume') {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        page_id: 'p1', code: 'c1', state: 'locked', can_write: true,
      }) });
    }
    return route.fulfill({ status: 404, body: 'nf' });
  });
  const page = await context.newPage();
  await page.goto('http://renderer.test/renderer', { waitUntil: 'domcontentloaded' });
  await page.waitForFunction(() => pageIdentity?.page_id, null, { timeout: 8000 });
  return { context, page };
}

const emit = (page, ev) => page.evaluate(e => handleIncomingEvent(e), ev);
const src = { display_name: '飞书', trust_decision: 'trusted' };
const inputPosted = (id, text, extra = {}) => ({
  kind: 'input_posted', message_id: id, delivery_id: `dlv-${id}`,
  source: src, stage: 'generated', cancellable: true, payload: text, ...extra,
});
const controlEv = (ctlKind, text, extra = {}) => ({
  kind: 'input_posted', message_id: `ctl-${ctlKind}`, delivery_id: `ctl-${ctlKind}`,
  delivery: { role: 'control' }, control: { kind: ctlKind },
  source: { kind: 'system', trust_decision: 'trusted' },
  stage: 'generated', cancellable: false, payload: text, ...extra,
});

const waitingItems = page => page.locator('.waiting-item');
const notes = (page, text) => page.locator(`.c2-note:has-text("${text}")`);
const controlNotes = (page, text) => page.locator(`.event-card:has-text("${text}")`);
// 等最终 note(跳过中间态"取消请求中…")
const waitNote = (page, text) => page.waitForFunction(
  t => [...document.querySelectorAll('.c2-note')].some(n => n.textContent.includes(t)), text, { timeout: 8000 });

async function main() {
  const rendererBody = fs.readFileSync(RENDERER_FILE, 'utf8');
  const browser = await chromium.launch();
  let passed = 0;
  const failures = [];
  const T = async (name, fn) => {
    try { await fn(); passed++; console.log(`[PASS] ${name}`); }
    catch (err) { failures.push(`${name}: ${err.message}`); console.log(`[FAIL] ${name} — ${err.message}`); }
  };

  // ── C2.1 删除按钮(待提交区取消) ──
  await T('C2.1a cancellable 条目带删除按钮;非 cancellable 不带', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, inputPosted('m1', '可取消的那条'));
    await emit(page, inputPosted('m2', '不可取消的那条', { cancellable: false }));
    await assert.waitFor?.(0) ?? null;
    const items = waitingItems(page);
    assert.strictEqual(await items.count(), 2, '两条待提交');
    const btns = page.locator('.waiting-item .waiting-cancel-btn');
    assert.strictEqual(await btns.count(), 1, '只有 cancellable 条目有删除按钮');
    await context.close();
  });

  await T('C2.1b /control 不可达(404)→明说"取消未发出"+按钮恢复+条目不消失(不本地静默删)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);  // 无 /control 路由 → 兜底 404
    await emit(page, inputPosted('m1', '要删的那条'));
    await page.locator('.waiting-cancel-btn').click();
    await waitNote(page, '取消未发出');
    const note = await page.locator('.c2-note').last().textContent();
    assert.ok(note.includes('取消未发出'), `诚实未发出文案, got: ${note}`);
    assert.strictEqual(await waitingItems(page).count(), 1, '条目仍在(绝不本地静默删)');
    assert.strictEqual(await page.locator('.waiting-cancel-btn').isDisabled(), false, '按钮恢复可重试');
    await context.close();
  });

  await T('C2.1d 真接线(非 stub):默认 transport 真打 VS /control、字段逐字 {kind,message_id,delivery_id}→"取消已请求"', async () => {
    const hits = [];
    const { context, page } = await bootContext(browser, rendererBody, { controlRouteHits: hits, controlReply: { ok: true } });
    await emit(page, inputPosted('m1', '要删的那条'));
    await page.locator('.waiting-cancel-btn').click();
    await waitNote(page, '取消已请求');
    const note = await page.locator('.c2-note').last().textContent();
    assert.ok(note.includes('取消已请求'), `已请求文案, got: ${note}`);
    assert.strictEqual(hits.length, 1, '默认 transport 真发了 1 次 POST /control(非 no_transport 空转)');
    assert.strictEqual(hits[0].kind, 'cancel', '字段 kind=cancel 逐字');
    assert.strictEqual(hits[0].message_id, 'm1', '字段 message_id 逐字(G-ADDR)');
    assert.strictEqual(hits[0].delivery_id, 'dlv-m1', '字段 delivery_id 逐字(G-ADDR)');
    assert.deepStrictEqual(Object.keys(hits[0]).sort(), ['delivery_id', 'kind', 'message_id'], '只带三字段、无多余');
    await context.close();
  });

  await T('C2.1e VS 受理但 ok=false(已注入/终态)→"取消未生效"+按钮恢复(不谎报已请求)', async () => {
    const { context, page } = await bootContext(browser, rendererBody, { controlRouteHits: [], controlReply: { ok: false } });
    await emit(page, inputPosted('m1', '要删的那条'));
    await page.locator('.waiting-cancel-btn').click();
    await waitNote(page, '取消未生效');
    const note = await page.locator('.c2-note').last().textContent();
    assert.ok(note.includes('取消未生效'), `未生效文案, got: ${note}`);
    assert.strictEqual(await page.locator('.c2-note:has-text("取消已请求")').count(), 0, '不谎报已请求');
    assert.strictEqual(await page.locator('.waiting-cancel-btn').isDisabled(), false, '按钮恢复');
    await context.close();
  });

  await T('C2.1c mock transport 在场→"取消已请求,待确认"+条目仍在;cancelled 事件才移除(单一事实源)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    const sent = [];
    await page.evaluate(() => { window.__c2Sent = []; window.__c2.setTransport({ send: m => { window.__c2Sent.push(m); return Promise.resolve({ ok: true }); } }); });
    await emit(page, inputPosted('m1', '要删的那条'));
    await page.locator('.waiting-cancel-btn').click();
    await waitNote(page, '取消已请求');
    const note = await page.locator('.c2-note').last().textContent();
    assert.ok(note.includes('取消已请求'), `已请求文案, got: ${note}`);
    const msgs = await page.evaluate(() => window.__c2Sent);
    assert.strictEqual(msgs.length, 1, 'transport 收到 1 条');
    assert.strictEqual(msgs[0].kind, 'cancel', 'kind=cancel');
    assert.strictEqual(msgs[0].delivery_id, 'dlv-m1', 'G-ADDR:寻址带 delivery_id');
    assert.strictEqual(msgs[0].message_id, 'm1', 'G-ADDR:寻址带 message_id');
    assert.strictEqual(await waitingItems(page).count(), 1, '条目仍在等事件流确认');
    // dev55 实证 hub 真 cancel SSE 线格式:{observed_delivery_id, delivery_stage, delivery_status:cancelled}
    await emit(page, { kind: 'delivery_update', message_id: 'm1', observed_delivery_id: 'dlv-m1',
      delivery_stage: 'helper_received', delivery_status: 'cancelled' });
    await page.waitForFunction(() => document.querySelectorAll('.waiting-item').length === 0, null, { timeout: 5000 });
    await context.close();
  });

  await T('C2.1f 无 status 的 delivery_update(如 helper_received 阶段推进)不摘条目(防误摘)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, inputPosted('m1', '条目'));
    await emit(page, { kind: 'delivery_update', message_id: 'm1', observed_delivery_id: 'dlv-m1',
      delivery_stage: 'helper_received' });
    await page.waitForTimeout(300);
    assert.strictEqual(await waitingItems(page).count(), 1, '仅阶段推进不摘(需 delivery_status=cancelled)');
    await context.close();
  });

  // ── C2.2 中断确认状态机(G-HONEST) ──
  await T('C2.2a 点中断→立即"已请求中断";无 transport 时回退 legacy /interrupt(POST 200≠已发出)', async () => {
    const hits = [];
    const { context, page } = await bootContext(browser, rendererBody, { interruptRouteHits: hits });
    await page.locator('#interruptBtn').click();
    await page.waitForSelector('.event-card:has-text("已请求中断")', { timeout: 5000 });
    const st = await page.evaluate(() => window.__c2.state.interrupt && window.__c2.state.interrupt.state);
    assert.strictEqual(st, 'requested', 'pending 状态=requested');
    await page.waitForFunction(() => true, null, { timeout: 200 }); // 让回退 fetch 发出
    await page.waitForTimeout(300);
    assert.ok(hits.length >= 1, '无 transport 回退 legacy POST /interrupt');
    assert.strictEqual(await page.locator('.event-card:has-text("中断已发出")').count(), 0, 'POST 200 不升级已发出(G-HONEST)');
    await context.close();
  });

  await T('C2.2b 流上 control 事件→升级"中断已发出"+pending 清空', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await page.locator('#interruptBtn').click();
    await page.waitForSelector('.event-card:has-text("已请求中断")', { timeout: 5000 });
    await emit(page, controlEv('interrupt'));
    await page.waitForSelector('.event-card:has-text("中断已发出")', { timeout: 5000 });
    const st = await page.evaluate(() => window.__c2.state.interrupt);
    assert.strictEqual(st, null, 'pending 已清空(回执到达)');
    await context.close();
  });

  await T('C2.2c 超时无回执→"中断未确认(无回执)"(永不谎报已发出)', async () => {
    const { context, page } = await bootContext(browser, rendererBody, {
      initScript: () => { window.C2_ACK_TIMEOUT_MS = 300; },
    });
    await page.locator('#interruptBtn').click();
    await page.waitForSelector('.event-card:has-text("中断未确认(无回执)")', { timeout: 5000 });
    assert.strictEqual(await page.locator('.event-card:has-text("中断已发出")').count(), 0, '无回执绝不显示已发出');
    await context.close();
  });

  await T('C2.2d 重复点击不重复发起(防重)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await page.evaluate(() => { window.__c2Sent = []; window.__c2.setTransport({ send: m => { window.__c2Sent.push(m); return new Promise(() => {}); } }); });
    await page.locator('#interruptBtn').click();
    await page.locator('#interruptBtn').click();
    await page.waitForTimeout(300);
    assert.strictEqual(await page.locator('.event-card:has-text("已请求中断")').count(), 1, '只有一条已请求');
    const n = await page.evaluate(() => window.__c2Sent.length);
    assert.strictEqual(n, 1, 'transport 只发 1 条 interrupt');
    await context.close();
  });

  await T('C2.2e mock transport 在场→中断走 transport(kind=interrupt)不回退 legacy', async () => {
    const hits = [];
    const { context, page } = await bootContext(browser, rendererBody, { interruptRouteHits: hits });
    await page.evaluate(() => { window.__c2Sent = []; window.__c2.setTransport({ send: m => { window.__c2Sent.push(m); return Promise.resolve({ ok: true }); } }); });
    await page.locator('#interruptBtn').click();
    await page.waitForTimeout(300);
    const msgs = await page.evaluate(() => window.__c2Sent);
    assert.strictEqual(msgs.length, 1, 'transport 收到 interrupt');
    assert.strictEqual(msgs[0].kind, 'interrupt');
    assert.strictEqual(hits.length, 0, '有 transport 时不回退 legacy /interrupt');
    await context.close();
  });

  await browser.close();
  console.log('='.repeat(60));
  if (failures.length) { console.log(`FAILED ${failures.length}: ${failures}`); process.exit(1); }
  console.log(`C2 删除+中断确认回归: ALL PASS (${passed})`);
}

main().catch(err => { console.error(err); process.exit(1); });
