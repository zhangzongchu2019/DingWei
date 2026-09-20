// 🔴 ts-No.78 · EasyMDE 在**暗色**下与本仓 CSS 的冲突(组长出单)
//
// 组长判据(逐字):验 `data-theme` 两种取值下**正文/工具栏/边框**的**对比度实测值**;
//                 🔴 阴格必须有:构造明知不合格的配色(~1.1)⇒ 探针必须报不合格;
//                 🔵 业主对"暗色下遮罩提示对比度降到 1.08"有过意见 —— 那个数就是现成的阴格基准;
//                 仍**只验不引入**;口径止于"这套配置下的实测对比度",**不升级成"暗色没问题"**。
//
// 🔴 属性名更正:板件写的是 `data-dw-theme`,**本仓真实属性名是 `data-theme`**
//    (renderer:`renderer-canary-sse.html:26` 变量集 / `:1021` `<body data-theme="light">`;
//     console:`internal/m8/console_auth.go:277` `html[data-theme="dark"]`)。本装置按**真实属性名**做。
//
// 两个观测面(不一致就如实报两个数,不取其一):
//   A) **解析层** getComputedStyle 取到的颜色对 ⇒ 可归因(哪个元素/哪个 class/哪个 token)
//   B) **像素层** 元素截图 → `px.py` 取实际渲染后的最暗/最亮像素 ⇒ 合成、半透明、抗锯齿之后的真值
//      🔴 为什么必须有 B:computed style 只会说"0.55 的黑",**不会**告诉你压在某个背景上最终是什么颜色。
//
// 产品 CSS **不复制、不硬编码**:运行时从真实产品文件里抽 `<style>` 块,并做形状守卫;
// 抽不到 / 变量集形状变了 ⇒ **rc=3 中止**(锚点过期就报错,不拿旧数字报)。
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');
const { requireVendor, PINS_MAIN, PINS_FA, sha256 } = require('./vendor-pins');

const HERE = __dirname;
const OUT = path.join(HERE, 'out');
const PROXY = 'http://127.0.0.1:9';
const PRODUCT = path.resolve(HERE, '../../../view-service/renderer-canary-sse.html');
const WAIT = 20000;

// 🔴 门槛 = WCAG 2.1 AA(正文 4.5:1;非文本/UI 组件 3.0:1)。
//    🔵 **这个门槛是我选的**,组长/业主可以改;**报数与判定分开写**,判定不掩盖数字。
const TH_TEXT = 4.5, TH_UI = 3.0;

const lin = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4); };
const lum = (a) => 0.2126 * lin(a[0]) + 0.7152 * lin(a[1]) + 0.0722 * lin(a[2]);
const ratio = (a, b) => { const la = lum(a), lb = lum(b); return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05); };
const R = (x) => Math.round(x * 1000) / 1000;
// 🔵 两种写法都要认:`getComputedStyle().backgroundColor` 回 `rgb(...)/rgba(...)`,
//    而 **CSS 变量的原始值**回 `#rrggbb`(第一跑我只认 rgb() ⇒ D1/D2 全红在守卫上,红的是我的解析器)。
const rgb = (s) => {
  if (!s) return null;
  const t = String(s).trim();
  const m = /rgba?\(([^)]+)\)/.exec(t);
  if (m) {
    const p = m[1].split(',').map((x) => parseFloat(x));
    if (p.slice(0, 3).some((x) => Number.isNaN(x))) return null;
    return { v: [p[0], p[1], p[2]], a: p.length > 3 ? p[3] : 1, raw: t };
  }
  const h = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(t);
  if (h) {
    let x = h[1];
    if (x.length === 3) x = x.split('').map((c) => c + c).join('');
    return { v: [0, 2, 4].map((i) => parseInt(x.slice(i, i + 2), 16)), a: 1, raw: t };
  }
  return null;
};

function px(png) {
  return JSON.parse(execFileSync('python3', [path.join(HERE, 'px.py'), png], { encoding: 'utf8' }).trim().split('\n').pop());
}

// ── 产品 CSS:运行时抽取 + 形状守卫 ───────────────────────────────────────────
function productCSS() {
  if (!fs.existsSync(PRODUCT)) {
    console.error('🔴 产品件不在:' + PRODUCT + ' ⇒ rc=3(路径/锚点过期,**不拿旧数字报**)');
    process.exit(3);
  }
  const html = fs.readFileSync(PRODUCT, 'utf8');
  const m = /<style>([\s\S]*?)<\/style>/.exec(html);
  if (!m) { console.error('🔴 产品件里抽不到 <style> 块 ⇒ rc=3(形状变了)'); process.exit(3); }
  const css = m[1];
  const need = ['--bg:', '--text:', '--line:', '--panel:', '[data-theme="dark"]'];
  const miss = need.filter((k) => !css.includes(k));
  if (miss.length) {
    console.error('🔴 产品 CSS 形状变了:缺 ' + miss.join(' , ') + ' ⇒ rc=3(主题变量集不是本装置量的那份)');
    process.exit(3);
  }
  return { css, sha: sha256(PRODUCT), bytes: html.length, styleBytes: css.length };
}

// 🔵 只留「变量集 + body」三条规则的子集 —— 给 D5 定因用:
//    若"字号/行高/字体被改写"在**只有 body 规则**时就已经发生,那原因就**不是**本仓的 `pre{}`,
//    而是 `body{font:14px/1.55 ui-sans-serif…}` 经 CodeMirror 自己的 `font:inherit` 链继承下来的。
//    🔴 这是"定因"而不是"猜因":读码预期(本仓 `pre{}` 命中 `.CodeMirror-line`)已被实测推翻过一次。
function bodyOnlyCSS(css) {
  const keep = [];
  const re = /(^|\})\s*((?::root|\[data-theme="dark"\]|body)\s*)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(css)) !== null) keep.push(m[2].trim() + '{' + m[3] + '}');
  return keep.join('\n');
}

// ── 夹具服务 ────────────────────────────────────────────────────────────────
function startServer(prod) {
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1');
    const u = url.pathname;
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/') {
      const theme = url.searchParams.get('theme') || 'light';
      const pcssMode = url.searchParams.get('pcss') || '1';   // 1=全量 · 0=不注入 · bodyonly=只注入变量集+body
      const mut = url.searchParams.get('mut') || '';
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(pageHTML(prod, theme, pcssMode, mut));
    }
    if (u === '/calib') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' }); return res.end(calibHTML()); }
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

const MD_SAMPLE = [
  '# 标题一',
  '',
  '正文一段:**粗体** 与 *斜体* 与 `行内代码` 与 [链接文字](https://intranet.example/a)。',
  '',
  '> 引用一段',
  '',
  '- 列表项一',
  '- 列表项二',
  '',
  '```js',
  'const x = 1; // 注释',
  '```',
  ''
].join('\n');

// 变异体(每一个都写明**它要回答什么问题**)
const MUTS = {
  // 阴格 B(真靶):把编辑器正文压到 ~1.10 —— 明知不合格,探针必须报出来
  dark110: '.EasyMDEContainer .CodeMirror{background:#111316 !important;color:#1a1d21 !important}'
    + '.EasyMDEContainer .CodeMirror .cm-s-easymde span,.CodeMirror-line{color:#1a1d21 !important}',
  // 定因:暗色下图标色实测是 rgb(255,255,255),而 --text 是 #eef2f6 ⇒ 不像来自 --text。
  //       若把 color-scheme 强制改回 light,图标**应当**翻成黑 ⇒ 证明它来自 UA 的 canvastext(随 color-scheme 走)
  schemelight: ':root{color-scheme:light !important}[data-theme="dark"]{color-scheme:light !important}',
  // 候选修法(**只实测,不落产品**):把 EasyMDE 写死的 hover/active 背景换成本仓暗色变量
  darkfix: '.editor-toolbar button.active,.editor-toolbar button:hover{background:var(--panel-2) !important;border-color:var(--line) !important}',
  // D8 风险显影:本仓 `view-service/renderer-canary-sse.html:990` 那条 bare `pre{color:var(--code-text)}`
  //   现在**打不到**编辑区 —— CodeMirror 自己的 `.CodeMirror pre.CodeMirror-line{color:inherit}` 特异性更高
  //   (D5 已实测:那条 bare `pre` 的 color 一个量都没改)。
  //   🔴 这一格模拟"有人给它加 `!important` 或把它挪进 `.foo pre` 提特异性":`!important` **直接越过**特异性,
  //      那道墙当场消失 ⇒ 问题是"消失之后**看得见的正文**有多少跟着变"。
  //   🔵 装置自审:本键**第一版根本不存在**(D8 调了 `precascade` 却没在 MUTS 里定义 ⇒ 注入为空,
  //      那一跑量的是**未变异**的页面)。红的是我的件,不是产品 —— 见交付件"探针第一次红"那一节。
  precascade: 'pre{color:var(--code-text) !important}'
};

function pageHTML(prod, theme, pcssMode, mut) {
  const extra = MUTS[mut] ? '<style>' + MUTS[mut] + '</style>' : '';
  const injected = pcssMode === '1' ? prod.css : (pcssMode === 'bodyonly' ? bodyOnlyCSS(prod.css) : '');
  return [
    '<!doctype html><meta charset="utf-8"><title>DW · ts-No.78 暗色对比度夹具</title>',
    injected ? '<style>' + injected + '</style>' : '',
    '<link rel="stylesheet" href="/vendor/easymde.min.css">',
    '<link rel="stylesheet" href="/vendor/fa.min.css">',
    extra,
    '<div id="dwWrap" style="padding:16px"><textarea id="md"></textarea></div>',
    '<script src="/vendor/easymde.min.js"></script>',
    '<script>',
    '(function(){',
    '  document.body.setAttribute("data-theme", ' + JSON.stringify(theme) + ');',
    '  window.__dwTheme = ' + JSON.stringify(theme) + ';',
    '  window.__dwPcss = ' + JSON.stringify(pcssMode) + ';',
    '  window.__dwMut = ' + JSON.stringify(mut) + ';',
    '  window.__dwEditor = new EasyMDE({',
    '    element: document.getElementById("md"),',
    '    spellChecker: false,                 // 引入硬条件①',
    '    autoDownloadFontAwesome: false,      // 引入硬条件①(FA 已本地自托管 = 硬条件②)',
    '    toolbar: ["bold","italic","heading","|","quote","unordered-list","ordered-list","|","link","image","|","preview","side-by-side","fullscreen"],',
    '    //  🔴 去掉了默认的 "guide"(它是外网站点,ts-No.77 实测)',
    '    initialValue: ' + JSON.stringify(MD_SAMPLE) + ',',
    '    minHeight: "200px"',
    '  });',
    '  window.__dwMeasure = function(){',
    '    function cs(el,p){ return getComputedStyle(el)[p]; }',
    '    function effBg(el){ var n=el; while(n && n.nodeType===1){ var c=getComputedStyle(n).backgroundColor;',
    '      var m=/rgba?\\(([^)]+)\\)/.exec(c); if(m){ var p=m[1].split(",").map(parseFloat); if(p.length<4||p[3]>0)',
    '        return {from:n.tagName+(n.className?"."+String(n.className).trim().split(/\\s+/)[0]:""), value:c}; } n=n.parentElement; }',
    '      return {from:"(none)", value:"rgba(0, 0, 0, 0)"}; }',
    '    var out = { theme: window.__dwTheme, pcss: window.__dwPcss, mut: window.__dwMut, vars:{}, els:{}, tokens:{}, line:{} };',
    '    ["--bg","--text","--muted","--line","--panel","--panel-2","--accent","--accent-2","--code-bg","--code-text"].forEach(function(k){',
    '      out.vars[k] = getComputedStyle(document.body).getPropertyValue(k).trim(); });',
    '    var b = document.body;',
    '    out.els.body = { bg: cs(b,"backgroundColor"), color: cs(b,"color"), fontFamily: cs(b,"fontFamily"),',
    '      fontSize: cs(b,"fontSize"), colorScheme: cs(b,"colorScheme"), htmlColorScheme: cs(document.documentElement,"colorScheme") };',
    '    var cm = document.querySelector(".EasyMDEContainer .CodeMirror");',
    '    if (cm) { var e = effBg(cm); out.els.codeMirror = { bg: e.value, bgFrom: e.from, color: cs(cm,"color"),',
    '      borderColor: cs(cm,"borderTopColor"), borderWidth: cs(cm,"borderTopWidth"), fontSize: cs(cm,"fontSize"),',
    '      fontFamily: cs(cm,"fontFamily"), rect: (function(r){return {w:Math.round(r.width),h:Math.round(r.height)};})(cm.getBoundingClientRect()) }; }',
    '    var line = document.querySelector(".CodeMirror-line");',
    '    if (line) { out.line = { tag: line.tagName, fontSize: cs(line,"fontSize"), lineHeight: cs(line,"lineHeight"),',
    '      maxHeight: cs(line,"maxHeight"), maxWidth: cs(line,"maxWidth"), tabSize: cs(line,"tabSize"),',
    '      overflow: cs(line,"overflow"), overflowX: cs(line,"overflowX"), whiteSpace: cs(line,"whiteSpace"),',
    '      wordBreak: cs(line,"wordBreak"), fontFamily: cs(line,"fontFamily"), color: cs(line,"color"),',
    '      margin: cs(line,"margin"), padding: cs(line,"padding") }; }',
    '    var tb = document.querySelector(".editor-toolbar");',
    '    if (tb) { var tbBg = effBg(tb); out.els.toolbar = { bg: tbBg.value, bgFrom: tbBg.from, color: cs(tb,"color"),',
    '      borderTopColor: cs(tb,"borderTopColor"), borderLeftColor: cs(tb,"borderLeftColor") }; }',
    '    var icon = document.querySelector(".editor-toolbar button.bold i") || document.querySelector(".editor-toolbar button.bold");',
    '    if (icon) { var btn = icon.closest("button"); var ib = effBg(btn);',
    '      out.els.toolbarIcon = { color: cs(icon,"color"), buttonColor: cs(btn,"color"), colorScheme: cs(btn,"colorScheme"),',
    '      fontFamily: cs(icon,"fontFamily"), bg: ib.value, bgFrom: ib.from, btnBorder: cs(btn,"borderTopColor") }; }',
    '    var sep = document.querySelector(".editor-toolbar i.separator");',
    '    if (sep) { out.els.separator = { borderLeftColor: cs(sep,"borderLeftColor"), borderRightColor: cs(sep,"borderRightColor") }; }',
    '    var sb = document.querySelector(".editor-statusbar");',
    '    if (sb) { var sbb = effBg(sb); out.els.statusbar = { color: cs(sb,"color"), bg: sbb.value, bgFrom: sbb.from, fontSize: cs(sb,"fontSize") }; }',
    '    var spans = document.querySelectorAll(".CodeMirror span[class*=cm-]");',
    '    for (var i=0;i<spans.length;i++){ var s=spans[i]; var c=cs(s,"color"); var cls=s.className;',
    '      if(!out.tokens[c]) out.tokens[c]={classes:{},n:0}; out.tokens[c].classes[cls]=(out.tokens[c].classes[cls]||0)+1; out.tokens[c].n++; }',
    '    out.tokenCount = spans.length;',
    // 🔴 D8 要用的量:编辑区里**继承** pre 颜色的 span 有多少(= 变异真正打得着的正文),
    //    以及**自带颜色声明**的 span 有多少(= 被 CodeMirror 主题色 shield 住的部分)。
    //    🔵 为什么不能只看像素层 contrast_max:它取的是整块的最暗/最亮对,**部分**文字变不可见它一动不动
    //       ⇒ 它证不了"正文没事"。所以这一维必须在解析层按 span 数。
    '    var lines = document.querySelectorAll(".CodeMirror-line");',
    '    out.inherit = { lines: lines.length, spans: 0, inheriting: 0, preColor: null, sampleInherit: [], sampleOwn: [] };',
    '    if (lines.length) {',
    '      out.inherit.preColor = cs(lines[0], "color");',
    '      for (var k = 0; k < spans.length; k++) {',
    '        var sp = spans[k]; out.inherit.spans++;',
    '        if (cs(sp, "color") === out.inherit.preColor) {',
    '          out.inherit.inheriting++;',
    '          if (out.inherit.sampleInherit.length < 3) out.inherit.sampleInherit.push(sp.className);',
    '        } else if (out.inherit.sampleOwn.length < 3) out.inherit.sampleOwn.push(sp.className + "=" + cs(sp, "color"));',
    '      }',
    '    }',
    '    return out;',
    '  };',
    '  window.__dwSetHoverTarget = function(sel){ var el=document.querySelector(sel); if(el) el.setAttribute("data-dw-probe", sel); };',
    '  window.__dwReady = true;',
    '})();',
    '</script>'
  ].join('\n');
}

// 阴格 A(刻度):不依赖 EasyMDE,直接渲染已知配色的文字块 ⇒ 证明这把尺子准
function calibHTML() {
  const pairs = [
    { id: 'c108', bg: '#111316', fg: '#181b1f', note: '≈1.08(业主对遮罩提示提过的那个数)' },
    { id: 'c110', bg: '#111316', fg: '#1a1d21', note: '≈1.10(组长判据里说的 ~1.1)' },
    { id: 'cGood', bg: '#111316', fg: '#eef2f6', note: '≈16.5(本仓暗色 --text vs --bg,必须合格)' }
  ];
  return ['<!doctype html><meta charset="utf-8"><title>calib</title>',
    '<style>body{margin:0;background:#111316}.c{width:300px;height:44px;font:16px/44px sans-serif;padding-left:8px}</style>'].concat(
    pairs.map((p) => `<div class="c" id="${p.id}" style="background:${p.bg};color:${p.fg}">${p.id} 刻度块</div>`),
    ['<script>window.__dwCalib=' + JSON.stringify(pairs) + ';window.__dwReady=true;</script>']).join('\n');
}

const classify = (url, port) => (url.startsWith(`http://127.0.0.1:${port}`) || url.startsWith(`http://localhost:${port}`)) ? 'local'
  : (/^(data|blob|about|filesystem):/.test(url) ? 'inline' : 'external');

async function open(browser, port, qs) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
  const page = await ctx.newPage();
  const rec = { cdpExt: [], pwExt: [], pageErrors: [] };
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.requestWillBeSent', (e) => { if (classify(e.request.url, port) === 'external') rec.cdpExt.push(e.request.url); });
  ctx.on('request', (r) => { if (classify(r.url(), port) === 'external') rec.pwExt.push(r.url()); });
  page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 140)));
  await page.goto(`http://127.0.0.1:${port}/${qs}`, { waitUntil: 'load', timeout: WAIT });
  await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });
  await page.evaluate('document.fonts.ready');
  await page.waitForTimeout(1200);
  return { ctx, page, rec };
}

function judge(label, value, th, kind) {
  const ok = value >= th;
  return { label, value: R(value), th, kind, ok, text: `${label} = ${R(value)} : 1  门槛 ${th}(${kind})⇒ ${ok ? '✅ 合格' : '🔴 低于门槛'}` };
}

(async () => {
  requireVendor(Object.assign({}, PINS_MAIN, PINS_FA), 'ts-No.78 暗色对比度');
  fs.mkdirSync(OUT, { recursive: true });
  const prod = productCSS();
  console.log('ts-No.78 · EasyMDE 在暗色下与本仓 CSS 的冲突(对比度实测)');
  console.log(`  产品件    ${PRODUCT}`);
  console.log(`  产品件 sha256=${prod.sha.slice(0, 16)}… · 文件 ${prod.bytes} B · 抽出 <style> ${prod.styleBytes} B`);
  console.log(`  属性名    🔴 本仓真实为 \`data-theme\`(板件写的 \`data-dw-theme\` 是错的;renderer :26/:1021,console console_auth.go:277)`);
  console.log(`  门槛      WCAG 2.1 AA:正文 ${TH_TEXT} · 非文本/UI ${TH_UI}  🔵 **门槛是我选的**,报数与判定分开`);
  console.log(`  隔离      ${PROXY}(死端口代理,与 ts-No.77 同一手段、同一条边界:浏览器层,非 OS 级)`);

  const { server, port } = await startServer(prod);
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox', `--proxy-server=${PROXY}`] });
  } catch (e) { console.error('环境不可用:' + e.message.split('\n')[0]); process.exit(3); }

  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 10).join('\n  ')}`); }
  };
  const report = [];

  async function measureTheme(theme, tag, mut) {
    const { ctx, page, rec } = await open(browser, port, `?theme=${theme}&mut=${mut || ''}`);
    try {
      const m = await page.evaluate('window.__dwMeasure()');
      assert.deepStrictEqual(rec.cdpExt, [], `🔴 断网下仍有外部请求(CDP):${JSON.stringify(rec.cdpExt)}`);
      assert.deepStrictEqual(rec.pageErrors, [], `页面报错:${JSON.stringify(rec.pageErrors)}`);

      // 🔴 守卫:主题**真的生效了**吗 —— body 背景必须等于该主题的 --bg
      const varBg = rgb(m.vars['--bg']), bodyBg = rgb(m.els.body.bg);
      assert.ok(varBg && bodyBg, `守卫:颜色解析不了(body=${m.els.body.bg} --bg=${m.vars['--bg']})⇒ 解析器不认这个写法,先修探针`);
      assert.deepStrictEqual(bodyBg.v, varBg.v,
        `🔴 主题没生效:body 背景 ${m.els.body.bg} ≠ --bg ${m.vars['--bg']} ⇒ 这一格量的是**另一个**配色,数字不可用`);

      const cmBg = rgb(m.els.codeMirror.bg);
      const rows = [];
      // ① 正文:编辑器自己的背景 vs 每一个 token 颜色(取最坏)
      const tokenRows = Object.keys(m.els.codeMirror ? m.tokens : {}).map((c) => {
        const t2 = rgb(c); const v = ratio(t2.v, cmBg.v);
        return { color: c, ratio: v, n: m.tokens[c].n, classes: Object.keys(m.tokens[c].classes).slice(0, 3).join(' ') };
      }).sort((a, b) => a.ratio - b.ratio);
      assert.ok(tokenRows.length > 0,
        `守卫:一个 token 色都没量到(tokenCount=${m.tokenCount})⇒ CodeMirror 没渲染出带 class 的 span,这一格是空的`);
      rows.push(judge(`正文最坏 token(${tokenRows[0].classes || tokenRows[0].color})`, tokenRows[0].ratio, TH_TEXT, '正文'));
      rows.push(judge('正文基础色(.CodeMirror color vs bg)', ratio(rgb(m.els.codeMirror.color).v, cmBg.v), TH_TEXT, '正文'));
      // ② 工具栏常态:图标色 vs 其有效背景
      rows.push(judge('工具栏图标(常态)', ratio(rgb(m.els.toolbarIcon.color).v, rgb(m.els.toolbarIcon.bg).v), TH_UI, 'UI 图标'));
      // ③ 工具栏 hover / 激活态 —— 🔴 **用真事件**,不用 classList.add 那种测试缝
      //    (缝能验的只是"我加的类会触发那条 CSS",验不到"产品自己会不会把类加上/鼠标移上去会不会变")
      const readBtn = (sel) => page.evaluate(`(function(){
        var b=document.querySelector(${JSON.stringify(sel)}); if(!b) return null;
        var i=b.querySelector('i')||b, cs=getComputedStyle(i), cb=getComputedStyle(b);
        return { icon: cs.color, bg: cb.backgroundColor, border: cb.borderTopColor, cls: b.className,
                 rect:(function(r){return {w:Math.round(r.width),h:Math.round(r.height)};})(b.getBoundingClientRect()) };
      })()`);
      await page.hover('.editor-toolbar button.italic');            // 真鼠标移入 ⇒ 产品自己的 :hover
      const hov = await readBtn('.editor-toolbar button.italic');
      const pngHov = path.join(OUT, `${tag}-btn-hover.png`);
      await (await page.$('.editor-toolbar button.italic')).screenshot({ path: pngHov });
      const pxHov = px(pngHov);
      await page.mouse.move(5, 5);                                   // 移开,避免污染下一步
      await page.click('.editor-toolbar button.bold');               // 真点击 ⇒ 产品自己 toggle .active
      const actReal = await readBtn('.editor-toolbar button.bold');
      const pngAct = path.join(OUT, `${tag}-btn-active.png`);
      await (await page.$('.editor-toolbar button.bold')).screenshot({ path: pngAct });
      const pxBtn = px(pngAct);
      assert.ok(actReal && /active/.test(actReal.cls),
        `守卫:真点击之后产品**没有**给按钮加 .active(实测 class="${actReal && actReal.cls}")⇒ 这一格量不到激活态,别拿合成态充数`);
      rows.push(judge('工具栏图标(**真 hover** 态)', ratio(rgb(hov.icon).v, rgb(hov.bg).v), TH_UI, 'UI 图标'));
      rows.push(judge('工具栏图标(**真点击 .active** 态)', ratio(rgb(actReal.icon).v, rgb(actReal.bg).v), TH_UI, 'UI 图标'));
      // 交叉核对:合成 .active(测试缝)与真事件应当给出**同一个**数;不一致就报,不取其一
      const actSyn = await page.evaluate(`(function(){
        var b=document.querySelector('.editor-toolbar button.quote'); b.classList.add('active');
        var i=b.querySelector('i')||b; return { icon: getComputedStyle(i).color, bg: getComputedStyle(b).backgroundColor };
      })()`);
      const synRatio = ratio(rgb(actSyn.icon).v, rgb(actSyn.bg).v);
      const realRatio = ratio(rgb(actReal.icon).v, rgb(actReal.bg).v);
      assert.ok(Math.abs(synRatio - realRatio) < 0.01,
        `🔴 合成态与真事件不一致(合成 ${R(synRatio)} vs 真点击 ${R(realRatio)})⇒ 有一边量的不是同一个东西,先查装置`);
      const act = actReal;
      // ④ 边框:编辑器边框 vs 页面背景(非文本对比)
      rows.push(judge('编辑器边框 vs 页面背景', ratio(rgb(m.els.codeMirror.borderColor).v, bodyBg.v), TH_UI, '边框'));
      rows.push(judge('工具栏边框 vs 页面背景', ratio(rgb(m.els.toolbar.borderTopColor).v, bodyBg.v), TH_UI, '边框'));
      // ⑤ 状态栏
      if (m.els.statusbar) rows.push(judge('状态栏文字', ratio(rgb(m.els.statusbar.color).v, rgb(m.els.statusbar.bg).v), TH_TEXT, '正文'));
      // ⑥ 分隔线(暗色下 border-right:#fff 会变白发丝线 —— 只报数)
      if (m.els.separator) {
        rows.push(judge('工具栏分隔线 left vs 页面背景', ratio(rgb(m.els.separator.borderLeftColor).v, bodyBg.v), TH_UI, '边框'));
        rows.push(judge('工具栏分隔线 right vs 页面背景', ratio(rgb(m.els.separator.borderRightColor).v, bodyBg.v), TH_UI, '边框'));
      }
      // ⑦ 观感边界差(**不是**可读性):编辑器块 vs 页面背景
      rows.push({ label: '编辑器块 vs 页面背景(🔵 观感边界差,**不是**可读性判据)', value: R(ratio(cmBg.v, bodyBg.v)), th: null, kind: '观感', ok: null, text: '' });

      // 像素层交叉核对:编辑区 + 工具栏 + 激活态那颗钮
      const pngCM = path.join(OUT, `${tag}-codemirror.png`);
      await (await page.$('.EasyMDEContainer .CodeMirror')).screenshot({ path: pngCM });
      const pxCm = px(pngCM);
      const pngTB = path.join(OUT, `${tag}-toolbar.png`);
      await (await page.$('.editor-toolbar')).screenshot({ path: pngTB });
      const pxTb = px(pngTB);
      const out = { theme, mut: mut || '', m, rows, tokenRows: tokenRows.slice(0, 8), pxCm, pxTb, pxBtn, pxHov, hov, act, actSyn, rec };
      report.push(out);

      console.log(`\n     主题 ${theme}${mut ? '(变异 ' + mut + ')' : ''} · --bg ${m.vars['--bg']} · --text ${m.vars['--text']} · --line ${m.vars['--line']}`);
      console.log(`     编辑器 bg=${m.els.codeMirror.bg} color=${m.els.codeMirror.color} · 工具栏 bg=${m.els.toolbar.bg}(来自 ${m.els.toolbar.bgFrom}) · 图标 color=${m.els.toolbarIcon.color}`);
      rows.forEach((r) => console.log('     ' + (r.text || `     ${r.label} = ${r.value}(只报数)`)));
      console.log(`     像素层 编辑区:bg ${JSON.stringify(pxCm.bg)} 最暗 ${JSON.stringify(pxCm.darkest)} 最亮 ${JSON.stringify(pxCm.lightest)} ⇒ contrast_max ${pxCm.contrast_max}`);
      console.log(`     像素层 工具栏:bg ${JSON.stringify(pxTb.bg)} 最暗 ${JSON.stringify(pxTb.darkest)} 最亮 ${JSON.stringify(pxTb.lightest)} ⇒ contrast_max ${pxTb.contrast_max}`);
      console.log(`     像素层 真 hover 钮:bg ${JSON.stringify(pxHov.bg)} 最暗 ${JSON.stringify(pxHov.darkest)} 最亮 ${JSON.stringify(pxHov.lightest)} ⇒ contrast_max ${pxHov.contrast_max}`);
      console.log(`     像素层 真点击 .active 钮:bg ${JSON.stringify(pxBtn.bg)} 最暗 ${JSON.stringify(pxBtn.darkest)} 最亮 ${JSON.stringify(pxBtn.lightest)} ⇒ contrast_max ${pxBtn.contrast_max}`);
      console.log(`     🔵 交叉核对:合成 .active(测试缝)= ${R(synRatio)} vs 真点击 = ${R(realRatio)} ⇒ **一致**(否则上面那条断言会红)`);
      const below = rows.filter((r) => r.ok === false);
      console.log(`     🔴 低于门槛 ${below.length} 项:${below.map((r) => `${r.label} ${r.value}`).join(' · ') || '(无)'}`);
      console.log(`     🔵 最坏的 5 个 token 色:${out.tokenRows.slice(0, 5).map((x) => `${x.color}→${R(x.ratio)}(${x.classes})`).join(' · ')}`);
      return out;
    } finally { await ctx.close().catch(() => {}); }
  }

  try {
    // ── D0 装置自证:产品 CSS 抽到了、隔离成立 ──────────────────────────────
    await t('D0 🔴 装置自证:产品 CSS 抽到 + 形状守卫过 + 隔离成立(外部域必须 BLOCKED)', async () => {
      const { ctx, page, rec } = await open(browser, port, '?theme=dark');
      try {
        const probe = await page.evaluate(`(async function(){
          var out={local:null,ext:{}};
          try{ var r=await fetch('/ping'); out.local='OK '+r.status; }catch(e){ out.local='FAILED '+e.name; }
          for (const [k,u] of Object.entries({jsdelivr:'https://cdn.jsdelivr.net/codemirror.spell-checker/latest/en_US.aff',
                                              bootstrapcdn:'https://maxcdn.bootstrapcdn.com/font-awesome/latest/css/font-awesome.min.css',
                                              example:'https://example.com/'})) {
            try{ var rr=await fetch(u,{mode:'no-cors'}); out.ext[k]='REACHED '+rr.status; }catch(e2){ out.ext[k]='BLOCKED '+e2.name; }
          }
          return out; })()`);
        console.log(`\n     ${JSON.stringify(probe)}`);
        assert.ok(/^OK 200$/.test(probe.local), `隔离把本地也断了(${probe.local})⇒ 夹具不可用`);
        const leaked = Object.keys(probe.ext).filter((k) => !/^BLOCKED/.test(probe.ext[k]));
        assert.deepStrictEqual(leaked, [], `🔴 隔离不成立:${leaked.map((k) => k + '=' + probe.ext[k]).join(' | ')} ⇒ 中止`);
        console.log(`     🔵 产品件 sha256=${prod.sha.slice(0, 16)}… · <style> ${prod.styleBytes} B 已注入夹具页`);
      } finally { await ctx.close().catch(() => {}); }
    });
    if (fail) { console.log('\n🔴 装置自证没过 ⇒ 中止,不产出任何对比度结论'); await browser.close(); await new Promise((r) => server.close(r)); process.exit(3); }

    // ── D1 / D2 两种主题 ────────────────────────────────────────────────────
    let light, dark;
    await t('D1 🧪 **浅色**(`data-theme="light"`)实测:正文/工具栏/边框 对比度', async () => { light = await measureTheme('light', 'light'); });
    await t('D2 🧪 **暗色**(`data-theme="dark"`)实测:正文/工具栏/边框 对比度', async () => { dark = await measureTheme('dark', 'dark'); });

    // ── D3 阴格 A(刻度):探针必须能把 1.08 / 1.10 判成不合格,把 16.5 判成合格 ──
    await t('D3 🔴 阴格 A(刻度自证):1.08 与 1.10 **必须判不合格**,16.54 **必须判合格**,且像素实测与解析值差 ≤0.10', async () => {
      const { ctx, page, rec } = await open(browser, port, '');
      try {
        await page.goto(`http://127.0.0.1:${port}/calib`, { waitUntil: 'load', timeout: WAIT });
        await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });
        const pairs = await page.evaluate('window.__dwCalib');
        const hex2rgb = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
        for (const p of pairs) {
          const analytic = ratio(hex2rgb(p.bg), hex2rgb(p.fg));
          const png = path.join(OUT, `calib-${p.id}.png`);
          await (await page.$('#' + p.id)).screenshot({ path: png });
          const measured = px(png).contrast_max;
          const verdict = measured >= TH_TEXT ? '合格' : '不合格';
          console.log(`\n     ${p.id} ${p.note}`);
          console.log(`       解析 ${R(analytic)} · 像素实测 ${measured} · 判定 ${verdict}`);
          assert.ok(Math.abs(measured - analytic) <= 0.10,
            `🔴 尺子不准:${p.id} 解析 ${R(analytic)} vs 像素实测 ${measured}(差 >0.10)⇒ 后面所有对比度数字都不可信`);
          if (p.id === 'cGood') assert.ok(measured >= TH_TEXT, `🔴 已知好配色(${p.id})被判不合格 ⇒ 探针过严/尺子坏`);
          else assert.ok(measured < TH_TEXT && measured < TH_UI,
            `🔴 阴格没牙:${p.id} 实测 ${measured} 却**没有**被判不合格 ⇒ 这把尺子分不出 1.08/1.10,后面的"合格"一个都不能信`);
        }
        assert.deepStrictEqual(rec.cdpExt, [], `外部请求:${JSON.stringify(rec.cdpExt)}`);
        console.log('     🔵 刻度自证过:1.077 / 1.100 都判不合格,16.54 判合格,像素与解析差 ≤0.10 ⇒ 尺子有牙且准');
      } finally { await ctx.close().catch(() => {}); }
    });

    // ── D4 阴格 B(真靶):暗色下把正文压到 1.10 ⇒ 必须被判不合格 ─────────────
    await t('D4 🔴 阴格 B(真靶):暗色下注入 ~1.10 的正文配色 ⇒ **必须**被判不合格(不红=探针没牙)', async () => {
      const r = await measureTheme('dark', 'dark-mut', 'dark110');
      const body = r.rows.find((x) => /正文基础色/.test(x.label));
      assert.ok(body, '装置:没量到"正文基础色"那一行');
      console.log(`\n     变异体正文基础色 = ${body.value}(期望 ≈1.10)· 判定 ${body.ok ? '合格' : '不合格'}`);
      assert.strictEqual(body.ok, false,
        `🔴 阴格没牙:正文被压到 ${body.value}(≈1.10)却**仍被判合格** ⇒ 本装置量不出"暗色下正文不可读",D1/D2 的所有"合格"都不可信`);
      assert.ok(Math.abs(body.value - 1.10) <= 0.10,
        `🔴 变异没落到预期位置:实测 ${body.value},期望 1.10±0.10 ⇒ 变异 CSS 被别的规则盖掉了,这一格是空的`);
      // 🔴 阳性对照的等待条件必须与变异无关(纪律):上面用的都是 computed/几何量,不依赖"正文正常时的产物"
      console.log('     🔵 真靶阴格过:同一套量法在 1.10 的正文上报"不合格" ⇒ D2 的报数有分辨力');
    });

    // ── D6 定因:暗色下图标那个白色**从哪来**(--text?还是 UA canvastext?) ──────────
    await t('D6 🔵 定因:暗色下工具栏图标是 `rgb(255,255,255)`,而 `--text` 是 `#eef2f6` ⇒ 白色**不是**来自 --text', async () => {
      const base = report.find((r) => r.theme === 'dark' && !r.mut);
      assert.ok(base, '装置:D2 没跑成 ⇒ 没有基线');
      console.log(`\n     基线(暗色,未变异):图标 color=${base.m.els.toolbarIcon.color} · button color=${base.m.els.toolbarIcon.buttonColor}`
        + ` · --text=${base.m.vars['--text']} · colorScheme(body/html)=${base.m.els.body.colorScheme}/${base.m.els.body.htmlColorScheme}`);
      assert.strictEqual(base.m.els.toolbarIcon.color, 'rgb(255, 255, 255)',
        `守卫:基线图标色不是纯白(实测 ${base.m.els.toolbarIcon.color})⇒ 下面这条定因实验的前提变了,别照抄结论`);
      const { ctx, page } = await open(browser, port, '?theme=dark&mut=schemelight');
      try {
        const m = await page.evaluate('window.__dwMeasure()');
        console.log(`     强制 color-scheme:light 后:图标 color=${m.els.toolbarIcon.color} · colorScheme=${m.els.body.colorScheme}`
          + ` · --bg=${m.vars['--bg']}(必须仍是暗色,否则这腿量的不是同一个主题)`);
        assert.strictEqual(m.vars['--bg'], base.m.vars['--bg'],
          `🔴 变异顺带把主题变量也改了(--bg ${m.vars['--bg']} ≠ ${base.m.vars['--bg']})⇒ 这腿分不清"是 color-scheme 的作用"还是"主题塌了"`);
        assert.notStrictEqual(m.els.toolbarIcon.color, 'rgb(255, 255, 255)',
          `🔴 强制 color-scheme:light 之后图标**仍是纯白**(${m.els.toolbarIcon.color})⇒ "白色来自 UA canvastext" 这个归因**不成立**,如实报`);
        console.log(`     🔵 **定因成立(实测,不是读码)**:图标色随 \`color-scheme\` 翻转 ⇒ 它来自 **UA 的 canvastext**,不是本仓 \`--text\`。`
          + `\n        本仓两个主题都设了 color-scheme(renderer-canary-sse.html:9 \`light\` / :27 \`dark\`)⇒ 常态图标两种主题都对;`
          + `\n        🔴 出事的是 **hover/active**:EasyMDE 给这两种态写死了 \`background:#fcfcfc\`,而图标色仍随 color-scheme 走 ⇒ 暗色下白压白。`);
      } finally { await ctx.close().catch(() => {}); }
    });

    // ── D7 候选修法(**只实测,不落产品**) ─────────────────────────────────────
    await t('D7 🔵 候选修法实测:把 EasyMDE 写死的 hover/active 背景换成本仓 `--panel-2`/`--line`(**未落产品**)', async () => {
      const r = await measureTheme('dark', 'dark-fix', 'darkfix');
      const hov = r.rows.find((x) => /真 hover/.test(x.label));
      const act = r.rows.find((x) => /真点击/.test(x.label));
      assert.ok(hov && act, '装置:没量到 hover/active 两行');
      console.log(`\n     真 hover ${hov.value} · 真点击 .active ${act.value}(未修时两者都是 1.026)`);
      assert.ok(hov.value >= TH_UI && act.value >= TH_UI,
        `🔴 候选修法**不够**:hover ${hov.value} / active ${act.value},门槛 ${TH_UI} ⇒ 别把这条写成"已解决"`);
      console.log(`     🔵 实测结论:一条规则 \`button.active,button:hover{background:var(--panel-2);border-color:var(--line)}\``
        + ` 把 hover/active 从 **1.026** 抬到 **${hov.value} / ${act.value}**(门槛 ${TH_UI})⇒ 够用。`
        + `\n        🔴 但这是**候选修法实测,未落产品**(边界:只验不引入);观感归业主,要不要这么改由组长/业主定。`);
    });


    // ── D5 附带:元素级级联(三腿**定因**,不是两腿猜因) ────────────────────────
    await t('D5 🔵 附带实测:本仓**元素级**选择器有没有级联进 EasyMDE 的 DOM(三腿定因 · 只报数不判定)', async () => {
      const withCss = report.find((r) => r.theme === 'dark' && !r.mut);
      assert.ok(withCss, '装置:D2(暗色)那腿没跑成 ⇒ 没有"全量本仓 CSS"的对照,先修 D2');
      const legs = {};
      for (const mode of ['0', 'bodyonly', '1']) {
        const { ctx, page } = await open(browser, port, `?theme=dark&pcss=${mode}`);
        try { legs[mode] = await page.evaluate('window.__dwMeasure()'); } finally { await ctx.close().catch(() => {}); }
      }
      // 🔴 **自审更正**:这段原先写的是「本仓 bare `pre{padding:12px;color:var(--code-text)}` **会**命中
      //    `.CodeMirror-line`,而 CodeMirror 自己那条**没有**写 padding」—— 后半句是**错的**,是我读码没读到:
      //    `vendor/easymde.min.css` 里实测有
      //      `.CodeMirror pre.CodeMirror-line,.CodeMirror pre.CodeMirror-line-like{padding:0 4px}`
      //      `.CodeMirror pre.CodeMirror-line,…{…font-family:inherit;font-size:inherit;margin:0;white-space:pre;
      //                                          line-height:inherit;color:inherit;overflow:visible;…}`
      //    ⇒ 特异性 (0,2,1) 全面压过 bare `pre` 的 (0,0,1)。padding/color/overflow 三行留在表里,
      //    🔵 它们是**实测出来的阴性对照**(证明"打不到"是量出来的,不是我推的)。
      //    `maxHeight` 同理留着:产品那条 bare `pre` **根本没有** max-height ⇒ 它是一条**空对照**,
      //    🔴 它"没变"证明不了任何事(这条我自己标出来,免得下一个人把它当证据)。
      // 🔵 keys 的取法:产品那条 bare `pre`(`renderer-canary-sse.html:990`)的 **10 个声明逐项都要有观测量**
      //    (margin/padding/overflow-x/max-width/color/font-family/font-size/line-height/white-space/tab-size),
      //    另加 `maxHeight`/`wordBreak` 两个**空对照**(产品没写它们 ⇒ 它们"没变"证明不了任何事,只是防漏)。
      const keys = ['fontSize', 'lineHeight', 'fontFamily', 'color', 'padding', 'maxWidth', 'tabSize', 'maxHeight', 'overflow', 'overflowX', 'whiteSpace', 'wordBreak', 'margin'];
      const A = legs['0'].line, B = legs['bodyonly'].line, C = legs['1'].line;
      assert.ok(A && B && C, `守卫:某一腿没量到 .CodeMirror-line(A=${!!A} B=${!!B} C=${!!C})⇒ 编辑器没起来,别把"没变"读成"没冲突"`);
      // 🔴 守卫:每一个量都必须**真的量到了**(不是 undefined)—— 否则 undefined === undefined 会被读成"没变",
      //    而"没变"和"没量"在表格里长得一模一样。(第一版 keys 里有 padding,而 __dwMeasure 没量 padding ⇒ 假阴性)
      keys.forEach((k) => assert.ok(A[k] !== undefined && B[k] !== undefined && C[k] !== undefined,
        `🔴 守卫:${k} 三腿里有 undefined(A=${A[k]} B=${B[k]} C=${C[k]})⇒ 这个量根本没量,别把它读成"没被改写"`));
      console.log('\n     `.CodeMirror-line`(它**是** `<pre>` ⇒ 本仓 bare `pre{}` 有机会命中它)—— 三腿对照:');
      console.log('       量              ①无本仓CSS        ②只变量集+body    ③全量本仓CSS     由谁改的');
      let byBody = 0, byOther = 0;
      for (const k of keys) {
        const a = String(A[k]), b = String(B[k]), c = String(C[k]);
        let who = '—';
        if (b !== a && c === b) { who = '🔴 body{font:…}'; byBody += 1; }
        else if (c !== b) { who = '🔴 其它元素级规则'; byOther += 1; }
        console.log(`       ${k.padEnd(15)} ${a.slice(0, 17).padEnd(17)} ${b.slice(0, 17).padEnd(17)} ${c.slice(0, 16).padEnd(16)} ${who}`);
      }
      // 🔴 实测枚举产品 CSS 里含 `pre` 的规则,并**区分 bare 与带前缀**。
      //    (第一版我用 /(?:^|[\\s;}])pre\\s*\\{/ 枚举,把 `.attachment pre` 这种**带前缀**的也算成了 bare
      //     ⇒ 报了"4 条 bare pre",其中只有 1 条真是 bare。**枚举方法错 = 证据错**,已改成按选择器解析。)
      const scan = (cssText) => {
        const found = []; let last = 0;
        for (const m of /[{};]/g.exec ? cssText.matchAll(/[{};]/g) : []) {
          if (m[0] === '{') {
            const sel = cssText.slice(last, m.index).trim();
            const end = cssText.indexOf('}', m.index);
            found.push({ sel: sel.replace(/\\s+/g, ' '), body: cssText.slice(m.index + 1, end < 0 ? undefined : end).replace(/\\s+/g, ' ').trim() });
            last = m.index + 1;
          } else { last = m.index + 1; }
        }
        return found;
      };
      const all = scan(prod.css).filter((r) => r.sel.split(',').some((p) => /(^|\\s)pre$/.test(p.trim())));
      const bare = all.filter((r) => r.sel.split(',').some((p) => p.trim() === 'pre'));
      const scoped = all.filter((r) => !bare.includes(r));
      console.log(`\n     产品 CSS 里含 \`pre\` 的规则(按选择器解析):**bare ${bare.length} 条** · 带前缀 ${scoped.length} 条`);
      bare.forEach((r, i) => console.log(`       bare[${i + 1}] ${r.body.slice(0, 160)}`));
      scoped.forEach((r, i) => console.log(`       带前缀[${i + 1}] \`${r.sel.slice(-60)}\` → ${r.body.slice(0, 90)}`));
      assert.ok(bare.length > 0,
        '守卫:产品 CSS 里**一条 bare `pre` 都没有** ⇒ 这一格的前提变了(本仓不再有能命中 .CodeMirror-line 的元素级规则),别照抄结论');
      console.log(`     🔵 **定因(三腿实测,不是读码)**:被改写的 ${byBody + byOther} 项里 **${byBody} 项在"只注入变量集+body"那腿就已经变了**`
        + ` ⇒ 来自本仓 \`body{font:14px/1.55 ui-sans-serif,…}\`,而 CodeMirror **自己**写了 \`font:inherit\`/\`font-size:inherit\`/\`line-height:inherit\``
        + ` ⇒ 它**主动**继承页面字体(所以编辑器跟着本仓字体走,这本身是好事)。其余 ${byOther} 项由别的元素级规则改。`);
      console.log(`     🔴 **我原先的读码预期被实测推翻(两处)**:`
        + `\n        (1) 我原以为本仓那条 bare \`pre\`(产品件 \`view-service/renderer-canary-sse.html:990\`,锚 8bfa7197;`
        + `声明是 \`margin:0;padding:12px;overflow-x:auto;max-width:100%;color:var(--code-text);font-family:ui-monospace…;`
        + `font-size:13px;line-height:1.55;white-space:pre;tab-size:4\`)会命中 \`.CodeMirror-line\`(它确实是 \`<pre>\`)`
        + ` ⇒ 实测 **padding/color/overflow-x/white-space/margin 一个都没被改写**;`
        + `\n            挡住它的是 \`vendor/easymde.min.css\` 里的 \`.CodeMirror pre.CodeMirror-line{padding:0 4px}\` 与`
        + ` \`.CodeMirror pre.CodeMirror-line{…color:inherit;overflow:visible;white-space:pre;margin:0…}\`(特异性 0,2,1 > 0,0,1)。`
        + `\n            (实测 \`whiteSpace\` 是 \`pre-wrap\` 不是 \`pre\` ⇒ 赢的是 EasyMDE 的 lineWrapping 那条同特异性、更靠后的规则。)`
        + `\n        (2) 我在**本装置的注释里**写过"CodeMirror 自己那条没有写 padding" ⇒ 🔴 **那是错的**,已就地更正(见 keys 上方)。`);
      console.log(`     🔵 这是本组今天第 N 次同型:**读码推出的结论必须实测核实** —— 而这次被推翻的是我自己的预期,不是产品的行为。`);
      console.log(`     🔴 口径:只到"这些量被改写了 / 由谁改的",**没验**它是否导致可见的渲染破损(那要另开一格)。`);
      assert.ok(byBody > 0,
        '守卫:"只注入变量集+body"那腿一个量都没改 ⇒ pcss=bodyonly 没生效(装置问题),不能把"没变"读成"没冲突"');
    });

    // ── D8 风险显影:那条**现在没命中**的 bare `pre{color:var(--code-text)}`,若被提特异性会怎样 ──
    await t('D8 🔴 风险显影:给本仓 bare `pre` 加 `!important`(模拟"有人提特异性")⇒ 正文对比度会掉到多少', async () => {
      const base = report.find((r) => r.theme === 'dark' && !r.mut);
      assert.ok(base, '装置:D2 没跑成 ⇒ 没有基线');
      // 🔴 装置自审:这一格第一版红过,原因是我调了一个 **MUTS 里根本不存在的键**(`precascade`)⇒ 注入为空,
      //    量的是未变异的页面;而 D8 那道"仍合格就是反常"的守卫**把它抓住了**(红在守卫,不是红在几何)。
      //    第二版另加一条**归因守卫**:变异必须真的打到 `pre` 上,否则一律判装置问题 ——
      //    🔵 本组规矩:**"没变"不许被读成"没风险"**,也不许被读成"有牙"。
      const cmBgOf = (r) => rgb(r.m.els.codeMirror.bg).v;
      const lineRatio = (r) => ratio(rgb(r.m.line.color).v, cmBgOf(r));
      const rowOf = (r, re) => { const x = r.rows.find((y) => re.test(y.label)); assert.ok(x, `装置:没量到 ${re} 那一行`); return x; };
      const worst = (r) => rowOf(r, /正文最坏 token/);
      console.log(`\n     基线(暗色):pre.CodeMirror-line 色 ${base.m.line.color} · 编辑区 bg ${base.m.els.codeMirror.bg}`
        + ` · --code-text=${base.m.vars['--code-text']} · --code-bg=${base.m.vars['--code-bg']}`);
      console.log(`     基线继承统计:编辑区 cm-span ${base.m.inherit.spans} 个,其中**与 pre 同色(即继承)** ${base.m.inherit.inheriting} 个`
        + `(样例继承 ${JSON.stringify(base.m.inherit.sampleInherit)} · 样例自带色 ${JSON.stringify(base.m.inherit.sampleOwn)})`);

      const r = await measureTheme('dark', 'dark-pre', 'precascade');
      assert.strictEqual(r.m.vars['--bg'], base.m.vars['--bg'],
        `🔴 变异顺带改了主题变量(--bg ${r.m.vars['--bg']} ≠ ${base.m.vars['--bg']})⇒ 分不清是 pre 的作用还是主题塌了`);
      // 归因守卫:变异**必须**打到 pre 上(否则这一格量的是基线,后面所有数字都没有意义)
      assert.notStrictEqual(r.m.line.color, base.m.line.color,
        `🔴 归因守卫:加了 \`pre{color:var(--code-text) !important}\` 之后 pre.CodeMirror-line 的颜色**没变**`
        + `(仍是 ${base.m.line.color})⇒ 变异没打进去(装置问题),不能把"没变"读成"没风险"`);
      const codeText = rgb(r.m.vars['--code-text']);
      assert.ok(codeText, `装置:--code-text 解析不了(${r.m.vars['--code-text']})⇒ 修探针,别拿它归因`);
      assert.deepStrictEqual(rgb(r.m.line.color).v, codeText.v,
        `🔴 归因守卫:pre 色变成了 ${r.m.line.color},**不是** --code-text(${r.m.vars['--code-text']})⇒ 打进去的不是我这条变异`);

      // 三层观测,一层都不省
      console.log(`     ① CSS/pre 层:pre.CodeMirror-line 色 ${base.m.line.color} → **${r.m.line.color}**`
        + ` ⇒ 对编辑区 bg 的对比度 ${R(lineRatio(base))} → **${R(lineRatio(r))}**(门槛 ${TH_TEXT})`);
      assert.ok(lineRatio(r) < TH_TEXT,
        `🔴 反常:pre 色已经是 --code-text(${r.m.line.color})了,它对写死的白底却仍 ≥${TH_TEXT}(${R(lineRatio(r))})⇒ 先查装置`);
      console.log(`     ② token 层:最坏 token ${worst(base).value} → **${worst(r).value}**`
        + ` · 继承 pre 色的 span ${base.m.inherit.inheriting}/${base.m.inherit.spans} → **${r.m.inherit.inheriting}/${r.m.inherit.spans}**`);
      console.log(`     ③ 像素层:编辑区最暗像素 ${JSON.stringify(base.pxCm.darkest)} → **${JSON.stringify(r.pxCm.darkest)}**`
        + ` · contrast_max ${base.pxCm.contrast_max} → **${r.pxCm.contrast_max}**`);
      console.log(`     🔵 像素层为什么**证不了**"正文没事":contrast_max 取的是整块最暗/最亮对,`
        + `**部分**文字变成不可见时它一动不动(实测 ${base.pxCm.contrast_max} → ${r.pxCm.contrast_max})⇒ 这一维只能靠 ② 按 span 数。`);

      const moved = Math.abs(worst(r).value - worst(base).value) > 0.05;
      if (moved) {
        assert.strictEqual(r.m.inherit.inheriting > 0, true,
          `🔴 反常:最坏 token 动了(${worst(base).value}→${worst(r).value})却一个继承 span 都没有 ⇒ 归因链断了,先查装置`);
        console.log(`     🔴 **实测后果(可见层)**:${r.m.inherit.inheriting}/${r.m.inherit.spans} 个 cm-span 跟着 pre 变成 ${r.m.line.color},`
          + ` 最坏 token 从 ${worst(base).value} 掉到 **${worst(r).value}** ⇒ "近白压白",业主会**立刻看见**部分正文消失。`);
      } else {
        assert.strictEqual(r.m.inherit.inheriting, 0,
          `🔴 反常:有 ${r.m.inherit.inheriting} 个 span 继承 pre 色,最坏 token 却纹丝不动`
          + `(${worst(base).value}→${worst(r).value})⇒ 归因链断了,先查装置`);
        console.log(`     🔵 **实测后果(可见层)**:**没有**任何 cm-span 继承 pre 色(${r.m.inherit.inheriting}/${r.m.inherit.spans}),`
          + ` 每个 span 都自带 CodeMirror 主题色 ⇒ 这一发变异在**渲染出来的文字上是空操作**。`
          + `\n        🔴 但"空操作"是有边界的:它成立**只因为**当前样本里每个 span 都自带颜色;`
          + `换一段没有 token 着色的文本(或 CM 主题改色策略)就会露出来 ⇒ 见交付件对这一格的登记口径。`);
      }
      console.log(`     🔵 **这条防线的性质**:现在挡住 bare \`pre\` 的是 CodeMirror \`.CodeMirror pre.CodeMirror-line{color:inherit}\` 的**特异性**`
        + `(D5 已实测),**不是**任何一方有意设计;而 \`!important\` 会**直接越过**特异性。`);
      console.log(`     🔵 引入时的对策(供裁,未落地):把本仓那条 \`pre\` 收窄成 \`:not(.CodeMirror-line)\`,或给编辑区显式写 \`color\`;`
        + `并且**必须有一条闸钉住这个组合**(否则它就是下一个 U7)。`);
    });

  } finally {
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }

  // ── 汇总:两种主题的判定表 ────────────────────────────────────────────────
  console.log('\n══ 汇总(同一套量法,两种主题) ══');
  for (const r of report.filter((x) => !x.mut)) {
    console.log(`\n【data-theme="${r.theme}"】`);
    r.rows.forEach((x) => { if (x.text) console.log('  ' + x.text); else console.log(`  🔵 ${x.label} = ${x.value}(只报数,不判定)`); });
    console.log(`  像素层交叉核对 编辑区 ${r.pxCm.contrast_max} · 工具栏 ${r.pxTb.contrast_max} · 激活钮 ${r.pxBtn.contrast_max}`);
  }
  console.log('\n  🔵 这条绿【看不见什么】:');
  console.log('     ① 口径止于「**这套配置、这份产品 CSS、这个视口下的实测对比度**」—— 🔴 **不是**「暗色没问题」;');
  console.log('     ② 门槛 WCAG 2.1 AA(4.5 / 3.0)**是我选的**;🔵 参照 —— 用**同一把尺子**量本仓自己的既有配色:');
  report.filter((x) => !x.mut).forEach((r) => {
    const v = r.m.vars;
    const lineBg = ratio(rgb(v['--line']).v, rgb(v['--bg']).v);
    const mutedPanel = ratio(rgb(v['--muted']).v, rgb(v['--panel']).v);
    const textBg = ratio(rgb(v['--text']).v, rgb(v['--bg']).v);
    console.log(`        data-theme="${r.theme}":--line vs --bg = ${R(lineBg)} · --muted vs --panel = ${R(mutedPanel)} · --text vs --bg = ${R(textBg)}`);
  });
  console.log('        ⇒ 🔴 **"低于 3.0"不等于"缺陷"**:本仓自己的边框数字也在这个量级(业主已认可 0.2.1.7 配色);');
  console.log('          **门槛归业主/组长**,我只负责把数字量准、把两项分开报。');
  console.log('     ③ 只量了默认工具栏(去掉 guide)+ 一个 markdown 样本;图片上传、预览面板、全屏/分屏态**未量**;');
  console.log('     ④ 暗色**没有**自写 CSS 覆盖(裁定:自写 CSS 是引入时的工作量)⇒ 这里量的是**原样撞车**的结果;');
  console.log('     ⑤ 隔离仍是**浏览器层**(死端口代理),与 ts-No.77 同一条边界。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
