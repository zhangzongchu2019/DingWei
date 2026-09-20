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

  // ── ④i 判据 9 的自用：**只测第二道闸** ────────────────────────────
  //
  // 🔴 起因是我自己量出来的：删掉 kindFromExt 里那句 FILE_EXT_NEVER_INLINE 判断，
  //    整套 26 条**零转红**（实测 N2）。原因是两道机制互为冗余：
  //      第一道 kindFromMediaType('image/svg+xml') → 'download'
  //      第二道 kindFromExt 的 FILE_EXT_NEVER_INLINE
  //      而即使两道都删，`FILE_EXT_KIND['svg']` 本来就 undefined ⇒ 默认仍落 'download'
  //    ⇒ **默认兜底把第二道闸盖住了，它看起来可以删** —— 这正是判据 9 说的形状。
  //
  // 本条的做法：把兜底**挪开**，让第二道闸成为唯一决定者 ——
  // 运行时给 FILE_EXT_KIND 注入 svg:'image'，此时若无 deny-set，.svg 就会进 <img>。
  // 🔴 注入的是**测试自己制造的、产品里被 ④h(a) 明令禁止的配置**：
  //    ④h(a) 保证产品里不会出现它，本条保证**万一出现时第二道闸仍然拦得住**。
  //    两条合起来才是完整的：一条防配置漂移，一条防守卫被删。
  await t('④i 判据 9:只测第二道闸——FILE_EXT_KIND 里混入 svg 时,deny-set 必须仍拦住', async () => {
    const svgUrl = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/99998888777766665555444433332222.svg`;
    await page.route('**/f/**', (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 405, body: '' })
      : route.fulfill({ status: 200, contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg"/>' })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    try {
      // 注入前先自证：注入真的生效了，否则本条会变成一条永远绿的空用例。
      const injected = await page.evaluate(() => {
        FILE_EXT_KIND.svg = 'image';
        return FILE_EXT_KIND.svg;
      });
      assert.strictEqual(injected, 'image',
        '注入失败 ⇒ 本条测不到第二道闸（页面作用域取不到 FILE_EXT_KIND 时会静默变成空用例）');
      await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), svgUrl);
      await page.waitForSelector('.attachment[data-dispatch="ext"]', { timeout: 20000 });
      assert.strictEqual(await page.locator('.attachment img').count(), 0,
        '🔴 FILE_EXT_KIND 混入 svg 后仍不得进 <img> —— 这一格只有 deny-set 拦得住');
      assert.strictEqual(await page.locator('.attachment a.attachment-dl').count(), 1,
        '仍应给下载链接');
    } finally {
      await page.evaluate(() => { delete FILE_EXT_KIND.svg; }).catch(() => {});
      await page.unroute('**/f/**');
    }
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
    // 🔴 R5-2 之后口径要跟着改:正文里会有**被隐藏**的链接。
    //    这里数的是**输入框(textarea)**里的链接 —— textarea 的值不受 R5 隐藏影响,
    //    隐藏只发生在**已发出消息的正文区**。两者是不同的地方,断言仍然成立且含义不变。
    assert.strictEqual(consistency.chips, consistency.links,
      `草稿附件块与输入框里的直链数不一致: ${JSON.stringify(consistency)} —— 视觉层与将要发出的正文脱节`);
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
    // 🔴 先验夹具本身:「中文名」必须真的含 >0x7f 的码点。
    //    用 ASCII 名(或被环境转义过的名)测文件名编码,测多少遍都测不出来 ——
    //    那就是「素材落在被测维度的同一侧」。这里把它焊成断言,不靠肉眼看着像中文。
    const nonAscii = names.filter((n) => [...n].some((c) => c.codePointAt(0) > 0x7f));
    assert.ok(nonAscii.includes('季度报告（终版）.pdf'), '夹具「中文名」必须含 >0x7f 码点');
    assert.ok(nonAscii.includes('📎emoji名.png'), '夹具「emoji 名」必须含 >0x7f 码点');
    assert.strictEqual(nonAscii.length, 2,
      `另外两个夹具是**按设计**的纯 ASCII(一个测空格与括号、一个测 %),不得也变成非 ASCII —— 否则「ASCII 路径没坏」这一半就没人测了,实测 ${JSON.stringify(nonAscii)}`);
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

  // ── ⑮ R5:链接—内容交替排列(业主 09-18:「一个文件链接下一个文件展示」)──────
  await t('⑮ R5-V1/V2/V3/V4:名(链接)→内容 交替;正文中已渲染的 URL 隐藏但仍在 DOM;过期链接保持可见', async () => {
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.setInputFiles('#filePicker', [fx('sample.png'), fx('sample.pdf'), fx('sample.webm')]);
    await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 3, null, { timeout: 60000 });
    const text = await page.inputValue('#humanInput');
    // R5-V4 的阳性对照:一条 HEAD 404 的过期链接,必须**不被隐藏**
    const gone = `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/deadbeefdeadbeefdeadbeefdeadbeef.png`;
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), `${text} ${gone}`);
    await page.waitForFunction(() => {
      const card = [...document.querySelectorAll('.message-card')].pop();
      return card && card.querySelectorAll('.attachments .attachment').length >= 4;
    }, null, { timeout: 30000 });
    await page.waitForFunction(() => {
      const card = [...document.querySelectorAll('.message-card')].pop();
      return card && card.querySelectorAll('.dw-link-rendered').length >= 3;
    }, null, { timeout: 20000 });
    const got = await page.evaluate(() => {
      // 🔴 取**最后一张**卡:套件里前面的用例留下了若干张卡,
      //    querySelector 取到的是最早那张 —— 探针里只有一张卡时看不出这个差别。
      const card = [...document.querySelectorAll('.message-card')].pop();
      const boxes = [...card.querySelectorAll('.attachments .attachment')];
      return {
        // R5-V1：每块内部都是 名 → 内容（head 在前，内容在后）；块与块顺序即交替顺序
        alternating: boxes.map((a) => {
          const head = a.firstElementChild;
          const hasName = !!head?.querySelector('.attachment-name');
          const after = [...a.children].slice(1).length;
          return hasName && after > 0;
        }),
        // R5-V2：名是 <a> 且 href 等于该块的直链
        links: boxes.map((a) => {
          const el = a.querySelector('a.attachment-name');
          return el ? el.getAttribute('href') === a.dataset.fileUrl : false;
        }),
        hidden: card.querySelectorAll('.dw-link-rendered').length,
        // R5-V3：DOM 里 /f/ 直链总数仍是 4（3 条被隐藏 + 1 条过期可见）
        totalInDom: (card.querySelector('.event-content').textContent.match(/\/f\//g) || []).length,
        // R5-V4：过期那条在**可见文本**里仍看得到
        goneVisible: card.querySelector('.event-content').innerText.includes('deadbeef'),
        renderedOk: boxes.filter((a) => a.dataset.rendered === 'ok').length,
      };
    });
    assert.ok(got.alternating.every(Boolean), `每块必须是「名在上、内容在下」: ${JSON.stringify(got.alternating)}`);
    assert.ok(got.links.every(Boolean), `R5-V2：文件名必须是 <a> 且 href == 该块直链: ${JSON.stringify(got.links)}`);
    assert.strictEqual(got.hidden, 3, `R5-V3：应恰好隐藏 3 条已渲染链接，实测 ${got.hidden}`);
    assert.strictEqual(got.hidden, got.renderedOk, '被隐藏的条数必须等于成功渲染的块数');
    assert.strictEqual(got.totalInDom, 4, `🔴 隐藏必须是视觉隐藏：DOM 中 /f/ 直链总数应仍为 4，实测 ${got.totalInDom}`);
    assert.ok(got.goneVisible, 'R5-V4：HEAD 404 的过期链接必须在正文中保持可见（否则用户连复制都做不到）');
    await page.screenshot({ path: path.join(SHOTS, '06-r5-alternating.png'), fullPage: true });
  });

  // ── ⑯ R6:批次级状态(业主 09-18 一次传 6 张图:「多文件传输时,没有提示,不知道是不是结束了」)──
  //
  // 🔴 这条盯的是**批次层**,不是单文件层。单文件层由 ⑨ 盯着,两条断言互不覆盖:
  //    ⑨ 说「每个文件都有过自己的一行、完成后不残留」,⑯ 说「整批还剩多少、结束了没有」。
  //    正因为是两层,产品里批次行用的是独立类名 .batch-row 而不是 .upload-row ——
  //    若复用同一个类,⑨ 那句「完成后不得残留进度行」会被批次行连带打红/连带满足,
  //    两层就再也分不开了。
  await t('⑯ R6-V1/V2/V3/V4:批次行计数递增、全成功后消失、有失败时留住并说清数目、单文件不出批次行', async () => {
    const six = ['sample.pdf', 'sample.png', 'sample-small.txt', 'sample.docx', 'sample.mp4', 'sample.webm'].map(fx);
    // 🔴 淡出时长取自**产品源码里的常量**,不在测试里另写一个数字 ——
    //    两处各写一个,产品改了测试不会红,这条判据就变成摆设。
    const src = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
    const lingerMs = Number((src.match(/BATCH_DONE_LINGER_MS\s*=\s*(\d+)/) || [])[1]);
    assert.ok(Number.isFinite(lingerMs) && lingerMs > 0, '产品里必须有 BATCH_DONE_LINGER_MS 常量,本条判据靠它取时长');

    const reset = () => page.evaluate(() => {
      document.querySelectorAll('#uploadQueue .upload-row, #uploadQueue .batch-row').forEach((e) => e.remove());
      const el = document.getElementById('humanInput');
      el.value = '';
      el.dispatchEvent(new Event('input', { bubbles: true }));
    });

    // 🔴 全程采集批次行文案,用 MutationObserver 抓**每一次**取值,而不是定时采样。
    //    理由(实测过才这么写):settle(第 6 个) 与 finish() 在同一个同步块里连着改文案,
    //    任何间隔采样都可能整段错过 6/6 —— 而那恰好是 V1 要的终点值。
    //    childList 记录里的 removedNodes 带着被顶掉的旧文本节点,6/6 就在里面。
    const installProbe = () => page.evaluate(() => {
      window.__batchSeen = [];
      window.__batchEver = false;
      const push = (v) => {
        const t2 = (v || '').trim();
        if (t2 && window.__batchSeen[window.__batchSeen.length - 1] !== t2) window.__batchSeen.push(t2);
      };
      const scan = () => {
        if (document.querySelector('#uploadQueue .batch-row')) window.__batchEver = true;
        const cur = document.querySelector('#uploadQueue .batch-row .b-meta');
        if (cur) push(cur.textContent);
      };
      window.__batchObs = new MutationObserver((records) => {
        for (const r of records) {
          const tgt = r.target;
          if (tgt && tgt.classList && tgt.classList.contains('b-meta')) {
            for (const n of r.removedNodes) if (n.nodeType === 3) push(n.data);
          }
        }
        scan();
      });
      window.__batchObs.observe(document.getElementById('uploadQueue'),
        { childList: true, subtree: true, characterData: true });
      scan();
    });
    const readProbe = () => page.evaluate(() => {
      if (window.__batchObs) window.__batchObs.disconnect();
      return { seen: window.__batchSeen || [], ever: !!window.__batchEver };
    });

    let routed = false;
    try {
      // ── R6-V1 / R6-V2：6 个文件全部成功 ────────────────────────────
      await reset();
      await installProbe();
      await page.setInputFiles('#filePicker', six);
      await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 6,
        null, { timeout: 90000 });
      await page.screenshot({ path: path.join(SHOTS, '07-r6-batch-done.png') });
      const okRun = await readProbe();
      assert.ok(okRun.ever, 'R6-V1：多文件上传必须出现过批次行');
      // 🔴 沿时间断言,不断言某一瞬间:1/6 … 6/6 每一格都必须**出现过**。
      //    (这正是上一轮自己更正过的那个维度问题:需求是「整批进度看得见」,是时间性质。)
      for (let k = 1; k <= 6; k += 1) {
        assert.ok(okRun.seen.some((m) => m.startsWith(`${k}/6 `)),
          `R6-V1：批次计数 ${k}/6 没有出现过,实测采集到 ${JSON.stringify(okRun.seen)}`);
      }
      // 批次行必须同时给字节量,否则 36 MB 那一批仍然只能看个数字
      assert.ok(okRun.seen.some((m) => /\/\s*\d/.test(m) && /(B|KiB|MiB)/.test(m)),
        `R6-V1：批次行必须带「已传 / 总」字节,实测 ${JSON.stringify(okRun.seen)}`);
      // R6-V2 上半：完成态出现过
      assert.ok(okRun.seen.some((m) => /6 个文件已全部附加/.test(m)),
        `R6-V2：全部成功后必须出现明确的完成态文案,实测 ${JSON.stringify(okRun.seen)}`);
      // R6-V2 下半：之后必须消失(常驻就又回到裁定②要清的堆积)
      // 🔴 等不到就直接超时的话,报告里只有「Timeout 16000ms」——看不出等的是什么、实测是什么。
      //    自己接住,把当时的批次行状态原样打出来。
      try {
        await page.waitForFunction(() => document.querySelectorAll('#uploadQueue .batch-row').length === 0,
          null, { timeout: lingerMs + 12000 });
      } catch (e) {
        const cur = await page.evaluate(() => {
          const r = document.querySelector('#uploadQueue .batch-row');
          return r ? { state: r.dataset.batchState, meta: r.querySelector('.b-meta')?.textContent } : null;
        });
        throw new Error(`R6-V2：全部成功的批次完成态必须淡出,等 ${lingerMs + 12000} ms 后仍在,实测 ${JSON.stringify(cur)}`);
      }
      assert.strictEqual(await page.locator('#uploadQueue .batch-row').count(), 0,
        'R6-V2：全部成功的批次完成态必须淡出,不得常驻');

      // ── R6-V4：单文件不出批次行 ───────────────────────────────────
      await reset();
      await installProbe();
      await page.setInputFiles('#filePicker', [fx('sample.pdf')]);
      await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 1,
        null, { timeout: 60000 });
      await page.waitForTimeout(400);
      const oneRun = await readProbe();
      assert.strictEqual(oneRun.ever, false,
        `R6-V4：单文件上传不得出现批次行(1/1 是噪音),实测采集到 ${JSON.stringify(oneRun.seen)}`);

      // ── R6-V3：6 个里第 4 个失败 ──────────────────────────────────
      await reset();
      await installProbe();
      let nth = 0;
      await page.route('**/files/upload', async (route) => {
        nth += 1;
        if (nth === 4) {
          await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: '存储暂时不可用' }) });
        } else {
          await route.continue();
        }
      });
      routed = true;
      await page.setInputFiles('#filePicker', six);
      await page.waitForFunction(() => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= 5,
        null, { timeout: 90000 });
      // 等批次结算完(finish 在整批走完之后)
      try {
        await page.waitForFunction(() => {
          const r = document.querySelector('#uploadQueue .batch-row');
          return !!r && r.dataset.batchState === 'has-failure';
        }, null, { timeout: 30000 });
      } catch (e) {
        // 🔴 把实测终态打出来:失败被记成成功时,这里会是 all-ok 与「6 个文件已全部附加」,
        //    一眼看得出是计数错了,而不是「等超时了」。
        const cur = await page.evaluate(() => {
          const r = document.querySelector('#uploadQueue .batch-row');
          return r ? { state: r.dataset.batchState, meta: r.querySelector('.b-meta')?.textContent }
                   : { state: '(批次行已不在)', meta: null };
        });
        throw new Error(`R6-V3：等不到「有失败」的批次终态,实测 ${JSON.stringify(cur)}`);
      }
      const badRun = await readProbe();
      assert.ok(badRun.seen.some((m) => /5 个已附加/.test(m) && /1 个失败/.test(m)),
        `R6-V3：有失败时完成态必须说清数目,实测 ${JSON.stringify(badRun.seen)}`);
      // 🔴 R6-V3 的牙在这里:等**超过**淡出时长之后,批次行必须还在。
      await page.waitForTimeout(lingerMs + 2500);
      assert.strictEqual(await page.locator('#uploadQueue .batch-row').count(), 1,
        `R6-V3：有失败的批次完成态不得淡出,必须留到用户自己清掉(已等 ${lingerMs + 2500} ms)`);
      const badMeta = await page.locator('#uploadQueue .batch-row .b-meta').textContent();
      assert.ok(/1 个失败/.test(badMeta), `R6-V3：留在屏幕上的那一行必须写着失败数,实测 ${JSON.stringify(badMeta)}`);
      // 裁定②里已定、本次不变:失败的**单文件行**同样保留
      assert.ok(await page.locator('#uploadQueue .upload-row.failed').count() >= 1,
        'R6-V3：失败的单文件行必须保留(裁定②:完成态清、失败态留)');
      await page.screenshot({ path: path.join(SHOTS, '08-r6-batch-failure.png') });
    } finally {
      if (routed) await page.unroute('**/files/upload').catch(() => {});
      await reset().catch(() => {});
    }
  });

  // ── ⑰ B-11：音频在 A 侧接得上；文本的两条路径必须一致 ────────────────
  //
  // 🔴 起因（我在 B 线增量评审里打出、组长 2026-09-19 授权本批修）：
  //    B 侧本批把音频能力做全了（落盘 .mp3 ＋ 真实 audio MIME ＋ nginx 七行），
  //    而 A 侧 kindFromMediaType 没有 audio 分支、FILE_EXT_KIND 没有音频扩展名
  //    ⇒ 端到端仍渲成下载链接，「否则不播」那个目的没达成。
  //
  // 🔴 断言落在 data-kind 上，不落在 <audio> 在不在上：
  //    夹具是假字节、且本机 chromium 未必有对应解码器 ⇒ <audio> 必然自我移除。
  //    「判成 audio 但播不了」与「根本没判成 audio」在 DOM 上长得一样 ——
  //    这正是 ④b 当初吃过的亏。data-kind 把**分派**与**解码能力**分开。
  await t('⑰ B-11:音频两条路径都判成 audio 且必有下载出路;文本降级路径与 HEAD 路径一致;html/svg 仍 download', async () => {
    const mk = (id, ext) => `${BASE}/f/fixtureowner/fixture-a-2642/26/0918/${id}.${ext}`;
    const kindOf = (u) => page.evaluate((url) => {
      const box = [...document.querySelectorAll('.attachment')].find((e) => e.dataset.fileUrl === url);
      return box ? { kind: box.dataset.kind, dl: box.querySelectorAll('a.attachment-dl').length } : null;
    }, u);

    // ── (a) HEAD 路径：服务端声明 audio/mpeg ⇒ 必须判成 audio
    const headAudio = mk('aaaa1111bbbb2222cccc3333dddd4444', 'mp3');
    await page.route('**/f/**', (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 200, headers: { 'Content-Type': 'audio/mpeg' }, body: '' })
      : route.fulfill({ status: 200, contentType: 'audio/mpeg', body: 'ID3fake' })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), headAudio);
    await page.waitForSelector('.attachment[data-dispatch="head"]', { timeout: 20000 });
    const a1 = await kindOf(headAudio);
    assert.ok(a1, 'HEAD 路径下应有该 URL 的附件块');
    assert.strictEqual(a1.kind, 'audio', `🔴 HEAD 声明 audio/mpeg 必须判成 audio，实测 ${JSON.stringify(a1)}`);
    // 能播时也该能存一份；解不了码时它是唯一出路 —— 两种情况都必须恰好一条，不能并排两条。
    assert.strictEqual(a1.dl, 1, `音频块必须恰好一条下载链接，实测 ${a1.dl} 条`);
    await page.unroute('**/f/**');

    // ── (b) 降级路径：HEAD 不可用，按 URL 扩展名 ⇒ 七种音频扩展名逐个判成 audio
    await page.route('**/f/**', (route) => (route.request().method() === 'HEAD'
      ? route.fulfill({ status: 405, body: '' })
      : route.fulfill({ status: 200, contentType: 'application/octet-stream', body: 'fake' })));
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    const audioExts = ['mp3', 'm4a', 'flac', 'aac', 'opus', 'oga', 'wav'];
    // 文本：与 B 侧 internal/fileservice/manager.go 的 filenameTextExts 对齐的那 15 个
    const textExts = ['txt', 'md', 'csv', 'json', 'log', 'py', 'js', 'go', 'java', 'yaml', 'yml', 'toml', 'ini', 'sh', 'sql'];
    // 🔴 阳性对照与音频/文本**同一轮**：若守卫被削弱，这两个会跟着一起变 —— 那时才分辨得出。
    const neverInline = ['html', 'svg'];
    const all = [...audioExts, ...textExts, ...neverInline];
    for (let i = 0; i < all.length; i += 1) {
      const id = String(i).padStart(2, '0').repeat(16).slice(0, 32);
      await page.evaluate((u) => handleIncomingEvent({ kind: 'user_input', text: u, ts: Date.now() }), mk(id, all[i]));
    }
    await page.waitForFunction((n) => document.querySelectorAll('.attachment[data-dispatch="ext"]').length >= n,
      all.length, { timeout: 60000 });
    const got = {};
    for (let i = 0; i < all.length; i += 1) {
      const id = String(i).padStart(2, '0').repeat(16).slice(0, 32);
      got[all[i]] = await kindOf(mk(id, all[i]));
    }
    for (const ext of audioExts) {
      assert.strictEqual(got[ext]?.kind, 'audio',
        `🔴 降级路径 .${ext} 必须判成 audio，实测 ${JSON.stringify(got[ext])}`);
      assert.strictEqual(got[ext]?.dl, 1, `.${ext} 必须恰好一条下载链接，实测 ${JSON.stringify(got[ext])}`);
    }
    for (const ext of textExts) {
      assert.strictEqual(got[ext]?.kind, 'text',
        `🔴 降级路径 .${ext} 必须判成 text（与 HEAD 路径一致；B 侧 filenameTextExts 统一出 text/plain），实测 ${JSON.stringify(got[ext])}`);
    }
    for (const ext of neverInline) {
      assert.strictEqual(got[ext]?.kind, 'download',
        `阳性对照：.${ext} 永不内联，必须仍是 download，实测 ${JSON.stringify(got[ext])}`);
    }
    await page.screenshot({ path: path.join(SHOTS, '09-b11-audio-and-text.png'), fullPage: true });
    await page.unroute('**/f/**');
  });

  // ── ⑱⑲⑳㉑ 三个入口 ＋ 焦点维度(P0 2026-09-19 现网回归)───────────────
  //
  // 🔴 本批的成因不是某个函数写错,是**三个入口(粘贴/选择文件/拖放)共用下游,而只测了一个**。
  //    已有的 ⑥ 把 paste 事件**直接 dispatch 到 humanInput** —— 不管真实焦点在哪都一定触发,
  //    于是它测不出「焦点不在输入框时监听器根本不被调用」这一整类。
  //    ⇒ 下面的粘贴一律派发到 **document.activeElement**,与真实粘贴同形。
  const pasteAtFocus = (spec) => page.evaluate(({ name, type, b64, text }) => {
    const dt = new DataTransfer();
    if (b64) {
      const bin = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
      dt.items.add(new File([bin], name, { type }));
    } else {
      dt.setData('text/plain', text || '');
    }
    const target = document.activeElement || document.body;
    const notCancelled = target.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
    return { tag: target.tagName, cls: String(target.className || '').slice(0, 40), prevented: !notCancelled };
  }, spec);
  const pngSpec = () => ({ name: 'pasted-at-focus.png', type: 'image/png', b64: fs.readFileSync(fx('sample.png')).toString('base64') });

  await t('⑱ 三个入口(粘贴/选择文件/拖放)各一条,走同一下游 —— 本次回归的根因是只测了一个', async () => {
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    // 入口①粘贴:焦点在输入框,事件派发到 activeElement
    await page.evaluate(() => document.getElementById('humanInput').focus());
    let before = uploadCalls.length;
    const pasted = await pasteAtFocus(pngSpec());
    assert.strictEqual(pasted.prevented, true, `粘贴入口:处理器必须接管(preventDefault),实测 ${JSON.stringify(pasted)}`);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '粘贴入口必须发出一次上传');

    // 入口②选择文件
    before = uploadCalls.length;
    await page.setInputFiles('#filePicker', [fx('sample.pdf')]);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 2, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '选择文件入口必须发出一次上传');

    // 入口③拖放
    before = uploadCalls.length;
    const dropB64 = fs.readFileSync(fx('sample-small.txt')).toString('base64');
    await page.evaluate((b64) => {
      const bin = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([bin], 'dropped.txt', { type: 'text/plain' }));
      dispatchEvent(new DragEvent('dragenter', { dataTransfer: dt, bubbles: true }));
      dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true }));
    }, dropB64);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 3, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '拖放入口必须发出一次上传');
  });

  await t('⑲ 🔴 P0 根因:焦点在正文附件链接上时,粘贴必须仍然生效(修复前必红)', async () => {
    // 先让正文渲染出附件块 —— 业主页面上必然有:传完是要发出去的
    const text = await page.inputValue('#humanInput');
    assert.ok(/\/f\//.test(text), '本条依赖前一条留下的直链');
    await page.evaluate((body) => handleIncomingEvent({ kind: 'user_input', text: body, ts: Date.now() }), text);
    await page.waitForSelector('.attachments a', { timeout: 20000 });

    // 🔴 0.2.0.7 新增的可聚焦元素只在**正文附件区**:实测 #draftAttachments / #uploadQueue 里没有。
    const focusables = await page.evaluate(() => [...document.querySelectorAll('.attachments a[href]')]
      .map((e) => e.className || e.tagName));
    assert.ok(focusables.length > 0, `正文附件区必须有可聚焦链接(本条的前提),实测 ${JSON.stringify(focusables)}`);

    const before = uploadCalls.length;
    await page.locator('.attachments a[href]').first().focus();
    const info = await pasteAtFocus(pngSpec());
    assert.notStrictEqual(info.tag, 'TEXTAREA', `前提:焦点必须真的离开了输入框,实测 ${JSON.stringify(info)}`);
    assert.strictEqual(info.prevented, true,
      `🔴 焦点在 ${info.tag}.${info.cls} 上时粘贴必须仍被接管 —— 监听器绑在 humanInput 叶子上时,事件冒泡不经过它,现象就是「完全没反应、控制台也没报错」`);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
      before ? 4 : 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '🔴 焦点在正文链接上时,粘贴必须真的发出上传');
  });

  await t('⑳ 扩大作用域的代价必须钉住:全页各处粘贴【纯文本】都不得被接管', async () => {
    // 🔴 组长加的守卫:挂 document 之后全页粘贴都过这个处理器。
    //    「没文件就 return」是一句承诺,这里把它变成判据 —— 否则谁动了那行,全页文本粘贴一起坏,且无人转红。
    const spots = [
      { label: '输入框', focus: () => page.evaluate(() => document.getElementById('humanInput').focus()) },
      { label: 'body', focus: () => page.evaluate(() => { document.activeElement && document.activeElement.blur(); document.body.focus(); }) },
      { label: '正文附件链接', focus: () => page.locator('.attachments a[href]').first().focus() },
    ];
    for (const spot of spots) {
      await spot.focus();
      const valueBefore = await page.inputValue('#humanInput');
      const before = uploadCalls.length;
      const info = await pasteAtFocus({ text: '这是一段纯文本,不是文件' });
      await page.waitForTimeout(400);
      assert.strictEqual(info.prevented, false,
        `${spot.label}:纯文本粘贴不得被接管(必须保持浏览器原行为),实测 ${JSON.stringify(info)}`);
      assert.strictEqual(uploadCalls.length, before, `${spot.label}:纯文本粘贴不得触发上传`);
      assert.strictEqual(await page.inputValue('#humanInput'), valueBefore, `${spot.label}:纯文本粘贴不得由我们改动输入框`);
    }
  });

  await t('㉑ 只读会话粘贴文件:必须给出可读提示,不得静默', async () => {
    await page.route('**/status', (route) => route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify({ session: 'fixture-a-2642', state: 'idle', readonly: true, owner: 'fixture', model: 'fixture-model' }),
    }));
    try {
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === true, null, { timeout: 15000 });
      const before = uploadCalls.length;
      await page.evaluate(() => document.getElementById('humanInput').focus());
      // 🔴 这里我第一版断言「只读下仍应接管」,跑出来是红的 —— **断言写错了,不是产品错了**:
      //    只读时产品会 humanInput.disabled = true(renderer 1503 行),
      //    而 **disabled 的元素无法获得焦点** ⇒ activeElement 落回 body。
      //    ⇒ 只读下该钉的不是「接不接管」这个实现细节,而是业主看得见的两件事:
      //      ① 不得发出上传请求  ② 必须有可读提示,不得静默。
      const info = await pasteAtFocus(pngSpec());
      assert.ok(info.tag === 'BODY' || info.tag === 'TEXTAREA', `前提:只读下焦点落在 ${info.tag}`);
      await page.waitForTimeout(800);
      assert.strictEqual(uploadCalls.length, before, '只读会话不得发出上传请求');
      const shown = await page.evaluate(() => document.body.innerText);
      assert.ok(shown.includes('只读模式'),
        '🔴 只读下必须有可读提示(「只读模式:先解锁再上传文件」),不得静默 —— 静默就是又一条「发了没反应」');
    } finally {
      await page.route('**/status', (route) => route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({ session: 'fixture-a-2642', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' }),
      }));
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    }
  });

  // ── ㉒㉓㉔㉕ 0.2.1.2 UI 批 ────────────────────────────────────────

  await t('㉒ 🔴 阴格(业主确认):窄窗口 <880px 下 📎/📷 仍然可见', async () => {
    // 🔴 这条是业主确认过的阴格,不是我加的边角:
    //    现 CSS 里 .sb-res 在 <880px 整排 display:none。📎 直搬进状态栏就会在窄窗口消失,
    //    退回 0.2.0.5「界面上看不见按钮 = 功能不存在」那个坑。
    const sizes = [{ w: 1280, h: 900, label: '宽' }, { w: 820, h: 900, label: '窄<880' }, { w: 480, h: 900, label: '很窄' }];
    try {
      for (const size of sizes) {
        await page.setViewportSize({ width: size.w, height: size.h });
        await page.waitForTimeout(150);
        const vis = await page.evaluate(() => {
          const seen = (sel) => {
            const el = document.querySelector(sel);
            if (!el) return 'missing';
            const r = el.getBoundingClientRect();
            return (getComputedStyle(el).display !== 'none' && r.width > 0 && r.height > 0) ? 'visible' : 'hidden';
          };
          return { attach: seen('#fileLabel'), shot: seen('#shotBtn'), picker: !!document.querySelector('#filePicker') };
        });
        assert.strictEqual(vis.attach, 'visible', `${size.label}(${size.w}px):📎 必须可见,实测 ${JSON.stringify(vis)}`);
        assert.strictEqual(vis.shot, 'visible', `${size.label}(${size.w}px):📷 必须可见,实测 ${JSON.stringify(vis)}`);
        assert.ok(vis.picker, `${size.label}:file input 必须仍在 DOM 里(三入口之一)`);
      }
      // 阳性对照:同一轮里,**该被隐藏的**在窄窗口下确实隐藏了 ——
      // 🔴 只验"📎 可见"是半条:一个"把整条响应式规则删掉"的实现也能让它全过。
      await page.setViewportSize({ width: 820, height: 900 });
      await page.waitForTimeout(150);
      const resHidden = await page.evaluate(() => {
        // 🔴 必须挑【没有豁免】的那一个:📎/📷 同时挂着 .sb-res 与 .sb-always,
        //    拿它们做对照会把"豁免生效"误判成"响应式规则没了"。
        const el = document.querySelector('.statusbar .sb-res:not(.sb-always)');
        return el ? getComputedStyle(el).display === 'none' : 'missing';
      });
      assert.strictEqual(resHidden, true, `阳性对照:窄窗口下 .sb-res 应仍被隐藏(否则是整条响应式规则被删了),实测 ${resHidden}`);
    } finally {
      await page.setViewportSize({ width: 1280, height: 900 });
    }
  });

  await t('㉓ 🔴 断线必须看得见:🕐 那一栏进入断线态、显示第 N 次、点击可立即重试', async () => {
    // 🔴 自动重连 ＋ 无指示 ⇒ 死掉的页面与正常页面**看起来完全一样**,业主会对着断线页面等消息。
    //    本条盯的就是「断线这件事在屏幕上存在」。
    const before = await page.evaluate(() => ({
      offline: document.getElementById('clockStatus')?.classList.contains('offline'),
      text: document.getElementById('localTime')?.textContent,
    }));
    // 直接触发事件流错误:mock 的 /events 已是空流,这里显式踢一次 onerror
    await page.evaluate(() => { if (typeof eventSource !== 'undefined' && eventSource) eventSource.dispatchEvent(new Event('error')); });
    await page.waitForFunction(() => document.getElementById('clockStatus')?.classList.contains('offline'), null, { timeout: 15000 });
    const during = await page.evaluate(() => ({
      offline: document.getElementById('clockStatus')?.classList.contains('offline'),
      text: document.getElementById('localTime')?.textContent,
      clickable: document.getElementById('clockStatus')?.tagName,
    }));
    assert.strictEqual(during.offline, true, `断线时 🕐 必须进入可见的断线态,实测 ${JSON.stringify({ before, during })}`);
    assert.ok(/断线重连中/.test(during.text || ''), `断线态文案必须说明正在重连,实测 ${JSON.stringify(during.text)}`);
    assert.ok(/第 \d+ 次/.test(during.text || ''), `🔴 必须给出第几次 —— 只说"断线"看不出它在不在动,实测 ${JSON.stringify(during.text)}`);
    assert.strictEqual(during.clickable, 'BUTTON',
      '🔴 手动退路不能随「重连」按钮一起消失:这一栏必须是可点的 button');
    // 退避节奏取自源码常量,不在用例里另写一份数字
    const src = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
    const backoff = (/const RECONNECT_BACKOFF_MS = \[([^\]]*)\]/.exec(src) || [])[1];
    assert.ok(backoff, '产品里必须有 RECONNECT_BACKOFF_MS 常量');
    assert.deepStrictEqual(backoff.split(',').map((x) => Number(x.trim())), [1000, 2000, 5000, 10000, 30000],
      `退避必须是 1s→2s→5s→10s→30s 封顶(业主定 30s,组长收口先快后慢),实测 ${backoff}`);
  });

  await t('㉔ 状态栏精简:移除项不再出现、保留项仍在、无未捕获异常;「归属 归属」不再重复', async () => {
    // 🔴 组长点名要钉的那一条:被移除的元素**都有 id,而有 id 的东西通常有 JS 在引用**。
    //    删 DOM 不删引用 ⇒ getElementById 返回 null ⇒ 后续 `.textContent=` 抛错
    //    ⇒ **整段脚本静默停摆,后面的监听器一个都不绑** —— 正是粘贴那次的形状。
    //    所以这里从 reload 开始就收未捕获异常,并在收完之后再动一次三个入口。
    const pageErrors = [];
    const onPageError = (e) => pageErrors.push(String((e && e.stack) || e));
    page.on('pageerror', onPageError);
    try {
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });
    const bar = await page.evaluate(() => {
      const el = document.querySelector('.statusbar');
      return { html: el ? el.innerHTML : '', text: el ? el.innerText : '' };
    });
    for (const gone of ['lockHelp', 'headerOwner', 'sessionDot', 'sessionState', 'loadStat', 'modelStat', 'reconnectBtn']) {
      assert.ok(!bar.html.includes(`id="${gone}"`), `状态栏不得再有 ${gone}(业主点名移除)`);
    }
    for (const kept of ['themeBtn', 'localTime', 'fileLabel', 'shotBtn', 'memStat', 'diskStat', 'turnCost', 'totalCost']) {
      assert.ok(bar.html.includes(`id="${kept}"`), `状态栏必须保留 ${kept}(业主保留清单)`);
    }
    // 🔴 「归属 归属」是缺陷不是设计:HTML 一次、JS 再拼一次。移除后不得再出现任何一处。
    assert.ok(!/归属/.test(bar.text), `「归属」已按业主要求移除,实测状态栏文本 ${JSON.stringify(bar.text.slice(0, 120))}`);
    const src = fs.readFileSync(path.join(__dirname, 'renderer-canary-sse.html'), 'utf8');
    assert.ok(!/headerOwner\.textContent\s*=/.test(src), 'JS 里也不得再拼「归属」(原缺陷:标签与值各拼一次)');
    // 内存 ≥4 位数进位;磁盘去百分比
    const fmt = await page.evaluate(() => ({
      mem4: formatMem({ used_mb: 1687, total_mb: 15485, pct: 11 }),
      mem3: formatMem({ used_mb: 120, total_mb: 900, pct: 13 }),
      disk: formatDisk({ used_gb: 44.2, total_gb: 232.4, pct: 19 }),
    }));
    assert.ok(/GB/.test(fmt.mem4) && !/MB/.test(fmt.mem4), `内存 ≥4 位数必须进位到 GB,实测 ${fmt.mem4}`);
    assert.ok(/MB/.test(fmt.mem3), `内存 3 位数时不进位,实测 ${fmt.mem3}`);
    assert.ok(!/%/.test(fmt.mem4) && !/%/.test(fmt.disk), `内存/磁盘都不再带百分比,实测 ${fmt.mem4} | ${fmt.disk}`);
    assert.strictEqual(fmt.disk, '44.2 / 232.4 GB', `磁盘格式按业主给的样子,实测 ${fmt.disk}`);

    // 再动一次三个入口:若脚本已停摆,监听器根本没绑,这里会露出来
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); el.focus(); });
    const before = uploadCalls.length;
    await pasteAtFocus({ name: 'after-trim.png', type: 'image/png', b64: fs.readFileSync(fx('sample.png')).toString('base64') });
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '精简之后粘贴入口必须仍然工作(脚本没停摆)');
    await page.setInputFiles('#filePicker', [fx('sample.pdf')]);
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 2, { timeout: 30000 });

    assert.deepStrictEqual(pageErrors, [],
      `🔴 移除那七项之后页面不得有任何未捕获异常 —— 有异常 = 整段脚本可能已停摆,实测 ${JSON.stringify(pageErrors.slice(0, 3))}`);
    } finally {
      page.off('pageerror', onPageError);
    }
  });

  await t('㉕ 页内截屏:抓帧→框选→裁剪→**走既有上传管道**(选源那一步只能人工,见注释)', async () => {
    // 🔴 自动化覆盖到哪一步、哪一步只能人工 —— 写在这里,不留给读的人猜:
    //   能自动:抓到帧之后的**全部** —— 框选(经 __dwSetShotRect)、按选区裁剪、
    //           生成 image/png 的 File、走 uploadFiles 上传、直链回填输入框。
    //   只能人工:`getDisplayMedia` 弹出的**「选择共享内容」框** —— 它要求真实用户手势,
    //           headless 给不了。产品里为此留了显式测试缝 __dwSetScreenshotFrameSource,
    //           用例替换的**只有抓帧这一步**,其后每一步都是产品自己的代码。
    await page.evaluate(() => { const el = document.getElementById('humanInput'); el.value = ''; el.dispatchEvent(new Event('input', { bubbles: true })); });
    const before = uploadCalls.length;
    // 替身帧:200x100 的画布,左半红右半蓝 —— 裁右半应得纯蓝,以此验"真的按选区裁了"
    await page.evaluate(() => {
      window.__dwSetScreenshotFrameSource(() => {
        const c = document.createElement('canvas');
        c.width = 200; c.height = 100;
        const g = c.getContext('2d');
        g.fillStyle = '#ff0000'; g.fillRect(0, 0, 100, 100);
        g.fillStyle = '#0000ff'; g.fillRect(100, 0, 100, 100);
        return Promise.resolve(c);
      });
    });
    await page.click('#shotBtn');
    await page.waitForSelector('#shotOverlay:not([hidden])', { timeout: 15000 });
    assert.strictEqual(await page.evaluate(() => document.getElementById('shotCanvas').width), 200, '抓到的帧应进画布');
    // 框选右半
    await page.evaluate(() => window.__dwSetShotRect({ x: 100, y: 0, w: 100, h: 100 }));
    await page.click('#shotConfirm');
    await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n, 1, { timeout: 30000 });
    assert.strictEqual(uploadCalls.length, before + 1, '🔴 截屏必须走【既有】上传管道,发出一次上传');
    const draft = await page.inputValue('#humanInput');
    assert.ok(/screenshot-.*\.png/.test(draft), `输入框应回填截屏文件名,实测 ${JSON.stringify(draft.slice(0, 160))}`);
    assert.strictEqual(await page.evaluate(() => document.getElementById('shotOverlay').hidden), true, '上传后遮罩必须关掉');
    // 取消路径:不得上传
    const before2 = uploadCalls.length;
    await page.click('#shotBtn');
    await page.waitForSelector('#shotOverlay:not([hidden])', { timeout: 15000 });
    await page.click('#shotCancel');
    await page.waitForTimeout(500);
    assert.strictEqual(uploadCalls.length, before2, '点取消不得上传 —— 只验"能传"是半条');
    assert.strictEqual(await page.evaluate(() => document.getElementById('shotOverlay').hidden), true, '取消后遮罩必须关掉');
  });

  await t('㉖ 🔴 未调用测试缝时,走的必须是【真的 getDisplayMedia】(组长 ts-No.6 第①条)', async () => {
    // 🔴 组长的顾虑:测试缝哪天悄悄变成默认路径 ⇒ 「截屏」静默变成「传一张假图」。
    //    本条就是钉这个:**另起一个浏览器、不设任何缝**,走完整条真路径。
    //
    // 🔵 组长建议的开关方向对,但**他给的组合不对**,实测(见交付件):
    //    --auto-select-desktop-capture-source=…   ⇒ NotReadableError: Could not start video source
    //    --use-fake-ui-for-media-stream 单用       ⇒ 同样 NotReadableError
    //    🔴 真正成的是 **--use-fake-ui-for-media-stream ＋ --use-fake-device-for-media-stream**
    //       ⇒ 拿到真轨 video:screen:-3:0,1280x900,**不弹框**。
    //
    // 🔵 为什么 ㉕ 仍然保留测试缝:开关给的是**合成画面,内容不可控** ——
    //    裁剪对不对(裁右半必须得到右半)只有可控画面才验得了。
    //    ⇒ 两条分工:㉕ 验**裁得对不对**(缝,画面可控);㉖ 验**真路径通不通**(开关,不用缝)。
    const flagged = await chromium.launch({
      headless: true,
      args: ['--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream'],
    });
    try {
      const fctx = await flagged.newContext({ ignoreHTTPSErrors: true, viewport: { width: 1280, height: 900 } });
      const fpage = await fctx.newPage();
      await fpage.route('**/status', (route) => route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({ session: 'fixture-a-2642', state: 'idle', readonly: false, owner: 'fixture', model: 'fixture-model' }),
      }));
      await fpage.route('**/events', (route) => route.fulfill({ status: 200, contentType: 'text/event-stream', body: '' }));
      await fpage.route('**/taskboard**', (route) => route.fulfill({ status: 404, body: '' }));
      await fpage.goto(`${BASE}/renderer-canary-sse.html`, { waitUntil: 'domcontentloaded' });
      await fpage.waitForFunction(() => typeof stats !== 'undefined' && stats.readonly === false, null, { timeout: 15000 });

      // 🔴 关键:**一行缝都不设**。这一页上 screenshotFrameSource 必须仍是 null。
      assert.strictEqual(await fpage.evaluate(() => typeof window.__dwSetScreenshotFrameSource), 'function',
        '测试缝的设置函数应存在(但本条不调用它)');
      await fpage.click('#shotBtn');
      await fpage.waitForSelector('#shotOverlay:not([hidden])', { timeout: 20000 });
      const got = await fpage.evaluate(() => ({
        w: document.getElementById('shotCanvas').width,
        h: document.getElementById('shotCanvas').height,
      }));
      assert.ok(got.w > 0 && got.h > 0, `真 getDisplayMedia 必须抓到一帧,实测 ${JSON.stringify(got)}`);
      // 🔴 排他:㉕ 的替身帧是 200x100。这里若也拿到 200x100,说明缝变成了默认路径。
      assert.ok(!(got.w === 200 && got.h === 100),
        `🔴 抓到的帧尺寸与测试替身相同(200x100)⇒ 走的是缝而不是真 getDisplayMedia,实测 ${JSON.stringify(got)}`);
    } finally {
      await flagged.close();
    }
  });

  // ── ㉗ U7 固化：框选裁剪必须【真的按选区裁】──────────────────────
  await t('㉗ 🔴 U7:真实 pointer 拖选后,上传字节的 PNG IHDR 宽高必须 == 选区宽高(传整张即红)', async () => {
    // 本条钉的是 U7 变异(忽略选区、一律传整张)。此前 ㉕ 只断言"上传发生了 1 次 ＋ 文件名 ＋ 遮罩关闭",
    // 对 U7 **零判别力**(变异后 37 条全绿)—— 业主框选了却收到整屏,用例本来就查不出来。
    //
    // 三条判据 → 各自的断言(判据落在 assert 上,不落在注释上):
    //   ① 上传字节的 IHDR 宽高 == 选区宽高       → A0/A1/A2/A3
    //   ② 真实 pointer 拖选,不用 __dwSetShotRect  → B1/B2 ＋ 守卫 C ＋ 落点对账
    //   ③ U7 ⇒ 必须转红                          → A0(排他句:整张尺寸即红)
    //   附带 D:尺寸对 ≠ 裁对了地方(解码上传字节本身,采样必须是选区那一象限的纯色)
    //
    // 🔴 本条【不设】__dwSetShotRect —— 设了,canvas 被 max-width 缩放后的坐标换算段
    //    (renderer:4556 toSource)就永远不会被执行,守卫 C 也就无从谈起。
    // 🔵 帧源仍用 __dwSetScreenshotFrameSource,沿用 ㉕/㉖ 既定分工:
    //    缝 = 画面可控(验裁得对不对),浏览器开关(㉖) = 验真路径通不通。
    const SRC_W = 2400, SRC_H = 1200;                 // 帧源宽 > wrap 宽 ⇒ 必被 max-width 缩小
    const SEL = { x: 1500, y: 20, w: 600, h: 160 };   // 源坐标选区,整块落在【右上绿】象限内
    const bodies = [];
    const onReq = (req) => {
      if (!req.url().endsWith('/files/upload')) return;
      bodies.push(req.postDataBuffer());
    };
    page.on('request', onReq);
    try {
      const refsBefore = (await page.inputValue('#humanInput')).split('/f/').length - 1;
      // 帧源四象限异色:纯色帧验不出"尺寸对但偏移错",四色才验得出
      await page.evaluate(([W, H]) => {
        window.__dwSetScreenshotFrameSource(() => {
          const c = document.createElement('canvas');
          c.width = W; c.height = H;
          const g = c.getContext('2d');
          g.fillStyle = '#ff0000'; g.fillRect(0, 0, W / 2, H / 2);          // 左上 红
          g.fillStyle = '#00ff00'; g.fillRect(W / 2, 0, W / 2, H / 2);       // 右上 绿
          g.fillStyle = '#0000ff'; g.fillRect(0, H / 2, W / 2, H / 2);       // 左下 蓝
          g.fillStyle = '#ffff00'; g.fillRect(W / 2, H / 2, W / 2, H / 2);   // 右下 黄
          return Promise.resolve(c);
        });
      }, [SRC_W, SRC_H]);

      await page.click('#shotBtn');
      await page.waitForSelector('#shotOverlay:not([hidden])', { timeout: 15000 });
      const geo = await page.evaluate(() => {
        const box = (id) => {
          const r = document.getElementById(id).getBoundingClientRect();
          return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
        };
        return {
          canvas: box('shotCanvas'), wrap: box('shotWrap'),
          cw: document.getElementById('shotCanvas').width,
          ch: document.getElementById('shotCanvas').height,
          selHidden: document.getElementById('shotSel').hidden,
        };
      });
      // 帧源对账:必须是我这一帧,不是 ㉕ 留在页面上的 200x100(那条用例不清缝)
      assert.strictEqual(geo.cw, SRC_W, `画布宽应为帧源宽 ${SRC_W}(若为 200 ⇒ 沿用了 ㉕ 的替身帧),实测 ${geo.cw}`);
      assert.strictEqual(geo.ch, SRC_H, `画布高应为帧源高 ${SRC_H},实测 ${geo.ch}`);

      // 守卫 C:canvas 必须真被缩小过。缩放比=1 时坐标换算段等于没跑,
      //        本条对"换算写错"零判别力 ⇒ 那种情况下本条必须自己失败,不能假装验过。
      assert.ok(geo.canvas.width > 0 && geo.canvas.height > 0, `canvas 显示尺寸无效: ${JSON.stringify(geo.canvas)}`);
      assert.ok(Math.abs(geo.canvas.width - SRC_W) > 1,
        `🔴 守卫 C:显示宽 ${geo.canvas.width} ≈ 源宽 ${SRC_W} ⇒ 缩放比=1,坐标换算段未被执行,本条无判别力`);
      const scale = SRC_W / geo.canvas.width;

      // 拖拽端点:由 aspect 映射从源坐标反算,并且必须落在【用户真能看见】的区域内。
      // (canvas 会被 .shot-canvas-wrap 裁剪;拖到看不见的地方不算真实拖选)
      const vis = {
        left: Math.max(geo.canvas.left, geo.wrap.left),
        top: Math.max(geo.canvas.top, geo.wrap.top),
        right: Math.min(geo.canvas.right, geo.wrap.right),
        bottom: Math.min(geo.canvas.bottom, geo.wrap.bottom),
      };
      const css = (sx, sy) => ({
        x: geo.canvas.left + (sx / SRC_W) * geo.canvas.width,
        y: geo.canvas.top + (sy / SRC_H) * geo.canvas.height,
      });
      const p0 = css(SEL.x, SEL.y);
      const p1 = css(SEL.x + SEL.w, SEL.y + SEL.h);
      for (const [label, p] of [['起点', p0], ['终点', p1]]) {
        assert.ok(p.x > vis.left + 3 && p.x < vis.right - 3 && p.y > vis.top + 3 && p.y < vis.bottom - 3,
          `🔴 拖选${label} (${p.x.toFixed(1)},${p.y.toFixed(1)}) 不在可见区 `
          + `(${vis.left.toFixed(1)},${vis.top.toFixed(1)})-(${vis.right.toFixed(1)},${vis.bottom.toFixed(1)}) 内 `
          + '⇒ 布局变了,请重挑选区;别把拖选放到用户看不见的地方');
      }

      // 断言 B1:拖之前选框必须隐藏 —— 产品里只有 pointermove 会把它显示出来,
      //          这是"下面的选区确由 pointer 事件产生"的前半段证据。
      assert.strictEqual(geo.selHidden, true, '拖选前 #shotSel 应隐藏,否则 B2 那条证据不成立');

      // 记录浏览器【实际投递】的坐标:期望值从实收坐标算,不假设 Chrome 保留小数
      await page.evaluate(() => {
        window.__u7probe = [];
        const c = document.getElementById('shotCanvas');
        const rec = (k) => (e) => window.__u7probe.push([k, e.clientX, e.clientY]);
        c.addEventListener('pointerdown', rec('down'), true);
        c.addEventListener('pointermove', rec('move'), true);
      });
      await page.mouse.move(p0.x, p0.y);
      await page.mouse.down();
      await page.mouse.move(p1.x, p1.y, { steps: 12 });
      await page.mouse.up();
      // 断言 B2:选框出现了 ⇒ 产品的 pointerdown/pointermove 处理器真的执行过
      await page.waitForSelector('#shotSel:not([hidden])', { timeout: 5000 });

      const drag = await page.evaluate(() => {
        const rows = window.__u7probe || [];
        const moves = rows.filter((r) => r[0] === 'move');
        return {
          n: rows.length,
          down: rows.find((r) => r[0] === 'down') || null,
          last: moves.length ? moves[moves.length - 1] : null,
          shotRect: typeof shotRect !== 'undefined' ? shotRect : null,
        };
      });
      assert.ok(drag.down && drag.last, `未收到真实 pointer 事件(down=${!!drag.down}, 共 ${drag.n} 条)`);
      // 期望选区 = 实收坐标经 aspect 映射回源坐标。这就是缩放映射的定义式:
      // 本条验的是"实现有没有按这个定义做",不是"定义对不对"。
      const toSrc = (clientX, clientY) => ({
        x: (clientX - geo.canvas.left) * (SRC_W / geo.canvas.width),
        y: (clientY - geo.canvas.top) * (SRC_H / geo.canvas.height),
      });
      const s0 = toSrc(drag.down[1], drag.down[2]);
      const s1 = toSrc(drag.last[1], drag.last[2]);
      const expW = Math.round(Math.abs(s1.x - s0.x));
      const expH = Math.round(Math.abs(s1.y - s0.y));
      // 落点对账:拖到的必须是我指定的那块,否则下面的期望值无从谈起
      assert.ok(Math.abs(expW - SEL.w) <= 2 && Math.abs(expH - SEL.h) <= 2,
        `拖选落点偏离指定选区:指定 ${SEL.w}x${SEL.h},由实收坐标算出 ${expW}x${expH}`);

      await page.click('#shotConfirm');
      await page.waitForFunction((n) => (document.getElementById('humanInput').value.match(/\/f\//g) || []).length >= n,
        refsBefore + 1, { timeout: 30000 });

      assert.strictEqual(bodies.length, 1, `应恰好捕获 1 次上传请求体,实测 ${bodies.length}`);
      const buf = bodies[0];
      assert.ok(buf && buf.length > 24,
        '未取到上传请求体(postDataBuffer 为空)⇒ 无法验 IHDR。这不是产品失败,是取证手段失效,先修取证再判产品');

      // 断言 A:逐字节解析上传体的 PNG IHDR —— 不经任何图像库,读的就是线上那份字节
      const PNG_SIG = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
      assert.ok(buf.subarray(0, 8).equals(PNG_SIG), `上传字节不是 PNG:前 8 字节 ${buf.subarray(0, 8).toString('hex')}`);
      assert.strictEqual(buf.subarray(12, 16).toString('latin1'), 'IHDR',
        `第一个 chunk 不是 IHDR,实测 ${JSON.stringify(buf.subarray(12, 16).toString('latin1'))}`);
      const ihdrW = buf.readUInt32BE(16);
      const ihdrH = buf.readUInt32BE(20);

      // 断言 A0(U7 的指纹 · 排他句):整张 = 2400x1200。忽略选区一律传整张 ⇒ 必须在这里红。
      assert.ok(!(ihdrW === SRC_W && ihdrH === SRC_H),
        `🔴 U7:上传的是整张 ${SRC_W}x${SRC_H} ⇒ 选区被忽略了(业主"框选了却收到整屏"就是这个形状)`);
      // 断言 A1/A2:IHDR 宽高 == 选区宽高(判据①;不接受"上传发生了一次")
      assert.strictEqual(ihdrW, expW,
        `IHDR 宽应 == 选区宽 ${expW}(源 ${SRC_W} → 显示 ${geo.canvas.width},缩放比 ${scale.toFixed(4)}),实测 ${ihdrW}`);
      assert.strictEqual(ihdrH, expH, `IHDR 高应 == 选区高 ${expH},实测 ${ihdrH}`);
      // 断言 A3:与产品自己记下的选区对账(判据①的字面形式:上传字节 == 选区)
      assert.ok(drag.shotRect, '读不到产品的 shotRect(点上传前它应仍在)⇒ 无法做"选区 ↔ 上传字节"对账');
      assert.strictEqual(ihdrW, Math.round(drag.shotRect.w),
        `IHDR 宽 ${ihdrW} ≠ 产品选区宽 ${Math.round(drag.shotRect.w)}`);
      assert.strictEqual(ihdrH, Math.round(drag.shotRect.h),
        `IHDR 高 ${ihdrH} ≠ 产品选区高 ${Math.round(drag.shotRect.h)}`);

      // 断言 D:尺寸对 ≠ 裁对了地方。解码【上传字节本身】,四角+中心必须全是右上象限的纯绿
      const px = await page.evaluate(async (b64) => {
        const bin = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
        const bmp = await createImageBitmap(new Blob([bin], { type: 'image/png' }));
        const c = document.createElement('canvas');
        c.width = bmp.width; c.height = bmp.height;
        const g = c.getContext('2d');
        g.drawImage(bmp, 0, 0);
        const at = (x, y) => Array.from(g.getImageData(x, y, 1, 1).data.slice(0, 3));
        const w = bmp.width;
        const h = bmp.height;
        return {
          w, h,
          pts: { tl: at(3, 3), tr: at(w - 4, 3), bl: at(3, h - 4), br: at(w - 4, h - 4), mid: at(w >> 1, h >> 1) },
        };
      }, buf.toString('base64'));
      assert.strictEqual(px.w, ihdrW, `解码宽 ${px.w} 与 IHDR 宽 ${ihdrW} 不一致 ⇒ 读的不是同一份字节`);
      assert.strictEqual(px.h, ihdrH, `解码高 ${px.h} 与 IHDR 高 ${ihdrH} 不一致`);
      for (const [name, rgb] of Object.entries(px.pts)) {
        assert.deepStrictEqual(rgb, [0, 255, 0],
          `采样点 ${name} 应为纯绿 [0,255,0](选区整块在右上绿象限内),实测 [${rgb.join(',')}] ⇒ 尺寸对但裁错了地方`);
      }
    } finally {
      page.off('request', onReq);
      // 收尾:帧源清回 null、探针清掉。后续用例都不截屏,留在页面上只是隐患。
      await page.evaluate(() => {
        window.__dwSetScreenshotFrameSource(null);
        window.__u7probe = null;
      }).catch(() => {});
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
