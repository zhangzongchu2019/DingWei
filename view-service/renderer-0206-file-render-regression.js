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
const crypto = require('crypto');
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
// 🔴 开跑前先对账：**被服务的 renderer 必须就是仓内这一份**。
//
// 为什么要有这一道：2026-09-18 评审方在锚上跑得
// `ReferenceError: FILE_EXT_NEVER_INLINE is not defined`，而同一锚在我这边 PASS。
// 实测复现出的根因是 —— **测试脚本来自新提交，被服务的 renderer 停在旧提交**。
// 那时的失败信息指向「测试里那个标识符不存在」，**看起来像测试写错了**，
// 而真正的事实是「你跑的不是这一版产品」。一次误诊耗掉了双方各一轮。
//
// 有了这道对账，同样的错配会直接说出它自己是什么。
async function assertServedRendererMatchesRepo(page) {
  const served = await page.content();
  const repo = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
  const norm = (t) => crypto.createHash('sha256').update(t.replace(/\s+$/u, '')).digest('hex');
  // page.content() 会返回序列化后的 DOM，与源文件不逐字节相同，
  // 故用一个只可能出现在本版产品里的标记做对账，而不是比整份哈希。
  const markers = ['FILE_EXT_NEVER_INLINE', 'FILE_EXT_KIND', 'data-dispatch', 'fileAttachmentsHost'];
  const missing = markers.filter((m) => repo.includes(m) && !served.includes(m));
  if (missing.length) {
    throw new Error(
      `\n🔴 被服务的 renderer 与仓内这一份不是同一版 —— 先修这个，别查测试。\n`
      + `   仓内文件  ${path.join(__dirname, 'renderer-canary-sse.html')}  sha256=${norm(repo).slice(0, 16)}…\n`
      + `   服务地址  ${BASE}/renderer-canary-sse.html\n`
      + `   服务端缺少本版标记: ${missing.join(', ')}\n`
      + `   常见成因：mock 从另一个目录/另一次检出启动，或 renderer 未随测试脚本一起更新。`);
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
  // 🔵 PW_CHANNEL=chrome ⇒ 用系统真 Chrome（含 H.264/AAC 与 PDFium）；留空则仍用 playwright 自带 chromium
  const launchOpts = { headless: true };
  if (process.env.PW_CHANNEL) launchOpts.channel = process.env.PW_CHANNEL;
  const browser = await chromium.launch(launchOpts);
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
  await assertServedRendererMatchesRepo(page);
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
  await t('④b mp4：能解码则必须内联 <video>，不能解码则降级为下载链接（两种都不得留裸 URL）', async () => {
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
    // 🔴 先问浏览器【这一台】能不能解 H.264，再据此断言 —— 否则这条测试断言的是
    //    「环境缺陷」而不是「产品行为」：内联时同样会附下载链接，`hasLink>=1` 两种情况都成立，
    //    于是它从来没有真正验过「mp4 能显示」。
    const canPlayH264 = await page.evaluate(() =>
      document.createElement('video').canPlayType('video/mp4; codecs="avc1.42E01E"'));
    assert.ok(hasLink >= 1, 'mp4 无论能否内联，都必须另给下载链接');
    // 🔴 两支都必须【等】：产品无法在插入 <video> 之前知道这台浏览器能不能解码，
    //    只能先插进去、由 media error 事件触发回退。同步断言测的是「谁跑得快」，不是产品行为。
    //    实测（不可解码机）：t≈0ms video=1 readyState=0 → t≈200ms video=0 links=2，此后稳定。
    if (canPlayH264 === 'probably' || canPlayH264 === 'maybe') {
      // 能解码：等到真的拿到元数据，才算「内联成功」；只看元素在不算。
      await last.locator('video').first().waitFor({ state: 'attached', timeout: 5000 });
      const ok = await last.locator('video').first().evaluate((v) => new Promise((resolve) => {
        if (v.readyState >= 1) return resolve({ readyState: v.readyState, w: v.videoWidth, h: v.videoHeight });
        v.addEventListener('loadedmetadata', () => resolve({ readyState: v.readyState, w: v.videoWidth, h: v.videoHeight }), { once: true });
        v.addEventListener('error', () => resolve({ readyState: v.readyState, err: v.error && v.error.code }), { once: true });
        setTimeout(() => resolve({ readyState: v.readyState, timeout: true }), 5000);
      }));
      assert.ok(ok.readyState >= 1 && ok.w > 0,
        `本浏览器可解 H.264（canPlayType=${canPlayH264}），mp4 必须真的内联，实测 ${JSON.stringify(ok)}`);
    } else {
      // 不可解码：等回退完成再断言 —— 断言的是「最终不留一个放不了的 <video>」，
      // 不是「一插进来就没有」（后者产品做不到，见上）。
      await last.locator('video').first().waitFor({ state: 'detached', timeout: 5000 });
      assert.strictEqual(await last.locator('video').count(), 0,
        `本浏览器不可解 H.264（canPlayType=${JSON.stringify(canPlayH264)}），回退后不得残留放不了的 <video>`);
      // 🔴 恰好一条：error 分支只移除 <video>，不再 replaceWith 出第二条。
      //    组长从我的证据截图里看出并排两个「⬇ 下载视频」——
      //    业主那台真浏览器上，凡解不了码的视频都会撞上（他那个 HEVC 的 .mov 正是）。
      assert.strictEqual(await last.locator('a.attachment-dl').count(), 1,
        `解不了码时应恰好一条下载链接，实测 ${await last.locator('a.attachment-dl').count()} 条`);
    }
  });

  // ── ⑨ 业主验收第 1 条:上传全程可见(不是静默 30 秒)────────────────
  await t('⑨ 上传进行中界面可见:文件名 + 已传/总字节 + 进度在动,多文件逐个一行', async () => {
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    // 用 route 把上传卡住,才能观察「进行中」那一刻;不卡住就只能看到终态。
    // 🔴 挂起必须放在 try/finally 里:若本用例中途断言失败,release 不执行 ⇒
    //    后面每一个用到上传的用例都会被这把锁拖死(实测变异 V2 时 1 条真红 + 5 条级联)。
    //    「一个用例的失败污染后续用例」会让变异结果读不出来 —— 红得太多和没红一样该查。
    let release = () => {};
    let routed = false;
    try {
    const held = new Promise((r) => { release = r; });
    await page.route('**/files/upload', async (route) => { await held; await route.continue(); });
    routed = true;
    const picking = page.setInputFiles('#filePicker', [fx('sample.webm'), fx('sample.pdf')]);
    // 进行中:队列必须可见,且每个文件各一行
    await page.waitForSelector('#uploadQueue .upload-row', { timeout: 20000 });
    // 🔴 不能断言「两行同时在」：上传是**顺序**的，且完成行在草稿块出现后即移除（组长裁定）
    //    ⇒ 任一瞬间通常只有一行。R1 要的是「每个文件都有过自己的一行」，那是**时间上的**性质。
    //    所以这里改为全程采集行名，最后断言两个文件名都出现过。
    await page.evaluate(() => {
      window.__nameSeen = new Set();
      const grab = () => document.querySelectorAll('#uploadQueue .upload-row .u-name')
        .forEach((e) => window.__nameSeen.add(e.textContent || ''));
      grab();
      window.__nameTimer = setInterval(grab, 30);
    });
    // 🔴 从此刻起把每一行 meta 的**全部取值**记下来。
    //    只看某一瞬间是不够的:去掉字节进度后,行会一直停在「等…」的不定态,
    //    那正是业主抱怨的形态 ——「条在动,但看不出有没有进展」。
    await page.evaluate(() => {
      window.__metaSeen = new Set();
      const grab = () => document.querySelectorAll('#uploadQueue .upload-row .u-meta')
        .forEach((e) => window.__metaSeen.add(e.textContent || ''));
      grab();
      window.__metaTimer = setInterval(grab, 30);
    });
    const rows = await page.locator('#uploadQueue .upload-row').evaluateAll((els) => els.map((e) => ({
      name: e.querySelector('.u-name')?.textContent || '',
      meta: e.querySelector('.u-meta')?.textContent || '',
      indeterminate: e.classList.contains('indeterminate'),
    })));
    assert.ok(rows.length >= 1, '进行中必须至少有一行可见');
    for (const r of rows) {
      assert.ok(r.meta.length > 0, `行状态不可读: ${JSON.stringify(r)}`);
      assert.ok(r.name.length > 0, `行必须带文件名: ${JSON.stringify(r)}`);
    }
    await page.screenshot({ path: path.join(SHOTS, '04-upload-in-progress.png') });
    release();
    await picking;
    await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 2, null, { timeout: 60000 });
    const metaSeen = await page.evaluate(() => { clearInterval(window.__metaTimer); return [...window.__metaSeen]; });
    const nameSeen = await page.evaluate(() => { clearInterval(window.__nameTimer); return [...window.__nameSeen]; });
    // 每个文件都必须有过自己的一行 —— 不是只显示一个总数（组长 R1 明示）
    for (const want of ['sample.webm', 'sample.pdf']) {
      assert.ok(nameSeen.some((n) => n.includes(want)), `${want} 没有过自己的进度行: ${JSON.stringify(nameSeen)}`);
    }
    // 🔴 整个上传过程中,必须至少出现过一次**带字节数的进度**(形如「37% · 1.2 MiB / 3.4 MiB」)。
    //    只有不定态文案不算:那与业主原话「没有任何提示,很容易认为是出问题了」是同一种体验。
    // 🔴 这里只验「可见性」,不验「字节在涨」—— 边界写明白:
    //    本机上传走的是同机 mock,54~62 KiB 一个 chunk 就发完,
    //    xhr.upload 的 progress 实际观测不到中间值(实测 metaSeen 只有初始值与终态)。
    //    ⇒ 「progress 回调是否真的接上」由**单元级** renderer-t147 的 ⑧ 盯着(fakeXHR 可控地发进度);
    //      变异「去掉 progress 监听」在那一层红。**两级分工,各自写明覆盖的是哪一半。**
    assert.ok(metaSeen.some((m) => /\/\s.+(B|KiB|MiB)/.test(m)),
      `未出现带「已传/总」字节的状态,见到: ${JSON.stringify(metaSeen)}`);
    // 组长裁定：完成态的行在草稿块出现后即移除 ⇒ 这里断言「都传完后队列清空」，
    // 而不是断言「看得见已附加」——后者会把「完成行留在屏幕上」也算通过，正是要清掉的行为。
    await page.waitForFunction(() => document.querySelectorAll('#uploadQueue .upload-row').length === 0,
      null, { timeout: 15000 });
    assert.strictEqual(await page.locator('#uploadQueue .upload-row').count(), 0, '完成后不得残留进度行');
    // 而「已附加」这件事必须由草稿块表达（两处冗余是被裁掉的那一处）
    assert.ok(await page.locator('#draftAttachments .draft-chip').count() >= 2,
      '完成行清掉之后，草稿块必须接住「已附加」这件事');
    } finally {
      release();
      if (routed) await page.unroute('**/files/upload').catch(() => {});
    }
  });

  // ── ⑩ 业主验收第 2 条:草稿里的链接要有缩略图,且正文仍是直链 ────────
  await t('⑩ 草稿附件块:图片出缩略图、非图片出图标;🔴 正文仍保留原样直链', async () => {
    // 🔴 自备输入,不吃上一条用例留下的草稿 —— 否则针对 ⑨ 的变异会连带把本条打红,
    //    而本条与进度行无关。用例间的状态耦合会让变异的爆炸半径读不出来。
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.setInputFiles('#filePicker', [fx('sample.png'), fx('sample.pdf')]);
    await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 2, null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    await page.waitForSelector('#draftAttachments .draft-chip', { timeout: 20000 });
    const chips = await page.locator('#draftAttachments .draft-chip').count();
    assert.ok(chips >= 2, `每条直链一个附件块,实测 ${chips}`);
    // 图片那条必须是真 <img> 且真的加载出来(有自然宽高),不是占位图标
    await page.evaluate(() => { document.getElementById('humanInput').value = ''; document.getElementById('humanInput').dispatchEvent(new Event('input', { bubbles: true })); });
    await page.setInputFiles('#filePicker', [fx('sample.png')]);
    await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
    await page.waitForSelector('#draftAttachments .draft-chip img', { timeout: 20000 });
    const shot = await page.locator('#draftAttachments .draft-chip img').first().evaluate((im) => new Promise((resolve) => {
      if (im.complete && im.naturalWidth) return resolve({ w: im.naturalWidth, h: im.naturalHeight });
      im.addEventListener('load', () => resolve({ w: im.naturalWidth, h: im.naturalHeight }), { once: true });
      im.addEventListener('error', () => resolve({ err: true }), { once: true });
      setTimeout(() => resolve({ timeout: true }), 5000);
    }));
    assert.ok(shot.w > 0, `缩略图必须真的加载出来,实测 ${JSON.stringify(shot)}`);
    // 🔴 正文仍必须是那条 https 直链 —— 接收侧靠扫 /f/ 分派,拿掉链接接收方就什么都渲染不出来
    const draft = await page.inputValue('#humanInput');
    assert.ok(/https:\/\/[^\s]+\/f\//.test(draft), `正文必须保留直链,实测 ${JSON.stringify(draft)}`);
    // 🔴 不变式:附件块数 == 正文里的 /f/ 直链数。
    //    附件块只是正文的投影;两者一旦不等,用户看到的就不是他将要发出去的东西。
    //    实测过失同步的途径:有代码直接赋值 humanInput.value 而不派 input 事件。
    const consistency = await page.evaluate(() => ({
      chips: document.querySelectorAll('#draftAttachments .draft-chip').length,
      links: new Set((document.getElementById('humanInput').value.match(/https?:\/\/[^\s<>"'`）】]+/gu) || [])
        .map((u) => u.replace(/[.,;:!?，。；：！？、)\]}」』>]+$/u, ''))
        .filter((u) => { try { const x = new URL(u, location.href); return x.origin === location.origin && x.pathname.startsWith('/f/'); } catch { return false; } })).size,
    }));
    assert.strictEqual(consistency.chips, consistency.links,
      `附件块与正文直链数不一致: ${JSON.stringify(consistency)} —— 视觉层与将要发出的正文脱节`);
    await page.screenshot({ path: path.join(SHOTS, '05-draft-thumbnail.png') });
    // 用户删掉文字 ⇒ 附件块随之消失(它只是正文的投影,不是独立状态)
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.waitForFunction(() => document.querySelectorAll('#draftAttachments .draft-chip').length === 0, null, { timeout: 10000 });
  });

  // ── ⑪ R4：下载存盘名必须是原名,不是 32 位 .bin(业主 2026-09-18 验收)────
  await t('⑪ R4-V1/V2:本页上传的 docx,下载属性带原名(含空格与括号);V3:服务端不带 filename 时回退不编名', async () => {
    // 自备输入:清空草稿,只上传本用例这一个文件,免得 .last() 取到别人留下的附件
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    // 业主那个文件名的形状:带空格、带括号
    const trickyName = 'marketing-price-center-v2 (1).docx';
    const src = fx('sample.docx');
    const tmp = path.join(FIXTURES, trickyName);
    fs.copyFileSync(src, tmp);
    try {
      await page.setInputFiles('#filePicker', [tmp]);
      await page.waitForFunction(() => /\/f\//.test(document.getElementById('humanInput').value), null, { timeout: 60000 });
      const draft = await page.inputValue('#humanInput');
      await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), draft);
      await page.waitForSelector('.attachments .attachment a.attachment-dl', { timeout: 20000 });
      const dl = await page.locator('.attachments .attachment a.attachment-dl').last().evaluate((a) => ({
        download: a.getAttribute('download'), href: a.getAttribute('href'),
      }));
      // 🔴 V1/V2:存盘名必须是原名,且空格与括号原样保留
      assert.strictEqual(dl.download, trickyName,
        `下载存盘名必须是原名,实测 download=${JSON.stringify(dl.download)} href=${dl.href}`);
      assert.ok(/\.bin$/.test(dl.href), '本例服务端落盘应为 .bin —— 正是「显示对、存下来错」的那一格');
    } finally {
      fs.rmSync(tmp, { force: true });
    }
  });

  await t('⑪b R4-V3:接收侧服务端不带 Content-Disposition filename ⇒ 不报错、不编名(回退空 download)', async () => {
    // 刷新清掉本页上传记录,使其只能走 HEAD —— 即接收方的处境
    const carried = carriedUrls;
    const binUrl = (carried.match(/https:\/\/[^\s(]+\.bin/u) || [])[0];
    assert.ok(binUrl, '需要一个 .bin 直链');
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate(() => uploadedFileHints.clear());
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), binUrl);
    await page.waitForSelector('.attachments .attachment a.attachment-dl', { timeout: 20000 });
    const dl = await page.locator('.attachments .attachment a.attachment-dl').last().getAttribute('download');
    assert.strictEqual(dl, '', `服务端不带 filename 时必须回退空 download(不编名),实测 ${JSON.stringify(dl)}`);
  });

  // ── ⑫ R1 的另一半:失败也必须看得见,且带可读原因 ─────────────────
  //     业主原话是「没有任何提示,很容易认为是出问题了」—— 真出问题时更不能没提示。
  await t('⑫ 上传失败:该行标为失败并带服务端可读原因,且不清掉用户已输入的文字(R3)', async () => {
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = '用户写到一半的话'; el.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.route('**/files/upload', (route) => route.fulfill({
      status: 503, contentType: 'application/json', body: JSON.stringify({ error: '存储暂时不可用' }),
    }));
    try {
      await page.setInputFiles('#filePicker', [fx('sample.pdf')]);
      await page.waitForSelector('#uploadQueue .upload-row.failed', { timeout: 30000 });
      const meta = await page.locator('#uploadQueue .upload-row.failed .u-meta').last().textContent();
      assert.ok(/失败/.test(meta), `失败行要标明失败: ${JSON.stringify(meta)}`);
      assert.ok(/存储暂时不可用/.test(meta), `失败行要带服务端可读原因: ${JSON.stringify(meta)}`);
      // 🔴 R3：失败不得清掉用户已输入的文字
      assert.strictEqual(await page.inputValue('#humanInput'), '用户写到一半的话', '上传失败不得动用户已输入的文字');
      // 🔴 完成态的行会被清掉，**失败的行必须留着** —— 人要看得见哪个失败了（组长裁定）。
      await page.waitForTimeout(3000);
      assert.ok(await page.locator('#uploadQueue .upload-row.failed').count() >= 1,
        '失败行不得被自动清掉：完成态清、失败态留，这是两条不同的规则');
    } finally {
      await page.unroute('**/files/upload').catch(() => {});
      await page.evaluate(() => {
        document.querySelectorAll('#uploadQueue .upload-row').forEach((e) => e.remove());
        const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true }));
      });
    }
  });

  // ── ⑬ 契约 cb8ac605 第④条:外-A 七种扩展名在**两条路径**上都必须落下载兜底 ────
  //     组长判断「反向映射表不必新增条目,查不到即落 download」——
  //     这条判断关系到业主的 docx,我实测一次而不是接受它。
  await t('⑬ 外-A(docx/xlsx/pptx/doc/xls/ppt/zip)在 HEAD 与降级两条路径上都只给下载链接', async () => {
    for (const mode of ['head', 'ext']) {
      for (const ext of ['docx', 'xlsx', 'pptx', 'doc', 'xls', 'ppt', 'zip']) {
        const url = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/0123456789abcdef0123456789abc${ext.length}f0.${ext}`;
        await page.route(url, (route) => {
          if (route.request().method() === 'HEAD') {
            return mode === 'head'
              ? route.fulfill({ status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': 'attachment', 'Content-Length': '64' } })
              : route.fulfill({ status: 405, body: '' });
          }
          return route.fulfill({ status: 200, contentType: 'application/octet-stream', body: 'x' });
        });
        try {
          await page.reload({ waitUntil: 'domcontentloaded' });
          await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
          await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), url);
          await page.waitForSelector('.attachments .attachment', { timeout: 15000 });
          const got = await page.evaluate(() => {
            const a = document.querySelector('.attachments .attachment');
            return { d: a.dataset.dispatch, inline: a.querySelectorAll('img,video,iframe,pre').length, links: a.querySelectorAll('a.attachment-dl').length };
          });
          assert.strictEqual(got.inline, 0, `${mode}/${ext} 不得内联，实测 ${JSON.stringify(got)}`);
          assert.ok(got.links >= 1, `${mode}/${ext} 必须给下载链接，实测 ${JSON.stringify(got)}`);
        } finally {
          await page.unroute(url).catch(() => {});
        }
      }
    }
  });

  // ── ⑬b attachment 头本身的守卫:可内联的类型 ＋ attachment ⇒ 仍须下载 ──────
  //     🔴 这条是打 ⑬ 的变异时发现的缺口:外-A 的 Content-Type 本就是 octet-stream,
  //     所以 ⑬ 走的是 kindFromMediaType 那一支,**disposition 那道守卫从未被用到**
  //     (实测:把它整段去掉,⑬ 仍全绿)。能分辨它的只有「可内联类型 ＋ attachment」这一格。
  await t('⑬b 服务端说 image/png 但带 Content-Disposition: attachment ⇒ 必须下载,不内联', async () => {
    const url = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/0123456789abcdef0123456789abcdef.png`;
    await page.route(url, (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'image/png', 'Content-Disposition': 'attachment', 'Content-Length': '64' } })
      // 🔴 正文必须是**真 PNG**：用假字节的话，没有守卫时 <img> 也会因加载失败而自行降级，
      //    于是这条断言在「守卫在」和「守卫没了」两种情况下都绿 —— 变异被自己的 PoC 掩盖。
      //    （实测过：假字节时 X2 变异零转红。）
      : route.fulfill({ status: 200, contentType: 'image/png', body: fs.readFileSync(fx('sample.png')) })));
    try {
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
      await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), url);
      await page.waitForSelector('.attachments .attachment', { timeout: 15000 });
      const got = await page.evaluate(() => {
        const a = document.querySelector('.attachments .attachment');
        return { d: a.dataset.dispatch, img: a.querySelectorAll('img').length, links: a.querySelectorAll('a.attachment-dl').length };
      });
      assert.strictEqual(got.d, 'head', `应走 HEAD 支: ${JSON.stringify(got)}`);
      assert.strictEqual(got.img, 0, `服务端说要当附件下,就不得内联: ${JSON.stringify(got)}`);
      assert.ok(got.links >= 1, `必须给下载链接: ${JSON.stringify(got)}`);
    } finally {
      await page.unroute(url).catch(() => {});
    }
  });

  // ── ⑭ P0 A-E1:中文 / emoji / 空格括号 文件名必须【能传上去】────────────
  //     业主 2026-09-18 撞上:中文名 PDF 直接报 ISO-8859-1 异常,**请求根本没发出去**。
  //     🔴 此前 21/21 全绿却漏了,因为夹具名全是 ASCII ——
  //        用 ASCII 名测文件名编码,测多少遍都测不出来。
  await t('⑭ P0:中文/emoji/空格括号/含%% 的文件名都能成功上传,且草稿块显示原名', async () => {
    const names = ['季度报告（终版）.pdf', '📎emoji名.png', 'marketing v2 (1).docx', '50%off.pdf'];
    const made = [];
    try {
      for (const n of names) {
        const t2 = path.join(FIXTURES, n);
        fs.copyFileSync(fx('sample.pdf'), t2);
        made.push(t2);
      }
      await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
      const errs = [];
      page.on('pageerror', (e) => errs.push(String(e)));
      await page.setInputFiles('#filePicker', made);
      // 🔴 关键:四个都要真的上传成功。编码没做时,setRequestHeader 抛异常 ⇒ 这里永远等不到。
      await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 4,
        null, { timeout: 60000 });
      const draft = await page.inputValue('#humanInput');
      for (const n of names) {
        assert.ok(draft.includes(n), `输入框应回填原名 ${JSON.stringify(n)}，实测 ${JSON.stringify(draft.slice(0, 300))}`);
      }
      // 草稿块也必须显示原名(不是 %E5%AD%A3… 那种编码串)
      const chipNames = await page.locator('#draftAttachments .draft-chip .d-name').allTextContents();
      for (const n of names) {
        assert.ok(chipNames.some((c) => c === n), `草稿块应显示原名 ${JSON.stringify(n)}，实测 ${JSON.stringify(chipNames)}`);
      }
      assert.deepStrictEqual(errs, [], `上传过程中不得有未捕获异常: ${JSON.stringify(errs)}`);
    } finally {
      for (const f2 of made) fs.rmSync(f2, { force: true });
    }
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
    // 🔴 只数**这一张卡自己**的下载链接：原来数的是全页累计(>=3)，
    //    那个数会随前面新增用例而漂 —— 断言依赖无关用例的副作用，就不再是这条用例的断言。
    const truncated = page.locator('.turn-group .attachments .attachment').last();
    assert.ok(await truncated.locator('a.attachment-dl').count() >= 1, '截断后这张卡必须另给下载链接');
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
