// 🔵 ts-No.78 两个探针**共用**的量测核 —— **单一事实源**(与 `vendor-pins.js` 同一个理由:
//    两份尺子会各自漂移,而"漂移"看起来和"发现"一模一样)。
//    用它的两件:`dark-contrast-probe.js`(D0–D8,独立页 body 形状)·
//                `console-iframe-dark-probe.js`(C0–C4,真 console 的 iframe 形状 = 业主真实走的那条路)
//
// 🔴 门槛的**适用对象**由组长 ts-No.78 后裁定 (1) 限定(逐字):
//      适用 4.5/3.0   **承载信息**的元素:正文、图标、状态色、可点控件的可辨识特征
//      🔴 不适用      **纯装饰**的边框/分隔线/网格线 —— 低于门槛**不判缺陷**
//      🔴 闸里必须    判定行**同时打印本仓同尺参照值**,免得下一个人把"低于 4.5"直接读成缺陷
//    ⇒ 所以本文件里每一行观测都带 `dim`(信息维 / 装饰维),装饰维**只报数**;
//      `judge()` 不再自己决定判不判,由 `dim` 决定 —— 🔵 **把裁定写进代码,不靠读日志的人记得**。
'use strict';

const path = require('path');
const { execFileSync } = require('child_process');

const HERE = __dirname;

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

// ── 维度(裁定 (1)):信息维判定,装饰维只报数 ─────────────────────────────────
const DIM_INFO = '信息维', DIM_DECO = '装饰维';

// 🔴 每一个观测量都必须**先归维**,否则 `judge()` 拒绝出判定(缺维 = 装置问题,不是"默认判缺陷")。
function judge(label, value, th, kind, dim, refNote) {
  if (dim !== DIM_INFO && dim !== DIM_DECO) {
    throw new Error(`🔴 装置:"${label}" 没有归维(dim=${dim})⇒ 按裁定 (1) 我无权决定它判不判缺陷,先归维再跑`);
  }
  if (dim === DIM_DECO) {
    return {
      label, value: R(value), th, kind, dim, ok: null,
      text: `🔵 ${label} = ${R(value)} : 1(**装饰维** ⇒ 按裁定 (1) 不判缺陷;门槛 ${th} 只作参照${refNote ? ` · 本仓同尺参照 ${refNote}` : ''})`
    };
  }
  const ok = value >= th;
  return {
    label, value: R(value), th, kind, dim, ok,
    text: `${label} = ${R(value)} : 1 门槛 ${th}(${kind}·信息维)⇒ ${ok ? '✅ 合格' : '🔴 低于门槛'}${refNote ? `(本仓同尺参照 ${refNote})` : ''}`
  };
}

// 观感边界差(**不是**可读性判据)—— 一直只报数
const reportOnly = (label, value) => ({ label, value: R(value), th: null, kind: '观感', dim: null, ok: null, text: '' });

// 判定行的**同尺参照**:用同一把尺子量本仓自己的 --line vs --bg(裁定 (1) 要求"判定行同时打印")
function repoRef(vars) {
  const l = rgb(vars['--line']), b = rgb(vars['--bg']);
  return (l && b) ? `--line vs --bg=${R(ratio(l.v, b.v))}` : '(取不到)';
}

// ── 被测样本与工具栏(**两个探针必须用同一份**,否则 token 层的数字不可比)──────
const TOOLBAR = '["bold","italic","heading","|","quote","unordered-list","ordered-list","|","link","image","|","preview","side-by-side","fullscreen"]';

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

// ── 页内量测函数(逐字共用;🔴 改它 = 同时改两个探针的观测量)─────────────────
const MEASURE_LINES = [
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
];

// ── 🔴 阴格(真靶)配方 —— **两个探针共用同一条** ─────────────────────────────
//    共用不是为了省事:如果两处各写一条,"探针有牙"在两条路径上就是**两句话**,
//    而其中一句可能是假的(与 U7 / ㉒ 同族:机制在,但不在执行路径上)。
const MUT_DARK110 = '.EasyMDEContainer .CodeMirror{background:#111316 !important;color:#1a1d21 !important}'
  + '.EasyMDEContainer .CodeMirror .cm-s-easymde span,.CodeMirror-line{color:#1a1d21 !important}';

// ── 观测量的**集合**也只能有一个源 ───────────────────────────────────────────
//    🔴 行集合 = 判据本身。两个探针各写一份 rows,就等于两条路径量的是**两件事**,
//      而它们的日志看起来一模一样。所以这里统一产出。
function tokenTable(m) {
  const cmBg = rgb(m.els.codeMirror.bg).v;
  const t = Object.keys(m.tokens).map((c) => {
    const v = ratio(rgb(c).v, cmBg);
    return { color: c, ratio: v, n: m.tokens[c].n, classes: Object.keys(m.tokens[c].classes).slice(0, 3).join(' ') };
  }).sort((a, b) => a.ratio - b.ratio);
  if (!t.length) {
    throw new Error(`守卫:一个 token 色都没量到(tokenCount=${m.tokenCount})⇒ CodeMirror 没渲染出带 class 的 span,这一格是空的`);
  }
  return t;
}

// `hov` / `actReal` 由调用方用**真事件**取得(page.hover / page.click),这里只负责算与判。
// 🔴 不接受 classList.add 那种测试缝当输入(缝只能验"我加的类会触发那条 CSS")。
function buildRows(m, hov, actReal) {
  if (!m || !m.els || !m.els.codeMirror || !m.els.body) throw new Error('守卫:量测结果缺元素(编辑器没起来?)⇒ 这一格是空的');
  const ref = repoRef(m.vars);
  const varBg = rgb(m.vars['--bg']), bodyBg = rgb(m.els.body.bg);
  if (!varBg || !bodyBg) throw new Error(`守卫:颜色解析不了(body=${m.els.body.bg} --bg=${m.vars['--bg']})⇒ 解析器不认这个写法,先修探针`);
  // 🔴 守卫:主题**真的生效了**吗 —— body 背景必须等于该主题的 --bg(否则量的是另一个配色)
  if (JSON.stringify(bodyBg.v) !== JSON.stringify(varBg.v)) {
    throw new Error(`🔴 主题没生效:body 背景 ${m.els.body.bg} ≠ --bg ${m.vars['--bg']} ⇒ 这一格量的是**另一个**配色,数字不可用`);
  }
  const cmBg = rgb(m.els.codeMirror.bg);
  const tokenRows = tokenTable(m);
  const rows = [];
  // ① 正文:编辑器自己的背景 vs 每一个 token 颜色(取最坏)
  rows.push(judge(`正文最坏 token(${tokenRows[0].classes || tokenRows[0].color})`, tokenRows[0].ratio, TH_TEXT, '正文', DIM_INFO, ref));
  rows.push(judge('正文基础色(.CodeMirror color vs bg)', ratio(rgb(m.els.codeMirror.color).v, cmBg.v), TH_TEXT, '正文', DIM_INFO, ref));
  // ② 工具栏常态图标
  rows.push(judge('工具栏图标(常态)', ratio(rgb(m.els.toolbarIcon.color).v, rgb(m.els.toolbarIcon.bg).v), TH_UI, 'UI 图标', DIM_INFO, ref));
  // ③ hover / 激活态(真事件取得)
  rows.push(judge('工具栏图标(**真 hover** 态)', ratio(rgb(hov.icon).v, rgb(hov.bg).v), TH_UI, 'UI 图标', DIM_INFO, ref));
  rows.push(judge('工具栏图标(**真点击 .active** 态)', ratio(rgb(actReal.icon).v, rgb(actReal.bg).v), TH_UI, 'UI 图标', DIM_INFO, ref));
  // ④ 边框(裁定 (1):纯装饰 ⇒ 只报数)
  rows.push(judge('编辑器边框 vs 页面背景', ratio(rgb(m.els.codeMirror.borderColor).v, bodyBg.v), TH_UI, '边框', DIM_DECO, ref));
  rows.push(judge('工具栏边框 vs 页面背景', ratio(rgb(m.els.toolbar.borderTopColor).v, bodyBg.v), TH_UI, '边框', DIM_DECO, ref));
  // ⑤ 状态栏文字(承载信息)
  if (m.els.statusbar) rows.push(judge('状态栏文字', ratio(rgb(m.els.statusbar.color).v, rgb(m.els.statusbar.bg).v), TH_TEXT, '正文', DIM_INFO, ref));
  // ⑥ 分隔线(纯装饰)
  if (m.els.separator) {
    rows.push(judge('工具栏分隔线 left vs 页面背景', ratio(rgb(m.els.separator.borderLeftColor).v, bodyBg.v), TH_UI, '边框', DIM_DECO, ref));
    rows.push(judge('工具栏分隔线 right vs 页面背景', ratio(rgb(m.els.separator.borderRightColor).v, bodyBg.v), TH_UI, '边框', DIM_DECO, ref));
  }
  // ⑦ 观感边界差(**不是**可读性判据)
  rows.push(reportOnly('编辑器块 vs 页面背景(🔵 观感边界差,**不是**可读性判据)', ratio(cmBg.v, bodyBg.v)));
  return { rows, tokenRows, ref, bodyBg, cmBg };
}

// 两条路径(或两发变异)的**信息维判定集合**对照 —— 裁定 (3) 那条腿的判据
function verdictSet(rows) {
  return rows.filter((r) => r.dim === DIM_INFO).map((r) => `${r.label}=${r.value}:${r.ok ? 'ok' : 'LOW'}`).sort();
}

// `frame.evaluate` 用的形态:先装上 __dwMeasure,再调用它取返回值
const MEASURE_EVAL = '(() => {' + MEASURE_LINES.join('\n') + '\n return window.__dwMeasure(); })()';

module.exports = {
  HERE, TH_TEXT, TH_UI, DIM_INFO, DIM_DECO,
  lin, lum, ratio, R, rgb, px, judge, reportOnly, repoRef,
  MD_SAMPLE, TOOLBAR, MEASURE_LINES, MEASURE_EVAL,
  MUT_DARK110, tokenTable, buildRows, verdictSet
};
