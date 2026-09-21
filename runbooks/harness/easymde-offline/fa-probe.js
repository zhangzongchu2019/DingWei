// 🔵 ts-No.77 **附带实测**(不属组长判据,是我自己加的一格):
//    修法②(`autoDownloadFontAwesome:false`)的**代价**是什么?—— 工具栏图标会不会变成空白?
//    以及:自托管 FA 在**断网**下做不做得成?
//    🔴 不量这一格就给"关掉它"的建议 = 只报好处不报代价。
//    本文件**独立**于 run.js/page.html(那两个的字节未动,S0–S7 的数字仍由它们产出)。
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { execFileSync } = require('child_process');
const { chromium } = require('playwright');

const { requireVendor, PINS_MAIN, PINS_FA } = require('./vendor-pins');

const HERE = __dirname;
const OUT = path.join(HERE, 'out');
const PROXY = 'http://127.0.0.1:9';
const WAIT = 20000;

const INK_PY = `
import sys, json
from PIL import Image
from collections import Counter
im = Image.open(sys.argv[1]).convert('RGB')
px = list(im.getdata())
c = Counter(px)
bg, bgn = c.most_common(1)[0]
ink = sum(n for col, n in c.items() if col != bg)
print(json.dumps({"w": im.size[0], "h": im.size[1], "bg": list(bg), "bgpx": bgn, "ink": ink, "distinct": len(c)}))
`;

function ink(png) {
  const out = execFileSync('python3', ['-c', INK_PY, png], { encoding: 'utf8' });
  return JSON.parse(out.trim().split('\n').pop());
}

function startServer() {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/' || u === '/fa') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(path.join(HERE, 'fa-page.html')));
    }
    if (u.startsWith('/vendor/')) {
      const rel = u.slice('/vendor/'.length);
      const f = path.join(HERE, 'vendor', rel);
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

const classify = (url, port) => {
  if (url.startsWith(`http://127.0.0.1:${port}`) || url.startsWith(`http://localhost:${port}`)) return 'local';
  if (/^(data|blob|about|filesystem):/.test(url)) return 'inline';
  return 'external';
};

(async () => {
  requireVendor(Object.assign({}, PINS_MAIN, PINS_FA), 'F1–F3 Font Awesome A/B');
  fs.mkdirSync(OUT, { recursive: true });
  console.log('EasyMDE Font Awesome A/B(ts-No.77 附带实测 · 断网)');
  console.log(`  隔离手段  ${PROXY}(与 S0–S7 同一手段:死端口代理,浏览器层)`);
  console.log('  问题      修法② `autoDownloadFontAwesome:false` 的**代价**是什么?自托管 FA 在断网下成不成?');

  const { server, port } = await startServer();
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox', `--proxy-server=${PROXY}`] });
  } catch (e) { console.error('环境不可用:' + e.message.split('\n')[0]); process.exit(3); }

  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 8).join('\n  ')}`); }
  };

  const legs = {};
  async function leg(cfg) {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
    const page = await ctx.newPage();
    const rec = { cfg, cdp: [], pw: [], pageErrors: [] };
    const cdp = await ctx.newCDPSession(page);
    await cdp.send('Network.enable');
    cdp.on('Network.requestWillBeSent', (e) => rec.cdp.push({ url: e.request.url, cls: classify(e.request.url, port) }));
    ctx.on('request', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'req' }));
    ctx.on('requestfailed', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'failed' }));
    page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 120)));
    await page.goto(`http://127.0.0.1:${port}/fa?cfg=${cfg}`, { waitUntil: 'load', timeout: WAIT });
    await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });
    await page.waitForTimeout(2000);            // 给字体加载留时间
    await page.evaluate('document.fonts.ready');
    rec.fa = await page.evaluate('window.__dwFa()');
    const bar = await page.$('.editor-toolbar');
    assert.ok(bar, `装置:cfg=${cfg} 找不到 .editor-toolbar ⇒ 编辑器没起来,别把这条读成"图标正常"`);
    const png = path.join(OUT, `${cfg}-toolbar.png`);
    await bar.screenshot({ path: png });
    rec.png = png;
    rec.ink = ink(png);
    rec.externalCdp = rec.cdp.filter((x) => x.cls === 'external').map((x) => x.url);
    rec.externalPw = Array.from(new Set(rec.pw.filter((x) => x.cls === 'external').map((x) => x.url)));
    await ctx.close().catch(() => {});
    legs[cfg] = rec;
    return rec;
  }

  try {
    await t('F1 🔴 腿 A(不自托管 FA):图标**必须不存在**(无 FontAwesome face · ::before 无内容 · 元素 0×0)', async () => {
      const r = await leg('offline');
      console.log(`\n     faces=${JSON.stringify(r.fa.faces)} · boldIcon=${JSON.stringify(r.fa.boldIcon)}`);
      console.log(`     🔴 faCheck=${r.fa.faCheck} —— **不作判据**(理由见本格末尾那条自我更正)`);
      console.log(`     sheets=${JSON.stringify(r.fa.sheets)}`);
      console.log(`     工具栏像素 ${r.ink.w}x${r.ink.h}:背景色 ${JSON.stringify(r.ink.bg)} · 非背景像素 ${r.ink.ink} · 不同颜色 ${r.ink.distinct}`);
      assert.ok(!r.fa.faces.some((f) => /FontAwesome/.test(f)),
        `🔴 没自托管却枚举到 FontAwesome face:${JSON.stringify(r.fa.faces)} ⇒ A/B 的对照塌了`);
      assert.strictEqual(r.fa.boldIcon.beforeContent, 'none',
        `🔴 没有 FA 却仍有 ::before 内容(${JSON.stringify(r.fa.boldIcon.beforeContent)})⇒ 图标不靠 FA,我这条推论错了`);
      assert.ok(r.fa.boldIcon.rectW === 0 && r.fa.boldIcon.rectH === 0,
        `守卫:图标元素仍有尺寸 ${r.fa.boldIcon.rectW}x${r.fa.boldIcon.rectH} ⇒ "没有字形"这个说法要重新表述,别照抄`);
      assert.ok(!/FontAwesome/.test(r.fa.boldIcon.fontFamily),
        `守卫:computed font-family 仍是 FontAwesome(${r.fa.boldIcon.fontFamily})⇒ 与 faces=[] 矛盾,先查装置`);
      assert.ok(r.fa.toolbarButtons >= 8, `守卫:工具栏只有 ${r.fa.toolbarButtons} 颗钮 ⇒ 编辑器没正常装配`);
      assert.deepStrictEqual(r.externalCdp, [], `🔴 断网腿居然有外部请求:${JSON.stringify(r.externalCdp)}`);
      assert.deepStrictEqual(r.pageErrors, [], `页面报错:${JSON.stringify(r.pageErrors)}`);
      console.log('     🔵 自我更正(本件第二次"探针先怀疑自己"):我原先拿 document.fonts.check("14px FontAwesome") 当判据,'
        + '\n        实测它在**没有任何 @font-face 匹配**时**也返回 true**(faces=[] 而 check=true)⇒ **它证明不了"字体在"**。'
        + '\n        🔴 判据已换成三个互不依赖的量:`document.fonts` 枚举 · ::before content · 元素几何(0×0)。');
    });

    await t('F2 🔴 腿 B(**本地**自托管 FA):图标**必须在**,且**仍然零外部请求**', async () => {
      const r = await leg('localfa');
      console.log(`\n     faces=${JSON.stringify(r.fa.faces)} · boldIcon=${JSON.stringify(r.fa.boldIcon)}`);
      console.log(`     sheets=${JSON.stringify(r.fa.sheets)}`);
      console.log(`     工具栏像素 ${r.ink.w}x${r.ink.h}:背景色 ${JSON.stringify(r.ink.bg)} · 非背景像素 ${r.ink.ink} · 不同颜色 ${r.ink.distinct}`);
      assert.strictEqual(r.fa.faError, null, `装置:本地 FA 那份没挂上(${r.fa.faError})⇒ 这条腿不能用来证明"自托管可行"`);
      assert.ok(r.fa.faces.some((f) => /FontAwesome\/loaded/.test(f)),
        `🔴 本地挂了 FA 却没有 loaded 的 FontAwesome face(${JSON.stringify(r.fa.faces)})⇒ 自托管这条路**没验成**`
        + '(是我的装置或 FA 版本对不上,**不是产品缺陷**)');
      assert.ok(/FontAwesome/.test(r.fa.boldIcon.beforeFontFamily),
        `守卫:::before 的 font-family 不是 FontAwesome(${r.fa.boldIcon.beforeFontFamily})⇒ 图标不是本地这份 FA 画的`);
      assert.notStrictEqual(r.fa.boldIcon.beforeContent, 'none', '守卫:::before 仍无内容 ⇒ 图标没出来');
      assert.ok(r.fa.boldIcon.rectW > 0 && r.fa.boldIcon.rectH > 0,
        `守卫:图标元素仍是 0×0(${r.fa.boldIcon.rectW}x${r.fa.boldIcon.rectH})⇒ 字体 loaded 了却没画出来,别报"自托管可行"`);
      assert.deepStrictEqual(r.externalCdp, [], `🔴 自托管 FA 这腿仍有外部请求:${JSON.stringify(r.externalCdp)}`);
      assert.deepStrictEqual(r.externalPw, [], `🔴 同上(Playwright 面):${JSON.stringify(r.externalPw)}`);
      assert.ok(r.fa.sheets.some((s) => /\/vendor\/fa\.min\.css/.test(s)), `守卫:样式表里没有本地 FA ⇒ 这腿量的不是自托管:${JSON.stringify(r.fa.sheets)}`);
    });

    await t('F3 🔴 A/B 有分辨力:自托管后工具栏**真的多出字形**(单位高度墨迹必须显著增加)', async () => {
      const a = legs.offline, b = legs.localfa;
      assert.ok(a && b, '装置:两条腿没都跑成,无法 A/B');
      // 🔵 两腿工具栏高度不同(实测 52 vs 49 px)⇒ 用"每行墨迹"归一化后再比,不拿裸像素数糊弄
      const apx = a.ink.ink / a.ink.h, bpx = b.ink.ink / b.ink.h;
      console.log(`\n     不自托管 非背景像素 ${a.ink.ink} / 高 ${a.ink.h} = ${apx.toFixed(1)} 每行(不同颜色 ${a.ink.distinct})`);
      console.log(`     自托管   非背景像素 ${b.ink.ink} / 高 ${b.ink.h} = ${bpx.toFixed(1)} 每行(不同颜色 ${b.ink.distinct})`);
      assert.strictEqual(a.fa.toolbarButtons, b.fa.toolbarButtons,
        `守卫:两腿工具栏钮数不同(${a.fa.toolbarButtons} vs ${b.fa.toolbarButtons})⇒ A/B 比的不是同一个工具栏`);
      assert.ok(bpx > apx * 1.5,
        `🔴 自托管 FA 后每行墨迹没有显著增加(${apx.toFixed(1)} → ${bpx.toFixed(1)})⇒ **不能说"关掉自动下载只是图标变空、自托管就补回来"**;`
        + '\n     那意味着 FA 与这套图标其实无关,我这条推论没有证据 —— 如实报,不要替它找补。');
      console.log(`     🔵 结论:不自托管 FA ⇒ 工具栏图标**没有字形**(每行墨迹 ${apx.toFixed(1)}px,基本只剩边框/分隔线;`
        + `图标元素实测 0×0、::before content=none)`
        + `\n        从**本地**挂一份 FA 4.7.0 ⇒ 每行墨迹 ${bpx.toFixed(1)}px(${(bpx / Math.max(0.0001, apx)).toFixed(1)}×),图标元素 ${b.fa.boldIcon.rectW}x${b.fa.boldIcon.rectH},且**断网下零外部请求**`
        + '\n        ⇒ 修法② 的代价是**必须同时自托管 Font Awesome**(或改写 EasyMDE 的图标 CSS 用本仓图标),不是"关个开关就完事"。');
    });
  } finally {
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】:');
  console.log('     ① 只量了**工具栏**那一条截图的墨迹,没量编辑区/状态栏/预览面板的图标;');
  console.log('     ② 自托管用的是 **Font Awesome 4.7.0**(bootstrapcdn `latest` 当前发的那份),');
  console.log('        🔴 它是**外网取回来再本地服务**的字节 ⇒ 引入时该走 npm 包/内网镜像,并补哈希 pin,别抄这个来源;');
  console.log('     ③ 墨迹差是"有字形 vs 无字形"的判别,**不是**观感评价(好不好看归业主);');
  console.log('     ④ 隔离仍是**浏览器层**(死端口代理),与 S0–S7 同一手段、同一条边界。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
