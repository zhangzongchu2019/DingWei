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
//
// ── 三条用例 ────────────────────────────────────────────────────────────────
//   iframe-① 对账   被服务的 renderer == 仓内那一份 · iframe 铺满 #panes · 📷 可用
//   iframe-② 闸     ㉘ 同形判据,但**参照物换成 iframe 视口**;三档窗口 × 四维度
//   iframe-③ 交叉   🔴 **暗色 × iframe**(补主题交付件"没验到什么"第 6 条:
//                   ㉘ 与 iframe 那件此前都只在**浅色**下验过,两格各自绿、交叉处没人看)。
//                   暗色是一整块只在 [data-theme=dark] 下生效的 CSS ⇒ 只要它里面出现
//                   backdrop-filter/transform/filter/will-change/contain 之一,包含块就
//                   **只在暗色下**被改掉:浅色的闸全绿,业主一切到暗色就复发。
//                   主题切换走**业主的真实路径**(点 console 顶层 ◐,经 BroadcastChannel/
//                   storage 传到 pane),**不手设 data-theme** —— 手设量到的是"我给它上了
//                   暗色",不是"业主点了开关之后"。
//
// 🔴 iframe-③ 的取数方法(两种,别混):
//   · **实测像素**:遮罩是 rgba(0,0,0,.55) 的半透明层,computed style 只会说"0.55 的黑",
//     不会说它压在暗色页面上**最终成了什么颜色** —— 而那才是提示文字的真实底色。
//     故截 1x1 的图、再把图喂回浏览器解码取像素(不在 Node 侧手写 PNG 解码器)。
//     自证:浅色档实测 [111,111,112] ≈ 0.45×246(页面 bg)⇒ 采样与混合模型对得上。
//   · **computed style**:提示文字色、按钮前景/背景都是**不透明**值 ⇒ 它即真值,不必截图。
//   · 采样点由 pickScrimPoint() 在"遮罩内、画布与工具条外"里挑;挑不到就报守卫 G3,
//     并且若读到夹具填充色 #3366cc 也报 G3(说明落点掉进画布了)⇒ **不硬凑一个点**。
//   · 🔵 有两项**只打印不断言**:「遮罩 vs 页面背景」的对比度(暗色下必然低 —— 产品从没
//     给"变暗提示该多强"定过规格,断言它就是替产品立契约;实测数字交给组长/业主判断)
//     与遮罩的 computed 背景值(实测恒为 rgba(0,0,0,0.55) ⇒ **硬编码、不随主题变**)。
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

// ── WCAG 相对亮度 / 对比度(与 console-theme-global-regression.js 同一公式)──────────
function luminance(rgb) {
  const [r, g, b] = rgb.map((n) => {
    const c = Number(n) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function contrastRatio(a, b) {
  const l1 = luminance(a);
  const l2 = luminance(b);
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}
const parseRgb = (s) => { const m = String(s).match(/[\d.]+/g); return m ? m.slice(0, 3).map(Number) : null; };
const r2 = (n) => Math.round(n * 100) / 100;

// 🔴 遮罩底色必须【实测像素】,不能读 computed style:
//    `.shot-overlay{background:rgba(0,0,0,.55)}` 是**半透明**层,computed style 只会告诉你
//    "0.55 的黑",不会告诉你它压在暗色页面上**最终成了什么颜色** —— 而那才是提示文字的真实底色。
//    做法:截 1x1 的图,再把这张图喂回浏览器解码取像素(不在 Node 侧手写 PNG 解码器)。
async function samplePixel(page, x, y) {
  const png = await page.screenshot({ clip: { x: Math.round(x), y: Math.round(y), width: 1, height: 1 } });
  return page.evaluate(async (b64) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const c = document.createElement('canvas');
    c.width = img.naturalWidth; c.height = img.naturalHeight;
    const g = c.getContext('2d');
    g.drawImage(img, 0, 0);
    const d = g.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2]];
  }, png.toString('base64'));
}

// 在遮罩内、画布与工具条**之外**挑一个采样点(拿不到就返回 null ⇒ 由守卫报出来,不硬凑)
function pickScrimPoint(m) {
  const o = m.overlay;
  const inside = (x, y, r, pad) => x >= r.left - pad && x <= r.right + pad && y >= r.top - pad && y <= r.bottom + pad;
  const cands = [
    [o.left + 4, o.bottom - 4], [o.right - 4, o.bottom - 4], [o.left + o.width / 2, o.bottom - 4],
    [o.left + 4, m.bar.bottom + 8], [o.right - 4, m.bar.bottom + 8],
  ];
  for (const [x, y] of cands) {
    if (!inside(x, y, o, 0)) continue;
    if (inside(x, y, m.canvas, 2)) continue;
    if (inside(x, y, m.bar, 2)) continue;
    return [x, y];
  }
  return null;
}

let pass = 0;
let fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass += 1; } catch (e) { console.log('  🔴', name, '->', e && e.message); fail += 1; }
}

// 帧源(与 iframe-② 同一手法):走既有测试缝注入一张纯色画布,本条量的是**遮罩几何与观感**,
// 不是真 getDisplayMedia 的内容 —— 那条缺口在交付件里另有登记,不在这里冒充。
const INSTALL_SHOT_SOURCE = ([W, H]) => {
  window.__dwSetScreenshotFrameSource(() => {
    const c = document.createElement('canvas');
    c.width = W; c.height = H;
    const g = c.getContext('2d');
    g.fillStyle = '#3366cc'; g.fillRect(0, 0, W, H);
    return Promise.resolve(c);
  });
};

// 遮罩打开状态下的一次量测。全部在 pane 文档内取,坐标相对 **iframe 视口**。
const MEASURE = () => {
  const box = (el) => {
    const r = el.getBoundingClientRect();
    return { left: +r.left.toFixed(2), top: +r.top.toFixed(2), right: +r.right.toFixed(2),
             bottom: +r.bottom.toFixed(2), width: +r.width.toFixed(2), height: +r.height.toFixed(2) };
  };
  const cs = (el, p) => getComputedStyle(el)[p];
  const ov = document.getElementById('shotOverlay');
  const bar = document.querySelector('.shot-bar');
  const chain = [];
  let e = ov.parentElement;
  while (e && e.tagName !== 'HTML') { chain.push(e.tagName.toLowerCase() + (e.id ? `#${e.id}` : '')); e = e.parentElement; }
  const hit = document.elementsFromPoint(window.innerWidth / 2, window.innerHeight / 2);
  const btns = ['shotConfirm', 'shotCancel'].map((id) => {
    const el = document.getElementById(id);
    return { id, color: cs(el, 'color'), bg: cs(el, 'backgroundColor'),
             border: cs(el, 'borderTopColor'), box: box(el) };
  });
  return {
    theme: document.body.dataset.theme,
    bodyBg: cs(document.body, 'backgroundColor'),
    iframeViewport: [window.innerWidth, window.innerHeight],
    overlay: box(ov), canvas: box(document.getElementById('shotCanvas')), bar: box(bar), chain,
    overlayBg: cs(ov, 'backgroundColor'),
    hintColor: cs(document.getElementById('shotHint'), 'color'),
    btns,
    hitInOverlay: hit.some((el) => el === ov || ov.contains(el)),
    hitTop: hit.length ? (hit[0].id || String(hit[0].className) || hit[0].tagName) : 'none',
  };
};

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

    // ── iframe-③ 暗色 × iframe 交叉(主题件"没验到什么"第 6 条:㉘ 与 iframe 那件都只在**浅色**下验过)──
    //
    // 🔴 为什么必须交叉跑,而不是"两边都绿了所以交叉也绿":
    //    遮罩修复的正确性依赖「没有祖先成为 fixed 的包含块」。而暗色是一整块
    //    **只在 [data-theme=dark] 下生效**的 CSS(console_auth.go:277-290 /
    //    renderer-canary-sse.html:26-42)⇒ 只要暗色块里出现 backdrop-filter / transform /
    //    filter / will-change / contain 中任意一个,包含块就**只在暗色下**被改掉:
    //    浅色的闸全绿、业主一切到暗色就复发。**这正是本组反复撞的形状:两格各自绿,交叉处没人看。**
    // 🔵 主题切换走**业主的真实路径**:点 console 顶层那个 ◐(#themeBtn),经 BroadcastChannel /
    //    storage 传到 pane(renderer:4135 落在 body.dataset.theme)。**不手设 data-theme** ——
    //    手设量到的是"我给它上了暗色",不是"业主点了开关之后"。
    await t('iframe-③ 暗色交叉:console 顶层 ◐ 切暗后,真 iframe 内遮罩仍铺满 iframe 视口,且遮罩文字/按钮仍可读', async () => {
      const violations = [];
      const seen = [];
      for (const vp of WINDOWS) {
        const { context, page, frame } = await openConsoleWithAgent(vp);
        try {
          await frame.evaluate(INSTALL_SHOT_SOURCE, [800, 600]);
          const iframeBox = await page.evaluate(() => {
            const r = document.querySelector('#panes iframe').getBoundingClientRect();
            return { left: r.left, top: r.top };
          });
          // 开遮罩 → 量 → 关遮罩(浅色 / 暗色各一次,**同一个文档内**对照)
          const shoot = async () => {
            await frame.click('#shotBtn');
            await frame.waitForSelector('#shotOverlay:not([hidden])', { timeout: 20000 });
            const m = await frame.evaluate(MEASURE);
            const pt = pickScrimPoint(m);
            const scrim = pt ? await samplePixel(page, iframeBox.left + pt[0], iframeBox.top + pt[1]) : null;
            await frame.click('#shotCancel');
            // 🔴 踩坑记(本件自己踩的):waitForSelector 缺省 state='visible',
            //    而"遮罩已关"恰恰意味着它**不可见** ⇒ 用 `#shotOverlay[hidden]` 等可见会永远等不到
            //    (实测:44 次重试后超时,失败信息看起来像"取消按钮没生效")。要等的是 state='hidden'。
            //    与板件 0.2.1.35-⑱(elementHandle 默认自动等待 30s)同一族:**API 的缺省等待语义要先想清楚**。
            await frame.waitForSelector('#shotOverlay', { state: 'hidden', timeout: 20000 });
            return { m, pt, scrim };
          };
          const light = await shoot();
          await page.click('#themeBtn');
          // 🔴 必须等它真的传到 pane:BroadcastChannel / storage 都是异步的,
          //    不等就会量到"开关点了、pane 还是浅色"的那一瞬 —— 那正是本条要抓的缺陷。
          await frame.waitForFunction(() => document.body.dataset.theme === 'dark', null, { timeout: 20000 });
          const dark = await shoot();

          const dm = dark.m;
          const tag = `${vp.width}x${vp.height}(iframe 视口 ${dm.iframeViewport[0]}x${dm.iframeViewport[1]})· 暗色`;
          const T = 1;   // 亚像素容差:只吸收取整,吸收不了"只盖页脚"这种量级
          const lightBg = parseRgb(light.m.bodyBg);
          const darkBg = parseRgb(dm.bodyBg);

          // 守卫 G1/G2:必须**真的**暗了,否则本条量的是浅色,绿得毫无意义
          if (dm.theme !== 'dark') {
            violations.push(`守卫G1 ${tag}: pane 的 body[data-theme]=${dm.theme} 不是 dark ⇒ 本条量到的是浅色`);
          }
          if (!lightBg || !darkBg) {
            violations.push(`守卫G2 ${tag}: body 背景色读不出来(light=${light.m.bodyBg} dark=${dm.bodyBg})`);
          } else if (!(luminance(darkBg) < luminance(lightBg) - 0.05)) {
            violations.push(`守卫G2 ${tag}: 切暗后 body 背景亮度没有显著下降`
              + `(light ${light.m.bodyBg} L=${r2(luminance(lightBg))} → dark ${dm.bodyBg} L=${r2(luminance(darkBg))})`
              + ` ⇒ 主题没作用到这个文档,本条是空转`);
          }
          // 守卫 G3:采样点必须落在**纯遮罩**上(不是画布、不是工具条)
          if (!dark.pt || !dark.scrim) {
            violations.push(`守卫G3 ${tag}: 遮罩内找不到画布/工具条之外的采样点,或截图采样失败 ⇒ 对比度失去落点`);
          } else if (dark.scrim[0] === 51 && dark.scrim[1] === 102 && dark.scrim[2] === 204) {
            violations.push(`守卫G3 ${tag}: 采样点落在画布上(读到夹具填充色 #3366cc)⇒ 落点逻辑坏了`);
          }

          // 几何四查(与 iframe-② 同形,参照物 = **iframe 视口**)
          if (!(Math.abs(dm.overlay.left) <= T && Math.abs(dm.overlay.top) <= T
                && Math.abs(dm.overlay.width - dm.iframeViewport[0]) <= T
                && Math.abs(dm.overlay.height - dm.iframeViewport[1]) <= T)) {
            violations.push(`${tag}: 遮罩 rect=${JSON.stringify(dm.overlay)} ≠ iframe 视口 ${dm.iframeViewport[0]}x${dm.iframeViewport[1]}`);
          }
          if (dm.chain.join(',') !== 'body') {
            violations.push(`${tag}: 遮罩父链是 ${dm.chain.join('<')} 而不是 body ⇒ 暗色下包含块又被谁改了`);
          }
          if (!(dm.canvas.top >= dm.overlay.top - T && dm.canvas.bottom <= dm.overlay.bottom + T
                && dm.canvas.left >= dm.overlay.left - T && dm.canvas.right <= dm.overlay.right + T)) {
            violations.push(`${tag}: 画布 ${JSON.stringify(dm.canvas)} 未被遮罩完整包住`);
          }
          if (!dm.hitInOverlay) {
            violations.push(`${tag}: iframe 视口中心命中的是 ${JSON.stringify(dm.hitTop)} 而不是遮罩`);
          }

          // 可读性:提示文字压在**实测**遮罩底色上(半透明层,computed style 给不出真实底色)
          let hintCr = null;
          if (dark.scrim) {
            const hintRgb = parseRgb(dm.hintColor);
            hintCr = hintRgb ? r2(contrastRatio(hintRgb, dark.scrim)) : null;
            if (hintCr === null) violations.push(`${tag}: 提示文字颜色读不出来(${dm.hintColor})`);
            else if (hintCr < 4.5) violations.push(`${tag}: 提示文字 ${dm.hintColor} 压在实测遮罩底色 rgb(${dark.scrim.join(',')}) 上,对比度 ${hintCr} < 4.5(WCAG AA 正文)`);
          }
          // 按钮:前景/背景都是不透明的 var(--panel)/var(--text) ⇒ computed style 即真值
          const btnCr = [];
          for (const b of dm.btns) {
            const fg = parseRgb(b.color);
            const bg = parseRgb(b.bg);
            if (!fg || !bg) { violations.push(`${tag}: 按钮 #${b.id} 的颜色读不出来(color=${b.color} bg=${b.bg})`); continue; }
            const cr = r2(contrastRatio(fg, bg));
            btnCr.push(`#${b.id}=${cr}`);
            if (cr < 4.5) violations.push(`${tag}: 按钮 #${b.id} 文字 ${b.color} / 底 ${b.bg} 对比度 ${cr} < 4.5(WCAG AA)`);
          }

          seen.push({
            窗口: `${vp.width}x${vp.height}`, iframe视口: dm.iframeViewport, 父链: dm.chain.join('<'),
            浅色: { bodyBg: light.m.bodyBg, 遮罩computed: light.m.overlayBg, 实测遮罩底色: light.scrim,
                    提示色: light.m.hintColor, 提示对比度: light.scrim ? r2(contrastRatio(parseRgb(light.m.hintColor), light.scrim)) : null,
                    遮罩vs页面: light.scrim ? r2(contrastRatio(light.scrim, parseRgb(light.m.bodyBg))) : null },
            暗色: { bodyBg: dm.bodyBg, 遮罩computed: dm.overlayBg, 实测遮罩底色: dark.scrim,
                    提示色: dm.hintColor, 提示对比度: hintCr,
                    遮罩vs页面: dark.scrim ? r2(contrastRatio(dark.scrim, darkBg)) : null,
                    按钮对比度: btnCr.join(' ') },
            遮罩rect: dm.overlay, 画布rect: dm.canvas, 中心命中: dm.hitTop,
          });
        } finally {
          await context.close();
        }
      }
      console.log('    · iframe-③ 实测(浅色 / 暗色 同文档对照):', JSON.stringify(seen, null, 1));
      console.log('    · 🔵 只打印不断言的两项:「遮罩vs页面」是**变暗提示的强度**(暗色下必然低,'
        + ' 因为遮罩是 rgba(0,0,0,.55) 压在 #111316 上;产品没有给这个值定过规格 ⇒ 断言它就是替产品立契约);'
        + ' 「遮罩computed」恒为 rgba(0,0,0,0.55) ⇒ 它是硬编码的,**不随主题变** —— 这条也是实测出来的,不是读码。');
      assert.deepStrictEqual(violations, [],
        `🔴 暗色 × 真 console iframe 交叉跑不成立(共 ${violations.length} 项):\n       - ${violations.join('\n       - ')}`);
    });
  } finally {
    await browser.close();
    await new Promise((resolve) => server.close(resolve));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}   (真 console iframe 路径)`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
