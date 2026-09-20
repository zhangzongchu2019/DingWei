// T-054 验收:辅助面板收进右侧边条,只留错误,无错误则不显示。
// 四格判据取自任务板 T-054 verify_level,②③ 必须同时断言 ——
// 只测③会让「把错误也一起去掉」平凡通过。
const assert = require('assert');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

(async () => {
  const fixture = await startRendererServer();
  const baseUrl = fixture.url;
  let browser;
  try {
    browser = await launchChromium();
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const checks = regressionCollector();
    await page.route('**/status', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      session: 'fixture-dev55-2642', state: 'idle', readonly: true, can_write: false,
      owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0,
    }) }));
    await page.route('**/events', route => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
    await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });

    // ③ 无错误时,入口**整体不显示**(不是显示一个空面板)
    await checks.check('③ 无错误时入口整体隐藏', async () => {
      assert.strictEqual(await page.locator('#workspaceDock').evaluate(n => n.hidden), true,
        '③ 无错误时错误看板入口必须整体隐藏');
      assert.strictEqual(await page.locator('#workspaceDock').isVisible(), false, '③ 入口不得可见');
    });

    // ① control / unknown 不得再产生任何卡片(正文区与边条都不得有)
    await page.evaluate(() => {
      handleIncomingEvent({ kind: 'control', control: { kind: 'interrupt' }, text: '探针-control', delivery_id: 'd-c', cursor: '10' });
      handleIncomingEvent({ kind: '这是一个未知类型', text: '探针-unknown', delivery_id: 'd-u', cursor: '11' });
    });
    await checks.check('① control/unknown 不产生卡片', async () => {
      assert.strictEqual(await page.locator('#activityList > .activity-card').count(), 0,
        '① control/unknown 不得进入边条');
      assert.strictEqual(await page.locator('#timeline > .activity-card').count(), 0,
        '① control/unknown 不得进入正文区');
      assert.strictEqual(await page.locator('#workspaceDock').evaluate(n => n.hidden), true,
        '① 只有 control/unknown 时入口仍应隐藏');
    });

    // ② error **必须仍产生卡片** —— 这是唯一不与待提交区重复的一类
    await page.evaluate(() => {
      handleIncomingEvent({ kind: 'error', text: '探针-真错误', data: { code: 't054' }, cursor: '12' });
    });
    await checks.check('② error 仍产生卡片', async () => {
      assert.strictEqual(await page.locator('#activityList > .activity-card').count(), 1,
        '② error 必须仍产生卡片');
    });

    // ④ 出现错误后入口出现,且带未读计数
    await checks.check('④ 错误入口、未读与展开行为', async () => {
      assert.strictEqual(await page.locator('#workspaceDock').evaluate(n => n.hidden), false,
        '④ 出现错误后入口必须出现');
      assert.strictEqual(await page.locator('#workspaceDockUnread').innerText(), '1', '④ 未读计数');
      assert.strictEqual(await page.locator('#workspaceDockBody').evaluate(n => n.hidden), true,
        '④ 不自动展开,仍需用户点开');
      await page.locator('#workspaceDockToggle').click();
      assert.strictEqual(await page.locator('#workspaceDockBody').evaluate(n => n.hidden), false, '④ 可展开');
      assert.strictEqual(await page.locator('#workspaceDockUnread').evaluate(n => n.hidden), true, '④ 展开后清未读');
    });

    // ⑤ 贴右边:与既有任务看板同侧,且确实在视口右缘
    const box = await page.locator('#workspaceDockToggle').boundingBox();
    const vw = await page.evaluate(() => window.innerWidth);
    await checks.check('⑤ 入口贴右边缘', async () => {
      assert.ok(box.x + box.width >= vw - 2, `⑤ 入口须贴右边缘,实测 x+w=${box.x + box.width} vw=${vw}`);
    });

    // ⑥ 重置后回到「无错误则不显示」
    await page.evaluate(() => resetConversation());
    await checks.check('⑥ reset 后隐藏并清空', async () => {
      assert.strictEqual(await page.locator('#workspaceDock').evaluate(n => n.hidden), true,
        '⑥ resetConversation 后须回到隐藏态');
      assert.strictEqual(await page.locator('#activityList > .activity-card').count(), 0, '⑥ 重置须清空错误卡');
    });

    checks.finish();
    console.log('T-054 errorboard regression: ALL PASS (6/6)');
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(finishRegression);
