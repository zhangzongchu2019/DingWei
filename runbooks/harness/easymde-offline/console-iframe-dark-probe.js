// 🔴 ts-No.78 追加腿(组长裁定 (3):**必须补,不是可选**)—— 同一套对比度量测,搬到【真 console 的 iframe 内】
//
// 组长判据(逐字):「**业主走的就是 console 那条路**」;
//                  「🔴 在业主真实走的那条路上没验过的绿,我从现在起一律不收」。
// 为什么推论不算数:`dark-contrast-probe.js`(D0–D8)量的是**独立页 + `data-theme` 挂 `<body>`** 的形状;
//   console 那条路上主题挂在**外层文档的 `<html>`**(`internal/m8/console_auth.go:337` 写 `documentElement.dataset.theme`,
//   CSS 是 `html[data-theme="dark"]{color-scheme:dark}` 等,`:277`),编辑器在 **iframe 里**,
//   而 iframe 内的 renderer 是**自己**把主题落在 `document.body.dataset.theme`(`renderer-canary-sse.html:4172` applyTheme)。
//   ⇒ 🔴 "外层 html 的 color-scheme 会不会穿过 iframe 边界"是一个**只能实测**的问题(推论等价 ≠ 实测等价)。
//
// 装置手法**沿用** `view-service/renderer-in-console-iframe-regression.js`(不另发明):
//   · console 页**从生产源码里取**(`console_auth.go` 的 `consolePageHTML`,起止标记守卫),不复制一份;
//   · `/view/<session>` 返回**仓内真实的** renderer 字节(sha 对账),只**注入**候选编辑器(注入点唯一性守卫);
//   · 主题切换走**业主的真实路径**:点 console 顶层那个 ◐(`#themeBtn`),经 BroadcastChannel / storage 落到 pane。
//     🔴 **不手设 `data-theme`** —— 手设量到的是"我给它上了暗色",不是"业主点了开关之后"。
//     🔵 而且这条不是靠我保证:C1 里有一条**装置自证**,它会 grep 本文件,一旦发现对 `data-theme` 的**写**就判红。
//
// 与 `dark-contrast-probe.js` 的关系:🔴 **同一把尺子、同一份样本、同一套行集合** —— 都来自 `measure-core.js`
//   (门槛、维度归属、`buildRows`、`MUT_DARK110`、页内量测函数)⇒ 两条路径量的是**同一件事**,数字可逐项对照(C4)。
//
// 门槛的**适用对象**按组长裁定 (1):信息维判定、装饰维只报数,判定行同时打印本仓同尺参照值(都写在 measure-core 里)。
//
// rc:0 全过 · 2 有格红 · 🔴 3 装置自证不过(console 抽不到 / renderer 对不上账 / 编辑器没起来 / 隔离不成立)⇒ 中止
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const crypto = require('crypto');
const { chromium } = require('playwright');
const { requireVendor, PINS_MAIN, PINS_FA, sha256 } = require('./vendor-pins');
const {
  TH_TEXT, TH_UI, DIM_INFO, DIM_DECO, ratio, R, rgb, px, repoRef,
  MD_SAMPLE, TOOLBAR, MEASURE_EVAL, MUT_DARK110, buildRows, verdictSet
} = require('./measure-core');

const HERE = __dirname;
const OUT = path.join(HERE, 'out');
const PROXY = 'http://127.0.0.1:9';
const REPO = path.resolve(HERE, '../../..');
const GO_SOURCE = path.join(REPO, 'internal', 'm8', 'console_auth.go');
const RENDERER = path.join(REPO, 'view-service', 'renderer-canary-sse.html');
const START = 'const consolePageHTML = `';
const END = '`\n\nfunc (h *Hub) HandleConsoleAgents';
const SESSION = 'owner-alpha-2642';
const VIEW = `/view/${encodeURIComponent(SESSION)}`;
const WAIT = 30000;
const MOUNT = 'dwProbeMount';
const sha256buf = (b) => crypto.createHash('sha256').update(b).digest('hex');

// ── console 页:从生产源码抽(与 renderer-in-console-iframe-regression.js 同一手法)──
function consoleHTML() {
  if (!fs.existsSync(GO_SOURCE)) { console.error('🔴 生产源码不在:' + GO_SOURCE + ' ⇒ rc=3(路径/锚点过期)'); process.exit(3); }
  const source = fs.readFileSync(GO_SOURCE, 'utf8');
  const start = source.indexOf(START);
  assert.notStrictEqual(start, -1, 'consolePageHTML 起始标记不见了 —— 生产源码结构变了,先修装置');
  const contentStart = start + START.length;
  const end = source.indexOf(END, contentStart);
  assert.notStrictEqual(end, -1, 'consolePageHTML 结束标记不见了 —— 生产源码结构变了,先修装置');
  const html = source.slice(contentStart, end);
  // 🔴 形状守卫:抽出来的东西必须**真的是**那个 console 页(否则后面量的都是别的页面)
  const need = ['id=themeBtn', 'id=panes', "html[data-theme=\"dark\"]", 'BroadcastChannel'];
  const miss = need.filter((k) => !html.includes(k));
  assert.deepStrictEqual(miss, [], `🔴 console 页形状变了:缺 ${miss.join(' , ')} ⇒ 先修装置,别拿旧形状报数`);
  return { html, sha: sha256buf(Buffer.from(html, 'utf8')), bytes: html.length };
}

// ── renderer:仓内真字节 + **只注入**候选编辑器 ────────────────────────────────
function rendererBundle() {
  if (!fs.existsSync(RENDERER)) { console.error('🔴 产品件不在:' + RENDERER + ' ⇒ rc=3'); process.exit(3); }
  const base = fs.readFileSync(RENDERER, 'utf8');
  const marker = '</body>';
  const n = base.split(marker).length - 1;
  assert.strictEqual(n, 1, `🔴 产品件里 \`${marker}\` 不是恰好 1 处(实测 ${n})⇒ 注入点不唯一,别猜`);
  const inject = [
    `<!-- 🔴 ts-No.78 追加腿的**装置注入**(不是产品件的一部分):把候选编辑器挂进真 renderer 文档 -->`,
    '<link rel="stylesheet" href="/vendor/easymde.min.css">',
    '<link rel="stylesheet" href="/vendor/fa.min.css">',
    `<div id="${MOUNT}" style="padding:16px"><textarea id="dwProbeMd"></textarea></div>`,
    '<script src="/vendor/easymde.min.js"></script>',
    '<script>',
    '(function(){',
    //  🔴 __dwTheme 做成**只读 getter**,读的是产品自己写在 body 上的那个值 ——
    //     装置不去"告诉"量测函数现在是什么主题,免得量到的是我设的而不是产品设的。
    '  Object.defineProperty(window, "__dwTheme", { configurable: true, get: function(){',
    '    return document.body ? (document.body.dataset.theme || null) : null; } });',
    '  window.__dwPcss = "real-product-css";',
    '  try {',
    '    window.__dwEditor = new EasyMDE({',
    '      element: document.getElementById("dwProbeMd"),',
    '      spellChecker: false,                 // 引入硬条件①',
    '      autoDownloadFontAwesome: false,      // 引入硬条件①(FA 本地自托管 = 硬条件②)',
    '      toolbar: ' + TOOLBAR + ',',
    '      initialValue: ' + JSON.stringify(MD_SAMPLE) + ',',
    '      minHeight: "200px"',
    '    });',
    '    window.__dwEditorReady = true;',
    '  } catch (e) { window.__dwEditorError = String((e && e.message) || e); }',
    '})();',
    '</script>'
  ].join('\n');
  const html = base.replace(marker, inject + '\n' + marker);
  assert.ok(html.includes(MOUNT) && html.includes('__dwEditorReady'), '装置:注入没成功');
  return {
    html,
    baseSha: sha256(RENDERER),
    injectedSha: sha256buf(Buffer.from(html, 'utf8')),
    baseBytes: base.length,
    injectedBytes: html.length
  };
}

function startServer(bundle, cons) {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/console') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(cons.html);
    }
    if (u === '/api/console/agents') return json({ agents: [{ session: SESSION, online: true }] });
    if (u === '/api/console/platform') return json({ role: 'owner', actions: ['read', 'submit'], items: [] });
    if (u === VIEW) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(bundle.html);
    }
    // 🔴 相对路径:iframe 内的 renderer 会请求 /view/<session>/status 与 /events(踩过:只挂 /status 会 404)
    if (u.endsWith('/status')) return json({ session: SESSION, state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' });
    if (u.endsWith('/events')) {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store', 'Connection': 'keep-alive' });
      res.write(': open\n\n');
      return;   // 🔴 不 end:保持长连接,否则 renderer 会进断线态、盖一层告警干扰量测
    }
    if (u.startsWith('/vendor/')) {
      const f = path.join(HERE, 'vendor', u.slice('/vendor/'.length));
      if (!f.startsWith(path.join(HERE, 'vendor')) || !fs.existsSync(f)) { res.writeHead(404); return res.end('nf'); }
      const ct = f.endsWith('.css') ? 'text/css' : f.endsWith('.js') ? 'text/javascript'
        : f.endsWith('.woff2') ? 'font/woff2' : 'application/octet-stream';
      res.writeHead(200, { 'Content-Type': ct, 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(f));
    }
    res.writeHead(404); res.end('nf');
  });
  return new Promise((r) => server.listen(0, '127.0.0.1', () => r({ server, port: server.address().port })));
}

const classify = (url, port) => (url.startsWith(`http://127.0.0.1:${port}`) || url.startsWith(`http://localhost:${port}`)) ? 'local'
  : (/^(data|blob|about|filesystem):/.test(url) ? 'inline' : 'external');

async function newPage(browser, port) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
  const page = await ctx.newPage();
  const rec = { cdpExt: [], pwExt: [], pageErrors: [] };
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.requestWillBeSent', (e) => { if (classify(e.request.url, port) === 'external') rec.cdpExt.push(e.request.url); });
  ctx.on('request', (r) => { if (classify(r.url(), port) === 'external') rec.pwExt.push(r.url()); });
  page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 140)));
  return { ctx, page, rec };
}

// 打开真 console → 点在线 Agent → 拿到 iframe 的 frame 对象(与既有那件同一手法)
async function openConsoleWithAgent(browser, port) {
  const { ctx, page, rec } = await newPage(browser, port);
  await page.goto(`http://127.0.0.1:${port}/console`, { waitUntil: 'domcontentloaded', timeout: WAIT });
  await page.locator(`nav button[title="${SESSION}"]`).waitFor({ state: 'attached', timeout: WAIT });
  await page.click(`nav button[title="${SESSION}"]`);
  await page.locator(`#panes iframe[title="Agent view: ${SESSION}"]`).waitFor({ state: 'attached', timeout: WAIT });
  const frame = page.frames().find((f) => f.url().includes(VIEW));
  assert.ok(frame, `没找到 ${VIEW} 那个 iframe 的 frame 对象`);
  await frame.waitForFunction('window.__dwEditorReady === true', null, { timeout: WAIT });
  return { ctx, page, frame, rec };
}

// 同一份字节的**独立页**形状(= D0–D8 那件量的形状),用来做 C4 的对照
async function openStandalone(browser, port) {
  const { ctx, page, rec } = await newPage(browser, port);
  await page.goto(`http://127.0.0.1:${port}${VIEW}`, { waitUntil: 'load', timeout: WAIT });
  await page.waitForFunction('window.__dwEditorReady === true', null, { timeout: WAIT });
  return { ctx, page, rec };
}

// ── 一次量测:真事件 + buildRows(共用判据)+ 像素层 ─────────────────────────
async function measureIn(t, page, tag, rec) {
  // 🔴 先把鼠标移到**中性位置**再取样:上一腿结束时鼠标可能还停在某颗工具栏钮上(真点击留下的),
  //    而"工具栏图标(常态)"取的是**当刻**的计算样式 ⇒ 不复位就会把 **hover 态读成常态**。
  //    🔵 这不是假设:第一跑 C5 的"常态"量到 1.026,而 C2/C3 同一行是 18.608 —— 差的正是 bold 上残留的 hover。
  //    (dark-contrast-probe 每腿开新 context,鼠标从未动过 ⇒ 那边不会撞上;这里 C2/C5 复用同一文档。)
  await page.mouse.move(5, 5);
  await page.waitForTimeout(200);
  const m = await t.evaluate(MEASURE_EVAL);
  assert.deepStrictEqual(rec.cdpExt, [], `🔴 断网下仍有外部请求(CDP):${JSON.stringify(rec.cdpExt)}`);
  const readBtn = (sel) => t.evaluate(`(function(){
    var b=document.querySelector(${JSON.stringify(sel)}); if(!b) return null;
    var i=b.querySelector('i')||b, cs=getComputedStyle(i), cb=getComputedStyle(b);
    return { icon: cs.color, bg: cb.backgroundColor, border: cb.borderTopColor, cls: b.className,
             rect:(function(r){return {w:Math.round(r.width),h:Math.round(r.height)};})(b.getBoundingClientRect()) };
  })()`);
  await t.hover('.editor-toolbar button.italic');                 // 真鼠标移入 ⇒ 产品自己的 :hover
  const hov = await readBtn('.editor-toolbar button.italic');
  const pngHov = path.join(OUT, `${tag}-btn-hover.png`);
  await (await t.$('.editor-toolbar button.italic')).screenshot({ path: pngHov });
  const pxHov = px(pngHov);
  await page.mouse.move(5, 5);                                    // 移开,避免污染下一步
  await t.click('.editor-toolbar button.bold');                   // 真点击 ⇒ 产品自己 toggle .active
  const actReal = await readBtn('.editor-toolbar button.bold');
  const pngAct = path.join(OUT, `${tag}-btn-active.png`);
  await (await t.$('.editor-toolbar button.bold')).screenshot({ path: pngAct });
  const pxBtn = px(pngAct);
  assert.ok(actReal && /active/.test(actReal.cls),
    `守卫:真点击之后产品**没有**给按钮加 .active(实测 class="${actReal && actReal.cls}")⇒ 量不到激活态,别拿合成态充数`);

  const built = buildRows(m, hov, actReal);                        // 🔴 行集合/维度/守卫都在 measure-core
  const actSyn = await t.evaluate(`(function(){
    var b=document.querySelector('.editor-toolbar button.quote'); b.classList.add('active');
    var i=b.querySelector('i')||b; return { icon: getComputedStyle(i).color, bg: getComputedStyle(b).backgroundColor };
  })()`);
  const synRatio = ratio(rgb(actSyn.icon).v, rgb(actSyn.bg).v);
  const realRatio = ratio(rgb(actReal.icon).v, rgb(actReal.bg).v);
  assert.ok(Math.abs(synRatio - realRatio) < 0.01,
    `🔴 合成态与真事件不一致(合成 ${R(synRatio)} vs 真点击 ${R(realRatio)})⇒ 有一边量的不是同一个东西,先查装置`);

  const pngCM = path.join(OUT, `${tag}-codemirror.png`);
  await (await t.$('.EasyMDEContainer .CodeMirror')).screenshot({ path: pngCM });
  const pxCm = px(pngCM);
  const pngTB = path.join(OUT, `${tag}-toolbar.png`);
  await (await t.$('.editor-toolbar')).screenshot({ path: pngTB });
  const pxTb = px(pngTB);

  const inFrame = await t.evaluate('window.self !== window.top');
  const out = Object.assign({ tag, m, inFrame, hov, actReal, actSyn, pxCm, pxTb, pxBtn, pxHov, rec }, built);
  console.log(`\n     【${tag}】theme(body)=${m.theme} · --bg ${m.vars['--bg']} · --text ${m.vars['--text']} · --line ${m.vars['--line']}`);
  console.log(`     编辑器 bg=${m.els.codeMirror.bg} color=${m.els.codeMirror.color} · 工具栏 bg=${m.els.toolbar.bg}(来自 ${m.els.toolbar.bgFrom}) · 图标 color=${m.els.toolbarIcon.color}`);
  console.log(`     color-scheme 链:iframe html=${m.els.body.htmlColorScheme} · body=${m.els.body.colorScheme} · 编辑器 rect=${JSON.stringify(m.els.codeMirror.rect)}`);
  out.rows.forEach((r) => console.log('     ' + (r.text || `     🔵 ${r.label} = ${r.value}(只报数)`)));
  console.log(`     像素层 编辑区:bg ${JSON.stringify(pxCm.bg)} 最暗 ${JSON.stringify(pxCm.darkest)} 最亮 ${JSON.stringify(pxCm.lightest)} ⇒ contrast_max ${pxCm.contrast_max}`);
  console.log(`     像素层 真 hover 钮 contrast_max ${pxHov.contrast_max} · 真点击 .active 钮 ${pxBtn.contrast_max}`);
  const belowInfo = out.rows.filter((r) => r.ok === false);
  const belowDeco = out.rows.filter((r) => r.dim === DIM_DECO && r.value < r.th);
  console.log(`     🔴 **信息维**低于门槛 ${belowInfo.length} 项(判缺陷):${belowInfo.map((r) => `${r.label} ${r.value}`).join(' · ') || '(无)'}`);
  console.log(`     🔵 **装饰维**低于门槛 ${belowDeco.length} 项(按裁定 (1) 不判缺陷):${belowDeco.map((r) => `${r.label} ${r.value}`).join(' · ') || '(无)'} · 本仓同尺参照 ${out.ref}`);
  console.log(`     🔵 最坏的 3 个 token 色:${out.tokenRows.slice(0, 3).map((x) => `${x.color}→${R(x.ratio)}`).join(' · ')}`
    + ` · 继承 pre 色的 span ${m.inherit.inheriting}/${m.inherit.spans}`);
  return out;
}

(async () => {
  requireVendor(Object.assign({}, PINS_MAIN, PINS_FA), 'ts-No.78 追加腿 · console iframe 暗色');
  fs.mkdirSync(OUT, { recursive: true });
  const cons = consoleHTML();
  const bundle = rendererBundle();

  console.log('ts-No.78 追加腿 · EasyMDE 暗色对比度**在真 console 的 iframe 内**(裁定 (3):业主走的就是这条路)');
  console.log(`  console 页  取自 internal/m8/console_auth.go 的 consolePageHTML · sha256=${cons.sha.slice(0, 16)}… · ${cons.bytes} B`);
  console.log(`  renderer    ${path.relative(REPO, RENDERER)} · sha256=${bundle.baseSha.slice(0, 16)}… · ${bundle.baseBytes} B`);
  console.log(`              🔴 服务端返回的是**注入编辑器之后**的字节 sha256=${bundle.injectedSha.slice(0, 16)}… · ${bundle.injectedBytes} B(注入点唯一性已守卫)`);
  console.log(`  门槛        WCAG AA:信息维 正文 ${TH_TEXT} / UI ${TH_UI};🔴 装饰维**只报数不判缺陷**(裁定 (1))`);
  console.log(`  隔离        ${PROXY}(死端口代理,浏览器层 —— 与 ts-No.77/78 同一条边界)`);

  const { server, port } = await startServer(bundle, cons);
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox', `--proxy-server=${PROXY}`] });
  } catch (e) { console.error('环境不可用:' + e.message.split('\n')[0]); process.exit(3); }

  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 12).join('\n  ')}`); }
  };

  let cons0 = null, iframeDark = null, standaloneDark = null;
  try {
    // ── C0 装置自证 ────────────────────────────────────────────────────────
    await t('C0 🔴 装置自证:console 页从生产源码抽到 + renderer 对账 + 编辑器真在 iframe 内 + 隔离成立', async () => {
      const { ctx, page, frame, rec } = await openConsoleWithAgent(browser, port);
      cons0 = { ctx, page, frame, rec };
      try {
        const probe = await frame.evaluate(`(async function(){
          var out={local:null,ext:{},editorHere:null,editorInTop:null,theme:null};
          try{ var r=await fetch('/ping'); out.local='OK '+r.status; }catch(e){ out.local='FAILED '+e.name; }
          for (const [k,u] of Object.entries({jsdelivr:'https://cdn.jsdelivr.net/codemirror.spell-checker/latest/en_US.aff',
                                              bootstrapcdn:'https://maxcdn.bootstrapcdn.com/font-awesome/latest/css/font-awesome.min.css',
                                              example:'https://example.com/'})) {
            try{ var rr=await fetch(u,{mode:'no-cors'}); out.ext[k]='REACHED '+rr.status; }catch(e2){ out.ext[k]='BLOCKED '+e2.name; }
          }
          out.editorHere = !!document.querySelector('.EasyMDEContainer .CodeMirror');
          out.theme = document.body.dataset.theme || null;
          return out; })()`);
        const topEditor = await page.evaluate(`!!document.querySelector('.EasyMDEContainer .CodeMirror')`);
        console.log(`\n     iframe 内探针 ${JSON.stringify(probe)}`);
        console.log(`     外层 console 文档里有编辑器吗:${topEditor}(必须是 false —— 编辑器在 pane 里,不在外壳里)`);
        assert.ok(/^OK 200$/.test(probe.local), `隔离把本地也断了(${probe.local})⇒ 夹具不可用`);
        const leaked = Object.keys(probe.ext).filter((k) => !/^BLOCKED/.test(probe.ext[k]));
        assert.deepStrictEqual(leaked, [], `🔴 隔离不成立:${leaked.map((k) => k + '=' + probe.ext[k]).join(' | ')} ⇒ 中止`);
        assert.strictEqual(probe.editorHere, true, 'iframe 内没有编辑器 ⇒ 注入没生效,这一格是空的');
        assert.strictEqual(topEditor, false, '外层文档里也有编辑器 ⇒ 我量的可能不是 pane 里那一个');
        assert.strictEqual(probe.theme, 'light', `起点不是浅色(实测 ${probe.theme})⇒ C1 证明不了"点击让它变暗"`);
        console.log(`     🔵 页面报错(不判红,只登记)${rec.pageErrors.length} 条:${JSON.stringify(rec.pageErrors.slice(0, 3))}`);
        // 🔴 C0 那几条外部请求是**探针自己故意发的**(为了证明隔离成立:必须全 BLOCKED),不是被测件发的。
        //    而 C2/C5 复用同一个 context ⇒ 不划掉就会被读成"编辑器出网了"(一条**假失败**,
        //    而假失败会吃掉同套件后面所有断言的执行 —— 本组在 E′ 那条上吃过这个亏)。
        //    🔵 划掉**必须打印条数**,不许静默清账;而且只有"已经逐条核过是 BLOCKED"之后才划。
        const nCdp = rec.cdpExt.length, nPw = rec.pwExt.length;
        rec.cdpExt.length = 0; rec.pwExt.length = 0;
        rec.selftestExt = { cdp: nCdp, pw: nPw };
        console.log(`     🔵 划掉 C0 自证**故意**发的外部请求:CDP ${nCdp} 条 / PW ${nPw} 条(上面已逐条核为 BLOCKED)`);
      } catch (e) { await ctx.close().catch(() => {}); throw e; }
    });
    if (fail) { console.log('\n🔴 装置自证没过 ⇒ 中止,不产出任何对比度结论'); await browser.close(); await new Promise((r) => server.close(r)); process.exit(3); }

    // ── C1 业主的真实路径:点 console 顶层 ◐ ───────────────────────────────
    await t('C1 🔴 主题走**业主的真实路径**:点 console 顶层 ◐ ⇒ 外层 `html[data-theme=dark]` 且 iframe 内 body 跟着暗', async () => {
      const { ctx, page, frame, rec } = cons0;
      // 🔴 装置自证:本文件里**不许**出现对 data-theme 的写(正则里的反斜杠让它不会匹配到自己)
      const selfSrc = fs.readFileSync(__filename, 'utf8');
      const writes = selfSrc.match(new RegExp('dataset\\.theme\\s*=[^=]|setAttribute\\(\\s*[\'"]data-theme', 'g')) || [];
      assert.deepStrictEqual(writes, [],
        `🔴 装置自证失败:本文件里出现了对 data-theme 的**写**(${JSON.stringify(writes)})⇒ "业主点了开关"这句话就不成立`);
      const before = {
        outer: await page.evaluate('document.documentElement.getAttribute("data-theme")'),
        outerCs: await page.evaluate('getComputedStyle(document.documentElement).colorScheme'),
        inner: await frame.evaluate('document.body.dataset.theme || null'),
        innerCs: await frame.evaluate('getComputedStyle(document.body).colorScheme')
      };
      console.log(`\n     点击前:外层 html[data-theme]=${before.outer} color-scheme=${before.outerCs} · iframe 内 body=${before.inner} color-scheme=${before.innerCs}`);
      await page.click('#themeBtn');                                 // 🔴 真点击 console 顶层那个 ◐,不手设属性
      await frame.waitForFunction(() => document.body.dataset.theme === 'dark', null, { timeout: WAIT });
      const after = {
        outer: await page.evaluate('document.documentElement.getAttribute("data-theme")'),
        outerCs: await page.evaluate('getComputedStyle(document.documentElement).colorScheme'),
        inner: await frame.evaluate('document.body.dataset.theme || null'),
        innerCs: await frame.evaluate('getComputedStyle(document.body).colorScheme'),
        htmlCsInIframe: await frame.evaluate('getComputedStyle(document.documentElement).colorScheme'),
        cmCs: await frame.evaluate('getComputedStyle(document.querySelector(".EasyMDEContainer .CodeMirror")).colorScheme'),
        iconColor: await frame.evaluate('(function(){var i=document.querySelector(".editor-toolbar button.bold i")||document.querySelector(".editor-toolbar button.bold");return getComputedStyle(i).color;})()'),
        stored: await frame.evaluate('(function(){try{return localStorage.getItem("dw-theme");}catch(e){return "ERR "+e.name;}})()'),
        outerStored: await page.evaluate('(function(){try{return localStorage.getItem("dw-theme");}catch(e){return "ERR "+e.name;}})()')
      };
      console.log(`     点击后:外层 html[data-theme]=${after.outer} color-scheme=${after.outerCs}`
        + ` · iframe 内 html color-scheme=${after.htmlCsInIframe} / body=${after.inner} color-scheme=${after.innerCs}`
        + ` · 编辑器元素 color-scheme=${after.cmCs} · 图标色=${after.iconColor}`);
      console.log(`     🔵 传输证据:localStorage["dw-theme"] iframe 内=${after.stored} · 外层=${after.outerStored}`
        + `(🔴 这个键**只有产品自己写** —— renderer:4150 THEME_KEY;装置不写它)`);
      assert.strictEqual(after.outer, 'dark', `外层 console 没变暗(实测 ${after.outer})⇒ 点的不是那个开关`);
      assert.strictEqual(after.inner, 'dark', `iframe 内没跟着变暗(实测 ${after.inner})⇒ 主题没传到 pane,这一格量的是浅色`);
      assert.strictEqual(after.outerCs, 'dark', `外层 color-scheme 没变(实测 ${after.outerCs})`);
      assert.notStrictEqual(before.inner, after.inner, '守卫:点击前后 iframe 内主题没变 ⇒ 这一腿是空转');
      cons0.chain = after;
    });

    // ── C2 iframe 形状(暗色)实测 ─────────────────────────────────────────
    await t('C2 🧪 **console iframe 内**(暗色,业主真实走的那条路)实测:正文/工具栏/边框 对比度', async () => {
      const { page, frame, rec } = cons0;
      iframeDark = await measureIn(frame, page, 'cons-iframe-dark', rec);
      const hov = iframeDark.rows.find((r) => /真 hover/.test(r.label));
      const act = iframeDark.rows.find((r) => /真点击/.test(r.label));
      assert.ok(hov && act, '装置:没量到 hover/active 两行');
      console.log(`     🔴 **业主那条路上的核心数**:真 hover ${hov.value} · 真点击 .active ${act.value}(门槛 ${TH_UI})`
        + ` ⇒ ${hov.ok ? '合格' : '**低于门槛:白压白在这条路上同样成立**'}`);
    });

    // ── C3 独立页形状(暗色)实测:同一份字节的对照腿 ──────────────────────
    await t('C3 🧪 **独立 renderer 页**(暗色,主题走 renderer 自己那个 ◐)实测 —— C2 的对照腿', async () => {
      const { ctx, page, rec } = await openStandalone(browser, port);
      try {
        const before = await page.evaluate('document.body.dataset.theme || null');
        await page.click('#themeBtn');                                // 🔴 也是真点击,不手设
        await page.waitForFunction(() => document.body.dataset.theme === 'dark', null, { timeout: WAIT });
        const after = await page.evaluate('document.body.dataset.theme');
        assert.notStrictEqual(before, after, '守卫:独立页这条腿的主题没变 ⇒ 量的是浅色,对照没有意义');
        standaloneDark = await measureIn(page, page, 'standalone-dark', rec);
      } finally { await ctx.close().catch(() => {}); }
    });

    // ── C4 🔴 两条路径逐项对照(裁定 (3) 的判据落点)───────────────────────
    await t('C4 🔴 两条路径**逐项对照**:信息维判定集合必须一致(不一致 = 发现,不是装置问题)', async () => {
      assert.ok(iframeDark && standaloneDark, '装置:C2/C3 没跑成 ⇒ 无从对照');
      const A = verdictSet(iframeDark.rows), B = verdictSet(standaloneDark.rows);
      console.log('\n     观测量                          console iframe   独立页   差');
      const labels = iframeDark.rows.map((r) => r.label);
      let maxDiff = 0;
      labels.forEach((lb) => {
        const a = iframeDark.rows.find((r) => r.label === lb);
        const b = standaloneDark.rows.find((r) => r.label === lb);
        if (!a || !b) { console.log(`     🔴 ${lb.slice(0, 30).padEnd(30)} 只有一边量到 ⇒ 行集合不同源,先查装置`); return; }
        const d = Math.abs(a.value - b.value);
        maxDiff = Math.max(maxDiff, d);
        const flag = d > 0.001 ? (a.dim === DIM_INFO ? '  🔴 信息维有差' : '  🔵 装饰维有差') : '';
        console.log(`     ${lb.slice(0, 30).padEnd(30)} ${String(a.value).padEnd(14)} ${String(b.value).padEnd(8)} ${R(d)}${flag}`);
      });
      console.log(`     最大绝对差 ${R(maxDiff)} · 像素层 编辑区 ${iframeDark.pxCm.contrast_max} vs ${standaloneDark.pxCm.contrast_max}`
        + ` · hover 钮 ${iframeDark.pxHov.contrast_max} vs ${standaloneDark.pxHov.contrast_max}`);
      console.log(`     color-scheme 链 iframe 形状:外层 html=dark · iframe html=${cons0.chain.htmlCsInIframe} · body=${cons0.chain.innerCs} · 编辑器=${cons0.chain.cmCs}`
        + `\n                     独立页形状:html=${standaloneDark.m.els.body.htmlColorScheme} · body=${standaloneDark.m.els.body.colorScheme}`);
      // 🔴 判据:信息维的"合格/不合格"集合必须逐项相同 —— 否则"独立页上量到的结论"就不能搬到业主那条路上
      assert.deepStrictEqual(A, B,
        `🔴 **两条路径的信息维判定不一致** ⇒ 这是**发现**,不是装置问题:\n        console iframe:${JSON.stringify(A)}\n        独立页       :${JSON.stringify(B)}\n`
        + `        ⇒ 停在这里,把两个数**都**报上去(裁定 (3):在业主不走的那条路上验过的绿不算绿)`);
      // 🔴 对照守卫:两条腿必须**真的是两种嵌入上下文**,否则这条对照是假的(自己跟自己比,当然一致)
      assert.strictEqual(iframeDark.inFrame, true,
        '🔴 守卫:C2 那腿 `window.self === window.top` ⇒ 它根本不在 iframe 里,量的不是业主那条路');
      assert.strictEqual(standaloneDark.inFrame, false,
        '🔴 守卫:C3 那腿竟然在 iframe 里 ⇒ 两条腿量的是同一个上下文,对照无效');
      // 🔵 顺带把"外层暗有没有穿过 iframe 边界"这个量钉住(实测:没有 —— iframe 内 html 仍是 light)
      assert.strictEqual(iframeDark.m.els.body.htmlColorScheme, 'light',
        `🔴 与实测预期不符:iframe 内 html 的 color-scheme = ${iframeDark.m.els.body.htmlColorScheme}`
        + '(renderer :9 的 ":root{color-scheme:light}" 应当赢过外层 console 的 dark)⇒ 浏览器行为变了,先查装置再报结论');
      const hovA = iframeDark.rows.find((r) => /真 hover/.test(r.label));
      assert.strictEqual(hovA.ok, false,
        `🔴 反常:业主那条路上 hover 竟然合格(${hovA.value})⇒ 与 D2 的 1.026 矛盾,先查装置(是不是量到了别的按钮)`);
      console.log(`     🔵 **实测结论**:同一份产品字节、同一把尺子,console iframe 与独立页的信息维判定**逐项相同**`
        + `(最大绝对差 ${R(maxDiff)})⇒ 🔴 D1–D8 的结论**在业主真实走的那条路上成立**,这是实测的,不是推论的。`);
    });

    // ── C5 阴格:这条路上尺子也得有牙 ─────────────────────────────────────
    await t('C5 🔴 阴格(**在 iframe 形状下**):注入 ~1.10 的正文配色 ⇒ 必须判不合格(否则 C2 的绿没有分辨力)', async () => {
      const { page, frame, rec } = cons0;
      // 🔵 这一腿**允许**用注入(测试缝):它验的是**尺子在 iframe 里有没有牙**,不是产品行为。
      //    与 C1 的"不手设 data-theme"不冲突 —— 那里验的是产品会不会把主题传进来,这里验的是量测会不会漏。
      await frame.addStyleTag({ content: MUT_DARK110 });
      // 🔴 装置自审(第一跑就红在这里,守卫是对的):C2 已经把 bold **点成 `.active`** 了,
      //    而 measureIn 会再点一次 ⇒ 那一下是**取消**激活 ⇒ "真点击 .active" 那行量不到。
      //    (dark-contrast-probe 每腿开新 context,所以没这个问题;这里 C2/C5 复用同一个文档 ⇒ 必须先复位。)
      const clsOf = () => frame.evaluate('(function(){var b=document.querySelector(".editor-toolbar button.bold");return b?b.className:null;})()');
      const pre = await clsOf();
      if (pre && /active/.test(pre)) {
        await frame.click('.editor-toolbar button.bold');
        await frame.waitForFunction('!/active/.test(document.querySelector(".editor-toolbar button.bold").className)', null, { timeout: WAIT });
      }
      const post = await clsOf();
      assert.ok(post && !/active/.test(post),
        `装置:bold 复位失败(点击前 class="${pre}" → 现在 class="${post}")⇒ 后面那行量不到激活态,别拿合成态充数`);
      console.log(`\n     🔵 复位:button.bold class "${pre}" → "${post}"(measureIn 会再点一次 ⇒ 那一次才是"从未激活到激活")`);
      const r = await measureIn(frame, page, 'cons-iframe-dark110', rec);
      const body = r.rows.find((x) => /正文基础色/.test(x.label));
      const tok = r.rows.find((x) => /正文最坏 token/.test(x.label));
      assert.ok(body && tok, '装置:没量到正文那两行');
      console.log(`     变异体:正文基础色 ${body.value} · 正文最坏 token ${tok.value}(期望 ≈1.10;门槛 ${TH_TEXT})`);
      assert.strictEqual(body.ok, false, `🔴 阴格没过:注入 ~1.10 的正文配色之后**仍判合格**(${body.value})⇒ 探针在这条路上没有分辨力`);
      assert.ok(body.value < 1.5, `🔴 阴格数值不对(期望 ≈1.10,实测 ${body.value})⇒ 变异没打进去,先查装置`);
      console.log(`     🔵 阴格过:同一把尺子在 **console iframe 内**也能把 1.10 判成不合格 ⇒ C2 那些数字有分辨力。`);
    });

  } finally {
    if (cons0) await cons0.ctx.close().catch(() => {});
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }

  console.log('\n══ 汇总:同一套量法,两条路径(都是暗色) ══');
  [iframeDark, standaloneDark].forEach((r) => {
    console.log(`\n【${r.tag}】`);
    r.rows.forEach((x) => { if (x.text) console.log('  ' + x.text); else console.log(`  🔵 ${x.label} = ${x.value}(只报数,不判定)`); });
  });
  console.log('\n  🔵 这条绿【看不见什么】:');
  console.log('     ① 编辑器是**装置注入**进 renderer 文档的(注入点 `</body>` 前),🔴 **不是产品自带的** ——');
  console.log('        真引入时它挂在哪、外层有没有别的容器/滚动上下文,**会改变观感但不会改变这里的颜色对**(颜色只取决于级联到的 CSS)。');
  console.log('        🔴 这句"不会改变"是**读码/推理**,不是实测:挂载位置变了就得重量。');
  console.log('     ② 只验了 **1280x900** 一档窗口;窄面板(console 的 nav 是固定 240px 列)下**没量**。');
  console.log('     ③ 主题切换只验了 **light → dark** 一次;来回切、跨 tab 广播、隐藏 pane(display:none)那些路径**没量**。');
  console.log('     ④ console 外壳自己的对比度(nav/tabs/hint)**不在本格** —— 那属 `console-theme-global-regression.js` 的范围。');
  console.log('     ⑤ 门槛 WCAG AA 与"装饰维不判缺陷"都来自组长裁定 (1);**门槛值本身归业主**。');
  console.log('     ⑥ 隔离仍是**浏览器层**(死端口代理),非 OS 级;🔴 且本席是 Linux + Playwright 自带 chromium,');
  console.log('        **业主那台 macOS + 系统 Chrome 上没跑过**(组长 ts-No.78 后自己点出的同型缺口,我照实登记)。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
