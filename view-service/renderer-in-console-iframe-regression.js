// 🔴 在【真 console 页的真 iframe 内】验截屏遮罩 —— 因为业主实际走的就是这条路。
//
// 为什么必须单独立这一件(组长 ts-No.4 判为**阻断项**,不是备查):
//   `position:fixed` 在 iframe 内相对的是【iframe 自己的视口】,不是浏览器窗口
//   ⇒ "遮罩铺满视口"在**独立 renderer 页**与**console iframe**两条路上根本不是同一件事。
//   业主走的是 console 那条(internal/m8/console_auth.go:306 建 iframe,src='/view/'+session),
//   独立 renderer 页他从不走 ⇒ 只在独立页上验收 = **在用户不走的那条路上验收**。
//   🔴 推论等价 ≠ 实测等价。本件就是把那条推论换成实测。
//
// 装置沿用 view-service/console-browser-regression.js(C2)的手法:
//   console 页**从生产源码里取**(console_auth.go 的 consolePageHTML),不复制一份;
//   只替换它的 HTTP 依赖(/api/console/agents、/api/console/platform、/view/*)。
//   🔴 与 C2 那件的关键区别:这里 `/view/*` **不返回占位页,而返回仓内真实的
//      renderer-canary-sse.html** —— 否则验的不是业主看到的那一份。
//   🔴 另一处必须注意:iframe 内 renderer 的 `status` / `events` 是**相对路径**,
//      会被解析成 `/view/<session>/status`(实测踩过:只挂 `/status` 会 404 ⇒
//      stats.readonly 恒为 true ⇒ 📷 点不动,而失败信息看起来像"页面没加载好")。
//
// 环境变量:
//   RENDERER_OVERRIDE=<path>  换一份 renderer 字节来跑 —— 专为"修复前必红"的
//                             **阳性对照**留的口子(缺省 = 仓内那一份,用例① 会对账)
//   PW_CHANNEL=chrome         用系统 Chrome(缺省用 Playwright 自带 chromium)
const assert = require('assert');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const GO_SOURCE = path.join(__dirname, '..', 'internal', 'm8', 'console_auth.go');
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(__dirname, 'renderer-canary-sse.html');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const SESSION = 'owner-alpha-2642';
// 三档浏览器窗口:两档高度(证明与高度无关)＋ 一档窄窗口。
// 🔵 窄窗口那一档量出来的是**console 自己的**问题(nav 是固定 240px 列,无响应式规则)
//    ⇒ 420 宽的面板只剩 180px。本件**如实打印**,不替它找补、也不在这里改它。
const WINDOWS = [{ width: 1280, height: 900 }, { width: 1280, height: 1600 }, { width: 420, height: 900 }];

function consoleHTMLFromProductionSource() {
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML 起始标记不见了 —— 生产源码结构变了,先修装置');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML 结束标记不见了 —— 生产源码结构变了,先修装置');
  return source.slice(contentStart, end);
}

const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

let pass = 0;
let fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; } catch (e) { console.log('  🔴', name, '->', e && e.message); fail += 1; }
}

(async () => {
  const consoleHTML = consoleHTMLFromProductionSource();
  const rendererBytes = fs.readFileSync(RENDERER);
  const repoRendererBytes = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'));
  console.log(`  · console 页取自生产源码 internal/m8/console_auth.go(${consoleHTML.length} 字节)`);
  console.log(`  · renderer  ${RENDERER}`);
  console.log(`    sha256=${sha256(rendererBytes).slice(0, 16)}… ${rendererBytes.length} 字节`
    + (process.env.RENDERER_OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这是【阳性对照】跑,不是仓内那一份' : ''));

  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    if (u === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(consoleHTML);
    }
    if (u === '/api/console/agents') return json({ agents: [{ session: SESSION, online: true }] });
    if (u === '/api/console/platform') return json({ role: 'owner', actions: ['read', 'submit'], items: [] });
    if (u === `/view/${encodeURIComponent(SESSION)}`) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(rendererBytes);
    }
    // 🔴 相对路径:iframe 内的 renderer 会请求 /view/<session>/status 与 /events
    if (u.endsWith('/status')) return json({ session: SESSION, state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' });
    if (u.endsWith('/events')) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', 'Connection': 'keep-alive' });
      res.write(': open\n\n');
      return;   // 🔴 不 end:保持长连接,否则 renderer 会进断线态、盖一层告警干扰量测
    }
    res.writeHead(404, { 'Content-Type': 'text/plain' });
    res.end('not found');
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  const base = `http://127.0.0.1:${port}`;

  const launchOptions = { headless: true };
  if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOptions);

  // 打开真 console → 点在线 Agent → 拿到 iframe 的 frame 对象
  const openConsoleWithAgent = async (vp) => {
    const context = await browser.newContext({ viewport: vp });
    const page = await context.newPage();
    page.on('pageerror', (e) => console.log(`    [pageerror ${vp.width}x${vp.height}]`, String(e.message).slice(0, 200)));
    await page.goto(`${base}/console`, { waitUntil: 'domcontentloaded' });
    await page.locator(`nav button[title="${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
    await page.click(`nav button[title="${SESSION}"]`);
    await page.locator(`#panes iframe[title="Agent view: ${SESSION}"]`).waitFor({ state: 'attached', timeout: 20000 });
    const frame = page.frames().find((f) => f.url().includes(`/view/${encodeURIComponent(SESSION)}`));
    assert.ok(frame, '没找到 /view/<session> 那个 iframe 的 frame 对象');
    await frame.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 30000 });
    return { context, page, frame };
  };

  try {
    await t('iframe-① 真 console 开 Agent tab,iframe 内跑的是【仓内那一份】renderer(不是占位页)', async () => {
      const { context, page, frame } = await openConsoleWithAgent(WINDOWS[0]);
      try {
        // 对账:被服务的 renderer 必须就是仓内这一份(RENDERER_OVERRIDE 时例外,且已在开头大声标注)
        if (!process.env.RENDERER_OVERRIDE) {
          assert.strictEqual(sha256(rendererBytes), sha256(repoRendererBytes),
            '被服务的 renderer 与仓内 view-service/renderer-canary-sse.html 不是同一份字节');
        }
        const served = await frame.content();
        assert.ok(served.includes('shotOverlay') && served.includes('__dwSetScreenshotFrameSource'),
          'iframe 内不是本版 renderer(缺 shotOverlay / 测试缝标记)⇒ 验的不是业主看到的那一份');
        // 面板确实就是那个 iframe:量它 == 量 #panes(否则"铺满 iframe 视口"这句话没有落点)
        const boxes = await page.evaluate(() => {
          const b = (el) => { const r = el.getBoundingClientRect(); return [r.left, r.top, r.width, r.height].map((n) => +n.toFixed(1)); };
          return { panes: b(document.getElementById('panes')), iframe: b(document.querySelector('#panes iframe')) };
        });
        assert.deepStrictEqual(boxes.iframe, boxes.panes,
          `iframe 没有铺满 #panes(panes=${JSON.stringify(boxes.panes)} iframe=${JSON.stringify(boxes.iframe)})`);
        const st = await frame.evaluate(() => ({ readonly: stats.readonly, session: stats.sessionName,
          shotBtn: !!document.getElementById('shotBtn') }));
        assert.strictEqual(st.readonly, false, `iframe 内会话应为可写,实测 ${JSON.stringify(st)}`);
        assert.strictEqual(st.session, SESSION, `iframe 内 sessionName 应为 ${SESSION},实测 ${JSON.stringify(st.session)}`);
        assert.strictEqual(st.shotBtn, true, 'iframe 内应有 📷 截屏入口');
      } finally {
        await context.close();
      }
    });

    await t('iframe-② 🔴 遮罩必须铺满【iframe 自己的视口】—— 三档浏览器窗口各验一次', async () => {
      // 🔴 判据与独立页那条(㉘)同形,但**参照物换了**:这里比的是 iframe 的 innerWidth/innerHeight,
      //    不是浏览器窗口的。二者在 console 里差得很远(窗口 1280 宽 ⇒ 面板只有 1040 宽)。
      // 🔵 只验"铺满"会漏掉"铺满了但画布仍被裁切"与"铺满了但被别的层压在下面",故三维度一起验。
      const violations = [];
      const seen = [];
      for (const vp of WINDOWS) {
        const { context, page, frame } = await openConsoleWithAgent(vp);
        try {
          await frame.evaluate(([W, H]) => {
            window.__dwSetScreenshotFrameSource(() => {
              const c = document.createElement('canvas');
              c.width = W; c.height = H;
              const g = c.getContext('2d');
              g.fillStyle = '#3366cc'; g.fillRect(0, 0, W, H);
              return Promise.resolve(c);
            });
          }, [800, 600]);
          await frame.click('#shotBtn');
          await frame.waitForSelector('#shotOverlay:not([hidden])', { timeout: 20000 });
          const m = await frame.evaluate(() => {
            const box = (el) => {
              const r = el.getBoundingClientRect();
              return { left: +r.left.toFixed(2), top: +r.top.toFixed(2), right: +r.right.toFixed(2),
                       bottom: +r.bottom.toFixed(2), width: +r.width.toFixed(2), height: +r.height.toFixed(2) };
            };
            const ov = document.getElementById('shotOverlay');
            const hit = document.elementsFromPoint(window.innerWidth / 2, window.innerHeight / 2);
            const chain = [];
            let e = ov.parentElement;
            while (e && e.tagName !== 'HTML') { chain.push(e.tagName.toLowerCase() + (e.id ? `#${e.id}` : '')); e = e.parentElement; }
            return {
              iframeViewport: [window.innerWidth, window.innerHeight],
              overlay: box(ov), canvas: box(document.getElementById('shotCanvas')), chain,
              hitInOverlay: hit.some((el) => el === ov || ov.contains(el)),
              hitTop: hit.length ? (hit[0].id || String(hit[0].className) || hit[0].tagName) : 'none',
            };
          });
          // 顶层:遮罩开着的时候,console 自己的 chrome 还在不在(iframe 盖不到自己外面)
          const chrome = await page.evaluate(() => {
            const nav = document.querySelector('nav').getBoundingClientRect();
            const els = document.elementsFromPoint(nav.left + nav.width / 2, nav.top + 40);
            return els.length ? (els[0].id || String(els[0].className) || els[0].tagName) : 'none';
          });
          seen.push({ win: `${vp.width}x${vp.height}`, iframeViewport: m.iframeViewport,
                      ovTop: m.overlay.top, ovH: m.overlay.height, cvBottom: m.canvas.bottom,
                      hit: m.hitInOverlay, chain: m.chain.join('<'), consoleNavTopElement: chrome });

          const T = 1;   // 亚像素容差:只吸收取整,吸收不了"只盖页脚"这种量级
          const tag = `${vp.width}x${vp.height}(iframe 视口 ${m.iframeViewport[0]}x${m.iframeViewport[1]})`;
          if (!(Math.abs(m.overlay.left) <= T && Math.abs(m.overlay.top) <= T
                && Math.abs(m.overlay.width - m.iframeViewport[0]) <= T
                && Math.abs(m.overlay.height - m.iframeViewport[1]) <= T)) {
            violations.push(`${tag}: 遮罩 rect=${JSON.stringify(m.overlay)} ≠ iframe 视口 ${m.iframeViewport[0]}x${m.iframeViewport[1]}`);
          }
          if (m.chain.join(',') !== 'body') {
            violations.push(`${tag}: 遮罩父链是 ${m.chain.join('<')} 而不是 body ⇒ 包含块又被谁改了`);
          }
          if (!(m.canvas.top >= m.overlay.top - T && m.canvas.bottom <= m.overlay.bottom + T
                && m.canvas.left >= m.overlay.left - T && m.canvas.right <= m.overlay.right + T)) {
            violations.push(`${tag}: 画布 ${JSON.stringify(m.canvas)} 未被遮罩完整包住 ⇒ 面板内仍需滚动才能框选`);
          }
          if (!m.hitInOverlay) {
            violations.push(`${tag}: iframe 视口中心命中的是 ${JSON.stringify(m.hitTop)} 而不是遮罩`);
          }
          await frame.click('#shotCancel');
        } finally {
          await context.close();
        }
      }
      // 🔵 实测值一律打出来:红的时候看得到红在哪,绿的时候也不采信自报值
      console.log('    · iframe-② 实测:', JSON.stringify(seen, null, 1));
      console.log('    · 🔴 如实报(不断言):遮罩开着时 console 自己的 nav 仍是最上层命中元素 ——'
        + ' iframe 盖不到自己外面,这是架构固有边界,不是本次修复能解决的。'
        + ' 不断言它,因为断言等于把缺陷钉成契约、将来真要修就会红。');
      assert.deepStrictEqual(violations, [],
        `🔴 真 console 的 iframe 内,遮罩没有铺满 iframe 视口(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}   (真 console iframe 路径)`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
