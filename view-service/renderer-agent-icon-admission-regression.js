const assert = require('assert');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

const epoch = 'abcdef0123456789';
const trustedCanonical = (id, label = 'qwen38mvb4', provenance = {}) => ({
  kind: 'input_posted', schema_version: 'delivery.v2',
  message_id: `msg-${id}`, delivery_id: `dw-dlv-${id}`, stage: 'hub_received',
  payload: `canonical-${id}`,
  source: { kind: 'agent', display_name: label, trust_decision: 'trusted' },
  delivery: { role: 'primary' },
  provenance: { kind: 'agent', display_name: label, attested_by: 'hub', trust_decision: 'trusted', ...provenance },
  cancellable: true, updated_at: '2026-11-16T07:46:56.841Z',
});
const injected = id => ({
  kind: 'delivery_update', schema_version: 'delivery.v2',
  message_id: `msg-${id}`, observed_delivery_id: `dw-dlv-${id}`,
  delivery_stage: 'cli_injected', turn_id: `turn-${id}`,
  occurred_at: '2026-11-16T07:46:59.102Z', updated_at: '2026-11-16T07:46:59.905Z',
});
const admitted = id => ({
  kind: 'user_input', delivery_id: `dw-dlv-${id}`, origin_delivery_id: `dw-dlv-${id}`,
  turn_id: `turn-${id}`, text: `admitted-${id}`,
  data: { lifecycle: 'admitted', source: 'agent' },
});

async function sendFrame(page, frame, seq) {
  frame.sse_id = `${epoch}-${seq}`;
  frame._hubEpoch = epoch;
  frame._hubSeq = seq;
  await page.evaluate(event => {
    if (typeof setTimelineEventEpoch === 'function') setTimelineEventEpoch(event, `hub:${event._hubEpoch}`);
    handleIncomingEvent(event);
  }, frame);
}

async function boot(browser, baseUrl) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.route('**/status', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
    session: 'fixture-agent-icon-2642', state: 'idle', readonly: true, can_write: false,
    owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0,
  }) }));
  await page.route('**/events', route => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.addInitScript(() => { window.PROVENANCE_ICONS_V1 = true; });
  await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(2300); // let startApp finish its initial history bootstrap
  await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });
  return page;
}

(async () => {
  const fixture = await startRendererServer();
  let browser;
  try {
    const checks = regressionCollector();
    browser = await launchChromium();
    const page = await boot(browser, fixture.url);

    for (const [id, label] of [['agent', 'qwen38mvb4'], ['timer', 'timer']]) {
      await page.evaluate(() => resetConversation());
      await sendFrame(page, trustedCanonical(id, label), 1);
      await sendFrame(page, injected(id), 2);
      await checks.check(`${id}: cli_injected 后 canonical 来源图标仍为一`, async () => {
        assert.strictEqual(await page.locator('.prov-ic').count(), 1,
          `SSE ${epoch}-2: 注入阶段应保留 ${label} 图标`);
      });
      await sendFrame(page, admitted(id), 3);
      await page.waitForTimeout(1000); // live echo merge hold; then inspect the admitted turn card
      await checks.check(`${id}: admitted turn 保留唯一 trusted Agent 来源图标`, async () => {
        assert.strictEqual(await page.locator('.prov-ic').count(), 1,
          `SSE ${epoch}-3: admitted 后来源图标应恰为 1`);
        assert.strictEqual(await page.locator(`.prov-ic[title*="${label}"], .prov-ic[aria-label*="${label}"]`).count(), 1,
          `SSE ${epoch}-3: admitted 后须保留 ${label} 图标`);
        assert.strictEqual(await page.locator('[data-turn-key="turn-' + id + '"]').count(), 1,
          `SSE ${epoch}-3: admitted input 应并入对应 turn`);
        assert.strictEqual(await page.locator('.turn-group[data-turn-key="turn-' + id + '"] .event-content').filter({ hasText: `admitted-${id}` }).count(), 1,
          `SSE id ${epoch}-3: 应显示 admitted echo 正文`);
        assert.strictEqual(await page.locator('.turn-group[data-turn-key="turn-' + id + '"]').getAttribute('data-timeline-seq'), '3',
          `SSE id ${epoch}-3: admitted echo 的 Hub 顺序号应保留`);
      });
    }

    await page.evaluate(() => resetConversation());
    await sendFrame(page, trustedCanonical('history', 'qwen38mvb4'), 1);
    await sendFrame(page, injected('history'), 2);
    await page.evaluate(event => {
      historyApplying = true;
      try { handleIncomingEvent(event); } finally { historyApplying = false; }
    }, { ...admitted('history'), sse_id: `${epoch}-3`, _hubEpoch: epoch, _hubSeq: 3 });
    await checks.check('history replay 合并保留 canonical trusted 图标', async () => {
      assert.strictEqual(await page.locator('.prov-ic').count(), 1,
        `history SSE id ${epoch}-3: 回放 admitted 后应保留唯一来源图标`);
      assert.strictEqual(await page.locator('.turn-group[data-turn-key="turn-history"] .event-content').textContent(), 'admitted-history');
    });

    await page.evaluate(() => resetConversation());
    await sendFrame(page, trustedCanonical('untrusted', 'not-trusted', { trust_decision: 'untrusted' }), 1);
    await sendFrame(page, injected('untrusted'), 2);
    await sendFrame(page, admitted('untrusted'), 3);
    await checks.check('非 trusted canonical 不产生 trusted 图标', async () => {
      assert.strictEqual(await page.locator('.prov-ic').count(), 0, 'untrusted canonical 不得画来源图标');
    });

    await page.evaluate(() => resetConversation());
    const absent = trustedCanonical('absent');
    delete absent.provenance;
    await sendFrame(page, absent, 1);
    await sendFrame(page, injected('absent'), 2);
    await sendFrame(page, admitted('absent'), 3);
    await page.waitForTimeout(1000);
    await checks.check('缺失 canonical provenance 不产生 trusted 图标', async () => {
      assert.strictEqual(await page.locator('.prov-ic').count(), 0, 'absent canonical provenance 不得产生来源图标');
    });

    await page.evaluate(() => resetConversation());
    await sendFrame(page, trustedCanonical('unattested', 'unattested', { attested_by: 'client' }), 1);
    await sendFrame(page, injected('unattested'), 2);
    await sendFrame(page, admitted('unattested'), 3);
    await page.waitForTimeout(1000);
    await checks.check('非 Hub attested canonical 不产生 trusted 图标', async () => {
      assert.strictEqual(await page.locator('.prov-ic').count(), 0, '非 Hub attestation 不得产生来源图标');
    });

    await page.evaluate(() => resetConversation());
    await sendFrame(page, trustedCanonical('wrong-source'), 1);
    await sendFrame(page, injected('wrong-source'), 2);
    const unrelated = admitted('other-delivery');
    unrelated.turn_id = 'turn-other-delivery';
    await sendFrame(page, unrelated, 3);
    await checks.check('不同 delivery 不继承 canonical 图标', async () => {
      assert.strictEqual(await page.locator('.prov-ic').count(), 1,
        '未匹配的 admitted echo 不得新增图标，原 canonical 卡应保留其图标');
    });

    checks.finish();
    console.log('renderer-agent-icon-admission-regression: 9/9 通过（含 qwen、timer、history、SSE id、负向关联）');
    await page.close();
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
