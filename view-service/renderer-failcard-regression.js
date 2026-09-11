// 业主 2026-09-11:「就算他是出错信息,我也不希望看到」
// ⇒ 投递失败不进正文区,改收右侧错误看板。
// 🔴 三格互为护栏:只测「正文没有」会让「把失败整个丢掉」平凡通过。
const { chromium } = require('playwright');
const assert = require('assert');
const base = process.env.RENDERER_URL || 'http://127.0.0.1:19312/renderer-canary-sse.html';
(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.route('**/status', r => r.fulfill({ status:200, contentType:'application/json', body: JSON.stringify({
      session:'f', state:'idle', readonly:true, can_write:false, owner:'f', model:'m', cost_total:0, cost_last:0, turns:0 })}));
    await page.route('**/events', r => r.fulfill({ status:200, contentType:'text/event-stream', body:'' }));
    await page.goto(`${base}?mock=1&unlocked=1`, { waitUntil:'domcontentloaded' });
    await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });

    const snap = () => page.evaluate(() => ({
      nodes: timeline.children.length,
      dock: document.querySelectorAll('#activityList > .activity-card').length,
      dockHidden: workspaceDock.hidden,
    }));

    // 基线:一条成功的输入 ⇒ 正文 1 张,看板空且隐藏
    await page.evaluate(() => handleIncomingEvent({ kind:'input_posted', text:'成功消息',
      message_id:'mOK', delivery_id:'dlv_ok', stage:'cli_injected', cursor:'1' }));
    await page.waitForTimeout(120);
    let s = await snap();
    assert.strictEqual(s.nodes, 1, '基线:成功消息应产 1 张正文卡');
    assert.strictEqual(s.dockHidden, true, '基线:无错误时看板须整体隐藏');

    // ① 失败**不进正文区**
    await page.evaluate(() => handleIncomingEvent({ kind:'input_posted', text:'失败消息',
      message_id:'mBad', delivery_id:'dlv_bad', stage:'failed', data:{ reason:'timeout' }, cursor:'2' }));
    await page.waitForTimeout(150);
    s = await snap();
    assert.strictEqual(s.nodes, 1, '① 失败不得在正文区产卡(仍应只有那 1 张成功卡)');

    // ② 但**必须**收进错误看板 —— 缺这格,「把失败整个丢掉」会让①平凡通过
    assert.strictEqual(s.dock, 1, '② 失败必须进错误看板,不得凭空消失');
    assert.strictEqual(s.dockHidden, false, '② 有错误后看板入口须出现');

    // ③ 失败之后,后续成功消息照常只产 1 张(业主报过的粘连现象的守卫)
    await page.evaluate(() => handleIncomingEvent({ kind:'input_posted', text:'后续成功',
      message_id:'mOK2', delivery_id:'dlv_ok2', stage:'cli_injected', cursor:'3' }));
    await page.waitForTimeout(150);
    s = await snap();
    assert.strictEqual(s.nodes, 2, '③ 失败之后的成功消息仍只产 1 张(不得粘连多产)');
    assert.strictEqual(s.dock, 1, '③ 后续成功不得再往看板里加');

    console.log('failure-card regression: ALL PASS');
  } finally { await browser.close(); }
})();
