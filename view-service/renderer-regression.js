// 🔴 2026-09-20(㉒ 同格,组长 ts-No.22「顺手把端口这条治本」):本件原先**硬编码依赖 :19312 的外部静态服务**
//    (`RENDERER_URL` 的缺省值就指它)。两个后果,今晚都实测踩过:
//      · 服务不在 ⇒ `page.goto` 抛 `ERR_CONNECTION_REFUSED`、**未捕获异常 rc=1** ⇒
//        从 rc 上看不出是"环境缺失"还是"断言失败"(正是 ⑭ 要治的形状);
//      · :19312 是**共享端口** ⇒ 谁先起谁绊倒后面的人(`EADDRINUSE`),而"记得先起服务"正是本组在治的病。
//    ⇒ 改成**自启自收、自挑空闲端口**(形状照 `coder56solzzc` 那四件与 `renderer-sse-reconnect.js` 的 `startFixture`)。
//    🔵 rc 同时对齐发布约定:**0 全过 / 2 断言失败 / 3 环境不可用**(原先无论什么原因一律 1)。
//    🔵 env 面:只读**已登记**的 `PW_CHANNEL`;**不再读 `RENDERER_URL`**(未登记面,跑者的机械扫描会拒)。
//    🔵 合并注记:`coder56solzzc/browser-selfhost-02135` 上有共享件 `renderer-regression-selfhost.js`
//       (`startRendererServer` / `launchChromium` / `regressionCollector` / `finishRegression`)。
//       🔴 **它不在本件的基线上** ⇒ 直接 require 会 MODULE_NOT_FOUND(= rc=3),所以这里内联等价的一小段。
//       两条线合并后**应换成那个 helper**,别再实现第三次。
const fs = require('fs');
const http = require('http');
const path = require('path');
const assert = require('assert');

const SHOTS = '/tmp/renderer-pw';

class EnvironmentUnavailable extends Error {}

function chromiumForRegression() {
  try { return require('playwright').chromium; }
  catch (err) { throw new EnvironmentUnavailable(`Playwright 不可用: ${err.message}`); }
}

async function launchChromium() {
  const chromium = chromiumForRegression();
  try {
    const options = { headless: true };
    if (process.env.PW_CHANNEL) options.channel = process.env.PW_CHANNEL;   // 已登记的 env 面
    return await chromium.launch(options);
  } catch (err) {
    if (/executable doesn't exist|browser.*not found|failed to launch/i.test(err.message || '')) {
      throw new EnvironmentUnavailable(`Chromium 不可用: ${err.message}`);
    }
    throw err;
  }
}

// 自起一个**只**服务仓内那一份 renderer 的 HTTP 服务:端口 0(自挑空闲)、cwd 无关、退出必收。
// 🔵 不用 `python3 -m http.server`:那会把整个目录暴露出去,而且多一个进程要收。
async function startRendererServer() {
  const rendererPath = path.join(__dirname, 'renderer-canary-sse.html');
  let renderer;
  try { renderer = fs.readFileSync(rendererPath); }
  catch (err) { throw new EnvironmentUnavailable(`renderer 件读不到(${rendererPath}): ${err.message}`); }
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://renderer.test').pathname;
    if (pathname !== '/renderer-canary-sse.html') {
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('not found');
      return;
    }
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
    res.end(renderer);
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  }).catch((err) => { throw new EnvironmentUnavailable(`静态服务起不来: ${err.message}`); });
  const port = server.address().port;
  const crypto = require('crypto');
  console.log(`  · 自起静态服务 http://127.0.0.1:${port}/renderer-canary-sse.html(自挑端口 · 退出必收 · 不再依赖 :19312)`);
  console.log(`    renderer sha256=${crypto.createHash('sha256').update(renderer).digest('hex').slice(0, 16)}…`);
  return { url: `http://127.0.0.1:${port}/renderer-canary-sse.html`, close: () => new Promise((resolve) => server.close(resolve)) };
}

(async () => {
  const fixture = await startRendererServer();
  const baseUrl = fixture.url;
  fs.mkdirSync(SHOTS, { recursive: true });
  let browser;
  try {
    browser = await launchChromium();
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
      await page.screenshot({ path: path.join(SHOTS, `waiting-bottom-${viewport.width}.png`), fullPage: true });
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
      // 🔴 2026-09-20(㉒):本格的**代理过期了**,与上面那格 2026-09-11 修掉的那个同源,只是这一格漏了。
      //    产品自 09-11 起只认「**紧随真实手势** 400ms 内、且离底 >160px」的 scroll 为"用户接管"
      //    (`renderer-canary-sse.html:1658` / `:1659` / `:1691`,锚 `260ad413`);
      //    而这里只设 `scrollTop` + 派发**合成** scroll,**没有任何真手势** ⇒ 产品按设计不交还跟随权
      //    ⇒ 连续流把人带回底部 ⇒ 本格长期红(实测 `3164 !== 0`,rc=1)。
      //    🔵 定界七格实测(探针未入仓):真 wheel 上翻后连续流 2.1s,scrollTop **纹丝不动**(1234 → 1234,
      //       `timelineAtBottom` 第 1 个采样就交还并全程保持);同一条腿**去掉手势**必被拽到底(0 → 2503)。
      //       ⇒ 🔴 红的是代理,不是产品。
      //    ⇒ 修法与上面那格一致:**补一次真 wheel 手势**,断言原样保留
      //       (那格的注释也写着"⚠️ 不能靠删掉这条断言来「修好」")。
      //    ⚠️ 坐标必须落在**当前视口内**(本件跑 1280 与 390 两档)⇒ 取元素中心,不写死。
      //    ⚠️ Playwright 的 `mouse.wheel` **异步**(实测比调用返回晚约 20ms)⇒ 等一下再置位,
      //       否则置位派发的 scroll 会抢在手势之前被处理,被正确地判成"非用户滚动"。
      const continuousBox = await page.locator('#timeline').boundingBox();
      await page.mouse.move(continuousBox.x + continuousBox.width / 2, continuousBox.y + continuousBox.height / 2);
      await page.mouse.wheel(0, -400);
      await page.waitForTimeout(60);
      const continuousScrollTop = await page.locator('#timeline').evaluate(node => {
        const previous = node.style.scrollBehavior;
        node.style.scrollBehavior = 'auto';
        node.scrollTop = 0;
        node.style.scrollBehavior = previous;
        return node.scrollTop;
      });
      await page.waitForTimeout(2100);
      assert.strictEqual(await page.evaluate(() => replayInProgress), false);
      // 🔴 断言比原来**多一条**:位置没被拽走之外,跟随权也不得被悄悄夺回
      //    (`scrollTimelineToBottom()` 结尾会把 `timelineAtBottom` 置真 ⇒ 位置这一帧没动,
      //     下一条内容照样会把人带到底)。这正是 D1 的形状,不能只量位置。
      const continuousEnd = await page.locator('#timeline').evaluate(node => ({
        st: node.scrollTop,
        gap: node.scrollHeight - node.clientHeight - node.scrollTop,
        atBottom: timelineAtBottom,
      }));
      assert.strictEqual(continuousEnd.st, continuousScrollTop,
        `用户上翻后被拽回底部:scrollTop ${continuousScrollTop} → ${continuousEnd.st}`
        + `(离底 ${continuousEnd.gap}px · timelineAtBottom=${continuousEnd.atBottom})⇒ 连续流期间产品夺回了滚动控制权`);
      assert.strictEqual(continuousEnd.atBottom, false,
        `跟随权被重新夺回(timelineAtBottom=true,离底 ${continuousEnd.gap}px)`
        + ` ⇒ 即使这一帧位置没动,后续内容仍会把用户带到底`);
      await page.evaluate(() => clearInterval(window.__continuousTimer));
      const layout = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      assert.strictEqual(layout.scrollWidth, layout.clientWidth);
      await page.screenshot({ path: path.join(SHOTS, `turn-group-${viewport.width}.png`), fullPage: true });
      await page.close();
    }
    console.log('renderer regression PASS: waiting admission + continuous SSE scroll ownership + async reflow (1280x900, 390x844)');
  } finally {
    if (browser) await browser.close();
    await fixture.close();
  }
})().catch(err => {
  // rc 约定:0 全过 / 2 断言失败 / 3 环境不可用(原先一律 1 ⇒ 从 rc 上分不清是哪种)
  if (err instanceof EnvironmentUnavailable) {
    console.error(`ENVIRONMENT_UNAVAILABLE: ${err.message}`);
    process.exit(3);
  }
  console.error(err && (err.stack || err.message) || err);
  process.exit(2);
});
