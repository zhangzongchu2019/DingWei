// C2 · 待提交区删除 + 中断确认 回归(renderer-c2-regression.js)
// 配对 C2 契约现状 v0.1 红绿门 sketch:G-HONEST(诚实呈现)/G-ADDR(寻址字段随请求带出)。
// 全部断言=呈现语义(契约无关):transport 经 window.__c2.setTransport 注入 mock。
// 用法: RENDERER_FILE=<path> node renderer-c2-regression.js
const { chromium } = require('playwright');
const fs = require('fs');
const assert = require('assert');

const RENDERER_FILE = process.env.RENDERER_FILE || 'renderer-c2.html';
const HUB_V2_GOLDEN_FILE = process.env.HUB_V2_GOLDEN_FILE || 'hub-delivery-v2-golden.json';
const SESSION = 'c2-test-2642';

async function bootContext(browser, rendererBody, { interruptRouteHits, initScript, controlRouteHits, controlReply, servePath = '/renderer', apiRequestUrls } = {}) {
  const context = await browser.newContext();
  if (initScript) await context.addInitScript(initScript);
  await context.route('http://renderer.test/**', async route => {
    const path = new URL(route.request().url()).pathname;
    if (apiRequestUrls && /\/(events|input|interrupt|control|status|page)$/.test(path)) apiRequestUrls.push(path);
    if (path === servePath) return route.fulfill({ contentType: 'text/html', body: rendererBody });
    if (path === '/status' || path.endsWith('/status')) {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        session: SESSION, auth_mode: 'view_page_v2', readonly: false, state: 'idle',
      }) });
    }
    if (path === '/events' || path.endsWith('/events')) return route.abort();
    if (path === '/interrupt' || path.endsWith('/interrupt')) {
      interruptRouteHits && interruptRouteHits.push(route.request().method());
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ok: true }) });
    }
    if ((path === '/control' || path.endsWith('/control')) && (controlRouteHits || controlReply)) {
      const body = route.request().postDataJSON() || {};
      controlRouteHits && controlRouteHits.push(body);
      const reply = controlReply || { ok: true };
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify(reply) });
    }
    if (path === '/page' || path.endsWith('/page')) {
      return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({
        page_id: 'p1', page_token: 't1', code: 'c1', state: 'locked',
      }) });
    }
    if (path === '/page/resume' || path.endsWith('/page/resume')) {
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({
        page_id: 'p1', code: 'c1', state: 'locked', can_write: true,
      }) });
    }
    return route.fulfill({ status: 404, body: 'nf' });
  });
  const page = await context.newPage();
  await page.goto('http://renderer.test' + servePath, { waitUntil: 'domcontentloaded' });
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

// Hub canonical delivery.v2 冻结形状(与 publishInputObservation/publishDeliveryUpdate 逐字同形)
const V2_TS = '2026-07-22T00:00:00.000Z';
const v2src = (kind, name) => ({ kind, display_name: name, trust_decision: 'trusted' });
const v2posted = (id, source, role, stage = 'hub_received') => ({
  schema_version: 'delivery.v2', kind: 'input_posted', message_id: id, delivery_id: `dlv-${id}`,
  stage, payload: `text-${id}`, source, delivery: { role },
  ...(role === 'copy' ? { copy_kind: 'route_observation' } : {}),
  cancellable: role === 'primary', updated_at: V2_TS,
});
const v2update = (id, dlvId, stage, status, reason = '') => ({
  schema_version: 'delivery.v2', kind: 'delivery_update', message_id: id,
  observed_delivery_id: dlvId, delivery_stage: stage,
  ...(status ? { delivery_status: status } : {}),
  reason, cancelable: false, interruptible: false, updated_at: V2_TS,
});
const hubV2Golden = JSON.parse(fs.readFileSync(HUB_V2_GOLDEN_FILE, 'utf8'));
const goldenEvent = name => JSON.parse(JSON.stringify(hubV2Golden[name]));

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
    await emit(page, v2update('m1', 'dlv-m1', 'helper_received', 'cancelled'));
    await page.waitForFunction(() => document.querySelectorAll('.waiting-item').length === 0, null, { timeout: 5000 });
    await context.close();
  });

  await T('C2.1f 无 status 的 delivery_update(如 helper_received 阶段推进)不摘条目(防误摘)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, inputPosted('m1', '条目'));
    await emit(page, v2update('m1', 'dlv-m1', 'helper_received', ''));
    await page.waitForTimeout(300);
    assert.strictEqual(await waitingItems(page).count(), 1, '仅阶段推进不摘(需 delivery_status=cancelled)');
    await context.close();
  });

  await T('C2.1g canonical failed 终态→摘条目(failed/cancelled 正反之正)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, inputPosted('m1', '条目'));
    await emit(page, v2update('m1', 'dlv-m1', 'helper_received', 'failed'));
    await page.waitForFunction(() => document.querySelectorAll('.waiting-item').length === 0, null, { timeout: 5000 });
    await context.close();
  });

  await T('C2.1h input_posted role=copy(源会话出站副本)→显示但不进 waiting(grammar 冻结)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, inputPosted('m1', '副本那条', { delivery: { role: 'copy', copy_kind: 'view_observation' } }));
    await page.waitForTimeout(300);
    assert.strictEqual(await waitingItems(page).count(), 0, 'copy 不进待提交区');
    await context.close();
  });

  // ── C2.4 Hub canonical delivery.v2 兼容门 ──
  await T('C2.4a primary hub_received→进 waiting', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, goldenEvent('primary_feishu'));
    await page.waitForSelector('.waiting-item', { timeout: 5000 });
    assert.strictEqual(await waitingItems(page).count(), 1, 'primary hub_received 入待提交区');
    await context.close();
  });

  await T('C2.4b copy route_observation→显示但绝不 waiting', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, goldenEvent('copy_agent'));
    await page.waitForTimeout(400);
    assert.strictEqual(await waitingItems(page).count(), 0, 'route_observation copy 绝不进 waiting');
    const card = page.locator('.event-card[data-message-id="m-copy"]');
    assert.strictEqual(await card.count(), 1, 'copy 显示为卡');
    const cardText = await card.textContent();
    assert.ok(cardText.includes('Agent·source'), '徽标显示');
    assert.ok(cardText.includes('已同步/镜像'), 'copy 脚注(不追 ACK)');
    await context.close();
  });

  await T('C2.4c delivery_update cli_injected(无 status)→摘 waiting+建时间线卡', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, goldenEvent('primary_feishu'));
    await page.waitForSelector('.waiting-item', { timeout: 5000 });
    await emit(page, goldenEvent('cli_injected'));
    await page.waitForFunction(() => document.querySelectorAll('.waiting-item').length === 0, null, { timeout: 5000 });
    assert.strictEqual(await page.locator('.event-card[data-message-id="m-primary"]').count(), 1, '注入后建时间线卡');
    await context.close();
  });

  await T('C2.4d 三来源徽标不丢(feishu/view/agent)', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, goldenEvent('primary_feishu'));
    await emit(page, goldenEvent('primary_view'));
    await emit(page, goldenEvent('primary_agent'));
    await page.waitForFunction(() => document.querySelectorAll('.waiting-item').length === 3, null, { timeout: 5000 });
    const text = await page.locator('#waitingArea').textContent();
    assert.ok(text.includes('飞书'), 'feishu 徽标');
    assert.ok(text.includes('人工'), 'view 徽标');
    assert.ok(text.includes('Agent·claudek3'), 'agent 徽标带 display_name');
    await context.close();
  });

  await T('C2.4e helper_received 无 status→只推进不摘', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    await emit(page, goldenEvent('primary_feishu'));
    await page.waitForSelector('.waiting-item', { timeout: 5000 });
    for (const ack of ['delivered', 'busy', 'retryable']) {
      await emit(page, { ...goldenEvent('helper_received'), ack });
      await page.waitForTimeout(100);
    }
    assert.strictEqual(await waitingItems(page).count(), 1, 'ACK 三形无 status 均不摘');
    await context.close();
  });

  await T('C2.4f v2 缺 message_id→schema_conflict fail-closed', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    const bad = goldenEvent('primary_feishu');
    delete bad.message_id;
    await emit(page, bad);
    await page.waitForTimeout(300);
    const cards = await page.locator('.event-card').allTextContents();
    assert.ok(cards.some(t => t.includes('消息格式异常') && t.includes('v2_missing_message_id')),
      'v2 U5 fail-closed 可观测');
    assert.strictEqual(await page.locator('.source-badge--trusted').count(), 0, 'conflict 徽标关');
    await context.close();
  });

  await T('C2.4g 同形去 schema_version→legacy 无 v2 conflict', async () => {
    const { context, page } = await bootContext(browser, rendererBody);
    const legacy = goldenEvent('primary_feishu');
    delete legacy.schema_version;
    await emit(page, legacy);
    await page.waitForTimeout(300);
    const cards = await page.locator('.event-card').allTextContents();
    assert.ok(!cards.some(t => t.includes('v2_missing_message_id')), '无版本不触发 v2 专属 conflict');
    await context.close();
  });

  // ── C2.3 scoped base(M1):自定位 /view 前缀全保,六端点同算法 ──
  const SCOPED_FIXTURES = [
    ['F1 /dingwei 前缀+尾斜杠', '/dingwei/view/sess-1/', '/dingwei/view/sess-1/control'],
    ['F2 /dingwei 前缀无尾斜杠', '/dingwei/view/sess-1', '/dingwei/view/sess-1/control'],
    ['F3 /dingwei 前缀带页名', '/dingwei/view/sess-1/renderer', '/dingwei/view/sess-1/control'],
    ['F4 裸 /view', '/view/sess-1/', '/view/sess-1/control'],
  ];
  for (const [name, servePath, expectControl] of SCOPED_FIXTURES) {
    await T(`C2.3 ${name}:真点击删除→POST ${expectControl}+六端点同算法`, async () => {
      const hits = [];
      const { context, page } = await bootContext(browser, rendererBody, { servePath, controlRouteHits: hits, controlReply: { ok: true } });
      const api = await page.evaluate(() => API);
      const base = servePath.replace(/\/(renderer)?$/, '');
      for (const ep of ['events', 'input', 'interrupt', 'control', 'status', 'page']) {
        assert.strictEqual(api[ep], `${base}/${ep}`, `${name} API.${ep} 同算法`);
      }
      assert.strictEqual(api.tasks, '/tasks', 'tasks 全局端点不 scoped');
      await emit(page, inputPosted('m1', '要删的那条'));
      await page.locator('.waiting-cancel-btn').click();
      await waitNote(page, '取消已请求');
      assert.strictEqual(hits.length, 1, '真点击发出 1 次 control');
      await context.close();
    });
  }

  await T('C2.3 F5 根直出回退:base=根相对,control 404→诚实"取消未发出"(G-HONEST 不破)', async () => {
    const { context, page } = await bootContext(browser, rendererBody, { servePath: '/renderer' });
    const api = await page.evaluate(() => API);
    for (const ep of ['events', 'input', 'interrupt', 'control', 'status', 'page']) {
      assert.strictEqual(api[ep], `/${ep}`, `根直出 API.${ep}=/${ep}`);
    }
    await emit(page, inputPosted('m1', '要删的那条'));
    await page.locator('.waiting-cancel-btn').click();
    await waitNote(page, '取消未发出');
    await context.close();
  });


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
