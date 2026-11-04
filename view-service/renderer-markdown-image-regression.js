// 🔴 ts-No.79「图片按类型自动显示」的闸 —— 板件锚 `96d82d6e`(派工节逐字:业主「只要是图片的就显示预览的图」
//    「感叹号可以不用了」;组长判定**阻塞 `0.2.1.50`**,本格**改产品件**)。
//
// 钉的不变量(四条,与派工单①②③④一一对应):
//   ① markdown 里指向**图片类型**的链接 ⇒ **就地渲染成 `<img>`**;判据**不另写清单**,
//      复用附件那条路已在用的 `kindFromExt()`(⇒ `FILE_EXT_KIND` ＋ `FILE_EXT_NEVER_INLINE`),
//      而 `FILE_EXT_KIND` 的图片行由本件 **G7 与 `deploy/nginx-dingwei.conf` 的 `types{image/*}` 逐项对账**。
//   ② `![alt](url)` 保留兼容,也渲染成图 ⇒ 那个「`!` ＋ 链接」的怪形态消失(感叹号不再出现)。
//   ③ 非图片链接**仍是链接**(行为不变)。
//   ④ 🔴 **一律 `<img>`,绝不内联 SVG 源码**;`svg` 不靠"名单里没写"来挡,靠 `FILE_EXT_NEVER_INLINE` **写死**那一层。
//
// ── 🔴 边界:只对**本站 `/f/` 直链**就地渲染 ─────────────────────────────────────────
//   产品件 `renderer-canary-sse.html` 自己写着(附件那条路的前提):
//     「只认本站 /f/ 直链(附录 A1 的 URL 前缀),**不对任意外站 URL 做任何请求**」
//   ⇒ markdown 里写外站图片 URL **仍是链接**,且**一个请求都不发**(G4 用请求账目实测,不是读码)。
//   🔵 这是一条**产品可见**的边界:若业主要外站图也预览,得由他解除这条不变量 —— 已报组长,本件不替他决定。
//
// ── 🔴 一图不显示两次(本件自己撞出来的交互,不是派工单里的)────────────────────────────
//   正文里的本站 `/f/` 直链**早就**会被 `attachFileRenders` 扫出来建一张附件卡(卡里就是 `<img>`)。
//   ① 落地后,同一条 URL 会**既就地出图、又在下面出一张卡** ⇒ 一图两处。
//   ⇒ 产品侧加了"就地那张真加载出来了 ⇒ 附件卡让位"的规则;判据是**当前 DOM 里那张 `<img>` 的状态**,
//     不是"写在 markdown 语法里"。🔴 为什么不能用后者(G5/G6 各钉一条):
//       · **超长正文只渲染摘要** ⇒ 摘要外的图不会就地出现 ⇒ 卡必须留着,否则**图直接没了**
//       · **7 天过期 / 解不了码** ⇒ 就地那张撤掉 ⇒ 卡照旧给「文件已不可取(服务端 404)…7 天保留期」
//         与「⬇ 下载图片」的出路 ⇒ 不比现状差
//
// ── 🔴 观测量先证对变异敏感(组长新立纪律)────────────────────────────────────────────
//   五条阳性对照(M-G1~M-G5),**各打不同一层**,全部在本进程内合成(每处锚点须恰好命中一次,否则报错不静默跳过):
//     M-G1 图片判据掏空(恒 false)        ⇒ G1 的 `<img>` 数必须 16→0、感叹号必须回来
//     M-G2 拆掉 deny-set ＋ 表里塞 svg    ⇒ G2 的 `.svg` 必须变成 `<img>`(④ 那层写死的守卫有牙)
//     M-G3 escapeHtml 去掉 `"`→`&quot;`  ⇒ G3 必须观测到 on* 属性/活元素(承重墙有牙)
//     M-G4 去掉"附件卡让位"             ⇒ G5 必须观测到一图两处
//     M-G5 去掉本站边界                  ⇒ G4 必须观测到外站 `<img>` ＋ **外部请求数 > 0**
//   🔴 等待条件一律**与变异无关**(等 SENTINEL,不等 `<img>`)—— 否则变异生效那一刻等待条件就不成立,
//      对照腿会以「装置异常/超时」收场,把"探针有牙"伪装成"装置坏了"(组长 ts-No.76 立的纪律)。
//
// ── 真灌,不走测试缝 ─────────────────────────────────────────────────────────────────
//   payload 一律作为**真 SSE 事件**灌进去,走产品自己的流式渲染与回合重建两条路径,不调用 renderMarkdown()。
//   🔴 唯一例外:G0 的装置自证会直接调 `isOwnFileUrl()` —— 那是**量装置**(证明边界前提成立),
//      不是产品断言;凡产品结论一律来自真灌后的 DOM。
//
// ── 注入腿复用 ts-No.76,不另起炉灶 ──────────────────────────────────────────────────
//   形状清单来自 `md-injection-payloads.js`(单一事实源,两件 require 同一个文件),
//   本件只取其中与 URL/图片相关的子集(IMAGE_RELEVANT),另加 9 条**图片语法专属**形状(Q01~Q09)。
//
// 装置:自带 HTTP+SSE 夹具(自启自收、`listen(0)` 自挑空闲端口、路径一律走 __dirname,cwd 无关)。
//   🔴 夹具的 Content-Type **不自己编表**:直接从 `deploy/nginx-dingwei.conf` 的 `types{}` 解析;
//      nginx 不放行的扩展名(svg/exe/html…)夹具也**404**,与生产同形。
//   🔴 图片字节是本件自己合成的 2×2 PNG(79 B),**不是第三方字节**;所有扩展名都喂同一份合法 PNG ——
//      本格验的是**按扩展名分派**,不是浏览器能不能解那个格式(后者见文末「没验到什么」)。
//
// rc:0 全过 / 2 断言失败 / 3 环境不可用。env 面只有已登记的两个:`PW_CHANNEL` / `RENDERER_OVERRIDE`。
// 🔴 本件**不自行接 SUITES**(跑者归 `coder56solzzc`)。
'use strict';

const fs = require('fs');
const crypto = require('crypto');
const assert = require('assert');
const http = require('http');
const path = require('path');
const { chromium } = require('playwright');
const { CANARY, PAYLOADS, escTextNode } = require('./md-injection-payloads.js');

const HERE = __dirname;
const RENDERER = process.env.RENDERER_OVERRIDE || path.join(HERE, 'renderer-canary-sse.html');
const OVERRIDE = !!process.env.RENDERER_OVERRIDE;
const NGINX_CONF = path.join(HERE, '..', 'deploy', 'nginx-dingwei.conf');
const SESSION = 'mdimg-alpha-2642';
const VP = { width: 1280, height: 900 };
const WAIT_MS = 30000;
const SETTLE_MS = 1600;          // 其他用例的原有稳定窗口;G6 改为等待两张目标卡出现
const CASE_FILTER = process.argv.find((arg) => arg.startsWith('--case='))?.slice(7) || '';
const MUTATE_NO_G6_CARD = process.argv.includes('--mutate-no-g6-card');
const SENTINEL = 'DWMDIMGSENTINEL';   // 🔴 与变异无关的等待锚:每例正文都带它

let RENDERER_BYTES;
try {
  RENDERER_BYTES = fs.readFileSync(RENDERER);
  if (MUTATE_NO_G6_CARD) {
    const src = RENDERER_BYTES.toString('utf8');
    const anchor = 'slot.replaceWith(box); rec.node = box;';
    if (!src.includes(anchor)) throw new Error('G6 mutation anchor missing');
    RENDERER_BYTES = Buffer.from(src.replace(anchor, 'slot.remove(); rec.node = slot;'));
  }
} catch (e) {
  console.error(`环境不可用:读不到 renderer(${RENDERER}):${e.message}`);
  process.exit(3);
}
const RENDERER_SHA = crypto.createHash('sha256').update(RENDERER_BYTES).digest('hex');
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const now = () => Date.now();

// 🔴 本件自己合成的 2×2 PNG(79 B)。不是第三方字节,不入仓任何外部下载物。
const PNG_BYTES = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGM8ISfHwMDAxMDAwMDAAAANBAEIfXHKZgAAAABJRU5ErkJggg==',
  'base64');
const BAD_BYTES = Buffer.from('这不是图片字节,解码必然失败(ts-No.79 G6 用)'.repeat(3), 'utf8');
// 🔴 M-G2 用:声明成 image/svg+xml 就**必须**喂真 SVG 字节。
//    第一跑我图省事喂了 PNG 字节 ⇒ Chrome 按 MIME 选 SVG 解码器 ⇒ 解码失败 ⇒ 就地那张被 error 分支撤掉
//    ⇒ **观测量看不见变异**(又一次"盲量",而且成因是夹具不忠实,不是产品)。
const SVG_BYTES = Buffer.from(
  '<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="#c83232"/></svg>',
  'utf8');

// ── nginx 是**唯一**的白名单来源:夹具的 Content-Type 表也从这里解析,不自己编 ──────────────
let NGINX_RAW;
try {
  NGINX_RAW = fs.readFileSync(NGINX_CONF, 'utf8');
} catch (e) {
  console.error(`环境不可用:读不到 nginx 配置(${NGINX_CONF}):${e.message}`);
  process.exit(3);
}
// `types { image/jpeg jpg; … text/plain txt md csv …; }` —— 一行可能是"一个 mime 多个扩展名"
const NGINX_TYPES = {};                                   // ext -> mime
for (const m of NGINX_RAW.matchAll(/^\s*([a-z0-9-]+\/[a-z0-9.+-]+)\s+([a-z0-9 ]+);/gm)) {
  for (const ext of m[2].trim().split(/\s+/)) NGINX_TYPES[ext] = m[1];
}
const NGINX_IMAGE_EXTS = Object.keys(NGINX_TYPES).filter((e) => NGINX_TYPES[e].startsWith('image/'));
// `location ~ "^/f/…\.(?<file_ext>a|b|c)$"` —— 与 internal/filepolicy/policy_test.go 同一取法
const NGINX_SERVED_EXTS = new Set(
  [...NGINX_RAW.matchAll(/\(\?<file_ext>([^)]+)\)/g)].flatMap((m) => m[1].split('|')));
// 第二个 location(bin|docx|…)带 `Content-Disposition attachment always` ⇒ 夹具照抄这个形状
const NGINX_ATTACHMENT_EXTS = new Set(
  [...NGINX_RAW.matchAll(/\(\?<file_ext>(bin\|[^)]+)\)/g)].flatMap((m) => m[1].split('|')));

// ── renderer 源码里的两处判据(G7 静态对账用)──────────────────────────────────────────
function readRendererLists(src) {
  const kindBlock = (/const FILE_EXT_KIND = \{([\s\S]*?)\n {4}\};/.exec(src) || [])[1];
  const neverBlock = (/const FILE_EXT_NEVER_INLINE = new Set\(\[([^\]]*)\]\)/.exec(src) || [])[1];
  assert.ok(kindBlock, '源码里取不到 FILE_EXT_KIND —— 名字被改过?(G7 的锚点要跟着改,别把这条静默跳过)');
  assert.ok(neverBlock, '源码里取不到 FILE_EXT_NEVER_INLINE —— 名字被改过?');
  const imageRows = [...kindBlock.matchAll(/([a-z0-9]+)\s*:\s*'image'/g)].map((m) => m[1]);
  const allRows = [...kindBlock.matchAll(/([a-z0-9]+)\s*:\s*'([a-z]+)'/g)].map((m) => ({ ext: m[1], kind: m[2] }));
  const never = [...neverBlock.matchAll(/'([^']+)'/g)].map((m) => m[1]);
  return { imageRows, allRows, never };
}

// ── /f/ 直链构造:形状照 nginx 的 location 正则(两段 1-64 + 2 位 + 4 位 + 32 hex)──────────
const hex32 = (s) => crypto.createHash('md5').update(String(s)).digest('hex');
const relOf = (name, ext) => `f/fixowner/fix-a-2642/26/0918/${hex32(name)}${ext ? '.' + ext : ''}`;

// ── 自带夹具:一套路由,按 opt.events 灌真 SSE;/f/ 的形状照 nginx ─────────────────────────
async function startServer(opt) {
  const files = new Map();      // rel path -> spec
  for (const f of opt.files || []) files.set(relOf(f.name, f.ext), f);
  let EVENTS = null;            // 🔴 正文里的 URL 含端口 ⇒ 必须先 listen 才知道 base,事件因此延迟构造
  const server = http.createServer((req, res) => {
    const u = new URL(req.url, 'http://127.0.0.1');
    const p = u.pathname;
    const json = (o) => { res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(o)); };
    const html = () => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(opt.bytes || RENDERER_BYTES); };
    if (p === '/status') return json({ session: SESSION, state: 'running', readonly: false, can_write: true, auth_mode: 'legacy', owner: 'zzc', model: 'm', cost_total: 0, cost_last: 0, turns: 1 });
    if (p === '/tasks' || p === '/alerts') return json(p === '/alerts' ? [] : {});

    if (p.startsWith('/f/')) {
      const rel = p.slice(1);                        // 去掉前导 '/'
      const spec = files.get(rel);
      const ext = (rel.split('.').pop() || '').toLowerCase();
      // 🔴 nginx 不放行的扩展名(svg/exe/html/…)⇒ 夹具也 404,与生产同形(`location /f/ { return 404; }`)
      //    bin/docx/… 那组由第二个 location 用 `default_type application/octet-stream` ＋ 强制 attachment 出。
      // 🔵 spec.mime 是**故意的例外**:让 nginx 本来不放行的扩展名(如 svg)能被"假装放行"。
      //    为什么需要:阴格②要证明的是"就算文件服务肯出 svg,渲染器也**写死**不内联它" ——
      //    若夹具照 nginx 一律 404,那张图会因加载失败被撤掉,**观测量看不见变异**(第一跑就撞上了)。
      const mime = (spec && spec.mime) || NGINX_TYPES[ext] || (NGINX_ATTACHMENT_EXTS.has(ext) ? 'application/octet-stream' : '');
      if (!mime) { res.writeHead(404, { 'Content-Type': 'text/plain' }); return res.end('404 not found'); }
      if (spec && spec.status) { res.writeHead(spec.status, { 'Content-Type': 'text/plain' }); return res.end(''); }
      if (req.method === 'HEAD' && spec && spec.headStatus) { res.writeHead(spec.headStatus); return res.end(); }
      const body = (spec && spec.body) || PNG_BYTES;
      const headers = { 'Content-Type': mime, 'Content-Length': String(body.length), 'Cache-Control': 'no-store' };
      if (NGINX_ATTACHMENT_EXTS.has(ext)) headers['Content-Disposition'] = 'attachment';
      if (req.method === 'HEAD') { res.writeHead(200, headers); return res.end(); }
      res.writeHead(200, headers);
      return res.end(body);
    }

    if (p === `/view/${encodeURIComponent(SESSION)}`) return html();
    if (!p.endsWith('/events')) return html();
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
    res.flushHeaders();
    let cur = 0;
    const alive = () => !res.writableEnded && !res.destroyed;
    (async () => {
      try {
        for (const step of EVENTS) {
          if (!alive()) return;
          if (step.wait) { await sleep(step.wait); continue; }
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
  const url = `http://127.0.0.1:${port}/`;
  EVENTS = opt.eventsFor(url);
  return { url, port, close: () => new Promise((r) => server.close(r)) };
}

const initEv = () => ({ ev: { kind: 'state_change', data: { change: 'session_init', session_id: 's1', model: 'm' }, delivery_id: '', turn_id: '' } });
const userEv = (text, id) => ({ ev: { kind: 'user_input', text, delivery_id: id, turn_id: id, stage: 'cli_injected' } });
const asstEv = (text, id) => ({ ev: { kind: 'assistant_text', text, delivery_id: id, turn_id: id } });
const doneEv = (id) => ({ ev: { kind: 'state_change', data: { change: 'turn_completed', turn_cost_usd: 0.1 }, delivery_id: id, turn_id: id } });

// 一例 = 一个回合:用户问一句 → 助手正文(带 SENTINEL)→ [可选保持流式] → 回合完成
function eventsFor(text, id, opt = {}) {
  const evs = [initEv(), userEv(opt.userText || '请渲染下面的正文', id), { after: 60 }];
  for (const chunk of opt.preChunks || []) evs.push(asstEv(chunk, id), { after: 30 });
  evs.push(asstEv(text, id), { after: 120 });
  if (opt.holdStream) evs.push({ wait: 1400 });
  if (!opt.noComplete) evs.push(doneEv(id), { after: 200 });
  return evs;
}

// ── DOM 扫描:一次取全(图 / 链接 / 附件卡 / 活元素 / 请求账目由外面合并)─────────────────
async function scan(page) {
  return page.evaluate(() => {
    const BAD_SCHEME = /^(javascript|vbscript|data:text\/html|data:image\/svg)/;
    const clean = (v) => String(v || '').replace(/[\u0000-\u0020\u00ad]+/g, '').toLowerCase();
    const tl = document.getElementById('timeline');
    const out = {
      mdImgs: [], mdLinks: [], mdSvg: 0, attachments: [], longtext: 0, mdCount: 0,
      onAttrs: [], badScheme: [], live: [], canary: null, text: '', html: '', legit: {},
    };
    for (const el of document.querySelectorAll('*')) {
      for (const a of Array.from(el.attributes || [])) {
        if (/^on/i.test(a.name)) out.onAttrs.push(`${el.tagName.toLowerCase()}@${a.name}="${a.value.slice(0, 60)}"`);
        if (/^(href|src|xlink:href|action|formaction|poster|data)$/i.test(a.name) && BAD_SCHEME.test(clean(a.value))) {
          out.badScheme.push(`${el.tagName.toLowerCase()}@${a.name}="${a.value.slice(0, 90)}"`);
        }
      }
    }
    if (!tl) return out;
    out.text = tl.textContent || '';
    out.html = tl.innerHTML || '';
    out.mdCount = tl.querySelectorAll('.md').length;
    out.longtext = tl.querySelectorAll('.turn-assistant.longtext').length;
    tl.querySelectorAll('.md img').forEach((im) => out.mdImgs.push({
      src: im.getAttribute('src') || '', alt: im.getAttribute('alt') || '', cls: im.className || '',
      dataMdImg: im.hasAttribute('data-md-img'), complete: !!im.complete, naturalWidth: im.naturalWidth || 0,
    }));
    tl.querySelectorAll('.md a').forEach((a) => out.mdLinks.push({ href: a.getAttribute('href') || '', text: a.textContent || '' }));
    out.mdSvg = tl.querySelectorAll('.md svg').length;
    tl.querySelectorAll('.attachment').forEach((b) => out.attachments.push({
      url: b.dataset.fileUrl || '', dispatch: b.dataset.dispatch || '', kind: b.dataset.kind || '',
      rendered: b.dataset.rendered || '', imgs: b.querySelectorAll('img').length,
      dls: [...b.querySelectorAll('a.attachment-dl')].map((a) => a.textContent || ''),
      note: (b.querySelector('.attachment-note') || {}).textContent || '',
    }));
    tl.querySelectorAll('.turn-assistant .event-content script, .turn-assistant .event-content iframe, .turn-assistant .event-content object, .turn-assistant .event-content embed, .turn-assistant .event-content svg').forEach((el) => out.live.push(el.tagName.toLowerCase()));
    out.legit = {
      strong: tl.querySelectorAll('strong').length, em: tl.querySelectorAll('em').length,
      a: tl.querySelectorAll('.md a[href^="https://"], .md a[href^="http://"]').length,
      code: tl.querySelectorAll('code').length, pre: tl.querySelectorAll('pre').length,
      table: tl.querySelectorAll('table').length, heading: tl.querySelectorAll('h1,h2,h3').length,
      quote: tl.querySelectorAll('blockquote').length, br: tl.querySelectorAll('br').length,
      checkbox: tl.querySelectorAll('input[type=checkbox]').length,
    };
    out.canary = typeof window.__dwPwned === 'undefined' ? null : String(window.__dwPwned);
    return out;
  });
}

function breachOf(s) {
  return [
    s.canary ? `🔴 canary 被置(window.__dwPwned=${s.canary})⇒ 有脚本真执行了` : null,
    s.onAttrs.length ? `🔴 on* 事件属性 ${s.onAttrs.length} 处:${s.onAttrs.slice(0, 4).join(' | ')}` : null,
    s.badScheme.length ? `🔴 危险协议 ${s.badScheme.length} 处:${s.badScheme.slice(0, 4).join(' | ')}` : null,
    s.live.length ? `🔴 消息容器内出现活元素:${s.live.slice(0, 6).join(' | ')}` : null,
    s.mdSvg ? `🔴 .md 里出现 <svg> ${s.mdSvg} 个 —— 派工单④:绝不内联 SVG 源码` : null,
  ].filter(Boolean);
}

// ── 跑一例:起夹具 → 开页 → 真灌 → (按 probe)扫 DOM ─────────────────────────────────────
async function runCase(browser, c) {
  const ctxRef = { v: {} };
  const srv = await startServer({
    bytes: c.bytes,
    files: c.files,
    eventsFor: (base) => { const p = c.prepare(base); ctxRef.v = p.ctx || {}; return p.events; },
  });
  const base = srv.url;
  const page = await browser.newPage({ viewport: VP });
  const out = { name: c.name, base, ctx: ctxRef.v, dialogs: [], pageErrors: [], requests: [], external: [], scans: [] };
  page.on('dialog', async (d) => { out.dialogs.push(`${d.type()}:${d.message().slice(0, 60)}`); try { await d.dismiss(); } catch {} });
  page.on('pageerror', (e) => out.pageErrors.push(String(e.message).slice(0, 120)));
  // 🔴 请求账目:凡不是本夹具 origin 的都记为外部(G4 钉"不对任意外站 URL 做任何请求")
  page.on('request', (req) => {
    const u = req.url();
    out.requests.push(u);
    let host = '';
    try { const p = new URL(u); host = `${p.hostname}:${p.port || ''}`; } catch { host = u.slice(0, 40); }
    if (host !== `127.0.0.1:${srv.port}`) out.external.push(u);
  });
  try {
    await page.goto(`${base}view/${encodeURIComponent(SESSION)}`, { waitUntil: 'domcontentloaded', timeout: WAIT_MS });
    for (const step of c.probe) await step(page, out, base);
  } catch (e) {
    out.error = `${e.message.split('\n')[0]}`;
  } finally {
    await page.close().catch(() => {});
    await srv.close().catch(() => {});
  }
  return out;
}

// 🔴 等待条件与变异无关:等 SENTINEL 出现在 #timeline 的 HTML 里,不等 <img>(纪律见文件头)
const waitSentinel = (page) => page.waitForFunction(
  (s) => ((document.getElementById('timeline') || {}).innerHTML || '').includes(s), SENTINEL, { timeout: WAIT_MS })
  .catch(() => { throw new Error(`等不到 #timeline 里出现 SENTINEL ⇒ 正文根本没渲染,这一格会是空洞的绿,先修装置`); });
const settle = async (page, out, at) => { await sleep(SETTLE_MS); out.scans.push({ at, s: await scan(page) }); };

// ── 图片语法专属的注入形状(Q 系列)——🔵 与共享清单里的 P 系列互补,不重复 ─────────────────
// u(name, ext) ⇒ 本站 /f/ 直链(32 hex,形状照 nginx 的 location 正则)
const SVG_B64 = Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" onload="${CANARY}"></svg>`, 'utf8').toString('base64');
function qPayloads(u) {
  return [
    { id: 'Q01', text: `![x](javascript:${CANARY})`, why: '图片语法塞 javascript: 协议' },
    { id: 'Q02', text: `![x](JaVaScRiPt:${CANARY})`, why: '图片语法 + 协议名大小写绕过' },
    { id: 'Q03', text: `![x](data:image/svg+xml;base64,${SVG_B64})`, why: '🔴 data:image/svg+xml(内联 SVG 源码最短的一条路)' },
    { id: 'Q04', text: `![x](https://ext.example/a.png"onerror="${CANARY})`, why: '外站 + 引号突破(无空格无右括号 ⇒ 能穿过 [^)\\s]+ 的捕获)' },
    { id: 'Q05', text: `![x](${u('q5', 'png').slice(0, -3)}png"onerror="${CANARY})`, why: '🔴 **本站** + 双引号突破:本站边界不能代替转义' },
    { id: 'Q06', text: `![x](${u('q6', 'png').slice(0, -3)}png'onload='${CANARY})`, why: '本站 + **单引号**突破' },
    { id: 'Q07', text: `![<img src=x7 onerror="${CANARY}">](${u('q7', 'png')})`, why: '🔴 payload 在 **alt 文本**里:alt 会变成属性值' },
    { id: 'Q08', text: `<img src="${u('q8', 'png')}" onerror="${CANARY}">`, why: '裸 HTML img(本站 URL)必须仍是死文本' },
    { id: 'Q09', text: '```md\n' + `![x](${u('q9', 'png')})\n` + '```', why: '代码围栏里的图片语法必须是死文本' },
  ];
}
// P 系列里与"URL/图片"相关的子集(🔴 复用共享清单,不另起炉灶)
const IMAGE_RELEVANT = ['P02', 'P04', 'P06', 'P07', 'P08', 'P09', 'P10', 'P11', 'P12', 'P13', 'P16', 'P17'];

// ── 五条变异:本进程内合成,锚点须恰好命中一次 ──────────────────────────────────────────
const MUTATIONS = {
  'M-G1': {
    label: '图片判据掏空(isInlineImageUrl 恒 false)',
    steps: [[
      `      return isOwnFileUrl(url) && kindFromExt(url) === 'image';`,
      `      return false;`,
    ]],
    moved: (before, after) => `就地 <img> ${before.mdImgs}→${after.mdImgs} · 感叹号 ${before.bang}→${after.bang}(信息项,见下)`,
    // 🔴 只判 <img> 那一维。第一跑我写成 `&& bang === true`,**红了 —— 错的是我的预测,不是探针**:
    //    产品里感叹号是被 `(!?)` **无条件**吃掉的(业主:「感叹号可以不用了」),与"判成图片"无关
    //    ⇒ 掏空图片判据后,`![图](url)` 变成**没有感叹号的链接**,bang 仍然是 false。
    //    🔵 "非图片目标也不许有感叹号"这件事由 **G2** 单独钉(它灌的全是非图片扩展名)。
    breached: (m) => m.mdImgs === 0,
    want: 'G1 的 16 张就地 <img> 全没了(派工单①那一维)',
  },
  'M-G2': {
    label: '拆掉写死的 deny-set ＋ 往表里塞 svg:image(④ 那层守卫)',
    steps: [
      [`      if (FILE_EXT_NEVER_INLINE.has(ext)) return 'download';`, `      if (false) return 'download';`],
      [`      png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image',`,
       `      png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image', svg: 'image',`],
    ],
    moved: (before, after) => `.svg 就地 <img> ${before.svgImgs}→${after.svgImgs} · .md 里 <svg> ${before.mdSvg}→${after.mdSvg}`,
    breached: (m) => m.svgImgs > 0,
    want: '.svg 变成 <img>(说明"写死那一层"真的是承重的,不是靠名单里没写)',
    diag: (out) => {
      const s = last(out);
      const svgSrc = (out.ctx.expect.find((e) => e.tag === 'svg') || {}).url || '';
      return `就地 <img> ${JSON.stringify(s.mdImgs.map((i) => ({ src: i.src.slice(-24), complete: i.complete, w: i.naturalWidth })))}`
        + ` · svg 那条 URL 在 HTML 里出现 ${(s.html.split(svgSrc).length - 1)} 次 · 附件卡 ${s.attachments.length} 张`;
    },
  },
  'M-G3': {
    label: 'escapeHtml 去掉 " → &quot;(ts-No.76 认定的承重墙)',
    steps: [[
      `        .replaceAll('"', '&quot;')\n`,
      ``,
    ]],
    moved: (before, after) => `on* 属性 ${before.onAttrs}→${after.onAttrs} · 活元素 ${before.live}→${after.live} · canary ${before.canary}→${after.canary}`,
    breached: (m) => m.onAttrs > 0 || m.live > 0 || m.canary !== null,
    want: '本站 + 引号突破那条(Q05)真的突破出 on* 属性',
  },
  'M-G4': {
    label: '去掉"附件卡让位"(一图两处)',
    // 🔴 两处都得改:让位规则在**建卡前**与**探测回来时**各有一道。
    //    只拆前一道 ⇒ 后一道会把卡再撤掉 ⇒ 观测量不动 ⇒ 这条对照会**假报"探针没牙"**(实际是变异没生效)。
    steps: [
      [
        `        if (url === openTail || longerExists(url) || seen.has(url) || inlineShown.has(url)) continue;`,
        `        if (url === openTail || longerExists(url) || seen.has(url)) continue;`,
      ],
      [
        `            if (mdInlineImageUrls(cardEl).has(url)) { slot.remove(); return; }`,
        `            // (M-G4 变异:让位规则第二道已拆)`,
      ],
      [
        `      for (const el of [...cardEl._fileHost.children]) {
        if (el.dataset && el.dataset.fileUrl === url) el.remove();
      }`,
        `      // (M-G4 变异:让位规则第三道已拆)`,
      ],
    ],
    // 🔴 为什么是三道:让位规则有三层(建卡前跳过 / HEAD 探测回来时撤卡 / img load 事件里撤卡)。
    //    第一跑我只拆了第一道 ⇒ **观测量一动不动** ⇒ 那条对照红成"探针没牙"。
    //    🔵 真相是**变异等价**(剩下两层照样把重复卡撤掉),不是探针瞎 —— 纪律要求把这两者分清,
    //    所以这里把三层一次拆干净;顺带量出一件有用的事:**这条规则是三层冗余的,拆一层不够**。
    moved: (before, after) => `同一 URL 的附件卡 ${before.cards}→${after.cards}(就地 <img> 恒为 ${after.imgs})`,
    breached: (m) => m.cards > 0 && m.imgs > 0,
    want: '同一张图既就地出图、又出一张附件卡',
  },
  'M-G5': {
    label: '去掉本站 /f/ 边界(外站图也就地渲染)',
    steps: [[
      `      return isOwnFileUrl(url) && kindFromExt(url) === 'image';`,
      `      return kindFromExt(url) === 'image';`,
    ]],
    moved: (before, after) => `外站就地 <img> ${before.extImgs}→${after.extImgs} · 外部请求 ${before.external}→${after.external}`,
    breached: (m) => m.extImgs > 0 || m.external > 0,
    want: '外站图片 URL 变成 <img>,并且**真的发出了外部请求**',
  },
};
function buildMutation(name, srcText) {
  const m = MUTATIONS[name];
  assert.ok(m, `未知变异 ${name}`);
  let s = srcText;
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

// ══════════════════════════════════════════════════════════════════════════════════
// 用例
// ══════════════════════════════════════════════════════════════════════════════════
const last = (out) => { assert.ok(out.scans.length, '一次扫描都没做 ⇒ 装置没跑到断言'); return out.scans[out.scans.length - 1].s; };
const BASE_RUNS = {};       // 产品字节那几例的完整 out,给阳性对照当 "before"

function assertNoBreach(s, at) {
  const b = breachOf(s);
  assert.deepStrictEqual(b, [], `🔴 [${at}] 出现了不该有的东西:\n     ${b.join('\n     ')}`);
  assert.deepStrictEqual(s.canary, null, `🔴 [${at}] canary 被置(${s.canary})⇒ 有脚本真执行了`);
}

// 两条渲染路径各扫一次:流式 innerHTML(turn_completed 之前)＋ 回合重建(之后)
function probeBoth() {
  return [
    async (page, out) => {
      await waitSentinel(page);
      await sleep(SETTLE_MS);
      out.scans.push({ at: '流式渲染中(turn_completed 之前)', s: await scan(page) });
    },
    async (page, out) => {
      await page.waitForSelector('#timeline .turn-assistant .event-content .md', { timeout: WAIT_MS, state: 'attached' })
        .catch(() => { throw new Error('等不到回合重建后的 .md ⇒ 正文没进回合重建路径,先修装置(这条等待与变异无关)'); });
      await sleep(SETTLE_MS);
      out.scans.push({ at: '回合重建后(turn_completed 之后)', s: await scan(page) });
    },
  ];
}

// ── G0 静态对账:renderer 的图片判据 == nginx 的内联图片白名单(不许另写一份清单)──────────
const G0 = {
  name: 'G0', title: '🔴 静态对账:图片扩展名判据只有一份,且 == nginx `types{image/*}`(派工单①「不许另写一份清单」)',
  run: () => {
    // (a) nginx 侧解析自证:解析不出来就是**我的锚点过期**,不是产品问题
    assert.ok(NGINX_IMAGE_EXTS.length >= 1, '🔴 从 nginx conf 解析不到任何 `image/* ext;` 行 ⇒ 解析锚点过期(装置问题)');
    console.log(`\n     nginx 内联图片白名单(${NGINX_IMAGE_EXTS.length}):${NGINX_IMAGE_EXTS.join(' ')}`);
    const notServed = NGINX_IMAGE_EXTS.filter((e) => !NGINX_SERVED_EXTS.has(e));
    assert.deepStrictEqual(notServed, [],
      `🔴 nginx 把 ${notServed.join(',')} 声明成 image/* 却不在 /f/ 的 location 正则里 ⇒ 那种图在生产上根本取不到(404)`);
    // (b) renderer 侧
    const { imageRows, never } = readRendererLists(RENDERER_BYTES.toString('utf8'));
    console.log(`     renderer FILE_EXT_KIND 的 image 行(${imageRows.length}):${imageRows.join(' ')}`);
    console.log(`     renderer FILE_EXT_NEVER_INLINE(${never.length}):${never.join(' ')}`);
    const missing = NGINX_IMAGE_EXTS.filter((e) => !imageRows.includes(e));
    assert.deepStrictEqual(missing, [],
      `🔴 漂移:nginx 认作图片、renderer 的表里却不是 image ⇒ 这些扩展名的图**不会**就地显示:${missing.join(' ')}`
      + '\n     修法只有一条:改 FILE_EXT_KIND(它是**唯一**那份清单),不要在 markdown 那边再列一份。');
    // 🔴 反向也要钉:表里多出来的必须**只有** jpeg 这一个已登记别名(mime image/jpeg 的两个写法),
    //    否则"多出来的"就是第二份清单的种子。
    const extra = imageRows.filter((e) => !NGINX_IMAGE_EXTS.includes(e));
    assert.deepStrictEqual(extra, ['jpeg'],
      `🔴 FILE_EXT_KIND 的 image 行比 nginx 多了 ${JSON.stringify(extra)}(允许的只有已登记别名 'jpeg')`
      + '\n     多出来的每一个都得说出依据,否则就是漂移。');
    // (c) ④ 那层"写死"的守卫:6 个 token 逐个必须在(与 0206 件 ④h 同一手法:绝对断言,不只断"不相交")
    for (const must of ['svg', 'svgz', 'html', 'htm', 'xhtml', 'xml']) {
      assert.ok(never.includes(must), `🔴 FILE_EXT_NEVER_INLINE 缺少 ${must}(派工单④:svg 要**写死**,不靠名单兜)`);
    }
    const leaked = never.filter((e) => imageRows.includes(e));
    assert.deepStrictEqual(leaked, [], `🔴 永不内联的扩展名混进了 image 行:${leaked.join(' ')}`);
    // (d) 🔴 "只有一份清单"这件事本身也要有闸:图片白名单里那几个独有 token 在产品源码里只许出现一次。
    //     出现第二次 ⇒ 有人另写了一份(两份必漂移)⇒ 本条红。
    const src = RENDERER_BYTES.toString('utf8');
    for (const tok of ['avif', 'heic', 'tiff']) {
      const n = (src.match(new RegExp('\\b' + tok + '\\b', 'g')) || []).length;
      assert.strictEqual(n, 1,
        `🔴 '${tok}' 在产品源码里出现 ${n} 次(应恰好 1 次 = FILE_EXT_KIND 那一行)`
        + ' ⇒ 有人另写了一份图片清单。两份必漂移,请改为复用 FILE_EXT_KIND/kindFromExt。');
    }
    // (e) markdown 那条分支必须**真的**去问那两处判据(而不是自己另判一遍)
    const pred = (src.match(/function isInlineImageUrl\(url\) \{[\s\S]{0,220}?\n {4}\}/) || [])[0];
    assert.ok(pred, '🔴 源码里找不到 isInlineImageUrl(url) —— markdown 图片分支的判据函数被改名/删掉了?');
    assert.ok(/kindFromExt\(/.test(pred), `🔴 isInlineImageUrl 没走 kindFromExt(那份唯一清单)——\n${pred}`);
    assert.ok(/isOwnFileUrl\(/.test(pred), `🔴 isInlineImageUrl 没走 isOwnFileUrl(本站 /f/ 边界)——\n${pred}`);
    console.log('     🔵 判据链:markdown → isInlineImageUrl → isOwnFileUrl(本站边界) ＋ kindFromExt(FILE_EXT_NEVER_INLINE → FILE_EXT_KIND)');
    console.log('     🔵 单一事实源:deploy/nginx-dingwei.conf 的 types{image/*};renderer 只有一份 FILE_EXT_KIND,由本条对账');
  },
};

// ── G0b 夹具自证(🔵 量装置,不是产品断言)────────────────────────────────────────────
function caseG0b(bytes) {
  return {
    name: 'G0b', bytes, files: [{ name: 'selfproof', ext: 'png' }, { name: 'selfsvg', ext: 'svg' }],
    title: '🔵 夹具自证:图片字节真能解码 · 本站边界前提成立 · nginx 不放行的扩展名夹具也 404',
    prepare: (base) => ({
      events: eventsFor(`${SENTINEL} 夹具自证(正文不含图片语法)`, 'd-g0b'),
      ctx: { png: base + relOf('selfproof', 'png'), svg: base + relOf('selfsvg', 'svg'), base },
    }),
    probe: [async (page, out) => {
      await waitSentinel(page);
      out.selfproof = await page.evaluate(async ({ png, svg, base }) => {
        const loadIt = (u) => new Promise((res) => {
          const im = new Image();
          im.onload = () => res({ ok: true, w: im.naturalWidth, h: im.naturalHeight });
          im.onerror = () => res({ ok: false });
          im.src = u;
        });
        const st = async (u, method) => { try { const r = await fetch(u, { method }); return r.status; } catch { return -1; } };
        return {
          pngLoad: await loadIt(png),
          pngHead: await st(png, 'HEAD'), pngGet: await st(png, 'GET'),
          svgStatus: await st(svg, 'GET'),
          ownPng: isOwnFileUrl(png), ownSvg: isOwnFileUrl(svg),
          external: isOwnFileUrl('https://ext.example/a.png'),
          kindPng: kindFromExt(png), kindSvg: kindFromExt(svg),
        };
      }, out.ctx);
      // 🔵 也扫一次 DOM:① 公共守卫要求每例至少一次扫描 ② 顺手证明自证那段正文里一张图都没出
      out.scans.push({ at: '夹具自证', s: await scan(page) });
    }],
    check: (out) => {
      const p = out.selfproof;
      assert.strictEqual(last(out).mdImgs.length, 0, '🔴 自证那例的正文里没有任何图片语法,却出了就地 <img> ⇒ 判据不受语法控制');
      assert.ok(p, '装置异常:自证那段没跑到');
      assert.ok(p.pngLoad.ok && p.pngLoad.w > 0,
        `🔴 夹具的 PNG 字节解不出来(${JSON.stringify(p.pngLoad)})⇒ 后面所有"加载成功后附件卡让位"的腿都会变成空洞的绿。先修夹具。`);
      assert.strictEqual(p.pngHead, 200, '夹具 HEAD 不是 200 ⇒ 附件卡会走降级路径,量的不是主路径');
      assert.strictEqual(p.pngGet, 200, '夹具 GET 不是 200');
      assert.strictEqual(p.svgStatus, 404,
        '🔴 夹具把 .svg 也当 200 出了 ⇒ 与生产不同形(nginx 的 `location /f/ { return 404; }`)⇒ 阴格会失真');
      assert.strictEqual(p.ownPng, true, 'isOwnFileUrl(本站 /f/ png) 应为 true ⇒ 边界前提不成立');
      assert.strictEqual(p.external, false, 'isOwnFileUrl(外站 png) 应为 false ⇒ 边界前提不成立');
      assert.strictEqual(p.kindSvg, 'download',
        `🔴 kindFromExt(.svg)=${p.kindSvg}(应 download)⇒ ④ 那层"写死"的守卫不在执行路径上`);
      console.log(`\n     🔵 自证:PNG ${p.pngLoad.w}x${p.pngLoad.h} 可解码 · HEAD/GET 200 · .svg 404(与 nginx 同形)`);
      console.log(`     🔵 自证:isOwnFileUrl 本站=true 外站=false · kindFromExt(png)=${p.kindPng} kindFromExt(svg)=${p.kindSvg}`);
      console.log('     🔵 这三行是**量装置**:它们证明后面几格的前提成立,不是产品结论');
    },
  };
}

// ── G1 阳格:nginx 白名单每种扩展名 × 两条语法(![…] 与裸链接)⇒ 就地 <img> ──────────────
function caseG1(bytes) {
  const exts = NGINX_IMAGE_EXTS;
  const files = [];
  exts.forEach((e) => { files.push({ name: 'g1a-' + e, ext: e }, { name: 'g1b-' + e, ext: e }); });
  return {
    name: 'G1', bytes, files,
    title: `🔴 阳格:${exts.length} 种图片扩展名 × 两条语法(「![alt](url)」与「[text](url)」)⇒ 全部就地渲染成 <img>,且感叹号消失`,
    prepare: (base) => {
      const u = (n, e) => base + relOf(n, e);
      const expect = [];
      const lines = [`${SENTINEL} 阳格:${exts.length} 种扩展名 × 两条语法`];
      for (const e of exts) {
        const a = u('g1a-' + e, e); const b = u('g1b-' + e, e);
        lines.push(`![图${e}](${a})`);
        lines.push(`[链接${e}](${b})`);
        expect.push({ url: a, alt: `图${e}`, via: `![…](${e})` }, { url: b, alt: `链接${e}`, via: `[…](${e})` });
      }
      const text = lines.join('\n\n');
      assert.ok(text.length < 4000, `装置问题:正文 ${text.length} 字会触发超长摘要路径(LONGTEXT_THRESHOLD=4000)⇒ 这一格验的不是就地渲染`);
      return { events: eventsFor(text, 'd-g1', { holdStream: true }), ctx: { expect } };
    },
    probe: probeBoth(),
    check: (out) => {
      const { expect } = out.ctx;
      for (const { at, s } of out.scans) {
        assertNoBreach(s, at);
        assert.strictEqual(s.longtext, 0, `🔴 [${at}] 守卫:走了超长摘要路径 ⇒ 本例量的不是就地渲染`);
        assert.ok(s.mdCount >= 1, `🔴 [${at}] 守卫:.md 一个都没有 ⇒ 正文根本没渲染,这一格是空洞的绿`);
        assert.strictEqual(s.mdImgs.length, expect.length,
          `🔴 [${at}] 就地 <img> ${s.mdImgs.length} 张,应 ${expect.length} 张(派工单①②)`
          + `\n     实测 src 末段:${JSON.stringify(s.mdImgs.map((i) => i.src.split('/').pop()).slice(0, 20))}`);
        for (const e of expect) {
          const hit = s.mdImgs.filter((i) => i.src === e.url);
          assert.strictEqual(hit.length, 1, `🔴 [${at}] ${e.via} 的就地 <img> 数=${hit.length}(应恰好 1)`);
          assert.ok(hit[0].dataMdImg, `🔴 [${at}] ${e.via} 的 <img> 没有 data-md-img 标记 ⇒ 它不是 markdown 那条路产出的`);
          assert.strictEqual(hit[0].alt, e.alt, `🔴 [${at}] ${e.via} 的 alt=${JSON.stringify(hit[0].alt)}(应 ${JSON.stringify(e.alt)})`);
          assert.ok(hit[0].complete && hit[0].naturalWidth > 0,
            `🔴 [${at}] 守卫:${e.via} 这张图没真解码(complete=${hit[0].complete} naturalWidth=${hit[0].naturalWidth})`
            + ' ⇒ "加载成功后附件卡让位"的前提不成立,去重那几格会是空洞的绿。先修夹具字节。');
        }
        const stillLinks = s.mdLinks.filter((a) => expect.some((e) => e.url === a.href));
        assert.deepStrictEqual(stillLinks, [],
          `🔴 [${at}] 这些图片 URL 仍被渲染成链接(派工单①):${JSON.stringify(stillLinks.slice(0, 4))}`);
        assert.ok(!s.text.includes('!图') && !s.text.includes('!['),
          `🔴 [${at}] 感叹号还在(派工单②:业主「感叹号可以不用了」)—— 正文片段:${JSON.stringify(s.text.slice(0, 160))}`);
        const dupCards = s.attachments.filter((a) => expect.some((e) => e.url === a.url));
        assert.deepStrictEqual(dupCards, [],
          `🔴 [${at}] 一图两处:同一 URL 既就地出图又出附件卡 ${JSON.stringify(dupCards.map((c) => c.url.split('/').pop()))}`);
        console.log(`\n     🔵 [${at}] 就地 <img> ${s.mdImgs.length}/${expect.length} · 全部真解码 · 感叹号 0 · 重复附件卡 0 · on*/危险协议/活元素 0`);
      }
    },
  };
}

// ── G2 阴格①:非图片类型 ⇒ 仍是链接,绝不变成图 ──────────────────────────────────────
function caseG2(bytes) {
  const exts = ['svg', 'exe', 'html', 'php', 'pdf', 'mp4', 'txt', 'docx', 'bin', ''];
  const files = exts.flatMap((e) => [{ name: 'g2a-' + (e || 'noext'), ext: e }, { name: 'g2b-' + (e || 'noext'), ext: e }]);
  // 🔴 故意比生产**更宽**:让 .svg 真能取到(image/svg+xml)。
  //    这样"svg 不出图"就只可能来自产品里那层**写死**的 deny-set(FILE_EXT_NEVER_INLINE),
  //    而不是来自"nginx 反正 404"。🔵 没有这一步,M-G2 那条对照的观测量根本不动(实测过)。
  files.filter((f) => f.ext === 'svg').forEach((f) => { f.mime = 'image/svg+xml'; f.body = SVG_BYTES; });
  return {
    name: 'G2', bytes, files,
    title: '🔴 阴格①:非图片扩展名(svg/exe/html/php/pdf/mp4/txt/docx/bin/无扩展)⇒ **仍是链接**,一张图都不许出;svg 也绝不进 <svg>',
    prepare: (base) => {
      const u = (n, e) => base + relOf(n, e);
      const expect = [];
      const lines = [`${SENTINEL} 阴格①:非图片类型`];
      for (const e of exts) {
        const tag = e || 'noext';
        const a = u('g2a-' + tag, e); const b = u('g2b-' + tag, e);
        lines.push(`![危险${tag}](${a})`);
        lines.push(`[链接${tag}](${b})`);
        expect.push({ url: a, tag }, { url: b, tag });
      }
      const text = lines.join('\n\n');
      assert.ok(text.length < 4000, `装置问题:正文 ${text.length} 字会触发超长摘要路径`);
      return { events: eventsFor(text, 'd-g2', { holdStream: true }), ctx: { expect } };
    },
    probe: probeBoth(),
    check: (out) => {
      const { expect } = out.ctx;
      for (const { at, s } of out.scans) {
        assertNoBreach(s, at);
        assert.strictEqual(s.mdImgs.length, 0,
          `🔴 [${at}] 非图片类型也就地出图了 ${s.mdImgs.length} 张:${JSON.stringify(s.mdImgs.map((i) => i.src.split('/').pop()))}`
          + '\n     派工单阴格①:非白名单扩展**必须仍是链接,不得变成图**');
        assert.strictEqual(s.mdSvg, 0, `🔴 [${at}] .md 里出现 <svg> ${s.mdSvg} 个(派工单④:绝不内联 SVG 源码)`);
        for (const e of expect) {
          assert.ok(s.mdLinks.some((a) => a.href === e.url),
            `🔴 [${at}] ${e.tag} 的 URL 不再是链接了(派工单③:非图片链接行为不变)`);
        }
        assert.ok(!s.text.includes('!危险'),
          `🔴 [${at}] 感叹号还在(派工单②)—— 正文片段:${JSON.stringify(s.text.slice(0, 160))}`);
        const cardImgs = s.attachments.filter((a) => a.imgs > 0);
        assert.deepStrictEqual(cardImgs, [],
          `🔴 [${at}] 附件卡里出现了 <img>(说明非图片被判成了图片):${JSON.stringify(cardImgs.map((c) => c.url.split('/').pop()))}`);
        assert.ok(s.attachments.length >= expect.length,
          `🔴 [${at}] 守卫:附件卡只有 ${s.attachments.length} 张(< ${expect.length})⇒ "没出图"可能只是因为什么都没渲染,这一格是空的`);
        console.log(`\n     🔵 [${at}] 就地 <img> 0 · <svg> 0 · ${expect.length} 条 URL 全部仍是链接 · 附件卡 ${s.attachments.length} 张(其中出图的 0 张)`);
      }
    },
  };
}

// ── G3 阴格③:注入腿(复用 ts-No.76 的相关形状 ＋ 9 条图片语法专属形状)──────────────────
function caseG3(bytes) {
  return {
    name: 'G3', bytes, files: ['q5', 'q6', 'q7', 'q8', 'q9'].map((n) => ({ name: n, ext: 'png' })),
    title: `🔴 阴格③:注入腿 —— ts-No.76 的 ${IMAGE_RELEVANT.length} 条相关形状 ＋ 图片语法专属 ${9} 条 ⇒ 一律不得变活`,
    prepare: (base) => {
      const u = (n, e) => base + relOf(n, e);
      const qs = qPayloads(u);
      const ps = PAYLOADS.filter((p) => IMAGE_RELEVANT.includes(p.id));
      const text = [`${SENTINEL} 注入腿`, ...ps.map((p) => p.text), ...qs.map((q) => q.text)].join('\n\n');
      assert.ok(text.length < 4000, `装置问题:正文 ${text.length} 字会触发超长摘要路径`);
      return {
        events: eventsFor(text, 'd-g3', { holdStream: true }),
        ctx: {
          qs, ps, q7url: u('q7', 'png'),
          // 反证("被转义了"而不是"被悄悄丢掉了"):停在文本里的形状用转义形态核;
          // 被链接正则吃掉的形状(Q04/Q05/Q06/P12)核**属性里那个 &quot;**。
          textFrags: ['Q01', 'Q02', 'Q03', 'Q08'].map((id) => escTextNode(qs.find((q) => q.id === id).text)),
        },
      };
    },
    probe: probeBoth(),
    check: (out) => {
      const { qs, q7url, textFrags } = out.ctx;
      assert.deepStrictEqual(out.dialogs, [], `🔴 弹出了对话框(说明脚本真执行了):${JSON.stringify(out.dialogs)}`);
      assert.deepStrictEqual(out.pageErrors, [], `🔴 页面报错:${JSON.stringify(out.pageErrors)}`);
      for (const { at, s } of out.scans) {
        assertNoBreach(s, at);
        assert.ok(s.mdCount >= 1, `🔴 [${at}] 守卫:.md 一个都没有 ⇒ 这一格是空洞的绿`);
        // 🔴 只允许**一条**就地出图:Q07 的 URL 是干净的本站 .png(它的 payload 在 alt 文本里)
        assert.strictEqual(s.mdImgs.length, 1,
          `🔴 [${at}] 注入例里就地出图 ${s.mdImgs.length} 张(应恰好 1:只有 Q07 的 URL 是干净本站 .png)`
          + `\n     实测:${JSON.stringify(s.mdImgs.map((i) => i.src.slice(-46)))}`);
        assert.strictEqual(s.mdImgs[0].src, q7url, `🔴 [${at}] 就地出图的那张不是 Q07 的 URL:${s.mdImgs[0].src.slice(-60)}`);
        assert.ok(s.mdImgs[0].alt.includes('<img src=x7 onerror='),
          `🔴 [${at}] Q07 的 alt 不是死文本(它必须原样是文字,不得变成元素):${JSON.stringify(s.mdImgs[0].alt)}`);
        assert.strictEqual(s.onAttrs.length, 0, `🔴 [${at}] on* 属性 ${s.onAttrs.length} 处:${s.onAttrs.slice(0, 3).join(' | ')}`);
        // 反证:payload 以**转义形态**可见(丢字不算安全,算失真)
        for (const frag of textFrags) {
          assert.ok(s.html.includes(frag), `🔴 [${at}] 反证失败:payload 的转义形态没出现「${frag.slice(0, 50)}…」⇒ 它是被悄悄丢掉了`);
        }
        assert.ok(s.html.includes('&quot;onerror=&quot;'),
          `🔴 [${at}] 反证失败:引号突破那几条(Q04/Q05/Q06/P12)的 &quot; 没出现在 HTML 里`
          + ' ⇒ escapeHtml 的 " → &quot; 那道承重墙不在(ts-No.76 P12 认定它是承重的)');
        // Q09 必须落在代码围栏里(死文本),不得就地出图
        assert.ok((s.html.match(/<pre>/g) || []).length >= 1, `🔴 [${at}] 守卫:代码围栏没渲染出来 ⇒ Q09 那一格是空的`);
        console.log(`\n     🔵 [${at}] 注入 ${IMAGE_RELEVANT.length + qs.length} 形状:on* 0 · 危险协议 0 · 活元素 0 · canary 未置 · 就地出图仅 Q07 那一张(alt 是死文本)`);
      }
    },
  };
}

// ── G4 边界腿:外站图片 URL ⇒ 仍是链接,且**一个外部请求都不发** ───────────────────────
function caseG4(bytes) {
  return {
    name: 'G4', bytes, files: [],
    title: '🔴 边界腿:外站图片 URL 仍是链接,且**外部请求数 = 0**(钉产品件自己那句「不对任意外站 URL 做任何请求」)',
    prepare: () => ({
      events: eventsFor([
        `${SENTINEL} 外站边界`,
        '![外站图](https://ext.example/a.png)',
        '[外站链](https://ext.example/b.png)',
        'https://ext.example/c.png',
      ].join('\n\n'), 'd-g4', { holdStream: true }),
      ctx: { ext: ['https://ext.example/a.png', 'https://ext.example/b.png'] },
    }),
    probe: probeBoth(),
    check: (out) => {
      for (const { at, s } of out.scans) {
        assertNoBreach(s, at);
        assert.strictEqual(s.mdImgs.length, 0,
          `🔴 [${at}] 外站图片 URL 就地出图了 ${s.mdImgs.length} 张:${JSON.stringify(s.mdImgs.map((i) => i.src))}`
          + '\n     产品件的信任边界是"只认本站 /f/ 直链";要放开这条得由业主裁,不是本格能顺手改的');
        assert.deepStrictEqual(out.external, [],
          `🔴 [${at}] 发出了 ${out.external.length} 条外部请求:${JSON.stringify(out.external.slice(0, 6))}`
          + '\n     产品件逐字写着「只认本站 /f/ 直链…**不对任意外站 URL 做任何请求**」');
        for (const href of out.ctx.ext) {
          assert.ok(s.mdLinks.some((a) => a.href === href), `🔴 [${at}] 外站链接 ${href} 没被渲染成链接(派工单③:行为不变)`);
        }
        assert.deepStrictEqual(s.attachments, [], `🔴 [${at}] 外站 URL 竟然进了附件路径:${JSON.stringify(s.attachments.map((a) => a.url))}`);
        assert.ok(!s.text.includes('!外站图'), `🔴 [${at}] 感叹号还在(派工单②)`);
      }
      const s = last(out);
      // 🔵 账目**可审**:打印总请求数与外部数,而不是只报一个 0(组长 ts-No.78 的裁定:把账目变成可审的)
      console.log(`\n     🔵 请求账目:总 ${out.requests.length} 条 · 外部 ${out.external.length} 条 · 就地 <img> ${s.mdImgs.length} 张 · 附件卡 ${s.attachments.length} 张`);
      console.log(`     🔵 外部请求逐条(应为空):${JSON.stringify(out.external)}`);
    },
  };
}

// ── G5 去重腿:一图不显示两处;裸 URL 那条老路原样不动 ────────────────────────────────
function caseG5(bytes) {
  return {
    name: 'G5', bytes, files: ['g5u1', 'g5u2', 'g5u3'].map((n) => ({ name: n, ext: 'png' })),
    title: '🔴 一图不显示两处:markdown 写法 ⇒ 就地出图且**附件卡让位**;裸 URL ⇒ 老路(卡里出图)原样不动',
    prepare: (base) => {
      const u = (n) => base + relOf(n, 'png');
      const u1 = u('g5u1'); const u2 = u('g5u2'); const u3 = u('g5u3');
      const text = [
        `${SENTINEL} 去重`,
        `![图一](${u1})`,
        u2,                                   // 🔵 裸 URL:不进 markdown 语法 ⇒ 附件卡那条老路
        `![图三](${u3})`,
        u3,                                   // 🔴 同一条 URL 既 markdown 又裸写(边界:只许一处出图)
      ].join('\n\n');
      return { events: eventsFor(text, 'd-g5', { holdStream: true }), ctx: { u1, u2, u3 } };
    },
    probe: probeBoth(),
    check: (out) => {
      const { u1, u2, u3 } = out.ctx;
      const imgsOf = (s, u) => s.mdImgs.filter((i) => i.src === u).length;
      const cardsOf = (s, u) => s.attachments.filter((a) => a.url === u);
      for (const { at, s } of out.scans) {
        assertNoBreach(s, at);
        assert.strictEqual(imgsOf(s, u1), 1, `🔴 [${at}] markdown 写法那张就地 <img>=${imgsOf(s, u1)}(应 1)`);
        assert.deepStrictEqual(cardsOf(s, u1), [],
          `🔴 [${at}] 一图两处:markdown 那张既就地出图、又出了附件卡 ${JSON.stringify(cardsOf(s, u1))}`);
        assert.strictEqual(imgsOf(s, u2), 0, `🔴 [${at}] 裸 URL 竟然也就地出图了(${imgsOf(s, u2)})⇒ 派工单③"非 markdown 的路径行为不变"被破坏`);
        const c2 = cardsOf(s, u2);
        assert.strictEqual(c2.length, 1, `🔴 [${at}] 裸 URL 的附件卡=${c2.length}(应 1)⇒ 老路被本格的改动弄坏了`);
        assert.strictEqual(c2[0].imgs, 1, `🔴 [${at}] 裸 URL 的附件卡里没有 <img>(实测 ${JSON.stringify(c2[0])})⇒ 老路退化`);
        assert.strictEqual(imgsOf(s, u3), 1, `🔴 [${at}] 同一条 URL 既 markdown 又裸写时,就地 <img>=${imgsOf(s, u3)}(应 1)`);
        assert.deepStrictEqual(cardsOf(s, u3), [], `🔴 [${at}] 同一条 URL 出了附件卡 ⇒ 一图两处`);
        const allImgs = s.mdImgs.length + s.attachments.reduce((n, a) => n + a.imgs, 0);
        assert.strictEqual(allImgs, 3, `🔴 [${at}] 整条消息里 <img> 总数=${allImgs}(应 3:两条就地 ＋ 裸 URL 卡里一条)`);
        console.log(`\n     🔵 [${at}] 就地 2 张(u1/u3)· 附件卡 1 张(u2,里面有图)· 重复 0 · <img> 总数 ${allImgs}`);
      }
    },
  };
}

// ── G6 失败腿:图取不到时**不留空白、不丢出路**(附件卡接手,含 7 天过期那句)───────────
function caseG6(bytes) {
  return {
    name: 'G6', bytes,
    files: [
      { name: 'g6bad', ext: 'png', body: BAD_BYTES },      // HEAD 说 image/png,字节解不了
      { name: 'g6gone', ext: 'png', status: 404 },          // 7 天保留期到了(HEAD/GET 都 404)
    ],
    title: '🔴 图取不到时:就地那张撤掉,**附件卡接手**(解不了码 ⇒ 下载链接;404 ⇒ 「已过 7 天保留期」那句仍在)',
    prepare: (base) => {
      const ubad = base + relOf('g6bad', 'png');
      const ugone = base + relOf('g6gone', 'png');
      const text = [
        `${SENTINEL} 失败腿`,
        `![坏图](${ubad})`,
        `![过期图](${ugone})`,
        '这一行是守卫:正文没有渲染成空。',
      ].join('\n\n');
      return { events: eventsFor(text, 'd-g6'), ctx: { ubad, ugone } };
    },
    probe: [async (page, out) => {
      await page.waitForSelector('#timeline .turn-assistant .event-content .md', { timeout: WAIT_MS, state: 'attached' })
        .catch(() => { throw new Error('等不到回合重建后的 .md ⇒ 先修装置'); });
      const { ubad, ugone } = out.ctx;
      const cardsReady = await page.waitForFunction(({ bad, gone }) => {
        const cards = [...document.querySelectorAll('#timeline .attachment')];
        const badCard = cards.find((node) => node.dataset.fileUrl === bad);
        const goneCard = cards.find((node) => node.dataset.fileUrl === gone);
        const unresolved = [...document.querySelectorAll('#timeline .md img')]
          .some((node) => node.src === bad || node.src === gone);
        return !unresolved && badCard?.querySelector('a.attachment-dl') &&
          goneCard?.dataset.dispatch === 'gone' && (goneCard.querySelector('.attachment-note')?.textContent || '').includes('7 天');
      }, { bad: ubad, gone: ugone }, { timeout: 60000 }).then(() => true, () => false);
      if (!cardsReady) console.log('🔵 G6 条件等待到 60s：', await page.evaluate(({ bad, gone }) => ({
        images: [...document.querySelectorAll('#timeline .md img')].map((node) => node.src),
        cards: [...document.querySelectorAll('#timeline .attachment')].map((node) => ({ url: node.dataset.fileUrl, dispatch: node.dataset.dispatch, dls: node.querySelectorAll('a.attachment-dl').length, note: node.querySelector('.attachment-note')?.textContent || '' })),
      }), { bad: ubad, gone: ugone }));
      out.scans.push({ at: '回合重建后', s: await scan(page) });
    }],
    check: (out) => {
      const { ubad, ugone } = out.ctx;
      const s = last(out);
      assertNoBreach(s, '回合重建后');
      assert.ok(s.text.includes('这一行是守卫'), '🔴 守卫:正文里没有那行哨兵文字 ⇒ "没出图"可能只是因为什么都没渲染');
      assert.strictEqual(s.mdImgs.length, 0,
        `🔴 就地那张没撤掉(解不了码/取不到的图留在正文里就是破图标):${JSON.stringify(s.mdImgs.map((i) => i.src.slice(-40)))}`);
      const cbad = s.attachments.filter((a) => a.url === ubad);
      const cgone = s.attachments.filter((a) => a.url === ugone);
      assert.strictEqual(cbad.length, 1, `🔴 解不了码那张的附件卡=${cbad.length}(应 1:卡要接手,不能什么都没有)`);
      assert.ok(cbad[0].dls.some((t) => t.includes('下载图片')),
        `🔴 解不了码那张没有下载出路(实测卡内链接 ${JSON.stringify(cbad[0].dls)})⇒ 比现状差`);
      assert.strictEqual(cgone.length, 1, `🔴 过期那张的附件卡=${cgone.length}(应 1)`);
      assert.strictEqual(cgone[0].dispatch, 'gone', `🔴 过期那张的 dispatch=${cgone[0].dispatch}(应 gone)`);
      assert.ok(cgone[0].note.includes('7 天'),
        `🔴 过期那句说明不见了(实测 note=${JSON.stringify(cgone[0].note)})`
        + ' ⇒ 业主看不到"为什么图没了"。这是 0.2.1.50 spec 图片的**已知现实**(直链只活 7 天),不许丢。');
      console.log(`\n     🔵 就地 <img> 0(两张都撤了)· 附件卡 2 张接手:坏图给「${cbad[0].dls.join('/')}」,过期给「${cgone[0].note.slice(0, 30)}…」`);
    },
  };
}

// ── G8 反证腿:正常 markdown 不许被这一格弄坏 ────────────────────────────────────────
function caseG8(bytes) {
  return {
    name: 'G8', bytes, files: [{ name: 'g8img', ext: 'png' }],
    title: '🔵 反证(防"渲染成空也算通过"):正常 markdown 全在,且与就地图片共存',
    prepare: (base) => {
      const uimg = base + relOf('g8img', 'png');
      const text = [
        `${SENTINEL} 反证`,
        '# 正常标题',
        '**粗体** 与 *斜体* 与 `行内代码` 与 [正常链接](https://example.com/a)',
        '> 正常引用',
        '- [ ] 待办项',
        '- 普通列表项',
        '| 列一 | 列二 |',
        '| --- | --- |',
        '| 值一 | 值二 |',
        '```python',
        'def f():',
        '    return 1',
        '```',
        `![共存图](${uimg})`,
        '第一行',
        '第二行',
        // 🔴 这里必须是**单换行**:表格的两行被空行隔开就不是表格了(第一跑我写成 '\n\n',
        //    结果"等不到表格"—— 那是**我写错了**,不是产品缺陷。已按实测改。)
      ].join('\n');
      return { events: eventsFor(text, 'd-g8'), ctx: { uimg } };
    },
    probe: [async (page, out) => {
      await page.waitForSelector('#timeline .turn-assistant .event-content table', { timeout: WAIT_MS, state: 'attached' })
        .catch(() => { throw new Error('等不到表格 ⇒ 正文没渲染,先修装置'); });
      await sleep(SETTLE_MS);
      out.scans.push({ at: '正常 markdown', s: await scan(page) });
    }],
    check: (out) => {
      const s = last(out);
      assertNoBreach(s, '正常 markdown');
      const need = { strong: 1, em: 1, a: 1, code: 1, pre: 1, table: 1, heading: 1, quote: 1, br: 1, checkbox: 1 };
      const miss = Object.keys(need).filter((k) => !(s.legit[k] >= need[k]));
      assert.deepStrictEqual(miss, [],
        `🔴 过度屏蔽:正常 markdown 缺 ${miss.join(', ')};实测 ${JSON.stringify(s.legit)}`
        + '\n     🔵 这条腿是"探针没瞎"的证据:一个把正文渲染成空的改动会让其它所有腿假绿');
      assert.strictEqual(s.mdImgs.length, 1, `🔴 共存那张图没出(实测 ${s.mdImgs.length})`);
      assert.strictEqual(s.mdImgs[0].src, out.ctx.uimg, '🔴 共存那张图的 src 不对');
      console.log(`\n     🔵 正常渲染全在:${JSON.stringify(s.legit)} · 就地图片 1 张与之共存`);
    },
  };
}

// ── G9 两条路一致:HEAD 不可用时按扩展名降级,8 种图片扩展名都得判成 image ────────────────
function caseG9(bytes) {
  const exts = NGINX_IMAGE_EXTS;
  return {
    name: 'G9', bytes, files: exts.map((e) => ({ name: 'g9-' + e, ext: e, headStatus: 405 })),
    title: `🔴 一个文件两条路不得两种结果:HEAD 不可用(405)⇒ 按扩展名降级,${exts.length} 种图片扩展名都得判成 image`,
    prepare: (base) => {
      const urls = exts.map((e) => base + relOf('g9-' + e, e));
      const text = [`${SENTINEL} 降级路径`, ...urls].join('\n\n');
      return { events: eventsFor(text, 'd-g9'), ctx: { urls, exts } };
    },
    probe: [async (page, out) => {
      await waitSentinel(page);
      await sleep(SETTLE_MS);
      out.scans.push({ at: '降级路径(裸 URL,附件卡)', s: await scan(page) });
    }],
    check: (out) => {
      const s = last(out);
      const { urls, exts } = out.ctx;
      assert.strictEqual(s.mdImgs.length, 0, '🔴 裸 URL 不该就地出图(那是附件卡的路)');
      const bad = [];
      urls.forEach((u, i) => {
        const c = s.attachments.filter((a) => a.url === u);
        if (c.length !== 1) { bad.push(`${exts[i]}: 卡数=${c.length}`); return; }
        if (c[0].dispatch !== 'ext') bad.push(`${exts[i]}: dispatch=${c[0].dispatch}(应 ext ⇒ HEAD 没被当成可用)`);
        if (c[0].kind !== 'image') bad.push(`${exts[i]}: data-kind=${c[0].kind}(应 image)`);
      });
      assert.deepStrictEqual(bad, [],
        `🔴 降级路径上这几种扩展名没被判成图片:${bad.join(' | ')}`
        + '\n     成因通常是 FILE_EXT_KIND 的图片行没跟 nginx 的 types{image/*} 对齐(G0 会先红)');
      console.log(`\n     🔵 ${exts.length} 种扩展名在**降级路径**上全部 data-kind=image · dispatch=ext(与 HEAD 路径同结论)`);
    },
  };
}

// ══════════════════════════════════════════════════════════════════════════════════
// 阳性对照:每条变异量哪个数
// ══════════════════════════════════════════════════════════════════════════════════
const METRIC = {
  'M-G1': { caseName: 'G1', get: (out) => { const s = last(out); return { mdImgs: s.mdImgs.length, bang: s.text.includes('!图') || s.text.includes('![') }; } },
  'M-G2': { caseName: 'G2', get: (out) => { const s = last(out); return { svgImgs: s.mdImgs.filter((i) => /\.svg/.test(i.src)).length, mdSvg: s.mdSvg }; } },
  'M-G3': { caseName: 'G3', get: (out) => { const s = last(out); return { onAttrs: s.onAttrs.length, live: s.live.length, canary: s.canary }; } },
  'M-G4': { caseName: 'G5', get: (out) => { const s = last(out); return { cards: s.attachments.filter((a) => a.url === out.ctx.u1).length, imgs: s.mdImgs.filter((i) => i.src === out.ctx.u1).length }; } },
  'M-G5': { caseName: 'G4', get: (out) => { const s = last(out); return { extImgs: s.mdImgs.filter((i) => i.src.includes('ext.example')).length, external: out.external.length }; } },
};

// ══════════════════════════════════════════════════════════════════════════════════
// 主流程
// ══════════════════════════════════════════════════════════════════════════════════
(async () => {
  console.log('renderer-markdown-image(ts-No.79 · 板件锚 96d82d6e · 图片按类型自动显示)');
  console.log(`  renderer  ${RENDERER}`);
  console.log(`  sha256    ${RENDERER_SHA.slice(0, 16)}…${OVERRIDE ? '  🔴 RENDERER_OVERRIDE 生效:这是变异/对照跑,不是产品那份字节' : ''}`);
  console.log(`  nginx     ${NGINX_CONF}`);
  console.log(`  白名单    image/* ${NGINX_IMAGE_EXTS.length} 种:${NGINX_IMAGE_EXTS.join(' ')}(🔴 单一来源 = nginx conf,夹具的 Content-Type 也从这里解析)`);
  console.log(`  注入形状  复用 ts-No.76 共享清单 ${IMAGE_RELEVANT.length} 条 ＋ 图片语法专属 9 条 · canary=window.__dwPwned`);
  console.log('  🔴 结论口径:本件只证明「这些形状/这些扩展名如此」,**不得**读成「无 XSS」或「所有图片格式都能显示」');

  let browser;
  try {
    const launchOptions = { headless: true, args: ['--no-sandbox'] };
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
      console.log(`\n  🔴 ${e.message.split('\n').slice(0, 14).join('\n  ')}`);
    }
  };

  const BROWSERS_CASES = [
    () => caseG0b(RENDERER_BYTES),
    () => caseG1(RENDERER_BYTES),
    () => caseG2(RENDERER_BYTES),
    () => caseG3(RENDERER_BYTES),
    () => caseG4(RENDERER_BYTES),
    () => caseG5(RENDERER_BYTES),
    () => caseG6(RENDERER_BYTES),
    () => caseG8(RENDERER_BYTES),
    () => caseG9(RENDERER_BYTES),
  ];

  try {
    await t(`${G0.name} ${G0.title}`, async () => { G0.run(); console.log('     ✅ 静态对账过'); });

    for (const mk of BROWSERS_CASES) {
      const c = mk();
      if (CASE_FILTER && c.name !== CASE_FILTER) continue;
      await t(`${c.name} ${c.title}`, async () => {
        const out = await runCase(browser, c);
        if (out.error) throw new Error(`装置异常:${out.error}`);
        assert.ok(out.scans.length > 0, '一次扫描都没做 ⇒ 装置没跑到断言');
        c.check(out);
        BASE_RUNS[c.name] = out;
      });
    }

    // ── 五条阳性对照:变异必须让**对应那格的观测量**动起来 ──────────────────────────
    const srcText = RENDERER_BYTES.toString('utf8');
    for (const name of (CASE_FILTER ? [] : Object.keys(MUTATIONS))) {
      const m = MUTATIONS[name];
      const spec = METRIC[name];
      await t(`PC ${name} 阳性对照:${m.label} ⇒ **必须观测到观测量移动**(不红说明探针没有分辨力)`, async () => {
        const baseRun = BASE_RUNS[spec.caseName];
        assert.ok(baseRun, `🔴 产品字节的 ${spec.caseName} 那一格没跑成 ⇒ 无从比较"变异前"。先看那条红,别读这条。`);
        const before = spec.get(baseRun);
        const v = buildMutation(name, srcText);
        console.log(`\n     (变异副本 sha256=${v.sha256.slice(0, 16)}… 由产品字节 ${RENDERER_SHA.slice(0, 16)}… 在本进程内合成)`);
        const c = { 'G1': caseG1, 'G2': caseG2, 'G3': caseG3, 'G4': caseG4, 'G5': caseG5 }[spec.caseName](v.bytes);
        const out = await runCase(browser, c);
        if (out.error) throw new Error(`装置异常:${out.error}(变异把页面弄坏了也要说清,别当成"对照成立")`);
        const after = spec.get(out);
        assert.ok(m.breached(after),
          `🔴 阳性对照失效:施加「${m.label}」之后**观测量没动**(期望:${m.want})。`
          + `\n     变异前 ${JSON.stringify(before)} → 变异后 ${JSON.stringify(after)}`
          + '\n     ⇒ 说明**这一格的观测量对它要钉的变异不敏感**(组长立的"盲量"纪律),前面那条绿不可信。先修探针。'
          + (m.diag ? `\n     诊断:${m.diag(out)}` : '')
          + '\n     🔵 另一种可能是**变异等价**(它没改变行为)—— 两者必须分清:等价的话这条对照该撤,盲量的话该修探针。');
        console.log(`     ✅ 变异生效,观测量有牙:${m.moved(before, after)}`);
        console.log(`        变异前 ${JSON.stringify(before)} → 变异后 ${JSON.stringify(after)}`);
        // 🔵 分层守卫:M-G1/M-G5 改的是同一行,但**只应**各自那一维动
        if (name === 'M-G1') {
          assert.strictEqual(after.mdImgs, 0, '分层:M-G1 下应一张图都不出');
        }
        if (name === 'M-G5') {
          assert.ok(after.extImgs > 0 || after.external > 0, '分层:M-G5 必须让外站那条动');
        }
      });
    }
  } finally {
    await browser.close();
  }

  console.log(`通过 ${pass} / 失败 ${fail}   (renderer sha256=${RENDERER_SHA.slice(0, 16)}…)`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】(边界随绿一起打印,不用谁去翻交付件):');
  console.log(`     ① 只证明「nginx 那 ${NGINX_IMAGE_EXTS.length} 种扩展名在本站 /f/ 直链上会就地出图」—— 🔴 **不得**读成「所有图片都能预览」。`);
  console.log('     ② 🔴 **外站图片 URL 一律不出图**(G4 钉的就是这条):业主要外站图也预览的话,得先解除产品件里');
  console.log('        「不对任意外站 URL 做任何请求」那条不变量 —— 那是产品可见决定,归业主,本格没替他改。');
  console.log('     ③ 🔴 **格式解码能力没验**:8 种扩展名喂的都是同一份合法 PNG 字节(本格验的是"按扩展名分派",不是"能不能解那个格式")。');
  console.log('        🔵 唯一例外:G2/M-G2 的 .svg 喂的是**真 SVG 字节**(本件自己写的 2x2 无脚本 svg)——');
  console.log('        因为声明成 image/svg+xml 却喂 PNG 字节会解码失败 ⇒ 就地那张被 error 分支撤掉 ⇒ **观测量看不见变异**(第一跑实测撞到)。');
  console.log('        Chrome 能不能真解 avif/heic/tiff **不在本格**;若解不了,产品行为是"就地那张撤掉、附件卡给下载出路"(G6 钉了)。');
  console.log('     ④ 注入腿只覆盖清单里那些形状(复用 ts-No.76 ＋ 9 条图片专属)—— 🔴 **不得**读成「无 XSS」。');
  console.log('     ⑤ 单档 1280×900 · headless · 默认(浅色)主题;**暗色与窄档未跑**。');
  console.log('     ⑥ 只验 timeline 里 assistant 正文的 markdown 路径;**console 外壳 / Specs 独立页 / 草稿输入框**未跑。');
  console.log('     ⑦ 🔴 本席是 Linux + Playwright 自带 chromium,**业主那台 macOS + 系统 Chrome 上没跑过**(同型缺口已登记)。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
