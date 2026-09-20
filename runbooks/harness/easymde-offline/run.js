// 🔴 ts-No.77 · EasyMDE 断网实跑(组长出单)—— **只验不引入**
//   装置本身已按裁定 (a) **进仓**:`runbooks/harness/easymde-offline/`。
//   🔴 但**被测第三方字节不进仓**:`vendor/` 由 `prepare-vendor.js` 从 npm(或 `DW_NPM_MIRROR`)取回,
//      按 `vendor-pins.js` 里的 sha256 逐字校验(不对就 rc=3),`.gitignore` 挡住它入库。
//   🔴 产品件一行未动;`package.json` 里依赖仍为 **0 个**;装置**不进 SUITES**(裁定 (a))。
//
// 判据(组长逐字):真断外网(不是改 host、不是拦域名白名单 —— 要能证明"任何外部域都出不去");
//                  载入 ＋ 编辑 ＋ 保存 全程:**外部请求数 = 0**。
// 阴格(组长逐字):**必须先证探针有牙** —— 故意留着默认拼写检查(会去 jsdelivr)⇒ 必须被探针抓到;
//                  抓不到 ⇒ "0 个外部请求"没有分辨力,**先修探针再报结论**。
//
// 🔴 隔离手段的诚实交代(这条要跟着结论一起报):
//   本席**没有** OS 级隔离能力 —— 实测 `CapEff=0`、`unshare -U` / `-n` / `-Un` 全部 EPERM、
//   `unshare(CLONE_NEWUSER|CLONE_NEWNET)` 亦 EPERM ⇒ 建不了 netns,也没有 iptables/nft 权限。
//   ⇒ 退而用**死端口代理**:`--proxy-server=http://127.0.0.1:9`(discard 端口,无人监听)。
//      它不是域名白名单:**任何**非 loopback 的请求都被送进一个必然 connect 失败的本地套接字;
//      Chromium 对 loopback 有隐式 bypass,所以本地夹具照常可用。
//   ⇒ 而且**不靠它自证**:S0 每一跑都先做隔离自证(本地必须通、三个外部域必须不通),
//      自证不过 ⇒ **rc=3 中止全跑**,绝不产出"看起来是 0"的绿。
//   🔵 计数用**两个互相独立的观测面**:CDP `Network.requestWillBeSent`(浏览器网络栈层)
//      与 Playwright 的 request/requestfailed 事件;两者不一致就报,不取其一。
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

const HERE = __dirname;
const PROXY = 'http://127.0.0.1:9';

// 🔴 前置校验:被测字节**必须在场且哈希与 pin 一致** —— 否则一条"外部请求 0"都不该产出。
//    pin 的**唯一源**是 `vendor-pins.js`(三个探针共用,防第二事实源);`vendor/` 不入仓 ⇒ 先跑 `node prepare-vendor.js`。
const { requireVendor, PINS_MAIN } = require('./vendor-pins');

// 🔵 装置自检钩子 —— **只能让格变红,不能让它变绿**:
//    DW_SELFTEST_FAIL=S0|S1|S6  ⇒ 对应阴格必定判红,用来**证明硬中止真的会响**(见 S1/S6 后的 exit(3))。
const FORCE_FAIL = process.env.DW_SELFTEST_FAIL || '';
const forced = (id) => { if (FORCE_FAIL === id) throw new Error('装置自检:DW_SELFTEST_FAIL=' + id + ' ⇒ 本阴格被**强制判红**(用来验证硬中止,不是产品缺陷)'); };
const TYPED = '断网实跑 #标题\n\n**粗体** 与 `code` 与 [链接](https://intranet.example/a)\n';
const WAIT = 20000;

const saved = [];
function startServer() {
  const server = http.createServer((req, res) => {
    const u = req.url.split('?')[0];
    if (u === '/ping') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('pong'); }
    if (u === '/save' && req.method === 'POST') {
      let b = ''; req.on('data', (c) => { b += c; });
      return req.on('end', () => { saved.push(b); res.writeHead(200, { 'Content-Type': 'text/plain' }); res.end(String(b.length)); });
    }
    if (u === '/' || u === '/index.html') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(fs.readFileSync(path.join(HERE, 'page.html')));
    }
    if (u.startsWith('/vendor/')) {
      const f = path.join(HERE, 'vendor', path.basename(u));
      if (!fs.existsSync(f)) { res.writeHead(404); return res.end('nf'); }
      res.writeHead(200, { 'Content-Type': f.endsWith('.css') ? 'text/css' : 'text/javascript', 'Cache-Control': 'no-store' });
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

async function runScenario(browser, sc, port) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 }, serviceWorkers: 'block' });
  const page = await ctx.newPage();
  const rec = { id: sc.id, cdp: [], pw: [], dialogs: [], pageErrors: [], popups: 0 };
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Network.enable');
  cdp.on('Network.requestWillBeSent', (e) => rec.cdp.push({ url: e.request.url, cls: classify(e.request.url, port), type: e.type }));
  // 🔵 context 级监听:popup 的请求也一定收得到(per-page 监听会**竞态漏掉** popup 的首个导航请求 ——
  //    第一跑 S4 就是这么假的红:popup 明明开了 2 个,却报"0 条外部请求")
  ctx.on('request', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'ctx', rt: r.resourceType() }));
  ctx.on('requestfailed', (r) => rec.pw.push({ url: r.url(), cls: classify(r.url(), port), tag: 'ctx', rt: r.resourceType(), failed: (r.failure() || {}).errorText }));
  page.on('dialog', async (d) => { rec.dialogs.push(d.type() + ':' + d.message().slice(0, 50)); try { await d.dismiss(); } catch {} });
  page.on('pageerror', (e) => rec.pageErrors.push(String(e.message).slice(0, 120)));
  ctx.on('page', () => { rec.popups += 1; });   // 只计一次(第一跑我在这儿和 guide 分支里各加了一次 ⇒ 重复计数)

  await page.goto(`http://127.0.0.1:${port}/?cfg=${sc.cfg}`, { waitUntil: 'load', timeout: WAIT });
  await page.waitForFunction('window.__dwReady === true', null, { timeout: WAIT });

  if (sc.kind === 'selftest') {
    rec.probe = await page.evaluate('window.__dwProbe()');
  } else {
    // 载入 → 编辑(真键盘)→ 保存
    if (sc.kind !== 'guide') {
      await page.click('.CodeMirror', { timeout: WAIT });
      await page.keyboard.type(TYPED, { delay: 4 });
      rec.typed = await page.evaluate('window.__dwEditor.value()');
      if (sc.clickToolbar) {
        const btns = await page.$$('.editor-toolbar button');
        rec.clicked = [];
        for (const b of btns) {
          const title = (await b.getAttribute('title')) || '';
          if (/Markdown Guide/i.test(title)) continue;   // guide 单独一格量,不混进"载入+编辑+保存"
          try { await b.click({ timeout: 2000 }); rec.clicked.push(title.split('(')[0].trim() || '(untitled)'); } catch {}
          await page.waitForTimeout(60);
        }
      }
      if (sc.save) {
        saved.length = 0;
        await page.click('#dwSave');
        await page.waitForFunction('document.getElementById("dwOut").textContent.startsWith("saved:")', null, { timeout: WAIT });
        rec.saved = saved.slice();
      }
    }
    if (sc.kind === 'guide') {
      const btns = await page.$$('.editor-toolbar button');
      let hit = null;
      for (const b of btns) { if (/Markdown Guide/i.test((await b.getAttribute('title')) || '')) { hit = b; break; } }
      assert.ok(hit, '装置:默认工具栏里找不到 "Markdown Guide" 按钮 ⇒ 这个版本形状变了,别当"没有外联"');
      // 🔵 主证据走 **window.open 桩**:它量的是"这颗按钮要把用户送去哪",不依赖 popup 的网络时序,
      //    所以在断网/有网两种情况下都同样成立(第一跑只靠网络面,popup 请求竞态漏掉 ⇒ 假红)。
      await page.evaluate(`window.__dwOpened = [];
        const __o = window.open;
        window.open = function (u) { window.__dwOpened.push(String(u)); return null; };`);
      await hit.click();
      await page.waitForTimeout(800);
      rec.opened = await page.evaluate('window.__dwOpened');
      await page.waitForTimeout(1200);
    }
    await page.waitForTimeout(2500);   // 给懒加载(词典等)留时间
    rec.storage = await page.evaluate('window.__dwStorage()');
  }
  await ctx.close().catch(() => {});
  return rec;
}

const ext = (rec, key) => rec[key].filter((r) => r.cls === 'external');
const uniq = (arr) => Array.from(new Set(arr.map((r) => r.url.split('?')[0])));

(async () => {
  console.log('EasyMDE 断网实跑(ts-No.77 · 组长出单)');
  console.log(`  隔离手段  ${PROXY}(死端口代理;🔴 非 OS 级 —— 本席无 netns/iptables 权限,实测 EPERM)`);
  const vend = ['easymde.min.js', 'easymde.min.css'].map((f) => {
    const h = require('crypto').createHash('sha256').update(fs.readFileSync(path.join(HERE, 'vendor', f))).digest('hex');
    return `${f} sha256=${h.slice(0, 16)}…`;
  });
  console.log(`  被测件    EasyMDE 2.21.0 · ${vend.join(' · ')}`);
  console.log('  🔴 边界:只验不引入 —— 装置在仓内(runbooks/harness/easymde-offline),被测字节在 vendor/(不进仓);仓内 package.json 依赖仍为 0 个,产品件未动');

  const vendOk = requireVendor(PINS_MAIN, 'S0–S7 主判据');
  console.log('  rc 语义   0=全过 · 2=有格红 · 🔴 3=**中止**(隔离自证/阴格不过 ⇒ 后面的绿一条都没产出)');
  if (FORCE_FAIL) console.log('  🔵 装置自检:DW_SELFTEST_FAIL=' + FORCE_FAIL + ' ⇒ 该阴格将被**强制判红**(用来证明硬中止会响)');

  const { server, port } = await startServer();
  let browser;
  try {
    browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  } catch (e) { console.error('环境不可用:Playwright 起不来 ' + e.message.split('\n')[0]); process.exit(3); }

  const results = [];
  let pass = 0, fail = 0; const failures = [];
  const t = async (title, fn) => {
    process.stdout.write(`· ${title} … `);
    try { await fn(); pass += 1; console.log('✅'); }
    catch (e) { fail += 1; failures.push(title.split(':')[0]); console.log(`\n  🔴 ${String(e.message).split('\n').slice(0, 10).join('\n  ')}`); }
  };

  const isoArgs = ['--no-sandbox', `--proxy-server=${PROXY}`];
  const launchIso = async () => chromium.launch({ headless: true, args: isoArgs });

  try {
    // ── S0 隔离自证:不过就中止全跑 ─────────────────────────────────────────────
    let isoBrowser;
    await t('S0 🔴 隔离自证:本地必须通、三个外部域必须【全都不通】(不通才算真断)', async () => {
      forced('S0');
      isoBrowser = await launchIso();
      const r = await runScenario(isoBrowser, { id: 'S0', cfg: 'default', kind: 'selftest' }, port);
      results.push(r);
      console.log(`\n     探针结果:${JSON.stringify(r.probe)}`);
      if (!/^OK 200$/.test(r.probe.local)) throw new Error(`隔离把本地也断了(local=${r.probe.local})⇒ 夹具不可用,这不是"断外网",是"全断"`);
      const leaked = ['jsdelivr', 'bootstrapcdn', 'example'].filter((k) => !/^BLOCKED/.test(r.probe[k]));
      assert.deepStrictEqual(leaked, [],
        `🔴 隔离**不成立**:这些外部域居然通了 ⇒ ${leaked.map((k) => `${k}=${r.probe[k]}`).join(' | ')}`
        + '\n     ⇒ 中止全跑:在隔离不成立的前提下报"外部请求 0"是假绿。');
      console.log('     🔵 隔离成立:外部一律 BLOCKED,错误名见上(它说明是**哪一层**挡的 —— 报结论时要一起报)');
    });
    if (fail) { console.log('\n🔴 隔离自证没过 ⇒ 按判据中止,不产出任何"0 外部请求"的结论'); await server.close(); process.exit(3); }

    // ── S1 阴格:默认配置(留着拼写检查)必须被探针抓到 ─────────────────────────
    await t('S1 🔴 阴格(探针有牙):默认配置 ⇒ **必须**观测到外联(jsdelivr 词典 + bootstrapcdn 字体)', async () => {
      forced('S1');
      const r = await runScenario(isoBrowser, { id: 'S1', cfg: 'default', kind: 'teeth' }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      console.log(`\n     CDP 观测外部 ${cdpE.length} 条:${JSON.stringify(uniq(cdpE))}`);
      console.log(`     PW  观测外部 ${pwE.length} 条:${JSON.stringify(uniq(pwE))}`);
      assert.ok(cdpE.length > 0 && pwE.length > 0,
        `🔴 探针没有分辨力:默认配置下两个观测面都是 0(CDP=${cdpE.length} PW=${pwE.length})`
        + '\n     ⇒ 按判据**先修探针再报结论**,后面那些"0"一个都不能信。');
      const urls = uniq(cdpE).join(' ');
      assert.ok(/jsdelivr/.test(urls), `🔴 没抓到已知的 jsdelivr 词典请求 ⇒ 探针漏报。实测:${urls}`);
      assert.ok(/bootstrapcdn|font-awesome/.test(urls), `🔴 没抓到 bootstrapcdn 的 Font Awesome ⇒ 探针漏报。实测:${urls}`);
      console.log('     🔵 探针有牙:默认配置下**两类**外联都被抓到(词典 + 字体),后面 S2/S3 的 0 才有意义');
    });

    // 🔴 **引入硬条件⑥(组长 ts-No.77 裁定)**:阴格不过 ⇒ **后面的绿一条都不该产出**。
    //    原结构里 `t()` 会 catch 每格异常、计入 fail 后**继续跑** ⇒ S1 红时 S2–S7 仍会各自打印 "0",
    //    而那些 0 **技术上正确、语义上是假绿**(探针没有分辨力时,"0 个外部请求"什么都证明不了)。
    //    🔵 治它不能靠"读日志的人记得先看 rc" —— **靠记得 = 没有**;所以做成与 S0 同形的硬中止。
    if (fail) {
      console.log('\n🔴 阴格 S1 没过 ⇒ **中止全跑**:S2–S7 的"外部请求 0"一条都不产出(rc=3)。');
      console.log('   理由:S1 红 = 探针**没有分辨力** ⇒ 此后所有 0 都是假绿;而假绿最贵,因为它不产生任何动作。');
      await isoBrowser.close().catch(() => {});
      await browser.close().catch(() => {});
      await new Promise((r) => server.close(r));
      process.exit(3);
    }

    // ── S2 主判据:载入 + 编辑 + 保存,外部请求 = 0 ─────────────────────────────
    await t('S2 🔴 主判据:offline 配置(spellChecker:false + autoDownloadFontAwesome:false)载入+编辑+保存 ⇒ 外部请求 **0**', async () => {
      const r = await runScenario(isoBrowser, { id: 'S2', cfg: 'offline', kind: 'flow', save: true }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      console.log(`\n     CDP 外部 ${cdpE.length} · PW 外部 ${pwE.length} · 本地 CDP ${r.cdp.filter((x) => x.cls === 'local').length} 条`);
      assert.deepStrictEqual(uniq(cdpE), [], `🔴 载入+编辑+保存期间仍有外部请求(CDP):${JSON.stringify(uniq(cdpE))}`);
      assert.deepStrictEqual(uniq(pwE), [], `🔴 同上(Playwright 面):${JSON.stringify(uniq(pwE))}`);
      assert.ok(r.cdp.filter((x) => x.cls === 'local').length >= 3, `守卫:本地请求只有 ${r.cdp.filter((x) => x.cls === 'local').length} 条 ⇒ 页面根本没正常加载,这个 0 是空的`);
      assert.strictEqual(r.typed, TYPED, `守卫:编辑没落到编辑器里(实测 ${JSON.stringify(String(r.typed).slice(0, 60))})`);
      assert.deepStrictEqual(r.saved, [TYPED], `🔴 保存环节没拿到我打进去的正文:${JSON.stringify(r.saved)}`);
      // 🔵 注意:本断言只在**不点工具栏**的例上成立(S2)。点过工具栏的例(S3)正文会被按钮改写过,
      //    那里改成"以打进去的正文开头 + 非空"—— 第一跑我把等值断言也用在 S3 上,红得对但不是产品问题。
      assert.deepStrictEqual(r.dialogs, [], `弹了对话框:${JSON.stringify(r.dialogs)}`);
      assert.deepStrictEqual(r.pageErrors, [], `页面报错:${JSON.stringify(r.pageErrors)}`);
      console.log('     🔵 正文从键盘进去、从"保存"按钮出来,一字不差;全程外部请求 0(两个观测面一致)');
    });

    // ── S3 把默认工具栏(除 guide)全点一遍,仍须 0 ─────────────────────────────
    await t('S3 🔴 工具栏全点(除 guide)+ 载入编辑保存 ⇒ 外部请求仍 **0**', async () => {
      const r = await runScenario(isoBrowser, { id: 'S3', cfg: 'offline', kind: 'flow', save: true, clickToolbar: true }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp');
      console.log(`\n     点过的按钮(${(r.clicked || []).length}):${JSON.stringify(r.clicked)}`);
      console.log(`     CDP 外部 ${cdpE.length} · PW 外部 ${ext(r, 'pw').length}`);
      assert.deepStrictEqual(uniq(cdpE), [], `🔴 点工具栏点出了外部请求:${JSON.stringify(uniq(cdpE))}`);
      assert.ok((r.clicked || []).length >= 8, `守卫:只点到 ${(r.clicked || []).length} 个按钮 ⇒ 这一格没验到工具栏`);
      // 工具栏按钮(Bold/Italic/Heading/Quote/列表/链接/图片…)**会往正文里插语法** ⇒
      // 🔴 这里不能断言"保存值 == 打进去的值"(第一跑我就这么写,红了,红的是我的期望不是产品)。
      //    改为:保存值必须**以打进去的正文开头**且**比它长**(证明按钮真的插了东西,而不是空转)。
      assert.ok(Array.isArray(r.saved) && r.saved.length === 1, `保存环节没落一次:${JSON.stringify(r.saved)}`);
      assert.ok(r.saved[0].startsWith(TYPED),
        `🔴 保存出来的正文不是以我打进去的开头 ⇒ 编辑内容被改写/丢失:${JSON.stringify(r.saved[0].slice(0, 90))}`);
      assert.ok(r.saved[0].length > TYPED.length,
        `守卫:点了 ${(r.clicked || []).length} 个按钮,正文却没变长(${r.saved[0].length} <= ${TYPED.length})⇒ 按钮可能是空转,这一格是空的`);
      console.log(`     🔵 工具栏确实改了正文(${TYPED.length} → ${r.saved[0].length} 字),而外部请求仍为 0`);
    });

    // ── S4 guide 按钮:默认工具栏里那颗**外网**按钮 ─────────────────────────────
    await t('S4 🔵 显影:默认工具栏的 "Markdown Guide" 按钮 = **外网站点**(点了就出网)', async () => {
      const r = await runScenario(isoBrowser, { id: 'S4', cfg: 'offline', kind: 'guide' }, port);
      results.push(r);
      const cdpE = ext(r, 'cdp'), pwE = ext(r, 'pw');
      const urls = uniq(cdpE).concat(uniq(pwE)).join(' ');
      console.log(`\n     window.open 目标(主证据)=${JSON.stringify(r.opened)}`);
      console.log(`     网络面:CDP 外部 ${cdpE.length} · PW(context 级)外部 ${pwE.length} · popup ${r.popups} · URL:${JSON.stringify(uniq(pwE))}`);
      assert.ok(Array.isArray(r.opened) && r.opened.some((u) => /markdownguide\.org/.test(u)),
        `🔴 形状变了:点 "Markdown Guide" 没有把用户送去 markdownguide.org(实测 window.open=${JSON.stringify(r.opened)})`
        + '\n     ⇒ 若将来它被改指内网,这一格会红 —— 那是好事,报组长把它转成"已内化"再撤');
      // 🔵 网络面在**断网**下可能因代理直接失败而不留 popup 请求记录;这一条只作旁证,不作判据
      if (pwE.length) console.log(`     🔵 旁证:网络面也确实尝试了 ${pwE.length} 条外部请求`);
      else console.log('     🔵 旁证:网络面 0 条(死端口代理下 popup 导航当场失败)—— 主证据是上面的 window.open 目标');
      console.log('     🔵 结论:这颗按钮**只在被点击时**出网(载入/编辑/保存全程不碰它 ⇒ 不影响 S2/S3 的 0);'
        + '但在内网侧点它 = 打不开一个外网站点 ⇒ 必须移除或改指内网文档');
    });

    // ── S5 不隔离的对照:证明那些外联是**真的会出去**,不是被我拦没了 ────────────
    await t('S5 🔵 对照(**不隔离**):默认配置 ⇒ 那些外联请求真的能到外网(证明 S1 抓到的是真请求)', async () => {
      const b2 = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
      try {
        const r = await runScenario(b2, { id: 'S5', cfg: 'default', kind: 'flow' }, port);
        results.push(r);
        const pwE = ext(r, 'pw');
        const ok = pwE.filter((x) => !x.failed);
        console.log(`\n     外部请求 ${pwE.length} 条,其中**成功** ${ok.length} 条:${JSON.stringify(uniq(ok))}`);
        assert.ok(ok.length > 0,
          `🔴 不隔离时外部请求也没成功过(${pwE.length} 条全失败)⇒ 我没法证明"隔离才是挡住它的那一层"。`
          + `实测:${JSON.stringify(pwE.slice(0, 4))}`);
        console.log('     🔵 对照成立:同样两个开关不关,请求**真能出去** ⇒ S2/S3 的 0 是隔离+配置共同作用的结果,不是探针瞎');
      } finally { await b2.close().catch(() => {}); }
    });

    // ── S6/S7 存储:先证探针有牙,再量默认配置 ─────────────────────────────────
    await t('S6 🔴 存储阴格(探针有牙):打开 autosave ⇒ **必须**看到 localStorage 被写', async () => {
      forced('S6');
      const r = await runScenario(isoBrowser, { id: 'S6', cfg: 'autosave', kind: 'flow', save: true }, port);
      results.push(r);
      const ls = (r.storage || {}).localStorage || {};
      console.log(`\n     localStorage=${JSON.stringify(ls)} sessionStorage=${JSON.stringify((r.storage || {}).sessionStorage)} idb=${JSON.stringify((r.storage || {}).indexedDB)}`);
      assert.ok(Object.keys(ls).length > 0,
        '🔴 存储探针没有分辨力:显式开了 autosave 却一个 key 都没看到 ⇒ 后面 S7 的"空"不可信,先修探针');
      console.log('     🔵 探针有牙:autosave 一开,localStorage 立刻有 key ⇒ S7 的"空"是真结论');
    });

    // 🔴 同硬条件⑥:S6 是 S7 的阴格 —— 存储探针没牙时,"四类存储全空"就是假绿。
    if (fail) {
      console.log('\n🔴 存储阴格 S6 没过 ⇒ **中止**:S7 的"存储全空"不产出(rc=3)。');
      await isoBrowser.close().catch(() => {});
      await browser.close().catch(() => {});
      await new Promise((r) => server.close(r));
      process.exit(3);
    }

    await t('S7 🔴 EasyMDE 有没有 Vditor 那种"悄悄写草稿"的默认?(offline 配置,autosave 不开)', async () => {
      const r = await runScenario(isoBrowser, { id: 'S7', cfg: 'offline', kind: 'flow', save: true }, port);
      results.push(r);
      const st = r.storage || {};
      console.log(`\n     localStorage=${JSON.stringify(st.localStorage)} sessionStorage=${JSON.stringify(st.sessionStorage)}`
        + ` indexedDB=${JSON.stringify(st.indexedDB)} serviceWorker=${JSON.stringify(st.serviceWorkerRegistrations)}`);
      assert.deepStrictEqual(Object.keys(st.localStorage || {}), [],
        `🔴 有同类默认:未开 autosave 却写了 localStorage:${JSON.stringify(st.localStorage)}`);
      assert.deepStrictEqual(Object.keys(st.sessionStorage || {}), [],
        `🔴 未开 autosave 却写了 sessionStorage:${JSON.stringify(st.sessionStorage)}`);
      assert.deepStrictEqual(st.indexedDB || [], [], `🔴 建了 IndexedDB 库:${JSON.stringify(st.indexedDB)}`);
      assert.deepStrictEqual(st.serviceWorkerRegistrations, 0, `🔴 注册了 service worker:${JSON.stringify(st.serviceWorkerRegistrations)}`);
      assert.deepStrictEqual(ext(r, 'cdp').map((x) => x.url), [], `🔴 同时还有外部请求:${JSON.stringify(uniq(ext(r, 'cdp')))}`);
      console.log('     🔵 实测结论:autosave 是**opt-in**(不开就一个字节都不落盘),没有 Vditor `cache.enable=true` 那种默认;'
        + '\n        ⇒ 对 I 项「正文唯一源」这条**不构成**同类风险(但 autosave 一旦被人打开,就会在浏览器里存第二份正文)');
    });

    await isoBrowser.close().catch(() => {});
  } finally {
    await browser.close().catch(() => {});
    await new Promise((r) => server.close(r));
  }

  console.log(`\n通过 ${pass} / 失败 ${fail}`);
  if (fail) { console.log(`  红的条:${failures.join(' / ')}`); process.exit(2); }
  console.log('  🔵 这条绿【看不见什么】:');
  console.log('     ① 隔离是**浏览器层**(死端口代理),不是 OS 级 netns —— 本席无权限(实测 EPERM)。');
  console.log('        它挡住的是"浏览器发出的任何非 loopback 请求",**不包括**任何非浏览器通道(本件也没有)。');
  console.log('     ② 只验了 EasyMDE 2.21.0 这一份字节;Vditor / Cherry **未验**(按裁定不并行)。');
  console.log('     ③ 只走"载入+编辑+保存+工具栏+guide"这条流程;图片上传、自定义 previewRender、插件生态未验。');
  console.log('     ④ 🔴 本装置**不判渲染保真度、不判暗色可读性、不判编辑手感** —— 它只量"出不出网 / 落不落盘"。');
  console.log('        (暗色对比度是另一格:`dark-contrast-probe.js`;渲染保真度按裁定挂起。)');
  console.log('     ⑤ 结论口径:「**这套配置下外部请求为 0**」,不是「EasyMDE 永不出网」——');
  console.log('        默认配置**会**出网(S1/S5 实测),必须显式关掉那两个开关 + 处理 guide 按钮。');
  process.exit(0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
