// G9(下半)· 业主 2026-09-28 逐字「这类消息可以发到错误消息栏」/ 09-29「传到 view 页错误提示区」
// ⇒ Hub 自发的那份 kind:'error' 事件**必须真的落进错误看板**,而不是被早退进回合正文。
//
// 🔴 本闸喂的事件**不是在这里写的**,是**读**出来的:
//      view-service/fixtures-failure-dock/hub-failure-dock-event.json
//    那一份是 Hub 侧 publishFailureDockEvent 的逐字节产出,由 Go 侧
//    TestFailureDockEventMatchesRendererFixture 钉住(漂移即红)。
//    ⇒ **一份形状、两个消费方**;这里再抄一份就是"两份清单各说各话"。
//
// 🔴 腿三是**渲染器侧的阳性对照**:同一个事件**只加一个 delivery_id**,
//    就必须**不再进看板**(被 :3114 早退进回合正文)。少了这一腿,
//    "进看板"可能只是因为它本来什么都会收 —— 那就是断在一个看不见变异的量上。
//
// 🔵 与 renderer-failcard-regression.js(业主 09-11「就算他是出错信息,我也不希望看到」)**不冲突**:
//    那份治的是 input_posted + stage:'failed'(正文区**已有一张卡**)⇒ 状态写回原卡、不另起卡;
//    本份治的是**正文区本来没有卡**的失败通知。两个闸都必须绿。
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const { finishRegression, launchChromium, regressionCollector, startRendererServer } = require('./renderer-regression-selfhost');

const fixturePath = path.join(__dirname, 'fixtures-failure-dock', 'hub-failure-dock-event.json');
const fixtureRaw = fs.readFileSync(fixturePath, 'utf8').trim();
const fixture = JSON.parse(fixtureRaw);

(async () => {
  const server = await startRendererServer();
  const base = server.url;
  let browser;
  try {
    browser = await launchChromium();
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    const checks = regressionCollector();
    await page.route('**/status', r => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
      session: 'dockfx', state: 'idle', readonly: true, can_write: false, owner: 'dockfx', model: 'm', cost_total: 0, cost_last: 0, turns: 0 }) }));
    await page.route('**/events', r => r.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
    await page.goto(`${base}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
    await page.evaluate(() => { clearInterval(statusTimer); resetConversation(); finishTimelineReplay(false); });

    const snap = () => page.evaluate(() => ({
      nodes: timeline.children.length,
      dock: document.querySelectorAll('#activityList > .activity-card').length,
      dockText: Array.from(document.querySelectorAll('#activityList > .activity-card')).map(n => n.textContent).join(' | '),
      dockHidden: workspaceDock.hidden,
      unread: workspaceDockUnread.hidden ? '' : workspaceDockUnread.textContent,
      toasts: Array.from(document.querySelectorAll('.toast')).map(n => n.textContent),
    }));

    // 腿一 · 装置自证:一开始看板是空的、隐藏的,正文区也是空的。
    let s = await snap();
    await checks.check('腿一 装置自证:初始无卡、看板隐藏', async () => {
      assert.strictEqual(s.nodes, 0, `初始正文区应为空,实为 ${s.nodes} 张`);
      assert.strictEqual(s.dock, 0, `初始看板应为空,实为 ${s.dock} 张`);
      assert.strictEqual(s.dockHidden, true, '初始看板入口应隐藏');
    });

    // 腿二 · 🔴 主判据:Hub 那份逐字节夹具 ⇒ 落进错误看板,且不进正文区。
    await page.evaluate(raw => handleIncomingEvent(JSON.parse(raw)), fixtureRaw);
    await page.waitForTimeout(160);
    s = await snap();
    await checks.check('腿二 Hub 夹具事件落进错误看板、不进正文区', async () => {
      assert.strictEqual(s.dock, 1, `看板应恰好 1 张卡,实为 ${s.dock}(正文区 ${s.nodes} 张)`);
      assert.strictEqual(s.nodes, 0, `🔴 不得进正文区(业主 09-11 不想要多出的正文卡),实为 ${s.nodes} 张`);
      assert.ok(s.dockText.includes('目标会话无法补全为在线 canonical 地址'),
        `🔴 看板卡必须有【内容】(DEFECT 件记的「只有数字、内容为空」就是这个形状),实为 ${JSON.stringify(s.dockText)}`);
      assert.ok(s.dockText.includes('#beta'), `看板卡应带目标写法,实为 ${JSON.stringify(s.dockText)}`);
      assert.strictEqual(s.dockHidden, false, '出现错误后看板入口必须出现(T-054)');
      assert.strictEqual(s.unread, '1', `未读角标应为 1,实为 ${JSON.stringify(s.unread)}`);
      assert.ok(s.toasts.some(t => t.includes('目标会话无法补全')), `应有错误 toast,实为 ${JSON.stringify(s.toasts)}`);
    });

    // 腿三 · 🔴 渲染器侧阳性对照:同一个事件**只加一个 delivery_id** ⇒ 不再进看板。
    const withKey = Object.assign({}, fixture, { delivery_id: 'dlv-would-break-dock' });
    await page.evaluate(ev => handleIncomingEvent(ev), withKey);
    await page.waitForTimeout(160);
    const after = await snap();
    await checks.check('腿三 阳性对照:加了 delivery_id 就进不了看板(被早退进回合正文)', async () => {
      assert.strictEqual(after.dock, 1,
        `🔴 阳性对照不成立:加了 delivery_id 之后看板仍是 ${after.dock} 张(应为 1,即**没有**新增)` +
        ` ⇒ 说明"进看板"与那个键无关,腿二证不了任何东西`);
      assert.ok(after.nodes >= 1,
        `加了 delivery_id 的那份应被 :3114 早退进【回合正文】,正文区应 ≥1 张,实为 ${after.nodes}`);
    });

    checks.finish();
    console.log('failure-dock regression: ALL PASS (3/3)');
  } finally { if (browser) await browser.close(); await server.close(); }
})().catch(finishRegression);
