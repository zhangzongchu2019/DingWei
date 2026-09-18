// 0.2.0.6 A 线功能级验收：**真的用界面传，真的看正文渲染**。
//
// 🔴 为什么是这个形状：0.2.0.5 的六条验收里没有一条是「打开界面试一次」，
//    于是「界面上根本没有上传入口」是业主自己用的时候发现的。
//    ⇒ 本件第一条断言就是「上传入口在界面上看得见」，其余每条都走真浏览器事件
//      （file picker / drop / paste），不是「代码里有这个元素」。
//
// 依赖：Playwright chromium；契约测试替身 renderer-0206-contract-mock.py（HTTPS）。
// 环境变量：MOCK_BASE=https://127.0.0.1:19443  FIXTURES=<样本目录>  SHOTS=<截图目录>
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { chromium } = require('playwright');

const BASE = process.env.MOCK_BASE || 'https://127.0.0.1:19443';
// 🔴 默认指向【仓内】样本目录，不要指向任何人的本机暂存目录。
//    此前默认是 /data/ai-dev-data/work/fixtures —— 那是我本机的路径，
//    在别人机器上必然 ENOENT，而失败信息是「缺样本」，看起来像环境问题、不像交付缺陷。
const FIXTURES = process.env.FIXTURES || path.join(__dirname, 'fixtures-0206');
const SHOTS = process.env.SHOTS || '/tmp/renderer-0206-shots';

const fx = (n) => path.join(FIXTURES, n);

// 两个大件不入仓（820 KB / 50 MiB），改为**用时现生成**，不依赖任何人的本地残留。
// 内容是确定性的：同一段代码在任何机器上生成同一份字节。
function ensureGeneratedFixtures() {
  const large = fx('sample-large.txt');
  if (!fs.existsSync(large)) {
    let text = '';
    for (let i = 0; i < 20000; i += 1) text += `${String(i).padStart(8, '0')} DingWei truncation fixture line\n`;
    fs.writeFileSync(large, text);
    console.log(`  · 生成 ${large}（${fs.statSync(large).size} 字节）`);
  }
  const over = fx('oversize-52428801.bin');
  if (!fs.existsSync(over)) {
    const fd = fs.openSync(over, 'w');
    const block = Buffer.alloc(1024 * 1024);
    for (let i = 0; i < 50; i += 1) fs.writeSync(fd, block);
    fs.writeSync(fd, Buffer.from('x'));   // 50 MiB + 1 字节：验的是「上限恰好被拒」
    fs.closeSync(fd);
    console.log(`  · 生成 ${over}（${fs.statSync(over).size} 字节）`);
  }
}
let pass = 0, fail = 0;
async function t(name, fn) {
  try { await fn(); console.log('  ✅', name); pass++; }
  catch (e) { console.log('  🔴', name, '->', e && e.message); fail++; }
}

(async () => {
  fs.mkdirSync(SHOTS, { recursive: true });
  ensureGeneratedFixtures();
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  const uploadCalls = [];
  let carriedUrls = '';   // ② 里传上去的 6 个 URL；刷新后输入框会清空，靠它跨用例传递
  page.on('request', (req) => { if (req.url().endsWith('/files/upload')) uploadCalls.push(req.url()); });
  page.on('console', (m) => { if (m.type() === 'error') console.log('    [console.error]', m.text()); });

  await page.route('**/status', (route) => route.fulfill({
    status: 200, contentType: 'application/json',
    body: JSON.stringify({ session: 'fixture-a-2642', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' }),
  }));
  await page.route('**/events', (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
  await page.route('**/taskboard**', (route) => route.fulfill({ status: 404, body: '' }));

  await page.goto(`${BASE}/renderer-canary-sse.html`, { waitUntil: 'domcontentloaded' });
  // stats 是脚本作用域的 const，不挂在 window 上
  await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });

  // ── ① 入口：界面上看得见 —— 这正是 0.2.0.5 漏掉的那条 ────────────
  await t('① 上传入口在界面上可见（文件选择按钮），不是只有隐藏的 paste 监听器', async () => {
    assert.strictEqual(await page.locator('#fileLabel').isVisible(), true, '#fileLabel 必须可见');
    assert.ok((await page.locator('#fileLabel').textContent()).includes('文件'), '按钮要有可读文案');
    assert.strictEqual(await page.locator('#filePicker').count(), 1, '要有 <input type=file>');
    assert.strictEqual(await page.locator('#filePicker').getAttribute('multiple') !== null, true, '必须可多选');
    await page.screenshot({ path: path.join(SHOTS, '01-upload-entry-visible.png') });
  });

  // ── ② 文件选择：一次多个、不限类型 ──────────────────────────────
  // 视频用 webm(VP9/Opus)：headless chromium 是开源构建，不带 h264 专有解码器，
  // 传 mp4 只能证明「元素建出来了」，证明不了「真的能播」。mp4 另在 ③b 单独验降级。
  const picked = ['sample.png', 'sample.webm', 'sample.pdf', 'sample-small.txt', 'sample.docx', 'sample-unknown.bin'];
  await t('② 文件选择一次传 6 个不同类型（图片/视频/pdf/文本/office/未知二进制）', async () => {
    await page.setInputFiles('#filePicker', picked.map(fx));
    await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 6,
      null, { timeout: 60000 });
    const value = await page.inputValue('#humanInput');
    for (const name of picked) assert.ok(value.includes(name), `输入框应回填 ${name}`);
    assert.strictEqual(uploadCalls.length, 6, `应发生 6 次上传，实际 ${uploadCalls.length}`);
  });

  // ── ③ 正文区按 media_type 分派（不经 turn 分组的输入卡路径）──────
  await t('③ 正文区渲染：图片 <img>、视频 <video>、pdf <iframe>、文本 <pre> 内联', async () => {
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), text);
    await page.waitForSelector('.attachments .attachment', { timeout: 20000 });
    await page.waitForFunction(() => document.querySelectorAll('.attachments .attachment').length >= 6, null, { timeout: 30000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 1, '图片应内联 <img>');
    assert.strictEqual(await page.locator('.attachment video').count(), 1, '视频应内联 <video>');
    // 「真的能播」而不只是「元素在」：等元数据到位，断言时长与画面尺寸都是真的。
    const media = await page.locator('.attachment video').first().evaluate((v) => new Promise((resolve) => {
      const done = () => resolve({ duration: v.duration, w: v.videoWidth, h: v.videoHeight, readyState: v.readyState });
      if (v.readyState >= 1) return done();
      v.addEventListener('loadedmetadata', done, { once: true });
      setTimeout(done, 8000);
    }));
    assert.ok(media.duration > 0.5, `视频时长应可读，实际 ${media.duration}`);
    assert.ok(media.w === 320 && media.h === 240, `视频画面尺寸应为 320x240，实际 ${media.w}x${media.h}`);
    assert.strictEqual(await page.locator('.attachment iframe').count(), 1, 'pdf 应内联 <iframe>');
    assert.ok(await page.locator('.attachment pre').count() >= 1, '纯文本应内联 <pre>');
    const preText = await page.locator('.attachment pre').first().textContent();
    assert.ok(preText.includes('DingWei 0.2.0.6'), '文本内容应真的取回来');
    // 🔴 小文本不得被误标「已截断」：Range 覆盖整个文件时服务端也回 206，
    //    拿 206 当截断依据会让 1.7 KiB 的文件显示成截断（截图里一眼看见的那个）。
    assert.strictEqual(await page.locator('.attachments .attachment-note').count(), 0,
      '1736 字节的小文本不该出现截断说明');
    // 证据截图：三种内联各截一张元素图（正文区自身是滚动容器，整页截图截不到上半部）。
    for (const [i, tag] of [[0, 'image'], [1, 'video'], [2, 'pdf']]) {
      const box = page.locator('.attachments .attachment').nth(i);
      await box.scrollIntoViewIfNeeded();
      await box.screenshot({ path: path.join(SHOTS, `02a-inline-${tag}.png`) });
    }
  });

  // ── ④ 🔴 兜底分支：office 与未知二进制 ⇒ 下载链接，不是报错、不是裸 URL ──
  await t('④ 兜底：office 与未知二进制渲染成下载链接（<a download>），不报错也不留裸 URL', async () => {
    const links = page.locator('.attachment a.attachment-dl');
    const n = await links.count();
    assert.ok(n >= 2, `兜底下载链接至少 2 个（docx + bin），实际 ${n}`);
    const hrefs = await links.evaluateAll((els) => els.map((e) => e.getAttribute('href')));
    for (const h of hrefs) assert.ok(/^https:\/\/.*\/f\//.test(h), `下载链接必须指向 /f/ 直链：${h}`);
    const hasDownloadAttr = await links.evaluateAll((els) => els.every((e) => e.hasAttribute('download')));
    assert.ok(hasDownloadAttr, '下载链接必须带 download 属性');
    // 「不是报错」：正文里不得出现错误卡
    assert.strictEqual(await page.locator('.turn-error').count(), 0, '兜底不得走错误卡');
    await page.screenshot({ path: path.join(SHOTS, '02-body-rendered-all-types.png'), fullPage: true });
  });

  // ── ④a 🔴 接收方路径：只有 URL、没有上传响应（附录 B2 真正要解决的那一半）──
  //     刷新页面 ⇒ uploadedFileHints 清空 ⇒ 只能靠 HEAD 读服务端声明的 Content-Type。
  //     这一条覆盖「不同 Agent 之间互传」：接收方从来没见过那个上传响应。
  await t('④a 接收方只拿到 URL（页面刷新后 hints 清空）⇒ 靠 HEAD 读服务端声明分派', async () => {
    carriedUrls = await page.inputValue('#humanInput');
    const carried = carriedUrls;
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    const hints = await page.evaluate(() => uploadedFileHints.size);
    assert.strictEqual(hints, 0, '刷新后本页上传记录必须是空的，否则这条测不到接收方路径');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), carried);
    await page.waitForFunction(() => document.querySelectorAll('.attachments .attachment').length >= 6, null, { timeout: 30000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 1, '.png 应按扩展名内联 <img>');
    assert.strictEqual(await page.locator('.attachment video').count(), 1, '.webm 应按扩展名内联 <video>');
    assert.strictEqual(await page.locator('.attachment iframe').count(), 1, '.pdf 应按扩展名内联 <iframe>');
    assert.ok(await page.locator('.attachment pre').count() >= 1, '.txt 应按扩展名内联 <pre>');
    const binLinks = await page.locator('.attachment a.attachment-dl[href$=".bin"]').count();
    assert.ok(binLinks >= 2, `.bin 必须走下载链接，实际 ${binLinks}`);
    assert.strictEqual(await page.locator('.attachments .attachment-note').count(), 0, '小文本不该报截断');
    // 附录 B2 (b′)：接收方一侧的类型必须来自**服务端声明的头**，不是 URL 扩展名的再解释。
    const heads = await page.locator('.attachment[data-dispatch="head"]').count();
    assert.ok(heads >= 6, `接收方的 6 张都应由 HEAD 定类型，实际 ${heads}`);
    await page.screenshot({ path: path.join(SHOTS, '02b-receiver-side-by-head.png'), fullPage: true });
  });

  // ── ④c 🔴 降级分支：nginx 若不放行 HEAD，必须退回扩展名分派而不是整个瞎掉 ──
  //     组长明示：`limit_except GET` 放行 HEAD 只从文档知道、未实测（prodops 在 H 收尾验）。
  //     实测通过前这条分支必须存在且被测到。
  await t('④c HEAD 被拒（405）时按 URL 扩展名降级分派，且标出「已降级」', async () => {
    const carried = carriedUrls;
    await page.route('**/f/**', (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 405, body: '' })
      : route.continue()));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), carried);
    await page.waitForFunction(() => document.querySelectorAll('.attachment[data-dispatch="ext"]').length >= 6, null, { timeout: 30000 });
    assert.strictEqual(await page.locator('.attachment[data-dispatch="head"]').count(), 0, 'HEAD 已被拒，不该还有 head 级分派');
    // 降级之后类型仍要对：.png→img、.webm→video、.pdf→iframe、.txt→pre、.bin→下载链接
    assert.strictEqual(await page.locator('.attachment[data-dispatch="ext"] img').count(), 1, '降级后 .png 仍应内联');
    assert.strictEqual(await page.locator('.attachment[data-dispatch="ext"] video').count(), 1, '降级后 .webm 仍应内联');
    assert.strictEqual(await page.locator('.attachment[data-dispatch="ext"] iframe').count(), 1, '降级后 .pdf 仍应内联');
    assert.ok(await page.locator('.attachment[data-dispatch="ext"] a.attachment-dl[href$=".bin"]').count() >= 2, '降级后 .bin 仍走下载链接');
    const note = await page.locator('.attachment[data-dispatch="ext"] .attachment-note').first().textContent();
    assert.ok(note.includes('已降级'), 'A-R04 要求界面上明写「已降级」，不许静默');
    await page.screenshot({ path: path.join(SHOTS, '02c-degraded-by-extension.png'), fullPage: true });
  });
  await page.unroute('**/f/**');   // 🔴 无论 ④c 过没过都要摘掉，否则后面每一条都被它污染

  // ── ④d 过期链接：7 天保留期到了，HEAD 自然 404 ⇒ 如实说，不画坏图 ──────
  await t('④d 文件已被清理（HEAD 404）时给出「已不可取」，不是坏掉的 <img> 也不是裸 URL', async () => {
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    const gone = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/deadbeefdeadbeefdeadbeefdeadbeef.png`;
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: `过期件：${u}`, ts: Date.now() }), gone);
    await page.waitForSelector('.attachment[data-dispatch="gone"]', { timeout: 20000 });
    const note = await page.locator('.attachment[data-dispatch="gone"] .attachment-note').textContent();
    assert.ok(note.includes('不可取'), `应如实说明已不可取，实际「${note}」`);
    assert.strictEqual(await page.locator('.attachment[data-dispatch="gone"] img').count(), 0, '不得为已清理的文件画 <img>');
  });

  // ── ④e 🔴 权威头优先：扩展名与 Content-Type 【故意不一致】────────────
  //     这是唯一能分辨 (b) 与 (b′) 的用例：(b) 看 URL 扩展名，(b′) 看服务端声明的头。
  //     契约裁的是 (b′) ⇒ 头赢，扩展名输。
  await t('④e 扩展名与 Content-Type 不一致时，以服务端声明的头为准（分辨 (b)/(b′)）', async () => {
    const urls = (carriedUrls.match(/https:\/\/[^\s(]+/gu) || []);
    const binUrl = urls.find((u) => u.endsWith('.bin'));
    const pngUrl = urls.find((u) => u.endsWith('.png'));
    assert.ok(binUrl && pngUrl, '需要一个 .bin 与一个 .png 的真实直链');
    // .bin 谎报成 image/png（且不带 attachment）；.png 谎报成 application/octet-stream
    // HEAD 与 GET 都按「服务端声明 image/png」来演，只有 URL 扩展名是 .bin ——
    // 只改 HEAD 会让 <img> 真去取到二进制而加载失败，那测的就不是分派依据了。
    const pngBytes = fs.readFileSync(fx('sample.png'));
    await page.route(binUrl, (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'image/png', 'Content-Length': String(pngBytes.length) } })
      : route.fulfill({ status: 200, contentType: 'image/png', body: pngBytes })));
    await page.route(pngUrl, (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Length': '2687' } })
      : route.continue()));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), `${binUrl}\n${pngUrl}`);
    await page.waitForFunction(() => document.querySelectorAll('.attachment[data-dispatch="head"]').length >= 2, null, { timeout: 30000 });
    const binBox = page.locator(`.attachment[data-file-url="${binUrl}"]`);
    const pngBox = page.locator(`.attachment[data-file-url="${pngUrl}"]`);
    // 扩展名 .bin，但头说 image/png ⇒ 必须内联成 <img>（若按扩展名走，这里会是下载链接）
    assert.strictEqual(await binBox.locator('img').count(), 1, '.bin + image/png 头 ⇒ 应按头内联 <img>');
    assert.strictEqual(await binBox.locator('a.attachment-dl').count(), 0, '.bin 不应因扩展名被判成下载');
    // 扩展名 .png，但头说 octet-stream ⇒ 必须走下载链接（若按扩展名走，这里会画 <img>）
    assert.strictEqual(await pngBox.locator('img').count(), 0, '.png + octet-stream 头 ⇒ 不得内联');
    assert.strictEqual(await pngBox.locator('a.attachment-dl').count(), 1, '.png + octet-stream 头 ⇒ 应给下载链接');
    await page.screenshot({ path: path.join(SHOTS, '02d-header-beats-extension.png'), fullPage: true });
  });
  await page.unroute(carriedUrls.match(/https:\/\/[^\s(]+\.bin/u)?.[0] || '**/never**').catch(() => {});
  await page.unroute(carriedUrls.match(/https:\/\/[^\s(]+\.png/u)?.[0] || '**/never**').catch(() => {});

  // ── ④f text/* 与 text/html：按服务端声明内联，且必须是【源码文本】不是被执行的 HTML ──
  await t('④f 服务端声明 text/* 即内联；text/html 以源码文本显示，不得被当 HTML 执行', async () => {
    const urls = (carriedUrls.match(/https:\/\/[^\s(]+/gu) || []);
    const target = urls.find((u) => u.endsWith('.bin'));
    assert.ok(target, '需要一个真实直链');
    const payload = '# 标题\n<script>window.__pwned = 1;</script>\n正文';
    await page.route(target, (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'text/html', 'Content-Length': String(Buffer.byteLength(payload)) } })
      : route.fulfill({ status: 200, contentType: 'text/html', body: payload })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), target);
    await page.waitForSelector('.attachment[data-dispatch="head"] pre', { timeout: 20000 });
    const shown = await page.locator('.attachment pre').first().textContent();
    assert.ok(shown.includes('<script>'), 'text/html 必须以源码文本显示，不能被解析成节点');
    assert.strictEqual(await page.evaluate(() => window.__pwned), undefined, '正文里的脚本绝不能被执行');
    assert.strictEqual(await page.locator('.attachment iframe').count(), 0, 'text/* 不得走 iframe');
    await page.unroute(target);
  });

  // ── ④g 附录 E：image/svg+xml 不内联 —— SVG 能内嵌脚本 ─────────────
  await t('④g 服务端声明 image/svg+xml 时不内联，走下载链接（附录 E）', async () => {
    const target = (carriedUrls.match(/https:\/\/[^\s(]+\.bin/u) || [])[0];
    assert.ok(target, '需要一个真实直链');
    await page.route(target, (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'image/svg+xml', 'Content-Length': '64' } })
      : route.continue()));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), target);
    await page.waitForSelector('.attachment[data-dispatch="head"]', { timeout: 20000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 0, 'svg 不得进 <img>');
    assert.strictEqual(await page.locator('.attachment iframe').count(), 0, 'svg 不得进 <iframe>');
    assert.strictEqual(await page.locator('.attachment a.attachment-dl').count(), 1, 'svg 应给下载链接');
    await page.unroute(target);
  });

  // ── ④h 降级路径上的 svg/html：不能靠「没写进表里」来安全 ─────────────
  //     ④g 验的是 HEAD 路径（服务端声明 image/svg+xml）。降级路径读的是 URL 扩展名，
  //     走的是另一张表 —— 那条路径此前没有任何断言盯着。
  await t('④h 降级路径:.svg 仍走下载链接,且 FILE_EXT_KIND 里不得出现永不内联的扩展名', async () => {
    // (a) 配置形状本身:表里不得混入 svg/html 之类(防「将来有人补全白名单」)。
    // 🔴 改为**现读产品源码**，不再经 page.evaluate 取页面作用域里的 const：
    //    页面作用域能否被 evaluate 读到，取决于脚本怎么加载、页面处于什么状态 ——
    //    那是断言之外的变量。评审方在同一锚上得到 ReferenceError 而我这边 PASS，
    //    差别就出在这个变量上。现读源码则与浏览器状态无关，且与本仓已有的
    //    TestUploadLimitMatchesNginx / TestDirectLinkExtensionAllowListsExcludeActiveContent 同手法。
    const rendererSrc = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
    const neverInline = (/const FILE_EXT_NEVER_INLINE = new Set\(\[([^\]]*)\]\)/.exec(rendererSrc) || [])[1];
    const extKind = (/const FILE_EXT_KIND = \{([\s\S]*?)\};/.exec(rendererSrc) || [])[1];
    assert.ok(neverInline && extKind, '源码里取不到 FILE_EXT_NEVER_INLINE / FILE_EXT_KIND —— 名称被改过?');
    const forbidden = [...neverInline.matchAll(/'([^']+)'/g)].map((m) => m[1]);
    const listed = [...extKind.matchAll(/([a-z0-9]+)\s*:/g)].map((m) => m[1]);
    const leaked = forbidden.filter((e) => listed.includes(e));
    assert.deepStrictEqual(leaked, [], `FILE_EXT_KIND 混入了永不内联的扩展名: ${leaked}`);
    // 🔴 只断言「两集合不相交」还不够 —— 从禁止集合里删掉一个 token(如 'html'),
    //    因为它本来也不在 FILE_EXT_KIND 里，不相交仍然成立，**零转红**（实测 M13）。
    //    ⇒ 必须对禁止集合本身下**绝对断言**：逐个 token 必须在。
    //    这正是我在 C 线 R2b 第三发里用过的那一层：换一个 token 才证得出守卫查的是整个集合。
    for (const must of ['svg', 'svgz', 'html', 'htm', 'xhtml', 'xml']) {
      assert.ok(forbidden.includes(must), `禁止集合缺少 ${must}（附录 E 要求 html/svg 永不内联）`);
    }
    // (b) 行为:HEAD 不可用(降级)时,.svg 链接必须仍是下载链接
    const svgUrl = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/aaaabbbbccccddddeeeeffff00001111.svg`;
    await page.route('**/f/**', (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 405, body: '' })
      : route.fulfill({ status: 200, contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg"/>' })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), svgUrl);
    await page.waitForSelector('.attachment[data-dispatch="ext"]', { timeout: 20000 });
    assert.strictEqual(await page.locator('.attachment img').count(), 0, '降级路径下 .svg 不得进 <img>');
    assert.strictEqual(await page.locator('.attachment a.attachment-dl').count(), 1, '降级路径下 .svg 应给下载链接');
    await page.unroute('**/f/**');
  });

  // ── ④b mp4：本浏览器解不了该编码时也要有出路，绝不留裸 URL ─────
  await t('④b mp4（本 chromium 无 h264 解码器）降级为下载链接，不报错、不留裸 URL', async () => {
    const before = await page.locator('.attachment').count();
    await page.evaluate(() => { document.getElementById('humanInput').value = ''; });
    await page.setInputFiles('#filePicker', [fx('sample.mp4')]);
    await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), text);
    await page.waitForFunction((n) => document.querySelectorAll('.attachment').length > n, before, { timeout: 30000 });
    const last = page.locator('.attachment').last();
    const hasVideo = await last.locator('video').count();
    const hasLink = await last.locator('a.attachment-dl').count();
    assert.ok(hasVideo + hasLink >= 1, 'mp4 必须要么内联要么给下载链接');
    assert.ok(hasLink >= 1, '解不了码时必须有下载链接兜底');
  });

  // ── ⑤ 拖放：一次拖入多个 ────────────────────────────────────────
  await t('⑤ 拖放一次拖入 2 个文件，两个都上传', async () => {
    const before = uploadCalls.length;
    const refsBefore = (await page.inputValue('#humanInput')).split('/f/').length - 1;
    const payload = ['sample.png', 'sample.pdf'].map((n) => ({
      name: n, type: n.endsWith('.png') ? 'image/png' : 'application/pdf',
      b64: fs.readFileSync(fx(n)).toString('base64'),
    }));
    await page.evaluate((files) => {
      const dt = new DataTransfer();
      for (const f of files) {
        const bin = Uint8Array.from(atob(f.b64), (c) => c.charCodeAt(0));
        dt.items.add(new File([bin], f.name, { type: f.type }));
      }
      dispatchEvent(new DragEvent('dragenter', { dataTransfer: dt, bubbles: true }));
      dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true }));
    }, payload);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
      refsBefore + 2, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 2, '拖放的 2 个文件都应上传');
  });

  // ── ⑥ 粘贴：扩到任意类型（不再只收 image/*）──────────────────────
  await t('⑥ 粘贴一个 pdf（非图片）也上传 —— 原实现只接 image/*', async () => {
    const before = uploadCalls.length;
    const refsBefore = (await page.inputValue('#humanInput')).split('/f/').length - 1;
    const b64 = fs.readFileSync(fx('sample.pdf')).toString('base64');
    await page.evaluate((data) => {
      const bin = Uint8Array.from(atob(data), (c) => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([bin], 'pasted.pdf', { type: 'application/pdf' }));
      document.getElementById('humanInput').dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
    }, b64);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
      refsBefore + 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '粘贴的 pdf 应上传');
    assert.ok((await page.inputValue('#humanInput')).includes('pasted.pdf'), '输入框应回填粘贴文件名');
  });

  // ── ⑦ 文本超限：截断 + 下载链接 ─────────────────────────────────
  await t('⑦ 大文本内联截断并给下载链接（不是整篇灌进正文）', async () => {
    await page.evaluate(() => { document.getElementById('humanInput').value = ''; });
    await page.setInputFiles('#filePicker', [fx('sample-large.txt')]);
    await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'assistant_text', text: body, turn_id: 'turn-large', ts: Date.now() }), text);
    await page.waitForSelector('.turn-group .attachments .attachment-note', { timeout: 30000 });
    const note = await page.locator('.attachments .attachment-note').last().textContent();
    assert.ok(note.includes('已截断'), `应给出截断说明，实际「${note}」`);
    const pre = await page.locator('.attachments pre').last().textContent();
    assert.ok(pre.length <= 262144, `内联文本不得超过上限，实际 ${pre.length}`);
    assert.ok(await page.locator('.attachments .attachment-dl').count() >= 3, '截断后必须另给下载链接');
    await page.screenshot({ path: path.join(SHOTS, '03-large-text-truncated.png'), fullPage: true });
  });

  // ── ⑧ 50 MiB 上限：发请求前就拒 ─────────────────────────────────
  await t('⑧ 超过 50 MiB（52428801 字节）在发请求前拒绝，不发上传请求', async () => {
    const before = uploadCalls.length;
    const kept = await page.inputValue('#humanInput');
    await page.setInputFiles('#filePicker', [fx('oversize-52428801.bin')]);
    await page.waitForTimeout(3000);
    assert.strictEqual(uploadCalls.length, before, '超限文件不得发起上传请求');
    assert.strictEqual(await page.inputValue('#humanInput'), kept, '上传失败不得改动输入框已有文字');
  });

  await browser.close();
  console.log(`\n通过 ${pass} / 失败 ${fail}   截图目录 ${SHOTS}`);
  process.exit(fail ? 1 : 0);
})().catch((e) => { console.error('harness error:', e); process.exit(2); });
