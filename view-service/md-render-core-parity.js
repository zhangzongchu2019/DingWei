// 🔴 md-render-core「行为对账」闸 —— 共享件 vs 产品页,同一组输入,输出必须**逐字节相同**。
//
// 为什么在漂移闸(`test_md_render_core.py`)之外还要这一件:
//   漂移闸比的是**源字节**(6 段是否逐字相同)。源字节相同**不等于**行为相同 ——
//   产品页里若有同名遮蔽/重定义、或某个符号依赖了 6 段之外的页面全局,源字节仍会一致而行为已经分家。
//   本件不比源,比**输出**:把同一个 `compute` 函数字面量分别送进【真浏览器里的产品页】与【Node 里的共享件】,
//   对 99 项结果逐字节对账。🔵 这正是 `0.2.1.50` 那条交付条件(「spec 展示面必须与 timeline 走**同一条**
//   渲染路径」)在**共享件这一侧**能证的部分;spec 页那一侧要等消费方接上后另验。
//
// 用法:
//   node md-render-core-parity.js                    # 对账产品件与共享件
//   node md-render-core-parity.js --core=<另一份核心>  # 🔴 变异/对照跑(会打出醒目提示)
//   🔵 用 CLI 旗标而不是新 env 变量:本仓已登记的 env 面只有 `PW_CHANNEL` / `RENDERER_OVERRIDE`,
//      再加一个就跑者的机械扫描会拒(那正是"未登记面"要防的)。
//
// rc:0 全过 / 2 断言失败(有项不同) / 3 环境不可用(Playwright/Chromium/被测件缺失、或两侧 location 未对齐)
//
// 🔴 已实测本闸**有牙**(四发变异各打不同一段,`--core=` 指到变异副本,产品件与共享件一字未动):
//      M1 `class="md-img"` → `md-image`(render 段)          ⇒ 99 项中 **21** 项不同,rc=2
//      M2 掏空 `"` → `&quot;`(escape 段的承重墙)            ⇒ **20** 项不同(含注入形状 pay[1]),rc=2
//      M3 `avif: 'image'` → `'download'`(ext-kind 段)      ⇒ **仅 2** 项不同,rc=2  🔵 分辨率到表的单行
//      M4 转义顺序换成先 `<` 后 `&`(escape 段)              ⇒ **14** 项不同(含 pay[0]/[1]/[2]),rc=2
//   🔵 未施变异时:**103/103 逐字节相同**。
//
// 🔴 本件**没验到**什么(照实写,别读成"渲染已全验"):
//   ① 只比**纯函数输出**,不比 DOM/CSS/事件 —— 图片"取不到时的下载出路"那层不在共享件里(见核心件头注)。
//   ② `compute` 里的样本是**我选的** 24 段 markdown + 15 个 URL + 17 条注入形状,不是穷尽。
//   ③ 两侧 `location` 由本件**垫成同一个 origin**(页面走 `context.route`,Node 走 shim)。
//      🔴 真实 spec 页的 origin 与 timeline **未必相同** ⇒ `isOwnFileUrl` 的判定结果也就未必相同;
//      那条要在消费方接上后按它自己的 origin 重验,本件证不了。
//   ④ 本变更新增 canonical HTTPS 来源映射；浏览器侧模拟 50443，规范来源为无端口 443。
//      单档 headless Chromium、Linux;业主那台 macOS + 系统 Chrome 上没跑过(同型缺口已登记)。
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const assert = require('assert');

const HERE = __dirname;
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const RENDERER_OVERRIDE = !!process.env.RENDERER_OVERRIDE;
const CORE = (() => {
  const flag = process.argv.find((a) => a.startsWith('--core='));
  return flag ? path.resolve(flag.slice('--core='.length)) : path.join(HERE, 'md-render-core.js');
})();
const CORE_OVERRIDE = CORE !== path.join(HERE, 'md-render-core.js');
const ORIGIN = 'https://dw123.cmjrt.com:50443';
const CANONICAL = 'https://dw123.cmjrt.com';
const HREF = ORIGIN + '/view/parity-session';
const own = (ext, id) => `${ORIGIN}/f/o/s/26/0920/${id}.${ext}`;

class EnvironmentUnavailable extends Error {}

function chromiumForParity() {
  try { return require('playwright').chromium; }
  catch (err) { throw new EnvironmentUnavailable(`Playwright 不可用: ${err.message}`); }
}

async function launchChromium() {
  const chromium = chromiumForParity();
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

// 17 条注入形状取自**既有单一事实源**,不在本件另抄一份(两份必漂移)。
let PAYLOAD_TEXTS;
try { PAYLOAD_TEXTS = require(path.join(HERE, 'md-injection-payloads.js')).PAYLOADS.map((p) => p.text); }
catch (err) { throw new EnvironmentUnavailable(`md-injection-payloads.js 读不到: ${err.message}`); }

const CTX = {
  MD: [
    '# 标题一', '## 标题二 ### 标题三',
    '正文 **粗** *斜* `code` 与 [链接](https://ext.example/a?b=1&c=2)',
    '```python\ndef f(x):\n    return 1  # c\n```',
    '```json\n{"a": true, "b": 12, "c": null}\n```',
    '```js\nconst x = 1; // c\n```',
    '| 甲 | 乙 |\n|---|---|\n| 1 | 2 |',
    '- 无序一\n- 无序二\n  - 嵌套', '1. 有序一\n2. 有序二',
    '> 引用行\n> 第二行', '---',
    `![示意图](${own('png', 'i1')})`, `[裸图片直链](${own('png', 'i2')})`, own('png', 'i3'),
    `![svg](${own('svg', 'i4')})`, own('svg', 'i5'), own('exe', 'i6'),
    '![外站](https://ext.example/a.png)', 'https://ext.example/a.png',
    `混合:\n# 头\n正文 ${own('jpg', 'i7')} 与 ![图](${own('webp', 'i8')})\n- 项\n\n| a | b |\n|---|---|\n| 1 | 2 |`,
    '尾部无换行', '', '   ', '实体 & < > " \' 混合',
  ],
  URLS: [
    own('png', 'u1'), own('jpeg', 'u2'), own('avif', 'u3'), own('heic', 'u4'), own('tiff', 'u5'),
    own('svg', 'u6'), own('exe', 'u7'), own('md', 'u8'), own('PDF', 'u9'),
    'https://ext.example/a.png', '/f/o/x.png', 'f/o/x.png',
    own('png', 'u10') + '?q=1#frag', 'not a url', '',
  ],
  PROG: ['def f(x):\n    return 1  # c', '{"a": true, "b": 12}', 'const x = 1; // c'],
  LANGS: ['python', 'json', 'js'],
  PAY: PAYLOAD_TEXTS,
};

// 🔴 两侧跑的是**同一个函数字面量**:Node 侧直接调,浏览器侧由 addInitScript 注入它的 toString()。
//    所以"探针自己写了两份、其中一份写错"这个形状不成立。
//    🔵 它只用自己的两个参数(api / C)与 JSON ⇒ 序列化进页面不丢任何闭包。
function compute(api, C) {
  const j = (v) => JSON.stringify(v);
  return {
    md: C.MD.map((s) => api.renderMarkdown(s)),
    mdText: C.MD.map((s) => api.renderMarkdownText(s)),
    pay: C.PAY.map((s) => api.renderMarkdown(s)),
    url: C.URLS.map((u) => j([api.isOwnFileUrl(u), api.fileExtOf(u), api.kindFromExt(u), api.isInlineImageUrl(u), api.mdImageHtml(u, 'ALT')])),
    canonical: [api.isOwnFileUrl(C.CANONICAL + '/f/o/s/26/0920/canonical.png'),
      api.localFileUrl(C.CANONICAL + '/f/o/s/26/0920/canonical.png'),
      api.canonicalFileUrl(C.ORIGIN + '/f/o/s/26/0920/local.png'),
      api.renderMarkdown(`![canonical](${C.CANONICAL}/f/o/s/26/0920/canonical.png)`) ],
    esc: ['<a href="x">&\'', 'a&b', '"q"', "s's", '<>&"\''].map((s) => api.escapeHtml(s)),
    hl: C.PROG.map((c, i) => api.highlightCode(c, C.LANGS[i])),
    code: C.PROG.map((c, i) => api.renderCode(c, C.LANGS[i])),
    table: [api.renderTable(['| a | b |', '|---|---|', '| 1 | 2 |'])],
    list: [api.renderList(['- 甲', '- 乙']), api.renderList(['1. 甲', '2. 乙'])],
    fenced: [String(api.splitFencedBlocks('a\n```js\nx\n```\nb').length)],
    tblStart: [j([api.isTableStart(['| a | b |', '|---|---|'], 0), api.isTableStart(['| a |', 'x'], 0)]), j(api.splitTableRow('| a | b |'))],
    kind: [j(api.FILE_EXT_KIND)],
    never: [j([...api.FILE_EXT_NEVER_INLINE].sort())],
  };
}

const sha = (b) => crypto.createHash('sha256').update(b).digest('hex').slice(0, 16) + '…';

(async () => {
  let rendererBytes, coreBytes;
  try { rendererBytes = fs.readFileSync(RENDERER); }
  catch (err) { throw new EnvironmentUnavailable(`renderer 件读不到(${RENDERER}): ${err.message}`); }
  try { coreBytes = fs.readFileSync(CORE); }
  catch (err) { throw new EnvironmentUnavailable(`共享件读不到(${CORE}): ${err.message}`); }

  const browser = await launchChromium();
  let inBrowser;
  try {
    const context = await browser.newContext({ ignoreHTTPSErrors: true });
    await context.addInitScript({ content: `globalThis.DW_CANONICAL_PUBLIC_ORIGIN=${JSON.stringify(CANONICAL)};` });
    // 🔴 注入的就是上面那个 compute 的源码本身 ⇒ 两侧同一份逻辑
    await context.addInitScript({ content: `globalThis.__dwMdParityCompute = ${compute.toString()};` });
    // 自足装置:context.route 直接喂字节,不起服务、不占端口、cwd 无关
    await context.route(ORIGIN + '/**', async (route) => {
      const p = new URL(route.request().url()).pathname;
      if (p === '/view/parity-session') return route.fulfill({ contentType: 'text/html; charset=utf-8', body: rendererBytes });
      if (p.endsWith('/status')) return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ session: 'parity-session', auth_mode: 'legacy', readonly: true, state: 'idle' }) });
      return route.fulfill({ status: 404, contentType: 'text/plain', body: 'not found' });
    });
    const page = await context.newPage();
    await page.goto(HREF, { waitUntil: 'domcontentloaded' });
    inBrowser = await page.evaluate((C) => {
      // 🔴 21 个符号一律用**裸标识符**取:若被包进 IIFE/模块作用域,这一行当场 ReferenceError。
      //    这就是核心件头注「硬约束①:不得包 IIFE」的**执行侧证据**,不靠注释提醒。
      const api = {
        escapeHtml, isOwnFileUrl, canonicalPublicOrigin, localFileUrl, canonicalFileUrl, fileExtOf, kindFromExt, FILE_EXT_KIND, FILE_EXT_NEVER_INLINE,
        splitFencedBlocks, renderMarkdown, renderMarkdownText, isTableStart, splitTableRow,
        renderTable, renderList, renderInline, renderCode, highlightCode, isInlineImageUrl, mdImageHtml,
      };
      if (typeof globalThis.__dwMdParityCompute !== 'function') throw new Error('compute 未注入到页面');
      return { out: globalThis.__dwMdParityCompute(api, C), loc: location.origin + '|' + location.href, n: Object.keys(api).length, canonicalOrigin: canonicalPublicOrigin() };
    }, { ...CTX, ORIGIN, CANONICAL });
  } finally { await browser.close(); }

  // Node 侧把 location 垫成与页面**同一个** origin/href。
  // 🔴 不垫 ⇒ own-url 段必然不同,那是**环境差异不是抽件差异**;下面 loc 守卫会先判"环境不可用",
  //    不让这种红被读成产品问题(与"假故障长得和真故障一模一样"同族)。
  globalThis.location = { href: HREF, origin: ORIGIN, protocol: 'https:', host: 'dw123.cmjrt.com:50443' };
  globalThis.DW_CANONICAL_PUBLIC_ORIGIN = CANONICAL;
  let core;
  try { core = require(CORE); }
  catch (err) { throw new EnvironmentUnavailable(`共享件 require 失败(${CORE}): ${err.message}`); }
  const inNode = { out: compute(core, { ...CTX, ORIGIN, CANONICAL }), loc: globalThis.location.origin + '|' + globalThis.location.href, n: Object.keys(core).length, canonicalOrigin: core.canonicalPublicOrigin() };

  console.log('md-render-core-parity(共享件 vs 产品页 · 输出逐字节对账 · 不比源字节)');
  console.log(`  产品件  ${path.basename(RENDERER)}  sha256=${sha(rendererBytes)}${RENDERER_OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这不是仓内那份产品件' : ''}`);
  console.log(`  共享件  ${path.basename(CORE)}  sha256=${sha(coreBytes)}${CORE_OVERRIDE ? '  🔴 --core 生效:变异/对照跑,不是仓内那份共享件' : ''}`);
  console.log(`  规范来源: browser=${inBrowser.canonicalOrigin} core=${inNode.canonicalOrigin}`);
  assert.strictEqual(inBrowser.n, 21, `产品页可见符号数 ${inBrowser.n} ≠ 21(有人包了作用域或删了符号)`);
  assert.strictEqual(inNode.n, 21, `共享件导出符号数 ${inNode.n} ≠ 21`);
  if (inBrowser.loc !== inNode.loc) {
    throw new EnvironmentUnavailable(`两侧 location 未对齐:页面 ${inBrowser.loc} vs Node ${inNode.loc} ⇒ 对账无意义,不产出结论`);
  }
  console.log(`  location 对齐 ✅ ${inBrowser.loc}`);
  assert.strictEqual(inNode.out.canonical[0], true, '50443 页面必须把规范 443 /f/ 地址认作本站文件');
  assert.strictEqual(inNode.out.canonical[1], `${ORIGIN}/f/o/s/26/0920/canonical.png`, '规范 443 /f/ 地址应映射到页面同源 50443');
  assert.strictEqual(inNode.out.canonical[2], `${CANONICAL}/f/o/s/26/0920/local.png`, '输入框同源 50443 /f/ 地址应规范化回 443');
  assert.ok(inNode.out.canonical[3].includes(`<img class="md-img" data-md-img src="${ORIGIN}/f/`), '规范来源图片渲染应从 50443 同源取图');

  let same = 0; const diffs = [];
  for (const k of Object.keys(inNode.out)) {
    const a = inBrowser.out[k], b = inNode.out[k];
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) { diffs.push([k, -1, '形状/长度不同', '']); continue; }
    for (let i = 0; i < a.length; i++) {
      if (a[i] === b[i]) { same++; continue; }
      let at = 0; while (at < Math.min(a[i].length, b[i].length) && a[i][at] === b[i][at]) at++;
      diffs.push([k, i, `第 ${at} 字起不同`, `产品页=${JSON.stringify(a[i].slice(at, at + 60))} 共享件=${JSON.stringify(b[i].slice(at, at + 60))}`]);
    }
  }
  const total = Object.values(inNode.out).reduce((n, v) => n + v.length, 0);
  console.log(`  对账 ${total} 项:逐字节相同 ${same} · 不同 ${diffs.length}`);
  for (const d of diffs.slice(0, 12)) console.log(`    🔴 ${d[0]}[${d[1]}] ${d[2]} ${d[3]}`);
  if (diffs.length > 12) console.log(`    ……另有 ${diffs.length - 12} 项(全部见上表口径,不静默截断结论)`);
  console.log('  🔵 本件没验到:①只比纯函数输出,不比 DOM/CSS/事件与"取不到图时的下载出路";');
  console.log('               ②样本是我选的,不穷尽;③两侧 location 由本件垫成同一 origin,');
  console.log('                 真实 spec 页的 origin 未必相同 ⇒ 那一侧要按它自己的 origin 重验;');
  console.log('               ④headless Chromium · Linux,业主的 macOS + 系统 Chrome 上没跑过。');
  if (diffs.length) { console.log(`\nFAIL: ${diffs.length} 项不同 —— 🔴 停下回报,不要"顺带修好"`); process.exit(2); }
  console.log(`\nmd-render-core-parity PASS: ${total}/${total} 项输出逐字节相同(含 17 注入形状 · 8 类图片扩展名 · 表格/代码/列表)`);
  process.exit(0);
})().catch((err) => {
  // rc 约定:0 全过 / 2 断言失败 / 3 环境不可用
  if (err instanceof EnvironmentUnavailable) { console.error(`ENVIRONMENT_UNAVAILABLE: ${err.message}`); process.exit(3); }
  console.error(err && (err.stack || err.message) || err);
  process.exit(2);
});
