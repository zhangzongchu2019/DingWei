const { chromium } = require('playwright');
const assert = require('assert');
const baseUrl = process.env.RENDERER_URL || 'http://127.0.0.1:19312/renderer-canary-sse.html';

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
      const page = await browser.newPage({ viewport });
      await page.route('**/status', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        session: 'fixture-dev55-2642', state: 'idle', readonly: true, can_write: false,
        owner: 'fixture', model: 'fixture-model', cost_total: 0, cost_last: 0, turns: 0,
      }) }));
      await page.route('**/events', route => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
      await page.goto(`${baseUrl}?mock=1&unlocked=1`, { waitUntil: 'domcontentloaded' });
      await page.evaluate(() => {
        clearInterval(statusTimer);
      });
      assert.strictEqual(await page.locator('#humanInput').isDisabled(), true,
        'URL parameters must not grant write access');
      await page.evaluate(() => {
        resetConversation();
        handleIncomingEvent({ kind: 'user_input', text: '排队中的外部任务\n第二行\n第三行', data: { lifecycle: 'queued' }, delivery_id: 'd-wait', turn_id: '', cursor: '1' });
      });
      assert.strictEqual(await page.locator('#waitingArea').isVisible(), true);
      assert.strictEqual(await page.locator('.waiting-title').count(), 0);
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').count(), 1);
      assert.match(await page.locator('#waitingList').innerText(), /排队中的外部任务/);
      assert.doesNotMatch(await page.locator('#waitingList').innerText(), /等候接纳/);
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').evaluate(node =>
        getComputedStyle(node, '::before').display), 'inline-block');
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').evaluate(node =>
        getComputedStyle(node, '::before').animationName), 'dw-waiting-spin');
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').evaluate(node =>
        getComputedStyle(node).whiteSpace), 'nowrap');
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').evaluate(node =>
        getComputedStyle(node).textOverflow), 'ellipsis');
      assert.ok(await page.locator('#waitingList > .waiting-item').evaluate(node => node.getBoundingClientRect().height < 50));
      await page.evaluate(() => setStatus('running'));
      assert.strictEqual(await page.locator('#taskProgress').isVisible(), true);
      await page.evaluate(() => setStatus('idle'));
      assert.strictEqual(await page.locator('#taskProgress').isVisible(), false);
      await page.evaluate(() => {
        for (let i = 0; i < 20; i += 1) {
          handleIncomingEvent({ kind: 'user_input', text: `排队任务 ${i}`, data: { lifecycle: 'queued' },
            delivery_id: `d-wait-${i}`, turn_id: '', cursor: `1-${i}` });
        }
      });
      await page.waitForTimeout(50);
      assert.strictEqual(await page.locator('#waitingArea').evaluate(node =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1), true);
      await page.evaluate(() => {
        resetConversation();
        handleIncomingEvent({ kind: 'user_input', text: '排队中的外部任务\n第二行\n第三行', data: { lifecycle: 'queued' },
          delivery_id: 'd-wait', turn_id: '', cursor: '1' });
      });
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 0);
      const bottomCluster = await page.evaluate(() => {
        const waiting = document.querySelector('#waitingArea').getBoundingClientRect();
        const composer = document.querySelector('.composer').getBoundingClientRect();
        const timeline = document.querySelector('#timeline').getBoundingClientRect();
        return { waitingBottom: waiting.bottom, composerTop: composer.top, composerBottom: composer.bottom,
          timelineBottom: timeline.bottom, viewportHeight: innerHeight };
      });
      assert.ok(bottomCluster.timelineBottom <= bottomCluster.waitingBottom);
      assert.ok(bottomCluster.waitingBottom <= bottomCluster.composerTop);
      assert.ok(Math.abs(bottomCluster.composerBottom - bottomCluster.viewportHeight) <= 1);
      await page.screenshot({ path: `/tmp/renderer-pw/waiting-bottom-${viewport.width}.png`, fullPage: true });
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'user_input', text: '排队中的外部任务\n第二行\n第三行', data: { lifecycle: 'admitted' }, delivery_id: 'd-wait', turn_id: 't-wait', cursor: '2' });
      });
      assert.strictEqual(await page.locator('#waitingArea').isVisible(), false);
      assert.strictEqual(await page.locator('#waitingList > .waiting-item').count(), 0);
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 1);
      assert.match(await page.locator('#timeline > .turn-group .turn-user').innerText(), /排队中的外部任务/);
      assert.match(await page.locator('#timeline > .turn-group .turn-user').innerText(), /排队中的外部任务\n第二行\n第三行/);
      await page.evaluate(() => {
        resetConversation();
        for (let i = 0; i < 30; i += 1) {
          handleIncomingEvent({ kind: 'user_input', text: `回放输入 ${i}`, delivery_id: `d-scroll-${i}`,
            turn_id: `t-scroll-${i}`, cursor: `${i}.1` });
          handleIncomingEvent({ kind: 'assistant_text', text: `回放回应 ${i}\n附加内容`, delivery_id: `d-scroll-${i}`,
            turn_id: `t-scroll-${i}`, cursor: `${i}.2` });
        }
      });
      await page.waitForTimeout(350);
      assert.strictEqual(await page.locator('#timeline').evaluate(node =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1), true);
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'assistant_text', text: '底部新增消息', delivery_id: 'd-scroll-live',
          turn_id: 't-scroll-live', cursor: 'live.1' });
      });
      await page.waitForTimeout(50);
      assert.strictEqual(await page.locator('#timeline').evaluate(node =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1), true);
      // 🔴 2026-09-11:本格原以「代码直接设 scrollTop」模拟用户上翻。
      // 旧实现分不清 scroll 来源,该代理才成立;修复后代码**按真实手势**判定用户接管
      // (因为浏览器滚动锚定/上限淘汰也会派发 scroll,旧实现把它们误判成用户上翻,
      //  正是刷新不滚底的根因)。故此处补一次**真实 wheel 手势**再设位置:
      // 测试**意图不变**(用户上翻后新消息不得把他拽回底部),只是代理手段升级为真手势。
      // ⚠️ 不能靠删掉这条断言来「修好」—— 那会把「尊重用户上翻」这条行为一起丢掉。
      // ⚠️ 坐标必须落在**当前视口内**:本测试跑 1280 与 390 两档,写死 640 在窄档会落到屏幕外,
      // wheel 根本不派发 —— 插桩实测第二档 sinceGesture 始终未触发。改为取元素中心。
      const tlBox = await page.locator('#timeline').boundingBox();
      await page.mouse.move(tlBox.x + tlBox.width / 2, tlBox.y + tlBox.height / 2);
      await page.mouse.wheel(0, -400);
      // ⚠️ Playwright 的 mouse.wheel **异步**:插桩实测手势事件比调用返回晚约 20ms 到达。
      // 不等一下的话,紧随其后的 scrollTop 赋值所派发的 scroll 会**抢在手势之前**被处理,
      // 于是被正确地判成「非用户滚动」—— 那是修复在起作用,不是修复有问题。
      await page.waitForTimeout(60);
      const scrolledUp = await page.locator('#timeline').evaluate(node => {
        const previous = node.style.scrollBehavior;
        node.style.scrollBehavior = 'auto';
        node.scrollTop = 0;
        node.style.scrollBehavior = previous;
        return node.scrollTop;
      });
      await page.waitForTimeout(50);
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'assistant_text', text: '上翻后新增消息', delivery_id: 'd-scroll-up',
          turn_id: 't-scroll-up', cursor: 'up.1' });
      });
      await page.waitForTimeout(50);
      assert.strictEqual(await page.locator('#timeline').evaluate(node => node.scrollTop), scrolledUp);
      await page.evaluate(() => {
        resetConversation();
        [
          { kind: 'user_input', text: '诊断 Milvus\n第二行参数\n第三行确认', delivery_id: 'd-pw', turn_id: 't-pw', cursor: '10' },
          { kind: 'tool_call', data: { name: 'Read', input: { file_path: '/tmp/a' } }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '20' },
          { kind: 'tool_call', data: { name: 'Bash', input: { command: 'milvus-check --token shencedeepseek-very-long-inline-token-without-breaks-00112233445566778899' } }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '30' },
          { kind: 'assistant_text', text: '诊断完成。', delivery_id: 'd-pw', turn_id: 't-pw', cursor: '40' },
        ].forEach(handleIncomingEvent);
      });
      assert.strictEqual(await page.locator('#timeline > .turn-group').count(), 1);
      assert.strictEqual(await page.locator('#timeline > .activity-card').count(), 0);
      const group = page.locator('#timeline > .turn-group');
      const tools = group.locator('.turn-tools');
      assert.strictEqual(await tools.getAttribute('open'), null);
      assert.strictEqual(await group.locator('.turn-tool-item').first().isVisible(), false);
      await tools.locator(':scope > summary').click();
      assert.notStrictEqual(await tools.getAttribute('open'), null);
      assert.strictEqual(await group.locator('.turn-tool-item').count(), 2);
      assert.match(await group.locator('.turn-tool-item').first().innerText(), /Read/);
      assert.match(await group.locator('.turn-tool-item').last().innerText(), /Bash/);
      assert.match(await group.locator('.turn-user').innerText(), /诊断 Milvus/);
      const multiline = await group.locator('.turn-user .event-content').evaluate(node => {
        const style = getComputedStyle(node);
        return {
          lines: node.innerText.split('\n').length,
          whiteSpace: style.whiteSpace,
          height: node.getBoundingClientRect().height,
          lineHeight: parseFloat(style.lineHeight),
        };
      });
      assert.strictEqual(multiline.lines, 3);
      assert.strictEqual(multiline.whiteSpace, 'pre-wrap');
      assert.ok(multiline.height >= multiline.lineHeight * 2.5, `multiline text did not render as 3 lines: ${JSON.stringify(multiline)}`);
      assert.match(await group.locator('.turn-assistant').innerText(), /诊断完成/);
      await page.evaluate(() => {
        handleIncomingEvent({ kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.1234 }, delivery_id: 'd-pw', turn_id: 't-pw', cursor: '50' });
      });
      assert.strictEqual(await group.locator('.turn-tools').count(), 0);
      // 🔴 2026-09-11 业主:「这里只要给个完成标志即可,不用单写一行文字」。
      // 原断言编码的是即将被去掉的行为,**不是为让测试通过而放宽判据**。
      // 改为两条:①标志仍在(整行删掉会让「结束」与「在跑」无法区分);②费用文字不再出现。
      assert.match(await group.locator('.turn-completed').innerText(), /✓/);
      assert.doesNotMatch(await group.locator('.turn-completed').innerText(), /本回合|累计|\$/);
      assert.deepStrictEqual(await group.locator(':scope > *').evaluateAll(nodes => nodes.map(n => n.className)), [
        'event-body turn-section turn-user',
        'event-body turn-section turn-assistant',
        'turn-completed',
      ]);
      await page.evaluate(() => {
        finishTimelineReplay(false);
        handleIncomingEvent({ kind: 'error', text: '独立错误进入运行动态', data: { code: 'iter2_probe' }, cursor: '51' });
      });
      assert.strictEqual(await page.locator('#timeline > .activity-card').count(), 0, '正文区不得出现运行动态卡');
      assert.strictEqual(await page.locator('#activityList > .activity-card').count(), 1, '错误留在运行动态历史');
      assert.strictEqual(await page.locator('#workspaceDockBody').evaluate(node => node.hidden), true, '错误不自动展开面板');
      assert.strictEqual(await page.locator('#workspaceDockUnread').innerText(), '1', '折叠时累积未读');
      assert.strictEqual(await page.locator('#toastRegion .toast:has-text("独立错误进入运行动态")').count(), 1, '错误只作独立 toast 提醒');
      await page.locator('#workspaceDockToggle').click();
      assert.strictEqual(await page.locator('#workspaceDockBody').evaluate(node => node.hidden), false, '用户可主动展开面板');
      assert.strictEqual(await page.locator('#workspaceDockUnread').evaluate(node => node.hidden), true, '展开后清除未读');
      await page.evaluate(() => {
        resetConversation();
        for (let i = 0; i < 30; i += 1) {
          handleIncomingEvent({ kind: 'assistant_text', text: `持续回放 ${i}\n内容`, delivery_id: `d-live-${i}`,
            turn_id: `t-live-${i}`, cursor: `live.${i}` });
        }
      });
      // 100ms 一条事件持续超过硬截止，包含不渲染进 timeline 的 heartbeat。
      await page.evaluate(() => {
        window.__continuousSeq = 0;
        window.__continuousTimer = setInterval(() => {
          const i = window.__continuousSeq++;
          const ev = i % 2 === 0
            ? { kind: 'state_change', data: { change: 'heartbeat', load: i }, cursor: `heartbeat.${i}` }
            : { kind: 'assistant_text', text: `live ${i}`, delivery_id: `d-cont-${i}`, turn_id: `t-cont-${i}`, cursor: `cont.${i}` };
          handleIncomingEvent(ev);
        }, 100);
      });
      await page.waitForTimeout(1200);
      assert.strictEqual(await page.evaluate(() => replayInProgress), false);
      assert.strictEqual(await page.locator('#timeline').evaluate(node =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1), true);
      // 模拟 Markdown/字体/折叠内容在 append 之后异步增高，ResizeObserver 应补到底。
      await page.evaluate(() => {
        const growth = document.createElement('div');
        growth.style.height = '240px';
        growth.textContent = 'async reflow';
        document.querySelector('#timeline > .turn-group:last-of-type').appendChild(growth);
      });
      await page.waitForTimeout(100);
      assert.strictEqual(await page.locator('#timeline').evaluate(node =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop) <= 1), true);
      const continuousScrollTop = await page.locator('#timeline').evaluate(node => {
        node.style.scrollBehavior = 'auto';
        node.scrollTop = 0;
        node.dispatchEvent(new Event('scroll'));
        return node.scrollTop;
      });
      await page.waitForTimeout(2100);
      assert.strictEqual(await page.evaluate(() => replayInProgress), false);
      assert.strictEqual(await page.locator('#timeline').evaluate(node => node.scrollTop), continuousScrollTop);
      await page.evaluate(() => clearInterval(window.__continuousTimer));
      const layout = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      assert.strictEqual(layout.scrollWidth, layout.clientWidth);
      await page.screenshot({ path: `/tmp/renderer-pw/turn-group-${viewport.width}.png`, fullPage: true });
      await page.close();
    }
    console.log('renderer regression PASS: waiting admission + continuous SSE scroll ownership + async reflow (1280x900, 390x844)');
  } finally {
    await browser.close();
  }
})().catch(err => {
  console.error(err);
  process.exit(1);
});
