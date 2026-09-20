// 🔴 自研 markdown 渲染的**注入断言**(板件 ts-No.76;组长裁定④:「它验的是【现在就在线上跑】的那份代码」)
//
// 钉的是这条不变量:
//   **事件正文里的 HTML/脚本/危险协议形状,经自研 markdown 渲染后不得变成活的东西** ——
//   不执行、不产生 `on*` 事件属性、不产生 `javascript:`/`vbscript:`/`data:text/html` 协议的可点链接。
//
// ── 为什么是这一件(而不是"换个成熟组件")────────────────────────────────────────
//   ts-No.75 调研里量到:现有自研渲染是「**先转义再 markdown**」——
//     `renderInline` 第一句就是 `escapeHtml(src)`,链接正则只放行 `https?://`,
//     `highlightCode` 亦先 `escapeHtml(code)`,表格/列表单元格一律经 `renderInline`。
//   而 marked 自己 README 逐字写着不 sanitize、markdown-it 的 `html` 默认值随 preset 变
//   ⇒ 组长据此裁定 **本期组件不接管展示**,展示仍走自研渲染。
//   🔴 但那是**读码结论**。本件把它变成**实测**:真灌、真渲染、真扫 DOM。
//
// ── 🔴 结论口径(组长明令,逐字守住)─────────────────────────────────────────────
//   本件**只能**得出「**这些形状不成立**」,**不得升级成「无 XSS」**。
//   理由:形状清单是我列的,不是穷尽的;路径也只覆盖 markdown 那几条(见文末「没验到什么」)。
//
// ── 真灌,不走测试缝 ─────────────────────────────────────────────────────────────
//   payload 一律作为**真 SSE 事件**(`assistant_text` / `user_input`)灌进去,走产品自己的
//   流式渲染路径与回合重建路径,**不调用 renderMarkdown()、不塞 innerHTML**。
//   🔵 这与 U7 那格同一个教训:走测试缝 ⇒ 真正要验的那段代码从未被执行。
//
// ── 阳性对照(组长硬性要求:「不红说明你的探针没有分辨力」)───────────────────────
//   四条,**各打不同的一层**;都在**本进程内合成**(每处锚点须恰好命中一次才用),
//   不依赖任何人的 /tmp;锚点过期 ⇒ **报错,不静默跳过**:
//     PC1  M-I1 `escapeHtml` 掏空            ⇒ 必须观测到"活的注入"(on* 属性 / 活元素 / canary 被置)
//     PC2  M-I2 链接正则放开协议(去掉 https?://) ⇒ 必须观测到 `javascript:` href
//     PC3  M-I3 `highlightCode` 掏空          ⇒ 必须观测到代码围栏里出现活元素
//     PC4  M-I4 `data-code` 不再 encodeURIComponent ⇒ 必须观测到 Copy 按钮那个属性被突破
//   🔵 PC2 另有一道**分层守卫**:只放开协议时**不得**顺带出现 on* 属性 —— 否则破坏不是来自我改的那一处,归因不成立。
//   🔴 PC 腿若"没观测到破坏",红的是**探针**,不是产品 —— 失败文本会这么说。
//
// ── 反证腿(防"渲染成空也算通过")─────────────────────────────────────────────────
//   I6:正常 markdown(**粗体** / 链接 / `code` / 表格 / 任务列表 / 代码高亮)**必须仍渲染出来**。
//   🔴 没有 I6,一个"把正文渲染成空字符串"的改动会让本件全绿 —— 那不是安全,那是瞎了。
//
// ── 显影腿 ───────────────────────────────────────────────────────────────────────
//   I7:钉**现状形状** —— markdown 图片语法 `![alt](url)` 现在**不**被渲染成 `<img>`
//   (自研 renderInline 只处理 `` ` ``、链接、粗体、斜体、换行;**没有图片分支**)。
//   🔵 这一格不是"该不该",是"现在是什么":它变了(有人补了图片支持)⇒ 红 ⇒ 报组长,
//      并按 ts-No.75 ① 的裁定口径重新对账(业主产品定义里逐字有「预览图片」)。
//
// 装置:本件**自带** HTTP+SSE 夹具(自启自收、自挑空闲端口 listen(0)、路径一律走 __dirname,cwd 无关)。
//   事件语义按 `internal/m8/terminal.go` HandleInternalViewEvents「先回放 eventRing,再挂实时流」模拟。
//   🔴 附件/文件那条路径**不在本件范围**:它已有 `renderer-0206-file-render-regression.js` 覆盖
//      (含白名单形状断言),两件**不要互相冒充**。
//
// 🔴 可被 tools/run-browser-regressions.py 直接加进 SUITES:
//   · 调用方可控的 env 面只有已登记的两个:`PW_CHANNEL` / `RENDERER_OVERRIDE`(不开新面)
//   · rc 与发布约定一致:**0 全过 / 2 断言失败 / 3 环境不可用**
'use strict';

const fs = require('fs');
const crypto = require('crypto');
const assert = require('assert');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');

const HERE = __dirname;
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const OVERRIDE = !!process.env.RENDERER_OVERRIDE;
const SESSION = 'inj-alpha-2642';
const VP = { width: 1280, height: 900 };
const WAIT_MS = 30000;

let RENDERER_BYTES;
try {
  RENDERER_BYTES = fs.readFileSync(RENDERER);
} catch (e) {
  console.error(`环境不可用:读不到 renderer(${RENDERER}):${e.message}`);
  process.exit(3);
}
const RENDERER_SHA = crypto.createHash('sha256').update(RENDERER_BYTES).digest('hex');

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const now = () => Date.now();

// ── payload 目录 ────────────────────────────────────────────────────────────────
// 🔵 canary 一律写 `window.__dwPwned`:它被置上 = 有脚本真执行了(而不是"看起来没执行")。
const CANARY = 'window.__dwPwned=1';
const PAYLOADS = [
  { id: 'P01', text: `<script>${CANARY}</script>`, why: '裸 script 标签' },
  { id: 'P02', text: `<img src=x onerror="${CANARY}">`, why: 'img onerror(最经典:src 必然加载失败 ⇒ 必然触发)' },
  { id: 'P03', text: `<iframe src="javascript:${CANARY}"></iframe>`, why: 'iframe + javascript: 协议' },
  { id: 'P04', text: `<svg onload="${CANARY}"></svg>`, why: 'svg onload' },
  { id: 'P05', text: `<details ontoggle="${CANARY}" open>x</details>`, why: 'ontoggle(open 属性 ⇒ 不需交互就触发)' },
  { id: 'P06', text: `<a href="javascript:${CANARY}">点我</a>`, why: '裸 a + javascript:' },
  { id: 'P07', text: `[x](javascript:${CANARY})`, why: 'markdown 链接塞 javascript: 协议' },
  { id: 'P08', text: `[x](JaVaScRiPt:${CANARY})`, why: '协议名大小写绕过' },
  { id: 'P09', text: '[x](  javascript:' + CANARY + ')', why: '协议前置空白绕过' },
  { id: 'P10', text: `[x](&#106;avascript:${CANARY})`, why: 'HTML 实体绕过(href 里实体会被解码)' },
  { id: 'P11', text: '[x](data:text/html;base64,PHNjcmlwdD53aW5kb3cuX19kd1B3bmVkPTE8L3NjcmlwdD4=)', why: 'data:text/html + base64' },
  { id: 'P12', text: `[x](https://ok.example/"onmouseover="${CANARY})`, why: '🔴 链接 URL 里的**属性突破**(整段无空格无右括号 ⇒ 能穿过 [^)\\s]+ 的捕获)' },
  { id: 'P13', text: `<div style="background:url(javascript:${CANARY})">x</div>`, why: 'style 属性里的 url()' },
  // 🔵 P14 原先写成了 `<body onload="${CANARY}>`(引号没闭合)—— 那是**我写错了**,不是故意的形状;
  //    已改回本意。"不闭合"这个形状由 P17 专门承担。
  { id: 'P14', text: `<body onload="${CANARY}">`, why: 'body onload(整页级)' },
  // 🔵 以下三种是第一跑绿之后**自己加的**:组长说「以及你认为该试的其它形状」。
  { id: 'P15', text: '`<img src=x15 onerror="' + CANARY + '">`', why: '行内代码里的 payload:必须是 <code> 内的**死文本**(反引号提取发生在转义之后)' },
  { id: 'P16', text: `&lt;img src=x16 onerror="${CANARY}"&gt;`, why: '🔴 **双重编码(mXSS)**:送进来时已是转义形态,渲染器若"帮忙解码一次"就会变活' },
  { id: 'P17', text: `<img src=x17 onerror="${CANARY}"`, why: '**不闭合标签**(没有结尾 >):赌解析器宽容度' },
];
const ALL_TEXT = PAYLOADS.map((p) => p.text).join('\n\n');

// ── 反证("被转义了"而不是"被悄悄丢掉了")的期望形式 ────────────────────────────────
// 🔴 这里踩过一次坑,记下来:期望值不能拿 escapeHtml 的输出直接当。
//    payload 被转义后是**文本节点**,浏览器序列化 innerHTML 时只转 `& < >`,**引号原样**;
//    而落在**属性值**里的那部分(P12)才会序列化成 `&quot;`。两种形态不一样,得分开写。
const escTextNode = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
PAYLOADS.forEach((p) => { p.expect = [escTextNode(p.text)]; });
// P12 的 URL 以 https:// 开头 ⇒ 会被链接正则吃掉,`[x](` 前缀不会以文本出现;
// 它真正要钉的是:**引号被转义了**(所以不可能突破出 href 属性变成 onmouseover 属性)。
const P12 = PAYLOADS.find((p) => p.id === 'P12');
P12.expect = ['&quot;onmouseover=&quot;'];
// P15 的反引号会被行内代码规则消费掉 ⇒ 整条 payload 的转义形态不会原样出现,只断言 <code> 里那一段
const P15 = PAYLOADS.find((p) => p.id === 'P15');
P15.expect = [escTextNode('<img src=x15 onerror="' + CANARY + '">')];
P15.inCode = 'src=x15';   // 额外断言:它必须落在 <code> 元素里,而不是散在正文中

// ── 自带夹具:一套路由,按 opt.events 灌真 SSE ─────────────────────────────────────
async function startServer(opt) {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    const html = () => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(opt.bytes || RENDERER_BYTES); };
    if (u === '/status') return json({ session: SESSION, state: 'running', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
    if (u === '/tasks' || u === '/alerts') return json(u === '/alerts' ? [] : {});
    if (u === `/view/${encodeURIComponent(SESSION)}`) return html();
    if (!u.endsWith('/events')) return html();
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
    res.flushHeaders();
    let cur = 0;
    const alive = () => !res.writableEnded && !res.destroyed;
    (async () => {
      try {
        for (const step of opt.events) {
          if (!alive()) return;
          if (step.wait) { await sleep(step.wait); continue; }
          if (step.signal) { opt.signals[step.signal] = true; continue; }
          cur += 1;
          res.write('data: ' + JSON.stringify(Object.assign({ cursor: String(cur).padStart(6, '0'), ts: now() }, step.ev)) + '\n\n');
          if (step.after) await sleep(step.after);
        }
        while (alive()) await sleep(50);
      } catch {}
    })();
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const port = server.address().port;
  return { url: `http://127.0.0.1:${port}/`, port, close: () => new Promise((r) => server.close(r)) };
}

// ── 事件构造 ────────────────────────────────────────────────────────────────────
const initEv = () => ({ ev: { kind: 'state_change', data: { change: 'session_init', session_id: 's1', model: 'm' }, delivery_id: '', turn_id: '' } });
const userEv = (text, id) => ({ ev: { kind: 'user_input', text, delivery_id: id, turn_id: id, stage: 'cli_injected' } });
const asstEv = (text, id) => ({ ev: { kind: 'assistant_text', text, delivery_id: id, turn_id: id } });
const doneEv = (id) => ({ ev: { kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.1 }, delivery_id: id, turn_id: id } });

// ── DOM 扫描:一次取全所有"活没活"的证据 ─────────────────────────────────────────
// 🔵 页面级扫 on*/危险协议是**合法的强断言**:实测产品源文件里 `on*="` 出现 **0** 次、
//    `javascript:` 出现 **0** 次 ⇒ 页面上任何一处出现都只可能来自注入。
async function scan(page) {
  return page.evaluate(() => {
    const BAD_SCHEME = /^(javascript|vbscript|data:text\/html)/;
    const clean = (v) => String(v || '').replace(/[\u0000-\u0020\u00ad]+/g, '').toLowerCase();
    const out = { onAttrs: [], badScheme: [], liveInTimeline: [], mdCount: -1, timelineText: '', timelineHTML: '', codeTexts: [], imgInTimeline: 0, longtext: 0, userCard: 0, legit: {} };
    for (const el of document.querySelectorAll('*')) {
      for (const a of Array.from(el.attributes || [])) {
        if (/^on/i.test(a.name)) out.onAttrs.push(`${el.tagName.toLowerCase()}@${a.name}="${a.value.slice(0, 60)}"`);
        if (/^(href|src|xlink:href|action|formaction|poster|data)$/i.test(a.name) && BAD_SCHEME.test(clean(a.value))) {
          out.badScheme.push(`${el.tagName.toLowerCase()}@${a.name}="${a.value.slice(0, 90)}"`);
        }
      }
    }
    const tl = document.getElementById('timeline');
    if (tl) {
      out.mdCount = tl.querySelectorAll('.md').length;
      out.timelineText = tl.textContent || '';
      out.timelineHTML = tl.innerHTML || '';
      out.codeTexts = Array.from(tl.querySelectorAll('code')).map((el) => el.textContent || '');
      out.imgInTimeline = tl.querySelectorAll('img').length;
      out.longtext = tl.querySelectorAll('.turn-assistant.longtext').length;
      out.userCard = tl.querySelectorAll('.turn-user').length;
      tl.querySelectorAll('script,iframe,object,embed,svg,img,link,meta').forEach((el) => {
        out.liveInTimeline.push(el.tagName.toLowerCase() + (el.getAttribute('src') ? `@src=${String(el.getAttribute('src')).slice(0, 40)}` : ''));
      });
      // 反证腿要看的"正常 markdown 是否仍渲染"
      out.legit = {
        strong: tl.querySelectorAll('strong').length,
        em: tl.querySelectorAll('em').length,
        a: tl.querySelectorAll('a[href^="https://"]').length,
        code: tl.querySelectorAll('code').length,
        pre: tl.querySelectorAll('pre').length,
        table: tl.querySelectorAll('table').length,
        th: tl.querySelectorAll('th').length,
        checkbox: tl.querySelectorAll('input[type=checkbox]').length,
        heading: tl.querySelectorAll('h1,h2,h3').length,
        quote: tl.querySelectorAll('blockquote').length,
        br: tl.querySelectorAll('br').length,
        tokKw: tl.querySelectorAll('.tok-kw').length,
        copyBtn: tl.querySelectorAll('button.copy-code').length,
      };
    }
    out.canary = typeof window.__dwPwned === 'undefined' ? null : String(window.__dwPwned);
    return out;
  });
}

function describe(s) {
  return [
    s.canary ? `🔴 canary 被置(window.__dwPwned=${s.canary})⇒ **有脚本真执行了**` : null,
    s.onAttrs.length ? `🔴 on* 事件属性 ${s.onAttrs.length} 处:${s.onAttrs.slice(0, 4).join(' | ')}` : null,
    s.badScheme.length ? `🔴 危险协议 ${s.badScheme.length} 处:${s.badScheme.slice(0, 4).join(' | ')}` : null,
    s.liveInTimeline.length ? `🔴 消息容器内出现活元素:${s.liveInTimeline.slice(0, 6).join(' | ')}` : null,
  ].filter(Boolean);
}
const breachOf = (s) => describe(s);

// ── 三条阳性对照的变异:**本进程内合成**,锚点必须恰好命中一次 ──────────────────────
const MUTATIONS = {
  'M-I1': {
    label: 'escapeHtml 掏空(恒等返回)',
    steps: [[
      `      return String(value ?? '')
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');`,
      `      return String(value ?? '');`,
    ]],
    // 期望破坏:on* 属性出现 / 消息容器里出现活元素 / canary 被置
    breached: (s) => breachOf(s).length > 0,
    want: 'on* 属性、活元素或 canary 三者之一',
  },
  'M-I2': {
    label: '链接正则放开协议(去掉 https?:// 限制)',
    steps: [[
      `out = out.replace(/\\[([^\\]]+)\\]\\((https?:\\/\\/[^)\\s]+)\\)/g, '<a href="$2" rel="noreferrer" target="_blank">$1</a>');`,
      `out = out.replace(/\\[([^\\]]+)\\]\\(([^)\\s]+)\\)/g, '<a href="$2" rel="noreferrer" target="_blank">$1</a>');`,
    ]],
    breached: (s) => s.badScheme.length > 0,
    want: 'javascript:/data:text/html 协议的 href',
  },
  'M-I3': {
    label: 'highlightCode 掏空(代码围栏内不转义)',
    steps: [[
      `      let html = escapeHtml(code);`,
      `      let html = String(code);`,
    ]],
    breached: (s) => s.liveInTimeline.length > 0 || s.canary !== null || s.onAttrs.length > 0,
    want: '代码围栏里出现活元素(或 canary 被置)',
  },
  'M-I4': {
    label: 'data-code 属性不再 encodeURIComponent(Copy 按钮那个属性)',
    steps: [[
      `      const encoded = encodeURIComponent(code);`,
      `      const encoded = String(code);`,
    ]],
    breached: (s) => s.liveInTimeline.length > 0 || s.canary !== null || s.onAttrs.length > 0 || s.badScheme.length > 0,
    want: 'data-code 属性被突破(活元素 / on* 属性 / canary 被置)',
  },
};
function buildMutation(name) {
  const m = MUTATIONS[name];
  assert.ok(m, `未知变异 ${name}`);
  let s = RENDERER_BYTES.toString('utf8');
  m.steps.forEach(([oldStr, newStr], i) => {
    const n = s.split(oldStr).length - 1;
    assert.strictEqual(n, 1,
      `🔴 阳性对照 ${name} 的第 ${i + 1} 处锚点命中 ${n} 次(应恰好 1 次)—— **装置锚点过期,不是产品回归**。`
      + `产品件动过了 ⇒ 请更新 MUTATIONS['${name}'].steps,别把这条对照腿静默跳过。锚点前 60 字:${JSON.stringify(oldStr.slice(0, 60))}`);
    s = s.replace(oldStr, newStr);
  });
  const bytes = Buffer.from(s, 'utf8');
  return { bytes, sha256: crypto.createHash('sha256').update(bytes).digest('hex') };
}

// ── 跑一例:起夹具 → 开页 → 真灌 → 扫 DOM ────────────────────────────────────────
async function runCase(browser, c) {
  const signals = {};
  const srv = await startServer({ bytes: c.bytes, events: c.events, signals });
  const page = await browser.newPage({ viewport: VP });
  const dialogs = [];
  const pageErrors = [];
  page.on('dialog', async (d) => { dialogs.push(`${d.type()}:${d.message().slice(0, 60)}`); try { await d.dismiss(); } catch {} });
  page.on('pageerror', (e) => pageErrors.push(String(e.message).slice(0, 120)));
  const out = { name: c.name, dialogs, pageErrors, scans: [] };
  try {
    await page.goto(`${srv.url}view/${encodeURIComponent(SESSION)}`, { waitUntil: 'domcontentloaded', timeout: WAIT_MS });
    for (const step of c.probe) {
      await step(page, out);
    }
  } catch (e) {
    out.error = `${e.message.split('\n')[0]}`;
  } finally {
    await page.close().catch(() => {});
    await srv.close().catch(() => {});
  }
  return out;
}

const waitSel = (page, sel, why) => page.waitForSelector(sel, { timeout: WAIT_MS, state: 'attached' })
  .catch(() => { throw new Error(`等不到 ${sel}(${why})⇒ 这一格会是空洞的绿,先修装置`); });
// 🔴 等待条件必须**与变异无关** —— 这是第一跑量出来的:
//    阳性对照那三发会把 payload 变成**活元素**,那时"转义后的文本"根本不存在,
//    用文本当等待条件 ⇒ 对照腿以「装置异常:等不到正文」收场,**而不是**报出"探针有牙"。
//    ⇒ 一律等 innerHTML 里的 canary 串(转义与否它都在),或等与 payload 无关的结构提示。
const waitHtml = (page, frag, why) => page.waitForFunction(
  (f) => ((document.getElementById('timeline') || {}).innerHTML || '').includes(f), frag, { timeout: WAIT_MS })
  .catch(() => { throw new Error(`等不到 #timeline 的 HTML 里出现「${String(frag).slice(0, 40)}…」(${why})⇒ 灌进去的东西没渲染,先修装置`); });

// ── 各例定义 ────────────────────────────────────────────────────────────────────
const FENCE = '```html\n' + `<img src=x onerror="${CANARY}">\n<script>${CANARY}</script>\n` + '```';
// 🔴 1188 不是随手写的:payload 从第 1188 字起,`onerror=` 起于第 1199 字 ⇒ 摘要在 1200 字硬截断
//    只吃到一个 "o" ⇒ 残片是「<img src=x o」。(第一跑用 1180,切点落在 `onerror="` 之后,守卫正确地红了。)
const FILLER = 'A'.repeat(1188);
const STRUCT = [
  `# 标题里的注入 <img src=x onerror="${CANARY}">`,
  `> 引用里的注入 <svg onload="${CANARY}"></svg>`,
  `- [ ] 任务里的注入 [x](javascript:${CANARY})`,
  '- [x] 已完成项',
  '',
  '| 表头 | 另一列 |',
  '| --- | --- |',
  `| 单元格注入 <img src=x onerror="${CANARY}"> | [x](javascript:${CANARY}) |`,
].join('\n');
const LEGIT = [
  '# 正常标题',
  '',
  '**粗体** 与 *斜体* 与 `行内代码` 与 [正常链接](https://example.com/a)',
  '',
  '> 正常引用',
  '',
  '- [ ] 待办项',
  '- 普通列表项',
  '',
  '| 列一 | 列二 |',
  '| --- | --- |',
  '| 值一 | 值二 |',
  '',
  '```python',
  'def f():',
  '    return 1',
  '```',
  '',
  '第一行',
  '第二行',
].join('\n');

function eventsFor(body, id, opt = {}) {
  const evs = [initEv(), userEv(opt.userText || '请渲染下面的正文', id), { after: 60 }];
  if (opt.preChunks) for (const t of opt.preChunks) evs.push(asstEv(t, id), { after: 20 });
  evs.push(asstEv(body, id), { after: 120 });
  if (opt.holdStream) evs.push({ wait: 1200 });
  if (!opt.noComplete) evs.push(doneEv(id), { after: 200 });
  return evs;
}

const CASES = [
  {
    name: 'I1',
    title: `assistant_text 正文:${PAYLOADS.length} 种注入形状一律不得变活(流式路径 + 回合重建路径各扫一次)`,
    events: eventsFor(ALL_TEXT, 'd-i1', { holdStream: true }),
    probe: [
      // ① 流式路径:turn_completed 之前,正文已经通过 innerHTML 上过屏一次
      async (page, out) => {
        await waitSel(page, '#timeline .md', '流式渲染的 .md 容器');
        await waitHtml(page, '__dwPwned', '流式正文');
        const s = await scan(page); out.scans.push({ at: '流式渲染中(turn_completed 之前)', s });
      },
      // ② 回合重建路径:turn_completed 之后重新 build 出来的那份
      async (page, out) => {
        await waitSel(page, '#timeline .turn-assistant .event-content .md', '回合重建后的 .md');
        await sleep(400);
        const s = await scan(page); out.scans.push({ at: '回合重建后(turn_completed 之后)', s });
      },
    ],
    kind: 'clean',
    expectFrags: PAYLOADS.flatMap((p) => p.expect),
    expectInCode: true,   // 只有这一例灌了 P15(行内代码),所以只有它验"payload 落在 <code> 里"
  },
  {
    name: 'I2',
    title: '代码围栏内的 payload(highlightCode 路径)必须是死文本',
    events: eventsFor(FENCE, 'd-i2'),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-assistant pre code', '代码块');
      await waitHtml(page, '__dwPwned', '代码块正文');
      out.scans.push({ at: '代码围栏(highlightCode)', s: await scan(page) });
    }],
    kind: 'clean',
    expectFrags: [escTextNode(`<img src=x onerror="${CANARY}">`), escTextNode(`<script>${CANARY}</script>`)],
    require: (s) => assert.ok(s.legit.pre >= 1 && s.legit.code >= 1, `守卫:代码块根本没渲染出来(pre=${s.legit.pre} code=${s.legit.code})⇒ 这一格是空的`),
  },
  {
    name: 'I3',
    title: '结构路径:标题 / 引用 / 任务列表 / 表格单元格(全部经 renderInline)必须是死文本',
    events: eventsFor(STRUCT, 'd-i3'),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-assistant table', '表格');
      await waitHtml(page, '__dwPwned', '结构路径正文');
      out.scans.push({ at: '标题/引用/任务列表/表格', s: await scan(page) });
    }],
    kind: 'clean',
    expectFrags: [escTextNode(`<img src=x onerror="${CANARY}">`), escTextNode(`<svg onload="${CANARY}"></svg>`),
      `[x](javascript:${CANARY})`],
    require: (s) => {
      assert.ok(s.legit.table >= 1 && s.legit.th >= 1, `守卫:表格没渲染出来(table=${s.legit.table} th=${s.legit.th})⇒ 这一格是空的`);
      assert.ok(s.legit.heading >= 1, `守卫:标题没渲染出来(h*=${s.legit.heading})`);
      assert.ok(s.legit.checkbox >= 1, `守卫:任务列表复选框没渲染出来(=${s.legit.checkbox})`);
      assert.ok(s.legit.quote >= 1, `守卫:引用块没渲染出来(blockquote=${s.legit.quote})`);
    },
  },
  {
    name: 'I4',
    title: '超长摘要:payload 跨 1200 字**硬截断点**仍必须 inert(且证明确实走了摘要路径)',
    // 无换行填充 ⇒ eventsTextPrefix 的 lastIndexOf('\n') 落在 600 之前 ⇒ 走"硬截断"分支,正好切在 payload 中间
    events: eventsFor(FILLER + `<img src=x onerror="${CANARY}">` + 'B'.repeat(3200), 'd-i4'),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-assistant.longtext', '超长摘要块');
      await waitHtml(page, '已折叠', '超长摘要提示(与 payload 无关 ⇒ 变异下也成立)');
      out.scans.push({ at: '超长摘要(截断点)', s: await scan(page) });
    }],
    kind: 'clean',
    expectFrags: [],   // 截断腿不做"转义形态在不在"的反证:payload 被切掉了后半截,守卫自己钉这件事
    require: (s) => {
      assert.ok(s.longtext >= 1, `守卫:没走摘要路径(.turn-assistant.longtext=${s.longtext})⇒ 这一格验的不是截断`);
      // 🔵 守卫:必须**真的**切在 payload 中间。否则"没执行"只是因为整段没进摘要 ⇒ 那是空洞的绿。
      assert.ok(s.timelineText.includes('<img src=x o'),
        '守卫:摘要里没有预期的截断残片「<img src=x o」⇒ 截断点没落在 payload 上'
        + `(实测:含 "<img "=${s.timelineText.includes('<img ')} 含 "onerror="=${s.timelineText.includes('onerror=')})`);
      assert.ok(!s.timelineText.includes('onerror='),
        '守卫:摘要里出现了完整 payload ⇒ 根本没截断,这一格验的不是边界');
    },
  },
  {
    name: 'I5',
    title: 'user_input 路径(escapeHtml,不走 markdown)必须是死文本',
    events: eventsFor('（本回合助手正文为占位）', 'd-i5', { userText: ALL_TEXT }),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-user .event-content', '用户卡');
      await waitHtml(page, '__dwPwned', '用户卡正文');
      out.scans.push({ at: 'user_input 卡(escapeHtml)', s: await scan(page) });
    }],
    kind: 'clean',
    // 🔵 用户卡不走 markdown ⇒ 连 P12 也是整条以文本节点出现(不是属性形态),所以这里用 escTextNode 而不是 p.expect
    expectFrags: PAYLOADS.map((p) => escTextNode(p.text)),
    require: (s) => assert.ok(s.userCard >= 1, `守卫:用户卡没渲染出来(.turn-user=${s.userCard})⇒ 这一格是空的`),
  },
  {
    name: 'I6',
    title: '🔵 反证(防"渲染成空也算通过"):正常 markdown 必须仍渲染出来',
    events: eventsFor(LEGIT, 'd-i6'),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-assistant table', '正常表格');
      await waitHtml(page, '正常链接', '正常正文');
      await sleep(300);
      out.scans.push({ at: '正常 markdown', s: await scan(page) });
    }],
    kind: 'legit',
  },
  {
    name: 'I7',
    title: '🔵 显影腿:markdown 图片语法 ![alt](url) 现状【不】被渲染成 <img>(变了要报,不一定是缺陷)',
    events: eventsFor('![示意图](https://ok.example/a.png)\n\n正文里还有一条 [链接](https://ok.example/b)', 'd-i7'),
    probe: [async (page, out) => {
      await waitSel(page, '#timeline .turn-assistant .md', '正文');
      await waitHtml(page, '示意图', '图片语法正文');
      out.scans.push({ at: '图片语法显影', s: await scan(page) });
    }],
    kind: 'develop',
  },
];

// ── 主流程 ──────────────────────────────────────────────────────────────────────
(async () => {
  console.log('renderer-markdown-injection(ts-No.76 · 组长裁定④)');
  console.log(`  renderer  ${RENDERER}`);
  console.log(`  sha256    ${RENDERER_SHA.slice(0, 16)}…${OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这是变异/对照跑,不是产品那份字节' : ''}`);
  console.log(`  payload   ${PAYLOADS.length} 种形状 · canary=window.__dwPwned`);
  console.log('  🔴 结论口径:本件只证明「这些形状不成立」,**不得**读成「无 XSS」');

  let browser;
  try {
    const launchOptions = { headless: true, args: ['--no-sandbox'] };
    // 🔵 env 面只有已登记的两个;PW_CHANNEL 设了就真用,不静默忽略(否则跑者以为切了 channel,其实没切)
    if (process.env.PW_CHANNEL) launchOptions.channel = process.env.PW_CHANNEL;
    browser = await chromium.launch(launchOptions);
  } catch (e) {
    console.error(`环境不可用:Playwright 起不来(${e.message.split('\n')[0]})`);
    process.exit(3);
  }
  let pass = 0; let fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); } catch (e) {
      fail += 1; failures.push(title.split(':')[0]);
      console.log(`\n  🔴 ${e.message.split('\n').slice(0, 12).join('\n  ')}`);
    }
  };

  try {
    // ── 产品字节:七例 ────────────────────────────────────────────────────────────
    for (const c of CASES) {
      await t(`${c.name} ${c.title}`, async () => {
        const r = await runCase(browser, Object.assign({}, c, { bytes: RENDERER_BYTES }));
        if (r.error) throw new Error(`装置异常:${r.error}`);
        assert.deepStrictEqual(r.dialogs, [], `🔴 弹出了对话框(说明脚本真执行了):${JSON.stringify(r.dialogs)}`);
        assert.deepStrictEqual(r.pageErrors, [], `🔴 页面报错(注入的脚本可能已执行):${JSON.stringify(r.pageErrors)}`);
        assert.ok(r.scans.length > 0, '一次扫描都没做 ⇒ 装置没跑到断言');

        if (c.kind === 'clean') {
          for (const { at, s } of r.scans) {
            const b = breachOf(s);
            assert.deepStrictEqual(b, [], `🔴 [${at}] 注入形状**变活了**:\n     ${b.join('\n     ')}`);
            assert.strictEqual(s.canary, null, `🔴 [${at}] canary 被置(${s.canary})⇒ 有脚本真执行了`);
            assert.ok(s.mdCount >= 1 || s.userCard >= 1, `🔴 [${at}] 守卫:消息容器里什么都没渲染(md=${s.mdCount} userCard=${s.userCard})⇒ 这一格是空洞的绿`);
            // 🔴 这道只在**真灌了该 payload 的那一例**上跑 —— 第一版我放进了公共循环,
            //    于是 I2/I3/I4/I5 全都因为"没找到 P15"而红,而那几例**根本没灌 P15**。
            //    🔵 红得对(守卫没有放水),但归因错了 ⇒ 改为按例开关。
            for (const pl of (c.expectInCode ? PAYLOADS.filter((x) => x.inCode) : [])) {
              assert.ok((s.codeTexts || []).some((t) => t.includes(pl.inCode)),
                `🔴 [${at}] ${pl.id} 的 payload 没有落在 <code> 元素里(实测 code 元素 ${(s.codeTexts || []).length} 个)`
                + ' ⇒ 行内代码路径的形状变了:它可能把 payload 吐回了正文(那就不再是"代码里的死文本")');
            }
            for (const frag of (c.expectFrags || [])) {
              assert.ok(s.timelineHTML.includes(frag),
                `🔴 [${at}] 反证失败:payload 的**转义形态**「${frag.slice(0, 60)}」没出现在 #timeline 的 HTML 里`
                + ' ⇒ 它不是被转义了,是被**悄悄丢掉**了(丢字不算安全,算失真;而且会让本腿变成空洞的绿)');
            }
            if (c.require) c.require(s);
            console.log(`\n     🔵 [${at}] 干净:on* 0 · 危险协议 0 · 活元素 0 · canary 未置 · payload 以文本可见`);
          }
        } else if (c.kind === 'legit') {
          const s = r.scans[0].s;
          const b = breachOf(s);
          assert.deepStrictEqual(b, [], `🔴 反证腿里也出现了破坏:${b.join(' | ')}`);
          const need = { strong: 1, em: 1, a: 1, code: 1, pre: 1, table: 1, th: 1, checkbox: 1, heading: 1, quote: 1, br: 1, tokKw: 1, copyBtn: 1 };
          const miss = Object.keys(need).filter((k) => !(s.legit[k] >= need[k]));
          assert.deepStrictEqual(miss, [],
            `🔴 过度屏蔽:正常 markdown 没渲染出来 ⇒ 缺 ${miss.join(', ')};实测 ${JSON.stringify(s.legit)}`
            + '\n     🔵 这条腿是"探针没瞎"的证据:一个把正文渲染成空的改动会让其它所有腿假绿');
          console.log(`\n     🔵 正常渲染全在:${JSON.stringify(s.legit)}`);
        } else if (c.kind === 'develop') {
          const s = r.scans[0].s;
          const b = breachOf(s);
          assert.deepStrictEqual(b, [], `🔴 显影腿里出现了破坏:${b.join(' | ')}`);
          assert.strictEqual(s.imgInTimeline, 0,
            `🔴 形状变了:消息容器里出现了 ${s.imgInTimeline} 个 <img> —— 说明**有人给自研渲染补了图片支持**。`
            + '\n     🔵 这不一定是缺陷:按 ts-No.75 ① 的裁定口径,业主产品定义里逐字有「预览图片」,'
            + '图片内嵌是**带号延后**项。⇒ 请报组长重新对账,别把这条红当成回归直接改回去。');
          // 🔵 真实形状是**第一跑量出来的**,不是我推的:`![alt](url)` 里的 `[alt](url)` 被链接正则吃掉,
          //    只剩一个孤零零的 "!" ⇒ 用户看到的是「!示意图」这样一个**带感叹号的链接**:
          //    不是图片,也不是干净文本。我原先以为字面量 "![" 会整体留下 —— **错了,已按实测改。**
          assert.ok(s.timelineText.includes('!示意图'),
            `🔴 形状变了:正文不再是「!示意图」这个形状 ⇒ 自研渲染开始消费图片语法了。`
            + `\n     实测正文片段:${JSON.stringify(s.timelineText.slice(0, 200))}`
            + '\n     🔵 报组长重新对账(ts-No.75 ①:业主产品定义里逐字有「预览图片」,本期按裁定**带号延后**),'
            + '**不要自行改回**。');
          assert.ok(s.legit.a >= 1,
            `守卫:alt 文本没有变成链接(a=${s.legit.a})⇒ 这一格验的不是"图片语法被当链接吃掉"那个形状`);
          console.log('\n     🔵 现状(显影,不是"该不该"):![alt](url) **不生成 <img>**;'
            + '而是被链接正则吃掉 `[alt](url)`,留下一个孤立的 "!" ⇒ 用户看到「!示意图」这样的带感叹号链接');
          console.log('     🔵 与 ts-No.75 ① 同源:业主产品定义里有「预览图片」,而自研渲染**没有图片分支** ⇒ 本期按裁定带号延后');
        }
      });
    }

    // ── 阳性对照:三发变异,各打一层 ─────────────────────────────────────────────
    const PC_CASE = {
      'M-I1': CASES[0],           // 正文那例:掏空 escapeHtml ⇒ 必炸
      'M-I2': CASES[0],           // 正文那例:放开协议 ⇒ 只炸 javascript: href
      'M-I3': { name: 'PC3', title: '', events: eventsFor(FENCE, 'd-pc3'), probe: CASES[1].probe, kind: 'clean' },
      'M-I4': { name: 'PC4', title: '', events: eventsFor(FENCE, 'd-pc4'), probe: CASES[1].probe, kind: 'clean' },
    };
    for (const name of Object.keys(MUTATIONS)) {
      const m = MUTATIONS[name];
      await t(`PC ${name} 阳性对照:${m.label} ⇒ **必须观测到破坏**(不红说明探针没有分辨力)`, async () => {
        const v = buildMutation(name);
        console.log(`\n     (变异副本 sha256=${v.sha256.slice(0, 16)}… 由产品字节 ${RENDERER_SHA.slice(0, 16)}… 在本进程内合成)`);
        const base = PC_CASE[name];
        const r = await runCase(browser, { name, bytes: v.bytes, events: base.events, probe: base.probe });
        if (r.error) throw new Error(`装置异常:${r.error}(变异把页面弄坏了也要说清,别当成"对照成立")`);
        const hits = [];
        for (const { at, s } of r.scans) if (m.breached(s)) hits.push(`[${at}] ${breachOf(s).join(' | ') || `canary=${s.canary}`}`);
        assert.ok(hits.length > 0,
          `🔴 阳性对照失效:施加「${m.label}」之后**没有观测到任何破坏**(期望看到:${m.want})。`
          + '\n     ⇒ 说明**探针没有分辨力**,前面那些绿不可信。先修探针,别读产品腿的结论。'
          + `\n     实测:dialogs=${JSON.stringify(r.dialogs)} pageErrors=${JSON.stringify(r.pageErrors)} scans=${r.scans.length}`);
        console.log(`     ✅ 变异生效,探针有牙:观测到 ${hits.length} 处破坏`);
        hits.slice(0, 2).forEach((h) => console.log(`        · ${h.slice(0, 220)}`));
        if (name === 'M-I2') {
          // 🔵 分层证据:M-I2 只应炸"协议"这一维,**不应**让 escapeHtml 那条也失效
          const s = r.scans[0].s;
          assert.strictEqual(s.onAttrs.length, 0,
            `🔴 分层错了:只放开链接协议,却观测到 on* 属性(${s.onAttrs.slice(0, 2).join(' | ')})`
            + ' ⇒ 说明破坏不是来自我改的那一处,这条对照的归因不成立');
        }
      });
    }
  } finally {
    await browser.close();
  }

  console.log(`通过 ${pass} / 失败 ${fail}   (renderer sha256=${RENDERER_SHA.slice(0, 16)}…)`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】(边界随绿一起打印,不用谁去翻交付件):');
  console.log(`     ① 结论**只到**「这 ${PAYLOADS.length} 种形状不成立」—— 🔴 **不得**读成「无 XSS」。形状清单是我列的,不是穷尽的。`);
  console.log('     ② 只覆盖 **markdown 渲染路径**(流式 innerHTML / 回合重建 / 代码围栏 / 标题·表格·任务列表·引用 /');
  console.log('        超长摘要截断点 / user_input 的 escapeHtml)。**未覆盖**:附件与文件那条(另有 0206 件)、');
  console.log('        来源标签与 c2Note、事件类型/状态标签、diag 键值、草稿输入框与粘贴路径、console 外壳自身。');
  console.log('     ③ **穷尽审计没做**:我是从注入形状往里打,不是把每一处 innerHTML 往外数。');
  console.log('     ④ 单档 1280×900 · headless · 默认主题;**暗色与窄档未跑**(推论上与转义无因果,但那是推论)。');
  console.log('     ⑤ I7 是**显影腿**:它钉的是"现在没有图片支持"这个形状,不是"不该有"。它红了要报,不要改回去。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
